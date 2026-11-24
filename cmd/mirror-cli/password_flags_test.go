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
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

const resetPassword = "Reset-Pass-4711-xyz"

func TestLoginHasNoPasswordFlag(t *testing.T) {
	login, _, err := NewRootCmd().Find([]string{"login"})
	if err != nil || login.Name() != "login" {
		t.Fatalf("login command not found: %v", err)
	}
	if login.Flags().Lookup("password") != nil {
		t.Fatal("mirror-cli login still has a --password flag")
	}
	add, _, err := NewRootCmd().Find([]string{"users", "add"})
	if err != nil || add.Name() != "add" {
		t.Fatalf("users add command not found: %v", err)
	}
	if add.Flags().Lookup("password") != nil {
		t.Fatal("mirror-cli users add still has a --password flag")
	}
}

func TestGeneratedDocsHaveNoEmptyShorthand(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "cli-*.md")
	if err != nil {
		t.Fatal(err)
	}
	if err := generateConsolidatedDocs(NewRootCmd(), f); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	doc := string(raw)
	if strings.Contains(doc, "  -, --") {
		t.Fatalf("generated docs render a flag without shorthand as \"-, --\":\n%s", regexp.MustCompile(`(?m)^.*-, --.*$`).FindString(doc))
	}
	if !strings.Contains(doc, "      --password-file string") {
		t.Fatal("generated docs miss the --password-file flag of users reset-password")
	}
	if !strings.Contains(doc, "  -v, --version") {
		t.Fatal("generated docs miss a flag with a shorthand")
	}
}

func newResetBackend(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/auth/me", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"username": "admin@localhost", "role": "admin"})
	})
	mux.HandleFunc("/api/v1/users/bob/reset-password", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"new_password": resetPassword})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestResetPasswordDoesNotPrintTheGivenPasswordFile(t *testing.T) {
	withHome(t)
	srv := newResetBackend(t)
	if err := SaveToken(testAPIToken); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "bob-password.txt")
	out := runRoot(t, "--mirror-url", srv.URL, "users", "reset-password", "bob", "--password-file", path, "--yes")
	if !strings.Contains(out, "PASSWORD RESET SUCCESSFUL") {
		t.Fatalf("reset failed:\n%s", out)
	}
	if strings.Contains(out, path) || strings.Contains(out, "bob-password") {
		t.Fatalf("output prints the --password-file path:\n%s", out)
	}
	if strings.Contains(out, resetPassword) {
		t.Fatalf("output prints the password:\n%s", out)
	}
	if !strings.Contains(out, "the file given with --password-file") {
		t.Fatalf("output does not say where the password is:\n%s", out)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != resetPassword+"\n" {
		t.Fatalf("password file %q %v", got, err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("password file mode %v %v, want 0600", info.Mode().Perm(), err)
	}

	missing := filepath.Join(t.TempDir(), "no-such-dir", "secret-name.txt")
	out = runRoot(t, "--mirror-url", srv.URL, "users", "reset-password", "bob", "--password-file", missing, "--yes")
	if !strings.Contains(out, "Failed to write the file given with --password-file") {
		t.Fatalf("missing directory not reported:\n%s", out)
	}
	if strings.Contains(out, "secret-name") || strings.Contains(out, "no-such-dir") {
		t.Fatalf("error prints the --password-file path:\n%s", out)
	}
}

func TestResetPasswordPrintsTheTempPasswordFile(t *testing.T) {
	withHome(t)
	t.Setenv("TMPDIR", t.TempDir())
	srv := newResetBackend(t)
	if err := SaveToken(testAPIToken); err != nil {
		t.Fatal(err)
	}
	out := runRoot(t, "--mirror-url", srv.URL, "users", "reset-password", "bob", "--yes")
	m := regexp.MustCompile(`Password File: (\S+)`).FindStringSubmatch(out)
	if m == nil {
		t.Fatalf("output has no temp password file path:\n%s", out)
	}
	got, err := os.ReadFile(m[1])
	if err != nil || string(got) != resetPassword+"\n" {
		t.Fatalf("temp password file %q %v", got, err)
	}
	if strings.Contains(out, resetPassword) {
		t.Fatalf("output prints the password:\n%s", out)
	}
}
