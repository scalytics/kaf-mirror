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
	"kaf-mirror/internal/database"
	"kaf-mirror/tests/mocks"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kgo"
)

type inFlight struct {
	record *kgo.Record
	ack    func(*kgo.Record, error)
}

func (f inFlight) key() string {
	return fmt.Sprintf("%d/%s", f.record.Partition, strings.TrimPrefix(string(f.record.Value), "v"))
}

func awaitInFlight(t *testing.T, sent chan inFlight, n int) []inFlight {
	t.Helper()
	var got []inFlight
	for len(got) < n {
		select {
		case f := <-sent:
			got = append(got, f)
		case <-time.After(5 * time.Second):
			keys := make([]string, 0, len(got))
			for _, f := range got {
				keys = append(keys, f.key())
			}
			t.Fatalf("only %d of %d records were in flight before the first ack: %v", len(got), n, keys)
		}
	}
	return got
}

func batchingMirror(t *testing.T, fetches kgo.Fetches, attempts int) (chan inFlight, chan []string, func()) {
	sent := make(chan inFlight, 64)
	km, _ := startMirrorForTest(t, fetches, func(ctx context.Context, r *kgo.Record, f func(*kgo.Record, error)) {
		sent <- inFlight{record: r, ack: f}
	})
	km.Consumer.SetProduceRetryForTest(attempts, 0)
	committed := make(chan []string, 16)
	km.Consumer.Client.(*mocks.MockKgoClient).CommitRecordsFunc = func(ctx context.Context, rs ...*kgo.Record) error {
		committed <- keys(rs)
		return nil
	}
	km.Start("job-50", func(database.ReplicationMetric) {}, func(string, string) {})
	return sent, committed, km.Stop
}

func assertNoCommit(t *testing.T, committed chan []string) {
	t.Helper()
	select {
	case call := <-committed:
		t.Fatalf("committed %v before the acks", call)
	default:
	}
}

func TestStartKeepsAWholeFetchInFlightAndCommitsOncePerPartition(t *testing.T) {
	fetches := kgo.Fetches{{Topics: []kgo.FetchTopic{
		fetchOf("big", partitionRecords{0, []int64{0, 1, 2, 3, 4}})[0].Topics[0],
	}}}
	fetches[0].Topics[0].Partitions = append(fetches[0].Topics[0].Partitions, fetchOf("big", partitionRecords{1, []int64{0, 1, 2}})[0].Topics[0].Partitions...)
	sent, committed, stop := batchingMirror(t, fetches, 5)
	defer stop()

	flights := awaitInFlight(t, sent, 8)
	assertNoCommit(t, committed)
	var order []string
	for _, f := range flights {
		order = append(order, f.key())
	}
	assert.Equal(t, []string{"0/0", "0/1", "0/2", "0/3", "0/4", "1/0", "1/1", "1/2"}, order)

	for _, f := range flights {
		f.ack(f.record, nil)
	}
	calls := waitForCommit(t, committed, "1/2")
	assert.Equal(t, [][]string{{"0/4", "1/2"}}, calls)
}

func TestStartFailureInsideABatchCommitsOnlyUpToTheLastAckedRecordBeforeIt(t *testing.T) {
	sent, committed, stop := batchingMirror(t, fetchOf("big", partitionRecords{0, []int64{0, 1, 2, 3, 4}}), 2)
	defer stop()

	flights := awaitInFlight(t, sent, 5)
	for _, f := range flights {
		if string(f.record.Value) == "v2" {
			f.ack(f.record, kerr.NotEnoughReplicas)
			continue
		}
		f.ack(f.record, nil)
	}
	calls := waitForCommit(t, committed, "0/1")
	assert.Equal(t, [][]string{{"0/1"}}, calls)

	resent := awaitInFlight(t, sent, 3)
	assertNoCommit(t, committed)
	var order []string
	for _, f := range resent {
		order = append(order, f.key())
	}
	assert.Equal(t, []string{"0/2", "0/3", "0/4"}, order)
	for _, f := range resent {
		f.ack(f.record, nil)
	}
	assert.Equal(t, [][]string{{"0/4"}}, waitForCommit(t, committed, "0/4"))
}

func TestStartFailsJobWhenARecordInsideABatchExhaustsItsAttempts(t *testing.T) {
	sent := make(chan inFlight, 64)
	km, log := startMirrorForTest(t, fetchOf("big", partitionRecords{0, []int64{0, 1, 2}}), func(ctx context.Context, r *kgo.Record, f func(*kgo.Record, error)) {
		sent <- inFlight{record: r, ack: f}
	})
	km.Consumer.SetProduceRetryForTest(1, 0)
	reasons := make(chan string, 1)
	km.Start("job-50", func(database.ReplicationMetric) {}, func(jobID string, reason string) {
		km.Stop()
		reasons <- reason
	})

	for _, f := range awaitInFlight(t, sent, 3) {
		if string(f.record.Value) == "v1" {
			f.ack(f.record, kerr.MessageTooLarge)
			continue
		}
		f.ack(f.record, nil)
	}
	select {
	case reason := <-reasons:
		assert.Contains(t, reason, "partition 0 offset 1 after 1 attempts")
		assert.Contains(t, reason, "MESSAGE_TOO_LARGE")
	case <-time.After(5 * time.Second):
		t.Fatal("the job was not failed")
	}
	assert.Equal(t, []string{"0/0"}, log.get())
}

func TestConsumeShutdownCommitsAckedRecordsWithALiveContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	log := &commitLog{}
	c := newRetryConsumer(fetchOf("orders", partitionRecords{0, []int64{0, 1, 2}}), cancel, log, 5, 0)
	var commitCtxErr error
	c.Client.(*mocks.MockKgoClient).CommitRecordsFunc = func(ctx context.Context, rs ...*kgo.Record) error {
		commitCtxErr = ctx.Err()
		return log.add(rs...)
	}

	var late []func(error)
	err := c.ConsumeAsync(ctx, func(ctx context.Context, r *kgo.Record, attempt int, done func(error)) {
		if r.Offset == 0 {
			done(nil)
			return
		}
		late = append(late, done)
		if r.Offset == 2 {
			cancel()
		}
	})

	require.NoError(t, err)
	assert.NoError(t, commitCtxErr)
	assert.Equal(t, []string{"0/0"}, log.get())
	for _, done := range late {
		done(nil)
		done(errors.New("late"))
	}
	assert.Equal(t, []string{"0/0"}, log.get())
	assert.Equal(t, int64(1), c.GetMetrics().RecordsProcessed)
}
