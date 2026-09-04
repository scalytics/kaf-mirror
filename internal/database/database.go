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
	_ "embed"
	"fmt"
	"kaf-mirror/pkg/utils"
	"os"
	"path/filepath"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/jmoiron/sqlx"
)

//go:embed schema.sql
var Schema string

var configuredPath = "data/kaf-mirror.db"

func sqliteDSN(dbPath string) string {
	params := "_fk=1&_busy_timeout=5000"
	if dbPath == ":memory:" {
		return fmt.Sprintf("file:memdb_%d?mode=memory&cache=shared&%s", time.Now().UnixNano(), params)
	}
	return fmt.Sprintf("file:%s?%s&_journal_mode=WAL", dbPath, params)
}

// Open opens the database using the path last passed to InitDB.
func Open() (*sqlx.DB, error) {
	return InitDB(configuredPath)
}

// InitDB initializes the SQLite database and creates the schema.
func InitDB(dbPath string) (*sqlx.DB, error) {
	dsn := sqliteDSN(dbPath)
	if dbPath == ":memory:" {
		// in-memory
	} else {
		configuredPath = dbPath
		if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
			return nil, err
		}
	}

	db, err := sqlx.Connect("sqlite3", dsn)
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		return nil, err
	}
	if dbPath == ":memory:" {
		db.SetMaxOpenConns(1)
		db.SetMaxIdleConns(1)
	}

	_, err = db.Exec(string(Schema))
	if err != nil {
		return nil, err
	}

	if err := RunMigrations(db); err != nil {
		return nil, err
	}

	return db, nil
}

// RunMigrations applies necessary database schema migrations.
func RunMigrations(db *sqlx.DB) error {
	// Migration 1: Add 'historical_trend' to ai_insights table
	var tableInfo string
	err := db.Get(&tableInfo, "SELECT sql FROM sqlite_master WHERE type='table' AND name='ai_insights'")
	if err != nil {
		// If the table doesn't exist, the schema will create it, so no migration needed.
		return nil
	}

	// Check if the new type already exists in the CHECK constraint
	if !utils.ContainsString(tableInfo, []string{"historical_trend"}) {
		tx, err := db.Beginx()
		if err != nil {
			return err
		}

		// 1. Rename the existing table
		if _, err := tx.Exec("ALTER TABLE ai_insights RENAME TO ai_insights_old"); err != nil {
			tx.Rollback()
			return err
		}

		// 2. Create the new table with the updated schema
		newSchema := `
		CREATE TABLE ai_insights (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			job_id TEXT,
			insight_type TEXT NOT NULL CHECK(insight_type IN ('anomaly', 'optimization', 'prediction', 'incident_report', 'historical_trend')),
			severity_level TEXT NOT NULL,
			ai_model TEXT NOT NULL,
			recommendation TEXT NOT NULL,
			timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
			resolution_status TEXT NOT NULL DEFAULT 'new',
			FOREIGN KEY (job_id) REFERENCES replication_jobs(id) ON DELETE SET NULL
		);`
		if _, err := tx.Exec(newSchema); err != nil {
			tx.Rollback()
			return err
		}

		// 3. Copy data from the old table to the new table
		if _, err := tx.Exec("INSERT INTO ai_insights (id, job_id, insight_type, severity_level, ai_model, recommendation, timestamp, resolution_status) SELECT id, job_id, insight_type, severity_level, ai_model, recommendation, timestamp, resolution_status FROM ai_insights_old"); err != nil {
			tx.Rollback()
			return err
		}

		// 4. Drop the old table
		if _, err := tx.Exec("DROP TABLE ai_insights_old"); err != nil {
			tx.Rollback()
			return err
		}

		if err := tx.Commit(); err != nil {
			return err
		}
	}

	// Migration 2: Add AI metrics columns (response_time_ms, accuracy_score, user_feedback, resolved_at)
	err = addAIMetricsColumns(db)
	if err != nil {
		return err
	}

	// Migration 3: Add inventory permissions for operator+ roles
	err = addInventoryPermissions(db)
	if err != nil {
		return err
	}

	// Migration 4: Add mirror state tracking tables
	err = addMirrorStateTables(db)
	if err != nil {
		return err
	}

	// Migration 5: Add mirror state permissions
	err = addMirrorStatePermissions(db)
	if err != nil {
		return err
	}

	// Migration 6: Add failed_reason to replication_jobs
	err = addFailedReasonToJobs(db)
	if err != nil {
		return err
	}

	// Migration 7: Add aggregated metrics table
	err = addAggregatedMetricsTable(db)
	if err != nil {
		return err
	}

	// Migration 8: Add cluster identifier fields
	err = addClusterIdentifierFields(db)
	if err != nil {
		return err
	}

	// Migration 9: Add mirror progress history table
	err = addMirrorProgressHistoryTable(db)
	if err != nil {
		return err
	}

	// Migration 10: Add events view permission
	err = addEventsViewPermission(db)
	if err != nil {
		return err
	}

	return nil
}

