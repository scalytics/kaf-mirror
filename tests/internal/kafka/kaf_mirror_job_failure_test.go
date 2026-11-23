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

type closeFlags struct {
	consumer atomic.Bool
	producer atomic.Bool
}

func idleMirrorForTest(t *testing.T, produce func(context.Context, *kgo.Record, func(*kgo.Record, error))) (*kafka.KafMirrorImpl, *closeFlags) {
	km, _ := startMirrorForTest(t, kgo.Fetches{}, produce)
	flags := &closeFlags{}
	km.Consumer.Client.(*mocks.MockKgoClient).CloseFunc = func() { flags.consumer.Store(true) }
	km.Producer.Client.(*mocks.MockKgoClient).CloseFunc = func() { flags.producer.Store(true) }
	return km, flags
}

func startAndWaitForFailure(t *testing.T, km *kafka.KafMirrorImpl, metrics func(database.ReplicationMetric)) string {
	reasons := make(chan string, 1)
	km.Start("job-33", metrics, func(jobID string, reason string) {
		km.Stop()
		reasons <- reason
	})
	select {
	case reason := <-reasons:
		return reason
	case <-time.After(5 * time.Second):
		t.Fatal("the failure handler did not stop the job")
		return ""
	}
}

func TestProducerErrorLimitStopsJobAndClosesClients(t *testing.T) {
	km, flags := idleMirrorForTest(t, func(ctx context.Context, r *kgo.Record, f func(*kgo.Record, error)) {
		f(r, errors.New("produce failed"))
	})
	for i := 0; i < 101; i++ {
		km.Producer.Produce(context.Background(), &kgo.Record{Topic: "big"}, nil)
	}
	km.SetIntervalsForTest(time.Millisecond, 0)

	reason := startAndWaitForFailure(t, km, func(database.ReplicationMetric) {})

	assert.Equal(t, "producer has failed 101 consecutive times", reason)
	assert.True(t, flags.consumer.Load())
	assert.True(t, flags.producer.Load())
}

func TestDiscoveryMappingConflictStopsJobAndClosesClients(t *testing.T) {
	restore := kafka.SetAdminClientFactoryForTest(func(config.ClusterConfig) (kafka.AdminClientAPI, error) {
		return &fakeAdmin{info: &kafka.ClusterInfo{Topics: map[string]kafka.TopicInfo{"ev.a": {Name: "ev.a", Partitions: 1}}}}, nil
	})
	defer restore()
	km, flags := idleMirrorForTest(t, nil)
	require.NoError(t, km.AddRegexMapForTest(`^ev\..*$`, "big"))
	km.SetIntervalsForTest(time.Hour, time.Millisecond)

	reason := startAndWaitForFailure(t, km, func(database.ReplicationMetric) {})

	assert.Equal(t, "topic discovery failed: target topic big is mapped from multiple sources: big and ev.a", reason)
	assert.True(t, flags.consumer.Load())
	assert.True(t, flags.producer.Load())
}

func TestConsumerPanicStopsJobAndClosesClients(t *testing.T) {
	km, log := startMirrorForTest(t, fetchOf("big", partitionRecords{0, []int64{0}}), func(ctx context.Context, r *kgo.Record, f func(*kgo.Record, error)) {
		f(r, nil)
	})
	closed := &closeFlags{}
	km.Consumer.Client.(*mocks.MockKgoClient).CloseFunc = func() { closed.consumer.Store(true) }
	km.SetRecordHook(func(string, []byte, []byte) { panic("hook exploded") })

	reason := startAndWaitForFailure(t, km, func(database.ReplicationMetric) {})

	assert.Equal(t, "consumer panic: hook exploded", reason)
	assert.True(t, closed.consumer.Load())
	assert.Empty(t, log.get())
}

func TestMetricsPanicStopsJobAndClosesClients(t *testing.T) {
	km, flags := idleMirrorForTest(t, nil)
	km.SetIntervalsForTest(time.Millisecond, 0)

	reason := startAndWaitForFailure(t, km, func(database.ReplicationMetric) { panic("sink exploded") })

	assert.Equal(t, "metrics panic: sink exploded", reason)
	assert.True(t, flags.consumer.Load())
	assert.True(t, flags.producer.Load())
}

type blockingAdmin struct {
	fakeAdmin
	enter func()
}

func (b *blockingAdmin) GetClusterInfo(ctx context.Context) (*kafka.ClusterInfo, error) {
	b.enter()
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestStopDuringDiscoveryDoesNotFailJob(t *testing.T) {
	entered := make(chan struct{})
	var once sync.Once
	restore := kafka.SetAdminClientFactoryForTest(func(config.ClusterConfig) (kafka.AdminClientAPI, error) {
		return &blockingAdmin{fakeAdmin: fakeAdmin{info: &kafka.ClusterInfo{}}, enter: func() { once.Do(func() { close(entered) }) }}, nil
	})
	defer restore()
	km, _ := idleMirrorForTest(t, nil)
	require.NoError(t, km.AddRegexMapForTest("^big.*", "$0"))
	km.SetIntervalsForTest(time.Hour, time.Millisecond)
	var failures atomic.Int32
	km.Start("job-33", func(database.ReplicationMetric) {}, func(string, string) { failures.Add(1) })
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("discovery did not start")
	}

	km.Stop()
	km.WaitForFailureReportsForTest()

	assert.Equal(t, int32(0), failures.Load())
}
