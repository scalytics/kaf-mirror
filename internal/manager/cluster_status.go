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
	"context"
	"errors"
	"sync"
	"time"

	"kaf-mirror/internal/database"
	"kaf-mirror/internal/kafka"
	"kaf-mirror/pkg/logger"
)

const (
	ClusterStatusInterval    = time.Minute
	ClusterStatusTimeout     = 10 * time.Second
	clusterStatusConcurrency = 4
)

type ClusterProbe func(ctx context.Context, cluster database.KafkaCluster) error

type clusterProbeMeta struct {
	gen    uint64
	flight *clusterStatusFlight
}

type clusterStatusFlight struct {
	fp        string
	gen       uint64
	done      chan struct{}
	status    string
	err       error
	abandoned bool
}

func probeCluster(ctx context.Context, cluster database.KafkaCluster) error {
	return kafka.TestConnection(ctx, database.ClusterConfigFromRow(cluster))
}

func clusterProbeFingerprint(cluster database.KafkaCluster) string {
	return cluster.UID + ":" + kafka.ProbeFingerprint(database.ClusterConfigFromRow(cluster))
}

func (jm *JobManager) SetClusterProbeForTest(probe ClusterProbe) {
	jm.clusterProbe = probe
}

func (jm *JobManager) SetClusterProbeSemWaitForTest(hook func()) {
	jm.clusterProbeMu.Lock()
	defer jm.clusterProbeMu.Unlock()
	jm.clusterProbeSemWait = hook
}

func (jm *JobManager) SetClusterProbeJoinForTest(hook func()) {
	jm.clusterProbeMu.Lock()
	defer jm.clusterProbeMu.Unlock()
	jm.clusterProbeJoin = hook
}

func (jm *JobManager) clusterProbeJoinHook() func() {
	jm.clusterProbeMu.Lock()
	defer jm.clusterProbeMu.Unlock()
	return jm.clusterProbeJoin
}

func (jm *JobManager) probe(ctx context.Context, cluster database.KafkaCluster) error {
	if jm.clusterProbe != nil {
		return jm.clusterProbe(ctx, cluster)
	}
	return probeCluster(ctx, cluster)
}

func (jm *JobManager) recordClusterHealth(name string, probeErr error) {
	if !jm.beginDBOp() {
		return
	}
	defer jm.dbOpsWg.Done()
	if err := database.RecordClusterHealth(jm.Db, name, probeErr == nil); err != nil {
		logger.Error("Failed to store status of cluster %s: %v", name, err)
	}
}

func (jm *JobManager) recordClusterHealthIfCurrent(name string, gen uint64, probeErr error) {
	if !jm.clusterProbeGenCurrent(name, gen) {
		return
	}
	jm.recordClusterHealth(name, probeErr)
}

func (jm *JobManager) clusterProbeGenCurrent(name string, gen uint64) bool {
	jm.clusterProbeMu.Lock()
	defer jm.clusterProbeMu.Unlock()
	if jm.clusterProbes == nil {
		return false
	}
	meta, ok := jm.clusterProbes[name]
	return ok && meta.gen == gen
}

func (jm *JobManager) bumpClusterProbeGen(name string) uint64 {
	jm.clusterProbeMu.Lock()
	defer jm.clusterProbeMu.Unlock()
	if jm.clusterProbes == nil {
		jm.clusterProbes = make(map[string]*clusterProbeMeta)
	}
	meta := jm.clusterProbes[name]
	if meta == nil {
		meta = &clusterProbeMeta{}
		jm.clusterProbes[name] = meta
	}
	meta.gen++
	return meta.gen
}

func (jm *JobManager) acquireClusterProbeSlot(ctx context.Context) error {
	sem := jm.clusterProbeSem
	if sem == nil {
		return errors.New("cluster probe semaphore is not initialised")
	}
	select {
	case sem <- struct{}{}:
		return nil
	default:
	}
	jm.clusterProbeMu.Lock()
	hook := jm.clusterProbeSemWait
	jm.clusterProbeMu.Unlock()
	if hook != nil {
		hook()
	}
	select {
	case sem <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-jm.backgroundContext().Done():
		return jm.backgroundContext().Err()
	}
}

func (jm *JobManager) releaseClusterProbeSlot() {
	if jm.clusterProbeSem == nil {
		return
	}
	<-jm.clusterProbeSem
}

func (jm *JobManager) joinOrStartClusterFlight(name, fp string) (*clusterStatusFlight, bool, uint64) {
	jm.clusterProbeMu.Lock()
	defer jm.clusterProbeMu.Unlock()
	if jm.clusterProbes == nil {
		jm.clusterProbes = make(map[string]*clusterProbeMeta)
	}
	meta := jm.clusterProbes[name]
	if meta == nil {
		meta = &clusterProbeMeta{}
		jm.clusterProbes[name] = meta
	}
	if meta.flight != nil && meta.flight.fp == fp {
		return meta.flight, true, meta.flight.gen
	}
	meta.gen++
	flight := &clusterStatusFlight{fp: fp, gen: meta.gen, done: make(chan struct{})}
	meta.flight = flight
	return flight, false, flight.gen
}

