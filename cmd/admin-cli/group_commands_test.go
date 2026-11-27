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
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestGroupCommandsRejectUnknownSubcommandsWithoutDatabase(t *testing.T) {
	for _, group := range []string{"completion", "users", "tokens", "backup", "restore", "import"} {
		t.Run(group, func(t *testing.T) {
			t.Chdir(t.TempDir())
			_, dbPath := writeProbeConfig(t)
			err := run([]string{group, "foo"})
			want := fmt.Sprintf("unknown command %q for %q", "foo", "admin-cli "+group)
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("got %v, want %s", err, want)
			}
			if _, err := os.Stat(dbPath); !os.IsNotExist(err) {
				t.Fatalf("database touched: %v", err)
			}
			if err := run([]string{group}); err != nil {
				t.Fatalf("bare group: %v", err)
			}
			if _, err := os.Stat(dbPath); !os.IsNotExist(err) {
				t.Fatalf("bare group touched database: %v", err)
			}
		})
	}
}
