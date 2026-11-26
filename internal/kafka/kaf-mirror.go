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
	"errors"
	"fmt"
	"kaf-mirror/internal/config"
	"kaf-mirror/internal/database"
	"kaf-mirror/pkg/logger"
	"sort"
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

type RecordInspector interface {
	SetRecordHook(func(topic string, key, value []byte))
	SetRecordGuard(func() error)
}

// KafMirrorImpl orchestrates the replication from a source to a target Kafka cluster.
type KafMirrorImpl struct {
	Consumer          *Consumer
	Producer          *Producer
	sourceCfg         config.ClusterConfig
	targetCfg         config.ClusterConfig
	mappings          []config.TopicMapping
	topicMap          map[string]string
	rules             TopicRules
	mapMu             sync.RWMutex
	wg                sync.WaitGroup
	reports           sync.WaitGroup
	cancelFunc        context.CancelFunc
	jobID             string
	onPanic           func(jobID string, reason string)
	discoveryInterval time.Duration
	metricsInterval   time.Duration

	// Incident tracking to prevent spam logging
	incidentStates map[string]bool
	incidentMutex  sync.RWMutex
	incidentHook   func(name string)
	recordHook     func(topic string, key, value []byte)
	recordGuard    func() error
	sizeHintMu     sync.Mutex
	sizeHinted     map[string]bool
	lagAdmin       *AdminClient
	groupLag       func(ctx context.Context, topics []string) (int64, error)
}

const groupLagTimeout = 5 * time.Second

func (r *KafMirrorImpl) incidentStarted(name string) {
	if r.incidentHook != nil {
		r.incidentHook(name)
	}
}

func (r *KafMirrorImpl) SetRecordHook(h func(topic string, key, value []byte)) {
	r.recordHook = h
}

// SetRecordGuard installs a protection check run before every send, including
// retries. Hooks inspect a record once; guards also stop already inspected work.
func (r *KafMirrorImpl) SetRecordGuard(guard func() error) {
	r.recordGuard = guard
}

// NewKafMirror creates a new replication orchestrator.
func NewKafMirror(cfg *config.Config, db *sqlx.DB) (KafMirror, error) {
	topics, topicMap, rules, err := resolveTopicMappings(cfg)
	if err != nil {
		return nil, err
	}

	// Validate cluster compatibility and sync state before starting
	if err := validateAndSyncClusters(cfg, db, topics, topicMap); err != nil {
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

	lagAdmin, err := NewAdminClient(cfg.Clusters["source"])
	if err != nil {
		consumer.Close()
		producer.Close()
		return nil, fmt.Errorf("failed to create source admin client for lag: %w", err)
	}

	return &KafMirrorImpl{
		Consumer:          consumer,
		Producer:          producer,
		sourceCfg:         cfg.Clusters["source"],
		targetCfg:         cfg.Clusters["target"],
		mappings:          cfg.Topics,
		topicMap:          topicMap,
		rules:             rules,
		discoveryInterval: discoveryInterval(cfg),
		incidentStates:    make(map[string]bool),
		lagAdmin:          lagAdmin,
		groupLag: func(ctx context.Context, topics []string) (int64, error) {
			return lagAdmin.GetGroupLag(ctx, consumerGroup, topics...)
		},
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
				r.reportFailure(onPanic, jobID, reason)
			}
		}()
		logger.Info("[Job %s] Starting consumer goroutine", jobID)
		err := r.Consumer.ConsumeAsync(ctx, func(ctx context.Context, record *kgo.Record, attempt int, done func(error)) {
			logger.Debug("[Job %s] Received record from topic %s, partition %d, offset %d", jobID, record.Topic, record.Partition, record.Offset)
			r.handleRecord(ctx, record, attempt, done)
		})
		if err != nil {
			reason := fmt.Sprintf("consumer stopped: %v", err)
			logger.Error("[Job %s] %s", jobID, reason)
			r.reportFailure(onPanic, jobID, reason)
		}
		logger.Info("[Job %s] Consumer goroutine ended", jobID)
	}()

	go func() {
		defer r.wg.Done()
		defer func() {
			if rec := recover(); rec != nil {
				reason := fmt.Sprintf("metrics panic: %v", rec)
				logger.Error("[Job %s] %s", jobID, reason)
				r.reportFailure(onPanic, jobID, reason)
			}
		}()
		logger.Info("[Job %s] Starting metrics collection goroutine", jobID)
		r.collectMetrics(ctx, jobID, metricsCallback, onPanic)
		logger.Info("[Job %s] Metrics collection goroutine ended", jobID)
	}()

	if r.rules.HasRegex() && r.discoveryInterval > 0 {
		r.wg.Add(1)
		go func() {
			defer r.wg.Done()
			r.discoverTopicsLoop(ctx)
		}()
	}

	logger.Info("[Job %s] Both goroutines started successfully", jobID)
}

