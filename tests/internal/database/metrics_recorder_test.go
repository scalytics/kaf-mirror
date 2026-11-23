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
	"path/filepath"
	"testing"

	"kaf-mirror/internal/database"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func metricsJob(t *testing.T, db *sqlx.DB) string {
	t.Helper()
	for _, name := range []string{"a", "b"} {
		if _, err := database.GetCluster(db, name); err != nil {
			require.NoError(t, database.CreateCluster(db, &database.KafkaCluster{Name: name, Brokers: "localhost:9092", SecurityConfig: "{}"}))
		}
	}
	jobID := uuid.NewString()
	require.NoError(t, database.CreateJob(db, &database.ReplicationJob{ID: jobID, Name: "metrics-" + jobID, SourceClusterName: "a", TargetClusterName: "b", Status: "paused"}))
	return jobID
}

func counters(jobID string, n int, tombstones int64) *database.ReplicationMetric {
	return &database.ReplicationMetric{JobID: jobID, MessagesReplicated: n, BytesTransferred: n * 2, MessagesConsumed: n, BytesConsumed: n * 2, ErrorCount: n / 10, TombstoneCount: tombstones}
}

func TestMetricsRecorderAfterProcessRestart(t *testing.T) {
	db, err := database.InitDB(":memory:")
	require.NoError(t, err)
	defer db.Close()
	jobID := metricsJob(t, db)

	before := database.NewMetricsRecorder()
	require.NoError(t, before.Insert(db, counters(jobID, 100, 4)))
	require.NoError(t, before.Insert(db, counters(jobID, 150, 9)))

	idle := database.NewMetricsRecorder()
	for i := 0; i < 3; i++ {
		require.NoError(t, idle.Insert(db, counters(jobID, 0, 0)))
	}
	m, err := database.GetLatestMetrics(db, jobID)
	require.NoError(t, err)
	assert.Equal(t, 150, m.MessagesReplicated)
	assert.Equal(t, int64(9), m.TombstoneCount)

	busy := database.NewMetricsRecorder()
	for _, n := range []int{10, 10, 10, 30} {
		require.NoError(t, busy.Insert(db, counters(jobID, n, int64(n/10))))
	}
	m, err = database.GetLatestMetrics(db, jobID)
	require.NoError(t, err)
	assert.Equal(t, 180, m.MessagesReplicated)
	assert.Equal(t, 360, m.BytesTransferred)
	assert.Equal(t, 180, m.MessagesConsumed)
	assert.Equal(t, 360, m.BytesConsumed)
	assert.Equal(t, 18, m.ErrorCount)
	assert.Equal(t, int64(12), m.TombstoneCount)
}

func TestMetricsRecorderStoresZeroSamples(t *testing.T) {
	db, err := database.InitDB(":memory:")
	require.NoError(t, err)
	defer db.Close()
	jobID := metricsJob(t, db)

	m, err := database.GetLatestMetrics(db, jobID)
	require.NoError(t, err)
	assert.Equal(t, 0, m.MessagesReplicated)
	assert.True(t, m.Timestamp.IsZero())

	recorder := database.NewMetricsRecorder()
	require.NoError(t, recorder.Insert(db, counters(jobID, 0, 0)))
	require.NoError(t, recorder.Insert(db, counters(jobID, 0, 0)))
	m, err = database.GetLatestMetrics(db, jobID)
	require.NoError(t, err)
	assert.Equal(t, 0, m.MessagesReplicated)
	assert.Equal(t, int64(0), m.TombstoneCount)
	assert.False(t, m.Timestamp.IsZero())

	rows, err := database.GetHistoricalMetrics(db, jobID, m.Timestamp.AddDate(0, 0, -1), m.Timestamp.AddDate(0, 0, 1))
	require.NoError(t, err)
	assert.Len(t, rows, 2)
}

func TestTombstoneColumnMigrationOnAnExistingDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	db, err := database.InitDB(path)
	require.NoError(t, err)
	jobID := metricsJob(t, db)
	_, err = db.Exec("ALTER TABLE aggregated_metrics DROP COLUMN tombstone_count_delta")
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO aggregated_metrics (job_id, timestamp, messages_replicated_delta, bytes_transferred_delta, avg_lag, error_count_delta)
		VALUES (?, datetime('now'), 7, 70, 0, 0)`, jobID)
	require.NoError(t, err)
	require.NoError(t, db.Close())

	for i := 0; i < 2; i++ {
		db, err = database.InitDB(path)
		require.NoError(t, err)
		m, err := database.GetLatestMetrics(db, jobID)
		require.NoError(t, err)
		assert.Equal(t, 7, m.MessagesReplicated)
		assert.Equal(t, int64(0), m.TombstoneCount)
		require.NoError(t, db.Close())
	}

	db, err = database.InitDB(path)
	require.NoError(t, err)
	defer db.Close()
	require.NoError(t, database.NewMetricsRecorder().Insert(db, counters(jobID, 3, 2)))
	m, err := database.GetLatestMetrics(db, jobID)
	require.NoError(t, err)
	assert.Equal(t, 10, m.MessagesReplicated)
	assert.Equal(t, int64(2), m.TombstoneCount)
}
