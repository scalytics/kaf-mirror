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
	"kaf-mirror/internal/config"
	"kaf-mirror/internal/database"
	"kaf-mirror/internal/kafka"
	"kaf-mirror/tests/mocks"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestJobRestartDoesNotGrowStoredMetrics(t *testing.T) {
	db, jm, _ := setupManagerTest(t)
	require.NoError(t, database.CreateCluster(db, &database.KafkaCluster{Name: "src", Brokers: "localhost:9092", SecurityConfig: "{}"}))
	require.NoError(t, database.CreateCluster(db, &database.KafkaCluster{Name: "tgt", Brokers: "localhost:9093", SecurityConfig: "{}"}))
	jobID := uuid.NewString()
	require.NoError(t, database.CreateJob(db, &database.ReplicationJob{ID: jobID, Name: "metrics-job", SourceClusterName: "src", TargetClusterName: "tgt", Status: "paused"}))
	require.NoError(t, database.UpdateMappingsForJob(db, jobID, []database.TopicMapping{{JobID: jobID, SourceTopicPattern: "big", TargetTopicPattern: "big", Enabled: true}}))

	var callbacks []func(database.ReplicationMetric)
	jm.KafMirrorFactory = func(cfg *config.Config) (kafka.KafMirror, error) {
		return &mocks.MockKafMirror{
			StartFunc: func(_ string, metrics func(database.ReplicationMetric), _ func(string, string)) {
				callbacks = append(callbacks, metrics)
			},
		}, nil
	}
	sample := func(n int) database.ReplicationMetric {
		return database.ReplicationMetric{JobID: jobID, MessagesReplicated: n, BytesTransferred: n * 10, MessagesConsumed: n, BytesConsumed: n * 10, ErrorCount: n / 100, TombstoneCount: int64(n / 10)}
	}
	latest := func() *database.ReplicationMetric {
		m, err := database.GetLatestMetrics(db, jobID)
		require.NoError(t, err)
		return m
	}

	require.NoError(t, jm.StartJob(jobID))
	callbacks[0](sample(0))
	callbacks[0](sample(500))
	callbacks[0](sample(1000))
	require.NoError(t, jm.StopJob(jobID))
	assert.Equal(t, 1000, latest().MessagesReplicated)

	require.NoError(t, jm.StartJob(jobID))
	for i := 0; i < 3; i++ {
		callbacks[1](sample(0))
	}
	assert.Equal(t, 1000, latest().MessagesReplicated)

	for i := 0; i < 3; i++ {
		callbacks[1](sample(200))
	}
	m := latest()
	assert.Equal(t, 1200, m.MessagesReplicated)
	assert.Equal(t, 12000, m.BytesTransferred)
	assert.Equal(t, 1200, m.MessagesConsumed)
	assert.Equal(t, 12000, m.BytesConsumed)
	assert.Equal(t, 12, m.ErrorCount)
	assert.Equal(t, int64(120), m.TombstoneCount)
	require.NoError(t, jm.StopJob(jobID))
}