// addMirrorProgressHistoryTable adds the mirror_progress_history table for historical data tracking
func addMirrorProgressHistoryTable(db *sqlx.DB) error {
	// Check if the mirror_progress_history table already exists
	var tableExists int
	err := db.Get(&tableExists, "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='mirror_progress_history'")
	if err != nil {
		return err
	}

	if tableExists == 0 {
		// Create the mirror_progress_history table
		_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS mirror_progress_history (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			job_id TEXT NOT NULL,
			source_topic TEXT NOT NULL,
			target_topic TEXT NOT NULL,
			partition_id INTEGER NOT NULL,
			source_offset INTEGER NOT NULL DEFAULT -1,
			target_offset INTEGER NOT NULL DEFAULT -1,
			source_high_water_mark INTEGER NOT NULL DEFAULT 0,
			target_high_water_mark INTEGER NOT NULL DEFAULT 0,
			last_replicated_offset INTEGER NOT NULL DEFAULT -1,
			replication_lag INTEGER NOT NULL DEFAULT 0,
			last_updated DATETIME DEFAULT CURRENT_TIMESTAMP,
			status TEXT NOT NULL DEFAULT 'active' CHECK(status IN ('active', 'paused', 'error', 'completed')),
			FOREIGN KEY (job_id) REFERENCES replication_jobs(id) ON DELETE CASCADE
		);`)
		if err != nil {
			return err
		}
	}

	return nil
}

func addEventsViewPermission(db *sqlx.DB) error {
	var permissionExists int
	err := db.Get(&permissionExists, "SELECT COUNT(*) FROM permissions WHERE name='events:view'")
	if err != nil {
		return err
	}
	if permissionExists > 0 {
		return nil
	}

	_, err = db.Exec("INSERT INTO permissions (name) VALUES ('events:view')")
	if err != nil {
		return err
	}

	roleNames := []string{"admin", "operator", "monitoring", "compliance"}
	for _, role := range roleNames {
		var roleID int
		if err := db.Get(&roleID, "SELECT id FROM roles WHERE name = ?", role); err != nil {
			continue
		}
		var permID int
		if err := db.Get(&permID, "SELECT id FROM permissions WHERE name='events:view'"); err != nil {
			return err
		}
		if _, err := db.Exec("INSERT OR IGNORE INTO role_permissions (role_id, permission_id) VALUES (?, ?)", roleID, permID); err != nil {
			return err
		}
	}

	return nil
}

// addClusterIdentifierFields adds the cluster_id and connection_string columns to the kafka_clusters table
func addClusterIdentifierFields(db *sqlx.DB) error {
	// Check if the cluster_id column already exists
	var columnExists int
	err := db.Get(&columnExists, "SELECT COUNT(*) FROM pragma_table_info('kafka_clusters') WHERE name='cluster_id'")
	if err != nil {
		return err
	}

	if columnExists == 0 {
		_, err = db.Exec("ALTER TABLE kafka_clusters ADD COLUMN cluster_id TEXT NOT NULL DEFAULT ''")
		if err != nil {
			return err
		}
	}

	// Check if the connection_string column already exists
	err = db.Get(&columnExists, "SELECT COUNT(*) FROM pragma_table_info('kafka_clusters') WHERE name='connection_string'")
	if err != nil {
		return err
	}

	if columnExists == 0 {
		_, err = db.Exec("ALTER TABLE kafka_clusters ADD COLUMN connection_string TEXT")
		if err != nil {
			return err
		}
	}

	return nil
}

// addFailedReasonToJobs adds the failed_reason column to the replication_jobs table
func addFailedReasonToJobs(db *sqlx.DB) error {
	// Check if the column already exists
	var columnExists int
	err := db.Get(&columnExists, "SELECT COUNT(*) FROM pragma_table_info('replication_jobs') WHERE name='failed_reason'")
	if err != nil {
		return err
	}

	if columnExists == 0 {
		_, err = db.Exec("ALTER TABLE replication_jobs ADD COLUMN failed_reason TEXT")
		if err != nil {
			return err
		}
	}

	return nil
}

// addAggregatedMetricsTable adds the aggregated_metrics table and migrates data
func addAggregatedMetricsTable(db *sqlx.DB) error {
	// Check if the aggregated_metrics table already exists
	var tableExists int
	err := db.Get(&tableExists, "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='aggregated_metrics'")
	if err != nil {
		return err
	}

	if tableExists == 0 {
		// Create the new aggregated_metrics table
		_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS aggregated_metrics (
			job_id TEXT NOT NULL,
			timestamp DATETIME NOT NULL,
			messages_replicated_delta INTEGER NOT NULL,
			bytes_transferred_delta INTEGER NOT NULL,
			messages_consumed_delta INTEGER NOT NULL DEFAULT 0,
			bytes_consumed_delta INTEGER NOT NULL DEFAULT 0,
			avg_lag INTEGER NOT NULL,
			error_count_delta INTEGER NOT NULL,
			PRIMARY KEY (job_id, timestamp)
		);`)
		if err != nil {
			return err
		}

		// Migrate the data from replication_metrics to aggregated_metrics
		_, err = db.Exec(`
		INSERT INTO aggregated_metrics (
			job_id,
			timestamp,
			messages_replicated_delta,
			bytes_transferred_delta,
			messages_consumed_delta,
			bytes_consumed_delta,
			avg_lag,
			error_count_delta
		)
		SELECT
			job_id,
			strftime('%Y-%m-%d %H:%M:00', timestamp),
			MAX(messages_replicated) - MIN(messages_replicated),
			MAX(bytes_transferred) - MIN(bytes_transferred),
			MAX(messages_replicated) - MIN(messages_replicated),
			MAX(bytes_transferred) - MIN(bytes_transferred),
			AVG(current_lag),
			MAX(error_count) - MIN(error_count)
		FROM
			replication_metrics
		GROUP BY
			job_id, strftime('%Y-%m-%d %H:%M:00', timestamp);
		`)
		if err != nil {
			return err
		}

		// Drop the old replication_metrics table
		_, err = db.Exec("DROP TABLE replication_metrics")
		if err != nil {
			return err
		}
	}

	if err := ensureAggregatedMetricsColumns(db); err != nil {
		return err
	}

	return nil
}

