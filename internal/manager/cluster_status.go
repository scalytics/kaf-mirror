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

func probeCluster(ctx context.Context, cluster database.KafkaCluster) error {
	return kafka.TestConnection(ctx, database.ClusterConfigFromRow(cluster))
}

func (jm *JobManager) SetClusterProbeForTest(probe ClusterProbe) {
	jm.clusterProbe = probe
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

func (jm *JobManager) CheckClusterStatus(ctx context.Context, name string) (string, error) {
	cluster, err := database.GetCluster(jm.Db, name)
	if err != nil {
		return "", err
	}
	if database.ClusterStatusIsRetired(cluster.Status) {
		return cluster.Status, nil
	}
	ctx, cancel := context.WithTimeout(ctx, ClusterStatusTimeout)
	defer cancel()
	probeErr := jm.probe(ctx, *cluster)
	if errors.Is(ctx.Err(), context.Canceled) && probeErr != nil {
		return cluster.Status, probeErr
	}
	jm.recordClusterHealth(name, probeErr)
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

func (jm *JobManager) CheckAllClusterStatuses(ctx context.Context) {
	clusters, err := database.ListClusters(jm.Db)
	if err != nil {
		logger.Error("Failed to list clusters for status checks: %v", err)
		return
	}
	sem := make(chan struct{}, clusterStatusConcurrency)
	var wg sync.WaitGroup
	for _, cluster := range clusters {
		if database.ClusterStatusIsRetired(cluster.Status) {
			continue
		}
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			wg.Wait()
			return
		}
		wg.Add(1)
		go func(name, previous string) {
			defer wg.Done()
			defer func() { <-sem }()
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
