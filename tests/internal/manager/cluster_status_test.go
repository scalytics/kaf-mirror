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
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"kaf-mirror/internal/config"
	"kaf-mirror/internal/database"
	"kaf-mirror/internal/manager"
	"kaf-mirror/tests/mocks"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type switchableProbe struct {
	mu        sync.Mutex
	down      map[string]bool
	deadlines []time.Duration
}

func (p *switchableProbe) set(name string, down bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.down[name] = down
}

func (p *switchableProbe) probe(ctx context.Context, cluster database.KafkaCluster) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if deadline, ok := ctx.Deadline(); ok {
		p.deadlines = append(p.deadlines, time.Until(deadline))
	} else {
		p.deadlines = append(p.deadlines, -1)
	}
	if p.down[cluster.Name] {
		return errors.New("dial tcp: connection refused")
	}
	return nil
}

func clusterStatus(t *testing.T, jm *manager.JobManager, name string) string {
	t.Helper()
	c, err := database.GetCluster(jm.Db, name)
	require.NoError(t, err)
	return c.Status
}

func TestClusterStatusIsStoredAndFollowsReachability(t *testing.T) {
	db, err := database.InitDB(":memory:")
	require.NoError(t, err)
	jm := manager.New(db, &config.Config{Server: config.ServerConfig{Mode: "test"}}, &mocks.MockHub{})
	t.Cleanup(func() {
		jm.Close()
		db.Close()
	})

	for _, name := range []string{"src", "tgt", "retired"} {
		require.NoError(t, database.CreateCluster(db, &database.KafkaCluster{Name: name, Brokers: "localhost:1", SecurityConfig: "{}"}))
	}
	require.NoError(t, database.SetClusterStatus(db, "retired", database.ClusterStatusInactive))
	assert.Equal(t, database.ClusterStatusUnknown, clusterStatus(t, jm, "src"))

	p := &switchableProbe{down: map[string]bool{}}
	jm.SetClusterProbeForTest(p.probe)

	jm.CheckAllClusterStatuses(context.Background())
	assert.Equal(t, database.ClusterStatusActive, clusterStatus(t, jm, "src"))
	assert.Equal(t, database.ClusterStatusActive, clusterStatus(t, jm, "tgt"))
	assert.Equal(t, database.ClusterStatusInactive, clusterStatus(t, jm, "retired"))

	p.set("tgt", true)
	jm.CheckAllClusterStatuses(context.Background())
	assert.Equal(t, database.ClusterStatusActive, clusterStatus(t, jm, "src"))
	assert.Equal(t, database.ClusterStatusUnreachable, clusterStatus(t, jm, "tgt"))

	p.set("tgt", false)
	jm.CheckAllClusterStatuses(context.Background())
	assert.Equal(t, database.ClusterStatusActive, clusterStatus(t, jm, "tgt"))
	assert.Equal(t, database.ClusterStatusInactive, clusterStatus(t, jm, "retired"))

	p.mu.Lock()
	defer p.mu.Unlock()
	require.Len(t, p.deadlines, 6)
	for _, d := range p.deadlines {
		assert.Greater(t, d, time.Duration(0))
		assert.LessOrEqual(t, d, manager.ClusterStatusTimeout)
	}
}

func TestClusterStatusCheckIsBoundedByTimeout(t *testing.T) {
	db, err := database.InitDB(":memory:")
	require.NoError(t, err)
	jm := manager.New(db, &config.Config{Server: config.ServerConfig{Mode: "test"}}, &mocks.MockHub{})
	t.Cleanup(func() {
		jm.Close()
		db.Close()
	})
	require.NoError(t, database.CreateCluster(db, &database.KafkaCluster{Name: "slow", Brokers: "localhost:1", SecurityConfig: "{}"}))
	jm.SetClusterProbeForTest(func(ctx context.Context, cluster database.KafkaCluster) error {
		<-ctx.Done()
		return ctx.Err()
	})

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	status, err := jm.CheckClusterStatus(ctx, "slow")
	require.Error(t, err)
	assert.Equal(t, database.ClusterStatusUnreachable, status)
	assert.Equal(t, database.ClusterStatusUnreachable, clusterStatus(t, jm, "slow"))
}

