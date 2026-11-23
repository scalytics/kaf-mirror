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

package widgets

import (
	"strings"
	"testing"

	"kaf-mirror/cmd/mirror-cli/dashboard/core"
)

func TestClustersListShowsReachability(t *testing.T) {
	dm := core.NewDataManager("t", core.DataFetchers{
		FetchClusters: func() ([]map[string]interface{}, error) {
			return []map[string]interface{}{
				{"name": "src", "provider": "plain", "status": "active", "brokers": "a:9092"},
				{"name": "tgt", "provider": "plain", "status": "unreachable", "brokers": "b:9092"},
				{"name": "new", "provider": "plain", "status": "unknown", "brokers": "c:9092"},
			}, nil
		},
	})
	cf := NewClustersFactory()
	if err := cf.UpdateListData(dm); err != nil {
		t.Fatal(err)
	}
	rows := strings.Join(cf.listWidget.Rows, "\n")
	for _, want := range []string{"✓ active | ✓ Reachable", "✗ unreachable | ✗ Unreachable", "unknown | ? Not checked yet"} {
		if !strings.Contains(rows, want) {
			t.Fatalf("rows %q miss %q", rows, want)
		}
	}
}

func TestClusterDetailShowsUnreachable(t *testing.T) {
	dm := core.NewDataManager("t", core.DataFetchers{
		FetchCluster: func(name string) (map[string]interface{}, error) {
			return map[string]interface{}{"name": name, "status": "unreachable", "brokers": "b:9092"}, nil
		},
	})
	cf := NewClustersFactory()
	if err := cf.UpdateDetailData(dm, "tgt"); err != nil {
		t.Fatal(err)
	}
	rows := strings.Join(cf.detailWidget.Rows, "\n")
	if !strings.Contains(rows, "Unreachable at the last check") {
		t.Fatalf("detail rows %q do not report the unreachable cluster", rows)
	}
}

func TestComplianceListShowsResourceAndDetails(t *testing.T) {
	dm := core.NewDataManager("t", core.DataFetchers{
		FetchLogs: func() ([]map[string]interface{}, error) {
			return []map[string]interface{}{{"id": 1, "timestamp": "2026-11-23T10:00:00Z", "event_type": "DELETE /api/v1/jobs/abc", "initiator": "bob", "details": "Deleted job"}}, nil
		},
	})
	cf := NewComplianceFactory()
	if err := cf.UpdateListData(dm); err != nil {
		t.Fatal(err)
	}
	rows := strings.Join(cf.listWidget.Rows, "\n")
	if !strings.Contains(rows, "bob | DELETE | jobs/abc | Deleted job") {
		t.Fatalf("rows %q miss the audited request", rows)
	}
}
