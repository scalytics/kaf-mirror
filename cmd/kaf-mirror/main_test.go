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
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteSecretFileWritesIntoGivenDirWithOwnerOnlyMode(t *testing.T) {
	dir := t.TempDir()

	path, err := writeSecretFile(dir, "kaf-mirror-bootstrap-*", "s3cret")
	if err != nil {
		t.Fatal(err)
	}

	if !filepath.IsAbs(path) {
		t.Fatalf("path %q is not absolute", path)
	}
	if filepath.Dir(path) != dir {
		t.Fatalf("file %q is not in %q", path, dir)
	}
	if !strings.HasPrefix(filepath.Base(path), "kaf-mirror-bootstrap-") {
		t.Fatalf("unexpected name %q", path)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode is %v, want 0600", info.Mode().Perm())
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "s3cret\n" {
		t.Fatalf("content is %q", data)
	}
}

func TestWriteSecretFileFailsForMissingDir(t *testing.T) {
	if _, err := writeSecretFile(filepath.Join(t.TempDir(), "missing"), "kaf-mirror-bootstrap-*", "s3cret"); err == nil {
		t.Fatal("expected an error for a missing directory")
	}
}
