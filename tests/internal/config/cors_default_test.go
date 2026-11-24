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
	"reflect"
	"strings"
	"testing"

	"kaf-mirror/internal/server/middleware"

	"gopkg.in/yaml.v2"
)

const defaultCORSOrigin = "http://localhost:8080"

func TestShippedCORSOriginsMatchTheDocumentedDefault(t *testing.T) {
	for _, name := range []string{"default.yml", "docker.yml"} {
		raw, err := os.ReadFile(filepath.Join(repoRoot, "configs", name))
		if err != nil {
			t.Fatal(err)
		}
		var cfg struct {
			Server struct {
				CORS struct {
					AllowedOrigins []string `yaml:"allowed_origins"`
				} `yaml:"cors"`
			} `yaml:"server"`
		}
		if err := yaml.Unmarshal(raw, &cfg); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(cfg.Server.CORS.AllowedOrigins, []string{defaultCORSOrigin}) {
			t.Errorf("configs/%s server.cors.allowed_origins = %q, want [%q]", name, cfg.Server.CORS.AllowedOrigins, defaultCORSOrigin)
		}
	}

	raw, err := os.ReadFile(filepath.Join(repoRoot, "deploy", "helm", "kaf-mirror", "values.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var values struct {
		Config struct {
			Server struct {
				CORSAllowedOrigins []string `yaml:"corsAllowedOrigins"`
			} `yaml:"server"`
		} `yaml:"config"`
	}
	if err := yaml.Unmarshal(raw, &values); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(values.Config.Server.CORSAllowedOrigins, []string{defaultCORSOrigin}) {
		t.Errorf("chart config.server.corsAllowedOrigins = %q, want [%q]", values.Config.Server.CORSAllowedOrigins, defaultCORSOrigin)
	}

	if got := middleware.CorsConfig(nil).AllowOrigins; got != defaultCORSOrigin {
		t.Errorf("empty allowed_origins falls back to %q, want %q", got, defaultCORSOrigin)
	}

	keys := docSection(t, "## Config keys")
	if !strings.Contains(keys, "| `server.cors.allowed_origins` | `[\""+defaultCORSOrigin+"\"]` |") {
		t.Errorf("docs/configuration.md does not give [%q] as the server.cors.allowed_origins default", defaultCORSOrigin)
	}
}