func (r *KafMirrorImpl) reportFailure(onPanic func(jobID string, reason string), jobID string, reason string) {
	r.reports.Add(1)
	go func() {
		defer r.reports.Done()
		onPanic(jobID, reason)
	}()
}

// Stop gracefully shuts down the kaf-mirror.
func (r *KafMirrorImpl) Stop() {
	if r.cancelFunc != nil {
		r.cancelFunc()
	}
	r.wg.Wait()
	r.Consumer.Close()
	r.Producer.Close()
	if r.lagAdmin != nil {
		r.lagAdmin.Close()
	}
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
	result := make(chan error, 1)
	r.handleRecord(context.Background(), record, 1, func(err error) { result <- err })
	return <-result
}

func (r *KafMirrorImpl) AddRegexMapForTest(pattern, target string) error {
	rule, err := NewTopicRule(pattern, target)
	if err != nil {
		return err
	}
	if !rule.IsRegex() {
		return fmt.Errorf("pattern %q is not a regex", pattern)
	}
	return r.rules.add(rule)
}

func (r *KafMirrorImpl) handleRecord(ctx context.Context, record *kgo.Record, attempt int, done func(error)) {
	r.mapMu.RLock()
	targetTopic, ok := r.topicMap[string(record.Topic)]
	r.mapMu.RUnlock()
	if !ok {
		targetTopic, _, _ = r.rules.RegexTargetFor(string(record.Topic))
	}

	if targetTopic == "" {
		logger.Warn("No mapping found for topic: %s", record.Topic)
		done(nil)
		return
	}
	if attempt == 1 {
		if r.recordHook != nil {
			r.recordHook(string(record.Topic), record.Key, record.Value)
		}

		r.analyzeRecordSize(record)
	}
	if err := ctx.Err(); err != nil {
		done(err)
		return
	}
	if r.recordGuard != nil {
		if err := r.recordGuard(); err != nil {
			done(err)
			return
		}
	}

	// Create a new record for the target topic
	outRecord := &kgo.Record{
		Topic:     targetTopic,
		Value:     record.Value,
		Key:       record.Key,
		Headers:   record.Headers,
		Partition: record.Partition,
	}
	if !record.Timestamp.IsZero() && record.Timestamp.UnixMilli() >= 0 {
		outRecord.Timestamp = record.Timestamp
	}

	r.Producer.Produce(ctx, outRecord, func(rec *kgo.Record, err error) {
		if err != nil {
			logger.Error("Failed to produce record to topic %s: %v", rec.Topic, err)
		} else {
			logger.Debug("Replicated record to topic %s, partition %d, offset %d", rec.Topic, rec.Partition, rec.Offset)
		}
		done(err)
	})
}

func (r *KafMirrorImpl) analyzeRecordSize(record *kgo.Record) {
	valueSize := len(record.Value)
	keySize := len(record.Key)
	totalSize := valueSize + keySize
	if totalSize == 0 {
		return
	}
	if totalSize > 2048 || record.Offset%100 == 0 {
		logger.DebugAI("message", "size", r.jobID,
			"Message analysis: topic=%s, partition=%d, total_size=%d bytes, value_size=%d bytes, key_size=%d bytes",
			record.Topic, record.Partition, totalSize, valueSize, keySize)
	}
	if totalSize <= 2048 {
		return
	}
	r.sizeHintMu.Lock()
	if r.sizeHinted == nil {
		r.sizeHinted = make(map[string]bool)
	}
	hinted := r.sizeHinted[record.Topic]
	r.sizeHinted[record.Topic] = true
	r.sizeHintMu.Unlock()
	if hinted {
		return
	}
	logger.InfoAI("compression", "candidate", r.jobID,
		"Topic %s has records larger than 2 KB, the first is %d bytes. lz4 or snappy compression could reduce the transfer size. This is logged once per topic while the job runs",
		record.Topic, totalSize)
}

