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
	"encoding/json"
	"fmt"
	"kaf-mirror/internal/config"
	"kaf-mirror/internal/database"
	"kaf-mirror/pkg/logger"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/twmb/franz-go/pkg/kgo"
)

// KafMirror is the interface for the replication orchestrator.
type KafMirror interface {
	Start(jobID string, metricsCallback func(database.ReplicationMetric), onPanic func(jobID string, reason string))
	Stop()
	GetConsumer() *Consumer
	GetProducer() *Producer
}

// KafMirrorImpl orchestrates the replication from a source to a target Kafka cluster.
type KafMirrorImpl struct {
	Consumer          *Consumer
	Producer          *Producer
	sourceCfg         config.ClusterConfig
	targetCfg         config.ClusterConfig
	mappings          []config.TopicMapping
	topicMap          map[string]string
	regexMaps         []regexMapping
	targetPartitions  map[string]int32
	mapMu             sync.RWMutex
	wg                sync.WaitGroup
	cancelFunc        context.CancelFunc
	jobID             string
	onPanic           func(jobID string, reason string)
	discoveryInterval time.Duration

	// Incident tracking to prevent spam logging
	incidentStates map[string]bool
	incidentMutex  sync.RWMutex
}

type regexMapping struct {
	regex  *regexp.Regexp
	target string
}

// NewKafMirror creates a new replication orchestrator.
func NewKafMirror(cfg *config.Config) (KafMirror, error) {
	topics, topicMap, regexMaps, err := resolveTopicMappings(cfg)
	if err != nil {
		return nil, err
	}

	// Validate cluster compatibility and sync state before starting
	targetPartitions, err := validateAndSyncClusters(cfg, topics, topicMap)
	if err != nil {
		return nil, fmt.Errorf("cluster validation failed: %w", err)
	}

	// Use job-specific consumer group to avoid conflicts between jobs
	consumerGroup := fmt.Sprintf("kaf-mirror-job-%s", cfg.Replication.JobID)
	consumer, err := NewConsumer(cfg.Clusters["source"], consumerGroup, cfg.Replication, cfg.Replication.JobID, topics...)
	if err != nil {
		return nil, err
	}

	producer, err := NewProducer(cfg.Clusters["target"], cfg.Replication, cfg.Replication.JobID)
	if err != nil {
		consumer.Close()
		return nil, err
	}

	return &KafMirrorImpl{
		Consumer:          consumer,
		Producer:          producer,
		sourceCfg:         cfg.Clusters["source"],
		targetCfg:         cfg.Clusters["target"],
		mappings:          cfg.Topics,
		topicMap:          topicMap,
		regexMaps:         regexMaps,
		targetPartitions:  targetPartitions,
		discoveryInterval: discoveryInterval(cfg),
		incidentStates:    make(map[string]bool),
	}, nil
}

// Start begins the replication process.
func (r *KafMirrorImpl) Start(jobID string, metricsCallback func(database.ReplicationMetric), onPanic func(jobID string, reason string)) {
	logger.Info("[Job %s] Starting replication process", jobID)
	logger.Info("[Job %s] Topic mappings: %v", jobID, r.topicMap)

	ctx, cancel := context.WithCancel(context.Background())
	r.cancelFunc = cancel
	r.jobID = jobID
	r.onPanic = onPanic
	r.wg.Add(2)

	go func() {
		defer r.wg.Done()
		defer func() {
			if rec := recover(); rec != nil {
				reason := fmt.Sprintf("consumer panic: %v", rec)
				logger.Error("[Job %s] %s", jobID, reason)
				onPanic(jobID, reason)
			}
		}()
		logger.Info("[Job %s] Starting consumer goroutine", jobID)
		r.Consumer.Consume(ctx, func(record *kgo.Record) error {
			logger.Debug("[Job %s] Received record from topic %s, partition %d, offset %d", jobID, record.Topic, record.Partition, record.Offset)
			return r.handleRecord(record)
		})
		logger.Info("[Job %s] Consumer goroutine ended", jobID)
	}()

	go func() {
		defer r.wg.Done()
		defer func() {
			if rec := recover(); rec != nil {
				reason := fmt.Sprintf("metrics panic: %v", rec)
				logger.Error("[Job %s] %s", jobID, reason)
				onPanic(jobID, reason)
			}
		}()
		logger.Info("[Job %s] Starting metrics collection goroutine", jobID)
		r.collectMetrics(ctx, jobID, metricsCallback, onPanic)
		logger.Info("[Job %s] Metrics collection goroutine ended", jobID)
	}()

	if len(r.regexMaps) > 0 && r.discoveryInterval > 0 {
		r.wg.Add(1)
		go func() {
			defer r.wg.Done()
			r.discoverTopicsLoop(ctx)
		}()
	}

	logger.Info("[Job %s] Both goroutines started successfully", jobID)
}

// Stop gracefully shuts down the kaf-mirror.
func (r *KafMirrorImpl) Stop() {
	if r.cancelFunc != nil {
		r.cancelFunc()
	}
	r.wg.Wait()
	r.Consumer.Close()
	r.Producer.Close()
}

