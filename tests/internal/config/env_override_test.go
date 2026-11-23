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

	"kaf-mirror/internal/config"

	"github.com/spf13/viper"
)

func TestEnvOverridesSecretKeysPresentInConfigFile(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	path := filepath.Join(t.TempDir(), "default.yml")
	content := []byte("server:\n  host: \"localhost\"\n  port: 8080\n  tls:\n    enabled: false\ndatabase:\n  path: \"data/kaf-mirror.db\"\n  retention_days: 30\nai:\n  token: \"\"\n  api_secret: \"\"\nmonitoring:\n  splunk:\n    hec_token: \"\"\n")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CONFIG_PATH", path)
	t.Setenv("KAF_MIRROR_SERVER_HOST", "0.0.0.0")
	t.Setenv("KAF_MIRROR_AI_TOKEN", "ai-token")
	t.Setenv("KAF_MIRROR_AI_API_SECRET", "ai-secret")
	t.Setenv("KAF_MIRROR_MONITORING_SPLUNK_HEC_TOKEN", "hec-token")

	cfg, err := config.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}

	if cfg.Server.Host != "0.0.0.0" {
		t.Errorf("server.host = %q, want 0.0.0.0", cfg.Server.Host)
	}
	if cfg.AI.Token != "ai-token" {
		t.Errorf("ai.token = %q, want ai-token", cfg.AI.Token)
	}
	if cfg.AI.APISecret != "ai-secret" {
		t.Errorf("ai.api_secret = %q, want ai-secret", cfg.AI.APISecret)
	}
	if cfg.Monitoring.Splunk.HECToken != "hec-token" {
		t.Errorf("monitoring.splunk.hec_token = %q, want hec-token", cfg.Monitoring.Splunk.HECToken)
	}
}
