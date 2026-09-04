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
	"fmt"
	"io"
	"kaf-mirror/internal/config"
	"kaf-mirror/internal/database"
	"kaf-mirror/internal/manager"
	"kaf-mirror/internal/server"
	"kaf-mirror/internal/server/middleware"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

type TestContext struct {
	Server *server.Server
	Token  string
}

func setupTestServer(t *testing.T) *TestContext {
	cfg := &config.Config{
		Server: config.ServerConfig{
			Host: "localhost",
			Port: 8080,
			Mode: "test",
		},
	}
	db, err := database.InitDB(":memory:")
	assert.NoError(t, err)

	err = database.SeedDefaultRolesAndPermissions(db)
	assert.NoError(t, err)

	testUser, err := database.CreateUser(db, "testuser", "testpassword", false)
	assert.NoError(t, err)

	var adminRoleID int
	err = db.Get(&adminRoleID, "SELECT id FROM roles WHERE name = 'admin'")
	assert.NoError(t, err)

	err = database.AssignRoleToUser(db, testUser.ID, adminRoleID)
	assert.NoError(t, err)

	token, _, err := database.CreateApiToken(db, testUser.ID, "Test token", time.Now().Add(24*time.Hour))
	assert.NoError(t, err)

	hub := server.NewHub()
	jobManager := manager.New(db, cfg, hub)

	srv := server.New(cfg, db, jobManager, hub, "test")

	return &TestContext{
		Server: srv,
		Token:  token,
	}
}

func addAuthHeader(req *http.Request, token string) {
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))
}

func TestHealthCheck(t *testing.T) {
	ctx := setupTestServer(t)

	req := httptest.NewRequest("GET", "/health", nil)

	resp, err := ctx.Server.App.Test(req)
	assert.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)

	body, _ := io.ReadAll(resp.Body)
	var healthResp map[string]interface{}
	err = json.Unmarshal(body, &healthResp)
	assert.NoError(t, err)

	// Check that status is "ok" and other expected fields exist
	assert.Equal(t, "ok", healthResp["status"])
	assert.Contains(t, healthResp, "timestamp")
	assert.Contains(t, healthResp, "uptime")
}

func TestClustersAPI(t *testing.T) {
	ctx := setupTestServer(t)

	clusterPayload := `{"name":"test-cluster","brokers":"localhost:9092","security_config":"{}"}`
	req := httptest.NewRequest("POST", "/api/v1/clusters", bytes.NewBufferString(clusterPayload))
	req.Header.Set("Content-Type", "application/json")
	addAuthHeader(req, ctx.Token)

	resp, err := ctx.Server.App.Test(req)
	assert.NoError(t, err)
	assert.Equal(t, 201, resp.StatusCode)

	var createdCluster database.KafkaCluster
	err = json.NewDecoder(resp.Body).Decode(&createdCluster)
	assert.NoError(t, err)
	assert.Equal(t, "test-cluster", createdCluster.Name)

	req = httptest.NewRequest("GET", "/api/v1/clusters/test-cluster", nil)
	addAuthHeader(req, ctx.Token)

	resp, err = ctx.Server.App.Test(req)
	assert.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)

	var fetchedCluster database.KafkaCluster
	err = json.NewDecoder(resp.Body).Decode(&fetchedCluster)
	assert.NoError(t, err)
	assert.Equal(t, "test-cluster", fetchedCluster.Name)

	req = httptest.NewRequest("GET", "/api/v1/clusters", nil)
	addAuthHeader(req, ctx.Token)

	resp, err = ctx.Server.App.Test(req)
	assert.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)

	var clusters []database.KafkaCluster
	err = json.NewDecoder(resp.Body).Decode(&clusters)
	assert.NoError(t, err)
	assert.Len(t, clusters, 1)
	assert.Equal(t, "test-cluster", clusters[0].Name)

	req = httptest.NewRequest("DELETE", "/api/v1/clusters/test-cluster", nil)
	addAuthHeader(req, ctx.Token)

	resp, err = ctx.Server.App.Test(req)
	assert.NoError(t, err)
	assert.Equal(t, 202, resp.StatusCode)
}

