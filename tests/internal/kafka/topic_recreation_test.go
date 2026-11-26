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
	"encoding/base64"
	"fmt"
	"io"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"kaf-mirror/internal/kafka"
	"kaf-mirror/pkg/logger"
	"kaf-mirror/tests/mocks"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kfake"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/kmsg"
)

type recreateRig struct {
	t        *testing.T
	ctx      context.Context
	admin    *kadm.Client
	producer *kgo.Client
	consumer *kafka.Consumer
	mu       sync.Mutex
	values   []string
	logMu    sync.Mutex
	log      bytes.Buffer
	done     chan error
}

func newRecreateRig(t *testing.T) *recreateRig {
	return newRecreateRigWithAssign(t, nil)
}

func newRecreateRigWithAssign(t *testing.T, onAssigned func(context.Context, map[string][]int32)) *recreateRig {
	consumer := kafka.NewConsumerForTest(nil, nil)
	cluster, err := kfake.NewCluster(kfake.NumBrokers(1), kfake.SeedTopics(2, "kin"))
	require.NoError(t, err)
	t.Cleanup(cluster.Close)
	producer, err := kgo.NewClient(kgo.SeedBrokers(cluster.ListenAddrs()...))
	require.NoError(t, err)
	t.Cleanup(producer.Close)
	client, err := kgo.NewClient(
		kgo.SeedBrokers(cluster.ListenAddrs()...),
		kgo.ConsumerGroup("kaf-mirror-job-recreate"),
		kgo.ConsumeTopics("kin"),
		kgo.DisableAutoCommit(),
		kgo.MetadataMinAge(10*time.Millisecond),
		kgo.MetadataMaxAge(100*time.Millisecond),
		kgo.OnPartitionsAssigned(func(ctx context.Context, _ *kgo.Client, assigned map[string][]int32) {
			consumer.AssignPartitions(assigned)
			if onAssigned != nil {
				onAssigned(ctx, assigned)
			}
		}),
		kgo.OnPartitionsRevoked(func(_ context.Context, _ *kgo.Client, revoked map[string][]int32) {
			consumer.RevokePartitions(revoked)
		}),
		kgo.OnPartitionsLost(func(_ context.Context, _ *kgo.Client, lost map[string][]int32) {
			consumer.RevokePartitions(lost)
		}),
	)
	require.NoError(t, err)
	t.Cleanup(client.Close)

	r := &recreateRig{t: t, admin: kadm.NewClient(producer), producer: producer, done: make(chan error, 1)}
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("consumer log:\n%s", r.logText())
		}
	})
	logger.SetOutput(writerFunc(func(p []byte) (int, error) {
		r.logMu.Lock()
		defer r.logMu.Unlock()
		return r.log.Write(p)
	}))
	t.Cleanup(func() { logger.SetOutput(io.Discard) })

	r.consumer = consumer
	r.consumer.Client = client
	r.consumer.SetWatchedTopicsForTest("kin")
	r.consumer.SetTopicCheckIntervalForTest(20 * time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	r.ctx = ctx
	go func() {
		r.done <- r.consumer.Consume(ctx, func(_ context.Context, rec *kgo.Record) error {
			r.mu.Lock()
			r.values = append(r.values, string(rec.Value))
			r.mu.Unlock()
			return nil
		})
	}()
	t.Cleanup(func() {
		cancel()
		assert.NoError(t, <-r.done)
	})
	return r
}

func (r *recreateRig) produce(values ...string) {
	for i, v := range values {
		res := r.producer.ProduceSync(context.Background(), &kgo.Record{Topic: "kin", Partition: int32(i % 2), Value: []byte(v)})
		require.NoError(r.t, res.FirstErr())
	}
}

func (r *recreateRig) waitFor(values ...string) {
	r.t.Helper()
	require.Eventually(r.t, func() bool {
		r.mu.Lock()
		defer r.mu.Unlock()
		got := make(map[string]bool)
		for _, v := range r.values {
			got[v] = true
		}
		for _, v := range values {
			if !got[v] {
				return false
			}
		}
		return true
	}, 20*time.Second, 10*time.Millisecond, "values %v", values)
}

