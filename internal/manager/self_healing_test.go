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

package manager

import (
	"testing"
	"time"

	"kaf-mirror/internal/config"
	"kaf-mirror/internal/database"
	"kaf-mirror/tests/mocks"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
)

func newSelfHealingManager(t *testing.T, remediation bool) (*sqlx.DB, *JobManager, string) {
	t.Helper()
	db, err := database.InitDB(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := database.CreateCluster(db, &database.KafkaCluster{Name: "src", Brokers: "localhost:9092", SecurityConfig: "{}"}); err != nil {
		t.Fatal(err)
	}
	if err := database.CreateCluster(db, &database.KafkaCluster{Name: "tgt", Brokers: "localhost:9093", SecurityConfig: "{}"}); err != nil {
		t.Fatal(err)
	}
	jobID := uuid.NewString()
	if err := database.CreateJob(db, &database.ReplicationJob{ID: jobID, Name: "heal", SourceClusterName: "src", TargetClusterName: "tgt", Status: "active", BatchSize: 100, Compression: "none"}); err != nil {
		t.Fatal(err)
	}
	jm := New(db, &config.Config{
		Server: config.ServerConfig{Mode: "test"},
		AI:     config.AIConfig{SelfHealing: config.SelfHealing{AutomatedRemediation: remediation}},
	}, &mocks.MockHub{})
	t.Cleanup(func() { jm.Close() })
	return db, jm, jobID
}

func insertThroughput(t *testing.T, db *sqlx.DB, jobID string, messages int) {
	t.Helper()
	_, err := db.Exec(`INSERT INTO aggregated_metrics (job_id, timestamp, messages_replicated_delta, bytes_transferred_delta, avg_lag, error_count_delta)
		VALUES (?, datetime('now'), ?, 0, 5, 0)`, jobID, messages)
	if err != nil {
		t.Fatal(err)
	}
}

func TestAutomatedRemediationRestartsAJobWhenAIIsOff(t *testing.T) {
	db, jm, jobID := newSelfHealingManager(t, true)
	if jm.AIClient.Enabled() {
		t.Fatal("AI client is enabled, the test needs AI off")
	}
	insertThroughput(t, db, jobID, 0)
	restarted := make(chan string, 1)
	jm.restartJob = func(id string) error {
		restarted <- id
		return nil
	}
	jm.performSmartAnalysis()
	select {
	case id := <-restarted:
		if id != jobID {
			t.Fatalf("restarted %s, want %s", id, jobID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("automated remediation did not restart the job")
	}
}

func TestAutomatedRemediationDoesNotRestartWhenThroughputIsNonZero(t *testing.T) {
	db, jm, jobID := newSelfHealingManager(t, true)
	insertThroughput(t, db, jobID, 12)
	jm.restartJob = func(string) error {
		t.Fatal("restarted a job that still has throughput")
		return nil
	}
	jm.performSmartAnalysis()
}

func TestAutomatedRemediationStaysOffWhenTheFlagIsFalse(t *testing.T) {
	db, jm, jobID := newSelfHealingManager(t, false)
	insertThroughput(t, db, jobID, 0)
	jm.restartJob = func(string) error {
		t.Fatal("restarted a job with automated_remediation off")
		return nil
	}
	jm.performSmartAnalysis()
}

func TestStartAIAnalysisReturnsWhenAIAndSelfHealingAreOff(t *testing.T) {
	_, jm, _ := newSelfHealingManager(t, false)
	jm.wg.Add(1)
	done := make(chan struct{})
	go func() {
		jm.startAIAnalysis()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("startAIAnalysis ran the loop while AI and self-healing were off")
	}
}