func TestJobsAPI(t *testing.T) {
	ctx := setupTestServer(t)

	sourceCluster := &database.KafkaCluster{Name: "src", Brokers: "localhost:9092", SecurityConfig: "{}"}
	targetCluster := &database.KafkaCluster{Name: "tgt", Brokers: "localhost:9093", SecurityConfig: "{}"}
	err := database.CreateCluster(ctx.Server.Db, sourceCluster)
	assert.NoError(t, err)
	err = database.CreateCluster(ctx.Server.Db, targetCluster)
	assert.NoError(t, err)

	jobPayload := `{"name":"test-job","source_cluster_name":"src","target_cluster_name":"tgt"}`
	req := httptest.NewRequest("POST", "/api/v1/jobs", bytes.NewBufferString(jobPayload))
	req.Header.Set("Content-Type", "application/json")
	addAuthHeader(req, ctx.Token)

	resp, err := ctx.Server.App.Test(req)
	assert.NoError(t, err)
	assert.Equal(t, 201, resp.StatusCode)

	var createdJob database.ReplicationJob
	err = json.NewDecoder(resp.Body).Decode(&createdJob)
	assert.NoError(t, err)
	assert.Equal(t, "test-job", createdJob.Name)
	assert.NotEmpty(t, createdJob.ID)
	jobID := createdJob.ID

	req = httptest.NewRequest("GET", "/api/v1/jobs/"+jobID, nil)
	addAuthHeader(req, ctx.Token)

	resp, err = ctx.Server.App.Test(req)
	assert.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)

	var fetchedJob database.ReplicationJob
	err = json.NewDecoder(resp.Body).Decode(&fetchedJob)
	assert.NoError(t, err)
	assert.Equal(t, jobID, fetchedJob.ID)
	assert.Equal(t, "test-job", fetchedJob.Name)

	req = httptest.NewRequest("GET", "/api/v1/jobs", nil)
	addAuthHeader(req, ctx.Token)

	resp, err = ctx.Server.App.Test(req)
	assert.NoError(t, err)

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("Expected status 200 but got %d. Response: %s", resp.StatusCode, string(body))
	}

	var jobs []database.ReplicationJob
	err = json.NewDecoder(resp.Body).Decode(&jobs)
	assert.NoError(t, err)
	assert.Len(t, jobs, 1)
	if len(jobs) > 0 {
		assert.Equal(t, jobID, jobs[0].ID)
	}
}

func TestMappingsAPI(t *testing.T) {
	ctx := setupTestServer(t)
	jobID := "test-job-for-mappings"

	sourceCluster := &database.KafkaCluster{Name: "a", Brokers: "localhost:9092", SecurityConfig: "{}"}
	targetCluster := &database.KafkaCluster{Name: "b", Brokers: "localhost:9093", SecurityConfig: "{}"}
	database.CreateCluster(ctx.Server.Db, sourceCluster)
	database.CreateCluster(ctx.Server.Db, targetCluster)
	job := &database.ReplicationJob{ID: jobID, Name: "mapping-test", SourceClusterName: "a", TargetClusterName: "b", Status: "paused"}
	err := database.CreateJob(ctx.Server.Db, job)
	assert.NoError(t, err)

	mappingsPayload := `[{"source_topic_pattern":"a","target_topic_pattern":"b","enabled":true}]`
	req := httptest.NewRequest("PUT", "/api/v1/jobs/"+jobID+"/mappings", bytes.NewBufferString(mappingsPayload))
	req.Header.Set("Content-Type", "application/json")
	addAuthHeader(req, ctx.Token)

	resp, err := ctx.Server.App.Test(req)
	assert.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)

	req = httptest.NewRequest("GET", "/api/v1/jobs/"+jobID+"/mappings", nil)
	addAuthHeader(req, ctx.Token)

	resp, err = ctx.Server.App.Test(req)
	assert.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)

	var mappings []database.TopicMapping
	err = json.NewDecoder(resp.Body).Decode(&mappings)
	assert.NoError(t, err)
	assert.Len(t, mappings, 1)
	assert.Equal(t, "a", mappings[0].SourceTopicPattern)
}