type stallProgress struct {
	lastConsumed, lastReplicated int64
	idleTicks                    int
}

func (s *stallProgress) stalled(consumed, replicated, lag int64) bool {
	if consumed < s.lastConsumed || replicated < s.lastReplicated {
		s.lastConsumed = consumed
		s.lastReplicated = replicated
		s.idleTicks = 0
		return false
	}
	moved := consumed > s.lastConsumed || replicated > s.lastReplicated
	s.lastConsumed = consumed
	s.lastReplicated = replicated
	if lag > 0 && !moved {
		s.idleTicks++
	} else {
		s.idleTicks = 0
	}
	return s.idleTicks > 6
}

func (r *KafMirrorImpl) sourceTopics() []string {
	r.mapMu.RLock()
	defer r.mapMu.RUnlock()
	topics := make([]string, 0, len(r.topicMap))
	for topic := range r.topicMap {
		topics = append(topics, topic)
	}
	sort.Strings(topics)
	return topics
}

func (r *KafMirrorImpl) currentLag(ctx context.Context, fetchLag int64) int64 {
	if r.groupLag == nil {
		return fetchLag
	}
	ctx, cancel := context.WithTimeout(ctx, groupLagTimeout)
	defer cancel()
	lag, err := r.groupLag(ctx, r.sourceTopics())
	if err != nil {
		logger.Debug("[Job %s] Group lag unavailable, using fetch lag %d: %v", r.jobID, fetchLag, err)
		return fetchLag
	}
	return lag
}

