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
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUnknownJobRoutesReturn404(t *testing.T) {
	ctx := setupTestServer(t)
	id := uuid.NewString()
	base := "/api/v1/jobs/" + id
	cases := []struct {
		method, path, body string
	}{
		{"GET", base, ""},
		{"DELETE", base, ""},
		{"POST", base + "/start", ""},
		{"POST", base + "/stop", ""},
		{"POST", base + "/pause", ""},
		{"POST", base + "/restart", ""},
		{"POST", base + "/force-restart", ""},
		{"GET", base + "/mappings", ""},
		{"PUT", base + "/mappings", "[]"},
		{"GET", base + "/metrics/current", ""},
		{"GET", base + "/metrics/history", ""},
		{"GET", base + "/lag", ""},
		{"GET", base + "/topic-health", ""},
		{"POST", base + "/ai/analyze", ""},
		{"GET", base + "/ai/insights", ""},
		{"POST", base + "/ai/historical-analysis", ""},
		{"GET", base + "/inventory/snapshots", ""},
		{"POST", base + "/inventory/snapshots", ""},
		{"GET", base + "/mirror/state", ""},
		{"GET", base + "/mirror/progress", ""},
		{"GET", base + "/mirror/resume-points", ""},
		{"POST", base + "/mirror/resume-points", ""},
		{"GET", base + "/mirror/gaps", ""},
		{"POST", base + "/mirror/validate-mirror", ""},
		{"POST", base + "/mirror/checkpoint", "{}"},
	}
	for _, tc := range cases {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			var rdr io.Reader
			if tc.body != "" {
				rdr = bytes.NewBufferString(tc.body)
			}
			req := httptest.NewRequest(tc.method, tc.path, rdr)
			if tc.body != "" {
				req.Header.Set("Content-Type", "application/json")
			}
			addAuthHeader(req, ctx.Token)
			resp, err := ctx.Server.App.Test(req, -1)
			require.NoError(t, err)
			body, err := io.ReadAll(resp.Body)
			require.NoError(t, err)
			assert.Equal(t, 404, resp.StatusCode, string(body))
			assert.Contains(t, strings.ToLower(string(body)), "job not found")
			assert.NotEqual(t, 500, resp.StatusCode)
		})
	}
}

func TestJobRoutesReturn500WhenTheDatabaseFails(t *testing.T) {
	ctx := setupTestServer(t)
	_, err := ctx.Server.Db.Exec("PRAGMA foreign_keys = OFF")
	require.NoError(t, err)
	_, err = ctx.Server.Db.Exec("DROP TABLE replication_jobs")
	require.NoError(t, err)

	id := uuid.NewString()
	base := "/api/v1/jobs/" + id
	cases := []struct {
		method, path, body string
	}{
		{"GET", base, ""},
		{"DELETE", base, ""},
		{"GET", base + "/topic-health", ""},
		{"GET", base + "/lag", ""},
		{"POST", base + "/start", ""},
	}
	for _, tc := range cases {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			var rdr io.Reader
			if tc.body != "" {
				rdr = bytes.NewBufferString(tc.body)
			}
			req := httptest.NewRequest(tc.method, tc.path, rdr)
			if tc.body != "" {
				req.Header.Set("Content-Type", "application/json")
			}
			addAuthHeader(req, ctx.Token)
			resp, err := ctx.Server.App.Test(req, -1)
			require.NoError(t, err)
			body, err := io.ReadAll(resp.Body)
			require.NoError(t, err)
			text := string(body)
			lower := strings.ToLower(text)
			assert.Equal(t, 500, resp.StatusCode, text)
			assert.Contains(t, lower, "failed to get job")
			assert.NotContains(t, lower, "job not found")
			assert.NotContains(t, lower, "no such table")
			assert.NotContains(t, lower, "sql:")
			assert.NotContains(t, lower, "database is closed")
		})
	}
}

func TestClusterRoutesReturn404Or500ByErrorKind(t *testing.T) {
	ctx := setupTestServer(t)
	req := httptest.NewRequest("GET", "/api/v1/clusters/does-not-exist", nil)
	addAuthHeader(req, ctx.Token)
	resp, err := ctx.Server.App.Test(req, -1)
	require.NoError(t, err)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, 404, resp.StatusCode, string(body))
	assert.Contains(t, strings.ToLower(string(body)), "cluster not found")

	_, err = ctx.Server.Db.Exec("PRAGMA foreign_keys = OFF")
	require.NoError(t, err)
	_, err = ctx.Server.Db.Exec("DROP TABLE kafka_clusters")
	require.NoError(t, err)

	req = httptest.NewRequest("GET", "/api/v1/clusters/does-not-exist", nil)
	addAuthHeader(req, ctx.Token)
	resp, err = ctx.Server.App.Test(req, -1)
	require.NoError(t, err)
	body, err = io.ReadAll(resp.Body)
	require.NoError(t, err)
	text := string(body)
	lower := strings.ToLower(text)
	assert.Equal(t, 500, resp.StatusCode, text)
	assert.Contains(t, lower, "failed to get cluster")
	assert.NotContains(t, lower, "cluster not found")
	assert.NotContains(t, lower, "no such table")
	assert.NotContains(t, lower, "sql:")
	assert.NotContains(t, lower, "database is closed")
}