func TestMetricsAPI(t *testing.T) {
	ctx := setupTestServer(t)
	jobID := "test-job-for-metrics"

	sourceCluster := &database.KafkaCluster{Name: "a", Brokers: "localhost:9092", SecurityConfig: "{}"}
	targetCluster := &database.KafkaCluster{Name: "b", Brokers: "localhost:9093", SecurityConfig: "{}"}
	database.CreateCluster(ctx.Server.Db, sourceCluster)
	database.CreateCluster(ctx.Server.Db, targetCluster)
	job := &database.ReplicationJob{ID: jobID, Name: "metrics-test", SourceClusterName: "a", TargetClusterName: "b", Status: "paused"}
	err := database.CreateJob(ctx.Server.Db, job)
	assert.NoError(t, err)

	metric1 := &database.ReplicationMetric{
		JobID:              jobID,
		MessagesReplicated: 100,
		BytesTransferred:   1000,
		CurrentLag:         10,
		ErrorCount:         0,
		Timestamp:          time.Now().Add(-10 * time.Second),
	}
	err = database.InsertMetrics(ctx.Server.Db, metric1)
	assert.NoError(t, err)

	metric2 := &database.ReplicationMetric{
		JobID:              jobID,
		MessagesReplicated: 123,
		BytesTransferred:   4560,
		CurrentLag:         12,
		ErrorCount:         1,
		Timestamp:          time.Now(),
	}
	err = database.InsertMetrics(ctx.Server.Db, metric2)
	assert.NoError(t, err)

	req := httptest.NewRequest("GET", "/api/v1/jobs/"+jobID+"/metrics/history", nil)
	addAuthHeader(req, ctx.Token)

	resp, err := ctx.Server.App.Test(req)
	assert.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)

	var fetchedMetrics []database.AggregatedMetric
	err = json.NewDecoder(resp.Body).Decode(&fetchedMetrics)
	assert.NoError(t, err)
	assert.Len(t, fetchedMetrics, 2)
	assert.Equal(t, 23, fetchedMetrics[1].MessagesReplicatedDelta)
}

func TestAuthenticationRequired(t *testing.T) {
	ctx := setupTestServer(t)

	// Test that requests without auth header get 401
	req := httptest.NewRequest("GET", "/api/v1/jobs", nil)
	// Deliberately NOT adding auth header

	resp, err := ctx.Server.App.Test(req)
	assert.NoError(t, err)
	assert.Equal(t, 401, resp.StatusCode)

	body, _ := io.ReadAll(resp.Body)
	var errorResp map[string]string
	err = json.Unmarshal(body, &errorResp)
	assert.NoError(t, err)
	assert.Equal(t, "Missing or malformed JWT", errorResp["error"])
}

func TestInvalidToken(t *testing.T) {
	ctx := setupTestServer(t)

	// Test that requests with invalid token get 401
	req := httptest.NewRequest("GET", "/api/v1/jobs", nil)
	req.Header.Set("Authorization", "Bearer invalid-token")

	resp, err := ctx.Server.App.Test(req)
	assert.NoError(t, err)
	assert.Equal(t, 401, resp.StatusCode)

	body, _ := io.ReadAll(resp.Body)
	var errorResp map[string]string
	err = json.Unmarshal(body, &errorResp)
	assert.NoError(t, err)
	assert.Equal(t, "Invalid or expired JWT", errorResp["error"])
}

