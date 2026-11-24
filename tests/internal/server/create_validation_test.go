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
	"net/http"
	"testing"

	"kaf-mirror/internal/database"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const ordersMapping = `"topic_mappings":[{"source_topic_pattern":"orders","target_topic_pattern":"orders","enabled":true}]`

func TestCreateClusterRejectsMissingRequiredFields(t *testing.T) {
	ctx := setupTestServer(t)
	cases := []struct {
		body string
		want string
	}{
		{`{"name":"","brokers":""}`, "name is required"},
		{`{"name":"   ","brokers":"localhost:9092"}`, "name is required"},
		{`{"brokers":"localhost:9092"}`, "name is required"},
		{`{"name":"no-brokers"}`, "brokers is required"},
		{`{"name":"blank-brokers","brokers":" , "}`, "brokers is required"},
		{`{"name":"a/b","brokers":"localhost:9092"}`, "name must not contain /"},
	}
	for _, tc := range cases {
		status, body := sendJobRequest(t, ctx, http.MethodPost, "/api/v1/clusters", tc.body)
		assert.Equal(t, http.StatusBadRequest, status, tc.body)
		assert.Contains(t, body, tc.want, tc.body)
	}
	clusters, err := database.ListClusters(ctx.Server.Db)
	require.NoError(t, err)
	assert.Empty(t, clusters)
}

func TestUpdateClusterRejectsEmptyBrokers(t *testing.T) {
	ctx := setupJobServer(t)
	status, body := sendJobRequest(t, ctx, http.MethodPut, "/api/v1/clusters/src", `{"brokers":""}`)
	assert.Equal(t, http.StatusBadRequest, status, body)
	assert.Contains(t, body, "brokers is required")
	stored, err := database.GetCluster(ctx.Server.Db, "src")
	require.NoError(t, err)
	assert.Equal(t, "localhost:9092", stored.Brokers)
}

func TestCreateClusterWithADuplicateNameIsAConflict(t *testing.T) {
	ctx := setupJobServer(t)
	status, body := sendJobRequest(t, ctx, http.MethodPost, "/api/v1/clusters", `{"name":"src","brokers":"localhost:9094"}`)
	assert.Equal(t, http.StatusConflict, status, body)
	assert.Contains(t, body, "a cluster with this name already exists")
}

func TestCreateJobRejectsMissingRequiredFields(t *testing.T) {
	ctx := setupJobServer(t)
	cases := []struct {
		body string
		want string
	}{
		{`{}`, "name is required"},
		{`{"name":" ","source_cluster_name":"src","target_cluster_name":"tgt",` + ordersMapping + `}`, "name is required"},
		{`{"name":"x","target_cluster_name":"tgt",` + ordersMapping + `}`, "source_cluster_name is required"},
		{`{"name":"x","source_cluster_name":"src",` + ordersMapping + `}`, "target_cluster_name is required"},
		{`{"name":"x","source_cluster_name":"   ","target_cluster_name":"tgt",` + ordersMapping + `}`, "source_cluster_name is required"},
		{`{"name":"x","source_cluster_name":"src","target_cluster_name":"  ",` + ordersMapping + `}`, "target_cluster_name is required"},
		{`{"name":"x","source_cluster_name":"src","target_cluster_name":"tgt"}`, "topic_mappings needs at least one mapping"},
		{`{"name":"x","source_cluster_name":"src","target_cluster_name":"tgt","topic_mappings":[]}`, "topic_mappings needs at least one mapping"},
	}
	for _, tc := range cases {
		status, body := sendJobRequest(t, ctx, http.MethodPost, "/api/v1/jobs", tc.body)
		assert.Equal(t, http.StatusBadRequest, status, tc.body)
		assert.Contains(t, body, tc.want, tc.body)
	}
	jobs, err := database.ListJobs(ctx.Server.Db)
	require.NoError(t, err)
	assert.Empty(t, jobs)
}

