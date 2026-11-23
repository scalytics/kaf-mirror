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
	"kaf-mirror/internal/config"
	"kaf-mirror/internal/database"
	"kaf-mirror/internal/kafka"
	"kaf-mirror/tests/mocks"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kgo"
)

func sourceWith(t *testing.T, names ...string) {
	t.Helper()
	topics := map[string]kafka.TopicInfo{}
	for _, n := range names {
		topics[n] = kafka.TopicInfo{Name: n, Partitions: 1}
	}
	restore := kafka.SetAdminClientFactoryForTest(func(config.ClusterConfig) (kafka.AdminClientAPI, error) {
		return &fakeAdmin{info: &kafka.ClusterInfo{Topics: topics}}, nil
	})
	t.Cleanup(restore)
}

func resolve(t *testing.T, mappings ...config.TopicMapping) ([]string, map[string]string, error) {
	t.Helper()
	cfg := &config.Config{
		Clusters: map[string]config.ClusterConfig{"source": {Brokers: "localhost:9092"}},
		Topics:   mappings,
	}
	topics, topicMap, err := kafka.ResolveTopicMappingsForTest(cfg)
	sort.Strings(topics)
	return topics, topicMap, err
}

func TestDottedTopicNameIsExact(t *testing.T) {
	sourceWith(t, "a.b", "a.b.c", "my-a.b", "aXb", "pay.v1", "pay.v1.dlq")

	topics, topicMap, err := resolve(t,
		config.TopicMapping{Source: "a.b", Target: "a.b", Enabled: true},
		config.TopicMapping{Source: "pay.v1", Target: "pay.v1", Enabled: true},
	)

	require.NoError(t, err)
	assert.Equal(t, []string{"a.b", "pay.v1"}, topics)
	assert.Equal(t, map[string]string{"a.b": "a.b", "pay.v1": "pay.v1"}, topicMap)
}

func TestRegexMatchesOnlyFullTopicNames(t *testing.T) {
	sourceWith(t, "app-a", "app-b", "myapp-c", "xapp-a", "app")

	topics, topicMap, err := resolve(t, config.TopicMapping{Source: "app-.*", Target: "mirror-$0", Enabled: true})

	require.NoError(t, err)
	assert.Equal(t, []string{"app-a", "app-b"}, topics)
	assert.Equal(t, map[string]string{"app-a": "mirror-app-a", "app-b": "mirror-app-b"}, topicMap)
}

func TestRegexWithoutTargetKeepsTopicNames(t *testing.T) {
	sourceWith(t, "app-a", "app-b", "other")

	topics, topicMap, err := resolve(t, config.TopicMapping{Source: "app-.*", Target: "", Enabled: true})

	require.NoError(t, err)
	assert.Equal(t, []string{"app-a", "app-b"}, topics)
	assert.Equal(t, map[string]string{"app-a": "app-a", "app-b": "app-b"}, topicMap)
}

func TestExactMappingWithoutTargetKeepsTopicName(t *testing.T) {
	sourceWith(t, "pay.v1")

	topics, topicMap, err := resolve(t, config.TopicMapping{Source: "pay.v1", Target: "", Enabled: true})

	require.NoError(t, err)
	assert.Equal(t, []string{"pay.v1"}, topics)
	assert.Equal(t, map[string]string{"pay.v1": "pay.v1"}, topicMap)
}

func TestIsTopicRegexIgnoresDots(t *testing.T) {
	for _, p := range []string{"pay.v1", "a.b", "my-topic_1", "a.b.c"} {
		assert.False(t, kafka.IsTopicRegex(p), p)
	}
	for _, p := range []string{"app-.*", "a+", "a?", "(a|b)", "[ab]", "a{2}", "^a", "a$", "a\\.b"} {
		assert.True(t, kafka.IsTopicRegex(p), p)
	}
}

func TestResolveTopicsSharedResolverUsesSameRule(t *testing.T) {
	lister := &fakeAdmin{info: &kafka.ClusterInfo{Topics: map[string]kafka.TopicInfo{
		"a.b": {Name: "a.b"}, "a.b.c": {Name: "a.b.c"}, "app-a": {Name: "app-a"}, "xapp-a": {Name: "xapp-a"},
	}}}

	topics, topicMap, err := kafka.ResolveTopics(context.Background(), lister, []config.TopicMapping{
		{Source: "a.b", Enabled: true},
		{Source: "app-.*", Enabled: true},
		{Source: "a.b.c", Enabled: false},
	})

	require.NoError(t, err)
	sort.Strings(topics)
	assert.Equal(t, []string{"a.b", "app-a"}, topics)
	assert.Equal(t, map[string]string{"a.b": "a.b", "app-a": "app-a"}, topicMap)
}

func TestHandleRecordRegexMatchesOnlyFullTopicNames(t *testing.T) {
	var produced []string
	km := kafka.NewKafMirrorImplForTest(&kafka.Producer{Client: &mocks.MockKgoClient{
		ProduceFunc: func(ctx context.Context, r *kgo.Record, f func(*kgo.Record, error)) {
			produced = append(produced, r.Topic)
			f(r, nil)
		},
	}}, map[string]string{})
	require.NoError(t, km.AddRegexMapForTest("app-.*", "copy-$0"))

	require.NoError(t, km.HandleRecordForTest(&kgo.Record{Topic: "myapp-a", Value: []byte("v")}))
	require.NoError(t, km.HandleRecordForTest(&kgo.Record{Topic: "app-a", Value: []byte("v")}))

	assert.Equal(t, []string{"copy-app-a"}, produced)
}

func TestDiscoveryRegexMatchesOnlyFullTopicNames(t *testing.T) {
	sourceWith(t, "big", "big.new", "xbig", "zz")
	f := newFeedMirror()
	require.NoError(t, f.km.AddRegexMapForTest("big.*", "$0"))
	require.NoError(t, f.km.AddRegexMapForTest("zz+", "$0"))
	f.km.SetIntervalsForTest(time.Hour, time.Millisecond)
	f.km.Start("job-37", func(database.ReplicationMetric) {}, func(string, string) {})
	defer f.km.Stop()

	var added []string
	for {
		topic := receive(t, f.added, "a discovered topic")
		added = append(added, topic)
		if topic == "zz" {
			break
		}
	}

	assert.Equal(t, []string{"big.new", "zz"}, added)
}
