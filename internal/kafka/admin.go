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

package kafka

import (
	"context"
	"errors"
	"fmt"
	"kaf-mirror/internal/config"
	"kaf-mirror/pkg/logger"

	"time"

	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kgo"
)

type ClusterInfo struct {
	ClusterID    string
	Topics       map[string]TopicInfo
	BrokerCount  int
	ControllerID int32
}

type TopicInfo struct {
	Name              string
	Partitions        int32
	ReplicationFactor int16
	Config            map[string]string
	PartitionInfo     []PartitionInfo
}

type PartitionInfo struct {
	ID       int32
	Leader   int32
	Replicas []int32
	ISR      []int32
}

type OffsetInfo struct {
	Topic         string
	Partition     int32
	Offset        int64
	HighWaterMark int64
	Lag           int64
}

type TopicDetails struct {
	Name              string `json:"name"`
	Partitions        int32  `json:"partitions"`
	ReplicationFactor int16  `json:"replication_factor"`
	CompressionType   string `json:"compression_type"`
}

type OffsetComparisonResult struct {
	ComparedAt        time.Time                         `json:"compared_at"`
	TopicComparisons  map[string]*TopicOffsetComparison `json:"topic_comparisons"`
	TotalGapsDetected int                               `json:"total_gaps_detected"`
	CriticalIssues    []string                          `json:"critical_issues"`
	Warnings          []string                          `json:"warnings"`
}

type TopicOffsetComparison struct {
	SourceTopic          string                      `json:"source_topic"`
	TargetTopic          string                      `json:"target_topic"`
	PartitionComparisons []PartitionOffsetComparison `json:"partition_comparisons"`
	GapsDetected         int                         `json:"gaps_detected"`
	TotalLag             int64                       `json:"total_lag"`
}

type PartitionOffsetComparison struct {
	PartitionID         int32 `json:"partition_id"`
	SourceOffset        int64 `json:"source_offset"`
	TargetOffset        int64 `json:"target_offset"`
	SourceHighWaterMark int64 `json:"source_high_water_mark"`
	TargetHighWaterMark int64 `json:"target_high_water_mark"`
	Gap                 int64 `json:"gap"`
	SafeResumeOffset    int64 `json:"safe_resume_offset"`
	HasGap              bool  `json:"has_gap"`
}

type AdminClient struct {
	client *kadm.Client
	raw    *kgo.Client
	cfg    config.ClusterConfig
}

func NewAdminClient(cfg config.ClusterConfig) (*AdminClient, error) {
	client, err := newAdminKgoClient(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create admin client: %w", err)
	}

	return &AdminClient{
		client: kadm.NewClient(client),
		raw:    client,
		cfg:    cfg,
	}, nil
}

func (a *AdminClient) KgoClientForTest() *kgo.Client {
	return a.raw
}

// TestConnection checks if a connection can be established to the cluster.
func TestConnection(ctx context.Context, cfg config.ClusterConfig) error {
	client, err := newAdminKgoClient(cfg)
	if err != nil {
		return fmt.Errorf("failed to create client for connection test: %w", err)
	}
	defer client.Close()

	if err := client.Ping(ctx); err != nil {
		return fmt.Errorf("failed to ping cluster: %w", err)
	}

	return nil
}

func (a *AdminClient) GetClusterInfo(ctx context.Context) (*ClusterInfo, error) {
	logger.Info("Retrieving cluster information for %s", a.cfg.Provider)

	brokers, err := a.client.ListBrokers(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list brokers: %w", err)
	}

	topicDetails, err := a.client.ListTopics(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list topics: %w", err)
	}

	clusterInfo := &ClusterInfo{
		Topics:      make(map[string]TopicInfo),
		BrokerCount: len(brokers),
	}

	clusterInfo.ControllerID = -1

	for topic, details := range topicDetails {
		if details.Err != nil {
			continue
		}
		topicInfo := TopicInfo{
			Name:          topic,
			Partitions:    int32(len(details.Partitions)),
			Config:        make(map[string]string),
			PartitionInfo: make([]PartitionInfo, 0, len(details.Partitions)),
		}

		if len(details.Partitions) > 0 {
			topicInfo.ReplicationFactor = int16(len(details.Partitions[0].Replicas))
		}

		for _, partition := range details.Partitions {
			partInfo := PartitionInfo{
				ID:       partition.Partition,
				Leader:   partition.Leader,
				Replicas: partition.Replicas,
				ISR:      partition.ISR,
			}
			topicInfo.PartitionInfo = append(topicInfo.PartitionInfo, partInfo)
		}

		clusterInfo.Topics[topic] = topicInfo
	}

	names := make([]string, 0, len(clusterInfo.Topics))
	for name := range clusterInfo.Topics {
		names = append(names, name)
	}
	configs, err := a.describeTopicConfigs(ctx, names)
	if err != nil {
		logger.Warn("Could not read all topic configs, compression is reported as NONE for those topics: %v", err)
	}
	for name, values := range configs {
		if info, ok := clusterInfo.Topics[name]; ok {
			info.Config = values
			clusterInfo.Topics[name] = info
		}
	}

	logger.Info("Retrieved cluster info: %d brokers, %d topics", clusterInfo.BrokerCount, len(clusterInfo.Topics))
	return clusterInfo, nil
}

