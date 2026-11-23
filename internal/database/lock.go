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

	"github.com/jmoiron/sqlx"
)

var ErrDatabaseInUse = errors.New("database is locked by kaf-mirror or by admin-cli restore/import")

var heldLocks sync.Map

type DBLock struct {
	path string
	f    *os.File
}

func LockPath(dbPath string) string {
	return dbPath + ".lock"
}

func AcquireDBLock(dbPath string) (*DBLock, error) {
	path := LockPath(dbPath)
	if existing, ok := heldLocks.Load(path); ok {
		return nil, fmt.Errorf("%w (%s is already held in this process)", ErrDatabaseInUse, existing.(*DBLock).path)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, privateFileMode)
	if err != nil {
		return nil, err
	}
	if err := f.Chmod(privateFileMode); err != nil {
		f.Close()
		return nil, err
	}
	if err := lockFileExclusive(f); err != nil {
		f.Close()
		if isLockBusy(err) {
			return nil, fmt.Errorf("%w", ErrDatabaseInUse)
		}
		return nil, err
	}
	l := &DBLock{path: path, f: f}
	if _, loaded := heldLocks.LoadOrStore(path, l); loaded {
		unlockFile(f)
		f.Close()
		return nil, fmt.Errorf("%w", ErrDatabaseInUse)
	}
	return l, nil
}

func (l *DBLock) Close() error {
	if l == nil || l.f == nil {
		return nil
	}
	heldLocks.Delete(l.path)
	errUnlock := unlockFile(l.f)
	errClose := l.f.Close()
	l.f = nil
	if errUnlock != nil {
		return errUnlock
	}
	return errClose
}

func IsDBLocked(dbPath string) (bool, error) {
	path := LockPath(dbPath)
	if _, ok := heldLocks.Load(path); ok {
		return true, nil
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, privateFileMode)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	defer f.Close()
	if err := f.Chmod(privateFileMode); err != nil {
		return false, err
	}
	if err := lockFileExclusive(f); err != nil {
		if isLockBusy(err) {
			return true, nil
		}
		return false, err
	}
	if err := unlockFile(f); err != nil {
		return false, err
	}
	return false, nil
}

func OpenLockedDB(dbPath string) (*sqlx.DB, *DBLock, error) {
	if dbPath == ":memory:" {
		db, err := InitDB(dbPath)
		return db, nil, err
	}
	if err := EnsurePrivateDir(filepath.Dir(dbPath)); err != nil {
		return nil, nil, err
	}
	lock, err := AcquireDBLock(dbPath)
	if err != nil {
		return nil, nil, err
	}
	db, err := InitDB(dbPath)
	if err != nil {
		lock.Close()
		return nil, nil, err
	}
	return db, lock, nil
}