func (r *recreateRig) topicID() string {
	details, err := r.admin.ListTopics(context.Background(), "kin")
	require.NoError(r.t, err)
	d, ok := details["kin"]
	require.True(r.t, ok)
	require.NoError(r.t, d.Err)
	return base64.RawURLEncoding.EncodeToString(d.ID[:])
}

func (r *recreateRig) deleteTopic() {
	resp, err := r.admin.DeleteTopics(context.Background(), "kin")
	require.NoError(r.t, err)
	require.NoError(r.t, resp.Error())
}

func (r *recreateRig) createTopic() {
	resp, err := r.admin.CreateTopic(context.Background(), 2, 1, nil, "kin")
	require.NoError(r.t, err)
	require.NoError(r.t, resp.Err)
}

func (r *recreateRig) logText() string {
	r.logMu.Lock()
	defer r.logMu.Unlock()
	return r.log.String()
}

func TestRecreatedSourceTopicSwitchesToTheNewTopicID(t *testing.T) {
	r := newRecreateRig(t)
	r.produce("old-0", "old-1", "old-2", "old-3")
	r.waitFor("old-0", "old-1", "old-2", "old-3")
	oldID := r.topicID()

	r.deleteTopic()
	r.createTopic()
	r.produce("new-0", "new-1", "new-2", "new-3", "new-4", "new-5")
	newID := r.topicID()
	require.NotEqual(t, oldID, newID)

	r.waitFor("new-0", "new-1", "new-2", "new-3", "new-4", "new-5")
	metrics := r.consumer.GetMetrics()
	assert.Equal(t, int64(1), metrics.TopicRecreations)
	assert.Empty(t, metrics.MissingTopics)
	assert.Contains(t, r.logText(), "source topic kin was recreated, topic ID "+oldID+" changed to "+newID)

	r.produce("new-6")
	r.waitFor("new-6")
	assert.Equal(t, int64(1), r.consumer.GetMetrics().TopicRecreations)
}

func TestDeletedSourceTopicIsReportedUntilItIsCreatedAgain(t *testing.T) {
	r := newRecreateRig(t)
	r.produce("old-0", "old-1")
	r.waitFor("old-0", "old-1")

	r.deleteTopic()
	require.Eventually(t, func() bool {
		m := r.consumer.GetMetrics()
		return len(m.MissingTopics) == 1 && m.FetchErrors >= 3
	}, 20*time.Second, 10*time.Millisecond)
	assert.Equal(t, []string{"kin"}, r.consumer.GetMetrics().MissingTopics)
	assert.Equal(t, int64(0), r.consumer.GetMetrics().TopicRecreations)
	assert.Equal(t, 1, strings.Count(r.logText(), "source topic kin does not exist"), "the missing topic is logged once per 30 seconds")
	select {
	case err := <-r.done:
		t.Fatalf("the consumer stopped while the topic was missing: %v", err)
	default:
	}

	r.createTopic()
	r.produce("new-0", "new-1")
	r.waitFor("new-0", "new-1")
	metrics := r.consumer.GetMetrics()
	assert.Equal(t, int64(1), metrics.TopicRecreations)
	assert.Empty(t, metrics.MissingTopics)
}

func TestRecreatedSourceTopicWithNoRecordsSwitchesToTheNewTopicID(t *testing.T) {
	r := newRecreateRig(t)
	require.Eventually(t, func() bool {
		_, ok := r.consumer.TopicIDForTest("kin")
		return ok
	}, 20*time.Second, 10*time.Millisecond)
	oldID, ok := r.consumer.TopicIDForTest("kin")
	require.True(t, ok)
	assert.Equal(t, r.topicID(), oldID)

	r.deleteTopic()
	r.createTopic()
	r.produce("new-0", "new-1", "new-2", "new-3")
	newID := r.topicID()
	require.NotEqual(t, oldID, newID)

	r.waitFor("new-0", "new-1", "new-2", "new-3")
	metrics := r.consumer.GetMetrics()
	assert.Equal(t, int64(1), metrics.TopicRecreations)
	assert.Empty(t, metrics.MissingTopics)
	assert.Contains(t, r.logText(), "source topic kin was recreated, topic ID "+oldID+" changed to "+newID)
}