// GetConsumer returns the underlying consumer.
func (r *KafMirrorImpl) GetConsumer() *Consumer {
	return r.Consumer
}

// GetProducer returns the underlying producer.
func (r *KafMirrorImpl) GetProducer() *Producer {
	return r.Producer
}

// HandleRecordForTest exposes record handling for tests.
func (r *KafMirrorImpl) HandleRecordForTest(record *kgo.Record) error {
	return r.handleRecord(record)
}

func (r *KafMirrorImpl) AddRegexMapForTest(pattern, target string) error {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return err
	}
	r.regexMaps = append(r.regexMaps, regexMapping{regex: re, target: target})
	return nil
}

func (r *KafMirrorImpl) handleRecord(record *kgo.Record) error {
	r.mapMu.RLock()
	targetTopic, ok := r.topicMap[string(record.Topic)]
	r.mapMu.RUnlock()
	if !ok {
		for _, rm := range r.regexMaps {
			if rm.regex.MatchString(string(record.Topic)) {
				targetTopic = rm.regex.ReplaceAllString(string(record.Topic), rm.target)
				break
			}
		}
	}

	if targetTopic == "" {
		logger.Warn("No mapping found for topic: %s", record.Topic)
		return nil
	}

	// Analyze message size for compression recommendations
	valueSize := len(record.Value)
	keySize := len(record.Key)
	totalSize := valueSize + keySize

	// Log message size analysis for AI recommendations (periodic sampling)
	if totalSize > 0 {
		// Sample every 100th message or messages > 2KB for compression analysis
		if totalSize > 2048 || record.Offset%100 == 0 {
			logger.InfoAI("message", "size", string(record.Topic),
				"Message analysis: topic=%s, partition=%d, total_size=%d bytes, value_size=%d bytes, key_size=%d bytes - potential compression candidate",
				record.Topic, record.Partition, totalSize, valueSize, keySize)
		}

		// AI recommendation triggers
		if totalSize > 5120 { // > 5KB
			logger.InfoAI("compression", "recommendation", string(record.Topic),
				"Large message detected: %d bytes on topic %s - consider enabling lz4 or snappy compression for better performance",
				totalSize, record.Topic)
		} else if totalSize > 2048 { // > 2KB
			logger.InfoAI("compression", "candidate", string(record.Topic),
				"Medium message detected: %d bytes on topic %s - snappy compression could reduce storage overhead",
				totalSize, record.Topic)
		}
	}

	// Create a new record for the target topic
	outRecord := &kgo.Record{
		Topic:   targetTopic,
		Value:   record.Value,
		Key:     record.Key,
		Headers: record.Headers,
	}
	r.mapMu.RLock()
	partitionCount, hasPartitions := r.targetPartitions[targetTopic]
	r.mapMu.RUnlock()
	if hasPartitions && partitionCount > 0 {
		if record.Partition < partitionCount {
			outRecord.Partition = record.Partition
		} else {
			logger.Warn("Source partition %d exceeds target partitions %d for topic %s", record.Partition, partitionCount, targetTopic)
		}
	}

	if err := r.Producer.ProduceSync(context.Background(), outRecord); err != nil {
		logger.Error("Failed to produce record to topic %s: %v", outRecord.Topic, err)
		return err
	}
	logger.Debug("Replicated record to topic %s, partition %d, offset %d", outRecord.Topic, outRecord.Partition, outRecord.Offset)
	return nil
}

