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

package manager_test

import (
	"context"
	"kaf-mirror/internal/config"
	"kaf-mirror/internal/database"
	"kaf-mirror/internal/kafka"
	"kaf-mirror/internal/manager"
	"kaf-mirror/tests/mocks"
	"sort"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStartJobPassesTopicDiscoveryIntervalToJobConfig(t *testing.T) {
	db, err := database.InitDB(":memory:")
	require.NoError(t, err)
	jm := manager.New(db, &config.Config{Replication: config.ReplicationConfig{TopicDiscoveryInterval: "15s"}}, &mocks.MockHub{})
	t.Cleanup(func() {
		jm.Close()
		db.Close()
	})
	require.NoError(t, database.CreateCluster(db, &database.KafkaCluster{Name: "src", Brokers: "localhost:9092", SecurityConfig: "{}"}))
	require.NoError(t, database.CreateCluster(db, &database.KafkaCluster{Name: "tgt", Brokers: "localhost:9093", SecurityConfig: "{}"}))
	jobID := uuid.NewString()
	require.NoError(t, database.CreateJob(db, &database.ReplicationJob{ID: jobID, Name: "regex-job", SourceClusterName: "src", TargetClusterName: "tgt", Status: "paused", BatchSize: 100, Compression: "none"}))
	require.NoError(t, database.UpdateMappingsForJob(db, jobID, []database.TopicMapping{{JobID: jobID, SourceTopicPattern: "app-.*", TargetTopicPattern: "mirror-$0", Enabled: true}}))

	var built *config.Config
	jm.KafMirrorFactory = func(cfg *config.Config) (kafka.KafMirror, error) {
		built = cfg
		return &mocks.MockKafMirror{}, nil
	}

	require.NoError(t, jm.StartJob(jobID))

	require.NotNil(t, built)
	assert.Equal(t, "15s", built.Replication.TopicDiscoveryInterval)
	assert.Equal(t, jobID, built.Replication.JobID)
	assert.Equal(t, 100, built.Replication.BatchSize)
	assert.Equal(t, []config.TopicMapping{{Source: "app-.*", Target: "mirror-$0", Enabled: true}}, built.Topics)
}

type topicLister struct {
	info *kafka.ClusterInfo
}

func (l *topicLister) GetClusterInfo(ctx context.Context) (*kafka.ClusterInfo, error) {
	return l.info, nil
}

func TestMirrorStateResolvesRegexMappingsToConcreteTopics(t *testing.T) {
	lister := &topicLister{info: &kafka.ClusterInfo{Topics: map[string]kafka.TopicInfo{
		"app-a": {Name: "app-a"}, "app-b": {Name: "app-b"}, "xapp-c": {Name: "xapp-c"}, "pay.v1": {Name: "pay.v1"}, "pay.v1.dlq": {Name: "pay.v1.dlq"},
	}}}
	mappings := []database.TopicMapping{
		{SourceTopicPattern: "app-.*", TargetTopicPattern: "mirror-$0", Enabled: true},
		{SourceTopicPattern: "pay.v1", TargetTopicPattern: "", Enabled: true},
		{SourceTopicPattern: "pay.v1.dlq", TargetTopicPattern: "", Enabled: false},
	}

	topics, topicMap, err := manager.ResolveJobTopicsForTest(context.Background(), lister, mappings)

	require.NoError(t, err)
	sort.Strings(topics)
	assert.Equal(t, []string{"app-a", "app-b", "pay.v1"}, topics)
	assert.Equal(t, map[string]string{"app-a": "mirror-app-a", "app-b": "mirror-app-b", "pay.v1": "pay.v1"}, topicMap)
	assert.NotContains(t, topicMap, "app-.*")
}
