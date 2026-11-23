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
	"crypto/tls"
	"fmt"
	"kaf-mirror/internal/config"
	"kaf-mirror/pkg/logger"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/sasl/kerberos"
	"github.com/twmb/franz-go/pkg/sasl/plain"
	"github.com/twmb/franz-go/pkg/sasl/scram"
)

type ConsumerMetrics struct {
	RecordsProcessed int64
	BytesProcessed   int64
	ConsumerLag      int64
	Tombstones       int64
}

const (
	defaultProduceAttempts = 5
	defaultProduceBackoff  = time.Second
)

type topicPartition struct {
	topic     string
	partition int32
}

type Consumer struct {
	Client           KgoClient
	recordsProcessed int64
	bytesProcessed   int64
	tombstones       int64
	jobID            string
	mu               sync.RWMutex
	highWaterMarks   map[string]map[int32]int64
	lastOffsets      map[string]map[int32]int64
	produceAttempts  int
	produceBackoff   time.Duration
	partMu           sync.Mutex
	partCancels      map[topicPartition]context.CancelFunc
}

func NewConsumer(cfg config.ClusterConfig, groupID string, replicationCfg config.ReplicationConfig, jobID string, topics ...string) (*Consumer, error) {
	logger.Info("Creating new Kafka consumer: provider=%s, brokers=%s, group=%s, topics=%v, job=%s, component=%s",
		cfg.Provider, cfg.Brokers, groupID, topics, jobID, "consumer")

	consumer := &Consumer{
		jobID:           jobID,
		highWaterMarks:  make(map[string]map[int32]int64),
		lastOffsets:     make(map[string]map[int32]int64),
		produceAttempts: defaultProduceAttempts,
		produceBackoff:  defaultProduceBackoff,
	}

	opts := []kgo.Opt{
		kgo.SeedBrokers(strings.Split(cfg.Brokers, ",")...),
		kgo.ConsumerGroup(groupID),
		kgo.ConsumeTopics(topics...),
		kgo.FetchMaxBytes(int32(replicationCfg.BatchSize * 1024)),
		kgo.DisableAutoCommit(),
		kgo.OnPartitionsAssigned(func(ctx context.Context, c *kgo.Client, assigned map[string][]int32) {
			logger.Info("Consumer partitions assigned: %v, job=%s, component=%s", assigned, jobID, "consumer")
		}),
		kgo.OnPartitionsRevoked(func(ctx context.Context, c *kgo.Client, revoked map[string][]int32) {
			logger.Info("Consumer partitions revoked: %v, job=%s, component=%s", revoked, jobID, "consumer")
			consumer.RevokePartitions(revoked)
		}),
		kgo.OnPartitionsLost(func(ctx context.Context, c *kgo.Client, lost map[string][]int32) {
			logger.Warn("Consumer partitions lost: %v, job=%s, component=%s", lost, jobID, "consumer")
			consumer.RevokePartitions(lost)
		}),
	}

	logger.Debug("Consumer configuration: batch_size=%dKB, parallelism=%d, job=%s, component=%s",
		replicationCfg.BatchSize, replicationCfg.Parallelism, jobID, "consumer")

	if strings.Contains(strings.ToUpper(cfg.Security.Protocol), "SSL") {
		logger.Info("Consumer: Enabling TLS connection")
		tlsConfig := &tls.Config{
			InsecureSkipVerify: false,
		}
		opts = append(opts, kgo.Dialer((&tls.Dialer{Config: tlsConfig}).DialContext))
	}

	// Generic SASL authentication for traditional Kafka clusters
	if cfg.Security.Enabled && cfg.Security.SASLMechanism != "" {
		logger.Info("Consumer: Configuring SASL authentication: mechanism=%s", cfg.Security.SASLMechanism)
		switch strings.ToUpper(cfg.Security.SASLMechanism) {
		case "GSSAPI":
			if cfg.Security.Kerberos.ServiceName == "" {
				return nil, fmt.Errorf("Kerberos service name is required for GSSAPI")
			}
			logger.Debug("Consumer: Using Kerberos authentication with service=%s", cfg.Security.Kerberos.ServiceName)
			kerberosAuth := kerberos.Auth{
				Service: cfg.Security.Kerberos.ServiceName,
			}
			opts = append(opts, kgo.SASL(kerberosAuth.AsMechanism()))
		case "PLAIN":
			if cfg.Security.Username == "" || cfg.Security.Password == "" {
				return nil, fmt.Errorf("username and password are required for PLAIN authentication")
			}
			logger.Debug("Consumer: Using PLAIN authentication with username=%s", cfg.Security.Username)
			opts = append(opts, kgo.SASL(plain.Auth{
				User: cfg.Security.Username,
				Pass: cfg.Security.Password,
			}.AsMechanism()))
		case "SCRAM-SHA-256", "SCRAM-SHA-512":
			if cfg.Security.Username == "" || cfg.Security.Password == "" {
				return nil, fmt.Errorf("username and password are required for SCRAM authentication")
			}
			logger.Debug("Consumer: Using SCRAM authentication: mechanism=%s, username=%s",
				cfg.Security.SASLMechanism, cfg.Security.Username)
			auth := scram.Auth{
				User: cfg.Security.Username,
				Pass: cfg.Security.Password,
			}
			if strings.ToUpper(cfg.Security.SASLMechanism) == "SCRAM-SHA-512" {
				opts = append(opts, kgo.SASL(auth.AsSha512Mechanism()))
			} else {
				opts = append(opts, kgo.SASL(auth.AsSha256Mechanism()))
			}
		default:
			return nil, fmt.Errorf("unsupported SASL mechanism: %s", cfg.Security.SASLMechanism)
		}
	}

	switch cfg.Provider {
	case "confluent":
		if cfg.Security.APIKey == "" || cfg.Security.APISecret == "" {
			return nil, fmt.Errorf("confluent provider requires API key and secret")
		}
		logger.Info("Consumer: Configuring Confluent Cloud connection with API key authentication")
		// Confluent Cloud always requires TLS and SASL PLAIN
		tlsConfig := &tls.Config{}
		opts = append(opts, kgo.DialTLSConfig(tlsConfig))
		opts = append(opts, kgo.SASL(plain.Auth{
			User: cfg.Security.APIKey,
			Pass: cfg.Security.APISecret,
		}.AsMechanism()))

	case "redpanda":
		logger.Info("Consumer: Configuring RedPanda connection")
		// RedPanda Cloud typically uses TLS + SASL SCRAM
		if cfg.Security.Username != "" && cfg.Security.Password != "" {
			tlsConfig := &tls.Config{}
			opts = append(opts, kgo.DialTLSConfig(tlsConfig))
			opts = append(opts, kgo.SASL(scram.Auth{
				User: cfg.Security.Username,
				Pass: cfg.Security.Password,
			}.AsSha256Mechanism()))
		}

	case "plain":
		logger.Info("Consumer: Using plain Kafka configuration")
		// Plain Kafka uses the existing security configuration logic above
		// No additional provider-specific setup needed

	default:
		// For unknown providers, log a warning but continue with generic setup
		if cfg.Provider != "" {
			logger.Warn("Consumer: Unknown provider '%s', using generic configuration", cfg.Provider)
		}
	}

	logger.Info("Consumer: Establishing connection to Kafka brokers, job=%s, component=%s", jobID, "consumer")
	client, err := kgo.NewClient(opts...)
	if err != nil {
		logger.ErrorAI("disaster", "connection", jobID, "Consumer: Failed to create Kafka client: %v", err)
		return nil, err
	}

	consumer.Client = client

	logger.Info("Consumer: Successfully created Kafka client, job=%s, component=%s", jobID, "consumer")
	return consumer, nil
}

