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

import "time"

// KafkaCluster represents a Kafka cluster's connection details.
type KafkaCluster struct {
	Name             string    `db:"name" json:"name"`
	Provider         string    `db:"provider" json:"provider"`
	ClusterID        string    `db:"cluster_id" json:"cluster_id"`
	Brokers          string    `db:"brokers" json:"brokers"`
	SecurityConfig   string    `db:"security_config" json:"security_config"`
	APIKey           string    `db:"api_key" json:"api_key"`
	APISecret        string    `db:"api_secret" json:"api_secret"`
	ConnectionString *string   `db:"connection_string" json:"connection_string"`
	Status           string    `db:"status" json:"status"`
	Role             string    `db:"role" json:"role"`
	UpdatedAt        time.Time `db:"updated_at" json:"updated_at"`
}

// ReplicationJob represents a single replication job stored in the database.
type ReplicationJob struct {
	ID                 string    `db:"id" json:"id"`
	Name               string    `db:"name" json:"name"`
	SourceClusterName  string    `db:"source_cluster_name" json:"source_cluster_name"`
	TargetClusterName  string    `db:"target_cluster_name" json:"target_cluster_name"`
	Status             string    `db:"status" json:"status"`
	FailedReason       *string   `db:"failed_reason" json:"failed_reason"`
	BatchSize          int       `db:"batch_size" json:"batch_size"`
	Parallelism        int       `db:"parallelism" json:"-"`
	Compression        string    `db:"compression" json:"compression"`
	PreservePartitions bool      `db:"preserve_partitions" json:"-"`
	CreatedAt          time.Time `db:"created_at" json:"created_at"`
	UpdatedAt          time.Time `db:"updated_at" json:"updated_at"`
}

// TopicMapping represents a topic mapping rule within a job.
type TopicMapping struct {
	ID                 int    `db:"id" json:"id"`
	JobID              string `db:"job_id" json:"job_id"`
	SourceTopicPattern string `db:"source_topic_pattern" json:"source_topic_pattern"`
	TargetTopicPattern string `db:"target_topic_pattern" json:"target_topic_pattern"`
	Enabled            bool   `db:"enabled" json:"enabled"`
}

// ReplicationMetric represents a single data point of replication metrics.
type ReplicationMetric struct {
	ID                 int       `db:"id" json:"id"`
	JobID              string    `db:"job_id" json:"job_id"`
	MessagesReplicated int       `db:"messages_replicated" json:"messages_replicated"`
	BytesTransferred   int       `db:"bytes_transferred" json:"bytes_transferred"`
	MessagesConsumed   int       `db:"messages_consumed" json:"messages_consumed"`
	BytesConsumed      int       `db:"bytes_consumed" json:"bytes_consumed"`
	CurrentLag         int       `db:"current_lag" json:"current_lag"`
	ErrorCount         int       `db:"error_count" json:"error_count"`
	SourceStalled      bool      `db:"-" json:"source_stalled"`
	TargetStalled      bool      `db:"-" json:"target_stalled"`
	CriticalLag        bool      `db:"-" json:"critical_lag"`
	HighErrorRate      bool      `db:"-" json:"high_error_rate"`
	ErrorSpike         bool      `db:"-" json:"error_spike"`
	TombstoneCount     int64     `db:"-" json:"tombstone_count"`
	Timestamp          time.Time `db:"timestamp" json:"timestamp"`
}

// AggregatedMetric represents a summarized view of metrics over a period.
type AggregatedMetric struct {
	JobID                   string    `db:"job_id" json:"job_id"`
	Period                  string    `db:"period" json:"period"`
	AvgThroughput           float64   `db:"avg_throughput" json:"avg_throughput"`
	AvgLag                  float64   `db:"avg_lag" json:"avg_lag"`
	TotalErrors             int       `db:"total_errors" json:"total_errors"`
	MessagesReplicatedDelta int       `db:"messages_replicated_delta" json:"messages_replicated_delta"`
	BytesTransferredDelta   int       `db:"bytes_transferred_delta" json:"bytes_transferred_delta"`
	MessagesConsumedDelta   int       `db:"messages_consumed_delta" json:"messages_consumed_delta"`
	BytesConsumedDelta      int       `db:"bytes_consumed_delta" json:"bytes_consumed_delta"`
	ErrorCountDelta         int       `db:"error_count_delta" json:"error_count_delta"`
	Timestamp               time.Time `db:"timestamp" json:"timestamp"`
}

