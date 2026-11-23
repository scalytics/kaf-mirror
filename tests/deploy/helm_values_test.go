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

package deploy_test

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v2"
)

const chartDir = "../../deploy/helm/kaf-mirror"

func valuesPaths(prefix string, node interface{}, out *[]string) {
	m, ok := node.(map[interface{}]interface{})
	if !ok || len(m) == 0 {
		*out = append(*out, prefix)
		return
	}
	for k, v := range m {
		valuesPaths(prefix+"."+k.(string), v, out)
	}
}

func TestEveryHelmValueIsReadByATemplate(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(chartDir, "values.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var values map[interface{}]interface{}
	if err := yaml.Unmarshal(raw, &values); err != nil {
		t.Fatal(err)
	}
	var paths []string
	for k, v := range values {
		valuesPaths(k.(string), v, &paths)
	}
	sort.Strings(paths)

	files, err := filepath.Glob(filepath.Join(chartDir, "templates", "*"))
	if err != nil {
		t.Fatal(err)
	}
	var templates strings.Builder
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		templates.Write(data)
	}
	text := templates.String()

	for _, p := range paths {
		parts := strings.Split(p, ".")
		used := false
		for i := len(parts); i > 0 && !used; i-- {
			ref := regexp.MustCompile(regexp.QuoteMeta(".Values."+strings.Join(parts[:i], ".")) + "([^A-Za-z0-9_.]|$)")
			used = ref.MatchString(text)
		}
		if !used {
			t.Errorf("values.yaml key %s is not read by any template", p)
		}
	}
}
