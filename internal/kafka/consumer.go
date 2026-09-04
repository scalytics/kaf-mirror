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

	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/sasl/kerberos"
	"github.com/twmb/franz-go/pkg/sasl/plain"
	"github.com/twmb/franz-go/pkg/sasl/scram"
)

type ConsumerMetrics struct {
	RecordsProcessed int64
	BytesProcessed   int64
	ConsumerLag      int64
}

type Consumer struct {
	Client           KgoClient
	recordsProcessed int64
	bytesProcessed   int64
	jobID            string
	mu               sync.RWMutex
	highWaterMarks   map[string]map[int32]int64
	lastOffsets      map[string]map[int32]int64
}

func NewConsumer(cfg config.ClusterConfig, groupID string, replicationCfg config.ReplicationConfig, jobID string, topics ...string) (*Consumer, error) {
	logger.Info("Creating new Kafka consumer: provider=%s, brokers=%s, group=%s, topics=%v, job=%s, component=%s",
		cfg.Provider, cfg.Brokers, groupID, topics, jobID, "consumer")

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

	consumer := &Consumer{
		Client:         client,
		jobID:          jobID,
		highWaterMarks: make(map[string]map[int32]int64),
		lastOffsets:    make(map[string]map[int32]int64),
	}

	logger.Info("Consumer: Successfully created Kafka client, job=%s, component=%s", jobID, "consumer")
	return consumer, nil
}

func (c *Consumer) Consume(ctx context.Context, handler func(*kgo.Record) error) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
			fetches := c.Client.PollFetches(ctx)
			if fetches.IsClientClosed() {
				return
			}

			recordCount := 0
			fetches.EachError(func(t string, p int32, err error) {
				logger.ErrorAI("disaster", "replication", c.jobID, "Consumer: Fetch error for topic %s, partition %d: %v", t, p, err)
			})
			fetches.EachPartition(func(partition kgo.FetchTopicPartition) {
				high := partition.HighWatermark
				topic := partition.Topic
				partID := partition.Partition
				c.mu.Lock()
				if c.highWaterMarks == nil {
					c.highWaterMarks = make(map[string]map[int32]int64)
				}
				if c.highWaterMarks[topic] == nil {
					c.highWaterMarks[topic] = make(map[int32]int64)
				}
				c.highWaterMarks[topic][partID] = high
				c.mu.Unlock()
			})

			fetches.EachRecord(func(record *kgo.Record) {
				recordCount++

				atomic.AddInt64(&c.recordsProcessed, 1)
				atomic.AddInt64(&c.bytesProcessed, int64(len(record.Value)+len(record.Key)))

				c.mu.Lock()
				if c.lastOffsets == nil {
					c.lastOffsets = make(map[string]map[int32]int64)
				}
				if c.lastOffsets[string(record.Topic)] == nil {
					c.lastOffsets[string(record.Topic)] = make(map[int32]int64)
				}
				c.lastOffsets[string(record.Topic)][record.Partition] = record.Offset
				c.mu.Unlock()

				if err := handler(record); err != nil {
					logger.Error("Consumer: handler failed for topic %s partition %d offset %d: %v", record.Topic, record.Partition, record.Offset, err)
					return
				}
				if err := c.Client.CommitRecords(ctx, record); err != nil {
					logger.Error("Consumer: failed to commit offset for topic %s partition %d offset %d: %v", record.Topic, record.Partition, record.Offset, err)
				}
			})
		}
	}
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
