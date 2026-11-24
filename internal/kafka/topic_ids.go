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
	"encoding/base64"
	"errors"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"kaf-mirror/pkg/logger"

	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/kmsg"
)

const (
	topicIDCheckEvery = 10 * time.Second
	topicRejoinWait   = 30 * time.Second
)

type topicID [16]byte

func (id topicID) String() string {
	return base64.RawURLEncoding.EncodeToString(id[:])
}

type topicIDState struct {
	mu          sync.Mutex
	known       map[string]topicID
	watched     map[string]bool
	revokeWait  chan struct{}
	revokeTopic string
	missing     map[string]time.Time
	check       chan struct{}
	every       time.Duration
	rejoinWait  time.Duration
	recreations int64
}

func (c *Consumer) topicCheckChan() chan struct{} {
	c.topicIDs.mu.Lock()
	defer c.topicIDs.mu.Unlock()
	if c.topicIDs.check == nil {
		c.topicIDs.check = make(chan struct{}, 1)
	}
	return c.topicIDs.check
}

func (c *Consumer) requestTopicCheck() {
	select {
	case c.topicCheckChan() <- struct{}{}:
	default:
	}
}

func (c *Consumer) watchTopics(topics ...string) {
	c.topicIDs.mu.Lock()
	defer c.topicIDs.mu.Unlock()
	if c.topicIDs.watched == nil {
		c.topicIDs.watched = make(map[string]bool)
	}
	for _, topic := range topics {
		if topic != "" {
			c.topicIDs.watched[topic] = true
		}
	}
}

func (c *Consumer) SetWatchedTopicsForTest(topics ...string) {
	c.watchTopics(topics...)
}

func (c *Consumer) TopicIDForTest(topic string) (string, bool) {
	id, ok := c.knownTopicIDs()[topic]
	if !ok {
		return "", false
	}
	return id.String(), true
}

func (c *Consumer) topicFetched(topic string, id topicID) {
	var check bool
	c.topicIDs.mu.Lock()
	if _, ok := c.topicIDs.missing[topic]; ok {
		delete(c.topicIDs.missing, topic)
		logger.Info("Consumer: source topic %s is back, job=%s", topic, c.jobID)
	}
	if id != (topicID{}) {
		if existing, ok := c.topicIDs.known[topic]; ok && existing != id {
			check = true
		} else {
			if c.topicIDs.known == nil {
				c.topicIDs.known = make(map[string]topicID)
			}
			c.topicIDs.known[topic] = id
		}
	}
	c.topicIDs.mu.Unlock()
	if check {
		c.requestTopicCheck()
	}
}

func (c *Consumer) noteTopicID(topic string, id topicID) {
	if id == (topicID{}) {
		return
	}
	c.topicIDs.mu.Lock()
	defer c.topicIDs.mu.Unlock()
	if existing, ok := c.topicIDs.known[topic]; ok && existing != id {
		return
	}
	if c.topicIDs.known == nil {
		c.topicIDs.known = make(map[string]topicID)
	}
	c.topicIDs.known[topic] = id
}

func (c *Consumer) topicsToCheck() []string {
	c.topicIDs.mu.Lock()
	defer c.topicIDs.mu.Unlock()
	seen := make(map[string]bool, len(c.topicIDs.known)+len(c.topicIDs.watched))
	for topic := range c.topicIDs.known {
		seen[topic] = true
	}
	for topic := range c.topicIDs.watched {
		seen[topic] = true
	}
	topics := make([]string, 0, len(seen))
	for topic := range seen {
		topics = append(topics, topic)
	}
	sort.Strings(topics)
	return topics
}

func (c *Consumer) knownTopicIDs() map[string]topicID {
	c.topicIDs.mu.Lock()
	defer c.topicIDs.mu.Unlock()
	known := make(map[string]topicID, len(c.topicIDs.known))
	for topic, id := range c.topicIDs.known {
		known[topic] = id
	}
	return known
}

func (c *Consumer) missingTopics() []string {
	c.topicIDs.mu.Lock()
	defer c.topicIDs.mu.Unlock()
	if len(c.topicIDs.missing) == 0 {
		return nil
	}
	topics := make([]string, 0, len(c.topicIDs.missing))
	for topic := range c.topicIDs.missing {
		topics = append(topics, topic)
	}
	sort.Strings(topics)
	return topics
}