type metadataScript struct {
	mu        sync.Mutex
	requests  int
	answer    func(n int) (topicID [16]byte, code int16)
	purged    []string
	added     []string
	committed []*kgo.Record
}

func (m *metadataScript) request(_ context.Context, req kmsg.Request) (kmsg.Response, error) {
	if list, ok := req.(*kmsg.ListOffsetsRequest); ok {
		resp := kmsg.NewPtrListOffsetsResponse()
		for _, t := range list.Topics {
			rt := kmsg.NewListOffsetsResponseTopic()
			rt.Topic = t.Topic
			for _, p := range t.Partitions {
				rp := kmsg.NewListOffsetsResponseTopicPartition()
				rp.Partition = p.Partition
				rp.Offset = 0
				rt.Partitions = append(rt.Partitions, rp)
			}
			resp.Topics = append(resp.Topics, rt)
		}
		return resp, nil
	}
	if _, ok := req.(*kmsg.MetadataRequest); !ok {
		return nil, fmt.Errorf("unexpected request %T", req)
	}
	m.mu.Lock()
	m.requests++
	n := m.requests
	m.mu.Unlock()
	id, code := m.answer(n)
	resp := kmsg.NewPtrMetadataResponse()
	topic := kmsg.NewMetadataResponseTopic()
	topic.Topic = kmsg.StringPtr("kin")
	topic.TopicID = id
	topic.ErrorCode = code
	part := kmsg.NewMetadataResponseTopicPartition()
	part.Partition = 0
	topic.Partitions = append(topic.Partitions, part)
	resp.Topics = append(resp.Topics, topic)
	return resp, nil
}

func (m *metadataScript) count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.requests
}

func (m *metadataScript) purges() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.purged...)
}

func (m *metadataScript) adds() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.added...)
}

func scriptedTopicConsumer(script *pollScript, meta *metadataScript) *kafka.Consumer {
	return scriptedTopicConsumerRevoke(script, meta, true)
}

func scriptedTopicConsumerRevoke(script *pollScript, meta *metadataScript, autoRevoke bool) *kafka.Consumer {
	consumer := kafka.NewConsumerForTest(nil, nil)
	consumer.AssignPartitions(map[string][]int32{"kin": {0}})
	consumer.Client = &mocks.MockKgoClient{
		PollFetchesFunc: script.poll,
		RequestFunc:     meta.request,
		PurgeTopicsFunc: func(topics ...string) {
			meta.mu.Lock()
			meta.purged = append(meta.purged, topics...)
			meta.mu.Unlock()
			if !autoRevoke {
				return
			}
			lost := make(map[string][]int32, len(topics))
			for _, topic := range topics {
				lost[topic] = []int32{0}
			}
			consumer.RevokePartitions(lost)
		},
		AddConsumeTopicsFunc: func(topics ...string) {
			meta.mu.Lock()
			meta.added = append(meta.added, topics...)
			meta.mu.Unlock()
		},
		CommitRecordsFunc: func(_ context.Context, rs ...*kgo.Record) error {
			meta.mu.Lock()
			meta.committed = append(meta.committed, rs...)
			meta.mu.Unlock()
			return nil
		},
	}
	consumer.SetSleepForTest((&sleepLog{}).sleep)
	consumer.SetTopicCheckIntervalForTest(time.Hour)
	return consumer
}

func recordsWithID(id [16]byte, offset int64) kgo.Fetches {
	f := fetchOf("kin", partitionRecords{0, []int64{offset}})
	f[0].Topics[0].TopicID = id
	return f
}

var topicIDKin = [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}

