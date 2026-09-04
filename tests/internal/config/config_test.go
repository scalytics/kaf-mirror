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

package config_test

import (
	"kaf-mirror/internal/config"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLoadConfig(t *testing.T) {
	// Test loading the default config (no parameters needed)
	cfg, err := config.LoadConfig()
	assert.NoError(t, err)
	assert.NotNil(t, cfg)

	// Basic validation of config structure
	assert.NotEmpty(t, cfg.Server.Host)
	assert.Greater(t, cfg.Server.Port, 0)
	assert.Equal(t, 30, cfg.Database.RetentionDays)
	assert.True(t, cfg.Compliance.Schedule.Enabled)
	assert.Equal(t, 2, cfg.Compliance.Schedule.RunHour)
	assert.True(t, cfg.Compliance.Schedule.Daily)
	assert.Equal(t, "5m", cfg.Replication.TopicDiscoveryInterval)
}

func TestConfigValidate_RetentionBounds(t *testing.T) {
	cfg := &config.Config{
		Server: config.ServerConfig{Port: 8080},
		Clusters: map[string]config.ClusterConfig{
			"source": {Brokers: "localhost:9092"},
		},
	}

	err := cfg.Validate()
	assert.NoError(t, err)
	assert.Equal(t, 30, cfg.Database.RetentionDays)

	cfg.Database.RetentionDays = 7
	err = cfg.Validate()
	assert.NoError(t, err)

	cfg.Database.RetentionDays = 45
	err = cfg.Validate()
	assert.Error(t, err)

	cfg.Database.RetentionDays = 7
	cfg.Compliance.Schedule.Enabled = true
	cfg.Compliance.Schedule.RunHour = 25
	err = cfg.Validate()
	assert.Error(t, err)

	cfg.Compliance.Schedule.RunHour = 2
	cfg.Compliance.Schedule.Daily = false
	cfg.Compliance.Schedule.Weekly = false
	cfg.Compliance.Schedule.Monthly = false
	err = cfg.Validate()
	assert.Error(t, err)

	cfg.Compliance.Schedule.Enabled = false
	cfg.Replication.TopicDiscoveryInterval = "-5m"
	err = cfg.Validate()
	assert.Error(t, err)
}

func TestConfigValidate_RejectsBlockedEgress(t *testing.T) {
	cfg := &config.Config{
		Server: config.ServerConfig{Port: 8080},
		Clusters: map[string]config.ClusterConfig{
			"source": {Brokers: "localhost:9092"},
		},
		AI: config.AIConfig{Endpoint: "http://169.254.169.254/latest/meta-data"},
	}
	err := cfg.Validate()
	assert.Error(t, err)
}

func TestConfigRedactedMasksCredentials(t *testing.T) {
	conn := "Endpoint=sb://ns/;SharedAccessKey=secret"
	cfg := config.Config{
		AI: config.AIConfig{Token: "ai-token", APISecret: "ai-secret"},
		Monitoring: config.MonitoringConfig{
			Splunk: config.SplunkConfig{HECToken: "hec-token"},
		},
		Clusters: map[string]config.ClusterConfig{
			"src": {
				Brokers: "localhost:9092",
				Security: config.SecurityConfig{
					Password:         "pw",
					APIKey:           "ckey",
					APISecret:        "csecret",
					ConnectionString: &conn,
				},
			},
		},
	}

	redacted := cfg.Redacted()
	assert.Equal(t, config.SecretPlaceholder, redacted.AI.Token)
	assert.Equal(t, config.SecretPlaceholder, redacted.AI.APISecret)
	assert.Equal(t, config.SecretPlaceholder, redacted.Monitoring.Splunk.HECToken)
	assert.Equal(t, config.SecretPlaceholder, redacted.Clusters["src"].Security.Password)
	assert.Equal(t, config.SecretPlaceholder, redacted.Clusters["src"].Security.APIKey)
	assert.Equal(t, config.SecretPlaceholder, redacted.Clusters["src"].Security.APISecret)
	assert.NotNil(t, redacted.Clusters["src"].Security.ConnectionString)
	assert.Equal(t, config.SecretPlaceholder, *redacted.Clusters["src"].Security.ConnectionString)

	assert.Equal(t, "ai-token", cfg.AI.Token)
	assert.Equal(t, "csecret", cfg.Clusters["src"].Security.APISecret)
	assert.Equal(t, conn, *cfg.Clusters["src"].Security.ConnectionString)
}

func TestServerListenAddrAndTLSGuard(t *testing.T) {
	cfg := config.ServerConfig{Host: "127.0.0.1", Port: 8443}
	assert.Equal(t, "127.0.0.1:8443", cfg.ListenAddr())

	emptyHost := config.ServerConfig{Port: 8080}
	assert.Equal(t, "localhost:8080", emptyHost.ListenAddr())

	prod := config.ServerConfig{Mode: "production", Port: 8080}
	assert.Error(t, prod.RequireSecureListen())

	prod.TLS.Enabled = true
	assert.NoError(t, prod.RequireSecureListen())

	prod.TLS.Enabled = false
	prod.AllowInsecure = true
	assert.NoError(t, prod.RequireSecureListen())

	dev := config.ServerConfig{Mode: "development", Port: 8080}
	assert.NoError(t, dev.RequireSecureListen())
}

func TestConfigRestoreUnchangedSecrets(t *testing.T) {
	conn := "Endpoint=sb://ns/;SharedAccessKey=secret"
	existing := &config.Config{
		AI: config.AIConfig{Token: "ai-token", APISecret: "ai-secret"},
		Monitoring: config.MonitoringConfig{
			Splunk: config.SplunkConfig{HECToken: "hec-token"},
		},
		Clusters: map[string]config.ClusterConfig{
			"src": {
				Brokers: "localhost:9092",
				Security: config.SecurityConfig{
					Password:         "pw",
					APIKey:           "ckey",
					APISecret:        "csecret",
					ConnectionString: &conn,
				},
			},
		},
	}

	incoming := existing.Redacted()
	incoming.Server.Port = 9090
	incoming.RestoreUnchangedSecrets(existing)

	assert.Equal(t, "ai-token", incoming.AI.Token)
	assert.Equal(t, "ai-secret", incoming.AI.APISecret)
	assert.Equal(t, "hec-token", incoming.Monitoring.Splunk.HECToken)
	assert.Equal(t, "pw", incoming.Clusters["src"].Security.Password)
	assert.Equal(t, "ckey", incoming.Clusters["src"].Security.APIKey)
	assert.Equal(t, "csecret", incoming.Clusters["src"].Security.APISecret)
	assert.NotNil(t, incoming.Clusters["src"].Security.ConnectionString)
	assert.Equal(t, conn, *incoming.Clusters["src"].Security.ConnectionString)
	assert.Equal(t, 9090, incoming.Server.Port)
}