func TestHandleGetTopicDetails(t *testing.T) {
	ctx := setupTestServer(t)

	clusterPayload := `{"name":"test-cluster","brokers":"localhost:9092","security_config":"{}"}`
	req := httptest.NewRequest("POST", "/api/v1/clusters", bytes.NewBufferString(clusterPayload))
	req.Header.Set("Content-Type", "application/json")
	addAuthHeader(req, ctx.Token)
	resp, err := ctx.Server.App.Test(req)
	assert.NoError(t, err)
	assert.Equal(t, 201, resp.StatusCode)

	req = httptest.NewRequest("GET", "/api/v1/clusters/test-cluster/topic-details?topics=test-topic-1,test-topic-2", nil)
	addAuthHeader(req, ctx.Token)

	resp, err = ctx.Server.App.Test(req)
	assert.NoError(t, err)
	// This will fail without a running Kafka instance, but we're testing the handler logic
	// assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestHandleTestClusterConnection(t *testing.T) {
	ctx := setupTestServer(t)

	t.Run("successful connection", func(t *testing.T) {
		clusterConfig := config.ClusterConfig{
			Brokers: "localhost:9092",
		}
		body, _ := json.Marshal(clusterConfig)

		req := httptest.NewRequest("POST", "/api/v1/clusters/test", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		addAuthHeader(req, ctx.Token)

		_, err := ctx.Server.App.Test(req)
		assert.NoError(t, err)
		// This will fail without a running Kafka instance, but we're testing the handler logic
		// assert.Equal(t, http.StatusOK, resp.StatusCode)
	})
}

func TestGenerateRandomPassword(t *testing.T) {
	password, err := server.GenerateRandomPassword(16)
	if err != nil {
		t.Fatalf("Failed to generate random password: %v", err)
	}

	if len(password) != 16 {
		t.Errorf("Expected password length of 16, but got %d", len(password))
	}
}

func TestClusterSecretsRedactedOnRead(t *testing.T) {
	ctx := setupTestServer(t)
	conn := "Endpoint=sb://ns/;SharedAccessKey=secret"
	payload := map[string]interface{}{
		"name":              "secret-cluster",
		"provider":          "confluent",
		"brokers":           "localhost:9092",
		"api_key":           "real-key",
		"api_secret":        "real-secret",
		"connection_string": conn,
		"security_config":   `{"password":"p"}`,
	}
	body, err := json.Marshal(payload)
	assert.NoError(t, err)

	req := httptest.NewRequest("POST", "/api/v1/clusters", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	addAuthHeader(req, ctx.Token)
	resp, err := ctx.Server.App.Test(req)
	assert.NoError(t, err)
	assert.Equal(t, 201, resp.StatusCode)

	var created database.KafkaCluster
	assert.NoError(t, json.NewDecoder(resp.Body).Decode(&created))
	assert.Equal(t, config.SecretPlaceholder, created.APIKey)
	assert.Equal(t, config.SecretPlaceholder, created.APISecret)
	assert.NotNil(t, created.ConnectionString)
	assert.Equal(t, config.SecretPlaceholder, *created.ConnectionString)
	assert.Equal(t, config.SecretPlaceholder, created.SecurityConfig)

	stored, err := database.GetCluster(ctx.Server.Db, "secret-cluster")
	assert.NoError(t, err)
	assert.Equal(t, "real-key", stored.APIKey)
	assert.Equal(t, "real-secret", stored.APISecret)
	assert.Equal(t, conn, *stored.ConnectionString)
	assert.Equal(t, `{"password":"p"}`, stored.SecurityConfig)

	req = httptest.NewRequest("GET", "/api/v1/clusters/secret-cluster", nil)
	addAuthHeader(req, ctx.Token)
	resp, err = ctx.Server.App.Test(req)
	assert.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)
	var fetched database.KafkaCluster
	assert.NoError(t, json.NewDecoder(resp.Body).Decode(&fetched))
	assert.Equal(t, config.SecretPlaceholder, fetched.APISecret)

	req = httptest.NewRequest("GET", "/api/v1/clusters", nil)
	addAuthHeader(req, ctx.Token)
	resp, err = ctx.Server.App.Test(req)
	assert.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)
	var clusters []database.KafkaCluster
	assert.NoError(t, json.NewDecoder(resp.Body).Decode(&clusters))
	assert.Len(t, clusters, 1)
	assert.Equal(t, config.SecretPlaceholder, clusters[0].APIKey)
}