func TestTransientUnknownTopicIDDuringCreationKeepsTheTopic(t *testing.T) {
	meta := &metadataScript{answer: func(int) ([16]byte, int16) { return topicIDKin, 0 }}
	gate := make(chan struct{})
	script := &pollScript{next: func(n int) kgo.Fetches {
		switch {
		case n == 1:
			return recordsWithID(topicIDKin, 0)
		case n <= 4:
			return fetchError("kin", kerr.UnknownTopicID)
		case n == 5:
			<-gate
			return recordsWithID(topicIDKin, 1)
		default:
			return nil
		}
	}}
	consumer := scriptedTopicConsumer(script, meta)
	go func() {
		for meta.count() == 0 {
			runtime.Gosched()
		}
		close(gate)
	}()

	consumeUntil(t, consumer, 2)

	assert.GreaterOrEqual(t, meta.count(), 1, "UNKNOWN_TOPIC_ID makes kaf-mirror read the topic ID")
	assert.Empty(t, meta.purges(), "the topic ID did not change, so the consumer keeps its positions")
	assert.Equal(t, int64(0), consumer.GetMetrics().TopicRecreations)
	assert.Equal(t, int64(3), consumer.GetMetrics().FetchErrors)
}

func TestSourceTopicWithoutTopicIDsIsFollowedByName(t *testing.T) {
	meta := &metadataScript{answer: func(int) ([16]byte, int16) { return [16]byte{9}, 0 }}
	script := &pollScript{next: func(n int) kgo.Fetches {
		switch {
		case n == 1:
			return recordsWithID([16]byte{}, 0)
		case n <= 4:
			return fetchError("kin", kerr.UnknownTopicID)
		case n == 5:
			return recordsWithID([16]byte{}, 1)
		default:
			return nil
		}
	}}
	consumer := scriptedTopicConsumer(script, meta)

	consumeUntil(t, consumer, 2)

	assert.Equal(t, 0, meta.count(), "without a topic ID there is nothing to compare")
	assert.Empty(t, meta.purges())
	assert.Equal(t, int64(0), consumer.GetMetrics().TopicRecreations)
}

func TestSuccessfulEmptyFetchClearsTheMissingTopic(t *testing.T) {
	meta := &metadataScript{answer: func(n int) ([16]byte, int16) {
		if n == 1 {
			return [16]byte{}, kerr.UnknownTopicOrPartition.Code
		}
		return topicIDKin, 0
	}}
	gate := make(chan struct{})
	script := &pollScript{next: func(n int) kgo.Fetches {
		switch n {
		case 1:
			return recordsWithID(topicIDKin, 0)
		case 2:
			return fetchError("kin", kerr.UnknownTopicID)
		case 3:
			<-gate
			return fetchOf("kin", partitionRecords{0, nil})
		default:
			return nil
		}
	}}
	consumer := scriptedTopicConsumer(script, meta)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- consumer.Consume(ctx, func(context.Context, *kgo.Record) error { return nil }) }()

	require.Eventually(t, func() bool {
		m := consumer.GetMetrics()
		return len(m.MissingTopics) == 1 && m.FetchErrors == 2
	}, 5*time.Second, time.Millisecond)
	assert.Equal(t, []string{"kin"}, consumer.GetMetrics().MissingTopics)
	assert.Equal(t, int64(2), consumer.GetMetrics().FetchErrors, "the fetch error and the missing topic both count")
	close(gate)
	require.Eventually(t, func() bool { return len(consumer.GetMetrics().MissingTopics) == 0 }, 5*time.Second, time.Millisecond)
	cancel()
	require.NoError(t, <-done)
	assert.Empty(t, meta.purges())
	assert.Equal(t, int64(0), consumer.GetMetrics().TopicRecreations)
}

func emptyFetchWithID(id [16]byte) kgo.Fetches {
	f := fetchOf("kin", partitionRecords{0, nil})
	f[0].Topics[0].TopicID = id
	return f
}

func TestSuccessfulEmptyFetchNotesTheTopicID(t *testing.T) {
	meta := &metadataScript{answer: func(int) ([16]byte, int16) { return topicIDKin, 0 }}
	script := &pollScript{next: func(n int) kgo.Fetches {
		if n == 1 {
			return emptyFetchWithID(topicIDKin)
		}
		return nil
	}}
	consumer := scriptedTopicConsumer(script, meta)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- consumer.Consume(ctx, func(context.Context, *kgo.Record) error { return nil }) }()

	require.Eventually(t, func() bool {
		id, ok := consumer.TopicIDForTest("kin")
		return ok && id == base64.RawURLEncoding.EncodeToString(topicIDKin[:])
	}, 5*time.Second, time.Millisecond)
	cancel()
	require.NoError(t, <-done)
	assert.Equal(t, int64(0), consumer.GetMetrics().TopicRecreations)
}