func ensureAggregatedMetricsColumns(db *sqlx.DB) error {
	existing := make(map[string]bool)
	rows, err := db.Queryx("PRAGMA table_info(aggregated_metrics)")
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var (
			cid        int
			name       string
			ctype      string
			notnull    int
			dfltValue  interface{}
			primaryKey int
		)
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dfltValue, &primaryKey); err != nil {
			return err
		}
		existing[name] = true
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if !existing["messages_consumed_delta"] {
		if _, err := db.Exec("ALTER TABLE aggregated_metrics ADD COLUMN messages_consumed_delta INTEGER NOT NULL DEFAULT 0"); err != nil {
			return err
		}
	}
	if !existing["bytes_consumed_delta"] {
		if _, err := db.Exec("ALTER TABLE aggregated_metrics ADD COLUMN bytes_consumed_delta INTEGER NOT NULL DEFAULT 0"); err != nil {
			return err
		}
	}
	return nil
}

// addMirrorStateTables adds the mirror state tracking tables if they don't exist
func addMirrorStateTables(db *sqlx.DB) error {
	// Check if mirror_progress table exists
	var tableExists int
	err := db.Get(&tableExists, "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='mirror_progress'")
	if err != nil {
		return err
	}

	if tableExists == 0 {
		// Create all mirror state tables
		mirrorStateTables := `
		-- Mirror progress tracking per job and partition
		CREATE TABLE IF NOT EXISTS mirror_progress (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			job_id TEXT NOT NULL,
			topic_name TEXT NOT NULL,
			partition_id INTEGER NOT NULL,
			source_offset INTEGER NOT NULL,
			target_offset INTEGER NOT NULL,
			last_updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			replication_lag INTEGER DEFAULT 0,
			is_synced BOOLEAN DEFAULT FALSE,
			UNIQUE(job_id, topic_name, partition_id)
		);

		-- Safe resume points for migration scenarios
		CREATE TABLE IF NOT EXISTS resume_points (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			job_id TEXT NOT NULL,
			topic_name TEXT NOT NULL,
			partition_id INTEGER NOT NULL,
			resume_offset INTEGER NOT NULL,
			source_high_water_mark INTEGER NOT NULL,
			target_high_water_mark INTEGER NOT NULL,
			calculated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			is_safe BOOLEAN DEFAULT TRUE,
			gap_size INTEGER DEFAULT 0,
			UNIQUE(job_id, topic_name, partition_id)
		);

		-- Migration checkpoint tracking
		CREATE TABLE IF NOT EXISTS migration_checkpoints (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			job_id TEXT NOT NULL,
			checkpoint_type TEXT NOT NULL, -- 'pre_migration', 'post_migration', 'validation'
			source_cluster_state TEXT NOT NULL, -- JSON of cluster state
			target_cluster_state TEXT NOT NULL, -- JSON of cluster state
			consumer_group_offsets TEXT NOT NULL, -- JSON of consumer group positions
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			validation_status TEXT DEFAULT 'pending', -- 'pending', 'valid', 'invalid'
			notes TEXT
		);

		-- Comprehensive mirror state analysis
		CREATE TABLE IF NOT EXISTS mirror_state_analysis (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			job_id TEXT NOT NULL,
			total_topics INTEGER NOT NULL,
			total_partitions INTEGER NOT NULL,
			synced_partitions INTEGER NOT NULL,
			lagging_partitions INTEGER NOT NULL,
			total_lag INTEGER NOT NULL,
			max_lag INTEGER NOT NULL,
			avg_lag REAL NOT NULL,
			analysis_timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
			is_migration_safe BOOLEAN DEFAULT FALSE,
			recommended_action TEXT
		);

		-- Mirror gap detection and tracking
		CREATE TABLE IF NOT EXISTS mirror_gaps (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			job_id TEXT NOT NULL,
			topic_name TEXT NOT NULL,
			partition_id INTEGER NOT NULL,
			gap_start_offset INTEGER NOT NULL,
			gap_end_offset INTEGER NOT NULL,
			gap_size INTEGER NOT NULL,
			detected_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			resolved_at DATETIME,
			gap_type TEXT NOT NULL, -- 'missing_data', 'offset_mismatch', 'replication_lag'
			priority TEXT DEFAULT 'medium' -- 'low', 'medium', 'high', 'critical'
		);

		-- Create indexes for performance
		CREATE INDEX IF NOT EXISTS idx_mirror_progress_job_id ON mirror_progress(job_id);
		CREATE INDEX IF NOT EXISTS idx_mirror_progress_topic ON mirror_progress(topic_name);
		CREATE INDEX IF NOT EXISTS idx_mirror_progress_updated_at ON mirror_progress(last_updated_at);

		CREATE INDEX IF NOT EXISTS idx_resume_points_job_id ON resume_points(job_id);
		CREATE INDEX IF NOT EXISTS idx_resume_points_topic ON resume_points(topic_name);
		CREATE INDEX IF NOT EXISTS idx_resume_points_calculated_at ON resume_points(calculated_at);

		CREATE INDEX IF NOT EXISTS idx_migration_checkpoints_job_id ON migration_checkpoints(job_id);
		CREATE INDEX IF NOT EXISTS idx_migration_checkpoints_type ON migration_checkpoints(checkpoint_type);
		CREATE INDEX IF NOT EXISTS idx_migration_checkpoints_created_at ON migration_checkpoints(created_at);

		CREATE INDEX IF NOT EXISTS idx_mirror_state_analysis_job_id ON mirror_state_analysis(job_id);
		CREATE INDEX IF NOT EXISTS idx_mirror_state_analysis_timestamp ON mirror_state_analysis(analysis_timestamp);

		CREATE INDEX IF NOT EXISTS idx_mirror_gaps_job_id ON mirror_gaps(job_id);
		CREATE INDEX IF NOT EXISTS idx_mirror_gaps_topic ON mirror_gaps(topic_name);
		CREATE INDEX IF NOT EXISTS idx_mirror_gaps_detected_at ON mirror_gaps(detected_at);
		`

		_, err = db.Exec(mirrorStateTables)
		if err != nil {
			return err
		}
	}

	return nil
}

