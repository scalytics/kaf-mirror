// Copyright 2025 Scalytics, Inc. and Scalytics Europe, LTD
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//     http://www.apache.org/licenses/LICENSE-2.0
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package kafka_test

import (
	"context"
	"errors"
	"kaf-mirror/internal/config"
	"kaf-mirror/internal/database"
	"kaf-mirror/internal/kafka"
	"kaf-mirror/tests/mocks"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kgo"
)

type flakyAdmin struct {
	fakeAdmin
	down   *atomic.Bool
	failed chan struct{}
}

func (a *flakyAdmin) GetClusterInfo(ctx context.Context) (*kafka.ClusterInfo, error) {
	if a.down.Load() {
		signal(a.failed)
		return nil, errors.New("source unreachable")
	}
	return a.info, nil
}

func signal(ch chan struct{}) {
	select {
	case ch <- struct{}{}:
	default:
	}
}

func receive[T any](t *testing.T, ch chan T, what string) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
		var zero T
		return zero
	}
}

type feedMirror struct {
	km       *kafka.KafMirrorImpl
	feed     chan kgo.Fetches
	added    chan string
	produced chan string
	closed   atomic.Bool
}

func newFeedMirror() *feedMirror {
	f := &feedMirror{feed: make(chan kgo.Fetches), added: make(chan string, 10), produced: make(chan string, 10)}
	consumer := kafka.NewConsumerForTest(nil, nil)
	consumer.Client = &mocks.MockKgoClient{
		PollFetchesFunc: func(ctx context.Context) kgo.Fetches {
			select {
			case fs := <-f.feed:
				return fs
			case <-ctx.Done():
				return kgo.Fetches{}
			}
		},
		CommitRecordsFunc: func(context.Context, ...*kgo.Record) error { return nil },
		AddConsumeTopicsFunc: func(topics ...string) {
			for _, t := range topics {
				f.added <- t
			}
		},
		CloseFunc: func() { f.closed.Store(true) },
	}
	f.km = kafka.NewKafMirrorImplForTest(&kafka.Producer{Client: &mocks.MockKgoClient{ProduceFunc: func(ctx context.Context, r *kgo.Record, cb func(*kgo.Record, error)) {
		f.produced <- r.Topic
		cb(r, nil)
	}}}, map[string]string{"big": "big"})
	f.km.Consumer = consumer
	return f
}

func TestDiscoveryOutageKeepsJobRunningAndRecovers(t *testing.T) {
	var down atomic.Bool
	var factoryCalls atomic.Int32
	failed := make(chan struct{}, 5)
	down.Store(true)
	restore := kafka.SetAdminClientFactoryForTest(func(config.ClusterConfig) (kafka.AdminClientAPI, error) {
		if factoryCalls.Add(1) == 1 {
			signal(failed)
			return nil, errors.New("dial tcp: connection refused")
		}
		info := &kafka.ClusterInfo{Topics: map[string]kafka.TopicInfo{
			"big":     {Name: "big", Partitions: 1},
			"big.new": {Name: "big.new", Partitions: 1},
		}}
		return &flakyAdmin{fakeAdmin: fakeAdmin{info: info}, down: &down, failed: failed}, nil
	})
	defer restore()
	f := newFeedMirror()
	require.NoError(t, f.km.AddRegexMapForTest("^big.*", "$0"))
	f.km.SetIntervalsForTest(time.Hour, time.Millisecond)
	var failures atomic.Int32
	f.km.Start("job-f2", func(database.ReplicationMetric) {}, func(string, string) { failures.Add(1) })

	for i := 0; i < 5; i++ {
		receive(t, failed, "a failed discovery run")
	}
	f.feed <- fetchOf("big", partitionRecords{0, []int64{0}})
	assert.Equal(t, "big", receive(t, f.produced, "the record of big"))
	assert.False(t, f.closed.Load())

	down.Store(false)
	assert.Equal(t, "big.new", receive(t, f.added, "big.new to be added"))
	f.feed <- fetchOf("big.new", partitionRecords{0, []int64{0}})
	assert.Equal(t, "big.new", receive(t, f.produced, "the record of big.new"))

	done := make(chan struct{})
	go func() {
		f.km.Stop()
		close(done)
	}()
	receive(t, done, "Stop to return")
	f.km.WaitForFailureReportsForTest()
	assert.Equal(t, int32(0), failures.Load())
	assert.True(t, f.closed.Load())
}

type rejectingAdmin struct {
	fakeAdmin
	enter func()
}

func (a *rejectingAdmin) EnsureTopicExists(ctx context.Context, topicName string, partitions int32, replicationFactor int16) error {
	a.enter()
	return errors.New("target unreachable")
}

func TestStopDuringTopicCreateRetryReturnsPromptly(t *testing.T) {
	entered := make(chan struct{})
	var once sync.Once
	restore := kafka.SetAdminClientFactoryForTest(func(config.ClusterConfig) (kafka.AdminClientAPI, error) {
		info := &kafka.ClusterInfo{Topics: map[string]kafka.TopicInfo{"big.new": {Name: "big.new", Partitions: 1}}}
		return &rejectingAdmin{fakeAdmin: fakeAdmin{info: info}, enter: func() { once.Do(func() { close(entered) }) }}, nil
	})
	defer restore()
	km, _ := idleMirrorForTest(t, nil)
	require.NoError(t, km.AddRegexMapForTest("^big.*", "$0"))
	km.SetIntervalsForTest(time.Hour, time.Millisecond)
	var failures atomic.Int32
	km.Start("job-f2", func(database.ReplicationMetric) {}, func(string, string) { failures.Add(1) })
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("discovery did not try to create the topic")
	}

	start := time.Now()
	km.Stop()
	km.WaitForFailureReportsForTest()

	assert.Less(t, time.Since(start), 500*time.Millisecond)
	assert.Equal(t, int32(0), failures.Load())
}