func (a *AdminClient) GetConsumerGroupOffsets(ctx context.Context, groupID string, topics []string) (map[string][]OffsetInfo, error) {
	logger.Info("Retrieving consumer group offsets for group %s", groupID)

	offsets, err := a.client.FetchOffsetsForTopics(ctx, groupID, topics...)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch consumer group offsets: %w", err)
	}

	result := make(map[string][]OffsetInfo)

	for topic, topicOffsets := range offsets {
		for partition, offset := range topicOffsets {
			if offset.Err != nil {
				logger.Warn("Error getting offset for topic %s, partition %d: %v", topic, partition, offset.Err)
				continue
			}

			offsetInfo := OffsetInfo{
				Topic:         topic,
				Partition:     partition,
				Offset:        offset.At,
				HighWaterMark: offset.At,
				Lag:           0,
			}

			result[topic] = append(result[topic], offsetInfo)
		}
	}

	return result, nil
}

func (a *AdminClient) GetTopicHighWaterMarks(ctx context.Context, topics []string) (map[string][]OffsetInfo, error) {
	listed, err := a.client.ListEndOffsets(ctx, topics...)
	if err != nil {
		return nil, fmt.Errorf("failed to list end offsets: %w", err)
	}
	result := make(map[string][]OffsetInfo)
	listed.Each(func(o kadm.ListedOffset) {
		if o.Err != nil {
			return
		}
		result[o.Topic] = append(result[o.Topic], OffsetInfo{
			Topic:         o.Topic,
			Partition:     o.Partition,
			Offset:        o.Offset,
			HighWaterMark: o.Offset,
		})
	})
	return result, nil
}

func (a *AdminClient) ValidateTopicCompatibility(ctx context.Context, sourceInfo, targetInfo TopicInfo) error {
	logger.Info("Validating compatibility between source topic %s and target topic %s", sourceInfo.Name, targetInfo.Name)

	if sourceInfo.Partitions != targetInfo.Partitions {
		return fmt.Errorf("partition count mismatch: source has %d partitions, target has %d partitions",
			sourceInfo.Partitions, targetInfo.Partitions)
	}

	if sourceInfo.ReplicationFactor != targetInfo.ReplicationFactor {
		logger.Warn("Replication factor differs: source=%d, target=%d (this is acceptable)",
			sourceInfo.ReplicationFactor, targetInfo.ReplicationFactor)
	}

	logger.Info("Topics %s and %s are compatible for replication", sourceInfo.Name, targetInfo.Name)
	return nil
}

func (a *AdminClient) EnsureTopicExists(ctx context.Context, topicName string, partitions int32, replicationFactor int16) error {
	return ensureTopicExists(ctx, a.client, topicName, partitions, replicationFactor)
}

type TopicCreator interface {
	ListTopics(ctx context.Context, topics ...string) (kadm.TopicDetails, error)
	CreateTopics(ctx context.Context, partitions int32, replicationFactor int16, configs map[string]*string, topics ...string) (kadm.CreateTopicResponses, error)
}

func topicExists(details kadm.TopicDetails, topicName string) bool {
	detail, ok := details[topicName]
	return ok && detail.Err == nil
}

func topicVisible(details kadm.TopicDetails, topicName string) bool {
	detail, ok := details[topicName]
	return ok && detail.Err == nil && len(detail.Partitions) > 0
}

