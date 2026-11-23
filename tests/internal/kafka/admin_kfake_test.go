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
	"strings"
	"testing"
	"time"

	"kaf-mirror/internal/config"
	"kaf-mirror/internal/kafka"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kfake"
	"github.com/twmb/franz-go/pkg/kgo"
)

func startPlainCluster(t *testing.T) (*kfake.Cluster, *kadm.Client, *kgo.Client) {
	t.Helper()
	c, err := kfake.NewCluster(kfake.NumBrokers(1))
	require.NoError(t, err)
	t.Cleanup(c.Close)
	cl, err := kgo.NewClient(kgo.SeedBrokers(c.ListenAddrs()...), kgo.RecordPartitioner(kgo.ManualPartitioner()))
	require.NoError(t, err)
	t.Cleanup(cl.Close)
	return c, kadm.NewClient(cl), cl
}

func plainAdmin(t *testing.T, c *kfake.Cluster) *kafka.AdminClient {
	t.Helper()
	admin, err := kafka.NewAdminClient(config.ClusterConfig{Provider: "plain", Brokers: strings.Join(c.ListenAddrs(), ",")})
	require.NoError(t, err)
	t.Cleanup(admin.Close)
	return admin
}

func createTopic(t *testing.T, adm *kadm.Client, topic string, partitions int32, configs map[string]*string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	resp, err := adm.CreateTopic(ctx, partitions, 1, configs, topic)
	require.NoError(t, err)
	require.NoError(t, resp.Err)
}

func produceTo(t *testing.T, cl *kgo.Client, topic string, partition int32, n int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for i := 0; i < n; i++ {
		require.NoError(t, cl.ProduceSync(ctx, &kgo.Record{Topic: topic, Partition: partition, Value: []byte("v")}).FirstErr())
	}
}

func TestGetClusterInfoFillsTopicConfig(t *testing.T) {
	c, adm, _ := startPlainCluster(t)
	zstd := "zstd"
	createTopic(t, adm, "zipped", 2, map[string]*string{"compression.type": &zstd})
	createTopic(t, adm, "plain", 1, nil)
	admin := plainAdmin(t, c)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	info, err := admin.GetClusterInfo(ctx)
	require.NoError(t, err)
	require.Contains(t, info.Topics, "zipped")
	assert.Equal(t, "zstd", info.Topics["zipped"].Config["compression.type"])
	assert.Equal(t, "producer", info.Topics["plain"].Config["compression.type"])

	details, err := admin.GetTopicDetails(ctx, "zipped", "plain")
	require.NoError(t, err)
	require.Len(t, details, 2)
	assert.Equal(t, "zipped", details[0].Name)
	assert.Equal(t, "zstd", details[0].CompressionType)
	assert.Equal(t, int32(2), details[0].Partitions)
	assert.Equal(t, "producer", details[1].CompressionType)
}

func TestGetTopicLagCountsFromCommitOrLogStart(t *testing.T) {
	c, adm, cl := startPlainCluster(t)
	createTopic(t, adm, "orders", 3, nil)
	produceTo(t, cl, "orders", 0, 10)
	produceTo(t, cl, "orders", 1, 5)
	produceTo(t, cl, "orders", 2, 4)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	deleted, err := adm.DeleteRecords(ctx, kadm.Offsets{"orders": {1: {Topic: "orders", Partition: 1, At: 2}}})
	require.NoError(t, err)
	require.NoError(t, deleted.Error())

	group := "kaf-mirror-job-lag"
	admin := plainAdmin(t, c)
	lag, err := admin.GetTopicLag(ctx, group, "orders")
	require.NoError(t, err)
	assert.Equal(t, int64(10+3+4), lag)

	offsets := kadm.Offsets{}
	offsets.AddOffset("orders", 0, 3, -1)
	offsets.AddOffset("orders", 2, 99, -1)
	committed, err := adm.CommitOffsets(ctx, group, offsets)
	require.NoError(t, err)
	require.NoError(t, committed.Error())

	lag, err = admin.GetTopicLag(ctx, group, "orders")
	require.NoError(t, err)
	assert.Equal(t, int64(7+3+0), lag)
}

func TestTopicLagTreatsAMissingCommitAsLogStart(t *testing.T) {
	ends := kadm.ListedOffsets{"t": {0: {Topic: "t", Partition: 0, Offset: 8}, 1: {Topic: "t", Partition: 1, Offset: 4}}}
	starts := kadm.ListedOffsets{"t": {0: {Topic: "t", Partition: 0, Offset: 5}, 1: {Topic: "t", Partition: 1, Offset: 0}}}
	committed := kadm.OffsetResponses{"t": {
		0: {Offset: kadm.Offset{Topic: "t", Partition: 0, At: -1}},
		1: {Offset: kadm.Offset{Topic: "t", Partition: 1, At: 6}},
	}}
	assert.Equal(t, int64(3), kafka.TopicLag("t", committed, starts, ends))
	assert.Equal(t, int64(0), kafka.TopicLag("missing", committed, starts, ends))
	assert.Equal(t, int64(8+4), kafka.TopicLag("t", nil, nil, ends))
}
