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
	"errors"
	"os"
	"strings"
	"testing"
)

func TestCompletion(t *testing.T) {
	for _, shell := range []string{"bash", "zsh", "fish", "powershell"} {
		t.Run(shell, func(t *testing.T) {
			var err error
			out := captureStdout(t, func() {
				root := NewRootCmd()
				root.SetArgs([]string{"completion", shell})
				err = root.Execute()
			})
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(out, "mirror-cli") || len(out) < 1000 {
				t.Fatalf("expected a completion script, got %q", out)
			}
		})
	}
}

func TestCompletionRuntimeRequest(t *testing.T) {
	for _, request := range []string{"__complete", "__completeNoDesc"} {
		t.Run(request, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			root := NewRootCmd()
			var out bytes.Buffer
			root.SetOut(&out)
			root.SetArgs([]string{request, "comp"})
			if err := root.Execute(); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(out.String(), "completion") || !strings.HasSuffix(out.String(), ":4\n") {
				t.Fatalf("expected completion in suggestions, got %q", out.String())
			}
		})
	}
	root := NewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"__complete", "completion", ""})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	for _, shell := range []string{"bash", "zsh", "fish", "powershell"} {
		if !strings.Contains(out.String(), shell) {
			t.Fatalf("missing %s in %q", shell, out.String())
		}
	}
}

func TestCompletionUnknownSubcommandExitsWithAnError(t *testing.T) {
	out, err := execute(t, "completion", "nope")
	if err == nil || errors.Is(err, errCommandFailed) {
		t.Fatalf("completion nope returned %v\n%s", err, out)
	}
	if !strings.Contains(err.Error(), `unknown command "nope" for "mirror-cli completion"`) {
		t.Fatalf("error %v", err)
	}
}

func TestBareCompletionPrintsHelp(t *testing.T) {
	root := NewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"completion"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Available Commands") || !strings.Contains(out.String(), "powershell") {
		t.Fatalf("bare completion did not print help:\n%s", out.String())
	}
}

func TestShellMenuListsCompletionAndHelpFromTheStart(t *testing.T) {
	root := NewRootCmd()
	out := captureStdout(t, func() { applyShellInput(root, nil, "") })
	for _, name := range []string{"completion", "help"} {
		if !strings.Contains(out, "  "+name) {
			t.Fatalf("root menu misses %s:\n%s", name, out)
		}
	}
	root.SetArgs([]string{"--help"})
	root.SetOut(&bytes.Buffer{})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	out = captureStdout(t, func() { applyShellInput(root, nil, "") })
	if strings.Contains(out, "__complete") {
		t.Fatalf("menu lists a hidden command:\n%s", out)
	}
	ctx := applyShellInput(root, nil, "completion")
	if len(ctx) != 1 || ctx[0] != "completion" {
		t.Fatalf("context after completion: %v", ctx)
	}
}

func TestGeneratedCompletionDocs(t *testing.T) {
	var out bytes.Buffer
	if err := generateConsolidatedDocs(NewRootCmd(), &out); err != nil {
		t.Fatal(err)
	}
	doc := out.String()
	for _, line := range strings.Split(doc, "\n") {
		if strings.HasPrefix(line, "#### Linux") || strings.HasPrefix(line, "#### macOS") {
			t.Fatalf("shell instructions render as a heading: %q", line)
		}
	}
	for _, shell := range []string{"bash", "zsh", "fish", "powershell"} {
		usage := "mirror-cli completion " + shell + " [flags]"
		if !strings.Contains(doc, usage) {
			t.Fatalf("generated docs miss usage line %q", usage)
		}
	}
	if !strings.Contains(doc, "**Linux:**") || !strings.Contains(doc, "**macOS:**") {
		t.Fatal("generated docs lost the Linux and macOS labels")
	}
}

func TestReadmeShellCompletionMatchesGeneratedDocs(t *testing.T) {
	raw, err := os.ReadFile("../../README.md")
	if err != nil {
		t.Fatal(err)
	}
	readme := string(raw)
	for _, line := range strings.Split(shellCompletionDocs, "\n") {
		if line == "" || strings.HasPrefix(line, "## ") {
			continue
		}
		if !strings.Contains(readme, line) {
			t.Fatalf("README misses a shell completion line: %q", line)
		}
	}
	if !strings.Contains(shellCompletionDocs, "Bash 3.2") {
		t.Fatal("shell completion docs miss the macOS Bash 3.2 note")
	}
}