var topicMetadataSleep = sleepContext

func SetTopicMetadataSleepForTest(fn func(context.Context, time.Duration) error) func() {
	prev := topicMetadataSleep
	if fn == nil {
		fn = sleepContext
	}
	topicMetadataSleep = fn
	return func() { topicMetadataSleep = prev }
}

func waitForTopicVisible(ctx context.Context, client TopicCreator, topicName string) error {
	var lastErr error
	for attempt := 1; attempt <= 5; attempt++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		existing, err := client.ListTopics(ctx, topicName)
		if err != nil {
			lastErr = err
		} else if topicVisible(existing, topicName) {
			return nil
		} else {
			lastErr = fmt.Errorf("topic %s is not visible in metadata yet", topicName)
		}
		if attempt == 5 {
			break
		}
		if err := topicMetadataSleep(ctx, time.Duration(attempt)*time.Second); err != nil {
			return err
		}
	}
	return fmt.Errorf("topic %s was created but is not visible after 5 metadata checks: %w", topicName, lastErr)
}

func ensureTopicExists(ctx context.Context, client TopicCreator, topicName string, partitions int32, replicationFactor int16) error {
	logger.Info("Ensuring topic %s exists with %d partitions", topicName, partitions)

	existing, err := client.ListTopics(ctx, topicName)
	if err != nil {
		return fmt.Errorf("failed to check if topic exists: %w", err)
	}

	if topicExists(existing, topicName) {
		logger.Info("Topic %s already exists", topicName)
		return nil
	}

	results, err := client.CreateTopics(ctx, partitions, replicationFactor, nil, topicName)
	if err != nil {
		return fmt.Errorf("failed to create topic %s: %w", topicName, err)
	}

	for _, result := range results {
		if errors.Is(result.Err, kerr.TopicAlreadyExists) {
			logger.Info("Topic %s was created by someone else in the meantime", result.Topic)
			continue
		}
		if result.Err != nil {
			return fmt.Errorf("failed to create topic %s: %w", result.Topic, result.Err)
		}
		logger.Info("Successfully created topic %s with %d partitions", result.Topic, partitions)
	}

	return waitForTopicVisible(ctx, client, topicName)
}

func EnsureTopicExistsForTest(ctx context.Context, client TopicCreator, topicName string, partitions int32, replicationFactor int16) error {
	return ensureTopicExists(ctx, client, topicName, partitions, replicationFactor)
}

func (a *AdminClient) describeTopicConfigs(ctx context.Context, topicNames []string) (map[string]map[string]string, error) {
	configs := make(map[string]map[string]string)
	if len(topicNames) == 0 {
		return configs, nil
	}
	described, err := a.client.DescribeTopicConfigs(ctx, topicNames...)
	for _, topic := range described {
		if topic.Err != nil {
			logger.Warn("Could not describe config for topic %s: %v", topic.Name, topic.Err)
			continue
		}
		values := make(map[string]string, len(topic.Configs))
		for _, entry := range topic.Configs {
			if entry.Value != nil {
				values[entry.Key] = *entry.Value
			}
		}
		configs[topic.Name] = values
	}
	if err != nil {
		return configs, fmt.Errorf("failed to describe topic configs: %w", err)
	}
	return configs, nil
}

func (a *AdminClient) GetTopicDetails(ctx context.Context, topicNames ...string) ([]TopicDetails, error) {
	listedTopics, err := a.client.ListTopics(ctx, topicNames...)
	if err != nil {
		return nil, fmt.Errorf("failed to list topics: %w", err)
	}

	configs, err := a.describeTopicConfigs(ctx, topicNames)
	if err != nil {
		return nil, err
	}

	var details []TopicDetails
	for _, name := range topicNames {
		values, ok := configs[name]
		if !ok {
			continue
		}
		compressionType, ok := values["compression.type"]
		if !ok {
			compressionType = "unknown"
		}

		baseDetails, ok := listedTopics[name]
		if !ok || baseDetails.Err != nil || len(baseDetails.Partitions) == 0 {
			continue
		}

		details = append(details, TopicDetails{
			Name:              name,
			Partitions:        int32(len(baseDetails.Partitions)),
			ReplicationFactor: int16(len(baseDetails.Partitions[0].Replicas)),
			CompressionType:   compressionType,
		})
	}

	return details, nil
}

