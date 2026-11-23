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
	"kaf-mirror/internal/config"
	"kaf-mirror/internal/kafka"
	"kaf-mirror/tests/mocks"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kgo"
)

func copyRecord(t *testing.T, in *kgo.Record) *kgo.Record {
	t.Helper()
	var produced *kgo.Record
	mockClient := &mocks.MockKgoClient{
		ProduceFunc: func(ctx context.Context, r *kgo.Record, f func(*kgo.Record, error)) {
			produced = r
			f(r, nil)
		},
	}
	km := kafka.NewKafMirrorImplForTest(
		&kafka.Producer{Client: mockClient},
		map[string]string{"orders": "orders_copy"},
	)
	require.NoError(t, km.HandleRecordForTest(in))
	require.NotNil(t, produced)
	return produced
}

func TestHandleRecord_CopiesAllRecordFields(t *testing.T) {
	ts := time.UnixMilli(1700000000123)
	headers := []kgo.RecordHeader{
		{Key: "trace-id", Value: []byte("abc")},
		{Key: "empty", Value: nil},
		{Key: "trace-id", Value: []byte("dup")},
	}
	out := copyRecord(t, &kgo.Record{
		Topic:     "orders",
		Partition: 5,
		Offset:    42,
		Key:       []byte("k1"),
		Value:     []byte("payload"),
		Headers:   headers,
		Timestamp: ts,
	})

	assert.Equal(t, "orders_copy", out.Topic)
	assert.Equal(t, int32(5), out.Partition)
	assert.Equal(t, []byte("k1"), out.Key)
	assert.Equal(t, []byte("payload"), out.Value)
	assert.Equal(t, headers, out.Headers)
	assert.Equal(t, ts.UnixMilli(), out.Timestamp.UnixMilli())
}

func TestHandleRecord_KeepsPartitionWithoutKnownTargetCount(t *testing.T) {
	out := copyRecord(t, &kgo.Record{Topic: "orders", Partition: 2, Value: []byte("v")})
	assert.Equal(t, int32(2), out.Partition)
}

func TestHandleRecord_KeepsNilKeyAndTombstone(t *testing.T) {
	out := copyRecord(t, &kgo.Record{Topic: "orders", Partition: 1, Timestamp: time.UnixMilli(1000)})
	assert.Nil(t, out.Key)
	assert.Nil(t, out.Value)
	assert.Nil(t, out.Headers)
	assert.Equal(t, int64(1000), out.Timestamp.UnixMilli())
}

func TestHandleRecord_KeepsEmptyKeyDistinctFromNil(t *testing.T) {
	out := copyRecord(t, &kgo.Record{Topic: "orders", Key: []byte{}, Value: []byte("v")})
	assert.NotNil(t, out.Key)
	assert.Len(t, out.Key, 0)
}

func TestHandleRecord_ZeroTimestampLeftForProducer(t *testing.T) {
	out := copyRecord(t, &kgo.Record{Topic: "orders", Value: []byte("v")})
	assert.True(t, out.Timestamp.IsZero())
}

func TestHandleRecord_MissingSourceTimestampLeftForProducer(t *testing.T) {
	out := copyRecord(t, &kgo.Record{Topic: "orders", Value: []byte("v"), Timestamp: time.UnixMilli(-1)})
	assert.True(t, out.Timestamp.IsZero())
}

func TestHandleRecord_EpochTimestampKept(t *testing.T) {
	out := copyRecord(t, &kgo.Record{Topic: "orders", Value: []byte("v"), Timestamp: time.UnixMilli(0)})
	assert.False(t, out.Timestamp.IsZero())
	assert.Equal(t, int64(0), out.Timestamp.UnixMilli())
}

func TestSourcePartitioner_KeepsPartitionInRange(t *testing.T) {
	tp := kafka.NewSourcePartitioner().ForTopic("orders_copy")
	assert.True(t, tp.RequiresConsistency(&kgo.Record{}))
	for p := int32(0); p < 4; p++ {
		assert.Equal(t, int(p), tp.Partition(&kgo.Record{Partition: p, Key: []byte("same")}, 4))
		assert.Equal(t, int(p), tp.Partition(&kgo.Record{Partition: p}, 4))
	}
}

func TestSourcePartitioner_FallsBackToKeyHashWhenTargetHasFewerPartitions(t *testing.T) {
	tp := kafka.NewSourcePartitioner().ForTopic("orders_copy")
	reference := kgo.StickyKeyPartitioner(nil).ForTopic("orders_copy")
	keys := []string{"a", "b", "customer-17", "customer-42", "zzz"}
	for _, k := range keys {
		r := &kgo.Record{Partition: 7, Key: []byte(k)}
		want := reference.Partition(&kgo.Record{Key: []byte(k)}, 3)
		got := tp.Partition(r, 3)
		assert.Equal(t, want, got, "key %s", k)
		assert.Equal(t, got, tp.Partition(&kgo.Record{Partition: 3, Key: []byte(k)}, 3), "key %s", k)
	}
}

func TestSourcePartitioner_FallbackWithoutKeyStaysInRange(t *testing.T) {
	tp := kafka.NewSourcePartitioner().ForTopic("orders_copy")
	nb, ok := tp.(kgo.TopicPartitionerOnNewBatch)
	require.True(t, ok)
	for i := 0; i < 50; i++ {
		got := tp.Partition(&kgo.Record{Partition: 9}, 2)
		assert.GreaterOrEqual(t, got, 0)
		assert.Less(t, got, 2)
		nb.OnNewBatch()
	}
}

func TestSourcePartitioner_NegativePartitionFallsBack(t *testing.T) {
	tp := kafka.NewSourcePartitioner().ForTopic("orders_copy")
	got := tp.Partition(&kgo.Record{Partition: -1, Key: []byte("k")}, 3)
	assert.GreaterOrEqual(t, got, 0)
	assert.Less(t, got, 3)
}

func TestSourcePartitioner_SinglePartitionTarget(t *testing.T) {
	tp := kafka.NewSourcePartitioner().ForTopic("orders_copy")
	assert.Equal(t, 0, tp.Partition(&kgo.Record{Partition: 0}, 1))
	assert.Equal(t, 0, tp.Partition(&kgo.Record{Partition: 4, Key: []byte("k")}, 1))
	assert.Equal(t, 0, tp.Partition(&kgo.Record{Partition: 4}, 1))
}

func TestNewProducer_UsesSourcePartitioner(t *testing.T) {
	producer, err := kafka.NewProducer(config.ClusterConfig{Brokers: "localhost:9092"}, config.ReplicationConfig{BatchSize: 1000}, "test-job")
	require.NoError(t, err)
	t.Cleanup(producer.Close)

	client, ok := producer.Client.(*kgo.Client)
	require.True(t, ok)
	partitioner, ok := client.OptValue(kgo.RecordPartitioner).(kgo.Partitioner)
	require.True(t, ok)
	tp := partitioner.ForTopic("orders_copy")
	assert.Equal(t, 2, tp.Partition(&kgo.Record{Partition: 2, Key: []byte("k")}, 3))
	assert.True(t, tp.RequiresConsistency(&kgo.Record{}))
}
