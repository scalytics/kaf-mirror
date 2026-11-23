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
	"os"
	"path/filepath"
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
	if created || shouldTightenDir(dir) {
		return os.Chmod(dir, privateDirMode)
	}
	return nil
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
	return os.Chmod(dbPath, privateFileMode)
}

func tightenDBSidecars(dbPath string) {
	for _, suffix := range []string{"", "-wal", "-shm", "-journal"} {
		path := dbPath + suffix
		if _, err := os.Stat(path); err == nil {
			_ = os.Chmod(path, privateFileMode)
		}
	}
}
