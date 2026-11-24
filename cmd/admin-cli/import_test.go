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
	"os"
	"path/filepath"
	"strings"
	"testing"

	"kaf-mirror/internal/database"
)

func TestImportDatabaseHasNoMergeFlag(t *testing.T) {
	cmd := newImportDatabaseCmd(new(string))
	addYesFlag(cmd)
	if cmd.Flags().Lookup("merge") != nil {
		t.Fatal("import database still has the --merge flag")
	}
	var help bytes.Buffer
	cmd.SetOut(&help)
	if err := cmd.Help(); err != nil {
		t.Fatal(err)
	}
	text := help.String()
	for _, bad := range []string{"merge", "not yet implemented"} {
		if strings.Contains(strings.ToLower(text), bad) {
			t.Fatalf("import database help mentions %q:\n%s", bad, text)
		}
	}
	if !strings.Contains(text, "--yes") {
		t.Fatalf("import database help has no --yes:\n%s", text)
	}
}

func TestImportDatabaseWithYesWithoutTerminal(t *testing.T) {
	src, writer := newServerDB(t)
	insertCluster(t, writer, "imported")
	writer.Close()

	dst := filepath.Join(t.TempDir(), "kaf-mirror.db")
	target, err := database.InitDB(dst)
	if err != nil {
		t.Fatal(err)
	}
	target.Close()

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	w.Close()
	old := os.Stdin
	os.Stdin = r
	t.Cleanup(func() {
		os.Stdin = old
		r.Close()
	})

	cmd := newImportDatabaseCmd(&dst)
	addYesFlag(cmd)
	cmd.SetArgs([]string{src, "--yes"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if n := countRows(t, dst, "imported"); n != 1 {
		t.Fatalf("imported db has %d rows of the source cluster, want 1", n)
	}
}
