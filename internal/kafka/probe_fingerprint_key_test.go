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

package kafka

import (
	"errors"
	"testing"

	"github.com/scalytics/kaf-mirror/v2/internal/config"

	"github.com/stretchr/testify/require"
)

func TestProbeFingerprintStableInProcessAndDiffersAcrossKeys(t *testing.T) {
	conn := "Endpoint=sb://hub.servicebus.windows.net/;SharedAccessKey=azure-secret-key"
	cfg := config.ClusterConfig{
		Provider: "confluent",
		Brokers:  "pkc.example:9092",
		Security: config.SecurityConfig{
			APIKey:           "confluent-api-key",
			APISecret:        "confluent-api-secret",
			Username:         "sasl-user",
			Password:         "sasl-password",
			ConnectionString: &conn,
			Kerberos:         config.KerberosConfig{Principal: "kafka/broker@REALM"},
		},
	}

	first, ok := ProbeFingerprint(cfg)
	require.True(t, ok)
	require.NotEmpty(t, first)
	second, ok := ProbeFingerprint(cfg)
	require.True(t, ok)
	require.Equal(t, first, second)

	key1 := bytesOf(1)
	key2 := bytesOf(2)
	acrossOne, ok := probeFingerprintWithKey(cfg, key1)
	require.True(t, ok)
	acrossOneAgain, ok := probeFingerprintWithKey(cfg, key1)
	require.True(t, ok)
	acrossTwo, ok := probeFingerprintWithKey(cfg, key2)
	require.True(t, ok)
	require.Equal(t, acrossOne, acrossOneAgain)
	require.NotEqual(t, acrossOne, acrossTwo)
	require.NotEqual(t, first, acrossOne)
	require.NotEqual(t, first, acrossTwo)

	for _, secret := range []string{
		"confluent-api-key",
		"confluent-api-secret",
		"sasl-user",
		"sasl-password",
		conn,
		"azure-secret-key",
		"kafka/broker@REALM",
	} {
		require.NotContains(t, first, secret)
		require.NotContains(t, acrossOne, secret)
		require.NotContains(t, acrossTwo, secret)
	}
}

func TestProbeFingerprintFailsClosedWithoutKey(t *testing.T) {
	cfg := config.ClusterConfig{
		Brokers: "pkc.example:9092",
		Security: config.SecurityConfig{
			Password: "sasl-password",
		},
	}
	restore := suspendProbeMACKeyForTest()
	defer restore()

	first, ok := ProbeFingerprint(cfg)
	require.False(t, ok)
	require.Empty(t, first)
	second, ok := ProbeFingerprint(cfg)
	require.False(t, ok)
	require.Empty(t, second)
}

func bytesOf(b byte) []byte {
	key := make([]byte, 32)
	for i := range key {
		key[i] = b
	}
	return key
}

func TestProbeFingerprintFailsClosedWhenCanonicalJSONCannotBeMarshaled(t *testing.T) {
	orig := marshalClusterConfig
	t.Cleanup(func() { marshalClusterConfig = orig })
	marshalClusterConfig = func(any) ([]byte, error) {
		return nil, errors.New("marshal failed")
	}
	fp, ok := probeFingerprintWithKey(config.ClusterConfig{Brokers: "broker-1:9092"}, bytesOf(1))
	require.False(t, ok)
	require.Empty(t, fp)
}
