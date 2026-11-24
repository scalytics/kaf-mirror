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

package database

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"kaf-mirror/pkg/logger"
)

const (
	privateDirMode  = 0700
	privateFileMode = 0600
)

func EnsurePrivateDir(dir string) error {
	if dir == "" {
		return nil
	}
	_, err := os.Stat(dir)
	created := os.IsNotExist(err)
	if err != nil && !created {
		return err
	}
	if err := os.MkdirAll(dir, privateDirMode); err != nil {
		return err
	}
	if created {
		return os.Chmod(dir, privateDirMode)
	}
	if !shouldTightenDir(dir) {
		return nil
	}
	info, err := os.Stat(dir)
	if err != nil {
		return err
	}
	if !ownedByProcess(info) {
		if err := unsafeForeignDir(dir, info); err != nil {
			return err
		}
		warnForeignDir(dir, info)
		return nil
	}
	return os.Chmod(dir, privateDirMode)
}

var ErrUnsafeDataDir = errors.New("unsafe data directory")

func unsafeForeignDir(dir string, info os.FileInfo) error {
	mode := info.Mode()
	if mode.Perm()&0002 == 0 || mode&os.ModeSticky != 0 {
		return nil
	}
	owner := "another user"
	if uid, gid, ok := fileOwner(info); ok {
		owner = fmt.Sprintf("UID %d and GID %d", uid, gid)
	}
	return fmt.Errorf("%w: %s has mode %04o, belongs to %s and every user can write to it, so another user could replace the database files in it. Give the directory to the kaf-mirror user (chown %d %s), take away write access for others (chmod o-w %s, kaf-mirror then needs write access as the owner or through the group), or set the sticky bit (chmod +t %s)", ErrUnsafeDataDir, dir, mode.Perm(), owner, processUID(), dir, dir, dir)
}

var warnedDirs sync.Map

func warnForeignDir(dir string, info os.FileInfo) {
	if info.Mode().Perm()&0077 == 0 {
		return
	}
	if _, loaded := warnedDirs.LoadOrStore(dir, true); loaded {
		return
	}
	logger.Warn("Data directory %s has mode %04o and belongs to another user, so kaf-mirror leaves its mode alone. The database files in it are still created with mode 0600.", dir, info.Mode().Perm())
}

func shouldTightenDir(dir string) bool {
	abs, err := filepath.Abs(dir)
	if err != nil {
		abs = filepath.Clean(dir)
	}
	if abs == string(filepath.Separator) {
		return false
	}
	if wd, err := os.Getwd(); err == nil {
		if abs == wd {
			return false
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		if abs == filepath.Clean(home) {
			return false
		}
	}
	for _, tmp := range []string{os.TempDir(), "/tmp", "/var/tmp"} {
		t, err := filepath.Abs(tmp)
		if err != nil {
			t = filepath.Clean(tmp)
		}
		if abs == t {
			return false
		}
	}
	return true
}

func prepareDBFile(dbPath string) error {
	if err := EnsurePrivateDir(filepath.Dir(dbPath)); err != nil {
		return err
	}
	info, err := os.Stat(dbPath)
	if os.IsNotExist(err) {
		f, err := os.OpenFile(dbPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, privateFileMode)
		if err != nil {
			return err
		}
		if err := f.Chmod(privateFileMode); err != nil {
			f.Close()
			os.Remove(dbPath)
			return err
		}
		return f.Close()
	}
	if err != nil {
		return err
	}
	if info.IsDir() {
		return os.ErrInvalid
	}
	return tightenExistingFile(dbPath, info)
}

func tightenExistingFile(path string, info os.FileInfo) error {
	if ownedByProcess(info) {
		return os.Chmod(path, privateFileMode)
	}
	if info.Mode().Perm()&0077 != 0 {
		return fmt.Errorf("%s has mode %04o and belongs to another user, so kaf-mirror cannot make it private; change its owner to the kaf-mirror user or its mode to 0600", path, info.Mode().Perm())
	}
	return nil
}

func tightenOpenFile(f *os.File) error {
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if !ownedByProcess(info) {
		return nil
	}
	return f.Chmod(privateFileMode)
}

func tightenDBSidecars(dbPath string) {
	for _, suffix := range []string{"", "-wal", "-shm", "-journal"} {
		path := dbPath + suffix
		if info, err := os.Stat(path); err == nil && ownedByProcess(info) {
			_ = os.Chmod(path, privateFileMode)
		}
	}
}