func (c *Consumer) SetTopicCheckIntervalForTest(every time.Duration) {
	c.topicIDs.mu.Lock()
	defer c.topicIDs.mu.Unlock()
	c.topicIDs.every = every
}

func (c *Consumer) SetTopicRejoinWaitForTest(wait time.Duration) {
	c.topicIDs.mu.Lock()
	defer c.topicIDs.mu.Unlock()
	c.topicIDs.rejoinWait = wait
}

func (c *Consumer) topicRejoinWait() time.Duration {
	c.topicIDs.mu.Lock()
	defer c.topicIDs.mu.Unlock()
	if c.topicIDs.rejoinWait > 0 {
		return c.topicIDs.rejoinWait
	}
	return topicRejoinWait
}

func (c *Consumer) clearRevokeWait(revoked chan struct{}) {
	c.topicIDs.mu.Lock()
	defer c.topicIDs.mu.Unlock()
	if c.topicIDs.revokeWait == revoked {
		c.topicIDs.revokeWait = nil
		c.topicIDs.revokeTopic = ""
	}
}

func (c *Consumer) watchTopicIDs(ctx context.Context) {
	c.topicIDs.mu.Lock()
	every := c.topicIDs.every
	c.topicIDs.mu.Unlock()
	if every <= 0 {
		every = topicIDCheckEvery
	}
	check := c.topicCheckChan()
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	c.checkTopicIDs(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-check:
		}
		c.checkTopicIDs(ctx)
	}
}

func (c *Consumer) checkTopicIDs(ctx context.Context) {
	topics := c.topicsToCheck()
	if len(topics) == 0 {
		return
	}
	known := c.knownTopicIDs()

	req := kmsg.NewPtrMetadataRequest()
	req.AllowAutoTopicCreation = false
	for _, topic := range topics {
		t := kmsg.NewMetadataRequestTopic()
		t.Topic = kmsg.StringPtr(topic)
		req.Topics = append(req.Topics, t)
	}
	raw, err := c.Client.Request(ctx, req)
	if err != nil {
		if ctx.Err() == nil {
			logger.Warn("Consumer: cannot read the metadata of the source topics %v: %v, job=%s", topics, err, c.jobID)
		}
		return
	}
	resp, ok := raw.(*kmsg.MetadataResponse)
	if !ok {
		return
	}
	for _, t := range resp.Topics {
		if t.Topic == nil {
			continue
		}
		topic := *t.Topic
		old, hadID := known[topic]
		err := kerr.ErrorForCode(t.ErrorCode)
		switch {
		case errors.Is(err, kerr.UnknownTopicOrPartition):
			if hadID {
				c.topicMissing(topic)
			}
		case err != nil:
			continue
		case topicID(t.TopicID) == (topicID{}):
			continue
		case !hadID:
			c.noteTopicID(topic, topicID(t.TopicID))
		case topicID(t.TopicID) == old:
			continue
		default:
			c.switchTopic(ctx, topic, old, topicID(t.TopicID), t.Partitions)
		}
	}
}

func (c *Consumer) topicMissing(topic string) {
	atomic.AddInt64(&c.fetchErrors, 1)
	now := time.Now
	if c.now != nil {
		now = c.now
	}
	t := now()
	c.topicIDs.mu.Lock()
	defer c.topicIDs.mu.Unlock()
	if c.topicIDs.missing == nil {
		c.topicIDs.missing = make(map[string]time.Time)
	}
	last, ok := c.topicIDs.missing[topic]
	if ok && t.Sub(last) < fetchErrorLogEvery {
		return
	}
	c.topicIDs.missing[topic] = t
	logger.ErrorAI("disaster", "replication", c.jobID, "Consumer: source topic %s does not exist. kaf-mirror keeps checking and switches to the topic when it is created again", topic)
}