func TestClusterStatusCheckKeepsStatusWhenCancelled(t *testing.T) {
	db, err := database.InitDB(":memory:")
	require.NoError(t, err)
	jm := manager.New(db, &config.Config{Server: config.ServerConfig{Mode: "test"}}, &mocks.MockHub{})
	t.Cleanup(func() {
		jm.Close()
		db.Close()
	})
	require.NoError(t, database.CreateCluster(db, &database.KafkaCluster{Name: "c", Brokers: "localhost:1", SecurityConfig: "{}"}))
	require.NoError(t, database.RecordClusterHealth(db, "c", true))
	jm.SetClusterProbeForTest(func(ctx context.Context, cluster database.KafkaCluster) error {
		<-ctx.Done()
		return ctx.Err()
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = jm.CheckClusterStatus(ctx, "c")
	require.Error(t, err)
	assert.Equal(t, database.ClusterStatusActive, clusterStatus(t, jm, "c"))
}

func TestFailedCheckDoesNotLeaveRestoredClusterUnknown(t *testing.T) {
	db, err := database.InitDB(":memory:")
	require.NoError(t, err)
	jm := manager.New(db, &config.Config{Server: config.ServerConfig{Mode: "test"}}, &mocks.MockHub{})
	t.Cleanup(func() {
		jm.Close()
		db.Close()
	})
	require.NoError(t, database.CreateCluster(db, &database.KafkaCluster{Name: "restored", Brokers: "localhost:1", SecurityConfig: "{}"}))
	assert.Equal(t, database.ClusterStatusUnknown, clusterStatus(t, jm, "restored"))
	jm.SetClusterProbeForTest(func(context.Context, database.KafkaCluster) error {
		return errors.New("dial tcp: connection refused")
	})
	status, err := jm.CheckClusterStatus(context.Background(), "restored")
	require.Error(t, err)
	assert.Equal(t, database.ClusterStatusUnreachable, status)
	assert.Equal(t, database.ClusterStatusUnreachable, clusterStatus(t, jm, "restored"))
}

func TestClusterStatusCheckKeepsRetiredStatusIfStateChangesDuringProbe(t *testing.T) {
	db, err := database.InitDB(":memory:")
	require.NoError(t, err)
	jm := manager.New(db, &config.Config{Server: config.ServerConfig{Mode: "test"}}, &mocks.MockHub{})
	t.Cleanup(func() {
		jm.Close()
		db.Close()
	})
	require.NoError(t, database.CreateCluster(db, &database.KafkaCluster{Name: "c", Brokers: "localhost:1", SecurityConfig: "{}"}))
	require.NoError(t, database.RecordClusterHealth(db, "c", true))

	started := make(chan struct{})
	release := make(chan struct{})
	jm.SetClusterProbeForTest(func(context.Context, database.KafkaCluster) error {
		close(started)
		<-release
		return nil
	})

	done := make(chan struct{})
	var status string
	var checkErr error
	go func() {
		defer close(done)
		status, checkErr = jm.CheckClusterStatus(context.Background(), "c")
	}()
	<-started
	require.NoError(t, database.SetClusterStatus(db, "c", database.ClusterStatusInactive))
	close(release)
	<-done
	require.NoError(t, checkErr)
	assert.Equal(t, database.ClusterStatusInactive, status)
	assert.Equal(t, database.ClusterStatusInactive, clusterStatus(t, jm, "c"))
}

func TestClusterStatusCheckDoesNotRecreateDeletedCluster(t *testing.T) {
	db, err := database.InitDB(":memory:")
	require.NoError(t, err)
	jm := manager.New(db, &config.Config{Server: config.ServerConfig{Mode: "test"}}, &mocks.MockHub{})
	t.Cleanup(func() {
		jm.Close()
		db.Close()
	})
	require.NoError(t, database.CreateCluster(db, &database.KafkaCluster{Name: "gone", Brokers: "localhost:1", SecurityConfig: "{}"}))

	started := make(chan struct{})
	release := make(chan struct{})
	jm.SetClusterProbeForTest(func(context.Context, database.KafkaCluster) error {
		close(started)
		<-release
		return nil
	})

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = jm.CheckClusterStatus(context.Background(), "gone")
	}()
	<-started
	require.NoError(t, database.DeleteCluster(db, "gone"))
	close(release)
	<-done
	_, err = database.GetCluster(db, "gone")
	require.Error(t, err)
}

func TestCheckAllClusterStatusesCapsConcurrencyAtFour(t *testing.T) {
	db, err := database.InitDB(":memory:")
	require.NoError(t, err)
	jm := manager.New(db, &config.Config{Server: config.ServerConfig{Mode: "test"}}, &mocks.MockHub{})
	t.Cleanup(func() {
		jm.Close()
		db.Close()
	})
	for i := 0; i < 5; i++ {
		name := "c" + string(rune('a'+i))
		require.NoError(t, database.CreateCluster(db, &database.KafkaCluster{Name: name, Brokers: "localhost:1", SecurityConfig: "{}"}))
	}

	var current, max, started int32
	release := make(chan struct{})
	entered := make(chan struct{}, 5)
	jm.SetClusterProbeForTest(func(context.Context, database.KafkaCluster) error {
		n := atomic.AddInt32(&current, 1)
		atomic.AddInt32(&started, 1)
		for {
			old := atomic.LoadInt32(&max)
			if n <= old || atomic.CompareAndSwapInt32(&max, old, n) {
				break
			}
		}
		entered <- struct{}{}
		<-release
		atomic.AddInt32(&current, -1)
		return nil
	})

	done := make(chan struct{})
	go func() {
		defer close(done)
		jm.CheckAllClusterStatuses(context.Background())
	}()
	for i := 0; i < 4; i++ {
		<-entered
	}
	assert.Equal(t, int32(4), atomic.LoadInt32(&current))
	assert.Equal(t, int32(4), atomic.LoadInt32(&max))
	assert.Equal(t, int32(4), atomic.LoadInt32(&started))
	close(release)
	<-done
	assert.Equal(t, int32(4), atomic.LoadInt32(&max))
	assert.Equal(t, int32(5), atomic.LoadInt32(&started))
}

func TestCloseDoesNotWriteClusterStatusAfterShutdown(t *testing.T) {
	db, err := database.InitDB(":memory:")
	require.NoError(t, err)
	jm := manager.New(db, &config.Config{Server: config.ServerConfig{Mode: "test"}}, &mocks.MockHub{})
	require.NoError(t, database.CreateCluster(db, &database.KafkaCluster{Name: "c", Brokers: "localhost:1", SecurityConfig: "{}"}))
	require.NoError(t, database.RecordClusterHealth(db, "c", true))

	started := make(chan struct{})
	jm.SetClusterProbeForTest(func(ctx context.Context, _ database.KafkaCluster) error {
		close(started)
		<-ctx.Done()
		return ctx.Err()
	})
	jm.CheckClusterStatusAsync("c")
	<-started
	jm.Close()
	assert.Equal(t, database.ClusterStatusActive, clusterStatus(t, jm, "c"))

	jm.SetClusterProbeForTest(func(context.Context, database.KafkaCluster) error {
		return errors.New("dial tcp: connection refused")
	})
	status, err := jm.CheckClusterStatus(context.Background(), "c")
	require.Error(t, err)
	assert.Equal(t, database.ClusterStatusActive, status)
	assert.Equal(t, database.ClusterStatusActive, clusterStatus(t, jm, "c"))
	db.Close()
}

func TestClusterStatusLoopDoesNotStartInTestMode(t *testing.T) {
	db, err := database.InitDB(":memory:")
	require.NoError(t, err)
	var probes int32
	jm := manager.New(db, &config.Config{Server: config.ServerConfig{Mode: "test"}}, &mocks.MockHub{})
	t.Cleanup(func() {
		jm.Close()
		db.Close()
	})
	jm.SetClusterProbeForTest(func(context.Context, database.KafkaCluster) error {
		atomic.AddInt32(&probes, 1)
		return nil
	})
	require.NoError(t, database.CreateCluster(db, &database.KafkaCluster{Name: "c", Brokers: "localhost:1", SecurityConfig: "{}"}))
	assert.Equal(t, database.ClusterStatusUnknown, clusterStatus(t, jm, "c"))
	assert.Equal(t, int32(0), atomic.LoadInt32(&probes))
}

func countingBlockedProbe(current, max, started *int32, entered chan struct{}, release <-chan struct{}) manager.ClusterProbe {
	return func(context.Context, database.KafkaCluster) error {
		n := atomic.AddInt32(current, 1)
		atomic.AddInt32(started, 1)
		for {
			old := atomic.LoadInt32(max)
			if n <= old || atomic.CompareAndSwapInt32(max, old, n) {
				break
			}
		}
		entered <- struct{}{}
		<-release
		atomic.AddInt32(current, -1)
		return nil
	}
}

func TestClusterProbeCapHoldsAcrossEveryEntryPoint(t *testing.T) {
	db, err := database.InitDB(":memory:")
	require.NoError(t, err)
	jm := manager.New(db, &config.Config{Server: config.ServerConfig{Mode: "test"}}, &mocks.MockHub{})
	t.Cleanup(func() {
		jm.Close()
		db.Close()
	})
	names := []string{"ca", "cb", "cc", "cd", "ce", "cf"}
	for _, name := range names {
		require.NoError(t, database.CreateCluster(db, &database.KafkaCluster{Name: name, Brokers: "localhost:1", SecurityConfig: "{}"}))
	}

	var current, max, started int32
	release := make(chan struct{})
	entered := make(chan struct{}, 6)
	jm.SetClusterProbeForTest(countingBlockedProbe(&current, &max, &started, entered, release))

	var wg sync.WaitGroup
	for _, name := range names[:3] {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			_, _ = jm.CheckClusterStatus(context.Background(), name)
		}(name)
	}
	for _, name := range names[3:] {
		jm.CheckClusterStatusAsync(name)
	}
	for i := 0; i < 4; i++ {
		<-entered
	}
	assert.Equal(t, int32(4), atomic.LoadInt32(&current))
	assert.Equal(t, int32(4), atomic.LoadInt32(&max))
	close(release)
	for i := 0; i < 2; i++ {
		<-entered
	}
	wg.Wait()
	assert.Equal(t, int32(4), atomic.LoadInt32(&max))
	assert.Equal(t, int32(6), atomic.LoadInt32(&started))
}

