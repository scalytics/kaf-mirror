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
	"github.com/scalytics/kaf-mirror/v2/internal/kafka"
	"github.com/scalytics/kaf-mirror/v2/tests/mocks"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestAdminClient_GetTopicDetails(t *testing.T) {
	mockAdmin := &mocks.MockAdminClient{}
	topicDetails := map[string]kafka.TopicDetails{
		"test-topic": {
			Name:              "test-topic",
			Partitions:        3,
			ReplicationFactor: 2,
			CompressionType:   "gzip",
		},
	}

	mockAdmin.On("GetTopicDetails", mock.Anything, []string{"test-topic"}).Return(topicDetails, nil)

	details, err := mockAdmin.GetTopicDetails(context.Background(), []string{"test-topic"})
	assert.NoError(t, err)
	assert.Equal(t, topicDetails, details)
	mockAdmin.AssertExpectations(t)
}

func TestAdminClient_GetTopicDetails_Error(t *testing.T) {
	mockAdmin := &mocks.MockAdminClient{}
	mockAdmin.On("GetTopicDetails", mock.Anything, []string{"test-topic"}).Return(map[string]kafka.TopicDetails{}, errors.New("kafka error"))

	_, err := mockAdmin.GetTopicDetails(context.Background(), []string{"test-topic"})
	assert.Error(t, err)
	assert.Equal(t, "kafka error", err.Error())
	mockAdmin.AssertExpectations(t)
}

func TestAdminClient_GetClusterInfo(t *testing.T) {
	mockAdmin := &mocks.MockAdminClient{}
	clusterInfo := &kafka.ClusterInfo{
		ClusterID:   "test-cluster",
		Topics:      make(map[string]kafka.TopicInfo),
		BrokerCount: 3,
	}

	mockAdmin.On("GetClusterInfo", mock.Anything).Return(clusterInfo, nil)

	info, err := mockAdmin.GetClusterInfo(context.Background())
	assert.NoError(t, err)
	assert.Equal(t, clusterInfo, info)
	mockAdmin.AssertExpectations(t)
}

func TestAdminClient_GetClusterInfo_Error(t *testing.T) {
	mockAdmin := &mocks.MockAdminClient{}
	mockAdmin.On("GetClusterInfo", mock.Anything).Return((*kafka.ClusterInfo)(nil), errors.New("kafka error"))

	_, err := mockAdmin.GetClusterInfo(context.Background())
	assert.Error(t, err)
	assert.Equal(t, "kafka error", err.Error())
	mockAdmin.AssertExpectations(t)
}

func TestAdminClient_GetConsumerGroupOffsets(t *testing.T) {
	mockAdmin := &mocks.MockAdminClient{}
	offsetInfo := map[string][]kafka.OffsetInfo{
		"test-topic": {
			{
				Topic:         "test-topic",
				Partition:     0,
				Offset:        100,
				HighWaterMark: 150,
				Lag:           50,
			},
		},
	}

	mockAdmin.On("GetConsumerGroupOffsets", mock.Anything, "test-group", []string{"test-topic"}).Return(offsetInfo, nil)

	offsets, err := mockAdmin.GetConsumerGroupOffsets(context.Background(), "test-group", []string{"test-topic"})
	assert.NoError(t, err)
	assert.Equal(t, offsetInfo, offsets)
	mockAdmin.AssertExpectations(t)
}

func TestAdminClient_GetConsumerGroupOffsets_Error(t *testing.T) {
	mockAdmin := &mocks.MockAdminClient{}
	mockAdmin.On("GetConsumerGroupOffsets", mock.Anything, "test-group", []string{"test-topic"}).Return((map[string][]kafka.OffsetInfo)(nil), errors.New("kafka error"))

	_, err := mockAdmin.GetConsumerGroupOffsets(context.Background(), "test-group", []string{"test-topic"})
	assert.Error(t, err)
	assert.Equal(t, "kafka error", err.Error())
	mockAdmin.AssertExpectations(t)
}
