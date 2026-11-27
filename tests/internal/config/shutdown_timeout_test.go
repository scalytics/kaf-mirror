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
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/scalytics/kaf-mirror/v2/internal/config"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestShutdownTimeoutDuration(t *testing.T) {
	timeout, err := config.ServerConfig{}.ShutdownTimeoutDuration()
	require.NoError(t, err)
	assert.Equal(t, 30*time.Second, timeout)

	timeout, err = config.ServerConfig{ShutdownTimeout: "90s"}.ShutdownTimeoutDuration()
	require.NoError(t, err)
	assert.Equal(t, 90*time.Second, timeout)

	for _, bad := range []string{"soon", "0s", "-5s"} {
		_, err := config.ServerConfig{ShutdownTimeout: bad}.ShutdownTimeoutDuration()
		assert.Error(t, err, bad)
	}
}

func TestValidateRejectsAnInvalidShutdownTimeout(t *testing.T) {
	cfg := &config.Config{
		Server:   config.ServerConfig{Port: 8080, ShutdownTimeout: "soon"},
		Clusters: map[string]config.ClusterConfig{"source": {Brokers: "localhost:9092"}},
	}
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "server shutdown_timeout must be a valid duration")
}

func TestShutdownTimeoutFromConfigFileAndEnv(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	path := filepath.Join(t.TempDir(), "default.yml")
	require.NoError(t, os.WriteFile(path, []byte("server:\n  port: 8080\n  shutdown_timeout: \"30s\"\n"), 0o600))
	t.Setenv("CONFIG_PATH", path)
	t.Setenv("KAF_MIRROR_SERVER_SHUTDOWN_TIMEOUT", "75s")

	cfg, err := config.LoadConfig()
	require.NoError(t, err)
	timeout, err := cfg.Server.ShutdownTimeoutDuration()
	require.NoError(t, err)
	assert.Equal(t, 75*time.Second, timeout)
}