func TestStaleClusterProbeResultIsDropped(t *testing.T) {
	db, err := database.InitDB(":memory:")
	require.NoError(t, err)
	jm := manager.New(db, &config.Config{Server: config.ServerConfig{Mode: "test"}}, &mocks.MockHub{})
	t.Cleanup(func() {
		jm.Close()
		db.Close()
	})
	require.NoError(t, database.CreateCluster(db, &database.KafkaCluster{Name: "c", Brokers: "old:1", SecurityConfig: "{}"}))
	require.NoError(t, database.RecordClusterHealth(db, "c", true))

	oldStarted := make(chan struct{})
	oldRelease := make(chan struct{})
	newStarted := make(chan struct{})
	jm.SetClusterProbeForTest(func(_ context.Context, cluster database.KafkaCluster) error {
		if cluster.Brokers == "old:1" {
			close(oldStarted)
			<-oldRelease
			return errors.New("old brokers unreachable")
		}
		close(newStarted)
		return nil
	})

	oldDone := make(chan struct{})
	var oldStatus string
	var oldErr error
	go func() {
		defer close(oldDone)
		oldStatus, oldErr = jm.CheckClusterStatus(context.Background(), "c")
	}()
	<-oldStarted

	row, err := database.GetCluster(db, "c")
	require.NoError(t, err)
	row.Brokers = "new:2"
	require.NoError(t, database.UpdateCluster(db, row))

	newDone := make(chan struct{})
	var newStatus string
	var newErr error
	go func() {
		defer close(newDone)
		newStatus, newErr = jm.CheckClusterStatus(context.Background(), "c")
	}()
	<-newStarted
	close(oldRelease)
	<-oldDone
	<-newDone

	require.NoError(t, newErr)
	assert.Equal(t, database.ClusterStatusActive, newStatus)
	assert.Equal(t, database.ClusterStatusActive, clusterStatus(t, jm, "c"))
	if oldErr == nil {
		assert.Equal(t, database.ClusterStatusActive, oldStatus)
	}
}

