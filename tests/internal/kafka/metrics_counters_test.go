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
	"sync"
	"testing"
	"time"

	"kaf-mirror/internal/database"
	"kaf-mirror/tests/mocks"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kgo"
)

func awaitMetric(t *testing.T, metrics chan database.ReplicationMetric, ok func(database.ReplicationMetric) bool) database.ReplicationMetric {
	t.Helper()
	for {
		select {
		case m := <-metrics:
			if ok(m) {
				return m
			}
		case <-time.After(5 * time.Second):
			t.Fatal("no matching metric")
			return database.ReplicationMetric{}
		}
	}
}

func TestReplicatedCountsEachRecordOnceWhenARetryResendsAckedRecords(t *testing.T) {
	sent := make(chan inFlight, 64)
	km, _ := startMirrorForTest(t, fetchOf("big", partitionRecords{0, []int64{0, 1, 2, 3, 4}}), func(ctx context.Context, r *kgo.Record, f func(*kgo.Record, error)) {
		sent <- inFlight{record: r, ack: f}
	})
	km.Consumer.SetProduceRetryForTest(2, 0)
	km.Consumer.Client.(*mocks.MockKgoClient).CommitRecordsFunc = func(context.Context, ...*kgo.Record) error { return nil }
	km.SetIntervalsForTest(time.Millisecond, 0)
	metrics := make(chan database.ReplicationMetric, 1024)
	km.Start("job-43", func(m database.ReplicationMetric) {
		select {
		case metrics <- m:
		default:
		}
	}, func(string, string) {})
	defer km.Stop()

	for _, f := range awaitInFlight(t, sent, 5) {
		if string(f.record.Value) == "v2" {
			f.ack(f.record, kerr.NotEnoughReplicas)
			continue
		}
		f.ack(f.record, nil)
	}
	for _, f := range awaitInFlight(t, sent, 3) {
		f.ack(f.record, nil)
	}

	m := awaitMetric(t, metrics, func(m database.ReplicationMetric) bool { return m.MessagesConsumed == 5 })
	assert.Equal(t, 5, m.MessagesReplicated)
	assert.Equal(t, 10, m.BytesTransferred)
	assert.Equal(t, 1, m.ErrorCount)
	assert.Equal(t, int64(7), km.Producer.GetMetrics().RecordsProduced)
}

func TestOneProduceErrorLogsOneErrorSpike(t *testing.T) {
	var calls sync.Map
	km, _ := startMirrorForTest(t, fetchOf("big", partitionRecords{0, []int64{0, 1, 2}}), func(ctx context.Context, r *kgo.Record, f func(*kgo.Record, error)) {
		if _, seen := calls.LoadOrStore(string(r.Value), true); !seen && string(r.Value) == "v1" {
			f(r, kerr.NotEnoughReplicas)
			return
		}
		f(r, nil)
	})
	km.Consumer.SetProduceRetryForTest(5, 0)
	km.SetIntervalsForTest(time.Millisecond, 0)
	var mu sync.Mutex
	var started []string
	km.SetIncidentHookForTest(func(name string) {
		mu.Lock()
		defer mu.Unlock()
		started = append(started, name)
	})
	metrics := make(chan database.ReplicationMetric, 1024)
	km.Start("job-49", func(m database.ReplicationMetric) {
		select {
		case metrics <- m:
		default:
		}
	}, func(string, string) {})

	spikes := 0
	awaitMetric(t, metrics, func(m database.ReplicationMetric) bool {
		if m.ErrorSpike {
			spikes++
		}
		return m.ErrorCount == 1
	})
	for i := 0; i < 20; i++ {
		m := awaitMetric(t, metrics, func(database.ReplicationMetric) bool { return true })
		require.Equal(t, 1, m.ErrorCount)
		if m.ErrorSpike {
			spikes++
		}
	}
	km.Stop()

	assert.Equal(t, 1, spikes)
	mu.Lock()
	defer mu.Unlock()
	count := 0
	for _, name := range started {
		if name == "error_spike" {
			count++
		}
	}
	assert.Equal(t, 1, count)
}
