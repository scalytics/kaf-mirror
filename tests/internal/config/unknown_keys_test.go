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

	"github.com/scalytics/kaf-mirror/v2/internal/config"

	"github.com/spf13/viper"
)

func TestOldConfigFilesWithRemovedKeysStillLoad(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	path := filepath.Join(t.TempDir(), "default.yml")
	content := []byte(`
server:
  host: "localhost"
  port: 8080
  admin_email: "ops@example.com"
  tls:
    enabled: false
logging:
  level: "INFO"
  file: "logs/kaf-mirror.log"
  max_size: 100
  max_backups: 5
  max_age: 30
  console: true
database:
  path: "data/kaf-mirror.db"
  retention_days: 30
replication:
  job_id: "legacy"
  batch_size: 1000
  compression: "none"
topic_mappings:
  - source: "orders"
    target: "orders"
    enabled: true
protection:
  refuse_write_to_prod: true
clusters:
  source:
    brokers: "localhost:9092"
`)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CONFIG_PATH", path)
	cfg, err := config.LoadConfig()
	if err != nil {
		t.Fatalf("old config file failed to load: %v", err)
	}
	if cfg.Replication.JobID != "" || cfg.Replication.BatchSize != 0 || cfg.Replication.Compression != "" {
		t.Fatalf("removed replication keys were still read: %+v", cfg.Replication)
	}
	if len(cfg.Topics) != 0 {
		t.Fatalf("topic_mappings were still read: %+v", cfg.Topics)
	}
	if cfg.Protection.RefuseWriteToProd {
		t.Fatal("refuse_write_to_prod was read from the file")
	}
}