func (r *KafMirrorImpl) collectMetrics(ctx context.Context, jobID string, callback func(database.ReplicationMetric), onPanic func(jobID string, reason string)) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	logger.Info("[Job %s] Metrics collection loop started", jobID)

	// Counter for periodic logging (every 5th tick = ~50 seconds)
	logCounter := 0

	for {
		select {
		case <-ticker.C:
			// Get current metrics from consumer and producer
			consumerMetrics := r.Consumer.GetMetrics()
			producerMetrics := r.Producer.GetMetrics()

			// Use producer acked totals to avoid counting failed sends
			totalMessages := producerMetrics.RecordsProduced
			totalBytes := producerMetrics.BytesProduced
			totalConsumed := consumerMetrics.RecordsProcessed
			totalConsumedBytes := consumerMetrics.BytesProcessed
			totalErrors := producerMetrics.ErrorCount
			currentLag := consumerMetrics.ConsumerLag

			// Detect performance disasters and incidents
			logCounter++

			// Disaster detection thresholds
			criticalLag := currentLag > 200
			highErrorRate := totalErrors > 0 && totalMessages > 0 && (float64(totalErrors)/float64(totalMessages)) > 0.1
			sourceStalled := logCounter > 6 && totalConsumed == 0
			targetStalled := logCounter > 6 && totalConsumed > 0 && totalMessages == 0
			errorSpike := totalErrors > 0

			// Performance milestones (positive events)
			isFirstProduced := totalMessages == 1 && logCounter <= 2
			isSignificantProduced := totalMessages > 0 && totalMessages%100 == 0
			isFirstConsumed := totalConsumed == 1 && logCounter <= 2
			isSignificantConsumed := totalConsumed > 0 && totalConsumed%100 == 0

			// Check for producer failure threshold
			if producerMetrics.ConsecutiveErrors > 100 {
				reason := fmt.Sprintf("producer has failed %d consecutive times", producerMetrics.ConsecutiveErrors)
				logger.Error("[Job %s] %s", jobID, reason)
				onPanic(jobID, reason) // Use onPanic to trigger a shutdown
				return
			}

			// Disaster logging with incident state tracking to prevent spam
			r.incidentMutex.Lock()

			if criticalLag {
				if !r.incidentStates["critical_lag"] {
					logger.ErrorAI("disaster", "performance", jobID, "Critical lag detected: Messages=%d, Bytes=%d, Lag=%d, Errors=%d",
						totalMessages, totalBytes, currentLag, totalErrors)
					r.incidentStates["critical_lag"] = true
				}
			} else {
				r.incidentStates["critical_lag"] = false
			}

			if highErrorRate {
				if !r.incidentStates["high_error_rate"] {
					logger.ErrorAI("incident", "escalation", jobID, "High error rate detected: Messages=%d, Errors=%d, Rate=%.2f%%",
						totalMessages, totalErrors, (float64(totalErrors)/float64(totalMessages))*100)
					r.incidentStates["high_error_rate"] = true
				}
			} else {
				r.incidentStates["high_error_rate"] = false
			}

			if sourceStalled {
				if !r.incidentStates["source_stalled"] {
					logger.WarnAI("incident", "start", jobID, "Source stalled: No messages consumed in 60+ seconds")
					r.incidentStates["source_stalled"] = true
				}
			} else if r.incidentStates["source_stalled"] {
				logger.InfoAI("incident", "resolved", jobID, "Source throughput resumed: Consumed=%d, Bytes=%d",
					totalConsumed, totalConsumedBytes)
				r.incidentStates["source_stalled"] = false
			}

			if targetStalled {
				if !r.incidentStates["target_stalled"] {
					logger.WarnAI("incident", "start", jobID, "Target stalled: Consumed=%d, Produced=%d in 60+ seconds",
						totalConsumed, totalMessages)
					r.incidentStates["target_stalled"] = true
				}
			} else if r.incidentStates["target_stalled"] {
				logger.InfoAI("incident", "resolved", jobID, "Target throughput resumed: Produced=%d, Bytes=%d",
					totalMessages, totalBytes)
				r.incidentStates["target_stalled"] = false
			}

			if errorSpike && logCounter%3 == 0 {
				if !r.incidentStates["error_spike"] {
					logger.ErrorAI("metrics", "errors", jobID, "Error spike detected: Messages=%d, Bytes=%d, Lag=%d, Errors=%d",
						totalMessages, totalBytes, currentLag, totalErrors)
					r.incidentStates["error_spike"] = true
				}
			} else {
				r.incidentStates["error_spike"] = false
			}

			r.incidentMutex.Unlock()

			// Milestone logging (reduced frequency for normal operations)
			if isFirstConsumed {
				logger.InfoAI("metrics", "milestone", jobID, "First message consumed: Consumed=%d, Bytes=%d, Lag=%d",
					totalConsumed, totalConsumedBytes, currentLag)
			} else if isSignificantConsumed {
				logger.InfoAI("metrics", "milestone", jobID, "Consumption milestone: Consumed=%d, Bytes=%d, Lag=%d",
					totalConsumed, totalConsumedBytes, currentLag)
			}

			if isFirstProduced {
				logger.InfoAI("metrics", "milestone", jobID, "First message produced: Produced=%d, Bytes=%d, Lag=%d",
					totalMessages, totalBytes, currentLag)
			} else if isSignificantProduced {
				logger.InfoAI("metrics", "milestone", jobID, "Production milestone: Produced=%d, Bytes=%d, Lag=%d",
					totalMessages, totalBytes, currentLag)
			}

			metric := database.ReplicationMetric{
				JobID:              jobID,
				MessagesReplicated: int(totalMessages),      // Total messages replicated (acked)
				BytesTransferred:   int(totalBytes),         // Total bytes transferred (acked)
				MessagesConsumed:   int(totalConsumed),      // Total messages consumed
				BytesConsumed:      int(totalConsumedBytes), // Total bytes consumed
				CurrentLag:         int(currentLag),         // Current consumer lag
				ErrorCount:         int(totalErrors),        // Total errors
				SourceStalled:      sourceStalled,
				TargetStalled:      targetStalled,
				CriticalLag:        criticalLag,
				HighErrorRate:      highErrorRate,
				ErrorSpike:         errorSpike,
				Timestamp:          time.Now(),
			}

			callback(metric)
		case <-ctx.Done():
			logger.Info("[Job %s] Metrics collection cancelled", jobID)
			return
		}
	}
}

