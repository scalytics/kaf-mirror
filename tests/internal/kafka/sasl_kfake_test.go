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

	"github.com/scalytics/kaf-mirror/v2/internal/config"
	"github.com/scalytics/kaf-mirror/v2/internal/kafka"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kfake"
	"github.com/twmb/franz-go/pkg/kgo"
)

const (
	kfakeUser     = "mirror"
	kfakePassword = "kfake-pass"
	kfakeTopic    = "sasl-topic"
)

var kfakeMechanisms = []string{"PLAIN", "SCRAM-SHA-256", "SCRAM-SHA-512"}

func startSASLCluster(t *testing.T, mechanism string) *kfake.Cluster {
	t.Helper()
	c, err := kfake.NewCluster(
		kfake.NumBrokers(1),
		kfake.EnableSASL(),
		kfake.Superuser(mechanism, kfakeUser, kfakePassword),
		kfake.SeedTopics(1, kfakeTopic),
	)
	require.NoError(t, err)
	t.Cleanup(c.Close)
	return c
}

func kfakeConfig(c *kfake.Cluster, mechanism, password string) config.ClusterConfig {
	return config.ClusterConfig{
		Provider: "plain",
		Brokers:  strings.Join(c.ListenAddrs(), ","),
		Security: config.SecurityConfig{
			Enabled:       true,
			Protocol:      "SASL_PLAINTEXT",
			SASLMechanism: mechanism,
			Username:      kfakeUser,
			Password:      password,
		},
	}
}

func TestTestConnectionAuthenticatesWithSASL(t *testing.T) {
	for _, mechanism := range kfakeMechanisms {
		t.Run(mechanism, func(t *testing.T) {
			c := startSASLCluster(t, mechanism)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			assert.NoError(t, kafka.TestConnection(ctx, kfakeConfig(c, mechanism, kfakePassword)))

			badCtx, badCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer badCancel()
			assert.Error(t, kafka.TestConnection(badCtx, kfakeConfig(c, mechanism, "wrong-password")))
		})
	}
}

func TestAdminClientListsTopicsOverSASL(t *testing.T) {
	for _, mechanism := range kfakeMechanisms {
		t.Run(mechanism, func(t *testing.T) {
			c := startSASLCluster(t, mechanism)
			admin, err := kafka.NewAdminClient(kfakeConfig(c, mechanism, kfakePassword))
			require.NoError(t, err)
			defer admin.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			topics, err := admin.ListTopics(ctx)
			require.NoError(t, err)
			assert.Contains(t, topics, kfakeTopic)
		})
	}
}

func TestProducerAndConsumerOverSASL(t *testing.T) {
	for _, mechanism := range kfakeMechanisms {
		t.Run(mechanism, func(t *testing.T) {
			c := startSASLCluster(t, mechanism)
			cfg := kfakeConfig(c, mechanism, kfakePassword)
			repl := config.ReplicationConfig{BatchSize: 100}

			producer, err := kafka.NewProducer(cfg, repl, "job")
			require.NoError(t, err)
			defer producer.Client.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			res := producer.Client.(*kgo.Client).ProduceSync(ctx, &kgo.Record{Topic: kfakeTopic, Value: []byte("hello")})
			require.NoError(t, res.FirstErr())

			consumer, err := kafka.NewConsumer(cfg, "sasl-group", repl, "job", kfakeTopic)
			require.NoError(t, err)
			defer consumer.Client.Close()
			var got []string
			for len(got) == 0 && ctx.Err() == nil {
				fetches := consumer.Client.PollFetches(ctx)
				fetches.EachRecord(func(r *kgo.Record) { got = append(got, string(r.Value)) })
			}
			assert.Equal(t, []string{"hello"}, got)
		})
	}
}

func TestPresetOverSASLSSLBuildsEveryClient(t *testing.T) {
	cases := map[string]config.ClusterConfig{
		"confluent": {Provider: "confluent", Brokers: "pkc-example.confluent.cloud:9092", Security: config.SecurityConfig{Enabled: true, Protocol: "SASL_SSL", SASLMechanism: "PLAIN", APIKey: "key", APISecret: "secret"}},
		"redpanda":  {Provider: "redpanda", Brokers: "seed.redpanda.example:9092", Security: config.SecurityConfig{Protocol: "SASL_SSL", Username: "u", Password: "secret"}},
	}
	for name, cfg := range cases {
		t.Run(name, func(t *testing.T) {
			consumer, err := kafka.NewConsumer(cfg, "group", config.ReplicationConfig{BatchSize: 100}, "job", "topic")
			require.NoError(t, err)
			consumer.Client.Close()
			producer, err := kafka.NewProducer(cfg, config.ReplicationConfig{BatchSize: 100}, "job")
			require.NoError(t, err)
			producer.Client.Close()
			admin, err := kafka.NewAdminClient(cfg)
			require.NoError(t, err)
			admin.Close()
		})
	}
}
