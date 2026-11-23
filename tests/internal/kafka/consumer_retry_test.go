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
	"fmt"
	"kaf-mirror/internal/kafka"
	"kaf-mirror/tests/mocks"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kgo"
)

type partitionRecords struct {
	partition int32
	offsets   []int64
}

func fetchOf(topic string, parts ...partitionRecords) kgo.Fetches {
	ft := kgo.FetchTopic{Topic: topic}
	for _, p := range parts {
		fp := kgo.FetchPartition{Partition: p.partition}
		for _, o := range p.offsets {
			fp.Records = append(fp.Records, &kgo.Record{Topic: topic, Partition: p.partition, Offset: o, Value: []byte(fmt.Sprintf("v%d", o))})
		}
		fp.HighWatermark = int64(len(p.offsets))
		ft.Partitions = append(ft.Partitions, fp)
	}
	return kgo.Fetches{{Topics: []kgo.FetchTopic{ft}}}
}

type commitLog struct {
	mu      sync.Mutex
	commits []string
}

func (l *commitLog) add(rs ...*kgo.Record) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, r := range rs {
		l.commits = append(l.commits, fmt.Sprintf("%d/%d", r.Partition, r.Offset))
	}
	return nil
}

func (l *commitLog) get() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.commits...)
}

func newRetryConsumer(fetches kgo.Fetches, cancel context.CancelFunc, log *commitLog, attempts int, backoff time.Duration) *kafka.Consumer {
	polls := 0
	c := kafka.NewConsumerForTest(nil, nil)
	c.Client = &mocks.MockKgoClient{
		PollFetchesFunc: func(ctx context.Context) kgo.Fetches {
			polls++
			if polls == 1 {
				return fetches
			}
			cancel()
			return kgo.Fetches{}
		},
		CommitRecordsFunc: func(ctx context.Context, rs ...*kgo.Record) error {
			return log.add(rs...)
		},
	}
	c.SetProduceRetryForTest(attempts, backoff)
	return c
}

func key(r *kgo.Record) string {
	return fmt.Sprintf("%d/%d", r.Partition, r.Offset)
}

func TestConsumeRetriesFailedProduceThenCommits(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	log := &commitLog{}
	c := newRetryConsumer(fetchOf("orders", partitionRecords{0, []int64{0, 1, 2}}), cancel, log, 5, 0)

	calls := map[string]int{}
	err := c.Consume(ctx, func(ctx context.Context, r *kgo.Record) error {
		calls[key(r)]++
		if r.Offset == 1 && calls[key(r)] < 3 {
			return errors.New("produce failed")
		}
		return nil
	})

	require.NoError(t, err)
	assert.Equal(t, 3, calls["0/1"])
	assert.Equal(t, []string{"0/0", "0/1", "0/2"}, log.get())
	assert.Equal(t, int64(3), c.GetMetrics().RecordsProcessed)
}

func TestConsumeRetryExhaustionStopsWithoutCommittingPastFailedRecord(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	log := &commitLog{}
	c := newRetryConsumer(fetchOf("orders", partitionRecords{0, []int64{0, 1, 2}}), cancel, log, 3, 0)

	calls := map[string]int{}
	err := c.Consume(ctx, func(ctx context.Context, r *kgo.Record) error {
		calls[key(r)]++
		if r.Offset == 1 {
			return errors.New("MESSAGE_TOO_LARGE")
		}
		return nil
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "topic orders partition 0 offset 1 after 3 attempts")
	assert.Contains(t, err.Error(), "MESSAGE_TOO_LARGE")
	assert.Equal(t, 3, calls["0/1"])
	assert.Equal(t, 0, calls["0/2"])
	assert.Equal(t, []string{"0/0"}, log.get())
	assert.Equal(t, int64(1), c.GetMetrics().RecordsProcessed)
}

func TestConsumeStopsOtherPartitionsAfterRetryExhaustion(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	log := &commitLog{}
	c := newRetryConsumer(fetchOf("orders", partitionRecords{0, []int64{0}}, partitionRecords{1, []int64{0}}), cancel, log, 1, 0)

	calls := map[string]int{}
	err := c.Consume(ctx, func(ctx context.Context, r *kgo.Record) error {
		calls[key(r)]++
		return errors.New("produce failed")
	})

	require.Error(t, err)
	assert.Equal(t, 1, calls["0/0"])
	assert.Equal(t, 0, calls["1/0"])
	assert.Empty(t, log.get())
}

func TestConsumeShutdownDuringRetryBackoffDoesNotCommit(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	log := &commitLog{}
	c := newRetryConsumer(fetchOf("orders", partitionRecords{0, []int64{0, 1, 2}}), cancel, log, 5, time.Hour)

	calls := map[string]int{}
	err := c.Consume(ctx, func(ctx context.Context, r *kgo.Record) error {
		calls[key(r)]++
		if r.Offset == 1 {
			cancel()
			return errors.New("produce failed")
		}
		return nil
	})

	require.NoError(t, err)
	assert.Equal(t, 1, calls["0/1"])
	assert.Equal(t, 0, calls["0/2"])
	assert.Equal(t, []string{"0/0"}, log.get())
}

func TestConsumeShutdownDuringInFlightProduceDoesNotCommit(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	log := &commitLog{}
	c := newRetryConsumer(fetchOf("orders", partitionRecords{0, []int64{0, 1}}), cancel, log, 5, 0)

	err := c.Consume(ctx, func(ctx context.Context, r *kgo.Record) error {
		cancel()
		<-ctx.Done()
		return ctx.Err()
	})

	require.NoError(t, err)
	assert.Empty(t, log.get())
}

func TestConsumeRevokedPartitionStopsRetryWithoutCommit(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	log := &commitLog{}
	c := newRetryConsumer(fetchOf("orders", partitionRecords{0, []int64{0, 1, 2}}, partitionRecords{1, []int64{0}}), cancel, log, 5, time.Hour)

	calls := map[string]int{}
	err := c.Consume(ctx, func(ctx context.Context, r *kgo.Record) error {
		calls[key(r)]++
		if r.Partition == 0 && r.Offset == 1 {
			c.RevokePartitions(map[string][]int32{"orders": {0}})
			return errors.New("produce failed")
		}
		return nil
	})

	require.NoError(t, err)
	assert.Equal(t, 1, calls["0/1"])
	assert.Equal(t, 0, calls["0/2"])
	assert.Equal(t, 1, calls["1/0"])
	assert.Equal(t, []string{"0/0", "1/0"}, log.get())
}

func TestConsumeSkipsPartitionRevokedBeforeItsRecordsAreHandled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	log := &commitLog{}
	c := newRetryConsumer(fetchOf("orders", partitionRecords{1, []int64{0}}, partitionRecords{0, []int64{0, 1}}), cancel, log, 5, 0)

	calls := map[string]int{}
	err := c.Consume(ctx, func(ctx context.Context, r *kgo.Record) error {
		calls[key(r)]++
		if r.Partition == 1 {
			c.RevokePartitions(map[string][]int32{"orders": {0}})
		}
		return nil
	})

	require.NoError(t, err)
	assert.Equal(t, 0, calls["0/0"])
	assert.Equal(t, 0, calls["0/1"])
	assert.Equal(t, []string{"1/0"}, log.get())
}