func resolveTopicMappings(cfg *config.Config) ([]string, map[string]string, []regexMapping, error) {
	topics := make([]string, 0)
	topicMap := make(map[string]string)
	regexMaps := make([]regexMapping, 0)

	for _, m := range cfg.Topics {
		if !m.Enabled {
			continue
		}
		targetPattern := m.Target
		if targetPattern == "" {
			targetPattern = m.Source
		}
		if isRegex(m.Source) {
			re, err := regexp.Compile(m.Source)
			if err != nil {
				return nil, nil, nil, fmt.Errorf("invalid regex pattern %q: %w", m.Source, err)
			}
			if targetPattern == "" {
				targetPattern = "$0"
			}
			regexMaps = append(regexMaps, regexMapping{regex: re, target: targetPattern})
			continue
		}
		topics = append(topics, m.Source)
		topicMap[m.Source] = targetPattern
	}

	if len(regexMaps) == 0 {
		return topics, topicMap, regexMaps, nil
	}

	admin, err := adminClientFactory(cfg.Clusters["source"])
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to create source admin client: %w", err)
	}
	defer admin.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	sourceInfo, err := admin.GetClusterInfo(ctx)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to list source topics for regex mapping: %w", err)
	}

	targetToSource := make(map[string]string)
	for sourceTopic, targetTopic := range topicMap {
		targetToSource[targetTopic] = sourceTopic
	}

	topicsSeen := make(map[string]bool)
	for _, topic := range topics {
		topicsSeen[topic] = true
	}

	for _, rm := range regexMaps {
		matched := false
		for topicName := range sourceInfo.Topics {
			if !rm.regex.MatchString(topicName) {
				continue
			}
			matched = true
			targetTopic := rm.regex.ReplaceAllString(topicName, rm.target)
			if targetTopic == "" {
				return nil, nil, nil, fmt.Errorf("regex mapping for %s produced empty target topic", topicName)
			}
			if existingTarget, ok := topicMap[topicName]; ok && existingTarget != targetTopic {
				return nil, nil, nil, fmt.Errorf("topic %s matches multiple targets: %s and %s", topicName, existingTarget, targetTopic)
			}
			if existingSource, ok := targetToSource[targetTopic]; ok && existingSource != topicName {
				return nil, nil, nil, fmt.Errorf("target topic %s is mapped from multiple sources: %s and %s", targetTopic, existingSource, topicName)
			}
			topicMap[topicName] = targetTopic
			targetToSource[targetTopic] = topicName
			if !topicsSeen[topicName] {
				topics = append(topics, topicName)
				topicsSeen[topicName] = true
			}
		}
		if !matched {
			logger.Warn("Regex mapping %s did not match any source topics", rm.regex.String())
		}
	}

	return topics, topicMap, regexMaps, nil
}

