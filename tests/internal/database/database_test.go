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

package database_test

import (
	"kaf-mirror/internal/database"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

func TestDatabase(t *testing.T) {
	db, err := database.InitDB(":memory:")
	assert.NoError(t, err)
	defer db.Close()

	t.Run("Jobs", func(t *testing.T) {
		jobID := uuid.NewString()
		job := &database.ReplicationJob{
			ID:                jobID,
			Name:              "test-job",
			SourceClusterName: "src",
			TargetClusterName: "tgt",
			Status:            "paused",
		}
		err := database.CreateJob(db, job)
		assert.NoError(t, err)

		fetchedJob, err := database.GetJob(db, jobID)
		assert.NoError(t, err)
		assert.Equal(t, "test-job", fetchedJob.Name)

		jobs, err := database.ListJobs(db)
		assert.NoError(t, err)
		assert.Len(t, jobs, 1)

		err = database.DeleteJob(db, jobID)
		assert.NoError(t, err)

		_, err = database.GetJob(db, jobID)
		assert.Error(t, err)
	})

	t.Run("Mappings", func(t *testing.T) {
		jobID := uuid.NewString()
		job := &database.ReplicationJob{ID: jobID, Name: "mapping-job", SourceClusterName: "a", TargetClusterName: "b", Status: "paused"}
		database.CreateJob(db, job)

		mappings := []database.TopicMapping{
			{JobID: jobID, SourceTopicPattern: "a", TargetTopicPattern: "b", Enabled: true},
		}
		err := database.UpdateMappingsForJob(db, jobID, mappings)
		assert.NoError(t, err)

		fetchedMappings, err := database.GetMappingsForJob(db, jobID)
		assert.NoError(t, err)
		assert.Len(t, fetchedMappings, 1)
		assert.Equal(t, "a", fetchedMappings[0].SourceTopicPattern)
	})

	t.Run("Metrics", func(t *testing.T) {
		jobID := uuid.NewString()
		job := &database.ReplicationJob{ID: jobID, Name: "metrics-job", SourceClusterName: "a", TargetClusterName: "b", Status: "paused"}
		database.CreateJob(db, job)

		metric1 := &database.ReplicationMetric{
			JobID:              jobID,
			MessagesReplicated: 100,
			BytesTransferred:   1000,
			MessagesConsumed:   120,
			BytesConsumed:      1400,
			CurrentLag:         10,
			ErrorCount:         0,
			Timestamp:          time.Now().Add(-10 * time.Second),
		}
		err := database.InsertMetrics(db, metric1)
		assert.NoError(t, err)

		metric2 := &database.ReplicationMetric{
			JobID:              jobID,
			MessagesReplicated: 123,
			BytesTransferred:   4560,
			MessagesConsumed:   150,
			BytesConsumed:      2000,
			CurrentLag:         12,
			ErrorCount:         1,
			Timestamp:          time.Now(),
		}
		err = database.InsertMetrics(db, metric2)
		assert.NoError(t, err)

		metrics, err := database.GetHistoricalMetrics(db, jobID, time.Now().Add(-1*time.Hour), time.Now())
		assert.NoError(t, err)
		assert.Len(t, metrics, 2)
		assert.Equal(t, 23, metrics[1].MessagesReplicatedDelta)
		assert.Equal(t, 3560, metrics[1].BytesTransferredDelta)
		assert.Equal(t, 30, metrics[1].MessagesConsumedDelta)
		assert.Equal(t, 600, metrics[1].BytesConsumedDelta)
		assert.Equal(t, 1, metrics[1].ErrorCountDelta)
	})

	t.Run("TestConfluentClusterUniqueness", func(t *testing.T) {
		// Create a confluent cluster
		cluster1 := &database.KafkaCluster{
			Name:      "confluent-1",
			Provider:  "confluent",
			ClusterID: "lkc-12345",
			Brokers:   "localhost:9092",
		}
		err := database.CreateCluster(db, cluster1)
		assert.NoError(t, err)

		// Attempt to create another confluent cluster with the same cluster_id (should fail)
		cluster2 := &database.KafkaCluster{
			Name:      "confluent-2",
			Provider:  "confluent",
			ClusterID: "lkc-12345",
			Brokers:   "localhost:9092",
		}
		err = database.CreateCluster(db, cluster2)
		assert.Error(t, err)

		// Attempt to create another confluent cluster with the same brokers but different cluster_id (should succeed)
		cluster3 := &database.KafkaCluster{
			Name:      "confluent-3",
			Provider:  "confluent",
			ClusterID: "lkc-67890",
			Brokers:   "localhost:9092",
		}
		err = database.CreateCluster(db, cluster3)
		assert.NoError(t, err)

		// Attempt to create a plain cluster with the same brokers (should succeed)
		cluster4 := &database.KafkaCluster{
			Name:     "plain-1",
			Provider: "plain",
			Brokers:  "localhost:9092",
		}
		err = database.CreateCluster(db, cluster4)
		assert.NoError(t, err)
	})
}

func TestKafkaClusterRedactedAndRestore(t *testing.T) {
	conn := "Endpoint=sb://ns/;SharedAccessKey=secret"
	cluster := database.KafkaCluster{
		Name:             "src",
		Provider:         "confluent",
		Brokers:          "localhost:9092",
		APIKey:           "ckey",
		APISecret:        "csecret",
		ConnectionString: &conn,
		SecurityConfig:   `{"password":"pw"}`,
	}

	redacted := cluster.Redacted()
	assert.Equal(t, "***", redacted.APIKey)
	assert.Equal(t, "***", redacted.APISecret)
	assert.NotNil(t, redacted.ConnectionString)
	assert.Equal(t, "***", *redacted.ConnectionString)
	assert.Equal(t, "***", redacted.SecurityConfig)
	assert.Equal(t, "csecret", cluster.APISecret)
	assert.Equal(t, conn, *cluster.ConnectionString)

	incoming := redacted
	incoming.Brokers = "localhost:9093"
	incoming.RestoreUnchangedSecrets(&cluster)
	assert.Equal(t, "ckey", incoming.APIKey)
	assert.Equal(t, "csecret", incoming.APISecret)
	assert.Equal(t, conn, *incoming.ConnectionString)
	assert.Equal(t, `{"password":"pw"}`, incoming.SecurityConfig)
	assert.Equal(t, "localhost:9093", incoming.Brokers)
}
