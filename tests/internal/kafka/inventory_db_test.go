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
	"os"
	"testing"

	"kaf-mirror/internal/config"
	"kaf-mirror/internal/database"
	"kaf-mirror/internal/kafka"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func inventoryTestConfig(t *testing.T, jobID string) *config.Config {
	t.Helper()
	admin := &fakeAdmin{info: &kafka.ClusterInfo{Topics: map[string]kafka.TopicInfo{
		"source-a": {Name: "source-a", Partitions: 3, ReplicationFactor: 1},
	}}}
	t.Cleanup(kafka.SetAdminClientFactoryForTest(func(cfg config.ClusterConfig) (kafka.AdminClientAPI, error) {
		return admin, nil
	}))
	return &config.Config{
		Clusters: map[string]config.ClusterConfig{
			"source": {Brokers: "localhost:9092"},
			"target": {Brokers: "localhost:9093"},
		},
		Replication: config.ReplicationConfig{JobID: jobID},
	}
}

func TestJobStartDoesNotOpenASecondDatabase(t *testing.T) {
	require.NoError(t, os.RemoveAll("data"))
	cfg := inventoryTestConfig(t, "stray-db-job")

	require.NoError(t, kafka.ValidateAndSyncClustersForTest(cfg, []string{"source-a"}, map[string]string{"source-a": "target-a"}))

	_, err := os.Stat("data")
	assert.True(t, os.IsNotExist(err), "job start created a database under %s/data", mustGetwd(t))
	require.NoError(t, os.RemoveAll("data"))
}

func mustGetwd(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	require.NoError(t, err)
	return wd
}

func TestJobStartInventoryUsesTheJobDatabase(t *testing.T) {
	db, err := database.InitDB(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	require.NoError(t, database.CreateCluster(db, &database.KafkaCluster{Name: "src", Brokers: "localhost:9092", SecurityConfig: "{}"}))
	require.NoError(t, database.CreateCluster(db, &database.KafkaCluster{Name: "tgt", Brokers: "localhost:9093", SecurityConfig: "{}"}))
	require.NoError(t, database.CreateJob(db, &database.ReplicationJob{ID: "inv-job", Name: "inv", SourceClusterName: "src", TargetClusterName: "tgt", Status: "active", BatchSize: 100, Compression: "none"}))
	cfg := inventoryTestConfig(t, "inv-job")

	require.NoError(t, kafka.ValidateAndSyncClustersWithDBForTest(cfg, db, []string{"source-a"}, map[string]string{"source-a": "target-a"}))

	var snapshots int
	require.NoError(t, db.Get(&snapshots, "SELECT COUNT(*) FROM job_inventory_snapshots WHERE job_id = ? AND snapshot_type = 'startup'", "inv-job"))
	assert.Equal(t, 1, snapshots)
	var groups int
	require.NoError(t, db.Get(&groups, "SELECT COUNT(*) FROM consumer_group_inventory WHERE group_id = ?", "kaf-mirror-job-inv-job"))
	assert.Equal(t, 1, groups)
	_, err = os.Stat("data")
	assert.True(t, os.IsNotExist(err))
}
