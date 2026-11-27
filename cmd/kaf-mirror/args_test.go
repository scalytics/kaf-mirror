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
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestServerFlagsProcess(t *testing.T) {
	raw := os.Getenv("KAF_MIRROR_TEST_ARGS")
	if raw == "" {
		return
	}
	var args []string
	if err := json.Unmarshal([]byte(raw), &args); err != nil {
		panic(err)
	}
	os.Args = append([]string{"kaf-mirror"}, args...)
	main()
	os.Exit(0)
}

func TestServerFlagsHaveNoStartupSideEffects(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		arg, want string
		code      int
	}{
		{"--help", "CONFIG_PATH", 0}, {"-h", "KAF_MIRROR_*", 0}, {"--version", Version, 0},
		{"--unknown", "flag provided but not defined", 1}, {"unexpected", "unexpected argument", 1},
	} {
		t.Run(tc.arg, func(t *testing.T) {
			dir := t.TempDir()
			args, _ := json.Marshal([]string{tc.arg})
			cmd := exec.Command(executable, "-test.run=^TestServerFlagsProcess$")
			cmd.Dir = dir
			cmd.Env = append(os.Environ(), "KAF_MIRROR_TEST_ARGS="+string(args), "CONFIG_PATH="+dir+"/missing.yml")
			out, err := cmd.CombinedOutput()
			code := 0
			if err != nil {
				if exit, ok := err.(*exec.ExitError); ok {
					code = exit.ExitCode()
				} else {
					t.Fatal(err)
				}
			}
			if code != tc.code || !strings.Contains(string(out), tc.want) {
				t.Fatalf("exit %d, output %q; want exit %d containing %q", code, out, tc.code, tc.want)
			}
			entries, err := os.ReadDir(dir)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 0 {
				t.Fatalf("created files: %v", entries)
			}
		})
	}
}

func TestServerWithoutArgumentsStarts(t *testing.T) {
	var out bytes.Buffer
	start, err := serverArgs(nil, &out)
	if err != nil || !start || out.Len() != 0 {
		t.Fatalf("start=%v, error=%v, output=%q", start, err, out.String())
	}
}
