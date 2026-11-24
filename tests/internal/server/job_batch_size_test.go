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
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"kaf-mirror/internal/database"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func sendJobRequest(t *testing.T, ctx *TestContext, method, path, body string) (int, string) {
	t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	addAuthHeader(req, ctx.Token)
	resp, err := ctx.Server.App.Test(req)
	require.NoError(t, err)
	data, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(data)
}

func setupJobServer(t *testing.T) *TestContext {
	t.Helper()
	ctx := setupTestServer(t)
	require.NoError(t, database.CreateCluster(ctx.Server.Db, &database.KafkaCluster{Name: "src", Brokers: "localhost:9092", SecurityConfig: "{}"}))
	require.NoError(t, database.CreateCluster(ctx.Server.Db, &database.KafkaCluster{Name: "tgt", Brokers: "localhost:9093", SecurityConfig: "{}"}))
	return ctx
}

func TestCreateJobWithoutBatchSizeUsesTheDefault(t *testing.T) {
	ctx := setupJobServer(t)
	status, body := sendJobRequest(t, ctx, http.MethodPost, "/api/v1/jobs", `{"name":"apps","source_cluster_name":"src","target_cluster_name":"tgt","topic_mappings":[{"source_topic_pattern":"app-.*","target_topic_pattern":"","enabled":true}]}`)
	require.Equal(t, http.StatusCreated, status, body)
	var job database.ReplicationJob
	require.NoError(t, json.Unmarshal([]byte(body), &job))
	assert.Equal(t, database.DefaultBatchSizeKB, job.BatchSize)
	stored, err := database.GetJob(ctx.Server.Db, job.ID)
	require.NoError(t, err)
	assert.Equal(t, database.DefaultBatchSizeKB, stored.BatchSize)
}

func TestCreateJobRejectsInvalidBatchSize(t *testing.T) {
	ctx := setupJobServer(t)
	for _, size := range []string{"-1", "102401", "2147483647"} {
		status, body := sendJobRequest(t, ctx, http.MethodPost, "/api/v1/jobs", `{"name":"bad-`+size+`","source_cluster_name":"src","target_cluster_name":"tgt","topic_mappings":[{"source_topic_pattern":"orders","target_topic_pattern":"orders","enabled":true}],"batch_size":`+size+`}`)
		assert.Equal(t, http.StatusBadRequest, status, size)
		assert.Contains(t, body, "batch_size must be between 1 and 102400 KB", size)
	}
	jobs, err := database.ListJobs(ctx.Server.Db)
	require.NoError(t, err)
	assert.Empty(t, jobs)
}

func TestUpdateJobValidatesBatchSizeAndRepairsAStoredZero(t *testing.T) {
	ctx := setupJobServer(t)
	id := uuid.NewString()
	require.NoError(t, database.CreateJob(ctx.Server.Db, &database.ReplicationJob{ID: id, Name: "old", SourceClusterName: "src", TargetClusterName: "tgt", Status: "paused", BatchSize: 0}))

	status, body := sendJobRequest(t, ctx, http.MethodPut, "/api/v1/jobs/"+id, `{"batch_size":-5}`)
	assert.Equal(t, http.StatusBadRequest, status, body)

	status, body = sendJobRequest(t, ctx, http.MethodPut, "/api/v1/jobs/"+id, `{"compression":"lz4"}`)
	require.Equal(t, http.StatusOK, status, body)
	stored, err := database.GetJob(ctx.Server.Db, id)
	require.NoError(t, err)
	assert.Equal(t, database.DefaultBatchSizeKB, stored.BatchSize)
	assert.Equal(t, "old", stored.Name)
	assert.Equal(t, "lz4", stored.Compression)

	status, body = sendJobRequest(t, ctx, http.MethodPut, "/api/v1/jobs/"+id, `{"batch_size":512}`)
	require.Equal(t, http.StatusOK, status, body)
	stored, err = database.GetJob(ctx.Server.Db, id)
	require.NoError(t, err)
	assert.Equal(t, 512, stored.BatchSize)
}

func TestUpdateJobWithAnInvalidMappingChangesNothing(t *testing.T) {
	ctx := setupJobServer(t)
	id := uuid.NewString()
	require.NoError(t, database.CreateJob(ctx.Server.Db, &database.ReplicationJob{ID: id, Name: "keep", SourceClusterName: "src", TargetClusterName: "tgt", Status: "paused", BatchSize: 1000}))

	status, body := sendJobRequest(t, ctx, http.MethodPut, "/api/v1/jobs/"+id, `{"name":"renamed","batch_size":200,"topic_mappings":[{"source_topic_pattern":"app-(","enabled":true}]}`)
	assert.Equal(t, http.StatusBadRequest, status, body)
	assert.True(t, strings.Contains(body, "Invalid topic mapping"), body)
	stored, err := database.GetJob(ctx.Server.Db, id)
	require.NoError(t, err)
	assert.Equal(t, "keep", stored.Name)
	assert.Equal(t, 1000, stored.BatchSize)
}
