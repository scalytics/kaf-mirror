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
	"github.com/scalytics/kaf-mirror/v2/internal/database"
	"github.com/scalytics/kaf-mirror/v2/internal/kafka"
	"github.com/scalytics/kaf-mirror/v2/tests/mocks"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kgo"
)

func startMirrorForTest(t *testing.T, fetches kgo.Fetches, produce func(context.Context, *kgo.Record, func(*kgo.Record, error))) (*kafka.KafMirrorImpl, *commitLog) {
	log := &commitLog{}
	var mu sync.Mutex
	polls := 0
	consumer := kafka.NewConsumerForTest(nil, nil)
	consumer.Client = &mocks.MockKgoClient{
		PollFetchesFunc: func(ctx context.Context) kgo.Fetches {
			mu.Lock()
			polls++
			first := polls == 1
			mu.Unlock()
			if first {
				return fetches
			}
			<-ctx.Done()
			return kgo.Fetches{}
		},
		CommitRecordsFunc: func(ctx context.Context, rs ...*kgo.Record) error {
			return log.add(rs...)
		},
	}
	km := kafka.NewKafMirrorImplForTest(&kafka.Producer{Client: &mocks.MockKgoClient{ProduceFunc: produce}}, map[string]string{"big": "big"})
	km.Consumer = consumer
	return km, log
}

func TestStartFailsJobWithoutCommittingPastFailedRecord(t *testing.T) {
	var v1Calls atomic.Int32
	km, log := startMirrorForTest(t, fetchOf("big", partitionRecords{0, []int64{0, 1, 2}}), func(ctx context.Context, r *kgo.Record, f func(*kgo.Record, error)) {
		if string(r.Value) == "v1" {
			v1Calls.Add(1)
			f(r, kerr.MessageTooLarge)
			return
		}
		f(r, nil)
	})

	km.Consumer.SetProduceRetryForTest(5, time.Millisecond)

	reasons := make(chan string, 1)
	km.Start("job-32", func(database.ReplicationMetric) {}, func(jobID string, reason string) {
		km.Stop()
		reasons <- reason
	})

	select {
	case reason := <-reasons:
		assert.Contains(t, reason, "partition 0 offset 1")
		assert.Contains(t, reason, "after 5 attempts")
		assert.Contains(t, reason, "MESSAGE_TOO_LARGE")
		assert.Equal(t, int32(5), v1Calls.Load())
	case <-time.After(5 * time.Second):
		t.Error("job was not failed after the produce error")
		km.Stop()
	}
	assert.Equal(t, []string{"0/0"}, log.get())
}

func TestStopDuringBlockedProduceReturnsWithoutCommit(t *testing.T) {
	produced := make(chan struct{}, 1)
	km, log := startMirrorForTest(t, fetchOf("big", partitionRecords{0, []int64{0, 1}}), func(ctx context.Context, r *kgo.Record, f func(*kgo.Record, error)) {
		select {
		case produced <- struct{}{}:
		default:
		}
	})

	km.Start("job-32", func(database.ReplicationMetric) {}, func(string, string) {})
	<-produced

	stopped := make(chan struct{})
	go func() {
		km.Stop()
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("Stop did not return while a produce was waiting for the target")
	}
	require.Empty(t, log.get())
}

func TestStartRunsHookOncePerRecordAcrossRetries(t *testing.T) {
	var mu sync.Mutex
	produced := map[string]int{}
	km, _ := startMirrorForTest(t, fetchOf("big", partitionRecords{0, []int64{7, 8}}), func(ctx context.Context, r *kgo.Record, f func(*kgo.Record, error)) {
		mu.Lock()
		produced[string(r.Value)]++
		fail := string(r.Value) == "v7" && produced["v7"] <= 2
		mu.Unlock()
		if fail {
			go f(r, kerr.NotEnoughReplicas)
			return
		}
		go f(r, nil)
	})
	km.Consumer.SetProduceRetryForTest(5, 0)
	committed := make(chan []string, 10)
	km.Consumer.Client.(*mocks.MockKgoClient).CommitRecordsFunc = func(ctx context.Context, rs ...*kgo.Record) error {
		committed <- keys(rs)
		return nil
	}
	hooks := map[string]int{}
	km.SetRecordHook(func(topic string, key, value []byte) {
		mu.Lock()
		hooks[string(value)]++
		mu.Unlock()
	})
	var guards atomic.Int32
	km.SetRecordGuard(func() error { guards.Add(1); return nil })

	km.Start("job-50", func(database.ReplicationMetric) {}, func(string, string) {})
	waitForCommit(t, committed, "0/8")
	km.Stop()

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, 3, produced["v7"])
	assert.GreaterOrEqual(t, produced["v8"], 1)
	assert.Equal(t, map[string]int{"v7": 1, "v8": 1}, hooks)
	assert.GreaterOrEqual(t, guards.Load(), int32(4), "the guard must also run on both retries")
}

func TestProtectionTriggerDoesNotProduceOrCommitTheTriggeringRecord(t *testing.T) {
	var produced []string
	km, log := startMirrorForTest(t, fetchOf("big", partitionRecords{0, []int64{0, 1, 2}}), func(_ context.Context, r *kgo.Record, done func(*kgo.Record, error)) {
		produced = append(produced, string(r.Value))
		done(r, nil)
	})
	halted := false
	km.SetRecordHook(func(_ string, _, value []byte) {
		if string(value) == "v1" {
			halted = true
		}
	})
	km.SetRecordGuard(func() error {
		if halted {
			return errors.New("replication halted: high-entropy rewrite")
		}
		return nil
	})
	km.Consumer.SetProduceRetryForTest(2, 0)
	reason := startAndWaitForFailure(t, km, func(database.ReplicationMetric) {})
	assert.Contains(t, reason, "replication halted: high-entropy rewrite")
	assert.Equal(t, []string{"v0"}, produced)
	assert.Equal(t, []string{"0/0"}, log.get())
}

func keys(rs []*kgo.Record) []string {
	out := make([]string, 0, len(rs))
	for _, r := range rs {
		out = append(out, key(r))
	}
	return out
}

func waitForCommit(t *testing.T, committed chan []string, want string) [][]string {
	t.Helper()
	var calls [][]string
	for {
		select {
		case call := <-committed:
			calls = append(calls, call)
			for _, k := range call {
				if k == want {
					return calls
				}
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("no commit of %s, commits so far %v", want, calls)
			return calls
		}
	}
}
