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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kerr"
)

type fakeTopicCreator struct {
	listed  kadm.TopicDetails
	created kadm.CreateTopicResponses
	creates []string
}

func (f *fakeTopicCreator) ListTopics(ctx context.Context, topics ...string) (kadm.TopicDetails, error) {
	return f.listed, nil
}

func (f *fakeTopicCreator) CreateTopics(ctx context.Context, partitions int32, replicationFactor int16, configs map[string]*string, topics ...string) (kadm.CreateTopicResponses, error) {
	f.creates = append(f.creates, topics...)
	return f.created, nil
}

func TestEnsureTopicExistsCreatesTopicListedWithAnError(t *testing.T) {
	f := &fakeTopicCreator{
		listed:  kadm.TopicDetails{"orders": {Topic: "orders", Err: kerr.UnknownTopicOrPartition}},
		created: kadm.CreateTopicResponses{"orders": {Topic: "orders"}},
	}

	require.NoError(t, kafka.EnsureTopicExistsForTest(context.Background(), f, "orders", 6, 1))

	assert.Equal(t, []string{"orders"}, f.creates)
}

func TestEnsureTopicExistsSkipsExistingTopic(t *testing.T) {
	f := &fakeTopicCreator{
		listed: kadm.TopicDetails{"orders": {Topic: "orders", Partitions: kadm.PartitionDetails{0: {Topic: "orders"}}}},
	}

	require.NoError(t, kafka.EnsureTopicExistsForTest(context.Background(), f, "orders", 6, 1))

	assert.Empty(t, f.creates)
}

func TestEnsureTopicExistsTreatsTopicAlreadyExistsAsSuccess(t *testing.T) {
	f := &fakeTopicCreator{
		listed:  kadm.TopicDetails{},
		created: kadm.CreateTopicResponses{"orders": {Topic: "orders", Err: kerr.TopicAlreadyExists}},
	}

	require.NoError(t, kafka.EnsureTopicExistsForTest(context.Background(), f, "orders", 6, 1))

	assert.Equal(t, []string{"orders"}, f.creates)
}

func TestEnsureTopicExistsReturnsOtherCreateErrors(t *testing.T) {
	f := &fakeTopicCreator{
		listed:  kadm.TopicDetails{},
		created: kadm.CreateTopicResponses{"orders": {Topic: "orders", Err: kerr.TopicAuthorizationFailed}},
	}

	err := kafka.EnsureTopicExistsForTest(context.Background(), f, "orders", 6, 1)

	require.Error(t, err)
	assert.True(t, errors.Is(err, kerr.TopicAuthorizationFailed))
}
