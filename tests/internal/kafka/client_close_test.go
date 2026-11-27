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
	"sync"
	"testing"

	"github.com/scalytics/kaf-mirror/v2/internal/config"
	"github.com/scalytics/kaf-mirror/v2/internal/kafka"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestKerberosClientIsClosedWhenKafkaClientCreationFails(t *testing.T) {
	keytabPath, krb5Path := writeKerberosFiles(t, "")
	cfg := config.ClusterConfig{
		Brokers:  "broker-1:not-a-port",
		Security: gssapiSecurity(keytabPath, krb5Path, "kaf-mirror@EXAMPLE.COM"),
	}
	var mu sync.Mutex
	closed := map[string]int{}
	var current string
	restore := kafka.SetSASLCloseObserverForTest(func(name string) {
		mu.Lock()
		defer mu.Unlock()
		closed[current+"/"+name]++
	})
	t.Cleanup(restore)

	current = "consumer"
	_, err := kafka.NewConsumer(cfg, "group", config.ReplicationConfig{BatchSize: 100}, "job", "topic")
	require.Error(t, err)
	current = "producer"
	_, err = kafka.NewProducer(cfg, config.ReplicationConfig{BatchSize: 100}, "job")
	require.Error(t, err)
	current = "admin"
	_, err = kafka.NewAdminClient(cfg)
	require.Error(t, err)

	assert.Equal(t, map[string]int{"consumer/GSSAPI": 1, "producer/GSSAPI": 1, "admin/GSSAPI": 1}, closed)
}

func TestKerberosClientIsNotClosedWhenKafkaClientCreationSucceeds(t *testing.T) {
	keytabPath, krb5Path := writeKerberosFiles(t, "")
	cfg := config.ClusterConfig{
		Brokers:  "broker-1:9092",
		Security: gssapiSecurity(keytabPath, krb5Path, "kaf-mirror@EXAMPLE.COM"),
	}
	calls := 0
	t.Cleanup(kafka.SetSASLCloseObserverForTest(func(string) { calls++ }))

	admin, err := kafka.NewAdminClient(cfg)
	require.NoError(t, err)
	assert.Equal(t, 0, calls)
	admin.Close()
}
