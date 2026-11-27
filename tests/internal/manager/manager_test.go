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
	"errors"
	"github.com/scalytics/kaf-mirror/v2/internal/config"
	"github.com/scalytics/kaf-mirror/v2/internal/database"
	"github.com/scalytics/kaf-mirror/v2/internal/kafka"
	"github.com/scalytics/kaf-mirror/v2/internal/manager"
	"github.com/scalytics/kaf-mirror/v2/tests/mocks"
	"testing"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/assert"
)

type mockHub struct {
	broadcasts int
}

func (m *mockHub) BroadcastJSON(v interface{}) {
	m.broadcasts++
}

func setupManagerTest(t *testing.T) (*sqlx.DB, *manager.JobManager, *mocks.MockHub) {
	db, err := database.InitDB(":memory:")
	assert.NoError(t, err)

	hub := &mocks.MockHub{}
	jm := manager.New(db, &config.Config{}, hub)

	// Ensure proper cleanup
	t.Cleanup(func() {
		jm.Close()
		db.Close()
	})

	return db, jm, hub
}

func TestJobManager_FullLifecycle(t *testing.T) {
	db, jm, _ := setupManagerTest(t)

	// 1. Create mock clusters
	sourceCluster := &database.KafkaCluster{Name: "source-cluster", Brokers: "localhost:9092", SecurityConfig: "{}"}
	targetCluster := &database.KafkaCluster{Name: "target-cluster", Brokers: "localhost:9093", SecurityConfig: "{}"}
	err := database.CreateCluster(db, sourceCluster)
	assert.NoError(t, err)
	err = database.CreateCluster(db, targetCluster)
	assert.NoError(t, err)

	// 2. Create a job
	jobID := uuid.NewString()
	job := &database.ReplicationJob{
		ID:                jobID,
		Name:              "manager-test-full",
		SourceClusterName: "source-cluster",
		TargetClusterName: "target-cluster",
		Status:            "paused",
	}
	err = database.CreateJob(db, job)
	assert.NoError(t, err)

	// 3. Create mappings
	mappings := []database.TopicMapping{
		{JobID: jobID, SourceTopicPattern: "topic-a", TargetTopicPattern: "topic-a-replica", Enabled: true},
	}
	err = database.UpdateMappingsForJob(db, jobID, mappings)
	assert.NoError(t, err)

	// 4. Mock the kaf-mirror factory to inspect the generated config
	var capturedConfig *config.Config
	jm.KafMirrorFactory = func(cfg *config.Config) (kafka.KafMirror, error) {
		capturedConfig = cfg
		return &mocks.MockKafMirror{
			StopFunc: func() {},
		}, nil
	}

	// 5. Start the job
	err = jm.StartJob(jobID)
	assert.NoError(t, err)

	// 6. Assertions
	// Check job status
	updatedJob, err := database.GetJob(db, jobID)
	assert.NoError(t, err)
	assert.Equal(t, "active", updatedJob.Status)

	// Check captured config
	assert.NotNil(t, capturedConfig)
	assert.Equal(t, "localhost:9092", capturedConfig.Clusters["source"].Brokers)
	assert.Equal(t, "localhost:9093", capturedConfig.Clusters["target"].Brokers)
	assert.Len(t, capturedConfig.Topics, 1)
	assert.Equal(t, "topic-a", capturedConfig.Topics[0].Source)

	// 7. Stop the job
	err = jm.StopJob(jobID)
	assert.NoError(t, err)

	// Check job status again
	updatedJob, err = database.GetJob(db, jobID)
	assert.NoError(t, err)
	assert.Equal(t, "paused", updatedJob.Status)
}

func TestJobManager_StartJob_ReplicatorError(t *testing.T) {
	db, jm, _ := setupManagerTest(t)

	// Setup clusters and job
	sourceCluster := &database.KafkaCluster{Name: "source-cluster", Brokers: "localhost:9092", SecurityConfig: "{}"}
	targetCluster := &database.KafkaCluster{Name: "target-cluster", Brokers: "localhost:9093", SecurityConfig: "{}"}
	err := database.CreateCluster(db, sourceCluster)
	assert.NoError(t, err)
	err = database.CreateCluster(db, targetCluster)
	assert.NoError(t, err)
	jobID := uuid.NewString()
	job := &database.ReplicationJob{ID: jobID, Name: "error-job", SourceClusterName: "source-cluster", TargetClusterName: "target-cluster", Status: "paused"}
	err = database.CreateJob(db, job)
	assert.NoError(t, err)

	// Mock the kaf-mirror factory to return an error
	jm.KafMirrorFactory = func(cfg *config.Config) (kafka.KafMirror, error) {
		return nil, errors.New("kafka is down")
	}

	err = jm.StartJob(jobID)
	assert.Error(t, err)
	assert.Equal(t, "kafka is down", err.Error())
}
