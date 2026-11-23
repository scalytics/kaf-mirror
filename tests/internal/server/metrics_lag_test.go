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
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"kaf-mirror/internal/database"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kfake"
	"github.com/twmb/franz-go/pkg/kgo"
)

func TestGetLagReturnsCommittedLagOfTheJobGroup(t *testing.T) {
	ctx := setupTestServer(t)
	c, err := kfake.NewCluster(kfake.NumBrokers(1), kfake.SeedTopics(2, "orders"))
	require.NoError(t, err)
	defer c.Close()
	cl, err := kgo.NewClient(kgo.SeedBrokers(c.ListenAddrs()...), kgo.RecordPartitioner(kgo.ManualPartitioner()))
	require.NoError(t, err)
	defer cl.Close()
	kctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for i := 0; i < 6; i++ {
		require.NoError(t, cl.ProduceSync(kctx, &kgo.Record{Topic: "orders", Partition: int32(i % 2), Value: []byte("v")}).FirstErr())
	}

	db := ctx.Server.Db
	require.NoError(t, database.CreateCluster(db, &database.KafkaCluster{Name: "lag-src", Brokers: strings.Join(c.ListenAddrs(), ","), SecurityConfig: "{}"}))
	require.NoError(t, database.CreateCluster(db, &database.KafkaCluster{Name: "lag-tgt", Brokers: "localhost:1", SecurityConfig: "{}"}))
	jobID := uuid.NewString()
	require.NoError(t, database.CreateJob(db, &database.ReplicationJob{ID: jobID, Name: "lag-job", SourceClusterName: "lag-src", TargetClusterName: "lag-tgt", Status: "paused"}))
	require.NoError(t, database.UpdateMappingsForJob(db, jobID, []database.TopicMapping{{JobID: jobID, SourceTopicPattern: "orders", TargetTopicPattern: "orders", Enabled: true}}))

	offsets := kadm.Offsets{}
	offsets.AddOffset("orders", 0, 1, -1)
	committed, err := kadm.NewClient(cl).CommitOffsets(kctx, "kaf-mirror-job-"+jobID, offsets)
	require.NoError(t, err)
	require.NoError(t, committed.Error())

	req := httptest.NewRequest("GET", "/api/v1/jobs/"+jobID+"/lag", nil)
	addAuthHeader(req, ctx.Token)
	resp, err := ctx.Server.App.Test(req, 15000)
	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode)
	var lag map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&lag))
	assert.Equal(t, map[string]any{"orders": float64(2 + 3)}, lag)
}

func TestCurrentMetricsReturnTombstoneCount(t *testing.T) {
	ctx := setupTestServer(t)
	db := ctx.Server.Db
	require.NoError(t, database.CreateCluster(db, &database.KafkaCluster{Name: "a", Brokers: "localhost:9092", SecurityConfig: "{}"}))
	require.NoError(t, database.CreateCluster(db, &database.KafkaCluster{Name: "b", Brokers: "localhost:9093", SecurityConfig: "{}"}))
	jobID := uuid.NewString()
	require.NoError(t, database.CreateJob(db, &database.ReplicationJob{ID: jobID, Name: "tombstones", SourceClusterName: "a", TargetClusterName: "b", Status: "paused"}))
	recorder := database.NewMetricsRecorder()
	require.NoError(t, recorder.Insert(db, &database.ReplicationMetric{JobID: jobID, MessagesReplicated: 3000, MessagesConsumed: 3000, TombstoneCount: 200}))
	require.NoError(t, recorder.Insert(db, &database.ReplicationMetric{JobID: jobID, MessagesReplicated: 3000, MessagesConsumed: 3000, TombstoneCount: 273}))

	req := httptest.NewRequest("GET", "/api/v1/jobs/"+jobID+"/metrics/current", nil)
	addAuthHeader(req, ctx.Token)
	resp, err := ctx.Server.App.Test(req)
	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode)
	var current database.ReplicationMetric
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&current))
	assert.Equal(t, int64(273), current.TombstoneCount)
	assert.Equal(t, 3000, current.MessagesReplicated)

	req = httptest.NewRequest("GET", "/api/v1/jobs/"+jobID+"/metrics/history", nil)
	addAuthHeader(req, ctx.Token)
	resp, err = ctx.Server.App.Test(req)
	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode)
	var history []database.AggregatedMetric
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&history))
	require.Len(t, history, 2)
	assert.Equal(t, int64(200), history[0].TombstoneCountDelta)
	assert.Equal(t, int64(73), history[1].TombstoneCountDelta)
}
