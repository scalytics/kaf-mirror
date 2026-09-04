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
	"kaf-mirror/internal/database"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/assert"
)

func TestPruneOldData_UsesRetentionDays(t *testing.T) {
	t.Run("ShortRetention", func(t *testing.T) {
		db, err := database.InitDB(":memory:")
		assert.NoError(t, err)
		defer db.Close()

		oldTime := time.Now().AddDate(0, 0, -10)
		recentTime := time.Now().AddDate(0, 0, -2)

		err = insertAggregatedMetric(db, "job-a", oldTime)
		assert.NoError(t, err)
		err = insertAggregatedMetric(db, "job-a", recentTime)
		assert.NoError(t, err)
		err = insertOperationalEvent(db, "event", "user", oldTime)
		assert.NoError(t, err)
		err = insertOperationalEvent(db, "event", "user", recentTime)
		assert.NoError(t, err)

		err = database.PruneOldData(db, 7)
		assert.NoError(t, err)

		assert.Equal(t, 1, countRows(t, db, "aggregated_metrics"))
		assert.Equal(t, 1, countRows(t, db, "operational_events"))
	})

	t.Run("ClampToDefault", func(t *testing.T) {
		db, err := database.InitDB(":memory:")
		assert.NoError(t, err)
		defer db.Close()

		oldTime := time.Now().AddDate(0, 0, -40)
		withinDefault := time.Now().AddDate(0, 0, -20)

		err = insertAggregatedMetric(db, "job-b", oldTime)
		assert.NoError(t, err)
		err = insertAggregatedMetric(db, "job-b", withinDefault)
		assert.NoError(t, err)
		err = insertOperationalEvent(db, "event", "user", oldTime)
		assert.NoError(t, err)
		err = insertOperationalEvent(db, "event", "user", withinDefault)
		assert.NoError(t, err)

		err = database.PruneOldData(db, 45)
		assert.NoError(t, err)

		assert.Equal(t, 1, countRows(t, db, "aggregated_metrics"))
		assert.Equal(t, 1, countRows(t, db, "operational_events"))
	})
}

func TestPruneOldData_KeepsCurrentMirrorProgress(t *testing.T) {
	db, err := database.InitDB(":memory:")
	assert.NoError(t, err)
	defer db.Close()

	assert.NoError(t, database.CreateCluster(db, &database.KafkaCluster{Name: "a", Provider: "plain", Brokers: "localhost:9092"}))
	assert.NoError(t, database.CreateCluster(db, &database.KafkaCluster{Name: "b", Provider: "plain", Brokers: "localhost:9093"}))
	assert.NoError(t, database.CreateJob(db, &database.ReplicationJob{
		ID: "job-progress", Name: "p", SourceClusterName: "a", TargetClusterName: "b", Status: "paused",
	}))

	old := time.Now().AddDate(0, 0, -10)
	_, err = db.Exec(`INSERT INTO mirror_progress (job_id, source_topic, target_topic, partition_id, source_offset, target_offset, last_updated, status)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, "job-progress", "t", "t", 0, 1, 1, old, "active")
	assert.NoError(t, err)
	_, err = db.Exec(`INSERT INTO mirror_progress_history (job_id, source_topic, target_topic, partition_id, source_offset, target_offset, last_updated, status)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, "job-progress", "t", "t", 0, 1, 1, old, "active")
	assert.NoError(t, err)

	assert.NoError(t, database.PruneOldData(db, 7))
	assert.Equal(t, 1, countRows(t, db, "mirror_progress"))
	assert.Equal(t, 0, countRows(t, db, "mirror_progress_history"))
}

func insertAggregatedMetric(db *sqlx.DB, jobID string, ts time.Time) error {
	_, err := db.Exec(`INSERT INTO aggregated_metrics (job_id, timestamp, messages_replicated_delta, bytes_transferred_delta, avg_lag, error_count_delta)
		VALUES (?, ?, ?, ?, ?, ?)`, jobID, ts, 1, 1, 1, 0)
	return err
}

func insertOperationalEvent(db *sqlx.DB, eventType, initiator string, ts time.Time) error {
	_, err := db.Exec(`INSERT INTO operational_events (event_type, initiator, details, timestamp)
		VALUES (?, ?, ?, ?)`, eventType, initiator, "{}", ts)
	return err
}

func countRows(t *testing.T, db *sqlx.DB, table string) int {
	t.Helper()
	var count int
	err := db.Get(&count, "SELECT COUNT(*) FROM "+table)
	assert.NoError(t, err)
	return count
}
