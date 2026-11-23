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
	"context"
	"errors"
	"kaf-mirror/internal/kafka"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kerr"
)

type fakeTopicCreator struct {
	lists   []kadm.TopicDetails
	listed  kadm.TopicDetails
	created kadm.CreateTopicResponses
	creates []string
	calls   int
}

func (f *fakeTopicCreator) ListTopics(ctx context.Context, topics ...string) (kadm.TopicDetails, error) {
	f.calls++
	if len(f.lists) == 0 {
		return f.listed, nil
	}
	i := f.calls - 1
	if i >= len(f.lists) {
		i = len(f.lists) - 1
	}
	return f.lists[i], nil
}

func (f *fakeTopicCreator) CreateTopics(ctx context.Context, partitions int32, replicationFactor int16, configs map[string]*string, topics ...string) (kadm.CreateTopicResponses, error) {
	f.creates = append(f.creates, topics...)
	return f.created, nil
}

func visibleOrders() kadm.TopicDetails {
	return kadm.TopicDetails{"orders": {Topic: "orders", Partitions: kadm.PartitionDetails{0: {Topic: "orders", Partition: 0}}}}
}

func missingOrders() kadm.TopicDetails {
	return kadm.TopicDetails{"orders": {Topic: "orders", Err: kerr.UnknownTopicOrPartition}}
}

func TestEnsureTopicExistsCreatesTopicListedWithAnError(t *testing.T) {
	f := &fakeTopicCreator{
		lists:   []kadm.TopicDetails{missingOrders(), visibleOrders()},
		created: kadm.CreateTopicResponses{"orders": {Topic: "orders"}},
	}

	require.NoError(t, kafka.EnsureTopicExistsForTest(context.Background(), f, "orders", 6, 1))

	assert.Equal(t, []string{"orders"}, f.creates)
	assert.Equal(t, 2, f.calls)
}

func TestEnsureTopicExistsSkipsExistingTopic(t *testing.T) {
	f := &fakeTopicCreator{
		listed: visibleOrders(),
	}

	require.NoError(t, kafka.EnsureTopicExistsForTest(context.Background(), f, "orders", 6, 1))

	assert.Empty(t, f.creates)
	assert.Equal(t, 1, f.calls)
}

func TestEnsureTopicExistsTreatsTopicAlreadyExistsAsSuccess(t *testing.T) {
	f := &fakeTopicCreator{
		lists:   []kadm.TopicDetails{kadm.TopicDetails{}, visibleOrders()},
		created: kadm.CreateTopicResponses{"orders": {Topic: "orders", Err: kerr.TopicAlreadyExists}},
	}

	require.NoError(t, kafka.EnsureTopicExistsForTest(context.Background(), f, "orders", 6, 1))

	assert.Equal(t, []string{"orders"}, f.creates)
	assert.Equal(t, 2, f.calls)
}

func TestEnsureTopicExistsReturnsOtherCreateErrors(t *testing.T) {
	f := &fakeTopicCreator{
		listed:  kadm.TopicDetails{},
		created: kadm.CreateTopicResponses{"orders": {Topic: "orders", Err: kerr.TopicAuthorizationFailed}},
	}

	err := kafka.EnsureTopicExistsForTest(context.Background(), f, "orders", 6, 1)

	require.Error(t, err)
	assert.True(t, errors.Is(err, kerr.TopicAuthorizationFailed))
	assert.Equal(t, 1, f.calls)
}

func TestEnsureTopicExistsWaitsUntilCreatedTopicIsVisible(t *testing.T) {
	f := &fakeTopicCreator{
		lists:   []kadm.TopicDetails{missingOrders(), missingOrders(), visibleOrders()},
		created: kadm.CreateTopicResponses{"orders": {Topic: "orders"}},
	}
	var sleeps []time.Duration
	restore := kafka.SetTopicMetadataSleepForTest(func(ctx context.Context, d time.Duration) error {
		sleeps = append(sleeps, d)
		return nil
	})
	t.Cleanup(restore)

	require.NoError(t, kafka.EnsureTopicExistsForTest(context.Background(), f, "orders", 6, 1))

	assert.Equal(t, []string{"orders"}, f.creates)
	assert.Equal(t, 3, f.calls)
	assert.Equal(t, []time.Duration{time.Second}, sleeps)
}

func TestEnsureTopicExistsFailsWhenCreatedTopicStaysInvisible(t *testing.T) {
	f := &fakeTopicCreator{
		lists:   []kadm.TopicDetails{missingOrders()},
		created: kadm.CreateTopicResponses{"orders": {Topic: "orders"}},
	}
	var sleeps []time.Duration
	restore := kafka.SetTopicMetadataSleepForTest(func(ctx context.Context, d time.Duration) error {
		sleeps = append(sleeps, d)
		return nil
	})
	t.Cleanup(restore)

	err := kafka.EnsureTopicExistsForTest(context.Background(), f, "orders", 6, 1)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "not visible after 5 metadata checks")
	assert.Equal(t, []string{"orders"}, f.creates)
	assert.Equal(t, 6, f.calls)
	assert.Equal(t, []time.Duration{time.Second, 2 * time.Second, 3 * time.Second, 4 * time.Second}, sleeps)
}

func TestEnsureTopicExistsStopsWaitingWhenContextIsCancelled(t *testing.T) {
	f := &fakeTopicCreator{
		lists:   []kadm.TopicDetails{missingOrders()},
		created: kadm.CreateTopicResponses{"orders": {Topic: "orders"}},
	}
	ctx, cancel := context.WithCancel(context.Background())
	restore := kafka.SetTopicMetadataSleepForTest(func(ctx context.Context, d time.Duration) error {
		cancel()
		return ctx.Err()
	})
	t.Cleanup(restore)

	err := kafka.EnsureTopicExistsForTest(ctx, f, "orders", 6, 1)

	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}
