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

func TestReadPasswordLineFromPipedStdin(t *testing.T) {
	got, err := readPasswordLine(strings.NewReader("s3cret\n"), ioDiscard(), "Password:")
	if err != nil {
		t.Fatal(err)
	}
	if got != "s3cret" {
		t.Fatalf("password %q, want s3cret", got)
	}
	if _, err := readPasswordLine(strings.NewReader("\n"), ioDiscard(), "Password:"); err == nil {
		t.Fatal("empty password: want error")
	}
}

type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }

func ioDiscard() discardWriter { return discardWriter{} }