func (a *AdminClient) ListTopics(ctx context.Context, topics ...string) (map[string]TopicInfo, error) {
	topicDetails, err := a.client.ListTopics(ctx, topics...)
	if err != nil {
		return nil, err
	}

	result := make(map[string]TopicInfo)
	for topic, details := range topicDetails {
		if details.Err != nil {
			continue
		}
		topicInfo := TopicInfo{
			Name:       topic,
			Partitions: int32(len(details.Partitions)),
			Config:     make(map[string]string),
		}

		if len(details.Partitions) > 0 {
			topicInfo.ReplicationFactor = int16(len(details.Partitions[0].Replicas))
		}

		result[topic] = topicInfo
	}

	return result, nil
}

func (a *AdminClient) GetTopicLag(ctx context.Context, groupID, topic string) (int64, error) {
	return a.GetGroupLag(ctx, groupID, topic)
}

func (a *AdminClient) GetGroupLag(ctx context.Context, groupID string, topics ...string) (int64, error) {
	if len(topics) == 0 {
		return 0, nil
	}
	topics = append([]string(nil), topics...)
	committed, err := a.client.FetchOffsetsForTopics(ctx, groupID, topics...)
	if errors.Is(err, kerr.GroupIDNotFound) {
		committed, err = nil, nil
	}
	if err != nil {
		return 0, fmt.Errorf("failed to fetch committed offsets: %w", err)
	}
	ends, err := a.client.ListEndOffsets(ctx, topics...)
	if err == nil {
		err = ends.Error()
	}
	if err != nil {
		return 0, fmt.Errorf("failed to list end offsets: %w", err)
	}
	starts, err := a.client.ListStartOffsets(ctx, topics...)
	if err == nil {
		err = starts.Error()
	}
	if err != nil {
		return 0, fmt.Errorf("failed to list start offsets: %w", err)
	}
	var total int64
	for _, topic := range topics {
		total += TopicLag(topic, committed, starts, ends)
	}
	return total, nil
}

func TopicLag(topic string, committed kadm.OffsetResponses, starts, ends kadm.ListedOffsets) int64 {
	var total int64
	for partition, end := range ends[topic] {
		from := int64(0)
		if start, ok := starts.Lookup(topic, partition); ok {
			from = start.Offset
		}
		if commit, ok := committed.Lookup(topic, partition); ok && commit.Err == nil && commit.At >= from {
			from = commit.At
		}
		if lag := end.Offset - from; lag > 0 {
			total += lag
		}
	}
	return total
}

func (a *AdminClient) CompareClusterOffsets(ctx context.Context, sourceAdmin *AdminClient, topicMap map[string]string) (*OffsetComparisonResult, error) {
	logger.Info("Starting cross-cluster offset comparison")

	result := &OffsetComparisonResult{
		ComparedAt:        time.Now(),
		TopicComparisons:  make(map[string]*TopicOffsetComparison),
		TotalGapsDetected: 0,
		CriticalIssues:    make([]string, 0),
		Warnings:          make([]string, 0),
	}

	for sourceTopic, targetTopic := range topicMap {
		comparison, err := a.compareTopicOffsets(ctx, sourceAdmin, sourceTopic, targetTopic)
		if err != nil {
			result.CriticalIssues = append(result.CriticalIssues,
				fmt.Sprintf("Failed to compare %s -> %s: %v", sourceTopic, targetTopic, err))
			continue
		}

		result.TopicComparisons[sourceTopic] = comparison
		result.TotalGapsDetected += comparison.GapsDetected

		if comparison.TotalLag > 10000 {
			result.Warnings = append(result.Warnings,
				fmt.Sprintf("end offsets differ for %s -> %s by %d (not a cross-cluster record gap)",
					sourceTopic, targetTopic, comparison.TotalLag))
		}
	}

	logger.Info("Offset comparison completed: %d gaps detected across %d topics",
		result.TotalGapsDetected, len(result.TopicComparisons))
	return result, nil
}

