-- Copyright 2025 Alexander Alten (2pk03) and Scalytics
--
-- Licensed under the Apache License, Version 2.0 (the "License");
-- you may not use this file except in compliance with the License.
-- You may obtain a copy of the License at
--
--     http://www.apache.org/licenses/LICENSE-2.0
--
-- Unless required by applicable law or agreed to in writing, software
-- distributed under the License is distributed on an "AS IS" BASIS,
-- WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
-- See the License for the specific language governing permissions and
-- limitations under the License.

-- Kafka Clusters: Stores connection details for Kafka clusters
CREATE TABLE IF NOT EXISTS kafka_clusters (
    name TEXT PRIMARY KEY,
    uid TEXT NOT NULL DEFAULT '',
    provider TEXT NOT NULL,
    cluster_id TEXT NOT NULL DEFAULT '',
    brokers TEXT NOT NULL,
    security_config TEXT,
    api_key TEXT,
    api_secret TEXT,
    connection_string TEXT,
    status TEXT DEFAULT 'unknown',
    role TEXT NOT NULL DEFAULT 'other',
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS protection_state (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    enabled INTEGER NOT NULL DEFAULT 0,
    halted INTEGER NOT NULL DEFAULT 0,
    halt_reason TEXT,
    halted_at DATETIME,
    halted_by TEXT
);

-- Replication Jobs: Stores replication job definitions
CREATE TABLE IF NOT EXISTS replication_jobs (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    source_cluster_name TEXT NOT NULL,
    target_cluster_name TEXT NOT NULL,
    status TEXT NOT NULL CHECK(status IN ('active', 'paused', 'failed', 'running')),
    batch_size INTEGER NOT NULL DEFAULT 1000,
    parallelism INTEGER NOT NULL DEFAULT 4,
    compression TEXT NOT NULL DEFAULT 'none',
    preserve_partitions BOOLEAN NOT NULL DEFAULT TRUE,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (source_cluster_name) REFERENCES kafka_clusters(name),
    FOREIGN KEY (target_cluster_name) REFERENCES kafka_clusters(name)
);

-- Topic Mappings: Defines source to target topic mappings
CREATE TABLE IF NOT EXISTS topic_mappings (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    job_id TEXT NOT NULL,
    source_topic_pattern TEXT NOT NULL,
    target_topic_pattern TEXT NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    FOREIGN KEY (job_id) REFERENCES replication_jobs(id) ON DELETE CASCADE
);

-- Replication Metrics: Time-series metrics data
CREATE TABLE IF NOT EXISTS replication_metrics (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    job_id TEXT NOT NULL,
    messages_replicated INTEGER NOT NULL,
    bytes_transferred INTEGER NOT NULL,
    current_lag INTEGER NOT NULL,
    error_count INTEGER NOT NULL,
    timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (job_id) REFERENCES replication_jobs(id) ON DELETE CASCADE
);

-- AI Insights: Stores AI-generated insights
CREATE TABLE IF NOT EXISTS ai_insights (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    job_id TEXT,
    insight_type TEXT NOT NULL CHECK(insight_type IN ('anomaly', 'optimization', 'prediction', 'incident_report', 'recommendation', 'enhanced_analysis', 'log_analysis', 'incident_analysis', 'historical_trend')),
    severity_level TEXT NOT NULL,
    ai_model TEXT NOT NULL,
    recommendation TEXT NOT NULL,
    timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
    resolution_status TEXT NOT NULL DEFAULT 'new',
    response_time_ms INTEGER DEFAULT 0,
    accuracy_score REAL,
    user_feedback TEXT,
    resolved_at DATETIME,
    FOREIGN KEY (job_id) REFERENCES replication_jobs(id) ON DELETE SET NULL
);


-- Operational Events: Audit log of all operations
CREATE TABLE IF NOT EXISTS operational_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    event_type TEXT NOT NULL,
    initiator TEXT NOT NULL,
    details TEXT, -- JSON blob
    timestamp DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- Configuration: Stores runtime configuration overrides
CREATE TABLE IF NOT EXISTS configuration (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- Users: Stores user accounts for authentication
CREATE TABLE IF NOT EXISTS users (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    username TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    is_initial BOOLEAN NOT NULL DEFAULT FALSE,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- API Tokens: Stores authentication tokens for users
CREATE TABLE IF NOT EXISTS api_tokens (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL,
    token_hash TEXT NOT NULL UNIQUE,
    description TEXT,
    expires_at DATETIME,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);

-- Roles: Defines a set of permissions
CREATE TABLE IF NOT EXISTS roles (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL UNIQUE
);

-- Permissions: Defines a specific action that can be performed
CREATE TABLE IF NOT EXISTS permissions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL UNIQUE
);

-- User Roles: Maps users to roles
CREATE TABLE IF NOT EXISTS user_roles (
    user_id INTEGER NOT NULL,
    role_id INTEGER NOT NULL,
    PRIMARY KEY (user_id, role_id),
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
    FOREIGN KEY (role_id) REFERENCES roles(id) ON DELETE CASCADE
);

-- Role Permissions: Maps roles to permissions
CREATE TABLE IF NOT EXISTS role_permissions (
    role_id INTEGER NOT NULL,
    permission_id INTEGER NOT NULL,
    PRIMARY KEY (role_id, permission_id),
    FOREIGN KEY (role_id) REFERENCES roles(id) ON DELETE CASCADE,
    FOREIGN KEY (permission_id) REFERENCES permissions(id) ON DELETE CASCADE
);

-- Compliance Reports: Stores compliance and audit reports
CREATE TABLE IF NOT EXISTS compliance_reports (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    period TEXT NOT NULL CHECK(period IN ('daily', 'weekly', 'monthly')),
    start_date DATETIME NOT NULL,
    end_date DATETIME NOT NULL,
    generated_by INTEGER NOT NULL,
    generated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    report_data TEXT NOT NULL, -- JSON blob
    FOREIGN KEY (generated_by) REFERENCES users(id) ON DELETE CASCADE
);

-- Job Inventory Snapshots: Stores comprehensive cluster state when jobs start
CREATE TABLE IF NOT EXISTS job_inventory_snapshots (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    job_id TEXT NOT NULL,
    snapshot_type TEXT NOT NULL CHECK(snapshot_type IN ('startup', 'periodic', 'manual')),
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (job_id) REFERENCES replication_jobs(id) ON DELETE CASCADE
);

-- Cluster Inventory: Detailed cluster information per snapshot
CREATE TABLE IF NOT EXISTS cluster_inventory (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    snapshot_id INTEGER NOT NULL,
    cluster_type TEXT NOT NULL CHECK(cluster_type IN ('source', 'target')),
    cluster_name TEXT NOT NULL,
    provider TEXT NOT NULL,
    brokers TEXT NOT NULL,
    broker_count INTEGER NOT NULL,
    total_topics INTEGER NOT NULL,
    controller_id INTEGER,
    cluster_id TEXT,
    FOREIGN KEY (snapshot_id) REFERENCES job_inventory_snapshots(id) ON DELETE CASCADE
);

-- Topic Inventory: Topic details per cluster snapshot
CREATE TABLE IF NOT EXISTS topic_inventory (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    cluster_inventory_id INTEGER NOT NULL,
    topic_name TEXT NOT NULL,
    partition_count INTEGER NOT NULL,
    replication_factor INTEGER NOT NULL,
    is_internal BOOLEAN NOT NULL DEFAULT FALSE,
    compression_type TEXT NOT NULL DEFAULT 'none', -- none, gzip, snappy, lz4, zstd
    config_data TEXT, -- JSON blob of topic configurations
    FOREIGN KEY (cluster_inventory_id) REFERENCES cluster_inventory(id) ON DELETE CASCADE
);

-- Partition Inventory: Partition details per topic
CREATE TABLE IF NOT EXISTS partition_inventory (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    topic_inventory_id INTEGER NOT NULL,
    partition_id INTEGER NOT NULL,
    leader_id INTEGER,
    replica_ids TEXT, -- JSON array of replica broker IDs
    isr_ids TEXT, -- JSON array of in-sync replica broker IDs
    high_water_mark INTEGER NOT NULL DEFAULT 0,
    FOREIGN KEY (topic_inventory_id) REFERENCES topic_inventory(id) ON DELETE CASCADE
);

-- Consumer Group Inventory: Consumer group state per snapshot
CREATE TABLE IF NOT EXISTS consumer_group_inventory (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    snapshot_id INTEGER NOT NULL,
    group_id TEXT NOT NULL,
    group_state TEXT NOT NULL,
    protocol_type TEXT,
    protocol TEXT,
    member_count INTEGER NOT NULL DEFAULT 0,
    FOREIGN KEY (snapshot_id) REFERENCES job_inventory_snapshots(id) ON DELETE CASCADE
);

-- Consumer Group Offsets: Offset information per partition
CREATE TABLE IF NOT EXISTS consumer_group_offsets (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    consumer_group_inventory_id INTEGER NOT NULL,
    topic_name TEXT NOT NULL,
    partition_id INTEGER NOT NULL,
    current_offset INTEGER NOT NULL DEFAULT -1,
    high_water_mark INTEGER NOT NULL DEFAULT 0,
    lag INTEGER NOT NULL DEFAULT 0,
    FOREIGN KEY (consumer_group_inventory_id) REFERENCES consumer_group_inventory(id) ON DELETE CASCADE
);

-- Connection Inventory: Connection details and authentication info (masked)
CREATE TABLE IF NOT EXISTS connection_inventory (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    snapshot_id INTEGER NOT NULL,
    connection_type TEXT NOT NULL CHECK(connection_type IN ('source_consumer', 'target_producer')),
    provider TEXT NOT NULL,
    brokers TEXT NOT NULL,
    security_protocol TEXT,
    sasl_mechanism TEXT,
    api_key_prefix TEXT, -- Only first 4 characters for security
    connection_successful BOOLEAN NOT NULL,
    connection_time_ms INTEGER,
    error_message TEXT,
    FOREIGN KEY (snapshot_id) REFERENCES job_inventory_snapshots(id) ON DELETE CASCADE
);

-- Mirror Progress: Tracks replication progress per job and topic partition (current state only)
CREATE TABLE IF NOT EXISTS mirror_progress (
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
    FOREIGN KEY (job_id) REFERENCES replication_jobs(id) ON DELETE CASCADE,
    UNIQUE(job_id, source_topic, partition_id)
);

-- Mirror Progress History: Historical snapshots of replication progress for time-series analysis
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
);

-- Resume Points: Safe resume points for migration scenarios
CREATE TABLE IF NOT EXISTS resume_points (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    job_id TEXT NOT NULL,
    source_topic TEXT NOT NULL,
    target_topic TEXT NOT NULL,
    partition_id INTEGER NOT NULL,
    safe_resume_offset INTEGER NOT NULL,
    calculated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    validation_status TEXT NOT NULL DEFAULT 'pending' CHECK(validation_status IN ('pending', 'validated', 'invalid')),
    migration_checkpoint_id INTEGER,
    gap_detected BOOLEAN NOT NULL DEFAULT FALSE,
    gap_start_offset INTEGER,
    gap_end_offset INTEGER,
    FOREIGN KEY (job_id) REFERENCES replication_jobs(id) ON DELETE CASCADE,
    FOREIGN KEY (migration_checkpoint_id) REFERENCES migration_checkpoints(id) ON DELETE SET NULL,
    UNIQUE(job_id, source_topic, partition_id, calculated_at)
);

-- Migration Checkpoints: Snapshots before server migrations
CREATE TABLE IF NOT EXISTS migration_checkpoints (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    job_id TEXT NOT NULL,
    checkpoint_type TEXT NOT NULL CHECK(checkpoint_type IN ('pre_migration', 'post_migration', 'recovery')),
    source_consumer_group_offsets TEXT, -- JSON blob of consumer group positions
    target_high_water_marks TEXT, -- JSON blob of target cluster state
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    created_by TEXT NOT NULL DEFAULT 'system',
    migration_reason TEXT,
    validation_results TEXT, -- JSON blob of validation outcomes
    FOREIGN KEY (job_id) REFERENCES replication_jobs(id) ON DELETE CASCADE
);

-- Mirror State Analysis: Results of cross-cluster state analysis
CREATE TABLE IF NOT EXISTS mirror_state_analysis (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    job_id TEXT NOT NULL,
    analysis_type TEXT NOT NULL CHECK(analysis_type IN ('gap_detection', 'offset_comparison', 'consistency_check', 'resume_validation')),
    source_cluster_state TEXT, -- JSON blob of source cluster state
    target_cluster_state TEXT, -- JSON blob of target cluster state
    analysis_results TEXT, -- JSON blob of analysis findings
    recommendations TEXT, -- JSON blob of recommendations
    critical_issues_count INTEGER NOT NULL DEFAULT 0,
    warning_issues_count INTEGER NOT NULL DEFAULT 0,
    analyzed_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    analyzer_version TEXT NOT NULL DEFAULT '1.0',
    FOREIGN KEY (job_id) REFERENCES replication_jobs(id) ON DELETE CASCADE
);

-- Mirror Gaps: Detected gaps in replication
CREATE TABLE IF NOT EXISTS mirror_gaps (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    job_id TEXT NOT NULL,
    source_topic TEXT NOT NULL,
    target_topic TEXT NOT NULL,
    partition_id INTEGER NOT NULL,
    gap_start_offset INTEGER NOT NULL,
    gap_end_offset INTEGER NOT NULL,
    gap_size INTEGER NOT NULL,
    detected_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    gap_type TEXT NOT NULL CHECK(gap_type IN ('missing_messages', 'offset_mismatch', 'partial_replication')),
    resolution_status TEXT NOT NULL DEFAULT 'unresolved' CHECK(resolution_status IN ('unresolved', 'in_progress', 'resolved', 'ignored')),
    resolution_method TEXT,
    resolved_at DATETIME,
    FOREIGN KEY (job_id) REFERENCES replication_jobs(id) ON DELETE CASCADE
);
