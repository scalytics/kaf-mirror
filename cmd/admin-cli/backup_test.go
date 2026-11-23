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
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"kaf-mirror/internal/database"

	"github.com/jmoiron/sqlx"
)

func openWALWriter(t *testing.T, dbPath string) *sqlx.DB {
	t.Helper()
	w, err := sqlx.Connect("sqlite3", fmt.Sprintf("file:%s?_busy_timeout=5000&_journal_mode=WAL", dbPath))
	if err != nil {
		t.Fatal(err)
	}
	w.SetMaxOpenConns(1)
	t.Cleanup(func() { w.Close() })
	if _, err := w.Exec("PRAGMA wal_autocheckpoint=0"); err != nil {
		t.Fatal(err)
	}
	return w
}

func countRows(t *testing.T, path, name string) int {
	t.Helper()
	r, err := openReadOnly(path)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	var n int
	if err := r.Get(&n, "SELECT COUNT(*) FROM kafka_clusters WHERE name = ?", name); err != nil {
		t.Fatalf("query %s: %v", path, err)
	}
	return n
}

func insertCluster(t *testing.T, w *sqlx.DB, name string) {
	t.Helper()
	if _, err := w.Exec("INSERT INTO kafka_clusters (name, provider, brokers) VALUES (?, 'plain', 'localhost:9092')", name); err != nil {
		t.Fatal(err)
	}
}

func newServerDB(t *testing.T) (string, *sqlx.DB) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "kaf-mirror.db")
	server, err := database.InitDB(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	server.Close()
	return dbPath, openWALWriter(t, dbPath)
}

func TestBackupIncludesUncheckpointedWALWrites(t *testing.T) {
	old := syscall.Umask(0)
	t.Cleanup(func() { syscall.Umask(old) })

	dbPath, writer := newServerDB(t)
	insertCluster(t, writer, "wal-check")

	if info, err := os.Stat(dbPath + "-wal"); err != nil || info.Size() == 0 {
		t.Fatalf("precondition: want a non-empty WAL held by the writer, got %v", err)
	}
	rawCopy := filepath.Join(t.TempDir(), "raw.db")
	if err := copyFile(dbPath, rawCopy); err != nil {
		t.Fatal(err)
	}
	if n := countRows(t, rawCopy, "wal-check"); n != 0 {
		t.Fatalf("precondition: the row must still be only in the WAL, main file has %d", n)
	}

	admin, err := database.InitDB(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()

	out := filepath.Join(t.TempDir(), "bk.db")
	if err := backupDatabase(admin, out); err != nil {
		t.Fatalf("backup: %v", err)
	}
	if n := countRows(t, out, "wal-check"); n != 1 {
		t.Fatalf("backup has %d wal-check rows, want 1", n)
	}
	info, err := os.Stat(out)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("backup mode %v, want 0600", info.Mode().Perm())
	}
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		if _, err := os.Stat(out + suffix); err == nil {
			t.Fatalf("backup left %s%s", out, suffix)
		}
	}
}

func TestBackupRefusesExistingFile(t *testing.T) {
	dbPath, _ := newServerDB(t)
	admin, err := database.InitDB(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()

	out := filepath.Join(t.TempDir(), "bk.db")
	if err := os.WriteFile(out, []byte("keep"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := backupDatabase(admin, out); err == nil {
		t.Fatal("backup over an existing file: want error")
	}
	got, err := os.ReadFile(out)
	if err != nil || string(got) != "keep" {
		t.Fatalf("existing file changed: %q %v", got, err)
	}
	info, _ := os.Stat(out)
	if info.Mode().Perm() != 0644 {
		t.Fatalf("existing file mode changed to %v", info.Mode().Perm())
	}
}

func TestConfigBackupIsPrivate(t *testing.T) {
	old := syscall.Umask(0)
	t.Cleanup(func() { syscall.Umask(old) })

	dir := t.TempDir()
	src := filepath.Join(dir, "default.yml")
	if err := os.WriteFile(src, []byte("ai:\n  token: secret\n"), 0644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dir, "config-backup.yml")
	if err := copyPrivateFile(src, dst); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(dst)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("config backup mode %v, want 0600", info.Mode().Perm())
	}
	if err := copyPrivateFile(src, dst); err == nil {
		t.Fatal("config backup over an existing file: want error")
	}
}

func TestRestoreReplacesDatabaseAndDropsStaleSidecars(t *testing.T) {
	dbPath, writer := newServerDB(t)
	insertCluster(t, writer, "in-backup")

	admin, err := database.InitDB(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "bk.db")
	if err := backupDatabase(admin, out); err != nil {
		t.Fatal(err)
	}
	insertCluster(t, writer, "after-backup")
	admin.Close()
	writer.Close()
	if _, err := os.Stat(dbPath + "-wal"); err == nil {
		t.Fatal("precondition: WAL must be gone after the last connection closed")
	}
	if err := os.WriteFile(dbPath+"-shm", []byte("stale"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := restoreDatabase(out, dbPath); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if n := countRows(t, dbPath, "in-backup"); n != 1 {
		t.Fatalf("restored db has %d in-backup rows, want 1", n)
	}
	if n := countRows(t, dbPath, "after-backup"); n != 0 {
		t.Fatalf("restored db has %d after-backup rows, want 0", n)
	}
	if _, err := os.Stat(dbPath + "-shm"); err == nil {
		t.Fatal("restore kept the stale -shm file")
	}
	info, err := os.Stat(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("restored db mode %v, want 0600", info.Mode().Perm())
	}
	reopened, err := database.InitDB(dbPath)
	if err != nil {
		t.Fatalf("server cannot open the restored db: %v", err)
	}
	reopened.Close()
}

func TestRestoreRefusesDatabaseInUse(t *testing.T) {
	dbPath, writer := newServerDB(t)
	insertCluster(t, writer, "live")

	backupDir := t.TempDir()
	other := filepath.Join(backupDir, "other.db")
	o, err := database.InitDB(other)
	if err != nil {
		t.Fatal(err)
	}
	o.Close()

	if err := restoreDatabase(other, dbPath); err == nil {
		t.Fatal("restore while another connection holds the db: want error")
	}
	if n := countRows(t, dbPath, "live"); n != 1 {
		t.Fatalf("live db changed, has %d live rows", n)
	}
}

func TestRestoreRejectsNonDatabase(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "kaf-mirror.db")
	s, err := database.InitDB(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	bad := filepath.Join(dir, "config-backup.yml")
	if err := os.WriteFile(bad, []byte("server:\n  port: 8080\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := restoreDatabase(bad, dbPath); err == nil {
		t.Fatal("restore from a YAML file: want error")
	}
	if err := checkDatabaseFile(dbPath); err != nil {
		t.Fatalf("db damaged by a rejected restore: %v", err)
	}
}
