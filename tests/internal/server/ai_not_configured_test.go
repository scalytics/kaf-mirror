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
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/scalytics/kaf-mirror/v2/internal/database"

	"github.com/google/uuid"
)

func TestAIEndpointsAnswer503WhenAIIsNotConfigured(t *testing.T) {
	ctx := setupTestServer(t)
	jobID := uuid.NewString()
	if err := database.CreateCluster(ctx.Server.Db, &database.KafkaCluster{Name: "ai-src", Brokers: "localhost:1", SecurityConfig: "{}"}); err != nil {
		t.Fatal(err)
	}
	if err := database.CreateCluster(ctx.Server.Db, &database.KafkaCluster{Name: "ai-tgt", Brokers: "localhost:2", SecurityConfig: "{}"}); err != nil {
		t.Fatal(err)
	}
	if err := database.CreateJob(ctx.Server.Db, &database.ReplicationJob{ID: jobID, Name: "ai-off", SourceClusterName: "ai-src", TargetClusterName: "ai-tgt", Status: "paused"}); err != nil {
		t.Fatal(err)
	}
	paths := []string{
		"/api/v1/ai/test",
		"/api/v1/ai/explain/lag",
		"/api/v1/ai/incidents/1/analyze",
		"/api/v1/jobs/" + jobID + "/ai/analyze",
		"/api/v1/jobs/" + jobID + "/ai/historical-analysis",
	}
	for _, path := range paths {
		req := httptest.NewRequest(http.MethodPost, path, nil)
		addAuthHeader(req, ctx.Token)
		resp, err := ctx.Server.App.Test(req)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusServiceUnavailable {
			t.Fatalf("%s: status %d, want 503, body %s", path, resp.StatusCode, body)
		}
		if !strings.Contains(string(body), "AI is not configured") {
			t.Fatalf("%s: body %q does not say AI is not configured", path, body)
		}
	}
	insights, err := database.ListAIInsights(ctx.Server.Db, "", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(insights) != 0 {
		t.Fatalf("got %d AI insights, want 0", len(insights))
	}
}
