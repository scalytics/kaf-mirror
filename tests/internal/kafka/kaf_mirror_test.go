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

	"github.com/stretchr/testify/assert"
	"github.com/twmb/franz-go/pkg/kgo"
)

type fakeAdmin struct {
	info        *kafka.ClusterInfo
	ensureCalls []string
}

func (f *fakeAdmin) GetClusterInfo(ctx context.Context) (*kafka.ClusterInfo, error) {
	return f.info, nil
}

func (f *fakeAdmin) GetConsumerGroupOffsets(ctx context.Context, groupID string, topics []string) (map[string][]kafka.OffsetInfo, error) {
	return map[string][]kafka.OffsetInfo{}, nil
}

func (f *fakeAdmin) GetTopicHighWaterMarks(ctx context.Context, topics []string) (map[string][]kafka.OffsetInfo, error) {
	return map[string][]kafka.OffsetInfo{}, nil
}

func (f *fakeAdmin) EnsureTopicExists(ctx context.Context, topicName string, partitions int32, replicationFactor int16) error {
	f.ensureCalls = append(f.ensureCalls, topicName)
	if f.info.Topics == nil {
		f.info.Topics = map[string]kafka.TopicInfo{}
	}
	if _, exists := f.info.Topics[topicName]; !exists {
		f.info.Topics[topicName] = kafka.TopicInfo{
			Name:              topicName,
			Partitions:        partitions,
			ReplicationFactor: replicationFactor,
		}
	}
	return nil
}

func (f *fakeAdmin) ValidateTopicCompatibility(ctx context.Context, sourceInfo, targetInfo kafka.TopicInfo) error {
	return nil
}

func (f *fakeAdmin) Close() {}

func TestResolveTopicMappings_RegexExpands(t *testing.T) {
	restore := kafka.SetAdminClientFactoryForTest(func(cfg config.ClusterConfig) (kafka.AdminClientAPI, error) {
		return &fakeAdmin{
			info: &kafka.ClusterInfo{
				Topics: map[string]kafka.TopicInfo{
					"orders":   {Name: "orders"},
					"events-1": {Name: "events-1"},
					"events-2": {Name: "events-2"},
				},
			},
		}, nil
	})
	t.Cleanup(restore)

	cfg := &config.Config{
		Clusters: map[string]config.ClusterConfig{
			"source": {Brokers: "localhost:9092"},
		},
		Topics: []config.TopicMapping{
			{Source: "orders", Target: "orders_copy", Enabled: true},
			{Source: "events-1$", Target: "events_copy", Enabled: true},
		},
	}

	topics, topicMap, err := kafka.ResolveTopicMappingsForTest(cfg)
	assert.NoError(t, err)
	assert.Equal(t, "orders_copy", topicMap["orders"])
	assert.Equal(t, "events_copy", topicMap["events-1"])

	assert.Contains(t, topics, "orders")
	assert.Contains(t, topics, "events-1")
	assert.NotContains(t, topics, "events-2")
}

func TestResolveTopicMappings_DefaultTarget(t *testing.T) {
	restore := kafka.SetAdminClientFactoryForTest(func(cfg config.ClusterConfig) (kafka.AdminClientAPI, error) {
		return &fakeAdmin{
			info: &kafka.ClusterInfo{
				Topics: map[string]kafka.TopicInfo{
					"orders": {Name: "orders"},
				},
			},
		}, nil
	})
	t.Cleanup(restore)

	cfg := &config.Config{
		Clusters: map[string]config.ClusterConfig{
			"source": {Brokers: "localhost:9092"},
		},
		Topics: []config.TopicMapping{
			{Source: "orders", Target: "", Enabled: true},
		},
	}

	_, topicMap, err := kafka.ResolveTopicMappingsForTest(cfg)
	assert.NoError(t, err)
	assert.Equal(t, "orders", topicMap["orders"])
}

func TestResolveTopicMappings_RegexSubstitution(t *testing.T) {
	restore := kafka.SetAdminClientFactoryForTest(func(cfg config.ClusterConfig) (kafka.AdminClientAPI, error) {
		return &fakeAdmin{
			info: &kafka.ClusterInfo{
				Topics: map[string]kafka.TopicInfo{
					"orders-1": {Name: "orders-1"},
					"orders-2": {Name: "orders-2"},
				},
			},
		}, nil
	})
	t.Cleanup(restore)

	cfg := &config.Config{
		Clusters: map[string]config.ClusterConfig{
			"source": {Brokers: "localhost:9092"},
		},
		Topics: []config.TopicMapping{
			{Source: `orders-(.*)`, Target: `backup-$1`, Enabled: true},
		},
	}

	_, topicMap, err := kafka.ResolveTopicMappingsForTest(cfg)
	assert.NoError(t, err)
	assert.Equal(t, "backup-1", topicMap["orders-1"])
	assert.Equal(t, "backup-2", topicMap["orders-2"])
}

