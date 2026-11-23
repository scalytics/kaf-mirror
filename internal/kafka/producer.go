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
	"kaf-mirror/internal/config"
	"kaf-mirror/pkg/logger"
	"os"
	"strings"
	"sync/atomic"

	"github.com/twmb/franz-go/pkg/kgo"
)

type ProducerMetrics struct {
	RecordsProduced   int64
	BytesProduced     int64
	ErrorCount        int64
	ConsecutiveErrors int64
}

type Producer struct {
	Client            KgoClient
	recordsProduced   int64
	bytesProduced     int64
	errorCount        int64
	consecutiveErrors int64
	jobID             string
}

func NewProducer(cfg config.ClusterConfig, replicationCfg config.ReplicationConfig, jobID string) (*Producer, error) {
	logger.Info("Creating new Kafka producer: provider=%s, brokers=%s, job=%s, component=%s", cfg.Provider, cfg.Brokers, jobID, "producer")
	logger.Debug("Producer configuration: batch_size=%dKB, compression=%s, job=%s, component=%s",
		replicationCfg.BatchSize, replicationCfg.Compression, jobID, "producer")

	opts := []kgo.Opt{
		kgo.ProducerBatchMaxBytes(int32(replicationCfg.BatchSize * 1024)),
		kgo.ProducerBatchCompression(getCompressionCodec(replicationCfg.Compression)),
		kgo.RecordPartitioner(NewSourcePartitioner()),
	}
	// OPS-004: idempotent writes require the target broker to implement
	// INIT_PRODUCER_ID (API key 22). Some brokers (e.g. KafScale today) do not.
	// Two opt-outs, checked in order:
	//   1. per-cluster config: disable_idempotent_writes: true in the cluster
	//      block of the YAML config (compile-time safe, typed).
	//   2. process-wide env var KAFMIRROR_DISABLE_IDEMPOTENT_WRITES=true
	//      (convenient for operators who configure clusters via the REST API
	//      rather than a static file, where the DB schema doesn't yet carry
	//      the flag).
	disableIdempotent := cfg.DisableIdempotentWrites
	if !disableIdempotent {
		v := strings.ToLower(strings.TrimSpace(os.Getenv("KAFMIRROR_DISABLE_IDEMPOTENT_WRITES")))
		if v == "true" || v == "1" || v == "yes" {
			disableIdempotent = true
		}
	}
	if disableIdempotent {
		opts = append(opts, kgo.DisableIdempotentWrite())
		logger.Info("Producer: idempotent writes DISABLED (OPS-004 workaround)")
	} else {
		logger.Info("Producer: Using idempotent writes (franz-go default)")
	}

	logger.Info("Producer: Establishing connection to Kafka brokers, job=%s, component=%s", jobID, "producer")
	client, err := newClient(cfg, opts...)
	if err != nil {
		logger.ErrorAI("disaster", "connection", jobID, "Producer: Failed to create Kafka client: %v", err)
		return nil, err
	}

	producer := &Producer{
		Client: client,
		jobID:  jobID,
	}

	logger.Info("Producer initialized for job %s, component=%s", jobID, "producer")

	logger.Info("Producer: Successfully created Kafka client, job=%s, component=%s", jobID, "producer")
	return producer, nil
}

func getCompressionCodec(compression string) kgo.CompressionCodec {
	switch strings.ToLower(compression) {
	case "gzip":
		return kgo.GzipCompression()
	case "snappy":
		return kgo.SnappyCompression()
	case "lz4":
		return kgo.Lz4Compression()
	case "zstd":
		return kgo.ZstdCompression()
	default:
		return kgo.NoCompression()
	}
}

// ProduceSync sends a record and waits for the produce ack.
func (p *Producer) ProduceSync(ctx context.Context, record *kgo.Record) error {
	done := make(chan error, 1)
	p.Produce(ctx, record, func(_ *kgo.Record, err error) {
		done <- err
	})
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Produce sends a record to the target Kafka cluster.
func (p *Producer) Produce(ctx context.Context, record *kgo.Record, callback func(*kgo.Record, error)) {
	// Track bytes before sending
	recordBytes := int64(len(record.Value) + len(record.Key))

	logger.Debug("Producing message to topic %s, key=%s, value_size=%d, job=%s, component=%s",
		record.Topic, string(record.Key), len(record.Value), p.jobID, "producer")

	// Use async produce with proper metrics tracking
	p.Client.Produce(ctx, record, func(rec *kgo.Record, err error) {
		// Track metrics based on result
		if err != nil {
			atomic.AddInt64(&p.errorCount, 1)
			atomic.AddInt64(&p.consecutiveErrors, 1)
			logger.ErrorAI("producer", "error", p.jobID, "Failed to produce message to topic %s: %v", rec.Topic, err)
		} else {
			atomic.StoreInt64(&p.consecutiveErrors, 0)
			atomic.AddInt64(&p.recordsProduced, 1)
			atomic.AddInt64(&p.bytesProduced, recordBytes)
			logger.Debug("Successfully produced message to topic %s, partition %d, offset %d, job=%s, component=%s",
				rec.Topic, rec.Partition, rec.Offset, p.jobID, "producer")
		}

		// Call the original callback
		if callback != nil {
			callback(rec, err)
		}
	})
}

// logOperation logs producer operations for AI analysis
func (p *Producer) logOperation(level, operation, message string, metadata map[string]interface{}) {
	// This function is now deprecated in favor of tagged logging.
}

// GetMetrics returns current producer performance metrics
func (p *Producer) GetMetrics() ProducerMetrics {
	return ProducerMetrics{
		RecordsProduced:   atomic.LoadInt64(&p.recordsProduced),
		BytesProduced:     atomic.LoadInt64(&p.bytesProduced),
		ErrorCount:        atomic.LoadInt64(&p.errorCount),
		ConsecutiveErrors: atomic.LoadInt64(&p.consecutiveErrors),
	}
}

// Close flushes any buffered records and closes the producer.
func (p *Producer) Close() {
	logger.Info("Producer is shutting down, job=%s, component=%s", p.jobID, "producer")
	p.Client.Close()
}