// validateAndSyncClusters validates cluster compatibility and syncs state before replication
func validateAndSyncClusters(cfg *config.Config, topics []string, topicMap map[string]string) (map[string]int32, error) {
	logger.Info("Starting cluster validation and state synchronization")

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Create admin clients for both clusters
	sourceAdmin, err := adminClientFactory(cfg.Clusters["source"])
	if err != nil {
		return nil, fmt.Errorf("failed to create source admin client: %w", err)
	}
	defer sourceAdmin.Close()

	targetAdmin, err := adminClientFactory(cfg.Clusters["target"])
	if err != nil {
		return nil, fmt.Errorf("failed to create target admin client: %w", err)
	}
	defer targetAdmin.Close()

	// Get cluster information
	sourceInfo, err := sourceAdmin.GetClusterInfo(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get source cluster info: %w", err)
	}

	targetInfo, err := targetAdmin.GetClusterInfo(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get target cluster info: %w", err)
	}

	logger.InfoAI("cluster", "validation", "", "Source cluster: %d brokers, %d topics", sourceInfo.BrokerCount, len(sourceInfo.Topics))
	logger.InfoAI("cluster", "validation", "", "Target cluster: %d brokers, %d topics", targetInfo.BrokerCount, len(targetInfo.Topics))

	// Validate and sync each topic mapping
	targetPartitions := make(map[string]int32)
	for sourceTopic, targetTopic := range topicMap {
		logger.InfoAI("topic", "validation", "", "Validating topic mapping: %s -> %s", sourceTopic, targetTopic)

		// Check if source topic exists
		sourceTopicInfo, sourceExists := sourceInfo.Topics[sourceTopic]
		if !sourceExists {
			return nil, fmt.Errorf("source topic %s does not exist", sourceTopic)
		}

		// Ensure target topic exists with correct partitions
		replicationFactor := clampReplicationFactor(sourceTopicInfo.ReplicationFactor, targetInfo.BrokerCount, sourceTopic)
		err = ensureTopicWithRetry(ctx, targetAdmin, targetTopic, sourceTopicInfo.Partitions, replicationFactor)
		if err != nil {
			return nil, fmt.Errorf("failed to ensure target topic %s exists: %w", targetTopic, err)
		}

		// Re-fetch target info after potential topic creation
		targetInfo, err = targetAdmin.GetClusterInfo(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to refresh target cluster info: %w", err)
		}

		// Validate topic compatibility
		targetTopicInfo, targetExists := targetInfo.Topics[targetTopic]
		if !targetExists {
			return nil, fmt.Errorf("target topic %s does not exist after creation attempt", targetTopic)
		}

		err = targetAdmin.ValidateTopicCompatibility(ctx, sourceTopicInfo, targetTopicInfo)
		if err != nil {
			return nil, fmt.Errorf("topic compatibility validation failed: %w", err)
		}
		targetPartitions[targetTopic] = targetTopicInfo.Partitions

		// Detect and log compression settings
		sourceCompression := detectTopicCompression(&sourceTopicInfo)
		targetCompression := detectTopicCompression(&targetTopicInfo)

		logger.Info("Topic %s: %d partitions, replication factor %d, source compression: %s",
			sourceTopic, sourceTopicInfo.Partitions, sourceTopicInfo.ReplicationFactor, sourceCompression)
		logger.Info("Target topic %s: compression: %s", targetTopic, targetCompression)

		// Log compression compatibility
		if sourceCompression != targetCompression {
			if targetCompression == "NONE" && sourceCompression != "NONE" {
				logger.WarnAI("compression", "mismatch", cfg.Replication.JobID,
					"Source topic '%s' uses %s compression but target '%s' has no compression - consider enabling %s on target for consistency",
					sourceTopic, sourceCompression, targetTopic, sourceCompression)
			} else if sourceCompression == "NONE" && targetCompression != "NONE" {
				logger.InfoAI("compression", "enhancement", cfg.Replication.JobID,
					"Target topic '%s' uses %s compression while source '%s' has none - this may improve storage efficiency",
					targetTopic, targetCompression, sourceTopic)
			} else if sourceCompression != "NONE" && targetCompression != "NONE" {
				logger.InfoAI("compression", "different", cfg.Replication.JobID,
					"Compression differs: source '%s' uses %s, target '%s' uses %s - performance may vary",
					sourceTopic, sourceCompression, targetTopic, targetCompression)
			}
		} else {
			logger.InfoAI("compression", "matched", cfg.Replication.JobID,
				"Compression settings matched: both using %s", sourceCompression)
		}

		for _, partInfo := range sourceTopicInfo.PartitionInfo {
			logger.Debug("Partition %d: Leader=%d, ISR=%v, Replicas=%v",
				partInfo.ID, partInfo.Leader, partInfo.ISR, partInfo.Replicas)
		}
	}

	// Check current consumer group offsets using job-specific group
	groupID := fmt.Sprintf("kaf-mirror-job-%s", cfg.Replication.JobID)
	logger.Info("Checking consumer group offsets for group: %s", groupID)

	sourceOffsets, err := sourceAdmin.GetConsumerGroupOffsets(ctx, groupID, topics)
	if err != nil {
		logger.Warn("Failed to get source consumer group offsets (this is normal for new groups): %v", err)
	} else {
		for _, offsets := range sourceOffsets {
			for _, offset := range offsets {
				logger.Info("Source consumer offset - Topic: %s, Partition: %d, Offset: %d, Lag: %d",
					offset.Topic, offset.Partition, offset.Offset, offset.Lag)
			}
		}
	}

	// Get high water marks for source topics
	sourceHighWaterMarks, err := sourceAdmin.GetTopicHighWaterMarks(ctx, topics)
	if err != nil {
		logger.Warn("Failed to get source high water marks: %v", err)
	} else {
		logger.Info("Source cluster high water marks:")
		for _, offsets := range sourceHighWaterMarks {
			for _, offset := range offsets {
				logger.Info("Topic: %s, Partition: %d, High Water Mark: %d",
					offset.Topic, offset.Partition, offset.HighWaterMark)
			}
		}
	}

	// Get high water marks for target topics
	var targetTopics []string
	for _, targetTopic := range topicMap {
		targetTopics = append(targetTopics, targetTopic)
	}

	targetHighWaterMarks, err := targetAdmin.GetTopicHighWaterMarks(ctx, targetTopics)
	if err != nil {
		logger.Warn("Failed to get target high water marks: %v", err)
	} else {
		logger.Info("Target cluster high water marks:")
		for _, offsets := range targetHighWaterMarks {
			for _, offset := range offsets {
				logger.Info("Topic: %s, Partition: %d, High Water Mark: %d",
					offset.Topic, offset.Partition, offset.HighWaterMark)
			}
		}
	}

	// Capture inventory snapshot for this job start
	err = CaptureJobInventory(cfg.Replication.JobID, cfg, sourceInfo, targetInfo, topics, topicMap)
	if err != nil {
		logger.Warn("Failed to capture job inventory: %v", err)
	}

	logger.Info("Cluster validation and state synchronization completed successfully")
	return targetPartitions, nil
}

func (r *KafMirrorImpl) discoverTopicsLoop(ctx context.Context) {
	ticker := time.NewTicker(r.discoveryInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := r.discoverAndSyncTopics(ctx); err != nil {
				logger.Error("[Job %s] Topic discovery failed: %v", r.jobID, err)
				if r.onPanic != nil {
					r.onPanic(r.jobID, err.Error())
				}
				return
			}
		case <-ctx.Done():
			return
		}
	}
}

