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
	"strings"
	"testing"
)

func execute(t *testing.T, args ...string) (string, error) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	var err error
	out := captureStdout(t, func() {
		root := NewRootCmd()
		root.SetArgs(args)
		err = root.Execute()
	})
	return out, err
}

func TestFailedCommandExitsWithAnError(t *testing.T) {
	out, err := execute(t, "--mirror-url", "http://127.0.0.1:1", "jobs", "delete", "00000000-0000-0000-0000-000000000000")
	if !errors.Is(err, errCommandFailed) {
		t.Fatalf("jobs delete of a missing id returned %v\n%s", err, out)
	}
	if !strings.Contains(out, "Error:") {
		t.Fatalf("stdout:\n%s", out)
	}
}

func TestUnknownSubcommandExitsWithAnError(t *testing.T) {
	out, err := execute(t, "clusters", "delete", "nope")
	if err == nil || errors.Is(err, errCommandFailed) {
		t.Fatalf("clusters delete returned %v\n%s", err, out)
	}
	if !strings.Contains(err.Error(), `unknown command "delete"`) {
		t.Fatalf("error %v\n%s", err, out)
	}
}

func TestClusterAddRunRemainsCallable(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := NewRootCmd()
	add, _, err := root.Find([]string{"clusters", "add"})
	if err != nil {
		t.Fatal(err)
	}
	out := captureStdout(t, func() { add.Run(add, nil) })
	if !strings.Contains(out, "Error:") {
		t.Fatalf("clusters add Run did not run:\n%s", out)
	}
}

func TestShellGroupCommandsEnterMenus(t *testing.T) {
	root := NewRootCmd()
	var ctx []string
	out := captureStdout(t, func() {
		ctx = applyShellInput(root, ctx, "clusters")
	})
	if len(ctx) != 1 || ctx[0] != "clusters" {
		t.Fatalf("context after clusters: %v", ctx)
	}
	if !strings.Contains(out, "Available commands:") || !strings.Contains(out, "list") {
		t.Fatalf("clusters menu:\n%s", out)
	}
	ctx = applyShellInput(root, ctx, "back")
	if len(ctx) != 0 {
		t.Fatalf("context after back: %v", ctx)
	}
	ctx = applyShellInput(root, nil, "jobs")
	if len(ctx) != 1 || ctx[0] != "jobs" {
		t.Fatalf("context after jobs: %v", ctx)
	}
	ctx = applyShellInput(root, ctx, "root")
	if len(ctx) != 0 {
		t.Fatalf("context after root: %v", ctx)
	}
}

func TestHelpAndBareGroupExitZero(t *testing.T) {
	if _, err := execute(t, "--help"); err != nil {
		t.Fatal(err)
	}
	if _, err := execute(t, "clusters"); err != nil {
		t.Fatal(err)
	}
}