func TestClusterStatusCheckCanBeCancelledWhileWaitingForSlot(t *testing.T) {
	db, err := database.InitDB(":memory:")
	require.NoError(t, err)
	jm := manager.New(db, &config.Config{Server: config.ServerConfig{Mode: "test"}}, &mocks.MockHub{})
	t.Cleanup(func() {
		jm.Close()
		db.Close()
	})
	for i := 0; i < 5; i++ {
		name := "c" + string(rune('a'+i))
		require.NoError(t, database.CreateCluster(db, &database.KafkaCluster{Name: name, Brokers: "localhost:1", SecurityConfig: "{}"}))
	}
	require.NoError(t, database.RecordClusterHealth(db, "ce", true))

	var current, max, started int32
	release := make(chan struct{})
	entered := make(chan struct{}, 5)
	jm.SetClusterProbeForTest(countingBlockedProbe(&current, &max, &started, entered, release))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var waitingOnce sync.Once
	waiting := make(chan struct{})
	jm.SetClusterProbeSemWaitForTest(func() {
		waitingOnce.Do(func() { close(waiting) })
	})

	var holders sync.WaitGroup
	for i := 0; i < 4; i++ {
		name := "c" + string(rune('a'+i))
		holders.Add(1)
		go func(name string) {
			defer holders.Done()
			_, _ = jm.CheckClusterStatus(context.Background(), name)
		}(name)
	}
	for i := 0; i < 4; i++ {
		<-entered
	}

	done := make(chan struct{})
	var status string
	var checkErr error
	go func() {
		defer close(done)
		status, checkErr = jm.CheckClusterStatus(ctx, "ce")
	}()
	<-waiting
	cancel()
	<-done
	require.Error(t, checkErr)
	assert.ErrorIs(t, checkErr, context.Canceled)
	assert.Equal(t, database.ClusterStatusActive, status)
	assert.Equal(t, database.ClusterStatusActive, clusterStatus(t, jm, "ce"))
	assert.Equal(t, int32(4), atomic.LoadInt32(&started))

	close(release)
	holders.Wait()
}