func (jm *JobManager) finishClusterFlight(name string, flight *clusterStatusFlight, status string, err error, abandoned bool) {
	flight.status = status
	flight.err = err
	flight.abandoned = abandoned
	close(flight.done)
	jm.clusterProbeMu.Lock()
	defer jm.clusterProbeMu.Unlock()
	if meta := jm.clusterProbes[name]; meta != nil && meta.flight == flight {
		meta.flight = nil
	}
}

func (jm *JobManager) statusAfterProbe(name string, probeErr error) (string, error) {
	current, err := database.GetCluster(jm.Db, name)
	if err != nil {
		if probeErr != nil {
			return database.ClusterStatusUnreachable, probeErr
		}
		return database.ClusterStatusActive, nil
	}
	if database.ClusterStatusIsRetired(current.Status) {
		return current.Status, nil
	}
	return current.Status, probeErr
}

func (jm *JobManager) CheckClusterStatus(ctx context.Context, name string) (string, error) {
	for {
		cluster, err := database.GetCluster(jm.Db, name)
		if err != nil {
			return "", err
		}
		if database.ClusterStatusIsRetired(cluster.Status) {
			return cluster.Status, nil
		}
		flight, joined, gen := jm.joinOrStartClusterFlight(name, clusterProbeFingerprint(*cluster))
		if joined {
			if hook := jm.clusterProbeJoinHook(); hook != nil {
				hook()
			}
			select {
			case <-flight.done:
				if flight.abandoned {
					continue
				}
				return flight.status, flight.err
			case <-ctx.Done():
				return cluster.Status, ctx.Err()
			}
		}
		status, err, retry := jm.runClusterStatusFlight(ctx, name, *cluster, flight, gen)
		if retry {
			continue
		}
		return status, err
	}
}

func (jm *JobManager) runClusterStatusFlight(ctx context.Context, name string, cluster database.KafkaCluster, flight *clusterStatusFlight, gen uint64) (string, error, bool) {
	if err := jm.acquireClusterProbeSlot(ctx); err != nil {
		jm.finishClusterFlight(name, flight, cluster.Status, err, true)
		return cluster.Status, err, false
	}
	defer jm.releaseClusterProbeSlot()

	current, err := database.GetCluster(jm.Db, name)
	if err != nil {
		jm.finishClusterFlight(name, flight, "", err, false)
		return "", err, false
	}
	if database.ClusterStatusIsRetired(current.Status) {
		jm.finishClusterFlight(name, flight, current.Status, nil, false)
		return current.Status, nil, false
	}
	if clusterProbeFingerprint(*current) != flight.fp {
		jm.finishClusterFlight(name, flight, current.Status, nil, true)
		return "", nil, true
	}

	probeCtx, cancel := context.WithTimeout(ctx, ClusterStatusTimeout)
	defer cancel()
	probeErr := jm.probe(probeCtx, *current)
	if errors.Is(probeCtx.Err(), context.Canceled) && probeErr != nil {
		jm.finishClusterFlight(name, flight, current.Status, probeErr, false)
		return current.Status, probeErr, false
	}
	jm.recordClusterHealthIfCurrent(name, gen, probeErr)
	status, err := jm.statusAfterProbe(name, probeErr)
	jm.finishClusterFlight(name, flight, status, err, false)
	return status, err, false
}

func (jm *JobManager) CheckAllClusterStatuses(ctx context.Context) {
	clusters, err := database.ListClusters(jm.Db)
	if err != nil {
		logger.Error("Failed to list clusters for status checks: %v", err)
		return
	}
	var wg sync.WaitGroup
	for _, cluster := range clusters {
		if database.ClusterStatusIsRetired(cluster.Status) {
			continue
		}
		select {
		case <-ctx.Done():
			wg.Wait()
			return
		default:
		}
		wg.Add(1)
		go func(name, previous string) {
			defer wg.Done()
			status, err := jm.CheckClusterStatus(ctx, name)
			if status != previous && status != "" {
				if err != nil {
					logger.Warn("Cluster %s is %s: %v", name, status, err)
				} else {
					logger.Info("Cluster %s is %s", name, status)
				}
			}
		}(cluster.Name, cluster.Status)
	}
	wg.Wait()
}

func (jm *JobManager) CheckClusterStatusAsync(name string) {
	if !jm.beginDBOp() {
		return
	}
	go func() {
		defer jm.dbOpsWg.Done()
		if _, err := jm.CheckClusterStatus(jm.backgroundContext(), name); err != nil {
			logger.Warn("Cluster %s status check failed: %v", name, err)
		}
	}()
}

func (jm *JobManager) backgroundContext() context.Context {
	if jm.bgCtx != nil {
		return jm.bgCtx
	}
	return context.Background()
}

func (jm *JobManager) startClusterStatusChecks() {
	defer jm.wg.Done()
	ticker := time.NewTicker(ClusterStatusInterval)
	defer ticker.Stop()
	jm.CheckAllClusterStatuses(jm.backgroundContext())
	for {
		select {
		case <-ticker.C:
			jm.CheckAllClusterStatuses(jm.backgroundContext())
		case <-jm.close:
			return
		case <-jm.backgroundContext().Done():
			return
		}
	}
}
