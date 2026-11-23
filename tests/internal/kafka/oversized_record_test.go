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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kgo"
)

func TestOversizedRecordFailsAloneAfterItsBatchFailedTogether(t *testing.T) {
	sent := make(chan inFlight, 64)
	km, log := startMirrorForTest(t, fetchOf("big", partitionRecords{0, []int64{0, 1, 2, 3}}), func(ctx context.Context, r *kgo.Record, f func(*kgo.Record, error)) {
		sent <- inFlight{record: r, ack: f}
	})
	km.Consumer.SetProduceRetryForTest(5, 0)
	reasons := make(chan string, 1)
	km.Start("job-69", func(database.ReplicationMetric) {}, func(jobID string, reason string) {
		km.Stop()
		reasons <- reason
	})

	for _, f := range awaitInFlight(t, sent, 4) {
		f.ack(f.record, kerr.MessageTooLarge)
	}

	var alone []string
	for len(alone) < 7 {
		flights := awaitInFlight(t, sent, 1)
		select {
		case extra := <-sent:
			t.Fatalf("sent %s while %s was not acked yet", extra.key(), flights[0].key())
		default:
		}
		f := flights[0]
		alone = append(alone, f.key())
		if string(f.record.Value) == "v3" {
			f.ack(f.record, kerr.MessageTooLarge)
			continue
		}
		f.ack(f.record, nil)
	}

	select {
	case reason := <-reasons:
		assert.Contains(t, reason, "partition 0 offset 3 after 5 attempts")
		assert.Contains(t, reason, "MESSAGE_TOO_LARGE")
	case <-time.After(5 * time.Second):
		t.Fatal("the job was not failed")
	}
	assert.Equal(t, []string{"0/0", "0/1", "0/2", "0/3", "0/3", "0/3", "0/3"}, alone)
	require.Equal(t, []string{"0/0", "0/1", "0/2"}, log.get())
}

func TestRecordsAfterAnIsolatedRangeAreSentTogetherAgain(t *testing.T) {
	sent, committed, stop := batchingMirror(t, fetchOf("big", partitionRecords{0, []int64{0, 1, 2, 3, 4}}), 5)
	defer stop()

	for _, f := range awaitInFlight(t, sent, 5) {
		if string(f.record.Value) == "v0" || string(f.record.Value) == "v1" {
			f.ack(f.record, kerr.MessageTooLarge)
			continue
		}
		f.ack(f.record, nil)
	}
	for _, want := range []string{"0/0", "0/1"} {
		f := awaitInFlight(t, sent, 1)[0]
		assert.Equal(t, want, f.key())
		f.ack(f.record, nil)
		waitForCommit(t, committed, want)
	}
	var rest []string
	for _, f := range awaitInFlight(t, sent, 3) {
		rest = append(rest, f.key())
		f.ack(f.record, nil)
	}
	assert.Equal(t, []string{"0/2", "0/3", "0/4"}, rest)
	waitForCommit(t, committed, "0/4")
}
