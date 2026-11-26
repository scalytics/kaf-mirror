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

package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

func writeProbeConfig(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "must-not-exist.db")
	cfgPath := filepath.Join(dir, "probe.yml")
	body := []byte("database:\n  path: " + dbPath + "\n")
	if err := os.WriteFile(cfgPath, body, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CONFIG_PATH", cfgPath)
	t.Cleanup(viper.Reset)
	viper.Reset()
	return cfgPath, dbPath
}

func TestConfigProbeOpensTheDatabaseForARealCommand(t *testing.T) {
	t.Chdir(t.TempDir())
	_, dbPath := writeProbeConfig(t)
	if err := run([]string{"users", "list"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dbPath); err != nil {
		t.Fatalf("a normal command should have created the database from the config: %v", err)
	}
}

func TestCompletionWithoutConfigOrDatabase(t *testing.T) {
	t.Chdir(t.TempDir())
	cfgPath, dbPath := writeProbeConfig(t)
	cases := map[string][]string{
		"bash":                    {"completion", "bash"},
		"zsh":                     {"completion", "zsh"},
		"fish":                    {"completion", "fish"},
		"powershell":              {"completion", "powershell"},
		"bash-no-descriptions":    {"completion", "bash", "--no-descriptions"},
		"request":                 {"__complete", "us"},
		"request-no-descriptions": {"__completeNoDesc", "us"},
		"request-subcommand":      {"__complete", "users", ""},
		"request-flag":            {"__complete", "users", "--db-path", ""},
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			viper.Reset()
			out, err := os.CreateTemp(t.TempDir(), "completion")
			if err != nil {
				t.Fatal(err)
			}
			defer out.Close()
			original := os.Stdout
			os.Stdout = out
			defer func() { os.Stdout = original }()
			if err := run(args); err != nil {
				t.Fatal(err)
			}
			raw, err := os.ReadFile(out.Name())
			if err != nil {
				t.Fatal(err)
			}
			switch {
			case name == "request" || name == "request-no-descriptions":
				if !strings.Contains(string(raw), "users") || !strings.HasSuffix(string(raw), ":4\n") {
					t.Fatalf("expected users completion, got %q", raw)
				}
			case name == "request-subcommand":
				if !strings.Contains(string(raw), "add") || !strings.Contains(string(raw), "list") {
					t.Fatalf("expected users subcommands, got %q", raw)
				}
			case strings.HasPrefix(name, "request"):
				if !strings.HasPrefix(string(raw), ":") {
					t.Fatalf("expected only a directive, got %q", raw)
				}
			default:
				if !strings.Contains(string(raw), "admin-cli") || len(raw) < 1000 {
					t.Fatalf("expected a completion script, got %q", raw)
				}
			}
			if db != nil {
				t.Fatal("completion left the database open")
			}
			if used := viper.ConfigFileUsed(); used != "" {
				t.Fatalf("completion loaded the config file %q", used)
			}
			if _, err := os.Stat(dbPath); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("completion created or touched the database: %v", err)
			}
			if _, err := os.Stat(cfgPath); err != nil {
				t.Fatal(err)
			}
		})
	}
}