func (c *Consumer) Consume(ctx context.Context, handler func(context.Context, *kgo.Record) error) error {
	for {
		if ctx.Err() != nil {
			return nil
		}
		fetches := c.Client.PollFetches(ctx)
		if fetches.IsClientClosed() {
			return nil
		}

		fetches.EachError(func(t string, p int32, err error) {
			logger.ErrorAI("disaster", "replication", c.jobID, "Consumer: Fetch error for topic %s, partition %d: %v", t, p, err)
		})

		partCtxs := c.trackPartitions(ctx, fetches)
		var failed error
		fetches.EachPartition(func(partition kgo.FetchTopicPartition) {
			c.setHighWaterMark(partition.Topic, partition.Partition, partition.HighWatermark)
			if failed != nil {
				return
			}
			tp := topicPartition{topic: partition.Topic, partition: partition.Partition}
			failed = c.consumePartition(ctx, partCtxs[tp], partition.Records, handler)
		})
		c.releasePartitions(partCtxs)

		if ctx.Err() != nil {
			return nil
		}
		if failed != nil {
			return failed
		}
	}
}

func (c *Consumer) consumePartition(ctx, partCtx context.Context, records []*kgo.Record, handler func(context.Context, *kgo.Record) error) error {
	for _, record := range records {
		c.setLastOffset(record.Topic, record.Partition, record.Offset)

		if err := c.handleWithRetry(partCtx, record, handler); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if partCtx.Err() != nil {
				logger.Warn("Consumer: partition revoked, stopped at topic %s partition %d offset %d without commit, job=%s", record.Topic, record.Partition, record.Offset, c.jobID)
				return nil
			}
			logger.Error("Consumer: handler failed for topic %s partition %d offset %d: %v", record.Topic, record.Partition, record.Offset, err)
			return fmt.Errorf("produce failed for topic %s partition %d offset %d after %d attempts: %w", record.Topic, record.Partition, record.Offset, max(c.produceAttempts, 1), err)
		}
		atomic.AddInt64(&c.recordsProcessed, 1)
		atomic.AddInt64(&c.bytesProcessed, int64(len(record.Value)+len(record.Key)))
		if len(record.Value) == 0 {
			atomic.AddInt64(&c.tombstones, 1)
		}
		if err := c.Client.CommitRecords(ctx, record); err != nil {
			logger.Error("Consumer: failed to commit offset for topic %s partition %d offset %d: %v", record.Topic, record.Partition, record.Offset, err)
		}
	}
	return nil
}