func TestCreateJobWithAnUnknownClusterIsNotFound(t *testing.T) {
	ctx := setupJobServer(t)
	status, body := sendJobRequest(t, ctx, http.MethodPost, "/api/v1/jobs", `{"name":"x","source_cluster_name":"nope","target_cluster_name":"tgt",`+ordersMapping+`}`)
	assert.Equal(t, http.StatusNotFound, status, body)
	assert.Contains(t, body, "Source cluster not found")

	status, body = sendJobRequest(t, ctx, http.MethodPost, "/api/v1/jobs", `{"name":"x","source_cluster_name":"src","target_cluster_name":"nope2",`+ordersMapping+`}`)
	assert.Equal(t, http.StatusNotFound, status, body)
	assert.Contains(t, body, "Target cluster not found")

	jobs, err := database.ListJobs(ctx.Server.Db)
	require.NoError(t, err)
	assert.Empty(t, jobs)
}

func TestCreateJobReturnsCreatedAtAndRefusesADuplicateName(t *testing.T) {
	ctx := setupJobServer(t)
	status, body := sendJobRequest(t, ctx, http.MethodPost, "/api/v1/jobs", `{"name":"orders","source_cluster_name":"src","target_cluster_name":"tgt",`+ordersMapping+`}`)
	require.Equal(t, http.StatusCreated, status, body)
	var job database.ReplicationJob
	require.NoError(t, json.Unmarshal([]byte(body), &job))
	assert.False(t, job.CreatedAt.IsZero(), "created_at: %s", body)
	stored, err := database.GetJob(ctx.Server.Db, job.ID)
	require.NoError(t, err)
	assert.WithinDuration(t, stored.CreatedAt, job.CreatedAt, 0)

	status, body = sendJobRequest(t, ctx, http.MethodPost, "/api/v1/jobs", `{"name":"orders","source_cluster_name":"src","target_cluster_name":"tgt",`+ordersMapping+`}`)
	assert.Equal(t, http.StatusConflict, status, body)
	assert.Contains(t, body, "a job with this name already exists")
}

func TestUpdateJobWithAnUnknownClusterIsNotFound(t *testing.T) {
	ctx := setupJobServer(t)
	id := uuid.NewString()
	require.NoError(t, database.CreateJob(ctx.Server.Db, &database.ReplicationJob{ID: id, Name: "keep", SourceClusterName: "src", TargetClusterName: "tgt", Status: "paused", BatchSize: 1000}))

	status, body := sendJobRequest(t, ctx, http.MethodPut, "/api/v1/jobs/"+id, `{"source_cluster_name":"nope"}`)
	assert.Equal(t, http.StatusNotFound, status, body)
	assert.Contains(t, body, "Source cluster not found")
	status, body = sendJobRequest(t, ctx, http.MethodPut, "/api/v1/jobs/"+id, `{"target_cluster_name":"nope"}`)
	assert.Equal(t, http.StatusNotFound, status, body)
	assert.Contains(t, body, "Target cluster not found")

	stored, err := database.GetJob(ctx.Server.Db, id)
	require.NoError(t, err)
	assert.Equal(t, "src", stored.SourceClusterName)
	assert.Equal(t, "tgt", stored.TargetClusterName)
}

func TestDeleteUnknownClusterIsNotFound(t *testing.T) {
	ctx := setupTestServer(t)
	status, body := sendJobRequest(t, ctx, http.MethodDelete, "/api/v1/clusters/nope", "")
	assert.Equal(t, http.StatusNotFound, status, body)
	assert.Contains(t, body, "Cluster not found")
}

func TestPurgeRemovesAnUnusedClusterWithAnEmptyName(t *testing.T) {
	ctx := setupJobServer(t)
	db := ctx.Server.Db
	require.NoError(t, database.CreateCluster(db, &database.KafkaCluster{Name: "", SecurityConfig: "{}"}))

	status, body := sendJobRequest(t, ctx, http.MethodDelete, "/api/v1/clusters/purge", "")
	require.Equal(t, http.StatusNoContent, status, body)
	clusters, err := database.ListClusters(db)
	require.NoError(t, err)
	var names []string
	for _, c := range clusters {
		names = append(names, c.Name)
	}
	assert.ElementsMatch(t, []string{"src", "tgt"}, names)
}