func TestMetadataAtStartNotesTheTopicID(t *testing.T) {
	meta := &metadataScript{answer: func(int) ([16]byte, int16) { return topicIDKin, 0 }}
	script := &pollScript{next: func(int) kgo.Fetches { return nil }}
	consumer := scriptedTopicConsumer(script, meta)
	consumer.SetWatchedTopicsForTest("kin")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- consumer.Consume(ctx, func(context.Context, *kgo.Record) error { return nil }) }()

	require.Eventually(t, func() bool {
		id, ok := consumer.TopicIDForTest("kin")
		return ok && id == base64.RawURLEncoding.EncodeToString(topicIDKin[:])
	}, 5*time.Second, time.Millisecond)
	cancel()
	require.NoError(t, <-done)
	assert.GreaterOrEqual(t, meta.count(), 1)
	assert.Equal(t, int64(0), consumer.GetMetrics().TopicRecreations)
	assert.Empty(t, meta.purges())
}

var topicIDNew = [16]byte{16, 15, 14, 13, 12, 11, 10, 9, 8, 7, 6, 5, 4, 3, 2, 1}

func (m *metadataScript) commits() []*kgo.Record {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]*kgo.Record(nil), m.committed...)
}

func TestFetchOfTheNewTopicBeforeTheCheckSwitchesTheTopic(t *testing.T) {
	meta := &metadataScript{answer: func(int) ([16]byte, int16) { return topicIDNew, 0 }}
	script := &pollScript{next: func(n int) kgo.Fetches {
		switch n {
		case 1:
			return recordsWithID(topicIDKin, 5)
		case 2:
			return recordsWithID(topicIDNew, 0)
		default:
			return nil
		}
	}}
	consumer := scriptedTopicConsumer(script, meta)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- consumer.Consume(ctx, func(context.Context, *kgo.Record) error { return nil }) }()

	require.Eventually(t, func() bool { return consumer.GetMetrics().TopicRecreations == 1 }, 5*time.Second, time.Millisecond)
	cancel()
	require.NoError(t, <-done)
	assert.Equal(t, []string{"kin"}, meta.purges())
	id, ok := consumer.TopicIDForTest("kin")
	require.True(t, ok)
	assert.Equal(t, base64.RawURLEncoding.EncodeToString(topicIDNew[:]), id)
	var reset bool
	for _, r := range meta.commits() {
		if r.Offset == -1 {
			reset = true
		}
	}
	assert.True(t, reset, "the earliest offset of the new topic is committed")
}

func TestLateRecordsOfTheOldTopicDoNotCommitOverTheReset(t *testing.T) {
	var blocked atomic.Bool
	meta := &metadataScript{answer: func(int) ([16]byte, int16) {
		if blocked.Load() {
			return topicIDNew, 0
		}
		return topicIDKin, 0
	}}
	script := &pollScript{next: func(n int) kgo.Fetches {
		switch n {
		case 1:
			return recordsWithID(topicIDKin, 0)
		case 2:
			return recordsWithID(topicIDKin, 7)
		default:
			return nil
		}
	}}
	consumer := scriptedTopicConsumer(script, meta)
	consumer.SetTopicCheckIntervalForTest(time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- consumer.Consume(ctx, func(_ context.Context, r *kgo.Record) error {
			if r.Offset == 7 {
				blocked.Store(true)
				for len(meta.purges()) == 0 {
					runtime.Gosched()
				}
			}
			return nil
		})
	}()

	require.Eventually(t, func() bool {
		return consumer.GetMetrics().TopicRecreations == 1 && script.count() > 2
	}, 5*time.Second, time.Millisecond)
	cancel()
	require.NoError(t, <-done)
	for _, r := range meta.commits() {
		assert.NotEqual(t, int64(7), r.Offset, "a record of the deleted topic must not commit over the reset")
	}
}

