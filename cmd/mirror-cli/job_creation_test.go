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
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/AlecAivazis/survey/v2"
	"github.com/AlecAivazis/survey/v2/terminal"
)

func TestJobCreationCancellation(t *testing.T) {
	for _, failAt := range []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10} {
		t.Run(fmt.Sprint(failAt), func(t *testing.T) {
			withHome(t)
			withStdin(t, "")
			var created atomic.Int32
			backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.URL.Path == "/auth/me":
					json.NewEncoder(w).Encode(map[string]string{"username": "admin@localhost", "role": "admin"})
				case r.URL.Path == "/api/v1/clusters":
					json.NewEncoder(w).Encode([]map[string]string{{"name": "source"}, {"name": "target"}})
				case strings.HasSuffix(r.URL.Path, "/topics"):
					json.NewEncoder(w).Encode([]string{"orders"})
				case strings.HasSuffix(r.URL.Path, "/topic-details"):
					json.NewEncoder(w).Encode([]map[string]interface{}{{"name": "orders", "partitions": 1}})
				case r.Method == http.MethodPost && r.URL.Path == "/api/v1/jobs":
					created.Add(1)
					w.WriteHeader(http.StatusCreated)
				default:
					http.Error(w, "unexpected request", 404)
				}
			}))
			defer backend.Close()
			if err := SaveToken(testAPIToken); err != nil {
				t.Fatal(err)
			}
			oldTerminal, oldAsk := jobCreationHasTerminal, askJobCreation
			t.Cleanup(func() { jobCreationHasTerminal, askJobCreation = oldTerminal, oldAsk })
			jobCreationHasTerminal = func() bool { return failAt != 0 }
			prompts := 0
			askJobCreation = func(p survey.Prompt, answer interface{}, opts ...survey.AskOpt) error {
				prompts++
				if prompts == failAt {
					return terminal.InterruptErr
				}
				switch prompts {
				case 1:
					*answer.(*string) = "orders-copy"
				case 2:
					*answer.(*string) = "source"
				case 3:
					*answer.(*string) = "target"
				case 4:
					*answer.(*[]string) = []string{"orders"}
				case 5:
					*answer.(*string) = "1000"
				case 6:
					*answer.(*string) = "none"
				case 7:
					*answer.(*bool) = true
				case 8:
					*answer.(*string) = "orders-copy"
				case 9:
					*answer.(*bool) = true
				default:
					t.Fatalf("unexpected prompt %d", prompts)
				}
				return nil
			}
			out, err := executeInHome(t, "--mirror-url", backend.URL, "jobs", "add")
			if failAt == 10 {
				if err != nil || created.Load() != 1 {
					t.Fatalf("success: err=%v, jobs=%d\n%s", err, created.Load(), out)
				}
				return
			}
			if !errors.Is(err, errCommandFailed) || !strings.Contains(out, "Operation cancelled") {
				t.Fatalf("err=%v\n%s", err, out)
			}
			if prompts != failAt || created.Load() != 0 {
				t.Fatalf("prompts=%d, want %d; jobs=%d", prompts, failAt, created.Load())
			}
			if failAt == 0 && (strings.Contains(out, "Enter job name") || !strings.Contains(out, "no terminal")) {
				t.Fatalf("no terminal: %s", out)
			}
		})
	}
}
