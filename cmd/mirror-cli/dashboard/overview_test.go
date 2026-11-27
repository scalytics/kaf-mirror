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

package dashboard

import (
	"strings"
	"testing"

	"github.com/scalytics/kaf-mirror/v2/cmd/mirror-cli/dashboard/core"
)

func TestComplianceOverviewShowsEventTypeAndInitiator(t *testing.T) {
	d := NewDashboard("t", core.DataFetchers{
		FetchLogs: func() ([]map[string]interface{}, error) {
			return []map[string]interface{}{
				{"id": 2, "event_type": "PUT /api/v1/clusters/prod", "initiator": "alice", "details": "x"},
				{"id": 1, "event_type": "topic_health_alert", "initiator": "system", "details": "y"},
			}, nil
		},
	})
	text := d.getComplianceOverview()
	if strings.Contains(text, "Unknown by System") {
		t.Fatalf("overview still shows placeholder values: %q", text)
	}
	if !strings.Contains(text, "PUT clusters/prod by alice") {
		t.Fatalf("overview misses the audited request: %q", text)
	}
	if !strings.Contains(text, "topic_health_alert N/A by system") {
		t.Fatalf("overview misses the system event: %q", text)
	}
}

func TestClustersOverviewShowsStoredStatus(t *testing.T) {
	d := NewDashboard("t", core.DataFetchers{
		FetchClusters: func() ([]map[string]interface{}, error) {
			return []map[string]interface{}{
				{"name": "src", "status": "active"},
				{"name": "tgt", "status": "unreachable"},
			}, nil
		},
	})
	text := d.getClustersOverview()
	for _, want := range []string{"src (active)", "tgt (unreachable)"} {
		if !strings.Contains(text, want) {
			t.Fatalf("overview %q misses %q", text, want)
		}
	}
}
