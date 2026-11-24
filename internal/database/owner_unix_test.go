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

package database

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func actAsAnotherUser(t *testing.T) {
	t.Helper()
	old := processUID
	other := os.Geteuid() + 1
	processUID = func() int { return other }
	t.Cleanup(func() { processUID = old })
}

func openDataDir(t *testing.T, mode os.FileMode) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "data")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, mode); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestServerStartsInADataDirOfAnotherUser(t *testing.T) {
	for _, mode := range []os.FileMode{0777 | os.ModeSticky, 0775, 0770, 0755} {
		dir := openDataDir(t, mode)
		actAsAnotherUser(t)

		dbPath := filepath.Join(dir, "kaf-mirror.db")
		db, lock, err := OpenLockedDB(dbPath)
		if err != nil {
			t.Fatalf("mode %04o: OpenLockedDB: %v", mode, err)
		}
		db.Close()
		lock.Close()

		info, err := os.Stat(dir)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode() & (os.ModePerm | os.ModeSticky); got != mode {
			t.Fatalf("data directory mode %04o, want it left at %04o", got, mode)
		}
		info, err = os.Stat(dbPath)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != 0600 {
			t.Fatalf("database mode %04o, want 0600", got)
		}
	}
}

func TestOwnDataDirIsStillMadePrivate(t *testing.T) {
	dir := openDataDir(t, 0777)
	db, lock, err := OpenLockedDB(filepath.Join(dir, "kaf-mirror.db"))
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	lock.Close()
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0700 {
		t.Fatalf("data directory mode %04o, want 0700", got)
	}
}

func TestReadableDatabaseOfAnotherUserIsRefused(t *testing.T) {
	dir := openDataDir(t, 0775)
	dbPath := filepath.Join(dir, "kaf-mirror.db")
	if err := os.WriteFile(dbPath, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dbPath, 0644); err != nil {
		t.Fatal(err)
	}
	actAsAnotherUser(t)

	_, err := InitDB(dbPath)
	if err == nil {
		t.Fatal("InitDB: want an error for a 0644 database of another user")
	}
	if !strings.Contains(err.Error(), "mode 0644 and belongs to another user") {
		t.Fatalf("InitDB: %v", err)
	}
}

func TestLockFileOfAnotherUserIsNotChanged(t *testing.T) {
	dir := openDataDir(t, 0777)
	dbPath := filepath.Join(dir, "kaf-mirror.db")
	if err := os.WriteFile(LockPath(dbPath), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(LockPath(dbPath), 0666); err != nil {
		t.Fatal(err)
	}
	actAsAnotherUser(t)

	lock, err := AcquireDBLock(dbPath)
	if err != nil {
		t.Fatalf("AcquireDBLock: %v", err)
	}
	lock.Close()
	inUse, err := IsDBLocked(dbPath)
	if err != nil || inUse {
		t.Fatalf("IsDBLocked: %v, %v", inUse, err)
	}
}

func TestServerRefusesAWorldWritableDataDirOfAnotherUser(t *testing.T) {
	dir := openDataDir(t, 0777)
	actAsAnotherUser(t)

	_, _, err := OpenLockedDB(filepath.Join(dir, "kaf-mirror.db"))
	if !errors.Is(err, ErrUnsafeDataDir) {
		t.Fatalf("OpenLockedDB: %v, want ErrUnsafeDataDir", err)
	}
	for _, want := range []string{dir, "mode 0777", "UID", "use storage that honors fsGroup", "set volumePermissions.enabled: true in the Helm values", "chown the directory to the kaf-mirror user outside Helm", "chmod +t"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not name %q", err, want)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "kaf-mirror.db")); !os.IsNotExist(err) {
		t.Fatalf("the database was created in the unsafe directory: %v", err)
	}
}
