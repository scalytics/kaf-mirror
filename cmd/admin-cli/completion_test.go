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

func TestCompletionWithoutConfigOrDatabase(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("CONFIG_PATH", filepath.Join(t.TempDir(), "missing.yml"))
	cases := map[string][]string{
		"bash":                    {"completion", "bash"},
		"zsh":                     {"completion", "zsh"},
		"fish":                    {"completion", "fish"},
		"powershell":              {"completion", "powershell"},
		"request":                 {"__complete", "us"},
		"request-no-descriptions": {"__completeNoDesc", "us"},
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			out, err := os.CreateTemp(t.TempDir(), "completion")
			if err != nil {
				t.Fatal(err)
			}
			defer out.Close()
			original := os.Stdout
			os.Stdout = out
			defer func() { os.Stdout = original }()
			if err := run(args); err != nil {
				t.Fatal(err)
			}
			raw, err := os.ReadFile(out.Name())
			if err != nil {
				t.Fatal(err)
			}
			if strings.HasPrefix(name, "request") {
				if !strings.Contains(string(raw), "users") || !strings.HasSuffix(string(raw), ":4\n") {
					t.Fatalf("expected users completion, got %q", raw)
				}
			} else if !strings.Contains(string(raw), "admin-cli") || len(raw) < 1000 {
				t.Fatalf("expected a completion script, got %q", raw)
			}
			if db != nil {
				t.Fatal("completion opened the database")
			}
		})
	}
}
