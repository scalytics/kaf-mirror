package server_test

import (
	"encoding/json"
	"github.com/scalytics/kaf-mirror/v2/internal/database"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestGetMirrorState_Empty(t *testing.T) {
	ctx := setupTestServer(t)
	defer ctx.Server.Db.Close()

	sourceCluster := &database.KafkaCluster{Name: "src", Brokers: "localhost:9092", SecurityConfig: "{}"}
	targetCluster := &database.KafkaCluster{Name: "tgt", Brokers: "localhost:9093", SecurityConfig: "{}"}
	database.CreateCluster(ctx.Server.Db, sourceCluster)
	database.CreateCluster(ctx.Server.Db, targetCluster)

	job := &database.ReplicationJob{
		ID:                "test-job",
		Name:              "Test Job",
		SourceClusterName: "src",
		TargetClusterName: "tgt",
		Status:            "active",
	}
	database.CreateJob(ctx.Server.Db, job)

	req := httptest.NewRequest("GET", "/api/v1/jobs/test-job/mirror/state", nil)
	addAuthHeader(req, ctx.Token)
	resp, _ := ctx.Server.App.Test(req)

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// For empty state without period parameter, expect null values (single object format)
	var response map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&response)

	assert.Equal(t, "test-job", response["job_id"])
	assert.Nil(t, response["mirror_progress"])
	assert.Nil(t, response["resume_points"])
	assert.Nil(t, response["mirror_gaps"])
	assert.Nil(t, response["state_analysis"])
}

func TestGetMirrorState_WithData(t *testing.T) {
	ctx := setupTestServer(t)
	defer ctx.Server.Db.Close()

	sourceCluster := &database.KafkaCluster{Name: "src", Brokers: "localhost:9092", SecurityConfig: "{}"}
	targetCluster := &database.KafkaCluster{Name: "tgt", Brokers: "localhost:9093", SecurityConfig: "{}"}
	database.CreateCluster(ctx.Server.Db, sourceCluster)
	database.CreateCluster(ctx.Server.Db, targetCluster)

	job := &database.ReplicationJob{
		ID:                "test-job",
		Name:              "Test Job",
		SourceClusterName: "src",
		TargetClusterName: "tgt",
		Status:            "active",
	}
	database.CreateJob(ctx.Server.Db, job)

	progress := database.MirrorProgress{
		JobID:                "test-job",
		SourceTopic:          "test-topic",
		TargetTopic:          "test-topic",
		PartitionID:          0,
		SourceOffset:         100,
		TargetOffset:         100,
		SourceHighWaterMark:  120,
		TargetHighWaterMark:  120,
		LastReplicatedOffset: 100,
		ReplicationLag:       0,
		LastUpdated:          time.Now(),
		Status:               "active",
	}
	database.UpdateMirrorProgress(ctx.Server.Db, progress)

	req := httptest.NewRequest("GET", "/api/v1/jobs/test-job/mirror/state", nil)
	addAuthHeader(req, ctx.Token)
	resp, _ := ctx.Server.App.Test(req)

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// For state with data without period parameter, expect single objects (not arrays)
	var response map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&response)

	assert.Equal(t, "test-job", response["job_id"])

	// Should have a single mirror_progress object (not array)
	assert.NotNil(t, response["mirror_progress"])
	mirrorProgress := response["mirror_progress"].(map[string]interface{})
	assert.Equal(t, "test-topic", mirrorProgress["source_topic"])
	assert.Equal(t, float64(100), mirrorProgress["source_offset"]) // JSON numbers are float64

	// Other fields should still be null since we only have progress data
	assert.Nil(t, response["resume_points"])
	assert.Nil(t, response["mirror_gaps"])
	assert.Nil(t, response["state_analysis"])
}
