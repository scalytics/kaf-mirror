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
	"kaf-mirror/internal/config"
	"kaf-mirror/internal/database"
	"kaf-mirror/internal/kafka"
	"kaf-mirror/tests/mocks"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type trackedMirror struct {
	mirror  *mocks.MockKafMirror
	onFail  func(jobID string, reason string)
	stopped atomic.Int32
}

func setupFailureTest(t *testing.T) (*sqlx.DB, func() []*trackedMirror, string, func() error, func() error, func() error) {
	db, jm, _ := setupManagerTest(t)
	require.NoError(t, database.CreateCluster(db, &database.KafkaCluster{Name: "src", Brokers: "localhost:9092", SecurityConfig: "{}"}))
	require.NoError(t, database.CreateCluster(db, &database.KafkaCluster{Name: "tgt", Brokers: "localhost:9093", SecurityConfig: "{}"}))
	jobID := uuid.NewString()
	require.NoError(t, database.CreateJob(db, &database.ReplicationJob{ID: jobID, Name: "failure-job", SourceClusterName: "src", TargetClusterName: "tgt", Status: "paused"}))
	require.NoError(t, database.UpdateMappingsForJob(db, jobID, []database.TopicMapping{{JobID: jobID, SourceTopicPattern: "big", TargetTopicPattern: "big", Enabled: true}}))

	var mirrors []*trackedMirror
	jm.KafMirrorFactory = func(cfg *config.Config) (kafka.KafMirror, error) {
		tm := &trackedMirror{}
		tm.mirror = &mocks.MockKafMirror{
			StartFunc: func(jobID string, _ func(database.ReplicationMetric), onPanic func(string, string)) {
				tm.onFail = onPanic
			},
			StopFunc: func() { tm.stopped.Add(1) },
		}
		mirrors = append(mirrors, tm)
		return tm.mirror, nil
	}
	return db, func() []*trackedMirror { return mirrors }, jobID, func() error { return jm.StartJob(jobID) }, func() error { return jm.StopJob(jobID) }, func() error { return jm.RestartJob(jobID) }
}

func TestJobFailureStopsCurrentInstanceAndMarksFailed(t *testing.T) {
	db, mirrors, jobID, start, _, _ := setupFailureTest(t)
	require.NoError(t, start())

	mirrors()[0].onFail(jobID, "producer has failed 101 consecutive times")

	job, err := database.GetJob(db, jobID)
	require.NoError(t, err)
	assert.Equal(t, "failed", job.Status)
	require.NotNil(t, job.FailedReason)
	assert.Equal(t, "producer has failed 101 consecutive times", *job.FailedReason)
	assert.Equal(t, int32(1), mirrors()[0].stopped.Load())
	require.NoError(t, start())
	assert.Len(t, mirrors(), 2)
}

func TestJobFailureOfReplacedInstanceLeavesNewInstanceAlone(t *testing.T) {
	db, mirrors, jobID, start, stop, _ := setupFailureTest(t)
	require.NoError(t, start())
	require.NoError(t, stop())
	require.NoError(t, start())

	mirrors()[0].onFail(jobID, "topic discovery failed: old instance")

	job, err := database.GetJob(db, jobID)
	require.NoError(t, err)
	assert.Equal(t, "active", job.Status)
	assert.Nil(t, job.FailedReason)
	assert.Equal(t, int32(1), mirrors()[0].stopped.Load())
	assert.Equal(t, int32(0), mirrors()[1].stopped.Load())
	require.NoError(t, stop())
	assert.Equal(t, int32(1), mirrors()[1].stopped.Load())
}

func TestJobFailureAfterStopKeepsJobPaused(t *testing.T) {
	db, mirrors, jobID, start, stop, _ := setupFailureTest(t)
	require.NoError(t, start())
	require.NoError(t, stop())

	mirrors()[0].onFail(jobID, "consumer stopped: late failure")

	job, err := database.GetJob(db, jobID)
	require.NoError(t, err)
	assert.Equal(t, "paused", job.Status)
	assert.Nil(t, job.FailedReason)
	assert.Equal(t, int32(1), mirrors()[0].stopped.Load())
}

func TestStopAfterJobFailureKeepsFailedReason(t *testing.T) {
	db, mirrors, jobID, start, stop, _ := setupFailureTest(t)
	require.NoError(t, start())

	mirrors()[0].onFail(jobID, "topic discovery failed: source unreachable")
	assert.Error(t, stop())

	job, err := database.GetJob(db, jobID)
	require.NoError(t, err)
	assert.Equal(t, "failed", job.Status)
	require.NotNil(t, job.FailedReason)
	assert.Equal(t, "topic discovery failed: source unreachable", *job.FailedReason)
	assert.Equal(t, int32(1), mirrors()[0].stopped.Load())
}

func TestRestartOfFailedJobClearsFailedReason(t *testing.T) {
	db, mirrors, jobID, start, _, restart := setupFailureTest(t)
	require.NoError(t, start())
	mirrors()[0].onFail(jobID, "topic discovery failed: source unreachable")

	require.NoError(t, restart())

	job, err := database.GetJob(db, jobID)
	require.NoError(t, err)
	assert.Equal(t, "active", job.Status)
	assert.Nil(t, job.FailedReason)
	assert.Len(t, mirrors(), 2)
}
