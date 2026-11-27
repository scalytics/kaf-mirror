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

package server_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/scalytics/kaf-mirror/v2/internal/database"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kfake"
)

func listedClusterStatus(t *testing.T, ctx *TestContext, name string) string {
	t.Helper()
	req := httptest.NewRequest("GET", "/api/v1/clusters/", nil)
	addAuthHeader(req, ctx.Token)
	resp, err := ctx.Server.App.Test(req)
	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode)
	var clusters []database.KafkaCluster
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&clusters))
	for _, c := range clusters {
		if c.Name == name {
			return c.Status
		}
	}
	t.Fatalf("cluster %s not listed", name)
	return ""
}

func getClusterStatus(t *testing.T, ctx *TestContext, name string) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest("GET", "/api/v1/clusters/"+name+"/status", nil)
	addAuthHeader(req, ctx.Token)
	resp, err := ctx.Server.App.Test(req, 15000)
	require.NoError(t, err)
	var body map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&body)
	return resp.StatusCode, body
}

func TestClusterStatusEndpointStoresReachableStatusForTheList(t *testing.T) {
	ctx := setupTestServer(t)
	c, err := kfake.NewCluster(kfake.NumBrokers(1))
	require.NoError(t, err)
	defer c.Close()
	require.NoError(t, database.CreateCluster(ctx.Server.Db, &database.KafkaCluster{Name: "live", Brokers: strings.Join(c.ListenAddrs(), ","), SecurityConfig: "{}"}))
	assert.Equal(t, "unknown", listedClusterStatus(t, ctx, "live"))

	code, body := getClusterStatus(t, ctx, "live")
	require.Equal(t, 200, code)
	assert.Equal(t, "active", body["status"])
	assert.Equal(t, "Cluster is reachable", body["message"])
	assert.Equal(t, "active", listedClusterStatus(t, ctx, "live"))
}

func TestClusterStatusEndpointMarksUnreachableAndRecovers(t *testing.T) {
	ctx := setupTestServer(t)
	require.NoError(t, database.CreateCluster(ctx.Server.Db, &database.KafkaCluster{Name: "flaky", Brokers: "localhost:1", SecurityConfig: "{}"}))
	down := true
	ctx.Manager.SetClusterProbeForTest(func(_ context.Context, _ database.KafkaCluster) error {
		if down {
			return errors.New("connection refused")
		}
		return nil
	})

	code, _ := getClusterStatus(t, ctx, "flaky")
	assert.Equal(t, 503, code)
	assert.Equal(t, "unreachable", listedClusterStatus(t, ctx, "flaky"))

	down = false
	code, body := getClusterStatus(t, ctx, "flaky")
	assert.Equal(t, 200, code)
	assert.Equal(t, "active", body["status"])
	assert.Equal(t, "active", listedClusterStatus(t, ctx, "flaky"))
}

func TestClusterStatusEndpointKeepsInactiveClusterInactive(t *testing.T) {
	ctx := setupTestServer(t)
	require.NoError(t, database.CreateCluster(ctx.Server.Db, &database.KafkaCluster{Name: "old", Brokers: "localhost:1", SecurityConfig: "{}"}))
	require.NoError(t, database.SetClusterStatus(ctx.Server.Db, "old", "inactive"))
	ctx.Manager.SetClusterProbeForTest(func(_ context.Context, _ database.KafkaCluster) error { return nil })

	code, body := getClusterStatus(t, ctx, "old")
	assert.Equal(t, 200, code)
	assert.Equal(t, "inactive", body["status"])
	assert.Equal(t, "Cluster status is inactive", body["message"])
	assert.Equal(t, "inactive", listedClusterStatus(t, ctx, "old"))
}

func TestClusterStatusEndpointKeepsInactiveWhenProbeWouldFail(t *testing.T) {
	ctx := setupTestServer(t)
	require.NoError(t, database.CreateCluster(ctx.Server.Db, &database.KafkaCluster{Name: "old", Brokers: "localhost:1", SecurityConfig: "{}"}))
	require.NoError(t, database.SetClusterStatus(ctx.Server.Db, "old", "inactive"))
	var probed int
	ctx.Manager.SetClusterProbeForTest(func(_ context.Context, _ database.KafkaCluster) error {
		probed++
		return errors.New("connection refused")
	})

	code, body := getClusterStatus(t, ctx, "old")
	assert.Equal(t, 200, code)
	assert.Equal(t, "inactive", body["status"])
	assert.Equal(t, "Cluster status is inactive", body["message"])
	assert.Equal(t, "inactive", listedClusterStatus(t, ctx, "old"))
	assert.Equal(t, 0, probed)
}

func TestClusterStatusEndpointRespectsGlobalProbeCap(t *testing.T) {
	ctx := setupTestServer(t)
	for i := 0; i < 6; i++ {
		name := "c" + string(rune('a'+i))
		require.NoError(t, database.CreateCluster(ctx.Server.Db, &database.KafkaCluster{Name: name, Brokers: "localhost:1", SecurityConfig: "{}"}))
	}

	var current, max, started int32
	release := make(chan struct{})
	entered := make(chan struct{}, 6)
	ctx.Manager.SetClusterProbeForTest(func(context.Context, database.KafkaCluster) error {
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

	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		name := "c" + string(rune('a'+i))
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			code, body := getClusterStatus(t, ctx, name)
			assert.Equal(t, 200, code)
			assert.Equal(t, "active", body["status"])
		}(name)
	}
	for i := 0; i < 4; i++ {
		<-entered
	}
	assert.Equal(t, int32(4), atomic.LoadInt32(&current))
	assert.Equal(t, int32(4), atomic.LoadInt32(&max))
	close(release)
	wg.Wait()
	assert.Equal(t, int32(4), atomic.LoadInt32(&max))
	assert.Equal(t, int32(6), atomic.LoadInt32(&started))
}

func TestClusterStatusEndpointSharesAnInFlightProbe(t *testing.T) {
	ctx := setupTestServer(t)
	require.NoError(t, database.CreateCluster(ctx.Server.Db, &database.KafkaCluster{Name: "shared", Brokers: "localhost:1", SecurityConfig: "{}"}))

	var started int32
	release := make(chan struct{})
	entered := make(chan struct{})
	joined := make(chan struct{}, 4)
	ctx.Manager.SetClusterProbeJoinForTest(func() {
		joined <- struct{}{}
	})
	ctx.Manager.SetClusterProbeForTest(func(context.Context, database.KafkaCluster) error {
		if atomic.AddInt32(&started, 1) == 1 {
			close(entered)
		}
		<-release
		return nil
	})

	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			code, body := getClusterStatus(t, ctx, "shared")
			assert.Equal(t, 200, code)
			assert.Equal(t, "active", body["status"])
		}()
	}
	<-entered
	for i := 0; i < 4; i++ {
		<-joined
	}
	close(release)
	wg.Wait()
	assert.Equal(t, int32(1), atomic.LoadInt32(&started))
}
