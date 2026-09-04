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
	"kaf-mirror/internal/kafka"
	"kaf-mirror/tests/mocks"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/twmb/franz-go/pkg/kgo"
)

func TestProducer_Produce(t *testing.T) {
	record := &kgo.Record{Value: []byte("hello")}
	var producedRecord *kgo.Record

	mock := &mocks.MockKgoClient{
		ProduceFunc: func(ctx context.Context, r *kgo.Record, f func(*kgo.Record, error)) {
			producedRecord = r
			f(r, nil)
		},
	}

	producer := &kafka.Producer{Client: mock}
	producer.Produce(context.Background(), record, func(r *kgo.Record, err error) {
		assert.NoError(t, err)
		assert.Equal(t, record.Value, r.Value)
	})

	assert.Equal(t, record.Value, producedRecord.Value)
}

func TestNewProducer_Compression_Gzip(t *testing.T) {
	cfg := config.ClusterConfig{
		Brokers: "localhost:9092",
	}

	replicationCfg := config.ReplicationConfig{
		BatchSize:   1000,
		Parallelism: 4,
		Compression: "gzip",
	}

	producer, err := kafka.NewProducer(cfg, replicationCfg, "test-job")
	assert.NoError(t, err)
	assert.NotNil(t, producer)
}

func TestNewProducer_Compression_Snappy(t *testing.T) {
	cfg := config.ClusterConfig{
		Brokers: "localhost:9092",
	}

	replicationCfg := config.ReplicationConfig{
		BatchSize:   2000,
		Parallelism: 8,
		Compression: "snappy",
	}

	producer, err := kafka.NewProducer(cfg, replicationCfg, "test-job")
	assert.NoError(t, err)
	assert.NotNil(t, producer)
}

func TestNewProducer_Compression_Lz4(t *testing.T) {
	cfg := config.ClusterConfig{
		Brokers: "localhost:9092",
	}

	replicationCfg := config.ReplicationConfig{
		BatchSize:   500,
		Parallelism: 2,
		Compression: "lz4",
	}

	producer, err := kafka.NewProducer(cfg, replicationCfg, "test-job")
	assert.NoError(t, err)
	assert.NotNil(t, producer)
}

func TestNewProducer_Compression_Zstd(t *testing.T) {
	cfg := config.ClusterConfig{
		Brokers: "localhost:9092",
	}

	replicationCfg := config.ReplicationConfig{
		BatchSize:   1500,
		Parallelism: 6,
		Compression: "zstd",
	}

	producer, err := kafka.NewProducer(cfg, replicationCfg, "test-job")
	assert.NoError(t, err)
	assert.NotNil(t, producer)
}

func TestNewProducer_NoCompression(t *testing.T) {
	cfg := config.ClusterConfig{
		Brokers: "localhost:9092",
	}

	replicationCfg := config.ReplicationConfig{
		BatchSize:   1000,
		Parallelism: 4,
		Compression: "none",
	}

	producer, err := kafka.NewProducer(cfg, replicationCfg, "test-job")
	assert.NoError(t, err)
	assert.NotNil(t, producer)
}

func TestNewProducer_SASL_PLAIN(t *testing.T) {
	cfg := config.ClusterConfig{
		Brokers: "localhost:9092",
		Security: config.SecurityConfig{
			Enabled:       true,
			Protocol:      "SASL_PLAINTEXT",
			SASLMechanism: "PLAIN",
			Username:      "testuser",
			Password:      "testpass",
		},
	}

	replicationCfg := config.ReplicationConfig{
		BatchSize:   1000,
		Parallelism: 4,
		Compression: "gzip",
	}

	producer, err := kafka.NewProducer(cfg, replicationCfg, "test-job")
	assert.NoError(t, err)
	assert.NotNil(t, producer)
}

func TestNewProducer_TLS_SSL(t *testing.T) {
	cfg := config.ClusterConfig{
		Brokers: "localhost:9092",
		Security: config.SecurityConfig{
			Enabled:  true,
			Protocol: "SSL",
		},
	}

	replicationCfg := config.ReplicationConfig{
		BatchSize:   1000,
		Parallelism: 4,
		Compression: "snappy",
	}

	producer, err := kafka.NewProducer(cfg, replicationCfg, "test-job")
	assert.NoError(t, err)
	assert.NotNil(t, producer)
}

func TestNewProducer_Kerberos(t *testing.T) {
	cfg := config.ClusterConfig{
		Brokers: "localhost:9092",
		Security: config.SecurityConfig{
			Enabled:       true,
			Protocol:      "SASL_SSL",
			SASLMechanism: "GSSAPI",
		},
	}
	cfg.Security.Kerberos.ServiceName = "kafka"

	replicationCfg := config.ReplicationConfig{
		BatchSize:   1000,
		Parallelism: 4,
		Compression: "lz4",
	}

	producer, err := kafka.NewProducer(cfg, replicationCfg, "test-job")
	assert.NoError(t, err)
	assert.NotNil(t, producer)
}

func TestNewProducer_InvalidSASL(t *testing.T) {
	cfg := config.ClusterConfig{
		Brokers: "localhost:9092",
		Security: config.SecurityConfig{
			Enabled:       true,
			SASLMechanism: "INVALID_MECHANISM",
		},
	}

	replicationCfg := config.ReplicationConfig{
		BatchSize:   1000,
		Parallelism: 4,
		Compression: "gzip",
	}

	producer, err := kafka.NewProducer(cfg, replicationCfg, "test-job")
	assert.Error(t, err)
	assert.Nil(t, producer)
	assert.Contains(t, err.Error(), "unsupported SASL mechanism")
}

func TestNewProducer_MissingKerberosServiceName(t *testing.T) {
	cfg := config.ClusterConfig{
		Brokers: "localhost:9092",
		Security: config.SecurityConfig{
			Enabled:       true,
			SASLMechanism: "GSSAPI",
		},
	}

	replicationCfg := config.ReplicationConfig{
		BatchSize:   1000,
		Parallelism: 4,
		Compression: "gzip",
	}

	producer, err := kafka.NewProducer(cfg, replicationCfg, "test-job")
	assert.Error(t, err)
	assert.Nil(t, producer)
	assert.Contains(t, err.Error(), "Kerberos service name is required")
}