func (c *Consumer) switchTopic(ctx context.Context, topic string, old, id topicID, partitions []kmsg.MetadataResponseTopicPartition) {
	starts, err := c.earliestOffsets(ctx, topic, partitions)
	if err != nil {
		if ctx.Err() == nil {
			logger.Warn("Consumer: source topic %s has the new topic ID %s, but its start offsets cannot be read: %v. kaf-mirror tries again, job=%s", topic, id, err, c.jobID)
		}
		return
	}
	c.topicCommitMu.Lock()
	c.topicIDs.mu.Lock()
	if current, ok := c.topicIDs.known[topic]; ok && current == id {
		c.topicIDs.mu.Unlock()
		c.topicCommitMu.Unlock()
		return
	}
	c.topicIDs.mu.Unlock()
	if len(starts) > 0 {
		if err := c.Client.CommitRecords(ctx, starts...); err != nil {
			c.topicCommitMu.Unlock()
			if ctx.Err() == nil {
				logger.Warn("Consumer: source topic %s has the new topic ID %s, but the start offsets cannot be committed: %v. kaf-mirror tries again, job=%s", topic, id, err, c.jobID)
			}
			return
		}
	}
	revoked := make(chan struct{})
	c.topicIDs.mu.Lock()
	if c.topicIDs.known == nil {
		c.topicIDs.known = make(map[string]topicID)
	}
	c.topicIDs.known[topic] = id
	delete(c.topicIDs.missing, topic)
	c.topicIDs.revokeWait = revoked
	c.topicIDs.revokeTopic = topic
	c.topicIDs.mu.Unlock()
	c.mu.Lock()
	delete(c.highWaterMarks, topic)
	delete(c.lastOffsets, topic)
	c.mu.Unlock()
	c.topicCommitMu.Unlock()

	c.Client.PurgeTopicsFromConsuming(topic)
	logger.Info("Consumer: waiting for the consumer group to drop topic %s before it reads the topic again, job=%s", topic, c.jobID)
	wait := c.topicRejoinWait()
	timer := time.NewTimer(wait)
	select {
	case <-revoked:
	case <-timer.C:
		logger.Warn("Consumer: the consumer group did not end its session within %s after topic %s was dropped, adding the topic again anyway, job=%s", wait, topic, c.jobID)
	case <-ctx.Done():
		timer.Stop()
		c.clearRevokeWait(revoked)
		return
	}
	timer.Stop()
	c.clearRevokeWait(revoked)
	if ctx.Err() != nil {
		return
	}
	c.Client.AddConsumeTopics(topic)
	atomic.AddInt64(&c.topicIDs.recreations, 1)

	logger.ErrorAI("disaster", "replication", c.jobID, "Consumer: source topic %s was recreated, topic ID %s changed to %s. kaf-mirror now copies the new topic from its earliest offsets", topic, old, id)
}

func (c *Consumer) earliestOffsets(ctx context.Context, topic string, partitions []kmsg.MetadataResponseTopicPartition) ([]*kgo.Record, error) {
	req := kmsg.NewPtrListOffsetsRequest()
	req.ReplicaID = -1
	t := kmsg.NewListOffsetsRequestTopic()
	t.Topic = topic
	for _, p := range partitions {
		rp := kmsg.NewListOffsetsRequestTopicPartition()
		rp.Partition = p.Partition
		rp.CurrentLeaderEpoch = -1
		rp.Timestamp = -2
		t.Partitions = append(t.Partitions, rp)
	}
	if len(t.Partitions) == 0 {
		return nil, nil
	}
	req.Topics = append(req.Topics, t)
	raw, err := c.Client.Request(ctx, req)
	if err != nil {
		return nil, err
	}
	resp, ok := raw.(*kmsg.ListOffsetsResponse)
	if !ok {
		return nil, fmt.Errorf("unexpected response %T", raw)
	}
	var starts []*kgo.Record
	for _, rt := range resp.Topics {
		if rt.Topic != topic {
			continue
		}
		for _, rp := range rt.Partitions {
			if err := kerr.ErrorForCode(rp.ErrorCode); err != nil {
				return nil, fmt.Errorf("partition %d: %w", rp.Partition, err)
			}
			starts = append(starts, &kgo.Record{Topic: topic, Partition: rp.Partition, LeaderEpoch: -1, Offset: rp.Offset - 1})
		}
	}
	if len(starts) != len(t.Partitions) {
		return nil, fmt.Errorf("got start offsets for %d of %d partitions", len(starts), len(t.Partitions))
	}
	return starts, nil
}

func (c *Consumer) sessionEnded(revoked map[string][]int32) {
	c.topicIDs.mu.Lock()
	defer c.topicIDs.mu.Unlock()
	if c.topicIDs.revokeWait == nil {
		return
	}
	if topic := c.topicIDs.revokeTopic; topic != "" {
		if _, ok := revoked[topic]; !ok {
			return
		}
	}
	close(c.topicIDs.revokeWait)
	c.topicIDs.revokeWait = nil
	c.topicIDs.revokeTopic = ""
}
