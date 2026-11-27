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
	"io/fs"
	"strings"
	"testing"

	"github.com/scalytics/kaf-mirror/v2/internal/database"
)

func TestStartupErrorOnlyBlamesARestoreWhenTheDatabaseIsLocked(t *testing.T) {
	locked := startupDBError("/app/data/kaf-mirror.db", fmt.Errorf("%w", database.ErrDatabaseInUse))
	if !strings.Contains(locked, "If a restore or import is running") {
		t.Fatalf("locked: %q", locked)
	}

	denied := startupDBError("/app/data/kaf-mirror.db", &fs.PathError{Op: "open", Path: "/app/data/kaf-mirror.db", Err: fs.ErrPermission})
	if strings.Contains(denied, "restore") || strings.Contains(denied, "another kaf-mirror process") {
		t.Fatalf("permission error blames a restore: %q", denied)
	}
	if !strings.Contains(denied, "must be writable by UID") {
		t.Fatalf("permission error: %q", denied)
	}
}
