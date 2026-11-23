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

//go:build unix

package database_test

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"kaf-mirror/internal/database"
)

func TestInitDBCreatesPrivateFiles(t *testing.T) {
	old := syscall.Umask(0)
	t.Cleanup(func() { syscall.Umask(old) })

	dir := filepath.Join(t.TempDir(), "data")
	dbPath := filepath.Join(dir, "kaf-mirror.db")
	db, err := database.InitDB(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	assertMode(t, dir, 0700)
	assertMode(t, dbPath, 0600)
	for _, suffix := range []string{"-wal", "-shm"} {
		path := dbPath + suffix
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		assertMode(t, path, 0600)
	}
}

func TestInitDBTightensExistingModes(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data")
	if err := os.Mkdir(dir, 0755); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(dir, "kaf-mirror.db")
	if err := os.WriteFile(dbPath, nil, 0644); err != nil {
		t.Fatal(err)
	}
	db, err := database.InitDB(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()

	assertMode(t, dir, 0700)
	assertMode(t, dbPath, 0600)
	for _, suffix := range []string{"-wal", "-shm"} {
		path := dbPath + suffix
		if _, err := os.Stat(path); err != nil {
			continue
		}
		assertMode(t, path, 0600)
	}
}

func TestAcquireDBLockDetectsInUse(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "kaf-mirror.db")
	db, err := database.InitDB(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()

	lock, err := database.AcquireDBLock(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { lock.Close() })

	inUse, err := database.IsDBLocked(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if !inUse {
		t.Fatal("IsDBLocked: want true while the lock is held")
	}
	if err := lock.Close(); err != nil {
		t.Fatal(err)
	}
	inUse, err = database.IsDBLocked(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if inUse {
		t.Fatal("IsDBLocked: want false after the lock is released")
	}
}

func assertMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("%s mode %o, want %o", path, got, want)
	}
}
