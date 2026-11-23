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
