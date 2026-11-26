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
	"strings"
	"testing"

	"gopkg.in/yaml.v2"
)

func readFile(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func hasValuesPath(values map[interface{}]interface{}, path string) bool {
	var node interface{} = values
	for _, part := range strings.Split(path, ".") {
		m, ok := node.(map[interface{}]interface{})
		if !ok {
			return false
		}
		node, ok = m[part]
		if !ok {
			return false
		}
	}
	return true
}

func TestEveryTemplateValueIsInValuesAndReadme(t *testing.T) {
	var values map[interface{}]interface{}
	if err := yaml.Unmarshal([]byte(readFile(t, filepath.Join(chartDir, "values.yaml"))), &values); err != nil {
		t.Fatal(err)
	}
	readme := readFile(t, filepath.Join(chartDir, "README.md"))

	files, err := filepath.Glob(filepath.Join(chartDir, "templates", "*"))
	if err != nil {
		t.Fatal(err)
	}
	ref := regexp.MustCompile(`\.Values\.([A-Za-z0-9_]+(?:\.[A-Za-z0-9_]+)*)`)
	seen := map[string]bool{}
	for _, f := range files {
		for _, m := range ref.FindAllStringSubmatch(readFile(t, f), -1) {
			seen[m[1]] = true
		}
	}
	if len(seen) == 0 {
		t.Fatal("no .Values references found in the templates")
	}
	for path := range seen {
		if !hasValuesPath(values, path) {
			t.Errorf("templates read .Values.%s, which is not in values.yaml", path)
		}
		parts := strings.Split(path, ".")
		listed := false
		for i := len(parts); i > 0 && !listed; i-- {
			listed = strings.Contains(readme, "| `"+strings.Join(parts[:i], ".")+"` |")
		}
		if !listed {
			t.Errorf("templates read .Values.%s, which is not in the Values table of the chart README", path)
		}
	}
}

func TestEveryDocumentedHelmCommandKeepsTheImageValues(t *testing.T) {
	helmCmd := regexp.MustCompile(`(?m)^helm (install|upgrade)\b.*(?:\\\n.*)*`)
	for _, doc := range []string{filepath.Join(chartDir, "README.md"), "../../README.md"} {
		cmds := helmCmd.FindAllString(readFile(t, doc), -1)
		if len(cmds) == 0 {
			t.Errorf("%s has no helm install or upgrade command", doc)
		}
		for _, cmd := range cmds {
			if strings.Contains(cmd, "--set") {
				t.Errorf("%s: %q uses --set; put the setting in a values file", doc, cmd)
			}
			fields := strings.Fields(cmd)
			first := ""
			for i, f := range fields {
				if f == "-f" && i+1 < len(fields) {
					first = fields[i+1]
					break
				}
			}
			if first != "km-values.yaml" {
				t.Errorf("%s: %q does not pass -f km-values.yaml as its first values file", doc, cmd)
			}
		}
	}
}

func TestInstallDocsNameTheImageLoadAndPodSecurity(t *testing.T) {
	chart := readFile(t, filepath.Join(chartDir, "README.md"))
	root := readFile(t, "../../README.md")
	config := readFile(t, "../../docs/configuration.md")
	for _, doc := range []string{chart, root} {
		for _, phrase := range []string{
			"kind load docker-image kaf-mirror:local",
			"kind load image-archive kaf-mirror-local.tar",
			"minikube image load kaf-mirror:local",
			"restricted Pod Security standard",
			"curl -sk https://localhost:8088/health",
		} {
			if !strings.Contains(doc, phrase) {
				t.Errorf("missing %q", phrase)
			}
		}
	}
	if !strings.Contains(config, "restricted Pod Security standard") {
		t.Error("configuration.md does not mention the restricted Pod Security standard")
	}
	combined := " -f tls-values.yaml -f secret-values.yaml"
	if strings.Index(chart, combined) < strings.Index(chart, "cat > secret-values.yaml") {
		t.Error("chart README runs the TLS and Secret upgrade before secret-values.yaml is written")
	}
	if strings.Contains(root, "\nchgrp 101 ") {
		t.Error("README still runs chgrp 101 without sudo")
	}
	for _, line := range []string{
		"sudo chgrp 101 tls.key && sudo chmod 0440 tls.key",
		"sudo chgrp 101 kafka.keytab && sudo chmod 0440 kafka.keytab",
		"chmod 0444 tls.key",
		"chmod 0444 kafka.keytab",
	} {
		if !strings.Contains(root, line) {
			t.Errorf("README missing %q", line)
		}
	}
}

func TestChartReadmeRestartsPortForwardOncePerSection(t *testing.T) {
	chart := readFile(t, filepath.Join(chartDir, "README.md"))
	start := strings.Index(chart, "## TLS\n")
	end := strings.Index(chart, "## AI analysis\n")
	if start < 0 || end < start {
		t.Fatal("TLS or AI analysis heading missing")
	}
	if n := strings.Count(chart[start:end], "port-forward"); n != 3 {
		t.Fatalf("TLS, Kafka TLS and Secret mention port-forward %d times, want once each", n)
	}
}