func (r *KafMirrorImpl) discoverAndSyncTopics(ctx context.Context) error {
	sourceAdmin, err := adminClientFactory(r.sourceCfg)
	if err != nil {
		return fmt.Errorf("failed to create source admin client: %w", err)
	}
	defer sourceAdmin.Close()

	targetAdmin, err := adminClientFactory(r.targetCfg)
	if err != nil {
		return fmt.Errorf("failed to create target admin client: %w", err)
	}
	defer targetAdmin.Close()

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	sourceInfo, err := sourceAdmin.GetClusterInfo(ctx)
	if err != nil {
		return fmt.Errorf("failed to get source cluster info: %w", err)
	}
	targetInfo, err := targetAdmin.GetClusterInfo(ctx)
	if err != nil {
		return fmt.Errorf("failed to get target cluster info: %w", err)
	}

	for _, rm := range r.regexMaps {
		for sourceTopic, sourceTopicInfo := range sourceInfo.Topics {
			if !rm.regex.MatchString(sourceTopic) {
				continue
			}
			r.mapMu.RLock()
			_, exists := r.topicMap[sourceTopic]
			r.mapMu.RUnlock()
			if exists {
				continue
			}

			targetTopic := rm.regex.ReplaceAllString(sourceTopic, rm.target)
			if targetTopic == "" {
				return fmt.Errorf("regex mapping for %s produced empty target topic", sourceTopic)
			}
			r.mapMu.RLock()
			for existingSource, existingTarget := range r.topicMap {
				if existingTarget == targetTopic && existingSource != sourceTopic {
					r.mapMu.RUnlock()
					return fmt.Errorf("target topic %s is mapped from multiple sources: %s and %s", targetTopic, existingSource, sourceTopic)
				}
			}
			r.mapMu.RUnlock()

			replicationFactor := clampReplicationFactor(sourceTopicInfo.ReplicationFactor, targetInfo.BrokerCount, sourceTopic)
			if err := ensureTopicWithRetry(ctx, targetAdmin, targetTopic, sourceTopicInfo.Partitions, replicationFactor); err != nil {
				return fmt.Errorf("failed to ensure target topic %s exists after retries: %w", targetTopic, err)
			}

			r.mapMu.Lock()
			r.topicMap[sourceTopic] = targetTopic
			if r.targetPartitions == nil {
				r.targetPartitions = make(map[string]int32)
			}
			r.targetPartitions[targetTopic] = sourceTopicInfo.Partitions
			r.mapMu.Unlock()

			r.Consumer.AddTopics(sourceTopic)
			logger.Info("[Job %s] Discovered new topic mapping %s -> %s", r.jobID, sourceTopic, targetTopic)
		}
	}

	return nil
}

func ensureTopicWithRetry(ctx context.Context, admin AdminClientAPI, topicName string, partitions int32, replicationFactor int16) error {
	var lastErr error
	for attempt := 1; attempt <= 5; attempt++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err := admin.EnsureTopicExists(ctx, topicName, partitions, replicationFactor); err != nil {
			lastErr = err
			logger.Warn("Failed to create target topic %s (attempt %d/5): %v", topicName, attempt, err)
			time.Sleep(time.Duration(attempt) * time.Second)
			continue
		}
		return nil
	}
	return fmt.Errorf("topic %s creation failed after 5 attempts: %w", topicName, lastErr)
}

func clampReplicationFactor(sourceRF int16, targetBrokerCount int, sourceTopic string) int16 {
	if sourceRF <= 0 {
		sourceRF = 1
	}
	if targetBrokerCount <= 0 {
		return sourceRF
	}
	if int(sourceRF) > targetBrokerCount {
		logger.Warn("Source topic %s replication factor %d exceeds target broker count %d; using %d", sourceTopic, sourceRF, targetBrokerCount, targetBrokerCount)
		return int16(targetBrokerCount)
	}
	return sourceRF
}

func discoveryInterval(cfg *config.Config) time.Duration {
	interval, err := time.ParseDuration(cfg.Replication.TopicDiscoveryInterval)
	if err != nil {
		return 5 * time.Minute
	}
	return interval
}

// CaptureJobInventory captures comprehensive inventory data when a job starts
func CaptureJobInventory(jobID string, cfg *config.Config, sourceInfo, targetInfo *ClusterInfo, topics []string, topicMap map[string]string) error {
	db, err := database.Open()
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer db.Close()

	// Create inventory snapshot
	snapshotID, err := database.CreateInventorySnapshot(db, jobID, "startup")
	if err != nil {
		return fmt.Errorf("failed to create inventory snapshot: %w", err)
	}

	// Capture source cluster inventory
	sourceClusterID, err := captureClusterInventory(db, snapshotID, "source", cfg.Clusters["source"], sourceInfo)
	if err != nil {
		return fmt.Errorf("failed to capture source cluster inventory: %w", err)
	}

	// Capture target cluster inventory
	targetClusterID, err := captureClusterInventory(db, snapshotID, "target", cfg.Clusters["target"], targetInfo)
	if err != nil {
		return fmt.Errorf("failed to capture target cluster inventory: %w", err)
	}

	// Capture topic inventories for both clusters
	err = captureTopicInventories(db, sourceClusterID, sourceInfo)
	if err != nil {
		return fmt.Errorf("failed to capture source topic inventories: %w", err)
	}

	err = captureTopicInventories(db, targetClusterID, targetInfo)
	if err != nil {
		return fmt.Errorf("failed to capture target topic inventories: %w", err)
	}

	// Capture consumer group inventory
	groupID := fmt.Sprintf("kaf-mirror-job-%s", jobID)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	sourceAdmin, err := NewAdminClient(cfg.Clusters["source"])
	if err != nil {
		logger.Warn("Failed to create admin client for consumer group inventory: %v", err)
	} else {
		defer sourceAdmin.Close()
		err = captureConsumerGroupInventory(db, snapshotID, sourceAdmin, groupID, topics, ctx)
		if err != nil {
			logger.Warn("Failed to capture consumer group inventory: %v", err)
		}
	}

	// Capture connection inventories
	err = captureConnectionInventory(db, snapshotID, cfg.Clusters["source"], "source_consumer")
	if err != nil {
		logger.Warn("Failed to capture source connection inventory: %v", err)
	}

	err = captureConnectionInventory(db, snapshotID, cfg.Clusters["target"], "target_producer")
	if err != nil {
		logger.Warn("Failed to capture target connection inventory: %v", err)
	}

	logger.Info("Captured comprehensive job inventory for job %s (snapshot ID: %d)", jobID, snapshotID)
	return nil
}

