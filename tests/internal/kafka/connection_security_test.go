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
	"testing"

	"kaf-mirror/internal/config"
	"kaf-mirror/internal/kafka"

	"github.com/stretchr/testify/assert"
)

func TestConnectionSecurityMatchesTheClientSettings(t *testing.T) {
	cases := []struct {
		name      string
		cfg       config.ClusterConfig
		protocol  string
		mechanism string
	}{
		{"plain", config.ClusterConfig{Brokers: "a:9092"}, "PLAINTEXT", ""},
		{"tls", config.ClusterConfig{Brokers: "a:9093", Security: config.SecurityConfig{Protocol: "SSL"}}, "SSL", ""},
		{"scram", config.ClusterConfig{Brokers: "a:9094", Security: config.SecurityConfig{Enabled: true, Protocol: "SASL_PLAINTEXT", SASLMechanism: "SCRAM-SHA-512", Username: "u", Password: "p"}}, "SASL_PLAINTEXT", "SCRAM-SHA-512"},
		{"confluent", config.ClusterConfig{Provider: "confluent", Brokers: "a:9092", Security: config.SecurityConfig{APIKey: "k", APISecret: "s"}}, "SASL_SSL", "PLAIN"},
	}
	for _, tc := range cases {
		protocol, mechanism := kafka.ConnectionSecurity(tc.cfg)
		assert.Equal(t, tc.protocol, protocol, tc.name)
		assert.Equal(t, tc.mechanism, mechanism, tc.name)
	}
}
