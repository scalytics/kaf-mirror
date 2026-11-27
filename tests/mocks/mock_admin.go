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

package mocks

import (
	"context"
	"github.com/scalytics/kaf-mirror/v2/internal/kafka"

	"github.com/stretchr/testify/mock"
)

// MockAdminClient provides a mock implementation of AdminClient for testing
type MockAdminClient struct {
	mock.Mock
}

func (m *MockAdminClient) GetClusterInfo(ctx context.Context) (*kafka.ClusterInfo, error) {
	args := m.Called(ctx)
	return args.Get(0).(*kafka.ClusterInfo), args.Error(1)
}

func (m *MockAdminClient) GetTopicDetails(ctx context.Context, topicNames []string) (map[string]kafka.TopicDetails, error) {
	args := m.Called(ctx, topicNames)
	return args.Get(0).(map[string]kafka.TopicDetails), args.Error(1)
}

func (m *MockAdminClient) GetConsumerGroupOffsets(ctx context.Context, groupID string, topics []string) (map[string][]kafka.OffsetInfo, error) {
	args := m.Called(ctx, groupID, topics)
	return args.Get(0).(map[string][]kafka.OffsetInfo), args.Error(1)
}

func (m *MockAdminClient) GetTopicHighWaterMarks(ctx context.Context, topics []string) (map[string][]kafka.OffsetInfo, error) {
	args := m.Called(ctx, topics)
	return args.Get(0).(map[string][]kafka.OffsetInfo), args.Error(1)
}

func (m *MockAdminClient) ValidateTopicCompatibility(ctx context.Context, sourceInfo, targetInfo kafka.TopicInfo) error {
	args := m.Called(ctx, sourceInfo, targetInfo)
	return args.Error(0)
}

func (m *MockAdminClient) EnsureTopicExists(ctx context.Context, topicName string, partitions int32, replicationFactor int16) error {
	args := m.Called(ctx, topicName, partitions, replicationFactor)
	return args.Error(0)
}

func (m *MockAdminClient) ListTopics(ctx context.Context, topics ...string) (map[string]kafka.TopicInfo, error) {
	args := m.Called(ctx, topics)
	return args.Get(0).(map[string]kafka.TopicInfo), args.Error(1)
}

func (m *MockAdminClient) GetTopicLag(ctx context.Context, groupID, topic string) (int64, error) {
	args := m.Called(ctx, groupID, topic)
	return args.Get(0).(int64), args.Error(1)
}

func (m *MockAdminClient) Close() {
	m.Called()
}