func captureClusterInventory(db *sqlx.DB, snapshotID int, clusterType string, clusterCfg config.ClusterConfig, clusterInfo *ClusterInfo) (int, error) {
	cluster := database.ClusterInventory{
		SnapshotID:  snapshotID,
		ClusterType: clusterType,
		ClusterName: fmt.Sprintf("%s-%s", clusterType, clusterCfg.Provider),
		Provider:    clusterCfg.Provider,
		Brokers:     clusterCfg.Brokers,
		BrokerCount: clusterInfo.BrokerCount,
		TotalTopics: len(clusterInfo.Topics),
		ClusterID:   clusterInfo.ClusterID,
	}

	if clusterInfo.ControllerID >= 0 {
		controllerID := int(clusterInfo.ControllerID)
		cluster.ControllerID = &controllerID
	}

	clusterID, err := database.InsertClusterInventory(db, cluster)
	if err != nil {
		return 0, err
	}

	logger.Info("Captured %s cluster inventory: %d brokers, %d topics", clusterType, clusterInfo.BrokerCount, len(clusterInfo.Topics))
	return clusterID, nil
}

func captureTopicInventories(db *sqlx.DB, clusterInventoryID int, clusterInfo *ClusterInfo) error {
	for _, topicInfo := range clusterInfo.Topics {
		configData, _ := json.Marshal(topicInfo.Config)

		// Extract compression type from topic configuration
		compressionType := "NONE"
		if topicInfo.Config != nil {
			if compType, exists := topicInfo.Config["compression.type"]; exists {
				switch compType {
				case "gzip", "snappy", "lz4", "zstd":
					compressionType = compType
				case "producer":
					compressionType = "producer" // Defer to producer setting
				default:
					compressionType = "NONE"
				}
			}
		}

		topic := database.TopicInventory{
			ClusterInventoryID: clusterInventoryID,
			TopicName:          topicInfo.Name,
			PartitionCount:     int(topicInfo.Partitions),
			ReplicationFactor:  int(topicInfo.ReplicationFactor),
			IsInternal:         strings.HasPrefix(topicInfo.Name, "__"),
			CompressionType:    compressionType,
			ConfigData:         string(configData),
		}

		topicID, err := database.InsertTopicInventory(db, topic)
		if err != nil {
			return err
		}

		for _, partInfo := range topicInfo.PartitionInfo {
			replicaJSON := intSliceToJSON(int32SliceToIntSlice(partInfo.Replicas))
			isrJSON := intSliceToJSON(int32SliceToIntSlice(partInfo.ISR))

			partition := database.PartitionInventory{
				TopicInventoryID: topicID,
				PartitionID:      int(partInfo.ID),
				ReplicaIDs:       replicaJSON,
				IsrIDs:           isrJSON,
				HighWaterMark:    0,
			}

			if partInfo.Leader >= 0 {
				leaderID := int(partInfo.Leader)
				partition.LeaderID = &leaderID
			}

			err = database.InsertPartitionInventory(db, partition)
			if err != nil {
				return err
			}
		}

		logger.Debug("Captured topic inventory for %s: %d partitions", topicInfo.Name, len(topicInfo.PartitionInfo))
	}

	return nil
}

func captureConsumerGroupInventory(db *sqlx.DB, snapshotID int, admin *AdminClient, groupID string, topics []string, ctx context.Context) error {
	consumerGroup := database.ConsumerGroupInventory{
		SnapshotID:   snapshotID,
		GroupID:      groupID,
		GroupState:   "Unknown",
		ProtocolType: "consumer",
		Protocol:     "range",
		MemberCount:  0,
	}

	groupInventoryID, err := database.InsertConsumerGroupInventory(db, consumerGroup)
	if err != nil {
		return err
	}

	offsets, err := admin.GetConsumerGroupOffsets(ctx, groupID, topics)
	if err != nil {
		logger.Warn("Failed to get consumer group offsets: %v", err)
		return nil
	}

	for _, topicOffsets := range offsets {
		for _, offset := range topicOffsets {
			consumerOffset := database.ConsumerGroupOffset{
				ConsumerGroupInventoryID: groupInventoryID,
				TopicName:                offset.Topic,
				PartitionID:              int(offset.Partition),
				CurrentOffset:            offset.Offset,
				HighWaterMark:            offset.HighWaterMark,
				Lag:                      offset.Lag,
			}

			err = database.InsertConsumerGroupOffset(db, consumerOffset)
			if err != nil {
				return err
			}
		}
	}

	logger.Info("Captured consumer group inventory for %s", groupID)
	return nil
}

