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
	"github.com/scalytics/kaf-mirror/v2/internal/config"
	"github.com/scalytics/kaf-mirror/v2/internal/kafka"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type staleMetadataAdmin struct {
	fakeAdmin
	hidden    string
	staleRuns int
	infoCalls int
}

func (s *staleMetadataAdmin) GetClusterInfo(ctx context.Context) (*kafka.ClusterInfo, error) {
	s.infoCalls++
	info := &kafka.ClusterInfo{Topics: map[string]kafka.TopicInfo{}, BrokerCount: 3}
	for name, topic := range s.info.Topics {
		info.Topics[name] = topic
	}
	if len(s.ensureCalls) > 0 && s.staleRuns > 0 {
		s.staleRuns--
		delete(info.Topics, s.hidden)
	}
	return info, nil
}

func staleMetadataSetup(t *testing.T, staleRuns int) (*staleMetadataAdmin, *[]time.Duration, *config.Config) {
	admin := &staleMetadataAdmin{
		fakeAdmin: fakeAdmin{info: &kafka.ClusterInfo{Topics: map[string]kafka.TopicInfo{
			"orders-eu": {Name: "orders-eu", Partitions: 3, ReplicationFactor: 1},
		}}},
		hidden:    "backup-eu",
		staleRuns: staleRuns,
	}
	t.Cleanup(kafka.SetAdminClientFactoryForTest(func(config.ClusterConfig) (kafka.AdminClientAPI, error) {
		return admin, nil
	}))
	var waits []time.Duration
	t.Cleanup(kafka.SetTopicMetadataSleepForTest(func(ctx context.Context, d time.Duration) error {
		waits = append(waits, d)
		return nil
	}))
	cfg := &config.Config{
		Clusters: map[string]config.ClusterConfig{
			"source": {Brokers: "localhost:9092"},
			"target": {Brokers: "localhost:9093"},
		},
		Replication: config.ReplicationConfig{JobID: "job-66"},
	}
	return admin, &waits, cfg
}

func TestValidateWaitsForANewTargetTopicInStaleMetadata(t *testing.T) {
	admin, waits, cfg := staleMetadataSetup(t, 2)

	err := kafka.ValidateAndSyncClustersForTest(cfg, []string{"orders-eu"}, map[string]string{"orders-eu": "backup-eu"})

	require.NoError(t, err)
	assert.Equal(t, []string{"backup-eu"}, admin.ensureCalls)
	assert.Equal(t, []time.Duration{time.Second, 2 * time.Second}, *waits)
}

func TestValidateGivesUpOnAMissingTargetTopicAfterFiveChecks(t *testing.T) {
	admin, waits, cfg := staleMetadataSetup(t, 100)

	err := kafka.ValidateAndSyncClustersForTest(cfg, []string{"orders-eu"}, map[string]string{"orders-eu": "backup-eu"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "target topic backup-eu does not exist after creation attempt")
	assert.Equal(t, 7, admin.infoCalls)
	assert.Equal(t, []time.Duration{time.Second, 2 * time.Second, 3 * time.Second, 4 * time.Second}, *waits)
}

func TestValidateStopsWaitingForATargetTopicWhenTheWaitIsCancelled(t *testing.T) {
	admin, _, cfg := staleMetadataSetup(t, 100)
	t.Cleanup(kafka.SetTopicMetadataSleepForTest(func(ctx context.Context, d time.Duration) error {
		return context.Canceled
	}))

	err := kafka.ValidateAndSyncClustersForTest(cfg, []string{"orders-eu"}, map[string]string{"orders-eu": "backup-eu"})

	require.Error(t, err)
	assert.True(t, errors.Is(err, context.Canceled))
	assert.Equal(t, 3, admin.infoCalls)
}