func TestPurgeKeepsAnEmptyNamedClusterThatAJobUses(t *testing.T) {
	ctx := setupJobServer(t)
	db := ctx.Server.Db
	require.NoError(t, database.CreateCluster(db, &database.KafkaCluster{Name: "", SecurityConfig: "{}"}))
	id := uuid.NewString()
	require.NoError(t, database.CreateJob(db, &database.ReplicationJob{ID: id, Name: "legacy", SourceClusterName: "", TargetClusterName: "tgt", Status: "paused", BatchSize: 1000}))

	status, body := sendJobRequest(t, ctx, http.MethodDelete, "/api/v1/clusters/purge", "")
	require.Equal(t, http.StatusNoContent, status, body)
	_, err := database.GetCluster(db, "")
	require.NoError(t, err)

	status, body = sendJobRequest(t, ctx, http.MethodDelete, "/api/v1/jobs/"+id, "")
	require.Equal(t, http.StatusNoContent, status, body)
	status, body = sendJobRequest(t, ctx, http.MethodDelete, "/api/v1/clusters/purge", "")
	require.Equal(t, http.StatusNoContent, status, body)
	_, err = database.GetCluster(db, "")
	require.Error(t, err)
}

func TestUpdateJobWithABlankNameKeepsTheStoredName(t *testing.T) {
	ctx := setupJobServer(t)
	id := uuid.NewString()
	require.NoError(t, database.CreateJob(ctx.Server.Db, &database.ReplicationJob{ID: id, Name: "keep", SourceClusterName: "src", TargetClusterName: "tgt", Status: "paused", BatchSize: 1000}))

	status, body := sendJobRequest(t, ctx, http.MethodPut, "/api/v1/jobs/"+id, `{"name":"   ","source_cluster_name":"  ","target_cluster_name":"   "}`)
	assert.Equal(t, http.StatusOK, status, body)
	stored, err := database.GetJob(ctx.Server.Db, id)
	require.NoError(t, err)
	assert.Equal(t, "keep", stored.Name)
	assert.Equal(t, "src", stored.SourceClusterName)
	assert.Equal(t, "tgt", stored.TargetClusterName)
}

func TestCreateClusterTrimsTheName(t *testing.T) {
	ctx := setupJobServer(t)
	status, body := sendJobRequest(t, ctx, http.MethodPost, "/api/v1/clusters", `{"name":"  edge  ","brokers":"localhost:9094","security_config":"{}"}`)
	require.Equal(t, http.StatusCreated, status, body)
	stored, err := database.GetCluster(ctx.Server.Db, "edge")
	require.NoError(t, err)
	assert.Equal(t, "edge", stored.Name)

	status, body = sendJobRequest(t, ctx, http.MethodPost, "/api/v1/clusters", `{"name":"edge ","brokers":"localhost:9094","security_config":"{}"}`)
	assert.Equal(t, http.StatusConflict, status, body)

	status, body = sendJobRequest(t, ctx, http.MethodPost, "/api/v1/jobs", `{"name":"j","source_cluster_name":" edge ","target_cluster_name":"tgt",`+ordersMapping+`}`)
	assert.Equal(t, http.StatusCreated, status, body)
}

func TestDuplicateCreatesPastTheCheckAnswerConflict(t *testing.T) {
	ctx := setupJobServer(t)
	db := ctx.Server.Db
	var restore func()
	restore = database.SetBeforeCreateInsertForTest(func() {
		restore()
		require.NoError(t, database.CreateCluster(db, &database.KafkaCluster{Name: "race", Brokers: "localhost:9095", SecurityConfig: "{}"}))
	})
	t.Cleanup(func() { restore() })
	status, body := sendJobRequest(t, ctx, http.MethodPost, "/api/v1/clusters", `{"name":"race","brokers":"localhost:9096","security_config":"{}"}`)
	assert.Equal(t, http.StatusConflict, status, body)

	restore = database.SetBeforeCreateInsertForTest(func() {
		restore()
		require.NoError(t, database.CreateJob(db, &database.ReplicationJob{ID: uuid.NewString(), Name: "racer", SourceClusterName: "src", TargetClusterName: "tgt", Status: "paused", BatchSize: 1000}))
	})
	status, body = sendJobRequest(t, ctx, http.MethodPost, "/api/v1/jobs", `{"name":"racer","source_cluster_name":"src","target_cluster_name":"tgt",`+ordersMapping+`}`)
	assert.Equal(t, http.StatusConflict, status, body)
	var count int
	require.NoError(t, db.Get(&count, "SELECT COUNT(*) FROM replication_jobs WHERE name = ?", "racer"))
	assert.Equal(t, 1, count)
}
