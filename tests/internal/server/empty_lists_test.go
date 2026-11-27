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
	"fmt"
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/scalytics/kaf-mirror/v2/internal/database"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kfake"
)

func getBody(t *testing.T, ctx *TestContext, path string) (int, string) {
	t.Helper()
	req := httptest.NewRequest("GET", path, nil)
	addAuthHeader(req, ctx.Token)
	resp, err := ctx.Server.App.Test(req, 15000)
	require.NoError(t, err)
	b, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, strings.TrimSpace(string(b))
}

func TestEmptyClusterAndJobListsAreEmptyArrays(t *testing.T) {
	ctx := setupTestServer(t)
	for _, path := range []string{"/api/v1/clusters/", "/api/v1/jobs/"} {
		code, body := getBody(t, ctx, path)
		assert.Equal(t, 200, code, path)
		assert.Equal(t, "[]", body, path)
	}
}

func TestEveryEmptyListEndpointReturnsAnEmptyArray(t *testing.T) {
	ctx := setupTestServer(t)
	db := ctx.Server.Db
	kc, err := kfake.NewCluster(kfake.NumBrokers(1))
	require.NoError(t, err)
	defer kc.Close()
	require.NoError(t, database.CreateCluster(db, &database.KafkaCluster{Name: "src", Brokers: strings.Join(kc.ListenAddrs(), ","), SecurityConfig: "{}"}))
	require.NoError(t, database.CreateCluster(db, &database.KafkaCluster{Name: "tgt", Brokers: "localhost:1", SecurityConfig: "{}"}))
	jobID := uuid.NewString()
	require.NoError(t, database.CreateJob(db, &database.ReplicationJob{ID: jobID, Name: "empty", SourceClusterName: "src", TargetClusterName: "tgt", Status: "paused"}))
	otherJobID := uuid.NewString()
	require.NoError(t, database.CreateJob(db, &database.ReplicationJob{ID: otherJobID, Name: "with-snapshot", SourceClusterName: "src", TargetClusterName: "tgt", Status: "paused"}))
	snapshotID, err := database.CreateInventorySnapshot(db, otherJobID, "manual")
	require.NoError(t, err)
	_, err = db.Exec("DELETE FROM operational_events")
	require.NoError(t, err)

	job := "/api/v1/jobs/" + jobID
	snap := fmt.Sprintf("/api/v1/inventory/snapshots/%d", snapshotID)
	paths := []string{
		"/api/v1/ai/insights",
		"/api/v1/ai/anomalies",
		"/api/v1/ai/recommendations",
		job + "/ai/insights",
		job + "/mappings",
		job + "/metrics/history",
		job + "/inventory/snapshots",
		job + "/topic-health",
		job + "/mirror/progress",
		job + "/mirror/resume-points",
		job + "/mirror/gaps",
		snap + "/topics?cluster_type=source",
		snap + "/topics?cluster_type=target",
		snap + "/consumer-groups",
		snap + "/connections",
		"/api/v1/clusters/src/topics",
		"/api/v1/compliance/reports",
		"/api/v1/events",
	}
	for _, path := range paths {
		code, body := getBody(t, ctx, path)
		assert.Equal(t, 200, code, path)
		assert.Equal(t, "[]", body, path)
	}

	code, body := getBody(t, ctx, job+"/lag")
	assert.Equal(t, 200, code)
	assert.Equal(t, "{}", body)

	code, body = getBody(t, ctx, job+"/mirror/state?period=today")
	assert.Equal(t, 200, code)
	assert.NotContains(t, body, "null")
}