func TestSwitchDuringTheFirstRebalanceReadsTheNewTopic(t *testing.T) {
	var first sync.Once
	entered := make(chan struct{})
	var r *recreateRig
	var ready atomic.Bool
	release := func() bool {
		return r.consumer.GetMetrics().TopicRecreations == 1 || strings.Contains(r.logText(), "waiting for the consumer group to drop topic kin")
	}
	r = newRecreateRigWithAssign(t, func(ctx context.Context, assigned map[string][]int32) {
		if len(assigned["kin"]) == 0 {
			return
		}
		first.Do(func() {
			close(entered)
			for !ready.Load() || !release() {
				if ctx.Err() != nil {
					return
				}
				runtime.Gosched()
			}
		})
	})
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("consumer did not enter an assignment containing kin")
	}
	require.Eventually(t, func() bool {
		_, ok := r.consumer.TopicIDForTest("kin")
		return ok
	}, 20*time.Second, time.Millisecond)
	oldID, _ := r.consumer.TopicIDForTest("kin")

	r.deleteTopic()
	r.createTopic()
	r.produce("new-0", "new-1", "new-2", "new-3")
	require.NotEqual(t, oldID, r.topicID())
	ready.Store(true)

	r.waitFor("new-0", "new-1", "new-2", "new-3")
	assert.Equal(t, int64(1), r.consumer.GetMetrics().TopicRecreations)
}

func TestSwitchDoesNotResubscribeWhenTheJobStops(t *testing.T) {
	meta := &metadataScript{answer: func(int) ([16]byte, int16) { return topicIDNew, 0 }}
	script := &pollScript{next: func(n int) kgo.Fetches {
		switch n {
		case 1:
			return recordsWithID(topicIDKin, 5)
		case 2:
			return recordsWithID(topicIDNew, 0)
		default:
			return nil
		}
	}}
	consumer := scriptedTopicConsumerRevoke(script, meta, false)
	consumer.SetTopicRejoinWaitForTest(time.Hour)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- consumer.Consume(ctx, func(context.Context, *kgo.Record) error { return nil }) }()

	require.Eventually(t, func() bool { return len(meta.purges()) > 0 }, 5*time.Second, time.Millisecond)
	cancel()
	require.NoError(t, <-done)
	assert.Empty(t, meta.adds())
	assert.Equal(t, int64(0), consumer.GetMetrics().TopicRecreations)
}

func TestSwitchWaitsForTheDroppedTopicNotAnotherRevoke(t *testing.T) {
	meta := &metadataScript{answer: func(int) ([16]byte, int16) { return topicIDNew, 0 }}
	script := &pollScript{next: func(n int) kgo.Fetches {
		switch n {
		case 1:
			return recordsWithID(topicIDKin, 5)
		case 2:
			return recordsWithID(topicIDNew, 0)
		default:
			return nil
		}
	}}
	consumer := scriptedTopicConsumerRevoke(script, meta, false)
	consumer.SetTopicRejoinWaitForTest(time.Hour)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- consumer.Consume(ctx, func(context.Context, *kgo.Record) error { return nil }) }()

	require.Eventually(t, func() bool { return len(meta.purges()) > 0 }, 5*time.Second, time.Millisecond)
	consumer.RevokePartitions(map[string][]int32{"other": {0}})
	require.Never(t, func() bool { return len(meta.adds()) > 0 }, 50*time.Millisecond, time.Millisecond)
	consumer.RevokePartitions(map[string][]int32{"kin": {0}})
	require.Eventually(t, func() bool {
		return len(meta.adds()) > 0 && consumer.GetMetrics().TopicRecreations == 1
	}, 5*time.Second, time.Millisecond)
	cancel()
	require.NoError(t, <-done)
	assert.Equal(t, []string{"kin"}, meta.adds())
}

