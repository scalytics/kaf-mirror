// Copyright 2026 Scalytics, Inc. and Scalytics Europe, LTD
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
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/AlecAivazis/survey/v2"
	"github.com/AlecAivazis/survey/v2/terminal"
)

type jobBackend struct {
	*httptest.Server
	mu       sync.Mutex
	requests []string
}

func newJobBackend(t *testing.T) *jobBackend {
	t.Helper()
	b := &jobBackend{}
	b.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b.mu.Lock()
		b.requests = append(b.requests, r.Method+" "+r.URL.Path)
		b.mu.Unlock()
		switch r.URL.Path {
		case "/auth/me":
			json.NewEncoder(w).Encode(map[string]string{"username": "admin@localhost", "role": "admin"})
		case "/api/v1/jobs":
			json.NewEncoder(w).Encode([]map[string]string{
				{"id": "job-active", "name": "orders", "status": "active"},
				{"id": "job-paused", "name": "payments", "status": "paused"},
			})
		default:
			http.Error(w, "unexpected request", http.StatusNotFound)
		}
	}))
	t.Cleanup(b.Close)
	return b
}

func (b *jobBackend) jobRequests() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []string
	for _, request := range b.requests {
		if !strings.HasSuffix(request, " /auth/me") && request != "GET /api/v1/jobs" {
			out = append(out, request)
		}
	}
	return out
}

func executeInHome(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var err error
	out := captureStdout(t, func() {
		root := NewRootCmd()
		root.SetArgs(args)
		err = root.Execute()
	})
	return out, err
}

var jobSelectionCommands = [][]string{
	{"jobs", "status", "full"},
	{"jobs", "status", "metrics"},
	{"jobs", "start"},
	{"jobs", "stop"},
	{"jobs", "pause"},
	{"jobs", "restart"},
	{"jobs", "force-restart"},
	{"jobs", "delete", "--yes"},
	{"jobs", "analyze"},
	{"jobs", "healthcheck"},
}

func TestJobCommandsWithoutTerminalRefuseWithoutJobID(t *testing.T) {
	for _, command := range jobSelectionCommands {
		t.Run(strings.Join(command, " "), func(t *testing.T) {
			withHome(t)
			withStdin(t, "")
			backend := newJobBackend(t)
			if err := SaveToken(testAPIToken); err != nil {
				t.Fatal(err)
			}
			out, err := executeInHome(t, append([]string{"--mirror-url", backend.URL}, command...)...)
			if !errors.Is(err, errCommandFailed) {
				t.Fatalf("returned %v, want a failed command\n%s", err, out)
			}
			if !strings.Contains(out, "Operation cancelled: no terminal to select a job, pass the job id") {
				t.Fatalf("output does not tell the user to pass the job id:\n%s", out)
			}
			if requests := backend.jobRequests(); len(requests) != 0 {
				t.Fatalf("sent job requests without a job id: %v\n%s", requests, out)
			}
		})
	}
}

func TestJobSelectionAbortedOrEmptyIsRefused(t *testing.T) {
	cases := map[string]struct {
		answer string
		err    error
		want   string
	}{
		"aborted": {err: terminal.InterruptErr, want: "Operation cancelled: job selection aborted"},
		"empty":   {want: "Operation cancelled: no job selected, pass the job id"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			withHome(t)
			backend := newJobBackend(t)
			if err := SaveToken(testAPIToken); err != nil {
				t.Fatal(err)
			}
			restoreTerminal, restoreAsk := jobSelectionHasTerminal, askJobSelection
			t.Cleanup(func() { jobSelectionHasTerminal, askJobSelection = restoreTerminal, restoreAsk })
			jobSelectionHasTerminal = func() bool { return true }
			askJobSelection = func(prompt *survey.Select, answer *string) error {
				*answer = tc.answer
				return tc.err
			}
			out, err := executeInHome(t, "--mirror-url", backend.URL, "jobs", "start")
			if !errors.Is(err, errCommandFailed) || !strings.Contains(out, tc.want) {
				t.Fatalf("returned %v, want %q\n%s", err, tc.want, out)
			}
			if requests := backend.jobRequests(); len(requests) != 0 {
				t.Fatalf("sent job requests without a job id: %v", requests)
			}
		})
	}
}

func TestEmptyJobIDArgumentIsRefused(t *testing.T) {
	withHome(t)
	backend := newJobBackend(t)
	if err := SaveToken(testAPIToken); err != nil {
		t.Fatal(err)
	}
	out, err := executeInHome(t, "--mirror-url", backend.URL, "jobs", "start", "")
	if !errors.Is(err, errCommandFailed) || !strings.Contains(out, "Operation cancelled: no job selected, pass the job id") {
		t.Fatalf("returned %v\n%s", err, out)
	}
	if requests := backend.jobRequests(); len(requests) != 0 {
		t.Fatalf("sent job requests without a job id: %v", requests)
	}
}
