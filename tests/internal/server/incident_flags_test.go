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

package server_test

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/scalytics/kaf-mirror/v2/internal/config"
	"github.com/scalytics/kaf-mirror/v2/internal/database"
	"github.com/scalytics/kaf-mirror/v2/internal/kafka"
	"github.com/scalytics/kaf-mirror/v2/tests/mocks"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCurrentMetricsShowTheIncidentFlagsOfTheRunningJob(t *testing.T) {
	ctx := setupTestServer(t)
	db := ctx.Server.Db
	require.NoError(t, database.CreateCluster(db, &database.KafkaCluster{Name: "src", Brokers: "localhost:9092", SecurityConfig: "{}"}))
	require.NoError(t, database.CreateCluster(db, &database.KafkaCluster{Name: "tgt", Brokers: "localhost:9093", SecurityConfig: "{}"}))
	jobID := uuid.NewString()
	require.NoError(t, database.CreateJob(db, &database.ReplicationJob{ID: jobID, Name: "stalled", SourceClusterName: "src", TargetClusterName: "tgt", Status: "paused"}))
	require.NoError(t, database.UpdateMappingsForJob(db, jobID, []database.TopicMapping{{JobID: jobID, SourceTopicPattern: "orders", TargetTopicPattern: "orders", Enabled: true}}))

	var report func(database.ReplicationMetric)
	ctx.Manager.KafMirrorFactory = func(*config.Config) (kafka.KafMirror, error) {
		return &mocks.MockKafMirror{
			StartFunc: func(_ string, metrics func(database.ReplicationMetric), _ func(string, string)) {
				report = metrics
			},
		}, nil
	}
	current := func() database.ReplicationMetric {
		req := httptest.NewRequest("GET", "/api/v1/jobs/"+jobID+"/metrics/current", nil)
		addAuthHeader(req, ctx.Token)
		resp, err := ctx.Server.App.Test(req)
		require.NoError(t, err)
		require.Equal(t, 200, resp.StatusCode)
		var m database.ReplicationMetric
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&m))
		return m
	}

	require.NoError(t, ctx.Manager.StartJob(jobID))
	require.NotNil(t, report)
	report(database.ReplicationMetric{JobID: jobID, CurrentLag: 1400, SourceStalled: true, CriticalLag: true, TopicRecreations: 2, MissingTopics: []string{"orders"}})

	m := current()
	assert.True(t, m.SourceStalled)
	assert.True(t, m.CriticalLag)
	assert.False(t, m.TargetStalled)
	assert.False(t, m.HighErrorRate)
	assert.False(t, m.ErrorSpike)
	assert.Equal(t, 1400, m.CurrentLag)
	assert.Equal(t, int64(2), m.TopicRecreations)
	assert.Equal(t, []string{"orders"}, m.MissingTopics)

	report(database.ReplicationMetric{JobID: jobID, CurrentLag: 0, TargetStalled: true, HighErrorRate: true, ErrorSpike: true})
	m = current()
	assert.False(t, m.SourceStalled)
	assert.False(t, m.CriticalLag)
	assert.True(t, m.TargetStalled)
	assert.True(t, m.HighErrorRate)
	assert.True(t, m.ErrorSpike)

	require.NoError(t, ctx.Manager.StopJob(jobID))
	m = current()
	assert.False(t, m.TargetStalled)
	assert.False(t, m.HighErrorRate)
	assert.False(t, m.ErrorSpike)
	assert.Equal(t, int64(0), m.TopicRecreations)
	assert.Empty(t, m.MissingTopics)
}
