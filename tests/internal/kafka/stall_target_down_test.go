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
	"sync"
	"testing"
	"time"

	"github.com/scalytics/kaf-mirror/v2/internal/database"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kgo"
)

func startTargetDownMirror(t *testing.T, lag func(context.Context, []string) (int64, error)) (chan database.ReplicationMetric, func() []string) {
	t.Helper()
	sent := make(chan inFlight, 64)
	km, _ := startMirrorForTest(t, fetchOf("big", partitionRecords{0, []int64{0, 1, 2}}), func(ctx context.Context, r *kgo.Record, f func(*kgo.Record, error)) {
		sent <- inFlight{record: r, ack: f}
	})
	km.SetGroupLagForTest(lag)
	km.SetIntervalsForTest(time.Millisecond, 0)
	var mu sync.Mutex
	var started []string
	km.SetIncidentHookForTest(func(name string) {
		mu.Lock()
		defer mu.Unlock()
		started = append(started, name)
	})
	metrics := make(chan database.ReplicationMetric, 1024)
	km.Start("job-65", func(m database.ReplicationMetric) {
		select {
		case metrics <- m:
		default:
		}
	}, func(string, string) {})
	t.Cleanup(km.Stop)
	awaitInFlight(t, sent, 3)
	return metrics, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), started...)
	}
}

func TestTargetDownWithGroupLagReportsSourceStalled(t *testing.T) {
	c, adm, cl := startPlainCluster(t)
	createTopic(t, adm, "big", 1, nil)
	produceTo(t, cl, "big", 0, 3)
	admin := plainAdmin(t, c)
	var mu sync.Mutex
	var asked []string
	metrics, started := startTargetDownMirror(t, func(ctx context.Context, topics []string) (int64, error) {
		mu.Lock()
		asked = append([]string(nil), topics...)
		mu.Unlock()
		return admin.GetGroupLag(ctx, "kaf-mirror-job-65", topics...)
	})

	var m database.ReplicationMetric
	for i := 0; i < 100 && !m.SourceStalled; i++ {
		m = awaitMetric(t, metrics, func(database.ReplicationMetric) bool { return true })
	}
	require.True(t, m.SourceStalled, "no source_stalled after 100 reports, last current_lag %d", m.CurrentLag)
	assert.Equal(t, 3, m.CurrentLag)
	assert.Equal(t, 0, m.MessagesReplicated)
	assert.Equal(t, 0, m.MessagesConsumed)
	assert.False(t, m.TargetStalled)
	assert.Contains(t, started(), "source_stalled")
	mu.Lock()
	assert.Equal(t, []string{"big"}, asked)
	mu.Unlock()
}

func TestStallFallsBackToFetchLagWhenGroupLagFails(t *testing.T) {
	metrics, started := startTargetDownMirror(t, func(context.Context, []string) (int64, error) {
		return 0, errors.New("source down")
	})

	for i := 0; i < 20; i++ {
		m := awaitMetric(t, metrics, func(database.ReplicationMetric) bool { return true })
		require.Equal(t, 0, m.CurrentLag)
		require.False(t, m.SourceStalled)
	}
	assert.NotContains(t, started(), "source_stalled")
}
