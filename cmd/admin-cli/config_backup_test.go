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
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

func TestHelpDoesNotNeedAConfigFile(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("CONFIG_PATH", filepath.Join(t.TempDir(), "missing.yml"))
	if err := run([]string{"--help"}); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"help"}); err != nil {
		t.Fatal(err)
	}
}

func TestBackupDatabaseDoesNotNeedAConfigFile(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("CONFIG_PATH", "")
	t.Cleanup(func() { viper.Reset() })
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "kaf-mirror.db")
	out := filepath.Join(dir, "copy.db")
	if err := run([]string{"--db-path", dbPath, "backup", "database", out}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(out); err != nil {
		t.Fatal(err)
	}
}

func TestBackupFullUsesDefaultYmlWhenProdIsMerged(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	t.Setenv("CONFIG_PATH", "")
	t.Cleanup(func() { viper.Reset() })
	configDir := filepath.Join(root, "configs")
	if err := os.Mkdir(configDir, 0700); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(root, "kaf-mirror.db")
	defaultBody := []byte(fmt.Sprintf("database:\n  path: %q\nlogging:\n  file: kaf-mirror.log\n", dbPath))
	prodBody := []byte(fmt.Sprintf("database:\n  path: %q\n", filepath.Join(root, "prod.db")))
	if err := os.WriteFile(filepath.Join(configDir, "default.yml"), defaultBody, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "prod.yml"), prodBody, 0600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(root, "backup")
	if err := os.Mkdir(out, 0700); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"backup", "full", out}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(onlyBackupDir(t, out), "config.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, defaultBody) {
		t.Fatalf("backed up %q, want default.yml", got)
	}
}

func TestExplicitMissingConfigStillFails(t *testing.T) {
	t.Setenv("CONFIG_PATH", filepath.Join(t.TempDir(), "missing.yml"))
	t.Cleanup(func() { viper.Reset() })
	dir := t.TempDir()
	err := run([]string{"--db-path", filepath.Join(dir, "kaf-mirror.db"), "backup", "database", filepath.Join(dir, "copy.db")})
	if err == nil || !strings.Contains(err.Error(), "failed to load configuration") {
		t.Fatalf("got %v", err)
	}
}

func TestBackupFullUsesLoadedConfigAndWarnsWhenMissing(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Cleanup(func() { viper.Reset() })
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "qa.yml")
	body := []byte("database:\n  path: ignored.db\nlogging:\n  file: kaf-mirror.log\n")
	if err := os.WriteFile(cfgPath, body, 0600); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(dir, "kaf-mirror.db")
	withConfig := filepath.Join(dir, "with-config")
	if err := os.Mkdir(withConfig, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CONFIG_PATH", cfgPath)
	var warned bytes.Buffer
	old := errOut
	errOut = &warned
	t.Cleanup(func() { errOut = old })
	if err := run([]string{"--db-path", dbPath, "backup", "full", withConfig}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(warned.String(), "Warning:") {
		t.Fatalf("warning while the loaded config exists:\n%s", warned.String())
	}
	copied := filepath.Join(onlyBackupDir(t, withConfig), "config.yml")
	got, err := os.ReadFile(copied)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, body) {
		t.Fatalf("backed up config %q, want the CONFIG_PATH file", got)
	}

	warned.Reset()
	os.Unsetenv("CONFIG_PATH")
	missing := filepath.Join(dir, "missing-config")
	if err := os.Mkdir(missing, 0700); err != nil {
		t.Fatal(err)
	}
	freshDB := filepath.Join(dir, "fresh.db")
	if err := run([]string{"--db-path", freshDB, "backup", "full", missing, "--config-path", filepath.Join(dir, "no-such.yml")}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(warned.String(), "Warning:") || !strings.Contains(warned.String(), "no-such.yml") {
		t.Fatalf("stderr:\n%s", warned.String())
	}
	backupDir := onlyBackupDir(t, missing)
	if _, err := os.Stat(filepath.Join(backupDir, "database.db")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(backupDir, "config.yml")); !os.IsNotExist(err) {
		t.Fatalf("missing config was copied: %v", err)
	}
}

func onlyBackupDir(t *testing.T, parent string) string {
	t.Helper()
	entries, err := os.ReadDir(parent)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || !entries[0].IsDir() {
		t.Fatalf("backup dir entries: %d", len(entries))
	}
	return filepath.Join(parent, entries[0].Name())
}