func captureConnectionInventory(db *sqlx.DB, snapshotID int, clusterCfg config.ClusterConfig, connectionType string) error {
	var securityProtocol, saslMechanism, apiKeyPrefix string

	switch clusterCfg.Provider {
	case "confluent":
		securityProtocol = "SASL_SSL"
		saslMechanism = "PLAIN"
		if clusterCfg.Security.APIKey != "" {
			apiKeyPrefix = maskAPIKey(clusterCfg.Security.APIKey)
		}
	case "redpanda":
		securityProtocol = "SASL_SSL"
		saslMechanism = "SCRAM-SHA-256"
		if clusterCfg.Security.Username != "" {
			apiKeyPrefix = maskAPIKey(clusterCfg.Security.Username)
		}
	default:
		securityProtocol = "PLAINTEXT"
	}

	connection := database.ConnectionInventory{
		SnapshotID:           snapshotID,
		ConnectionType:       connectionType,
		Provider:             clusterCfg.Provider,
		Brokers:              clusterCfg.Brokers,
		SecurityProtocol:     securityProtocol,
		SaslMechanism:        saslMechanism,
		APIKeyPrefix:         apiKeyPrefix,
		ConnectionSuccessful: true, // If we got this far, connection was successful
		ConnectionTimeMs:     nil,  // Would need to measure actual connection time
		ErrorMessage:         "",
	}

	err := database.InsertConnectionInventory(db, connection)
	if err != nil {
		return err
	}

	logger.Debug("Captured %s connection inventory", connectionType)
	return nil
}

func int32SliceToIntSlice(int32s []int32) []int {
	ints := make([]int, len(int32s))
	for i, v := range int32s {
		ints[i] = int(v)
	}
	return ints
}

func maskAPIKey(apiKey string) string {
	if len(apiKey) <= 4 {
		return "****"
	}
	return apiKey[:4] + "****"
}

func intSliceToJSON(slice []int) string {
	if len(slice) == 0 {
		return "[]"
	}
	data, _ := json.Marshal(slice)
	return string(data)
}

// detectTopicCompression extracts compression type from topic configuration
func detectTopicCompression(topicInfo *TopicInfo) string {
	if topicInfo.Config == nil {
		return "NONE"
	}

	if compType, exists := topicInfo.Config["compression.type"]; exists {
		switch compType {
		case "gzip", "snappy", "lz4", "zstd":
			return compType
		case "producer":
			return "producer" // Defer to producer setting
		default:
			return "NONE"
		}
	}
	return "NONE"
}

// isRegex checks if a string is likely a regular expression.
func isRegex(s string) bool {
	// A simple check, can be improved.
	return regexp.MustCompile(`[\.\*\+\?\{\}\(\)\[\]\^\$]`).MatchString(s)
}

type AdminClientAPI interface {
	GetClusterInfo(ctx context.Context) (*ClusterInfo, error)
	GetConsumerGroupOffsets(ctx context.Context, groupID string, topics []string) (map[string][]OffsetInfo, error)
	GetTopicHighWaterMarks(ctx context.Context, topics []string) (map[string][]OffsetInfo, error)
	EnsureTopicExists(ctx context.Context, topicName string, partitions int32, replicationFactor int16) error
	ValidateTopicCompatibility(ctx context.Context, sourceInfo, targetInfo TopicInfo) error
	Close()
}

var adminClientFactory = func(cfg config.ClusterConfig) (AdminClientAPI, error) {
	return NewAdminClient(cfg)
}

// SetAdminClientFactoryForTest replaces the admin client factory and returns a restore func.
func SetAdminClientFactoryForTest(factory func(config.ClusterConfig) (AdminClientAPI, error)) func() {
	previous := adminClientFactory
	adminClientFactory = factory
	return func() {
		adminClientFactory = previous
	}
}

// ResolveTopicMappingsForTest exposes mapping resolution for tests.
func ResolveTopicMappingsForTest(cfg *config.Config) ([]string, map[string]string, error) {
	topics, topicMap, _, err := resolveTopicMappings(cfg)
	return topics, topicMap, err
}

// NewKafMirrorImplForTest builds a minimal KafMirrorImpl for unit tests.
func NewKafMirrorImplForTest(producer *Producer, topicMap map[string]string, targetPartitions map[string]int32) *KafMirrorImpl {
	return &KafMirrorImpl{
		Producer:         producer,
		topicMap:         topicMap,
		targetPartitions: targetPartitions,
		incidentStates:   make(map[string]bool),
	}
}

// ValidateAndSyncClustersForTest exposes cluster validation for unit tests.
func ValidateAndSyncClustersForTest(cfg *config.Config, topics []string, topicMap map[string]string) (map[string]int32, error) {
	return validateAndSyncClusters(cfg, topics, topicMap)
}
