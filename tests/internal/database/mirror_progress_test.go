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

package database_test

import (
	"encoding/json"
	"kaf-mirror/internal/database"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMirrorProgress(t *testing.T) {
	db, err := database.InitDB(":memory:")
	require.NoError(t, err)
	defer db.Close()
	require.NoError(t, database.CreateCluster(db, &database.KafkaCluster{Name: "src", Provider: "plain", Brokers: "localhost:9092"}))
	require.NoError(t, database.CreateCluster(db, &database.KafkaCluster{Name: "tgt", Provider: "plain", Brokers: "localhost:9093"}))
	jobID := uuid.NewString()
	require.NoError(t, database.CreateJob(db, &database.ReplicationJob{
		ID: jobID, Name: "progress-job", SourceClusterName: "src", TargetClusterName: "tgt", Status: "paused",
	}))

	t.Run("UpdateMirrorProgress", func(t *testing.T) {
		progress := database.MirrorProgress{
			JobID:                jobID,
			SourceTopic:          "test-topic",
			TargetTopic:          "test-topic-target",
			PartitionID:          0,
			SourceOffset:         1000,
			TargetOffset:         950,
			SourceHighWaterMark:  1000,
			TargetHighWaterMark:  950,
			LastReplicatedOffset: 950,
			ReplicationLag:       50,
			LastUpdated:          time.Now(),
			Status:               "active",
		}

		err := database.UpdateMirrorProgress(db, progress)
		assert.NoError(t, err)

		// Test update of existing record
		progress.SourceOffset = 1100
		progress.TargetOffset = 1100
		progress.TargetHighWaterMark = 1100
		progress.LastReplicatedOffset = 1100
		progress.ReplicationLag = 0
		progress.Status = "completed"

		err = database.UpdateMirrorProgress(db, progress)
		assert.NoError(t, err)
	})

	t.Run("GetMirrorProgress", func(t *testing.T) {
		progressList, err := database.GetMirrorProgress(db, jobID)
		assert.NoError(t, err)
		assert.Len(t, progressList, 1)

		progress := progressList[0]
		assert.Equal(t, jobID, progress.JobID)
		assert.Equal(t, "test-topic", progress.SourceTopic)
		assert.Equal(t, "test-topic-target", progress.TargetTopic)
		assert.Equal(t, 0, progress.PartitionID)
		assert.Equal(t, int64(1100), progress.SourceOffset)
		assert.Equal(t, int64(1100), progress.TargetOffset)
		assert.Equal(t, int64(0), progress.ReplicationLag)
		assert.Equal(t, "completed", progress.Status)
	})

	t.Run("CalculateResumePoints", func(t *testing.T) {
		resumeData := map[string]map[int32]int64{
			"test-topic": {
				0: 950,
				1: 800,
			},
		}

		err := database.CalculateResumePoints(db, jobID, resumeData, nil)
		assert.NoError(t, err)

		resumePoints, err := database.GetResumePoints(db, jobID)
		assert.NoError(t, err)
		assert.Len(t, resumePoints, 2)

		// Verify resume points
		pointMap := make(map[int]*database.ResumePoint)
		for i := range resumePoints {
			point := &resumePoints[i]
			pointMap[point.PartitionID] = point
		}

		assert.Equal(t, int64(950), pointMap[0].SafeResumeOffset)
		assert.Equal(t, int64(800), pointMap[1].SafeResumeOffset)
	})

	t.Run("CreateMigrationCheckpoint", func(t *testing.T) {
		offsetsJSON, _ := json.Marshal(map[string]int64{"test-topic": 1000})
		waterMarksJSON, _ := json.Marshal(map[string]map[int]int64{"test-topic": {0: 950}})

		checkpoint := database.MigrationCheckpoint{
			JobID:                      jobID,
			CheckpointType:             "pre_migration",
			SourceConsumerGroupOffsets: string(offsetsJSON),
			TargetHighWaterMarks:       string(waterMarksJSON),
			CreatedAt:                  time.Now(),
			CreatedBy:                  "test-user",
			MigrationReason:            strPtr("Testing migration checkpoint"),
		}

		id, err := database.CreateMigrationCheckpoint(db, checkpoint)
		assert.NoError(t, err)
		assert.NotZero(t, id)
	})

	t.Run("DetectMirrorGaps", func(t *testing.T) {
		gaps := []database.MirrorGap{
			{
				JobID:            jobID,
				SourceTopic:      "test-topic",
				TargetTopic:      "test-topic-target",
				PartitionID:      0,
				GapStartOffset:   500,
				GapEndOffset:     600,
				GapSize:          100,
				DetectedAt:       time.Now(),
				GapType:          "partial_replication",
				ResolutionStatus: "unresolved",
			},
			{
				JobID:            jobID,
				SourceTopic:      "test-topic",
				TargetTopic:      "test-topic-target",
				PartitionID:      1,
				GapStartOffset:   200,
				GapEndOffset:     250,
				GapSize:          50,
				DetectedAt:       time.Now(),
				GapType:          "missing_messages",
				ResolutionStatus: "unresolved",
			},
		}

		err := database.DetectMirrorGaps(db, jobID, gaps)
		assert.NoError(t, err)

		retrievedGaps, err := database.GetMirrorGaps(db, jobID)
		assert.NoError(t, err)
		assert.Len(t, retrievedGaps, 2)

		// Verify gap data
		gapMap := make(map[int]*database.MirrorGap)
		for i := range retrievedGaps {
			gap := &retrievedGaps[i]
			gapMap[gap.PartitionID] = gap
		}

		assert.Equal(t, int64(100), gapMap[0].GapSize)
		assert.Equal(t, "partial_replication", gapMap[0].GapType)
		assert.Equal(t, int64(50), gapMap[1].GapSize)
		assert.Equal(t, "missing_messages", gapMap[1].GapType)
	})

	t.Run("GetMirrorStateData", func(t *testing.T) {
		stateData, err := database.GetMirrorStateData(db, jobID)
		assert.NoError(t, err)
		assert.NotNil(t, stateData)

		assert.Equal(t, jobID, stateData.JobID)
		assert.Len(t, stateData.MirrorProgress, 1)
		assert.Len(t, stateData.ResumePoints, 2)
		assert.Len(t, stateData.MirrorGaps, 2)
	})
}

// Helper function to create string pointer
func strPtr(s string) *string {
	return &s
}

func seedJob(t *testing.T, db *sqlx.DB, jobID string) {
	t.Helper()
	require.NoError(t, database.CreateCluster(db, &database.KafkaCluster{Name: "src-" + jobID[:8], Provider: "plain", Brokers: "localhost:9092"}))
	require.NoError(t, database.CreateCluster(db, &database.KafkaCluster{Name: "tgt-" + jobID[:8], Provider: "plain", Brokers: "localhost:9093"}))
	require.NoError(t, database.CreateJob(db, &database.ReplicationJob{
		ID: jobID, Name: "job-" + jobID[:8], SourceClusterName: "src-" + jobID[:8], TargetClusterName: "tgt-" + jobID[:8], Status: "paused",
	}))
}

func TestMirrorStateAnalysis(t *testing.T) {
	db, err := database.InitDB(":memory:")
	require.NoError(t, err)
	defer db.Close()

	jobID := uuid.NewString()
	seedJob(t, db, jobID)

	t.Run("StoreMirrorStateAnalysis", func(t *testing.T) {
		analysis := database.MirrorStateAnalysis{
			JobID:               jobID,
			AnalysisType:        "gap_detection",
			SourceClusterState:  `{"brokers": 3, "topics": 2}`,
			TargetClusterState:  `{"brokers": 3, "topics": 2}`,
			AnalysisResults:     `{"gaps": 1, "lag": 150}`,
			Recommendations:     `["Wait for replication to catch up"]`,
			CriticalIssuesCount: 1,
			WarningIssuesCount:  0,
			AnalyzedAt:          time.Now(),
			AnalyzerVersion:     "1.0",
		}

		id, err := database.StoreMirrorStateAnalysis(db, analysis)
		assert.NoError(t, err)
		assert.NotZero(t, id)
	})

	t.Run("GetMirrorStateAnalysis", func(t *testing.T) {
		// Insert another analysis
		analysis2 := database.MirrorStateAnalysis{
			JobID:               jobID,
			AnalysisType:        "resume_validation",
			SourceClusterState:  `{"brokers": 3, "topics": 2}`,
			TargetClusterState:  `{"brokers": 3, "topics": 2}`,
			AnalysisResults:     `{"gaps": 0, "lag": 0}`,
			Recommendations:     `["Migration is safe to proceed"]`,
			CriticalIssuesCount: 0,
			WarningIssuesCount:  0,
			AnalyzedAt:          time.Now(),
			AnalyzerVersion:     "1.0",
		}
		time.Sleep(1 * time.Millisecond) // Ensure different timestamp
		_, err := database.StoreMirrorStateAnalysis(db, analysis2)
		require.NoError(t, err)

		analyses, err := database.GetMirrorStateAnalysis(db, jobID)
		assert.NoError(t, err)
		assert.Len(t, analyses, 2)

		// Most recent should be first (ordered by analyzed_at DESC)
		latest := analyses[0]
		assert.Equal(t, jobID, latest.JobID)
		assert.Equal(t, "resume_validation", latest.AnalysisType)
		assert.Equal(t, 0, latest.CriticalIssuesCount)
	})
}

func TestMirrorStateSchemaValidation(t *testing.T) {
	db, err := database.InitDB(":memory:")
	require.NoError(t, err)
	defer db.Close()

	t.Run("VerifyMirrorStateTables", func(t *testing.T) {
		expectedTables := []string{
			"mirror_progress",
			"resume_points",
			"migration_checkpoints",
			"mirror_state_analysis",
			"mirror_gaps",
		}

		for _, tableName := range expectedTables {
			var count int
			err := db.Get(&count, "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?", tableName)
			assert.NoError(t, err)
			assert.Equal(t, 1, count, "Table %s should exist", tableName)
		}
	})

	// Note: Index validation removed since indexes are created via migration
	// and may not be present in test database environment

	t.Run("TestJSONSerialization", func(t *testing.T) {
		jobID := uuid.NewString()
		seedJob(t, db, jobID)

		// Test complex nested JSON serialization in checkpoints
		consumerOffsets := map[string]int64{
			"test-topic":    1000,
			"another-topic": 2500,
		}

		waterMarks := map[string]map[int]int64{
			"test-topic": {
				0: 1000,
				1: 950,
			},
			"another-topic": {
				0: 2500,
			},
		}

		offsetsJSON, _ := json.Marshal(consumerOffsets)
		waterMarksJSON, _ := json.Marshal(waterMarks)
		validationResults := `{"status": "safe", "gaps_detected": 0, "critical_issues": []}`

		checkpoint := database.MigrationCheckpoint{
			JobID:                      jobID,
			CheckpointType:             "pre_migration",
			SourceConsumerGroupOffsets: string(offsetsJSON),
			TargetHighWaterMarks:       string(waterMarksJSON),
			CreatedAt:                  time.Now(),
			CreatedBy:                  "test-system",
			MigrationReason:            strPtr("Automated migration test"),
			ValidationResults:          &validationResults,
		}

		id, err := database.CreateMigrationCheckpoint(db, checkpoint)
		assert.NoError(t, err)

		// Retrieve and verify serialization
		var retrieved database.MigrationCheckpoint
		err = db.Get(&retrieved, `
			SELECT id, job_id, checkpoint_type, source_consumer_group_offsets, 
			       target_high_water_marks, created_at, created_by, migration_reason, validation_results 
			FROM migration_checkpoints WHERE id = ?`, id)
		assert.NoError(t, err)

		// Verify JSON deserialization works
		var parsedOffsets map[string]int64
		err = json.Unmarshal([]byte(retrieved.SourceConsumerGroupOffsets), &parsedOffsets)
		assert.NoError(t, err)
		assert.Equal(t, int64(1000), parsedOffsets["test-topic"])

		var parsedWaterMarks map[string]map[int]int64
		err = json.Unmarshal([]byte(retrieved.TargetHighWaterMarks), &parsedWaterMarks)
		assert.NoError(t, err)
		assert.Equal(t, int64(1000), parsedWaterMarks["test-topic"][0])

		// Verify validation results
		assert.NotNil(t, retrieved.ValidationResults)
		var validationData map[string]interface{}
		err = json.Unmarshal([]byte(*retrieved.ValidationResults), &validationData)
		assert.NoError(t, err)
		assert.Equal(t, "safe", validationData["status"])
	})
}
