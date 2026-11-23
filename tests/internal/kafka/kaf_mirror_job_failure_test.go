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

func startAndWaitForFailure(t *testing.T, km *kafka.KafMirrorImpl) string {
	reasons := make(chan string, 1)
	km.Start("job-33", func(database.ReplicationMetric) {}, func(jobID string, reason string) {
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

	reason := startAndWaitForFailure(t, km)

	assert.Equal(t, "producer has failed 101 consecutive times", reason)
	assert.True(t, flags.consumer.Load())
	assert.True(t, flags.producer.Load())
}

func TestDiscoveryErrorStopsJobAndClosesClients(t *testing.T) {
	restore := kafka.SetAdminClientFactoryForTest(func(config.ClusterConfig) (kafka.AdminClientAPI, error) {
		return nil, errors.New("source unreachable")
	})
	defer restore()
	km, flags := idleMirrorForTest(t, nil)
	require.NoError(t, km.AddRegexMapForTest("^big.*", "$0"))
	km.SetIntervalsForTest(time.Hour, time.Millisecond)

	reason := startAndWaitForFailure(t, km)

	assert.Equal(t, "topic discovery failed: failed to create source admin client: source unreachable", reason)
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

	reason := startAndWaitForFailure(t, km)

	assert.Equal(t, "consumer panic: hook exploded", reason)
	assert.True(t, closed.consumer.Load())
	assert.Empty(t, log.get())
}
