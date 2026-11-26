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
	"sync/atomic"
	"testing"
	"time"

	"kaf-mirror/internal/config"
	"kaf-mirror/internal/database"
	"kaf-mirror/internal/kafka"
	"kaf-mirror/internal/protection"
	"kaf-mirror/tests/mocks"

	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kgo"
)

func TestPayloadAttackIsBlockedBeforeProduceAndPausesTheJob(t *testing.T) {
	for _, failPersistence := range []bool{false, true} {
		name := "persistent halt"
		if failPersistence {
			name = "halt storage failure"
		}
		t.Run(name, func(t *testing.T) { testPayloadAttack(t, failPersistence) })
	}
}

func testPayloadAttack(t *testing.T, failPersistence bool) {
	db, jm, _ := setupManagerTest(t)
	jm.Protection = protection.New(db, config.ProtectionConfig{AutoHalt: config.AutoHaltConfig{Enabled: true}})
	require.NoError(t, jm.Protection.SetEnabled(true))
	for _, name := range []string{"src", "dst"} {
		require.NoError(t, database.CreateCluster(db, &database.KafkaCluster{Name: name, Brokers: "localhost:9092", SecurityConfig: "{}", Role: "dr"}))
	}
	const jobID = "payload-attack"
	require.NoError(t, database.CreateJob(db, &database.ReplicationJob{ID: jobID, Name: jobID, SourceClusterName: "src", TargetClusterName: "dst", Status: "paused"}))
	require.NoError(t, database.UpdateMappingsForJob(db, jobID, []database.TopicMapping{{JobID: jobID, SourceTopicPattern: "events", TargetTopicPattern: "events", Enabled: true}}))
	var writes atomic.Int32
	km := kafka.NewKafMirrorImplForTest(&kafka.Producer{Client: &mocks.MockKgoClient{ProduceFunc: func(_ context.Context, r *kgo.Record, done func(*kgo.Record, error)) {
		writes.Add(1)
		done(r, nil)
	}}}, map[string]string{"events": "events"})
	km.Consumer = kafka.NewConsumerForTest(nil, nil)
	km.Consumer.Client = &mocks.MockKgoClient{PollFetchesFunc: func(ctx context.Context) kgo.Fetches {
		<-ctx.Done()
		return nil
	}}
	jm.KafMirrorFactory = func(*config.Config) (kafka.KafMirror, error) { return km, nil }
	require.NoError(t, jm.StartJob(jobID))
	if failPersistence {
		_, err := db.Exec(`CREATE TRIGGER reject_halt BEFORE UPDATE ON protection_state BEGIN SELECT RAISE(FAIL, 'storage failure'); END`)
		require.NoError(t, err)
	}
	plain := []byte(`{"customer":"alice","status":"active","city":"berlin"}`)
	for i := 0; i < 25; i++ {
		require.NoError(t, km.HandleRecordForTest(&kgo.Record{Topic: "events", Key: []byte{byte(i % 20)}, Value: plain}))
	}
	enc := make([]byte, 256)
	for i := range enc {
		enc[i] = byte(i)
	}
	detected := false
	for i := 0; i < 60; i++ {
		before := writes.Load()
		err := km.HandleRecordForTest(&kgo.Record{Topic: "events", Key: []byte{byte(i % 20)}, Value: enc})
		if err != nil {
			require.ErrorContains(t, err, "replication halted")
			require.Equal(t, before, writes.Load(), "the triggering record must not reach the producer")
			detected = true
			break
		}
	}
	require.True(t, detected)
	require.True(t, jm.Protection.Halted())
	before := writes.Load()
	require.Error(t, km.HandleRecordForTest(&kgo.Record{Topic: "events", Value: plain}))
	require.Equal(t, before, writes.Load())
	require.Eventually(t, func() bool {
		job, err := database.GetJob(db, jobID)
		return err == nil && job.Status == "paused" && job.FailedReason != nil
	}, 5*time.Second, time.Millisecond)
	require.ErrorContains(t, jm.StartJob(jobID), "replication halted")
}