func (c *Consumer) handleWithRetry(ctx context.Context, record *kgo.Record, handler func(context.Context, *kgo.Record) error) error {
	backoff := c.produceBackoff
	for attempt := 1; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := handler(ctx, record)
		if err == nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if attempt >= c.produceAttempts {
			return err
		}
		logger.Warn("Consumer: produce attempt %d of %d failed for topic %s partition %d offset %d, retrying in %s: %v", attempt, c.produceAttempts, record.Topic, record.Partition, record.Offset, backoff, err)
		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
		backoff *= 2
	}
}

func (c *Consumer) trackPartitions(ctx context.Context, fetches kgo.Fetches) map[topicPartition]context.Context {
	ctxs := make(map[topicPartition]context.Context)
	c.partMu.Lock()
	defer c.partMu.Unlock()
	if c.partCancels == nil {
		c.partCancels = make(map[topicPartition]context.CancelFunc)
	}
	fetches.EachPartition(func(partition kgo.FetchTopicPartition) {
		tp := topicPartition{topic: partition.Topic, partition: partition.Partition}
		if _, ok := ctxs[tp]; ok {
			return
		}
		partCtx, cancel := context.WithCancel(ctx)
		ctxs[tp] = partCtx
		c.partCancels[tp] = cancel
	})
	return ctxs
}

func (c *Consumer) releasePartitions(ctxs map[topicPartition]context.Context) {
	c.partMu.Lock()
	defer c.partMu.Unlock()
	for tp := range ctxs {
		if cancel, ok := c.partCancels[tp]; ok {
			cancel()
			delete(c.partCancels, tp)
		}
	}
}

func (c *Consumer) RevokePartitions(revoked map[string][]int32) {
	c.partMu.Lock()
	defer c.partMu.Unlock()
	for topic, partitions := range revoked {
		for _, partition := range partitions {
			if cancel, ok := c.partCancels[topicPartition{topic: topic, partition: partition}]; ok {
				cancel()
			}
		}
	}
}

func (c *Consumer) setHighWaterMark(topic string, partition int32, high int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.highWaterMarks == nil {
		c.highWaterMarks = make(map[string]map[int32]int64)
	}
	if c.highWaterMarks[topic] == nil {
		c.highWaterMarks[topic] = make(map[int32]int64)
	}
	c.highWaterMarks[topic][partition] = high
}

func (c *Consumer) setLastOffset(topic string, partition int32, offset int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.lastOffsets == nil {
		c.lastOffsets = make(map[string]map[int32]int64)
	}
	if c.lastOffsets[topic] == nil {
		c.lastOffsets[topic] = make(map[int32]int64)
	}
	c.lastOffsets[topic][partition] = offset
}

func (c *Consumer) GetMetrics() ConsumerMetrics {
	var calculatedLag int64

	c.mu.RLock()
	for topic, parts := range c.highWaterMarks {
		for p, high := range parts {
			lastOffset := int64(-1)
			if c.lastOffsets != nil {
				if topicOffsets, ok := c.lastOffsets[topic]; ok {
					if offset, exists := topicOffsets[p]; exists {
						lastOffset = offset
					}
				}
			}
			lag := high - (lastOffset + 1)
			if lag < 0 {
				lag = 0
			}
			calculatedLag += lag
		}
	}
	c.mu.RUnlock()

	return ConsumerMetrics{
		RecordsProcessed: atomic.LoadInt64(&c.recordsProcessed),
		BytesProcessed:   atomic.LoadInt64(&c.bytesProcessed),
		ConsumerLag:      calculatedLag,
		Tombstones:       atomic.LoadInt64(&c.tombstones),
	}
}

func (c *Consumer) Close() {
	logger.Info("Consumer is shutting down, job=%s, component=%s", c.jobID, "consumer")
	c.Client.Close()
}

// AddTopics adds new topics to the consumer group subscription.
func (c *Consumer) AddTopics(topics ...string) {
	if len(topics) == 0 {
		return
	}
	c.Client.AddConsumeTopics(topics...)
}

// NewConsumerForTest creates a Consumer with preset offsets for unit tests.
func NewConsumerForTest(highWaterMarks map[string]map[int32]int64, lastOffsets map[string]map[int32]int64) *Consumer {
	return &Consumer{
		highWaterMarks: highWaterMarks,
		lastOffsets:    lastOffsets,
	}
}

func (c *Consumer) SetProduceRetryForTest(attempts int, backoff time.Duration) {
	c.produceAttempts = attempts
	c.produceBackoff = backoff
}

func (c *Consumer) ProduceRetryForTest() (int, time.Duration) {
	return c.produceAttempts, c.produceBackoff
}
