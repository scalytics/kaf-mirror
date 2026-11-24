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
	"fmt"
	"kaf-mirror/internal/config"
	"kaf-mirror/pkg/logger"
	"sync"
	"sync/atomic"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"
)

type ConsumerMetrics struct {
	RecordsProcessed  int64
	BytesProcessed    int64
	RecordsReplicated int64
	BytesReplicated   int64
	ConsumerLag       int64
	Tombstones        int64
	FetchErrors       int64
	TopicRecreations  int64
	MissingTopics     []string
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
	recordsAcked     int64
	bytesAcked       int64
	tombstones       int64
	jobID            string
	mu               sync.RWMutex
	highWaterMarks   map[string]map[int32]int64
	lastOffsets      map[string]map[int32]int64
	produceAttempts  int
	produceBackoff   time.Duration
	partMu           sync.Mutex
	partCancels      map[topicPartition]context.CancelFunc
	revoked          map[topicPartition]bool
	sleep            func(context.Context, time.Duration) error
	now              func() time.Time
	fetchErrors      int64
	fetchBackoff     time.Duration
	fetchErrorLogs   map[fetchErrorKey]*fetchErrorLog
	topicIDs         topicIDState
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
		kgo.ConsumerGroup(groupID),
		kgo.ConsumeTopics(topics...),
		kgo.FetchMaxBytes(int32(replicationCfg.BatchSize * 1024)),
		kgo.DisableAutoCommit(),
		kgo.OnPartitionsAssigned(func(ctx context.Context, c *kgo.Client, assigned map[string][]int32) {
			logger.Info("Consumer partitions assigned: %v, job=%s, component=%s", assigned, jobID, "consumer")
			consumer.AssignPartitions(assigned)
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

	logger.Debug("Consumer configuration: batch_size=%dKB, job=%s, component=%s",
		replicationCfg.BatchSize, jobID, "consumer")

	logger.Info("Consumer: Establishing connection to Kafka brokers, job=%s, component=%s", jobID, "consumer")
	client, err := newClient(cfg, opts...)
	if err != nil {
		logger.ErrorAI("disaster", "connection", jobID, "Consumer: Failed to create Kafka client: %v", err)
		return nil, err
	}

	consumer.Client = client

	logger.Info("Consumer: Successfully created Kafka client, job=%s, component=%s", jobID, "consumer")
	return consumer, nil
}

type AsyncHandler func(ctx context.Context, record *kgo.Record, attempt int, done func(error))

func SyncHandler(handler func(context.Context, *kgo.Record) error) AsyncHandler {
	return func(ctx context.Context, record *kgo.Record, _ int, done func(error)) {
		done(handler(ctx, record))
	}
}

const shutdownCommitTimeout = 5 * time.Second

const (
	recordPending = iota
	recordAcked
	recordFailed
)

type partitionBatch struct {
	tp            topicPartition
	ctx           context.Context
	records       []*kgo.Record
	next          int
	status        []int
	sends         []int
	failures      []int
	acked         []bool
	errs          []error
	failedAt      int
	inflight      int
	roundFailures int
	lastFailed    int
	isolateUntil  int
}

type batchRound struct {
	mu       sync.Mutex
	pending  int
	finished bool
	done     chan struct{}
}

func (b *batchRound) release(n int) {
	b.pending -= n
	if b.pending == 0 && !b.finished {
		b.finished = true
		close(b.done)
	}
}

func (c *Consumer) Consume(ctx context.Context, handler func(context.Context, *kgo.Record) error) error {
	return c.ConsumeAsync(ctx, SyncHandler(handler))
}

func (c *Consumer) ConsumeAsync(ctx context.Context, handler AsyncHandler) error {
	watchCtx, stopWatch := context.WithCancel(ctx)
	defer stopWatch()
	go c.watchTopicIDs(watchCtx)
	for {
		if ctx.Err() != nil {
			return nil
		}
		fetches := c.Client.PollFetches(ctx)
		if fetches.IsClientClosed() || ctx.Err() != nil {
			return nil
		}
		c.handleFetchErrors(ctx, fetches)

		partCtxs := c.trackPartitions(ctx, fetches)
		var batches []*partitionBatch
		fetches.EachPartition(func(partition kgo.FetchTopicPartition) {
			c.setHighWaterMark(partition.Topic, partition.Partition, partition.HighWatermark)
			if len(partition.Records) == 0 {
				return
			}
			tp := topicPartition{topic: partition.Topic, partition: partition.Partition}
			batches = append(batches, &partitionBatch{
				tp:           tp,
				ctx:          partCtxs[tp],
				records:      partition.Records,
				status:       make([]int, len(partition.Records)),
				sends:        make([]int, len(partition.Records)),
				failures:     make([]int, len(partition.Records)),
				acked:        make([]bool, len(partition.Records)),
				errs:         make([]error, len(partition.Records)),
				failedAt:     -1,
				isolateUntil: -1,
			})
		})
		err := c.consumeBatches(ctx, batches, handler)
		c.releasePartitions(partCtxs)

		if ctx.Err() != nil {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

func (c *Consumer) consumeBatches(ctx context.Context, batches []*partitionBatch, handler AsyncHandler) error {
	attempts := max(c.produceAttempts, 1)
	for {
		active := c.activeBatches(batches)
		if len(active) == 0 {
			return nil
		}
		c.produceRound(ctx, active, handler)
		c.commitAcked(ctx, active)
		if ctx.Err() != nil {
			return nil
		}

		var failing []*partitionBatch
		var wait time.Duration
		for _, b := range active {
			if b.failedAt < 0 || b.ctx.Err() != nil {
				continue
			}
			record := b.records[b.failedAt]
			failures := b.failures[b.failedAt]
			if b.roundFailures > 1 {
				b.isolateUntil = max(b.isolateUntil, b.lastFailed)
				logger.Warn("Consumer: %d records of topic %s partition %d failed together, sending them one at a time from offset %d, job=%s", b.roundFailures, record.Topic, record.Partition, record.Offset, c.jobID)
			}
			if failures >= attempts {
				logger.Error("Consumer: handler failed for topic %s partition %d offset %d after %d attempts, job=%s", record.Topic, record.Partition, record.Offset, attempts, c.jobID)
				return fmt.Errorf("produce failed for topic %s partition %d offset %d after %d attempts: %w", record.Topic, record.Partition, record.Offset, attempts, b.errs[b.failedAt])
			}
			backoff := c.produceBackoff << (failures - 1)
			logger.Warn("Consumer: produce attempt %d of %d failed for topic %s partition %d offset %d, retrying in %s: %v", failures, attempts, record.Topic, record.Partition, record.Offset, backoff, b.errs[b.failedAt])
			wait = max(wait, backoff)
			failing = append(failing, b)
		}
		if len(failing) == 0 {
			continue
		}
		if err := c.backoff(ctx, failing, wait); err != nil && ctx.Err() != nil {
			return nil
		}
	}
}

func (c *Consumer) activeBatches(batches []*partitionBatch) []*partitionBatch {
	var active []*partitionBatch
	for _, b := range batches {
		if b.next >= len(b.records) {
			continue
		}
		if b.ctx.Err() != nil {
			if b.failedAt >= 0 {
				record := b.records[b.failedAt]
				logger.Warn("Consumer: partition revoked, stopped at topic %s partition %d offset %d without commit, job=%s", record.Topic, record.Partition, record.Offset, c.jobID)
			}
			b.next = len(b.records)
			continue
		}
		active = append(active, b)
	}
	return active
}

func (c *Consumer) produceRound(ctx context.Context, batches []*partitionBatch, handler AsyncHandler) {
	round := &batchRound{pending: 1, done: make(chan struct{})}
	var stops []func() bool
	for _, b := range batches {
		for i := b.next; i < len(b.records); i++ {
			b.status[i] = recordPending
		}
		b.failedAt = -1
		b.inflight = 0
		b.roundFailures = 0
		b.lastFailed = -1
		stops = append(stops, context.AfterFunc(b.ctx, func() {
			round.mu.Lock()
			defer round.mu.Unlock()
			if round.finished {
				return
			}
			n := b.inflight
			b.inflight = 0
			round.release(n)
		}))
	}

	for _, b := range batches {
		if ctx.Err() != nil {
			break
		}
		for i := b.next; i < len(b.records); i++ {
			if ctx.Err() != nil || b.ctx.Err() != nil {
				break
			}
			if i > b.next && b.next <= b.isolateUntil {
				break
			}
			round.mu.Lock()
			stopped := b.failedAt >= 0 || round.finished || b.ctx.Err() != nil
			if !stopped {
				round.pending++
				b.inflight++
			}
			round.mu.Unlock()
			if stopped {
				break
			}
			record := b.records[i]
			c.setLastOffset(record.Topic, record.Partition, record.Offset)
			b.sends[i]++
			handler(b.ctx, record, b.sends[i], c.recordDone(round, b, i))
		}
	}

	round.mu.Lock()
	round.release(1)
	round.mu.Unlock()
	select {
	case <-round.done:
	case <-ctx.Done():
	}
	round.mu.Lock()
	round.finished = true
	round.mu.Unlock()
	for _, stop := range stops {
		stop()
	}
}

func (c *Consumer) recordDone(round *batchRound, b *partitionBatch, i int) func(error) {
	var once sync.Once
	return func(err error) {
		once.Do(func() {
			round.mu.Lock()
			defer round.mu.Unlock()
			if round.finished || b.ctx.Err() != nil {
				return
			}
			if err == nil {
				b.status[i] = recordAcked
				if !b.acked[i] {
					b.acked[i] = true
					record := b.records[i]
					atomic.AddInt64(&c.recordsAcked, 1)
					atomic.AddInt64(&c.bytesAcked, int64(len(record.Value)+len(record.Key)))
				}
			} else {
				b.status[i] = recordFailed
				b.failures[i]++
				b.errs[i] = err
				b.roundFailures++
				b.lastFailed = max(b.lastFailed, i)
				if b.failedAt < 0 || i < b.failedAt {
					b.failedAt = i
				}
			}
			b.inflight--
			round.release(1)
		})
	}
}

func (c *Consumer) commitAcked(ctx context.Context, batches []*partitionBatch) {
	var commits []*kgo.Record
	for _, b := range batches {
		if c.isRevoked(b.tp) {
			continue
		}
		start := b.next
		for b.next < len(b.records) && b.status[b.next] == recordAcked {
			record := b.records[b.next]
			atomic.AddInt64(&c.recordsProcessed, 1)
			atomic.AddInt64(&c.bytesProcessed, int64(len(record.Value)+len(record.Key)))
			if len(record.Value) == 0 {
				atomic.AddInt64(&c.tombstones, 1)
			}
			b.next++
		}
		if b.next > start {
			commits = append(commits, b.records[b.next-1])
		}
	}
	if len(commits) == 0 {
		return
	}
	commitCtx := ctx
	if ctx.Err() != nil {
		var cancel context.CancelFunc
		commitCtx, cancel = context.WithTimeout(context.WithoutCancel(ctx), shutdownCommitTimeout)
		defer cancel()
	}
	if err := c.Client.CommitRecords(commitCtx, commits...); err != nil {
		logger.Error("Consumer: failed to commit %d partition offsets: %v, job=%s", len(commits), err, c.jobID)
	}
}

func (c *Consumer) backoff(ctx context.Context, failing []*partitionBatch, wait time.Duration) error {
	sleepCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var remaining atomic.Int32
	remaining.Store(int32(len(failing)))
	for _, b := range failing {
		stop := context.AfterFunc(b.ctx, func() {
			if remaining.Add(-1) == 0 {
				cancel()
			}
		})
		defer stop()
	}
	sleep := c.sleep
	if sleep == nil {
		sleep = sleepContext
	}
	return sleep(sleepCtx, wait)
}

func (c *Consumer) isRevoked(tp topicPartition) bool {
	c.partMu.Lock()
	defer c.partMu.Unlock()
	return c.revoked[tp]
}

func sleepContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
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
		if c.revoked[tp] {
			cancel()
		}
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
	if c.revoked == nil {
		c.revoked = make(map[topicPartition]bool)
	}
	for topic, partitions := range revoked {
		for _, partition := range partitions {
			tp := topicPartition{topic: topic, partition: partition}
			c.revoked[tp] = true
			if cancel, ok := c.partCancels[tp]; ok {
				cancel()
			}
		}
	}
}

func (c *Consumer) AssignPartitions(assigned map[string][]int32) {
	c.partMu.Lock()
	defer c.partMu.Unlock()
	for topic, partitions := range assigned {
		for _, partition := range partitions {
			delete(c.revoked, topicPartition{topic: topic, partition: partition})
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
		RecordsProcessed:  atomic.LoadInt64(&c.recordsProcessed),
		BytesProcessed:    atomic.LoadInt64(&c.bytesProcessed),
		RecordsReplicated: atomic.LoadInt64(&c.recordsAcked),
		BytesReplicated:   atomic.LoadInt64(&c.bytesAcked),
		ConsumerLag:       calculatedLag,
		Tombstones:        atomic.LoadInt64(&c.tombstones),
		FetchErrors:       atomic.LoadInt64(&c.fetchErrors),
		TopicRecreations:  atomic.LoadInt64(&c.topicIDs.recreations),
		MissingTopics:     c.missingTopics(),
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

func (c *Consumer) SetSleepForTest(sleep func(context.Context, time.Duration) error) {
	c.sleep = sleep
}

func (c *Consumer) ProduceRetryForTest() (int, time.Duration) {
	return c.produceAttempts, c.produceBackoff
}