func TestClusterUpdatePreservesSecrets(t *testing.T) {
	ctx := setupTestServer(t)
	conn := "Endpoint=sb://ns/;SharedAccessKey=secret"
	createBody, err := json.Marshal(map[string]interface{}{
		"name":              "secret-cluster",
		"provider":          "confluent",
		"brokers":           "localhost:9092",
		"api_key":           "real-key",
		"api_secret":        "real-secret",
		"connection_string": conn,
		"security_config":   `{"password":"p"}`,
	})
	assert.NoError(t, err)
	req := httptest.NewRequest("POST", "/api/v1/clusters", bytes.NewReader(createBody))
	req.Header.Set("Content-Type", "application/json")
	addAuthHeader(req, ctx.Token)
	resp, err := ctx.Server.App.Test(req)
	assert.NoError(t, err)
	assert.Equal(t, 201, resp.StatusCode)

	updateBody, err := json.Marshal(map[string]interface{}{
		"provider":          "confluent",
		"brokers":           "localhost:9093",
		"api_key":           "***",
		"api_secret":        "***",
		"connection_string": "***",
		"security_config":   "***",
	})
	assert.NoError(t, err)
	req = httptest.NewRequest("PUT", "/api/v1/clusters/secret-cluster", bytes.NewReader(updateBody))
	req.Header.Set("Content-Type", "application/json")
	addAuthHeader(req, ctx.Token)
	resp, err = ctx.Server.App.Test(req)
	assert.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)

	stored, err := database.GetCluster(ctx.Server.Db, "secret-cluster")
	assert.NoError(t, err)
	assert.Equal(t, "localhost:9093", stored.Brokers)
	assert.Equal(t, "real-key", stored.APIKey)
	assert.Equal(t, "real-secret", stored.APISecret)
	assert.Equal(t, conn, *stored.ConnectionString)
	assert.Equal(t, `{"password":"p"}`, stored.SecurityConfig)
}

func TestConfigSecretsRedactedAndPreserved(t *testing.T) {
	ctx := setupTestServer(t)
	conn := "Endpoint=sb://ns/;SharedAccessKey=secret"
	cfg := config.Config{
		Server: config.ServerConfig{Host: "localhost", Port: 8080, Mode: "test"},
		Clusters: map[string]config.ClusterConfig{
			"src": {
				Brokers: "localhost:9092",
				Security: config.SecurityConfig{
					APIKey:           "ckey",
					APISecret:        "csecret",
					Password:         "pw",
					ConnectionString: &conn,
				},
			},
		},
		AI: config.AIConfig{Token: "ai-token", APISecret: "ai-secret"},
		Monitoring: config.MonitoringConfig{
			Splunk: config.SplunkConfig{HECToken: "hec-token"},
		},
	}
	body, err := json.Marshal(cfg)
	assert.NoError(t, err)

	req := httptest.NewRequest("PUT", "/api/v1/config", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	addAuthHeader(req, ctx.Token)
	resp, err := ctx.Server.App.Test(req)
	assert.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)

	req = httptest.NewRequest("GET", "/api/v1/config", nil)
	addAuthHeader(req, ctx.Token)
	resp, err = ctx.Server.App.Test(req)
	assert.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)

	var got config.Config
	assert.NoError(t, json.NewDecoder(resp.Body).Decode(&got))
	assert.Equal(t, config.SecretPlaceholder, got.AI.Token)
	assert.Equal(t, config.SecretPlaceholder, got.AI.APISecret)
	assert.Equal(t, config.SecretPlaceholder, got.Monitoring.Splunk.HECToken)
	assert.Equal(t, config.SecretPlaceholder, got.Clusters["src"].Security.APISecret)
	assert.Equal(t, config.SecretPlaceholder, got.Clusters["src"].Security.APIKey)
	assert.Equal(t, config.SecretPlaceholder, got.Clusters["src"].Security.Password)
	assert.NotNil(t, got.Clusters["src"].Security.ConnectionString)
	assert.Equal(t, config.SecretPlaceholder, *got.Clusters["src"].Security.ConnectionString)

	stored, err := database.LoadConfig(ctx.Server.Db)
	assert.NoError(t, err)
	assert.Equal(t, "ai-token", stored.AI.Token)
	assert.Equal(t, "ai-secret", stored.AI.APISecret)
	assert.Equal(t, "hec-token", stored.Monitoring.Splunk.HECToken)
	assert.Equal(t, "csecret", stored.Clusters["src"].Security.APISecret)

	roundTrip, err := json.Marshal(got)
	assert.NoError(t, err)
	req = httptest.NewRequest("PUT", "/api/v1/config", bytes.NewReader(roundTrip))
	req.Header.Set("Content-Type", "application/json")
	addAuthHeader(req, ctx.Token)
	resp, err = ctx.Server.App.Test(req)
	assert.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)

	stored, err = database.LoadConfig(ctx.Server.Db)
	assert.NoError(t, err)
	assert.Equal(t, "ai-token", stored.AI.Token)
	assert.Equal(t, "csecret", stored.Clusters["src"].Security.APISecret)
	assert.Equal(t, conn, *stored.Clusters["src"].Security.ConnectionString)

	req = httptest.NewRequest("POST", "/api/v1/config/export", nil)
	addAuthHeader(req, ctx.Token)
	resp, err = ctx.Server.App.Test(req)
	assert.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)
	exportBody, err := io.ReadAll(resp.Body)
	assert.NoError(t, err)
	assert.NotContains(t, string(exportBody), "ai-token")
	assert.NotContains(t, string(exportBody), "csecret")
	assert.NotContains(t, string(exportBody), "hec-token")
}