// AIInsight represents an AI-generated insight.
type AIInsight struct {
	ID               int        `db:"id" json:"id"`
	JobID            *string    `db:"job_id" json:"job_id"`
	InsightType      string     `db:"insight_type" json:"insight_type"`
	SeverityLevel    string     `db:"severity_level" json:"severity_level"`
	AIModel          string     `db:"ai_model" json:"ai_model"`
	Recommendation   string     `db:"recommendation" json:"recommendation"`
	Timestamp        time.Time  `db:"timestamp" json:"timestamp"`
	ResolutionStatus string     `db:"resolution_status" json:"resolution_status"`
	ResponseTimeMs   int        `db:"response_time_ms" json:"response_time_ms"`
	AccuracyScore    *float64   `db:"accuracy_score" json:"accuracy_score"`
	UserFeedback     *string    `db:"user_feedback" json:"user_feedback"`
	ResolvedAt       *time.Time `db:"resolved_at" json:"resolved_at"`
}

// AIMetrics represents aggregated AI performance metrics.
type AIMetrics struct {
	TotalInsights       int     `json:"total_insights"`
	AvgResponseTimeMs   float64 `json:"avg_response_time_ms"`
	AccuracyRate        float64 `json:"accuracy_rate"`
	AnomalyCount        int     `json:"anomaly_count"`
	RecommendationCount int     `json:"recommendation_count"`
}

// OperationalEvent represents an audit log entry.
type OperationalEvent struct {
	ID        int       `db:"id" json:"id"`
	EventType string    `db:"event_type" json:"event_type"`
	Initiator string    `db:"initiator" json:"initiator"`
	Details   string    `db:"details" json:"details"`
	Timestamp time.Time `db:"timestamp" json:"timestamp"`
}

// User represents a user account.
type User struct {
	ID           int       `db:"id" json:"id"`
	Username     string    `db:"username" json:"username"`
	PasswordHash string    `db:"password_hash" json:"-"`
	IsInitial    bool      `db:"is_initial" json:"is_initial"`
	CreatedAt    time.Time `db:"created_at" json:"created_at"`
}

// ApiToken represents an API token for a user.
type ApiToken struct {
	ID          int       `db:"id" json:"id"`
	UserID      int       `db:"user_id" json:"user_id"`
	TokenHash   string    `db:"token_hash" json:"-"`
	Description string    `db:"description" json:"description"`
	ExpiresAt   time.Time `db:"expires_at" json:"expires_at"`
	CreatedAt   time.Time `db:"created_at" json:"created_at"`
}

type JobInventorySnapshot struct {
	ID           int       `db:"id" json:"id"`
	JobID        string    `db:"job_id" json:"job_id"`
	SnapshotType string    `db:"snapshot_type" json:"snapshot_type"`
	CreatedAt    time.Time `db:"created_at" json:"created_at"`
}

type ClusterInventory struct {
	ID           int    `db:"id" json:"id"`
	SnapshotID   int    `db:"snapshot_id" json:"snapshot_id"`
	ClusterType  string `db:"cluster_type" json:"cluster_type"`
	ClusterName  string `db:"cluster_name" json:"cluster_name"`
	Provider     string `db:"provider" json:"provider"`
	Brokers      string `db:"brokers" json:"brokers"`
	BrokerCount  int    `db:"broker_count" json:"broker_count"`
	TotalTopics  int    `db:"total_topics" json:"total_topics"`
	ControllerID *int   `db:"controller_id" json:"controller_id,omitempty"`
	ClusterID    string `db:"cluster_id" json:"cluster_id,omitempty"`
}

type TopicInventory struct {
	ID                 int    `json:"id" db:"id"`
	ClusterInventoryID int    `json:"cluster_inventory_id" db:"cluster_inventory_id"`
	TopicName          string `json:"topic_name" db:"topic_name"`
	PartitionCount     int    `json:"partition_count" db:"partition_count"`
	ReplicationFactor  int    `json:"replication_factor" db:"replication_factor"`
	IsInternal         bool   `json:"is_internal" db:"is_internal"`
	CompressionType    string `json:"compression_type" db:"compression_type"`
	ConfigData         string `json:"config_data" db:"config_data"`
}

