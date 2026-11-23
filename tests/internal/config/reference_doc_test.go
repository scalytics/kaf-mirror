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

package config_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	"kaf-mirror/internal/config"
)

const repoRoot = "../../.."

func structKeys(t reflect.Type, prefix string, out *[]string) {
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		tag := f.Tag.Get("mapstructure")
		if tag == "" || tag == "-" {
			continue
		}
		key := prefix + tag
		switch f.Type.Kind() {
		case reflect.Struct:
			structKeys(f.Type, key+".", out)
		case reflect.Map:
			structKeys(f.Type.Elem(), key+".<name>.", out)
		default:
			*out = append(*out, key)
		}
	}
}

func docSection(t *testing.T, heading string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(repoRoot, "docs", "configuration.md"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	start := strings.Index(text, "\n"+heading+"\n")
	if start < 0 {
		t.Fatalf("docs/configuration.md has no %q section", heading)
	}
	section := text[start+len(heading)+2:]
	if end := strings.Index(section, "\n## "); end >= 0 {
		section = section[:end]
	}
	return section
}

func firstColumnNames(section string) []string {
	row := regexp.MustCompile("(?m)^\\| `([^`]+)` \\|")
	var names []string
	for _, m := range row.FindAllStringSubmatch(section, -1) {
		names = append(names, m[1])
	}
	sort.Strings(names)
	return names
}

func TestConfigReferenceListsEveryKeyTheCodeReads(t *testing.T) {
	var want []string
	structKeys(reflect.TypeOf(config.Config{}), "", &want)
	sort.Strings(want)
	got := firstColumnNames(docSection(t, "## Config keys"))
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("docs/configuration.md config keys do not match config.Config\nmissing from docs: %v\nnot read by the code: %v", diff(want, got), diff(got, want))
	}
}

func TestConfigReferenceListsEveryEnvVarTheCodeReads(t *testing.T) {
	documented := map[string]bool{}
	for _, name := range firstColumnNames(docSection(t, "## Environment variables")) {
		documented[name] = true
	}
	getenv := regexp.MustCompile(`os\.(?:Getenv|LookupEnv)\("([A-Za-z0-9_]+)"\)`)
	found := 0
	err := filepath.WalkDir(repoRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (d.Name() == "tests" || d.Name() == ".git" || d.Name() == "web") {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, m := range getenv.FindAllStringSubmatch(string(raw), -1) {
			found++
			if !documented[m[1]] {
				t.Errorf("%s reads %s, which is not in docs/configuration.md", path, m[1])
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if found == 0 {
		t.Fatal("found no os.Getenv calls, the scan is broken")
	}
	if !documented["KAF_MIRROR_<KEY>"] || !documented["KAF_MIRROR_HALT"] {
		t.Fatal("docs/configuration.md does not document the KAF_MIRROR_ overrides and the default halt variable")
	}
}

func diff(a, b []string) []string {
	in := map[string]bool{}
	for _, s := range b {
		in[s] = true
	}
	var out []string
	for _, s := range a {
		if !in[s] {
			out = append(out, s)
		}
	}
	return out
}
