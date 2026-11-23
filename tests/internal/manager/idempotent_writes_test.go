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
	"testing"

	"kaf-mirror/internal/config"
	"kaf-mirror/internal/database"
	"kaf-mirror/internal/kafka"
	"kaf-mirror/internal/manager"
	"kaf-mirror/tests/mocks"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func startJobAndCaptureConfig(t *testing.T, cfg *config.Config) *config.Config {
	t.Helper()
	db, err := database.InitDB(":memory:")
	require.NoError(t, err)
	jm := manager.New(db, cfg, &mocks.MockHub{})
	t.Cleanup(func() {
		jm.Close()
		db.Close()
	})
	require.NoError(t, database.CreateCluster(db, &database.KafkaCluster{Name: "src", Brokers: "localhost:9092", SecurityConfig: "{}"}))
	require.NoError(t, database.CreateCluster(db, &database.KafkaCluster{Name: "kafscale", Brokers: "localhost:39092", SecurityConfig: "{}"}))
	jobID := uuid.NewString()
	require.NoError(t, database.CreateJob(db, &database.ReplicationJob{ID: jobID, Name: "to-kafscale", SourceClusterName: "src", TargetClusterName: "kafscale", Status: "paused", BatchSize: 100, Compression: "none"}))
	require.NoError(t, database.UpdateMappingsForJob(db, jobID, []database.TopicMapping{{JobID: jobID, SourceTopicPattern: "orders", Enabled: true}}))

	var built *config.Config
	jm.KafMirrorFactory = func(c *config.Config) (kafka.KafMirror, error) {
		built = c
		return &mocks.MockKafMirror{}, nil
	}
	require.NoError(t, jm.StartJob(jobID))
	require.NotNil(t, built)
	return built
}

func TestDisableIdempotentWritesFromConfigFileReachesTheJob(t *testing.T) {
	built := startJobAndCaptureConfig(t, &config.Config{
		Server:   config.ServerConfig{Mode: "test"},
		Clusters: map[string]config.ClusterConfig{"kafscale": {DisableIdempotentWrites: true}},
	})
	assert.True(t, built.Clusters["target"].DisableIdempotentWrites)
	assert.False(t, built.Clusters["source"].DisableIdempotentWrites)
}

func TestIdempotentWritesStayOnWithoutConfigFileFlag(t *testing.T) {
	built := startJobAndCaptureConfig(t, &config.Config{
		Server:   config.ServerConfig{Mode: "test"},
		Clusters: map[string]config.ClusterConfig{"src": {DisableIdempotentWrites: true}},
	})
	assert.False(t, built.Clusters["target"].DisableIdempotentWrites)
}
