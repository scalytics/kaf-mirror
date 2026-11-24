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
	"strings"
	"testing"
)

func TestUsersAddHasNoPasswordFlag(t *testing.T) {
	cmd := newAddUserCmd()
	if cmd.Flags().Lookup("password") != nil {
		t.Fatal("admin-cli users add still has a --password flag")
	}
	var help strings.Builder
	cmd.SetOut(&help)
	if err := cmd.Help(); err != nil {
		t.Fatal(err)
	}
	text := help.String()
	if strings.Contains(text, "--password ") || strings.Contains(text, "--password\n") || strings.Contains(text, "-, --password") {
		t.Fatalf("users add help still documents --password:\n%s", text)
	}
}