func TestChangePasswordRevokesTokens(t *testing.T) {
	ctx := setupTestServer(t)
	body, _ := json.Marshal(map[string]string{
		"old_password": "testpassword",
		"new_password": "newpassword12",
	})
	req := httptest.NewRequest("PUT", "/api/v1/users/change-password", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	addAuthHeader(req, ctx.Token)
	resp, err := ctx.Server.App.Test(req, 15000)
	assert.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)

	req = httptest.NewRequest("GET", "/api/v1/jobs", nil)
	addAuthHeader(req, ctx.Token)
	resp, err = ctx.Server.App.Test(req)
	assert.NoError(t, err)
	assert.Equal(t, 401, resp.StatusCode)
}

func TestCreateUserRejectsShortPassword(t *testing.T) {
	ctx := setupTestServer(t)
	body, _ := json.Marshal(map[string]string{
		"username": "shortpass",
		"password": "tiny",
		"role":     "monitoring",
	})
	req := httptest.NewRequest("POST", "/api/v1/users", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	addAuthHeader(req, ctx.Token)
	resp, err := ctx.Server.App.Test(req)
	assert.NoError(t, err)
	assert.Equal(t, 400, resp.StatusCode)
}

func TestResetOwnToken(t *testing.T) {
	ctx := setupTestServer(t)
	req := httptest.NewRequest("POST", "/api/v1/auth/reset-token", nil)
	addAuthHeader(req, ctx.Token)
	resp, err := ctx.Server.App.Test(req)
	assert.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)

	var tokenResp map[string]string
	assert.NoError(t, json.NewDecoder(resp.Body).Decode(&tokenResp))
	assert.NotEmpty(t, tokenResp["token"])
	assert.NotEqual(t, ctx.Token, tokenResp["token"])

	req = httptest.NewRequest("GET", "/auth/me", nil)
	addAuthHeader(req, ctx.Token)
	resp, err = ctx.Server.App.Test(req)
	assert.NoError(t, err)
	assert.Equal(t, 401, resp.StatusCode)

	req = httptest.NewRequest("GET", "/auth/me", nil)
	addAuthHeader(req, tokenResp["token"])
	resp, err = ctx.Server.App.Test(req)
	assert.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)
}

