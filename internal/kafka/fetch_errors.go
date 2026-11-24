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
	"sync/atomic"
	"time"

	"kaf-mirror/pkg/logger"

	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kgo"
)

const (
	fetchErrorBackoffMin = 100 * time.Millisecond
	fetchErrorBackoffMax = 5 * time.Second
	fetchErrorLogEvery   = 30 * time.Second
	UnknownTopicIDLimit  = 10
)

var ErrSourceTopicRecreated = errors.New("source topic was deleted or recreated")

type fetchErrorKey struct {
	topic     string
	partition int32
	err       string
}

type fetchErrorLog struct {
	last       time.Time
	suppressed int64
}

func (c *Consumer) handleFetchErrors(ctx context.Context, fetches kgo.Fetches) error {
	if c.unknownTopicIDs == nil {
		c.unknownTopicIDs = make(map[string]int)
	}
	fetches.EachPartition(func(p kgo.FetchTopicPartition) {
		if len(p.Records) > 0 {
			delete(c.unknownTopicIDs, p.Topic)
		}
	})

	errorCount := 0
	unknown := make(map[string]bool)
	fetches.EachError(func(topic string, partition int32, err error) {
		errorCount++
		atomic.AddInt64(&c.fetchErrors, 1)
		c.logFetchError(topic, partition, err)
		if errors.Is(err, kerr.UnknownTopicID) {
			unknown[topic] = true
		}
	})
	for topic := range unknown {
		c.unknownTopicIDs[topic]++
		if n := c.unknownTopicIDs[topic]; n >= UnknownTopicIDLimit {
			return fmt.Errorf("%w: fetches for topic %s answered UNKNOWN_TOPIC_ID %d times with no record in between. Check the topic, then start the job again", ErrSourceTopicRecreated, topic, n)
		}
	}

	if errorCount == 0 || fetches.NumRecords() > 0 {
		c.fetchBackoff = 0
		return nil
	}
	c.fetchBackoff = min(max(c.fetchBackoff*2, fetchErrorBackoffMin), fetchErrorBackoffMax)
	sleep := c.sleep
	if sleep == nil {
		sleep = sleepContext
	}
	_ = sleep(ctx, c.fetchBackoff)
	return nil
}

func (c *Consumer) logFetchError(topic string, partition int32, err error) {
	now := time.Now
	if c.now != nil {
		now = c.now
	}
	if c.fetchErrorLogs == nil {
		c.fetchErrorLogs = make(map[fetchErrorKey]*fetchErrorLog)
	}
	key := fetchErrorKey{topic: topic, partition: partition, err: err.Error()}
	entry, ok := c.fetchErrorLogs[key]
	t := now()
	if ok && t.Sub(entry.last) < fetchErrorLogEvery {
		entry.suppressed++
		return
	}
	if !ok {
		entry = &fetchErrorLog{}
		c.fetchErrorLogs[key] = entry
	}
	if entry.suppressed > 0 {
		logger.ErrorAI("disaster", "replication", c.jobID, "Consumer: Fetch error for topic %s, partition %d: %v (%d more since the last log line)", topic, partition, err, entry.suppressed)
	} else {
		logger.ErrorAI("disaster", "replication", c.jobID, "Consumer: Fetch error for topic %s, partition %d: %v", topic, partition, err)
	}
	entry.last = t
	entry.suppressed = 0
}

func (c *Consumer) SetClockForTest(now func() time.Time) {
	c.now = now
}
