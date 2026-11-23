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
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testAPIToken = "88c4535b299e72dc8f9994b83f3360cad78e20faec4feedc05cd50a9bb406c32"

type fakeBackend struct {
	*httptest.Server
	loginPassword string
}

func newFakeBackend(t *testing.T) *fakeBackend {
	t.Helper()
	fb := &fakeBackend{}
	mux := http.NewServeMux()
	mux.HandleFunc("/auth/me", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+testAPIToken {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"username": "admin@localhost", "role": "admin"})
	})
	mux.HandleFunc("/auth/token", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		json.NewDecoder(r.Body).Decode(&body)
		fb.loginPassword = body["password"]
		json.NewEncoder(w).Encode(map[string]string{"token": testAPIToken})
	})
	mux.HandleFunc("/api/v1/config", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.Write([]byte(`{}`))
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	fb.Server = httptest.NewServer(mux)
	t.Cleanup(fb.Close)
	return fb
}

func withHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	return home
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = w
	done := make(chan string)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()
	defer func() { os.Stdout = old }()
	fn()
	w.Close()
	return <-done
}

func withStdin(t *testing.T, input string) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.WriteString(input); err != nil {
		t.Fatal(err)
	}
	w.Close()
	old := os.Stdin
	os.Stdin = r
	t.Cleanup(func() {
		os.Stdin = old
		r.Close()
	})
}

func runRoot(t *testing.T, args ...string) string {
	t.Helper()
	return captureStdout(t, func() {
		root := NewRootCmd()
		root.SetArgs(args)
		root.Execute()
	})
}

func TestWhoamiDoesNotPrintToken(t *testing.T) {
	withHome(t)
	fb := newFakeBackend(t)
	if err := SaveToken(testAPIToken); err != nil {
		t.Fatal(err)
	}
	out := runRoot(t, "--mirror-url", fb.URL, "whoami")
	if strings.Contains(out, testAPIToken) || strings.Contains(out, testAPIToken[:16]) {
		t.Fatalf("whoami prints the token:\n%s", out)
	}
	if !strings.Contains(out, "admin@localhost") || !strings.Contains(out, "Role: admin") {
		t.Fatalf("whoami output misses the user:\n%s", out)
	}
}

func TestLoginReadsPasswordFromPipeAndHidesToken(t *testing.T) {
	home := withHome(t)
	fb := newFakeBackend(t)
	withStdin(t, "piped-pass-123\n")
	out := runRoot(t, "--mirror-url", fb.URL, "login", "admin@localhost")
	if fb.loginPassword != "piped-pass-123" {
		t.Fatalf("server got password %q, want the piped one; output:\n%s", fb.loginPassword, out)
	}
	if strings.Contains(out, testAPIToken) || strings.Contains(out, "piped-pass-123") {
		t.Fatalf("login prints a secret:\n%s", out)
	}
	info, err := os.Stat(filepath.Join(home, ".kaf-mirror", "token"))
	if err != nil {
		t.Fatalf("token not stored: %v", err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("token file mode %v, want 0600", info.Mode().Perm())
	}
}

func TestSaveTokenTightensExistingFile(t *testing.T) {
	home := withHome(t)
	dir := filepath.Join(home, ".kaf-mirror")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "token")
	if err := os.WriteFile(path, []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if err := SaveToken(testAPIToken); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("token file mode %v, want 0600", info.Mode().Perm())
	}
	got, err := LoadToken()
	if err != nil || got != testAPIToken {
		t.Fatalf("LoadToken %q %v", got, err)
	}
}

func TestConfigUpdateDoesNotPrintSecrets(t *testing.T) {
	withHome(t)
	fb := newFakeBackend(t)
	if err := SaveToken(testAPIToken); err != nil {
		t.Fatal(err)
	}
	BackendURL = fb.URL
	secrets := map[string]string{
		"ai.token":                    "sk-proj-verysecretvalue",
		"ai.api_secret":               "custom-api-secret-value",
		"monitoring.splunk.hec_token": "hec-0123456789abcdef",
	}
	for path, secret := range secrets {
		out := captureStdout(t, func() { updateConfigParameter(path, secret) })
		if !strings.Contains(out, "Successfully updated "+path) {
			t.Fatalf("%s: update failed:\n%s", path, out)
		}
		if strings.Contains(out, secret) || strings.Contains(out, secret[:4]) {
			t.Fatalf("%s: output shows the secret:\n%s", path, out)
		}
	}
	out := captureStdout(t, func() { updateConfigParameter("ai.model", "gpt-4") })
	if !strings.Contains(out, "ai.model = gpt-4") {
		t.Fatalf("plain value hidden:\n%s", out)
	}
}

func TestMaskSensitiveValueShowsNoCharacters(t *testing.T) {
	for _, v := range []string{"sk-abcdefgh", "abc", "12345"} {
		got := maskSensitiveValue(v)
		if strings.Contains(got, v[:2]) {
			t.Fatalf("maskSensitiveValue(%q) = %q shows part of the value", v, got)
		}
	}
	if maskSensitiveValue("") != "" || maskSensitiveValue("not set") != "not set" {
		t.Fatal("unset values must stay readable")
	}
}

func TestWritePasswordFileIsPrivate(t *testing.T) {
	path, err := writePasswordFile("", "generated-pass")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(path) })
	existing := filepath.Join(t.TempDir(), "pw.txt")
	if err := os.WriteFile(existing, []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(existing, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := writePasswordFile(existing, "generated-pass"); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{path, existing} {
		info, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0600 {
			t.Fatalf("%s mode %v, want 0600", p, info.Mode().Perm())
		}
		got, _ := os.ReadFile(p)
		if string(got) != "generated-pass\n" {
			t.Fatalf("%s content %q", p, got)
		}
	}
}
