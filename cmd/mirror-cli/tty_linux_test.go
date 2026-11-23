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
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

const cliArgsEnv = "KAF_MIRROR_CLI_TEST_ARGS"

func TestMain(m *testing.M) {
	if raw, ok := os.LookupEnv(cliArgsEnv); ok {
		var args []string
		if err := json.Unmarshal([]byte(raw), &args); err != nil {
			os.Exit(3)
		}
		os.Args = append([]string{"mirror-cli"}, args...)
		main()
		os.Exit(0)
	}
	os.Exit(m.Run())
}

type cliResult struct {
	code   int
	stdout string
	stderr string
}

func runCLIWithoutTTY(t *testing.T, args ...string) cliResult {
	t.Helper()
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^$")
	cmd.Env = append(os.Environ(), cliArgsEnv+"="+string(raw), "HOME="+t.TempDir())
	cmd.Stdin = nil
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	err = cmd.Run()
	if ctx.Err() != nil {
		t.Fatalf("mirror-cli %v hung without a terminal", args)
	}
	code := 0
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		code = exitErr.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return cliResult{code: code, stdout: stdout.String(), stderr: stderr.String()}
}

func countingBackend(t *testing.T) (*httptest.Server, *int64) {
	t.Helper()
	var calls int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&calls, 1)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(srv.Close)
	return srv, &calls
}

func assertNoPanic(t *testing.T, args []string, r cliResult) {
	t.Helper()
	if strings.Contains(r.stderr, "panic:") || strings.Contains(r.stdout, "panic:") {
		t.Fatalf("mirror-cli %v panicked:\n%s%s", args, r.stdout, r.stderr)
	}
}

func TestHelpWithoutTTY(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"-h"}, {"jobs", "--help"}, {"help"}} {
		r := runCLIWithoutTTY(t, args...)
		assertNoPanic(t, args, r)
		if r.code != 0 {
			t.Fatalf("mirror-cli %v exit %d, want 0:\n%s%s", args, r.code, r.stdout, r.stderr)
		}
		if !strings.Contains(r.stdout, "Usage:") {
			t.Fatalf("mirror-cli %v printed no help:\n%s", args, r.stdout)
		}
	}
}

func TestVersionWithoutTTY(t *testing.T) {
	for _, args := range [][]string{{"--version"}, {"-v"}} {
		r := runCLIWithoutTTY(t, args...)
		assertNoPanic(t, args, r)
		if r.code != 0 {
			t.Fatalf("mirror-cli %v exit %d, want 0:\n%s%s", args, r.code, r.stdout, r.stderr)
		}
	}
}

func TestShellWithoutTTYFailsCleanly(t *testing.T) {
	srv, calls := countingBackend(t)
	for _, args := range [][]string{{}, {"--mirror-url", srv.URL}} {
		r := runCLIWithoutTTY(t, args...)
		assertNoPanic(t, args, r)
		if r.code != 1 {
			t.Fatalf("mirror-cli %v exit %d, want 1:\n%s%s", args, r.code, r.stdout, r.stderr)
		}
		if !strings.Contains(r.stderr, "needs a terminal") {
			t.Fatalf("mirror-cli %v: no clear message:\n%s", args, r.stderr)
		}
	}
	if n := atomic.LoadInt64(calls); n != 0 {
		t.Fatalf("shell without a terminal called the backend %d times", n)
	}
}

func TestDashboardWithoutTTYFailsCleanly(t *testing.T) {
	srv, _ := countingBackend(t)
	args := []string{"--mirror-url", srv.URL, "dashboard"}
	r := runCLIWithoutTTY(t, args...)
	assertNoPanic(t, args, r)
	if r.code != 1 || !strings.Contains(r.stderr, "needs a terminal") {
		t.Fatalf("dashboard without a terminal: exit %d\n%s%s", r.code, r.stdout, r.stderr)
	}
}