func (a *AdminClient) compareTopicOffsets(ctx context.Context, sourceAdmin *AdminClient, sourceTopic, targetTopic string) (*TopicOffsetComparison, error) {
	sourceHWMs, err := sourceAdmin.GetTopicHighWaterMarks(ctx, []string{sourceTopic})
	if err != nil {
		return nil, fmt.Errorf("failed to get source high water marks: %w", err)
	}

	targetHWMs, err := a.GetTopicHighWaterMarks(ctx, []string{targetTopic})
	if err != nil {
		return nil, fmt.Errorf("failed to get target high water marks: %w", err)
	}

	sourceOffsets := sourceHWMs[sourceTopic]
	targetOffsets := targetHWMs[targetTopic]

	comparison := &TopicOffsetComparison{
		SourceTopic:          sourceTopic,
		TargetTopic:          targetTopic,
		PartitionComparisons: make([]PartitionOffsetComparison, 0),
		GapsDetected:         0,
		TotalLag:             0,
	}

	targetPartitionMap := make(map[int32]OffsetInfo)
	for _, targetOffset := range targetOffsets {
		targetPartitionMap[targetOffset.Partition] = targetOffset
	}

	for _, sourceOffset := range sourceOffsets {
		targetOffset, exists := targetPartitionMap[sourceOffset.Partition]
		if !exists {
			comparison.GapsDetected++
			comparison.PartitionComparisons = append(comparison.PartitionComparisons, PartitionOffsetComparison{
				PartitionID:         sourceOffset.Partition,
				SourceOffset:        sourceOffset.Offset,
				TargetOffset:        -1,
				SourceHighWaterMark: sourceOffset.HighWaterMark,
				TargetHighWaterMark: 0,
				Gap:                 sourceOffset.HighWaterMark,
				SafeResumeOffset:    0,
				HasGap:              true,
			})
			continue
		}

		gap := sourceOffset.HighWaterMark - targetOffset.HighWaterMark
		comparison.TotalLag += gap

		safeResumeOffset := targetOffset.HighWaterMark
		if safeResumeOffset < 0 {
			safeResumeOffset = 0
		}

		comparison.PartitionComparisons = append(comparison.PartitionComparisons, PartitionOffsetComparison{
			PartitionID:         sourceOffset.Partition,
			SourceOffset:        sourceOffset.Offset,
			TargetOffset:        targetOffset.Offset,
			SourceHighWaterMark: sourceOffset.HighWaterMark,
			TargetHighWaterMark: targetOffset.HighWaterMark,
			Gap:                 gap,
			SafeResumeOffset:    safeResumeOffset,
			HasGap:              false,
		})
	}

	return comparison, nil
}

func (a *AdminClient) AnalyzeMirrorState(ctx context.Context, sourceAdmin *AdminClient, jobID string, topicMap map[string]string, consumerGroup string) (*MirrorStateAnalysis, error) {
	logger.Info("Starting mirror state analysis for job %s", jobID)

	offsetComparison, err := a.CompareClusterOffsets(ctx, sourceAdmin, topicMap)
	if err != nil {
		return nil, fmt.Errorf("failed to compare cluster offsets: %w", err)
	}

	var sourceTopics []string
	for sourceTopic := range topicMap {
		sourceTopics = append(sourceTopics, sourceTopic)
	}

	consumerOffsets, err := sourceAdmin.GetConsumerGroupOffsets(ctx, consumerGroup, sourceTopics)
	if err != nil {
		logger.Warn("Failed to get consumer group offsets (this is normal for new groups): %v", err)
		consumerOffsets = make(map[string][]OffsetInfo)
	}

	resumePoints := make(map[string]map[int32]int64)
	for sourceTopic, _ := range topicMap {
		resumePoints[sourceTopic] = make(map[int32]int64)

		if comparison, exists := offsetComparison.TopicComparisons[sourceTopic]; exists {
			for _, partComparison := range comparison.PartitionComparisons {
				resumePoints[sourceTopic][partComparison.PartitionID] = partComparison.SafeResumeOffset
			}
		}
	}

	analysis := &MirrorStateAnalysis{
		JobID:               jobID,
		AnalysisType:        "gap_detection",
		OffsetComparison:    offsetComparison,
		ConsumerOffsets:     consumerOffsets,
		ResumePoints:        resumePoints,
		CriticalIssuesCount: len(offsetComparison.CriticalIssues),
		WarningIssuesCount:  len(offsetComparison.Warnings),
		AnalyzedAt:          time.Now(),
		AnalyzerVersion:     "1.0",
	}

	recommendations := make([]string, 0)

	if offsetComparison.TotalGapsDetected > 0 {
		recommendations = append(recommendations,
			fmt.Sprintf("Found %d replication gaps that need to be resolved before safe migration",
				offsetComparison.TotalGapsDetected))
	}

	if len(consumerOffsets) == 0 {
		recommendations = append(recommendations,
			"Consumer group has no committed offsets - replication will start from earliest/latest based on configuration")
	} else {
		recommendations = append(recommendations,
			"Consumer group offsets detected - can resume from last committed positions")
	}

	analysis.Recommendations = recommendations

	logger.Info("Mirror state analysis completed: %d critical issues, %d warnings, %d gaps",
		analysis.CriticalIssuesCount, analysis.WarningIssuesCount, offsetComparison.TotalGapsDetected)

	return analysis, nil
}

