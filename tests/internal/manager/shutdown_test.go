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
	"context"
	"kaf-mirror/internal/config"
	"kaf-mirror/internal/database"
	"kaf-mirror/internal/kafka"
	"kaf-mirror/internal/manager"
	"kaf-mirror/tests/mocks"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func createShutdownJob(t *testing.T, db *sqlx.DB, status string) string {
	t.Helper()
	jobID := uuid.NewString()
	require.NoError(t, database.CreateJob(db, &database.ReplicationJob{ID: jobID, Name: "job-" + jobID, SourceClusterName: "src", TargetClusterName: "tgt", Status: status}))
	require.NoError(t, database.UpdateMappingsForJob(db, jobID, []database.TopicMapping{{JobID: jobID, SourceTopicPattern: "orders", TargetTopicPattern: "orders", Enabled: true}}))
	return jobID
}

func setupShutdownTest(t *testing.T) (*sqlx.DB, *manager.JobManager) {
	db, jm, _ := setupManagerTest(t)
	require.NoError(t, database.CreateCluster(db, &database.KafkaCluster{Name: "src", Brokers: "localhost:9092", SecurityConfig: "{}"}))
	require.NoError(t, database.CreateCluster(db, &database.KafkaCluster{Name: "tgt", Brokers: "localhost:9093", SecurityConfig: "{}"}))
	return db, jm
}

func jobStatus(t *testing.T, db *sqlx.DB, jobID string) string {
	t.Helper()
	job, err := database.GetJob(db, jobID)
	require.NoError(t, err)
	return job.Status
}

func TestShutdownStopsRunningJobsAndKeepsThemActive(t *testing.T) {
	db, jm := setupShutdownTest(t)
	var stops atomic.Int32
	jm.KafMirrorFactory = func(cfg *config.Config) (kafka.KafMirror, error) {
		return &mocks.MockKafMirror{StopFunc: func() { stops.Add(1) }}, nil
	}
	first := createShutdownJob(t, db, "paused")
	second := createShutdownJob(t, db, "paused")
	require.NoError(t, jm.StartJob(first))
	require.NoError(t, jm.StartJob(second))

	require.NoError(t, jm.Shutdown(context.Background()))

	assert.Equal(t, int32(2), stops.Load())
	assert.Empty(t, jm.KafMirrors)
	assert.True(t, jm.ClosedForTest())
	assert.Equal(t, "active", jobStatus(t, db, first))
	assert.Equal(t, "active", jobStatus(t, db, second))
	assert.ErrorIs(t, jm.StartJob(first), manager.ErrShuttingDown)
	assert.Equal(t, int32(2), stops.Load())
}

func TestShutdownStopsJobsConcurrently(t *testing.T) {
	db, jm := setupShutdownTest(t)
	var both sync.WaitGroup
	both.Add(2)
	jm.KafMirrorFactory = func(cfg *config.Config) (kafka.KafMirror, error) {
		return &mocks.MockKafMirror{StopFunc: func() {
			both.Done()
			both.Wait()
		}}, nil
	}
	require.NoError(t, jm.StartJob(createShutdownJob(t, db, "paused")))
	require.NoError(t, jm.StartJob(createShutdownJob(t, db, "paused")))

	require.NoError(t, jm.Shutdown(context.Background()))
}

func TestShutdownReturnsWhenAJobDoesNotStopInTime(t *testing.T) {
	db, jm := setupShutdownTest(t)
	stopping := make(chan struct{})
	release := make(chan struct{})
	jm.KafMirrorFactory = func(cfg *config.Config) (kafka.KafMirror, error) {
		return &mocks.MockKafMirror{StopFunc: func() {
			close(stopping)
			<-release
		}}, nil
	}
	jobID := createShutdownJob(t, db, "paused")
	require.NoError(t, jm.StartJob(jobID))
	t.Cleanup(func() { close(release) })

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-stopping
		cancel()
	}()
	err := jm.Shutdown(ctx)

	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
	assert.Contains(t, err.Error(), "jobs did not stop before the shutdown timeout")
	assert.Equal(t, "active", jobStatus(t, db, jobID))
}

func TestShutdownDuringJobStartDoesNotStartTheJob(t *testing.T) {
	db, jm := setupShutdownTest(t)
	entered := make(chan struct{})
	proceed := make(chan struct{})
	var started, stopped atomic.Int32
	jm.KafMirrorFactory = func(cfg *config.Config) (kafka.KafMirror, error) {
		close(entered)
		<-proceed
		return &mocks.MockKafMirror{
			StartFunc: func(string, func(database.ReplicationMetric), func(string, string)) { started.Add(1) },
			StopFunc:  func() { stopped.Add(1) },
		}, nil
	}
	jobID := createShutdownJob(t, db, "paused")
	startErr := make(chan error, 1)
	go func() { startErr <- jm.StartJob(jobID) }()
	<-entered

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.Error(t, jm.Shutdown(ctx))
	close(proceed)

	assert.ErrorIs(t, <-startErr, manager.ErrShuttingDown)
	assert.Equal(t, int32(0), started.Load())
	assert.Equal(t, int32(1), stopped.Load())
	assert.Equal(t, "paused", jobStatus(t, db, jobID))
}

func TestReconcileAfterShutdownKeepsActiveJobsActive(t *testing.T) {
	db, jm := setupShutdownTest(t)
	jm.KafMirrorFactory = func(cfg *config.Config) (kafka.KafMirror, error) {
		t.Fatal("a job was started during shutdown")
		return nil, nil
	}
	jobID := createShutdownJob(t, db, "active")
	require.NoError(t, jm.Shutdown(context.Background()))

	require.NoError(t, jm.ReconcileForTest())
	assert.Equal(t, "active", jobStatus(t, db, jobID))
}
