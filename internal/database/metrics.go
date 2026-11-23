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

package database

import (
	"database/sql"
	"errors"
	"sync"
	"time"

	"github.com/jmoiron/sqlx"
)

type MetricsRecorder struct {
	mu   sync.Mutex
	last ReplicationMetric
}

func NewMetricsRecorder() *MetricsRecorder {
	return &MetricsRecorder{}
}

func counterDelta[T int | int64](current, last T) T {
	if current < last {
		return current
	}
	return current - last
}

func (r *MetricsRecorder) Insert(db *sqlx.DB, metric *ReplicationMetric) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	query := `INSERT INTO aggregated_metrics (
			  job_id,
			  timestamp,
			  messages_replicated_delta,
			  bytes_transferred_delta,
			  messages_consumed_delta,
			  bytes_consumed_delta,
			  avg_lag,
			  error_count_delta,
			  tombstone_count_delta
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`
	_, err := db.Exec(query, metric.JobID, time.Now(),
		counterDelta(metric.MessagesReplicated, r.last.MessagesReplicated),
		counterDelta(metric.BytesTransferred, r.last.BytesTransferred),
		counterDelta(metric.MessagesConsumed, r.last.MessagesConsumed),
		counterDelta(metric.BytesConsumed, r.last.BytesConsumed),
		metric.CurrentLag,
		counterDelta(metric.ErrorCount, r.last.ErrorCount),
		counterDelta(metric.TombstoneCount, r.last.TombstoneCount))
	if err != nil {
		return err
	}
	r.last = *metric
	return nil
}

// GetLatestMetrics retrieves the latest metrics for a given job.
func GetLatestMetrics(db *sqlx.DB, jobID string) (*ReplicationMetric, error) {
	var totals struct {
		MessagesReplicated int   `db:"messages_replicated"`
		BytesTransferred   int   `db:"bytes_transferred"`
		MessagesConsumed   int   `db:"messages_consumed"`
		BytesConsumed      int   `db:"bytes_consumed"`
		ErrorCount         int   `db:"error_count"`
		TombstoneCount     int64 `db:"tombstone_count"`
	}
	totalsQuery := `
        SELECT
            COALESCE(SUM(messages_replicated_delta), 0) as messages_replicated,
            COALESCE(SUM(bytes_transferred_delta), 0) as bytes_transferred,
            COALESCE(SUM(messages_consumed_delta), 0) as messages_consumed,
            COALESCE(SUM(bytes_consumed_delta), 0) as bytes_consumed,
            COALESCE(SUM(error_count_delta), 0) as error_count,
            COALESCE(SUM(tombstone_count_delta), 0) as tombstone_count
        FROM aggregated_metrics
        WHERE job_id = ?
    `
	err := db.Get(&totals, totalsQuery, jobID)
	if err != nil {
		return nil, err
	}

	var lastMetric struct {
		CurrentLag int       `db:"avg_lag"`
		Timestamp  time.Time `db:"timestamp"`
	}
	lastMetricQuery := "SELECT avg_lag, timestamp FROM aggregated_metrics WHERE job_id = ? ORDER BY timestamp DESC LIMIT 1"
	err = db.Get(&lastMetric, lastMetricQuery, jobID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}

	return &ReplicationMetric{
		JobID:              jobID,
		MessagesReplicated: totals.MessagesReplicated,
		BytesTransferred:   totals.BytesTransferred,
		MessagesConsumed:   totals.MessagesConsumed,
		BytesConsumed:      totals.BytesConsumed,
		ErrorCount:         totals.ErrorCount,
		TombstoneCount:     totals.TombstoneCount,
		CurrentLag:         lastMetric.CurrentLag,
		Timestamp:          lastMetric.Timestamp,
	}, nil
}

// GetHistoricalMetrics retrieves historical metrics for a given job within a time range.
func GetHistoricalMetrics(db *sqlx.DB, jobID string, start, end time.Time) ([]AggregatedMetric, error) {
	var metrics []AggregatedMetric
	err := db.Select(&metrics, "SELECT * FROM aggregated_metrics WHERE job_id = ? AND timestamp BETWEEN ? AND ? ORDER BY timestamp", jobID, start, end)
	return metrics, err
}

// GetAggregatedHistoricalMetrics retrieves aggregated historical metrics for a given job.
func GetAggregatedHistoricalMetrics(db *sqlx.DB, jobID string, periodDays int, granularity string) ([]AggregatedMetric, error) {
	var metrics []AggregatedMetric
	var groupBy string

	switch granularity {
	case "hourly":
		groupBy = "strftime('%Y-%m-%d %H:00:00', timestamp)"
	case "daily":
		groupBy = "strftime('%Y-%m-%d', timestamp)"
	default:
		groupBy = "strftime('%Y-%m-%d', timestamp)" // Default to daily
	}

	query := `
        SELECT
            ` + groupBy + ` as period,
            SUM(messages_replicated_delta) as avg_throughput,
            AVG(avg_lag) as avg_lag,
            SUM(error_count_delta) as total_errors
        FROM aggregated_metrics
        WHERE job_id = ? AND timestamp >= datetime('now', '-' || ? || ' days')
        GROUP BY period
        ORDER BY period ASC
    `

	err := db.Select(&metrics, query, jobID, periodDays)
	return metrics, err
}