func (a *AdminClient) CalculateSafeResumePoints(ctx context.Context, sourceAdmin *AdminClient, jobID string, topicMap map[string]string, consumerGroup string) (map[string]map[int32]int64, error) {
	logger.Info("Calculating safe resume points for job %s", jobID)

	offsetComparison, err := a.CompareClusterOffsets(ctx, sourceAdmin, topicMap)
	if err != nil {
		return nil, fmt.Errorf("failed to compare cluster offsets: %w", err)
	}

	resumePoints := make(map[string]map[int32]int64)

	for sourceTopic, targetTopic := range topicMap {
		resumePoints[sourceTopic] = make(map[int32]int64)

		if comparison, exists := offsetComparison.TopicComparisons[sourceTopic]; exists {
			for _, partComparison := range comparison.PartitionComparisons {
				// Safe resume offset is the target high water mark to avoid duplication
				safeOffset := partComparison.TargetHighWaterMark

				// If target has no data, start from beginning
				if safeOffset < 0 {
					safeOffset = 0
				}

				resumePoints[sourceTopic][partComparison.PartitionID] = safeOffset

				logger.Info("Safe resume point for %s[%d] -> %s[%d]: offset %d",
					sourceTopic, partComparison.PartitionID,
					targetTopic, partComparison.PartitionID, safeOffset)
			}
		}
	}

	return resumePoints, nil
}

type MirrorStateAnalysis struct {
	JobID               string                     `json:"job_id"`
	AnalysisType        string                     `json:"analysis_type"`
	OffsetComparison    *OffsetComparisonResult    `json:"offset_comparison"`
	ConsumerOffsets     map[string][]OffsetInfo    `json:"consumer_offsets"`
	ResumePoints        map[string]map[int32]int64 `json:"resume_points"`
	CriticalIssuesCount int                        `json:"critical_issues_count"`
	WarningIssuesCount  int                        `json:"warning_issues_count"`
	AnalyzedAt          time.Time                  `json:"analyzed_at"`
	AnalyzerVersion     string                     `json:"analyzer_version"`
	Recommendations     []string                   `json:"recommendations"`
}

func (a *AdminClient) Close() {
	if a.client != nil {
		a.client.Close()
	}
}

type TopicHealth struct {
	Name                      string `json:"name"`
	Partitions                int    `json:"partitions"`
	ReplicationFactor         int    `json:"replication_factor"`
	UnderReplicatedPartitions int    `json:"under_replicated_partitions"`
	IsHealthy                 bool   `json:"is_healthy"`
}

func (a *AdminClient) CheckTopicHealth(ctx context.Context, topics []string) ([]TopicHealth, error) {
	var health []TopicHealth
	topicDetails, err := a.client.ListTopics(ctx, topics...)
	if err != nil {
		return nil, fmt.Errorf("failed to list topics: %w", err)
	}

	for _, topic := range topics {
		details, ok := topicDetails[topic]
		if !ok || details.Err != nil {
			health = append(health, TopicHealth{
				Name:      topic,
				IsHealthy: false,
			})
			continue
		}

		underReplicated := 0
		for _, p := range details.Partitions {
			if len(p.ISR) < len(p.Replicas) {
				underReplicated++
			}
		}

		health = append(health, TopicHealth{
			Name:                      topic,
			Partitions:                len(details.Partitions),
			ReplicationFactor:         len(details.Partitions[0].Replicas),
			UnderReplicatedPartitions: underReplicated,
			IsHealthy:                 underReplicated == 0,
		})
	}

	return health, nil
}