// addMirrorStatePermissions adds mirror state permissions and assigns them to appropriate roles
func addMirrorStatePermissions(db *sqlx.DB) error {
	// Check if mirror_state:view permission already exists
	var permissionExists int
	err := db.Get(&permissionExists, "SELECT COUNT(*) FROM permissions WHERE name='mirror_state:view'")
	if err != nil {
		return err
	}

	if permissionExists == 0 {
		// Add mirror state permissions
		mirrorPermissions := []string{
			"mirror_state:view",
			"mirror_state:analyze",
			"mirror_state:manage",
			"mirror_state:migrate",
		}

		for _, perm := range mirrorPermissions {
			_, err = db.Exec("INSERT INTO permissions (name) VALUES (?)", perm)
			if err != nil {
				return err
			}
		}

		// Try to assign permissions to roles if they exist
		// Get role IDs (gracefully handle missing roles)
		var monitoringRoleID, operatorRoleID, adminRoleID int
		var hasMonitoring, hasOperator, hasAdmin bool

		err = db.Get(&monitoringRoleID, "SELECT id FROM roles WHERE name='monitoring'")
		hasMonitoring = (err == nil)

		err = db.Get(&operatorRoleID, "SELECT id FROM roles WHERE name='operator'")
		hasOperator = (err == nil)

		err = db.Get(&adminRoleID, "SELECT id FROM roles WHERE name='admin'")
		hasAdmin = (err == nil)

		// Only proceed with permission assignment if we have roles
		if hasMonitoring || hasOperator || hasAdmin {
			// Get permission IDs
			var viewPermID, analyzePermID, managePermID, migratePermID int
			err = db.Get(&viewPermID, "SELECT id FROM permissions WHERE name='mirror_state:view'")
			if err != nil {
				return err
			}
			err = db.Get(&analyzePermID, "SELECT id FROM permissions WHERE name='mirror_state:analyze'")
			if err != nil {
				return err
			}
			err = db.Get(&managePermID, "SELECT id FROM permissions WHERE name='mirror_state:manage'")
			if err != nil {
				return err
			}
			err = db.Get(&migratePermID, "SELECT id FROM permissions WHERE name='mirror_state:migrate'")
			if err != nil {
				return err
			}

			// Assign permissions to monitoring role (view only) if it exists
			if hasMonitoring {
				_, err = db.Exec("INSERT OR IGNORE INTO role_permissions (role_id, permission_id) VALUES (?, ?)", monitoringRoleID, viewPermID)
				if err != nil {
					return err
				}
			}

			// Assign permissions to operator role (view, analyze, manage) if it exists
			if hasOperator {
				operatorPermissions := []int{viewPermID, analyzePermID, managePermID}
				for _, permID := range operatorPermissions {
					_, err = db.Exec("INSERT OR IGNORE INTO role_permissions (role_id, permission_id) VALUES (?, ?)", operatorRoleID, permID)
					if err != nil {
						return err
					}
				}
			}

			// Assign all permissions to admin role if it exists
			if hasAdmin {
				adminPermissions := []int{viewPermID, analyzePermID, managePermID, migratePermID}
				for _, permID := range adminPermissions {
					_, err = db.Exec("INSERT OR IGNORE INTO role_permissions (role_id, permission_id) VALUES (?, ?)", adminRoleID, permID)
					if err != nil {
						return err
					}
				}
			}
		}
	}

	return nil
}

