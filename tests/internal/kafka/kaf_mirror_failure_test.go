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
	"kaf-mirror/internal/database"
	"kaf-mirror/internal/kafka"
	"kaf-mirror/tests/mocks"
	"sync"
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
	km, log := startMirrorForTest(t, fetchOf("big", partitionRecords{0, []int64{0, 1, 2}}), func(ctx context.Context, r *kgo.Record, f func(*kgo.Record, error)) {
		if string(r.Value) == "v1" {
			f(r, kerr.MessageTooLarge)
			return
		}
		f(r, nil)
	})

	reasons := make(chan string, 1)
	km.Start("job-32", func(database.ReplicationMetric) {}, func(jobID string, reason string) {
		km.Stop()
		reasons <- reason
	})

	select {
	case reason := <-reasons:
		assert.Contains(t, reason, "partition 0 offset 1")
		assert.Contains(t, reason, "MESSAGE_TOO_LARGE")
	case <-time.After(5 * time.Second):
		t.Error("job was not failed after the produce error")
		km.Stop()
	}
	assert.Equal(t, []string{"0/0"}, log.get())
}

func TestStopDuringBlockedProduceReturnsWithoutCommit(t *testing.T) {
	produced := make(chan struct{}, 1)
	km, log := startMirrorForTest(t, fetchOf("big", partitionRecords{0, []int64{0, 1}}), func(ctx context.Context, r *kgo.Record, f func(*kgo.Record, error)) {
		produced <- struct{}{}
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