func TestSwitchResubscribesAfterTheRevokeWaitTimesOut(t *testing.T) {
	meta := &metadataScript{answer: func(int) ([16]byte, int16) { return topicIDNew, 0 }}
	script := &pollScript{next: func(n int) kgo.Fetches {
		switch n {
		case 1:
			return recordsWithID(topicIDKin, 5)
		case 2:
			return recordsWithID(topicIDNew, 0)
		default:
			return nil
		}
	}}
	consumer := scriptedTopicConsumerRevoke(script, meta, false)
	consumer.SetTopicRejoinWaitForTest(time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- consumer.Consume(ctx, func(context.Context, *kgo.Record) error { return nil }) }()

	require.Eventually(t, func() bool {
		return len(meta.adds()) > 0 && consumer.GetMetrics().TopicRecreations == 1
	}, 5*time.Second, time.Millisecond)
	cancel()
	require.NoError(t, <-done)
	assert.Equal(t, []string{"kin"}, meta.adds())
	assert.Equal(t, []string{"kin"}, meta.purges())
}

func TestSwitchWaitsWhenAssignmentArrivesAsFetchingStops(t *testing.T) {
	meta := &metadataScript{answer: func(int) ([16]byte, int16) { return topicIDNew, 0 }}
	script := &pollScript{next: func(n int) kgo.Fetches {
		if n == 1 {
			return emptyFetchWithID(topicIDKin)
		}
		return nil
	}}
	consumer := scriptedTopicConsumerRevoke(script, meta, false)
	consumer.RevokePartitions(map[string][]int32{"kin": {0}})
	consumer.Client = &mocks.MockKgoClient{
		PollFetchesFunc: script.poll,
		RequestFunc:     meta.request,
		PurgeTopicsFunc: func(topics ...string) {
			meta.mu.Lock()
			meta.purged = append(meta.purged, topics...)
			meta.mu.Unlock()
			consumer.AssignPartitions(map[string][]int32{"kin": {0}})
		},
		AddConsumeTopicsFunc: func(topics ...string) {
			meta.mu.Lock()
			meta.added = append(meta.added, topics...)
			meta.mu.Unlock()
		},
		CommitRecordsFunc: func(_ context.Context, rs ...*kgo.Record) error {
			meta.mu.Lock()
			meta.committed = append(meta.committed, rs...)
			meta.mu.Unlock()
			return nil
		},
	}
	consumer.SetTopicCheckIntervalForTest(time.Millisecond)
	consumer.SetTopicRejoinWaitForTest(time.Hour)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- consumer.Consume(ctx, func(context.Context, *kgo.Record) error { return nil }) }()
	t.Cleanup(func() { cancel(); require.NoError(t, <-done) })

	require.Eventually(t, func() bool { return len(meta.purges()) > 0 }, 5*time.Second, time.Millisecond)
	require.Never(t, func() bool { return len(meta.adds()) > 0 }, 50*time.Millisecond, time.Millisecond)
	consumer.RevokePartitions(map[string][]int32{"kin": {0}})
	require.Eventually(t, func() bool {
		return len(meta.adds()) == 1 && consumer.GetMetrics().TopicRecreations == 1
	}, 5*time.Second, time.Millisecond)
	assert.Equal(t, []string{"kin"}, meta.adds())
}

func TestSwitchWithoutOwnedPartitionsDoesNotWaitForARevoke(t *testing.T) {
	meta := &metadataScript{answer: func(int) ([16]byte, int16) { return topicIDNew, 0 }}
	script := &pollScript{next: func(n int) kgo.Fetches {
		if n == 1 {
			return emptyFetchWithID(topicIDKin)
		}
		return nil
	}}
	consumer := scriptedTopicConsumerRevoke(script, meta, false)
	consumer.RevokePartitions(map[string][]int32{"kin": {0}})
	consumer.AssignPartitions(map[string][]int32{"other": {0}})
	consumer.SetTopicCheckIntervalForTest(time.Millisecond)
	consumer.SetTopicRejoinWaitForTest(time.Hour)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- consumer.Consume(ctx, func(context.Context, *kgo.Record) error { return nil }) }()
	t.Cleanup(func() { cancel(); require.NoError(t, <-done) })

	require.Eventually(t, func() bool {
		return len(meta.adds()) == 1 && consumer.GetMetrics().TopicRecreations == 1
	}, 5*time.Second, time.Millisecond, "must not wait an hour for an impossible revoke")
	assert.Equal(t, []string{"kin"}, meta.purges())
	assert.Equal(t, []string{"kin"}, meta.adds())
	assert.Equal(t, int64(-1), meta.commits()[0].Offset)
}