type PartitionInventory struct {
	ID               int    `db:"id" json:"id"`
	TopicInventoryID int    `db:"topic_inventory_id" json:"topic_inventory_id"`
	PartitionID      int    `db:"partition_id" json:"partition_id"`
	LeaderID         *int   `db:"leader_id" json:"leader_id,omitempty"`
	ReplicaIDs       string `db:"replica_ids" json:"replica_ids"`
	IsrIDs           string `db:"isr_ids" json:"isr_ids"`
	HighWaterMark    int64  `db:"high_water_mark" json:"high_water_mark"`
}

type ConsumerGroupInventory struct {
	ID           int    `db:"id" json:"id"`
	SnapshotID   int    `db:"snapshot_id" json:"snapshot_id"`
	GroupID      string `db:"group_id" json:"group_id"`
	GroupState   string `db:"group_state" json:"group_state"`
	ProtocolType string `db:"protocol_type" json:"protocol_type,omitempty"`
	Protocol     string `db:"protocol" json:"protocol,omitempty"`
	MemberCount  int    `db:"member_count" json:"member_count"`
}

type ConsumerGroupOffset struct {
	ID                       int    `db:"id" json:"id"`
	ConsumerGroupInventoryID int    `db:"consumer_group_inventory_id" json:"consumer_group_inventory_id"`
	TopicName                string `db:"topic_name" json:"topic_name"`
	PartitionID              int    `db:"partition_id" json:"partition_id"`
	CurrentOffset            int64  `db:"current_offset" json:"current_offset"`
	HighWaterMark            int64  `db:"high_water_mark" json:"high_water_mark"`
	Lag                      int64  `db:"lag" json:"lag"`
}

type ConnectionInventory struct {
	ID                   int    `db:"id" json:"id"`
	SnapshotID           int    `db:"snapshot_id" json:"snapshot_id"`
	ConnectionType       string `db:"connection_type" json:"connection_type"`
	Provider             string `db:"provider" json:"provider"`
	Brokers              string `db:"brokers" json:"brokers"`
	SecurityProtocol     string `db:"security_protocol" json:"security_protocol,omitempty"`
	SaslMechanism        string `db:"sasl_mechanism" json:"sasl_mechanism,omitempty"`
	APIKeyPrefix         string `db:"api_key_prefix" json:"api_key_prefix,omitempty"`
	ConnectionSuccessful bool   `db:"connection_successful" json:"connection_successful"`
	ConnectionTimeMs     *int   `db:"connection_time_ms" json:"connection_time_ms,omitempty"`
	ErrorMessage         string `db:"error_message" json:"error_message,omitempty"`
}

type InventoryData struct {
	Snapshot         JobInventorySnapshot     `json:"snapshot"`
	SourceCluster    *ClusterInventory        `json:"source_cluster,omitempty"`
	TargetCluster    *ClusterInventory        `json:"target_cluster,omitempty"`
	SourceTopics     []TopicInventory         `json:"source_topics,omitempty"`
	TargetTopics     []TopicInventory         `json:"target_topics,omitempty"`
	SourcePartitions []PartitionInventory     `json:"source_partitions,omitempty"`
	TargetPartitions []PartitionInventory     `json:"target_partitions,omitempty"`
	ConsumerGroups   []ConsumerGroupInventory `json:"consumer_groups,omitempty"`
	ConsumerOffsets  []ConsumerGroupOffset    `json:"consumer_offsets,omitempty"`
	Connections      []ConnectionInventory    `json:"connections,omitempty"`
}

// MirrorProgress tracks replication progress per job and topic partition
type MirrorProgress struct {
	ID                   int       `db:"id" json:"id"`
	JobID                string    `db:"job_id" json:"job_id"`
	SourceTopic          string    `db:"source_topic" json:"source_topic"`
	TargetTopic          string    `db:"target_topic" json:"target_topic"`
	PartitionID          int       `db:"partition_id" json:"partition_id"`
	SourceOffset         int64     `db:"source_offset" json:"source_offset"`
	TargetOffset         int64     `db:"target_offset" json:"target_offset"`
	SourceHighWaterMark  int64     `db:"source_high_water_mark" json:"source_high_water_mark"`
	TargetHighWaterMark  int64     `db:"target_high_water_mark" json:"target_high_water_mark"`
	LastReplicatedOffset int64     `db:"last_replicated_offset" json:"last_replicated_offset"`
	ReplicationLag       int64     `db:"replication_lag" json:"replication_lag"`
	LastUpdated          time.Time `db:"last_updated" json:"last_updated"`
	Status               string    `db:"status" json:"status"`
}

