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

func consumeUntil(t *testing.T, consumer *kafka.Consumer, want int) {
	t.Helper()
	got := make(chan struct{}, want)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- consumer.Consume(ctx, func(context.Context, *kgo.Record) error {
			got <- struct{}{}
			return nil
		})
	}()
	for i := 0; i < want; i++ {
		select {
		case <-got:
		case err := <-done:
			t.Fatalf("the consumer stopped after %d of %d records: %v", i, want, err)
		case <-time.After(10 * time.Second):
			t.Fatalf("got %d of %d records", i, want)
		}
	}
	cancel()
	require.NoError(t, <-done)
}

func TestUnknownTopicIDBacksOffAndKeepsConsuming(t *testing.T) {
	script := &pollScript{next: func(n int) kgo.Fetches {
		switch {
		case n <= 12:
			return fetchError("kin", kerr.UnknownTopicID)
		case n == 13:
			return fetchOf("kin", partitionRecords{0, []int64{0}})
		default:
			return nil
		}
	}}
	sleeps := &sleepLog{}
	consumer := scriptedConsumer(script, sleeps)

	consumeUntil(t, consumer, 1)

	assert.Equal(t, int64(12), consumer.GetMetrics().FetchErrors)
	assert.Equal(t, int64(0), consumer.GetMetrics().TopicRecreations)
	ms := time.Millisecond
	assert.Equal(t, []time.Duration{100 * ms, 200 * ms, 400 * ms, 800 * ms, 1600 * ms, 3200 * ms, 5000 * ms, 5000 * ms, 5000 * ms, 5000 * ms, 5000 * ms, 5000 * ms}, sleeps.get())
}

func TestAnySuccessfulFetchResetsTheErrorBackoff(t *testing.T) {
	script := &pollScript{next: func(n int) kgo.Fetches {
		switch {
		case n <= 9, n >= 11 && n <= 19:
			return fetchError("kin", kerr.UnknownTopicID)
		case n == 10:
			return fetchOf("kin", partitionRecords{0, nil})
		case n == 20:
			return fetchOf("kin", partitionRecords{0, []int64{0}})
		default:
			return nil
		}
	}}
	sleeps := &sleepLog{}
	consumer := scriptedConsumer(script, sleeps)

	consumeUntil(t, consumer, 1)

	assert.Equal(t, int64(18), consumer.GetMetrics().FetchErrors)
	ms := time.Millisecond
	climb := []time.Duration{100 * ms, 200 * ms, 400 * ms, 800 * ms, 1600 * ms, 3200 * ms, 5000 * ms, 5000 * ms, 5000 * ms}
	assert.Equal(t, append(append([]time.Duration(nil), climb...), climb...), sleeps.get(), "the empty fetch without errors starts the backoff again at 100ms")
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

func TestUnknownTopicIDKeepsTheJobRunningAndCountsTheErrors(t *testing.T) {
	km := kafka.NewKafMirrorImplForTest(&kafka.Producer{Client: &mocks.MockKgoClient{}}, map[string]string{"kin": "kin"})
	script := &pollScript{next: func(n int) kgo.Fetches {
		if n > 3 {
			return nil
		}
		return fetchError("kin", kerr.UnknownTopicID)
	}}
	km.Consumer = scriptedConsumer(script, &sleepLog{})
	km.SetIntervalsForTest(time.Millisecond, 0)

	counted := make(chan database.ReplicationMetric, 1)
	failed := make(chan string, 1)
	km.Start("job-87", func(m database.ReplicationMetric) {
		if m.ErrorCount == 3 {
			select {
			case counted <- m:
			default:
			}
		}
	}, func(_ string, reason string) {
		failed <- reason
	})
	defer km.Stop()

	select {
	case m := <-counted:
		assert.Equal(t, 3, m.FetchErrorCount)
	case reason := <-failed:
		t.Fatalf("the job failed: %s", reason)
	case <-time.After(5 * time.Second):
		t.Fatal("no metric counted the fetch errors")
	}
	select {
	case reason := <-failed:
		t.Fatalf("the job failed: %s", reason)
	default:
	}
}