// addInventoryPermissions adds inventory permissions and assigns them to operator+ roles
func addInventoryPermissions(db *sqlx.DB) error {
	// Check if inventory:view permission already exists
	var permissionExists int
	err := db.Get(&permissionExists, "SELECT COUNT(*) FROM permissions WHERE name='inventory:view'")
	if err != nil {
		return err
	}

	if permissionExists == 0 {
		// Add inventory permissions
		_, err = db.Exec("INSERT INTO permissions (name) VALUES ('inventory:view')")
		if err != nil {
			return err
		}

		_, err = db.Exec("INSERT INTO permissions (name) VALUES ('inventory:create')")
		if err != nil {
			return err
		}

		// Get role and permission IDs
		var operatorRoleID, adminRoleID int
		var inventoryViewPermID, inventoryCreatePermID int

		// Get role IDs
		err = db.Get(&operatorRoleID, "SELECT id FROM roles WHERE name='operator'")
		if err == nil {
			err = db.Get(&adminRoleID, "SELECT id FROM roles WHERE name='admin'")
			if err == nil {
				// Get permission IDs
				err = db.Get(&inventoryViewPermID, "SELECT id FROM permissions WHERE name='inventory:view'")
				if err == nil {
					err = db.Get(&inventoryCreatePermID, "SELECT id FROM permissions WHERE name='inventory:create'")
					if err == nil {
						// Assign permissions to operator role
						_, err = db.Exec("INSERT OR IGNORE INTO role_permissions (role_id, permission_id) VALUES (?, ?)", operatorRoleID, inventoryViewPermID)
						if err != nil {
							return err
						}
						_, err = db.Exec("INSERT OR IGNORE INTO role_permissions (role_id, permission_id) VALUES (?, ?)", operatorRoleID, inventoryCreatePermID)
						if err != nil {
							return err
						}

						// Assign permissions to admin role
						_, err = db.Exec("INSERT OR IGNORE INTO role_permissions (role_id, permission_id) VALUES (?, ?)", adminRoleID, inventoryViewPermID)
						if err != nil {
							return err
						}
						_, err = db.Exec("INSERT OR IGNORE INTO role_permissions (role_id, permission_id) VALUES (?, ?)", adminRoleID, inventoryCreatePermID)
						if err != nil {
							return err
						}
					}
				}
			}
		}
	}

	return nil
}

// addAIMetricsColumns adds the new AI metrics columns if they don't exist
func addAIMetricsColumns(db *sqlx.DB) error {
	// Check if response_time_ms column exists
	var columnExists int
	err := db.Get(&columnExists, "SELECT COUNT(*) FROM pragma_table_info('ai_insights') WHERE name='response_time_ms'")
	if err != nil {
		return err
	}

	if columnExists == 0 {
		// Add new columns for AI metrics
		_, err = db.Exec("ALTER TABLE ai_insights ADD COLUMN response_time_ms INTEGER DEFAULT 0")
		if err != nil {
			return err
		}

		_, err = db.Exec("ALTER TABLE ai_insights ADD COLUMN accuracy_score REAL")
		if err != nil {
			return err
		}

		_, err = db.Exec("ALTER TABLE ai_insights ADD COLUMN user_feedback TEXT")
		if err != nil {
			return err
		}

		_, err = db.Exec("ALTER TABLE ai_insights ADD COLUMN resolved_at DATETIME")
		if err != nil {
			return err
		}
	}

	return nil
}