func TestResolveTopicMappings_RejectsTargetCollisions(t *testing.T) {
	restore := kafka.SetAdminClientFactoryForTest(func(cfg config.ClusterConfig) (kafka.AdminClientAPI, error) {
		return &fakeAdmin{
			info: &kafka.ClusterInfo{
				Topics: map[string]kafka.TopicInfo{
					"events-1": {Name: "events-1"},
					"events-2": {Name: "events-2"},
				},
			},
		}, nil
	})
	t.Cleanup(restore)

	cfg := &config.Config{
		Clusters: map[string]config.ClusterConfig{
			"source": {Brokers: "localhost:9092"},
		},
		Topics: []config.TopicMapping{
			{Source: "events-.*", Target: "events_copy", Enabled: true},
		},
	}

	_, _, err := kafka.ResolveTopicMappingsForTest(cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "target topic events_copy is mapped from multiple sources")
}

func TestHandleRecord_PreservesPartitionWhenCompatible(t *testing.T) {
	var producedRecord *kgo.Record
	mockClient := &mocks.MockKgoClient{
		ProduceFunc: func(ctx context.Context, r *kgo.Record, f func(*kgo.Record, error)) {
			producedRecord = r
			f(r, nil)
		},
	}

	km := kafka.NewKafMirrorImplForTest(
		&kafka.Producer{Client: mockClient},
		map[string]string{"orders": "orders_copy"},
		map[string]int32{"orders_copy": 3},
	)

	km.HandleRecordForTest(&kgo.Record{
		Topic:     "orders",
		Partition: 2,
		Value:     []byte("payload"),
	})

	assert.NotNil(t, producedRecord)
	assert.Equal(t, int32(2), producedRecord.Partition)
	assert.Equal(t, "orders_copy", producedRecord.Topic)
}

func TestHandleRecord_RegexSubstitutesTarget(t *testing.T) {
	var producedRecord *kgo.Record
	mockClient := &mocks.MockKgoClient{
		ProduceFunc: func(ctx context.Context, r *kgo.Record, f func(*kgo.Record, error)) {
			producedRecord = r
			f(r, nil)
		},
	}

	km := kafka.NewKafMirrorImplForTest(
		&kafka.Producer{Client: mockClient},
		map[string]string{},
		nil,
	)
	assert.NoError(t, km.AddRegexMapForTest(`events-(.*)`, `copy-$1`))

	assert.NoError(t, km.HandleRecordForTest(&kgo.Record{
		Topic: "events-orders",
		Value: []byte("payload"),
	}))

	assert.NotNil(t, producedRecord)
	assert.Equal(t, "copy-orders", producedRecord.Topic)
}

func TestHandleRecord_LeavesPartitionUnsetWhenUnknown(t *testing.T) {
	var producedRecord *kgo.Record
	mockClient := &mocks.MockKgoClient{
		ProduceFunc: func(ctx context.Context, r *kgo.Record, f func(*kgo.Record, error)) {
			producedRecord = r
			f(r, nil)
		},
	}

	km := kafka.NewKafMirrorImplForTest(
		&kafka.Producer{Client: mockClient},
		map[string]string{"orders": "orders_copy"},
		nil,
	)

	km.HandleRecordForTest(&kgo.Record{
		Topic:     "orders",
		Partition: 2,
		Value:     []byte("payload"),
	})

	assert.NotNil(t, producedRecord)
	assert.Equal(t, int32(0), producedRecord.Partition)
}

func TestValidateAndSyncClusters_EnsuresTargetTopics(t *testing.T) {
	admin := &fakeAdmin{
		info: &kafka.ClusterInfo{
			Topics: map[string]kafka.TopicInfo{
				"source-a": {Name: "source-a", Partitions: 3, ReplicationFactor: 1},
			},
		},
	}
	restore := kafka.SetAdminClientFactoryForTest(func(cfg config.ClusterConfig) (kafka.AdminClientAPI, error) {
		return admin, nil
	})
	t.Cleanup(restore)

	cfg := &config.Config{
		Clusters: map[string]config.ClusterConfig{
			"source": {Brokers: "localhost:9092"},
			"target": {Brokers: "localhost:9093"},
		},
		Replication: config.ReplicationConfig{JobID: "test-job"},
	}

	_, err := kafka.ValidateAndSyncClustersForTest(cfg, []string{"source-a"}, map[string]string{"source-a": "target-a"})
	assert.NoError(t, err)
	assert.Equal(t, []string{"target-a"}, admin.ensureCalls)
}
