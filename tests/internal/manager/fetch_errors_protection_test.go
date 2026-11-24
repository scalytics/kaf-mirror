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

package manager_test

import (
	"testing"

	"kaf-mirror/internal/config"
	"kaf-mirror/internal/database"
	"kaf-mirror/internal/kafka"
	"kaf-mirror/internal/protection"
	"kaf-mirror/tests/mocks"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFetchErrorsDoNotTripTheProduceErrorAutoHalt(t *testing.T) {
	for _, tc := range []struct {
		name        string
		fetchErrors int
		wantHalt    bool
	}{
		{"fetch errors", 200, false},
		{"produce errors", 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, jm, _ := setupManagerTest(t)
			jm.Protection = protection.New(db, config.ProtectionConfig{AutoHalt: config.AutoHaltConfig{Enabled: true}})
			require.NoError(t, jm.Protection.SetEnabled(true))
			require.NoError(t, database.CreateCluster(db, &database.KafkaCluster{Name: "src", Brokers: "localhost:9092", SecurityConfig: "{}"}))
			require.NoError(t, database.CreateCluster(db, &database.KafkaCluster{Name: "tgt", Brokers: "localhost:9093", SecurityConfig: "{}"}))
			jobID := uuid.NewString()
			require.NoError(t, database.CreateJob(db, &database.ReplicationJob{ID: jobID, Name: "fetch", SourceClusterName: "src", TargetClusterName: "tgt", Status: "paused"}))
			require.NoError(t, database.UpdateMappingsForJob(db, jobID, []database.TopicMapping{{JobID: jobID, SourceTopicPattern: "kin", TargetTopicPattern: "kin", Enabled: true}}))
			var report func(database.ReplicationMetric)
			jm.KafMirrorFactory = func(*config.Config) (kafka.KafMirror, error) {
				return &mocks.MockKafMirror{StartFunc: func(_ string, metrics func(database.ReplicationMetric), _ func(string, string)) {
					report = metrics
				}}, nil
			}
			require.NoError(t, jm.StartJob(jobID))

			report(database.ReplicationMetric{JobID: jobID})
			report(database.ReplicationMetric{JobID: jobID, ErrorCount: 200, FetchErrorCount: tc.fetchErrors})

			assert.Equal(t, tc.wantHalt, jm.Protection.Halted())
			stored, err := database.GetLatestMetrics(db, jobID)
			require.NoError(t, err)
			assert.Equal(t, 200, stored.ErrorCount)
		})
	}
}