func (r *KafMirrorImpl) collectMetrics(ctx context.Context, jobID string, callback func(database.ReplicationMetric), onPanic func(jobID string, reason string)) {
	interval := r.metricsInterval
	if interval <= 0 {
		interval = 10 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	logger.Info("[Job %s] Metrics collection loop started", jobID)

	// Counter for periodic logging (every 5th tick = ~50 seconds)
	logCounter := 0
	var lastErrors int64
	var progress stallProgress

	for {
		select {
		case <-ticker.C:
			// Get current metrics from consumer and producer
			consumerMetrics := r.Consumer.GetMetrics()
			producerMetrics := r.Producer.GetMetrics()

			totalMessages := consumerMetrics.RecordsReplicated
			totalBytes := consumerMetrics.BytesReplicated
			totalConsumed := consumerMetrics.RecordsProcessed
			totalConsumedBytes := consumerMetrics.BytesProcessed
			totalErrors := producerMetrics.ErrorCount + consumerMetrics.FetchErrors
			currentLag := r.currentLag(ctx, consumerMetrics.ConsumerLag)

			// Detect performance disasters and incidents
			logCounter++

			// Disaster detection thresholds
			criticalLag := currentLag > 200
			highErrorRate := totalErrors > 0 && totalMessages > 0 && (float64(totalErrors)/float64(totalMessages)) > 0.1
			sourceStalled := progress.stalled(totalConsumed, totalMessages, currentLag)
			var targetStalled bool
			newErrors := totalErrors - lastErrors
			if newErrors < 0 {
				newErrors = totalErrors
			}
			lastErrors = totalErrors
			errorSpike := newErrors > 0

			// Performance milestones (positive events)
			isFirstProduced := totalMessages == 1 && logCounter <= 2
			isSignificantProduced := totalMessages > 0 && totalMessages%100 == 0
			isFirstConsumed := totalConsumed == 1 && logCounter <= 2
			isSignificantConsumed := totalConsumed > 0 && totalConsumed%100 == 0

			// Check for producer failure threshold
			if producerMetrics.ConsecutiveErrors > 100 {
				reason := fmt.Sprintf("producer has failed %d consecutive times", producerMetrics.ConsecutiveErrors)
				logger.Error("[Job %s] %s", jobID, reason)
				r.reportFailure(onPanic, jobID, reason)
				return
			}

			// Disaster logging with incident state tracking to prevent spam
			r.incidentMutex.Lock()

			if criticalLag {
				if !r.incidentStates["critical_lag"] {
					logger.ErrorAI("disaster", "performance", jobID, "Critical lag detected: Messages=%d, Bytes=%d, Lag=%d, Errors=%d",
						totalMessages, totalBytes, currentLag, totalErrors)
					r.incidentStates["critical_lag"] = true
					r.incidentStarted("critical_lag")
				}
			} else {
				r.incidentStates["critical_lag"] = false
			}

			if highErrorRate {
				if !r.incidentStates["high_error_rate"] {
					logger.ErrorAI("incident", "escalation", jobID, "High error rate detected: Messages=%d, Errors=%d, Rate=%.2f%%",
						totalMessages, totalErrors, (float64(totalErrors)/float64(totalMessages))*100)
					r.incidentStates["high_error_rate"] = true
					r.incidentStarted("high_error_rate")
				}
			} else {
				r.incidentStates["high_error_rate"] = false
			}

			if sourceStalled {
				if !r.incidentStates["source_stalled"] {
					logger.WarnAI("incident", "start", jobID, "Source stalled: no progress for about 70 seconds while lag is %d", currentLag)
					r.incidentStates["source_stalled"] = true
					r.incidentStarted("source_stalled")
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
					r.incidentStarted("target_stalled")
				}
			} else if r.incidentStates["target_stalled"] {
				logger.InfoAI("incident", "resolved", jobID, "Target throughput resumed: Produced=%d, Bytes=%d",
					totalMessages, totalBytes)
				r.incidentStates["target_stalled"] = false
			}

			if errorSpike {
				if !r.incidentStates["error_spike"] {
					logger.ErrorAI("metrics", "errors", jobID, "Error spike detected: NewErrors=%d, Messages=%d, Bytes=%d, Lag=%d, Errors=%d",
						newErrors, totalMessages, totalBytes, currentLag, totalErrors)
					r.incidentStates["error_spike"] = true
					r.incidentStarted("error_spike")
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
				MessagesReplicated: int(totalMessages),
				BytesTransferred:   int(totalBytes),
				MessagesConsumed:   int(totalConsumed),
				BytesConsumed:      int(totalConsumedBytes),
				CurrentLag:         int(currentLag),
				ErrorCount:         int(totalErrors),
				FetchErrorCount:    int(consumerMetrics.FetchErrors),
				SourceStalled:      sourceStalled,
				TargetStalled:      targetStalled,
				CriticalLag:        criticalLag,
				HighErrorRate:      highErrorRate,
				ErrorSpike:         errorSpike,
				TombstoneCount:     consumerMetrics.Tombstones,
				TopicRecreations:   consumerMetrics.TopicRecreations,
				MissingTopics:      consumerMetrics.MissingTopics,
				Timestamp:          time.Now(),
			}

			callback(metric)
		case <-ctx.Done():
			logger.Info("[Job %s] Metrics collection cancelled", jobID)
			return
		}
	}
}

func resolveTopicMappings(cfg *config.Config) ([]string, map[string]string, TopicRules, error) {
	rules, err := NewTopicRules(cfg.Topics)
	if err != nil {
		return nil, nil, TopicRules{}, err
	}
	if !rules.HasRegex() {
		topics, topicMap, err := rules.Resolve(nil)
		return topics, topicMap, rules, err
	}

	admin, err := adminClientFactory(cfg.Clusters["source"])
	if err != nil {
		return nil, nil, TopicRules{}, fmt.Errorf("failed to create source admin client: %w", err)
	}
	defer admin.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	topics, topicMap, err := ResolveTopics(ctx, admin, cfg.Topics)
	if err != nil {
		return nil, nil, TopicRules{}, err
	}
	return topics, topicMap, rules, nil
}

// validateAndSyncClusters validates cluster compatibility and syncs state before replication
func validateAndSyncClusters(cfg *config.Config, db *sqlx.DB, topics []string, topicMap map[string]string) error {
	logger.Info("Starting cluster validation and state synchronization")

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Create admin clients for both clusters
	sourceAdmin, err := adminClientFactory(cfg.Clusters["source"])
	if err != nil {
		return fmt.Errorf("failed to create source admin client: %w", err)
	}
	defer sourceAdmin.Close()

	targetAdmin, err := adminClientFactory(cfg.Clusters["target"])
	if err != nil {
		return fmt.Errorf("failed to create target admin client: %w", err)
	}
	defer targetAdmin.Close()

	// Get cluster information
	sourceInfo, err := sourceAdmin.GetClusterInfo(ctx)
	if err != nil {
		return fmt.Errorf("failed to get source cluster info: %w", err)
	}

	targetInfo, err := targetAdmin.GetClusterInfo(ctx)
	if err != nil {
		return fmt.Errorf("failed to get target cluster info: %w", err)
	}

	logger.InfoAI("cluster", "validation", "", "Source cluster: %d brokers, %d topics", sourceInfo.BrokerCount, len(sourceInfo.Topics))
	logger.InfoAI("cluster", "validation", "", "Target cluster: %d brokers, %d topics", targetInfo.BrokerCount, len(targetInfo.Topics))

	// Validate and sync each topic mapping
	for sourceTopic, targetTopic := range topicMap {
		logger.InfoAI("topic", "validation", "", "Validating topic mapping: %s -> %s", sourceTopic, targetTopic)

		// Check if source topic exists
		sourceTopicInfo, sourceExists := sourceInfo.Topics[sourceTopic]
		if !sourceExists {
			return fmt.Errorf("source topic %s does not exist", sourceTopic)
		}

		// Ensure target topic exists with correct partitions
		replicationFactor := clampReplicationFactor(sourceTopicInfo.ReplicationFactor, targetInfo.BrokerCount, sourceTopic)
		err = ensureTopicWithRetry(ctx, targetAdmin, targetTopic, sourceTopicInfo.Partitions, replicationFactor)
		if err != nil {
			return fmt.Errorf("failed to ensure target topic %s exists: %w", targetTopic, err)
		}

		targetTopicInfo, err := waitForTargetTopicInfo(ctx, targetAdmin, targetTopic)
		if err != nil {
			return err
		}
		if targetInfo.Topics == nil {
			targetInfo.Topics = make(map[string]TopicInfo)
		}
		targetInfo.Topics[targetTopic] = targetTopicInfo

		err = targetAdmin.ValidateTopicCompatibility(ctx, sourceTopicInfo, targetTopicInfo)
		if err != nil {
			return fmt.Errorf("topic compatibility validation failed: %w", err)
		}

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
	if db == nil {
		logger.Warn("No database for job %s, skipping the startup inventory snapshot", cfg.Replication.JobID)
	} else if err := CaptureJobInventory(db, sourceAdmin, cfg.Replication.JobID, cfg, sourceInfo, targetInfo, topics, topicMap); err != nil {
		logger.Warn("Failed to capture job inventory: %v", err)
	}

	logger.Info("Cluster validation and state synchronization completed successfully")
	return nil
}

func (r *KafMirrorImpl) discoverTopicsLoop(ctx context.Context) {
	ticker := time.NewTicker(r.discoveryInterval)
	defer ticker.Stop()

	failedRuns := 0
	for {
		select {
		case <-ticker.C:
			err := r.discoverAndSyncTopics(ctx)
			if ctx.Err() != nil {
				return
			}
			var mappingErr *discoveryMappingError
			switch {
			case err == nil:
				if failedRuns > 0 {
					logger.Info("[Job %s] Topic discovery works again after %d failed runs", r.jobID, failedRuns)
				}
				failedRuns = 0
			case errors.As(err, &mappingErr):
				reason := fmt.Sprintf("topic discovery failed: %v", err)
				logger.Error("[Job %s] %s", r.jobID, reason)
				if r.onPanic != nil {
					r.reportFailure(r.onPanic, r.jobID, reason)
				}
				return
			default:
				failedRuns++
				logger.Warn("[Job %s] Topic discovery failed (%d runs in a row), keeping the current topics and retrying in %s: %v", r.jobID, failedRuns, r.discoveryInterval, err)
			}
		case <-ctx.Done():
			return
		}
	}
}

type discoveryMappingError struct {
	err error
}

func (e *discoveryMappingError) Error() string {
	return e.err.Error()
}

func (e *discoveryMappingError) Unwrap() error {
	return e.err
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

	sourceTopics := make([]string, 0, len(sourceInfo.Topics))
	for name := range sourceInfo.Topics {
		sourceTopics = append(sourceTopics, name)
	}
	sort.Strings(sourceTopics)

	for _, sourceTopic := range sourceTopics {
		sourceTopicInfo := sourceInfo.Topics[sourceTopic]
		r.mapMu.RLock()
		_, exists := r.topicMap[sourceTopic]
		r.mapMu.RUnlock()
		if exists {
			continue
		}

		targetTopic, ok, err := r.rules.RegexTargetFor(sourceTopic)
		if err != nil {
			return &discoveryMappingError{err}
		}
		if !ok {
			continue
		}
		r.mapMu.RLock()
		for existingSource, existingTarget := range r.topicMap {
			if existingTarget == targetTopic && existingSource != sourceTopic {
				r.mapMu.RUnlock()
				return &discoveryMappingError{fmt.Errorf("target topic %s is mapped from multiple sources: %s and %s", targetTopic, existingSource, sourceTopic)}
			}
		}
		r.mapMu.RUnlock()

		replicationFactor := clampReplicationFactor(sourceTopicInfo.ReplicationFactor, targetInfo.BrokerCount, sourceTopic)
		if err := ensureTopicWithRetry(ctx, targetAdmin, targetTopic, sourceTopicInfo.Partitions, replicationFactor); err != nil {
			return fmt.Errorf("failed to ensure target topic %s exists after retries: %w", targetTopic, err)
		}

		r.mapMu.Lock()
		r.topicMap[sourceTopic] = targetTopic
		r.mapMu.Unlock()

		r.Consumer.AddTopics(sourceTopic)
		logger.Info("[Job %s] Discovered new topic mapping %s -> %s", r.jobID, sourceTopic, targetTopic)
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
			if attempt == 5 {
				break
			}
			if err := sleepContext(ctx, time.Duration(attempt)*time.Second); err != nil {
				return err
			}
			continue
		}
		return nil
	}
	return fmt.Errorf("topic %s creation failed after 5 attempts: %w", topicName, lastErr)
}

func waitForTargetTopicInfo(ctx context.Context, admin AdminClientAPI, topicName string) (TopicInfo, error) {
	var lastErr error
	for attempt := 1; attempt <= 5; attempt++ {
		info, err := admin.GetClusterInfo(ctx)
		if err != nil {
			lastErr = fmt.Errorf("failed to refresh target cluster info: %w", err)
		} else if topic, ok := info.Topics[topicName]; ok && topic.Partitions > 0 {
			return topic, nil
		} else {
			lastErr = fmt.Errorf("target topic %s does not exist after creation attempt", topicName)
		}
		if attempt == 5 {
			break
		}
		logger.Info("Target topic %s is not in the target metadata yet (check %d/5)", topicName, attempt)
		if err := topicMetadataSleep(ctx, time.Duration(attempt)*time.Second); err != nil {
			return TopicInfo{}, fmt.Errorf("waiting for target topic %s metadata: %w", topicName, err)
		}
	}
	return TopicInfo{}, lastErr
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
func CaptureJobInventory(db *sqlx.DB, sourceAdmin AdminClientAPI, jobID string, cfg *config.Config, sourceInfo, targetInfo *ClusterInfo, topics []string, topicMap map[string]string) error {
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

	if sourceAdmin != nil {
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

		compressionType := detectTopicCompression(&topicInfo)

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

func captureConsumerGroupInventory(db *sqlx.DB, snapshotID int, admin AdminClientAPI, groupID string, topics []string, ctx context.Context) error {
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
func NewKafMirrorImplForTest(producer *Producer, topicMap map[string]string) *KafMirrorImpl {
	return &KafMirrorImpl{
		Producer:       producer,
		topicMap:       topicMap,
		incidentStates: make(map[string]bool),
	}
}

func (r *KafMirrorImpl) WaitForFailureReportsForTest() {
	r.reports.Wait()
}

func (r *KafMirrorImpl) SetIncidentHookForTest(h func(name string)) {
	r.incidentHook = h
}

func (r *KafMirrorImpl) SetGroupLagForTest(lag func(ctx context.Context, topics []string) (int64, error)) {
	r.groupLag = lag
}

func (r *KafMirrorImpl) SetIntervalsForTest(metrics, discovery time.Duration) {
	r.metricsInterval = metrics
	r.discoveryInterval = discovery
}

// ValidateAndSyncClustersForTest exposes cluster validation for unit tests.
func ValidateAndSyncClustersForTest(cfg *config.Config, topics []string, topicMap map[string]string) error {
	return validateAndSyncClusters(cfg, nil, topics, topicMap)
}

func ValidateAndSyncClustersWithDBForTest(cfg *config.Config, db *sqlx.DB, topics []string, topicMap map[string]string) error {
	return validateAndSyncClusters(cfg, db, topics, topicMap)
}
