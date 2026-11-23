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
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"

	"kaf-mirror/internal/database"

	"github.com/jmoiron/sqlx"
	_ "github.com/mattn/go-sqlite3"
)

const privateFileMode = 0600

func createPrivateFile(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, privateFileMode)
	if err != nil {
		if errors.Is(err, fs.ErrExist) {
			return nil, fmt.Errorf("%s already exists, choose a new path", path)
		}
		return nil, err
	}
	if err := f.Chmod(privateFileMode); err != nil {
		f.Close()
		os.Remove(path)
		return nil, err
	}
	return f, nil
}

func backupDatabase(db *sqlx.DB, outputPath string) error {
	f, err := createPrivateFile(outputPath)
	if err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(outputPath)
		return err
	}
	if _, err := db.Exec("VACUUM INTO ?", outputPath); err != nil {
		os.Remove(outputPath)
		return fmt.Errorf("write consistent copy: %w", err)
	}
	if err := os.Chmod(outputPath, privateFileMode); err != nil {
		os.Remove(outputPath)
		return err
	}
	return nil
}

func copyPrivateFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := createPrivateFile(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(dst)
		return err
	}
	if err := out.Sync(); err != nil {
		out.Close()
		os.Remove(dst)
		return err
	}
	return out.Close()
}

func openReadOnly(path string) (*sqlx.DB, error) {
	u := url.URL{Scheme: "file", Opaque: path, RawQuery: "mode=ro&_busy_timeout=5000"}
	return sqlx.Open("sqlite3", u.String())
}

func checkDatabaseFile(path string) error {
	src, err := openReadOnly(path)
	if err != nil {
		return err
	}
	defer src.Close()
	var result string
	if err := src.Get(&result, "PRAGMA quick_check"); err != nil {
		return fmt.Errorf("%s is not a readable SQLite database: %w", path, err)
	}
	if result != "ok" {
		return fmt.Errorf("%s failed the SQLite integrity check: %s", path, result)
	}
	return nil
}

func resolveDBPath(dbPath string) (string, error) {
	info, err := os.Lstat(dbPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return dbPath, nil
		}
		return "", err
	}
	if info.Mode()&os.ModeSymlink == 0 {
		return dbPath, nil
	}
	resolved, err := filepath.EvalSymlinks(dbPath)
	if err != nil {
		return "", err
	}
	return resolved, nil
}

func databaseInUse(dbPath string) (bool, error) {
	return database.IsDBLocked(dbPath)
}

func restoreDatabase(backupPath, dbPath string) error {
	dbPath, err := resolveDBPath(dbPath)
	if err != nil {
		return err
	}
	inUse, err := databaseInUse(dbPath)
	if err != nil {
		return fmt.Errorf("check whether %s is in use: %w", dbPath, err)
	}
	if inUse {
		return fmt.Errorf("%s: %w", dbPath, database.ErrDatabaseInUse)
	}
	if err := checkDatabaseFile(backupPath); err != nil {
		return err
	}
	src, err := openReadOnly(backupPath)
	if err != nil {
		return err
	}
	defer src.Close()

	dir := filepath.Dir(dbPath)
	if err := database.EnsurePrivateDir(dir); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".restore-*.db")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	if _, err := src.Exec("VACUUM INTO ?", tmpPath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("copy %s: %w", backupPath, err)
	}
	if err := os.Chmod(tmpPath, privateFileMode); err != nil {
		os.Remove(tmpPath)
		return err
	}
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		if err := os.Remove(dbPath + suffix); err != nil && !errors.Is(err, fs.ErrNotExist) {
			os.Remove(tmpPath)
			return err
		}
	}
	if err := os.Rename(tmpPath, dbPath); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return nil
}
