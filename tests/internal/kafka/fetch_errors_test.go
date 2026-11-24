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

package kafka_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"kaf-mirror/internal/database"
	"kaf-mirror/internal/kafka"
	"kaf-mirror/pkg/logger"
	"kaf-mirror/tests/mocks"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kgo"
)

func fetchError(topic string, err error) kgo.Fetches {
	return kgo.Fetches{{Topics: []kgo.FetchTopic{{Topic: topic, Partitions: []kgo.FetchPartition{{Partition: 0, Err: err}}}}}}
}

type pollScript struct {
	mu    sync.Mutex
	polls int
	next  func(n int) kgo.Fetches
}

func (p *pollScript) poll(ctx context.Context) kgo.Fetches {
	p.mu.Lock()
	p.polls++
	n := p.polls
	p.mu.Unlock()
	if f := p.next(n); f != nil {
		return f
	}
	<-ctx.Done()
	return kgo.Fetches{}
}

func (p *pollScript) count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.polls
}

type sleepLog struct {
	mu    sync.Mutex
	waits []time.Duration
}

func (s *sleepLog) sleep(ctx context.Context, d time.Duration) error {
	s.mu.Lock()
	s.waits = append(s.waits, d)
	s.mu.Unlock()
	return ctx.Err()
}

func (s *sleepLog) get() []time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]time.Duration(nil), s.waits...)
}

func scriptedConsumer(script *pollScript, sleeps *sleepLog) *kafka.Consumer {
	consumer := kafka.NewConsumerForTest(nil, nil)
	consumer.Client = &mocks.MockKgoClient{PollFetchesFunc: script.poll}
	consumer.SetSleepForTest(sleeps.sleep)
	return consumer
}

func TestRecreatedSourceTopicStopsTheConsumerWithBackoff(t *testing.T) {
	script := &pollScript{next: func(int) kgo.Fetches { return fetchError("kin", kerr.UnknownTopicID) }}
	sleeps := &sleepLog{}
	consumer := scriptedConsumer(script, sleeps)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err := consumer.Consume(ctx, func(context.Context, *kgo.Record) error { return nil })

	require.Error(t, err)
	assert.True(t, errors.Is(err, kafka.ErrSourceTopicRecreated), "error: %v", err)
	assert.Contains(t, err.Error(), "topic kin")
	assert.Equal(t, kafka.UnknownTopicIDLimit, script.count())
	assert.Equal(t, int64(kafka.UnknownTopicIDLimit), consumer.GetMetrics().FetchErrors)
	ms := time.Millisecond
	assert.Equal(t, []time.Duration{100 * ms, 200 * ms, 400 * ms, 800 * ms, 1600 * ms, 3200 * ms, 5000 * ms, 5000 * ms, 5000 * ms}, sleeps.get())
}

func TestRecordsOfTheTopicResetTheUnknownTopicIDCount(t *testing.T) {
	limit := kafka.UnknownTopicIDLimit
	script := &pollScript{next: func(n int) kgo.Fetches {
		switch {
		case n == limit:
			return fetchOf("kin", partitionRecords{0, []int64{0}})
		case n < 2*limit:
			return fetchError("kin", kerr.UnknownTopicID)
		default:
			return nil
		}
	}}
	sleeps := &sleepLog{}
	consumer := scriptedConsumer(script, sleeps)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- consumer.Consume(ctx, func(context.Context, *kgo.Record) error { return nil }) }()

	require.Eventually(t, func() bool { return script.count() > 2*limit-1 }, 5*time.Second, time.Millisecond)
	cancel()
	require.NoError(t, <-done)
	assert.Equal(t, int64(2*limit-2), consumer.GetMetrics().FetchErrors)
	assert.Equal(t, 100*time.Millisecond, sleeps.get()[limit-1], "a poll with records resets the backoff")
}

func TestFetchErrorsAreRateLimitedInTheLog(t *testing.T) {
	var out bytes.Buffer
	var outMu sync.Mutex
	logger.SetOutput(writerFunc(func(p []byte) (int, error) {
		outMu.Lock()
		defer outMu.Unlock()
		return out.Write(p)
	}))
	t.Cleanup(func() { logger.SetOutput(io.Discard) })

	now := time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)
	var clockMu sync.Mutex
	script := &pollScript{next: func(n int) kgo.Fetches {
		if n > 101 {
			return nil
		}
		if n == 101 {
			clockMu.Lock()
			now = now.Add(31 * time.Second)
			clockMu.Unlock()
		}
		return fetchError("kin", kerr.TopicAuthorizationFailed)
	}}
	consumer := scriptedConsumer(script, &sleepLog{})
	consumer.SetClockForTest(func() time.Time {
		clockMu.Lock()
		defer clockMu.Unlock()
		return now
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- consumer.Consume(ctx, func(context.Context, *kgo.Record) error { return nil }) }()
	require.Eventually(t, func() bool { return script.count() > 101 }, 5*time.Second, time.Millisecond)
	cancel()
	require.NoError(t, <-done)

	outMu.Lock()
	text := out.String()
	outMu.Unlock()
	var lines []string
	for _, line := range strings.Split(text, "\n") {
		if strings.Contains(line, "Fetch error for topic kin") {
			lines = append(lines, line)
		}
	}
	require.Len(t, lines, 2, "log: %s", text)
	assert.Contains(t, lines[1], "(99 more since the last log line)")
	assert.Equal(t, int64(101), consumer.GetMetrics().FetchErrors)
}

type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) { return f(p) }

func TestRecreatedSourceTopicFailsTheJobAndCountsTheErrors(t *testing.T) {
	km := kafka.NewKafMirrorImplForTest(&kafka.Producer{Client: &mocks.MockKgoClient{}}, map[string]string{"kin": "kin"})
	gate := make(chan struct{})
	script := &pollScript{next: func(n int) kgo.Fetches {
		if n == 2 {
			<-gate
		}
		return fetchError("kin", kerr.UnknownTopicID)
	}}
	km.Consumer = scriptedConsumer(script, &sleepLog{})
	km.SetIntervalsForTest(time.Millisecond, 0)

	var once sync.Once
	var counted database.ReplicationMetric
	reason := startAndWaitForFailure(t, km, func(m database.ReplicationMetric) {
		if m.ErrorCount > 0 {
			once.Do(func() {
				counted = m
				close(gate)
			})
		}
	})

	assert.Contains(t, reason, "consumer stopped: source topic was deleted or recreated")
	assert.Contains(t, reason, "UNKNOWN_TOPIC_ID")
	assert.Equal(t, 1, counted.ErrorCount)
	assert.True(t, counted.ErrorSpike)
}