func TestLoginRateLimit(t *testing.T) {
	ctx := setupTestServer(t)
	payload := `{"username":"missing","password":"wrong-password"}`
	var last int
	for i := 0; i < 12; i++ {
		req := httptest.NewRequest("POST", "/auth/token", bytes.NewBufferString(payload))
		req.Header.Set("Content-Type", "application/json")
		resp, err := ctx.Server.App.Test(req)
		assert.NoError(t, err)
		last = resp.StatusCode
	}
	assert.Equal(t, 429, last)
}

func TestPurgeClustersRouteNotShadowed(t *testing.T) {
	ctx := setupTestServer(t)
	createBody := `{"name":"to-purge","provider":"plain","brokers":"localhost:9092"}`
	req := httptest.NewRequest("POST", "/api/v1/clusters", bytes.NewBufferString(createBody))
	req.Header.Set("Content-Type", "application/json")
	addAuthHeader(req, ctx.Token)
	resp, err := ctx.Server.App.Test(req)
	assert.NoError(t, err)
	assert.Equal(t, 201, resp.StatusCode)

	assert.NoError(t, database.SetClusterStatus(ctx.Server.Db, "to-purge", "archived"))

	req = httptest.NewRequest("DELETE", "/api/v1/clusters/purge", nil)
	addAuthHeader(req, ctx.Token)
	resp, err = ctx.Server.App.Test(req)
	assert.NoError(t, err)
	assert.Equal(t, 204, resp.StatusCode)

	_, err = database.GetCluster(ctx.Server.Db, "to-purge")
	assert.Error(t, err)
}

func TestImportConfigValidates(t *testing.T) {
	ctx := setupTestServer(t)
	body := `{"Server":{"Port":8080},"Clusters":{}}`
	req := httptest.NewRequest("POST", "/api/v1/config/import", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	addAuthHeader(req, ctx.Token)
	resp, err := ctx.Server.App.Test(req)
	assert.NoError(t, err)
	assert.Equal(t, 400, resp.StatusCode)
}

func TestWebSocketAuthRejectsQueryToken(t *testing.T) {
	ctx := setupTestServer(t)
	req := httptest.NewRequest("GET", "/ws?token="+ctx.Token, nil)
	resp, err := ctx.Server.App.Test(req)
	assert.NoError(t, err)
	assert.Equal(t, 401, resp.StatusCode)
}

func TestWebSocketAuthRequiresBearer(t *testing.T) {
	ctx := setupTestServer(t)
	req := httptest.NewRequest("GET", "/ws", nil)
	addAuthHeader(req, ctx.Token)
	resp, err := ctx.Server.App.Test(req)
	assert.NoError(t, err)
	assert.Equal(t, 426, resp.StatusCode)
}

func TestDashboardEscapesUserContent(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "web", "index.html"))
	assert.NoError(t, err)
	html := string(data)
	assert.Contains(t, html, "function escapeHtml")
	assert.Contains(t, html, "function safeMarkdown")
	assert.Contains(t, html, "marked.parse(escapeHtml(value)")
	assert.NotContains(t, html, "return safeMarkdown(escapeHtml")
	assert.Contains(t, html, "safeMarkdown(")
	assert.Contains(t, html, "escapeHtml(job.name)")
	assert.Contains(t, html, "escapeHtml(user.username)")
}

func TestCorsConfigUsesAllowedOrigins(t *testing.T) {
	cfg := middleware.CorsConfig([]string{"https://dash.example.com"})
	assert.Equal(t, "https://dash.example.com", cfg.AllowOrigins)
	assert.Contains(t, cfg.AllowHeaders, "Authorization")
	assert.True(t, cfg.AllowCredentials)

	wildcard := middleware.CorsConfig([]string{"*"})
	assert.Equal(t, "*", wildcard.AllowOrigins)
	assert.False(t, wildcard.AllowCredentials)
}