// ResumePoint represents safe resume points for migration scenarios
type ResumePoint struct {
	ID                    int       `db:"id" json:"id"`
	JobID                 string    `db:"job_id" json:"job_id"`
	SourceTopic           string    `db:"source_topic" json:"source_topic"`
	TargetTopic           string    `db:"target_topic" json:"target_topic"`
	PartitionID           int       `db:"partition_id" json:"partition_id"`
	SafeResumeOffset      int64     `db:"safe_resume_offset" json:"safe_resume_offset"`
	CalculatedAt          time.Time `db:"calculated_at" json:"calculated_at"`
	ValidationStatus      string    `db:"validation_status" json:"validation_status"`
	MigrationCheckpointID *int      `db:"migration_checkpoint_id" json:"migration_checkpoint_id,omitempty"`
	GapDetected           bool      `db:"gap_detected" json:"gap_detected"`
	GapStartOffset        *int64    `db:"gap_start_offset" json:"gap_start_offset,omitempty"`
	GapEndOffset          *int64    `db:"gap_end_offset" json:"gap_end_offset,omitempty"`
}

// MigrationCheckpoint represents snapshots before server migrations
type MigrationCheckpoint struct {
	ID                         int       `db:"id" json:"id"`
	JobID                      string    `db:"job_id" json:"job_id"`
	CheckpointType             string    `db:"checkpoint_type" json:"checkpoint_type"`
	SourceConsumerGroupOffsets string    `db:"source_consumer_group_offsets" json:"source_consumer_group_offsets"`
	TargetHighWaterMarks       string    `db:"target_high_water_marks" json:"target_high_water_marks"`
	CreatedAt                  time.Time `db:"created_at" json:"created_at"`
	CreatedBy                  string    `db:"created_by" json:"created_by"`
	MigrationReason            *string   `db:"migration_reason" json:"migration_reason,omitempty"`
	ValidationResults          *string   `db:"validation_results" json:"validation_results,omitempty"`
}

// MirrorStateAnalysis represents results of cross-cluster state analysis
type MirrorStateAnalysis struct {
	ID                  int       `db:"id" json:"id"`
	JobID               string    `db:"job_id" json:"job_id"`
	AnalysisType        string    `db:"analysis_type" json:"analysis_type"`
	SourceClusterState  string    `db:"source_cluster_state" json:"source_cluster_state"`
	TargetClusterState  string    `db:"target_cluster_state" json:"target_cluster_state"`
	AnalysisResults     string    `db:"analysis_results" json:"analysis_results"`
	Recommendations     string    `db:"recommendations" json:"recommendations"`
	CriticalIssuesCount int       `db:"critical_issues_count" json:"critical_issues_count"`
	WarningIssuesCount  int       `db:"warning_issues_count" json:"warning_issues_count"`
	AnalyzedAt          time.Time `db:"analyzed_at" json:"analyzed_at"`
	AnalyzerVersion     string    `db:"analyzer_version" json:"analyzer_version"`
}

// MirrorGap represents detected gaps in replication
type MirrorGap struct {
	ID               int        `db:"id" json:"id"`
	JobID            string     `db:"job_id" json:"job_id"`
	SourceTopic      string     `db:"source_topic" json:"source_topic"`
	TargetTopic      string     `db:"target_topic" json:"target_topic"`
	PartitionID      int        `db:"partition_id" json:"partition_id"`
	GapStartOffset   int64      `db:"gap_start_offset" json:"gap_start_offset"`
	GapEndOffset     int64      `db:"gap_end_offset" json:"gap_end_offset"`
	GapSize          int64      `db:"gap_size" json:"gap_size"`
	DetectedAt       time.Time  `db:"detected_at" json:"detected_at"`
	GapType          string     `db:"gap_type" json:"gap_type"`
	ResolutionStatus string     `db:"resolution_status" json:"resolution_status"`
	ResolutionMethod *string    `db:"resolution_method" json:"resolution_method,omitempty"`
	ResolvedAt       *time.Time `db:"resolved_at" json:"resolved_at,omitempty"`
}

// MirrorStateData represents comprehensive mirror state information
type MirrorStateData struct {
	JobID          string                `json:"job_id"`
	MirrorProgress []MirrorProgress      `json:"mirror_progress"`
	ResumePoints   []ResumePoint         `json:"resume_points"`
	MirrorGaps     []MirrorGap           `json:"mirror_gaps"`
	StateAnalysis  []MirrorStateAnalysis `json:"state_analysis"`
	LastCheckpoint *MigrationCheckpoint  `json:"last_checkpoint,omitempty"`
}
