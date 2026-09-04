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
	"encoding/json"
	"fmt"
	"kaf-mirror/internal/ai"
	"kaf-mirror/internal/config"
	"kaf-mirror/internal/database"
	"kaf-mirror/internal/kafka"
	"kaf-mirror/internal/metrics"
	"kaf-mirror/pkg/logger"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jmoiron/sqlx"
)

// KafMirrorFactory is an interface for creating new kaf-mirrors.
// This allows us to mock the creation of kaf-mirrors in tests.
type KafMirrorFactory func(cfg *config.Config) (kafka.KafMirror, error)

// Hub interface to avoid circular dependency
type Hub interface {
	BroadcastJSON(v interface{})
}

// JobManager manages the lifecycle of replication jobs.
type JobManager struct {
	Db                   *sqlx.DB
	Config               *config.Config
	KafMirrors           map[string]kafka.KafMirror
	Hub                  Hub
	Mu                   sync.Mutex
	KafMirrorFactory     KafMirrorFactory
	metricsSink          metrics.Sink
	AIClient             *ai.Client
	close                chan struct{}
	aiAnalysisTicker     *time.Ticker
	wg                   sync.WaitGroup
	dbOpsWg              sync.WaitGroup
	dbOpsMu              sync.Mutex
	dbOpsClosed          bool
	lastAIAnalysis       map[string]time.Time
	lastAIMetric         map[string]database.ReplicationMetric
	lastComplianceReport map[string]time.Time
	closing              int32
}

func New(db *sqlx.DB, cfg *config.Config, hub Hub) *JobManager {
	sink, err := metrics.NewSink(cfg.Monitoring)
	if err != nil {
		logger.Warn("Failed to create metrics sink: %v", err)
	}

	var aiConfig config.AIConfig
	if dbConfig, err := database.LoadConfig(db); err == nil {
		aiConfig = dbConfig.AI
	} else {
		aiConfig = cfg.AI
	}

	jm := &JobManager{
		Db:                   db,
		Config:               cfg,
		KafMirrors:           make(map[string]kafka.KafMirror),
		Hub:                  hub,
		KafMirrorFactory:     kafka.NewKafMirror,
		metricsSink:          sink,
		AIClient:             ai.NewClient(aiConfig),
		close:                make(chan struct{}),
		lastAIAnalysis:       make(map[string]time.Time),
		lastAIMetric:         make(map[string]database.ReplicationMetric),
		lastComplianceReport: make(map[string]time.Time),
	}

	if strings.EqualFold(cfg.Server.Mode, "test") {
		return jm
	}

	jm.wg.Add(1)
	go func() {
		defer jm.wg.Done()
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()

		sleepRemaining := 2 * time.Second
		for sleepRemaining > 0 {
			select {
			case <-ticker.C:
				sleepRemaining -= 100 * time.Millisecond
			case <-jm.close:
				return
			}
		}

		select {
		case <-jm.close:
			return
		default:
			logger.Info("Reconciling job states on startup...")
			if err := jm.reconcileAndStartJobs(); err != nil {
				logger.Error("Failed to reconcile and start jobs on startup: %v", err)
			}
		}
	}()

	jm.wg.Add(6)
	go jm.startPruning()
	go jm.startAIAnalysis()
	go jm.startHistoricalAnalysis()
	go jm.startTopicHealthChecks()
	go jm.startMirrorStateUpdates()
	go jm.startComplianceScheduler()
	return jm
}

func (jm *JobManager) reconcileAndStartJobs() error {
	if jm.Db == nil {
		logger.Error("Database not available for job reconciliation")
		return fmt.Errorf("database not available")
	}

	jobs, err := database.ListJobs(jm.Db)
	if err != nil {
		return fmt.Errorf("failed to list jobs for reconciliation: %v", err)
	}

	for _, job := range jobs {
		if job.Status == "active" {
			logger.Info("Job '%s' (%s) is marked as active, ensuring it is started.", job.Name, job.ID)
			if err := jm.StartJob(job.ID); err != nil {
				logger.Error("Failed to automatically start active job %s: %v", job.ID, err)
				job.Status = "paused"
				if updateErr := database.UpdateJob(jm.Db, &job); updateErr != nil {
					logger.Error("Failed to update status for job %s after start failure: %v", job.ID, updateErr)
				}
			}
		}
	}
	return nil
}

func (jm *JobManager) Close() {
	atomic.StoreInt32(&jm.closing, 1)
	close(jm.close)
	jm.wg.Wait()
	jm.dbOpsMu.Lock()
	jm.dbOpsClosed = true
	jm.dbOpsMu.Unlock()
	jm.dbOpsWg.Wait()
}

func (jm *JobManager) beginDBOp() bool {
	jm.dbOpsMu.Lock()
	defer jm.dbOpsMu.Unlock()

	if jm.dbOpsClosed || atomic.LoadInt32(&jm.closing) == 1 {
		return false
	}

	jm.dbOpsWg.Add(1)
	return true
}

func (jm *JobManager) startPruning() {
	defer jm.wg.Done()
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			err := database.PruneOldData(jm.Db, jm.Config.Database.RetentionDays)
			if err != nil {
				logger.Error("Failed to prune old data: %v", err)
			}
			err = database.ArchiveInactiveClusters(jm.Db, 72*time.Hour)
			if err != nil {
				logger.Error("Failed to archive inactive clusters: %v", err)
			}
		case <-jm.close:
			return
		}
	}
}

func (jm *JobManager) startComplianceScheduler() {
	defer jm.wg.Done()
	if !jm.Config.Compliance.Schedule.Enabled {
		return
	}

	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()

	run := func(now time.Time) {
		schedule := jm.Config.Compliance.Schedule
		periods := []string{}
		if schedule.Daily {
			periods = append(periods, "daily")
		}
		if schedule.Weekly {
			periods = append(periods, "weekly")
		}
		if schedule.Monthly {
			periods = append(periods, "monthly")
		}

		userID, err := database.GetInitialUserID(jm.Db)
		if err != nil {
			logger.Warn("Compliance scheduler skipped: failed to locate initial admin user: %v", err)
			return
		}

		for _, period := range periods {
			lastRun := jm.lastComplianceReport[period]
			if !shouldRunComplianceReport(period, now, lastRun, schedule.RunHour) {
				continue
			}
			if _, err := database.GenerateComplianceReport(jm.Db, period, userID); err != nil {
				logger.Error("Failed to generate %s compliance report: %v", period, err)
				continue
			}
			jm.lastComplianceReport[period] = now
			logger.Info("Generated %s compliance report", period)
		}
	}

	run(time.Now())

	for {
		select {
		case <-ticker.C:
			run(time.Now())
		case <-jm.close:
			return
		}
	}
}

func shouldRunComplianceReport(period string, now time.Time, lastRun time.Time, runHour int) bool {
	if now.Hour() != runHour {
		return false
	}

	switch period {
	case "daily":
		return !sameDay(now, lastRun)
	case "weekly":
		if now.Weekday() != time.Monday {
			return false
		}
		return !sameISOWeek(now, lastRun)
	case "monthly":
		if now.Day() != 1 {
			return false
		}
		return !sameMonth(now, lastRun)
	default:
		return false
	}
}

func sameDay(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd
}

func sameISOWeek(a, b time.Time) bool {
	ay, aw := a.ISOWeek()
	by, bw := b.ISOWeek()
	return ay == by && aw == bw
}

func sameMonth(a, b time.Time) bool {
	ay, am, _ := a.Date()
	by, bm, _ := b.Date()
	return ay == by && am == bm
}

// ShouldRunComplianceReportForTest exposes scheduling logic for tests.
func ShouldRunComplianceReportForTest(period string, now time.Time, lastRun time.Time, runHour int) bool {
	return shouldRunComplianceReport(period, now, lastRun, runHour)
}

func (jm *JobManager) StartAllJobs() error {
	jobs, err := database.ListJobs(jm.Db)
	if err != nil {
		return err
	}

	for _, job := range jobs {
		if job.Status != "active" {
			if err := jm.StartJob(job.ID); err != nil {
				logger.Error("Failed to start job %s: %v", job.ID, err)
			}
		}
	}

	return nil
}

func (jm *JobManager) SyncJobStates() error {
	jm.Mu.Lock()
	defer jm.Mu.Unlock()

	if jm.Db == nil {
		logger.Error("Database not available for job state sync")
		return nil
	}

	jobs, err := database.ListJobs(jm.Db)
	if err != nil {
		logger.Error("Failed to list jobs for state sync: %v", err)
		return nil
	}

	for _, job := range jobs {
		_, isRunning := jm.KafMirrors[job.ID]

		if job.Status == "active" && !isRunning {
			logger.Warn("Job %s marked as active in DB but not running - marking as paused", job.ID)
			job.Status = "paused"
			if err := database.UpdateJob(jm.Db, &job); err != nil {
				logger.Error("Failed to update job status for %s: %v", job.ID, err)
			}
		} else if job.Status == "paused" && isRunning {
			logger.Warn("Job %s marked as paused in DB but running - updating DB status", job.ID)
			job.Status = "active"
			if err := database.UpdateJob(jm.Db, &job); err != nil {
				logger.Error("Failed to update job status for %s: %v", job.ID, err)
			}
		}
	}

	return nil
}

func (jm *JobManager) RestartAllJobs() error {
	logger.Info("Restarting all jobs...")

	if err := jm.SyncJobStates(); err != nil {
		logger.Error("Failed to sync job states: %v", err)
	}

	if jm.Db == nil {
		logger.Error("Database not available for job restart")
		return fmt.Errorf("database not available")
	}

	jobs, err := database.ListJobs(jm.Db)
	if err != nil {
		return err
	}

	jm.Mu.Lock()
	runningJobs := make([]string, 0, len(jm.KafMirrors))
	for jobID := range jm.KafMirrors {
		runningJobs = append(runningJobs, jobID)
	}
	jm.Mu.Unlock()

	for _, jobID := range runningJobs {
		logger.Info("Stopping job %s for restart", jobID)
		if err := jm.StopJob(jobID); err != nil {
			logger.Error("Failed to stop job %s during restart all: %v", jobID, err)
		}
	}

	restartedCount := 0
	for _, job := range jobs {
		if job.Status == "active" || job.Status == "paused" {
			logger.Info("Starting job '%s' (%s)", job.Name, job.ID)
			if err := jm.StartJob(job.ID); err != nil {
				logger.Error("Failed to start job %s during restart all: %v", job.ID, err)
			} else {
				restartedCount++
			}
		}
	}

	logger.Info("Completed restart of all jobs (%d jobs restarted)", restartedCount)
	return nil
}

func (jm *JobManager) RestartJob(jobID string) error {
	job, err := database.GetJob(jm.Db, jobID)
	if err != nil {
		return fmt.Errorf("failed to get job %s: %v", jobID, err)
	}

	wasRunning := job.Status == "active"

	if wasRunning {
		if err := jm.StopJob(jobID); err != nil {
			logger.Error("Failed to stop job during restart: %v", err)
		}
	}

	if err := jm.StartJob(jobID); err != nil {
		return fmt.Errorf("failed to start job after restart: %v", err)
	}

	job.Status = "active"
	if err := database.UpdateJob(jm.Db, job); err != nil {
		return fmt.Errorf("failed to update job status after restart: %v", err)
	}

	return nil
}

func (jm *JobManager) ForceRestartJob(jobID string) error {
	jm.Mu.Lock()
	if kafMirror, ok := jm.KafMirrors[jobID]; ok {
		logger.Info("Forcefully stopping existing instance of job %s", jobID)
		kafMirror.Stop()
		delete(jm.KafMirrors, jobID)
	}
	jm.Mu.Unlock()

	job, err := database.GetJob(jm.Db, jobID)
	if err != nil {
		return fmt.Errorf("failed to get job %s: %v", jobID, err)
	}
	job.Status = "paused"
	job.FailedReason = nil
	if err := database.UpdateJob(jm.Db, job); err != nil {
		return fmt.Errorf("failed to reset job state before force-restart: %v", err)
	}

	logger.Info("Performing pre-flight health checks for job %s", jobID)
	sourceCluster, err := database.GetCluster(jm.Db, job.SourceClusterName)
	if err != nil {
		return fmt.Errorf("failed to get source cluster: %v", err)
	}
	targetCluster, err := database.GetCluster(jm.Db, job.TargetClusterName)
	if err != nil {
		return fmt.Errorf("failed to get target cluster: %v", err)
	}

	if err := jm.healthCheckCluster(sourceCluster, "source"); err != nil {
		return fmt.Errorf("source cluster health check failed: %v", err)
	}

	if err := jm.healthCheckCluster(targetCluster, "target"); err != nil {
		return fmt.Errorf("target cluster health check failed: %v", err)
	}

	if err := jm.validateMirrorState(jobID, sourceCluster, targetCluster); err != nil {
		return fmt.Errorf("mirror state validation failed: %v", err)
	}

	logger.Info("Pre-flight checks passed. Starting job %s", jobID)
	return jm.StartJob(jobID)
}

func (jm *JobManager) StopAllJobs() error {
	jm.Mu.Lock()
	defer jm.Mu.Unlock()

	jobCount := len(jm.KafMirrors)
	if jobCount == 0 {
		return nil
	}

	for jobID, kafMirror := range jm.KafMirrors {
		kafMirror.Stop()
		delete(jm.KafMirrors, jobID)

		job, err := database.GetJob(jm.Db, jobID)
		if err != nil {
			logger.Error("Failed to get job %s for status update: %v", jobID, err)
			continue
		}

		job.Status = "paused"
		if err := database.UpdateJob(jm.Db, job); err != nil {
			logger.Error("Failed to update status for job %s: %v", jobID, err)
		}
	}

	return nil
}

func (jm *JobManager) StartJob(jobID string) error {
	jm.Mu.Lock()
	defer jm.Mu.Unlock()

	if _, ok := jm.KafMirrors[jobID]; ok {
		logger.Warn("Job %s is already running in memory", jobID)
		return fmt.Errorf("job %s is already running", jobID)
	}

	job, err := database.GetJob(jm.Db, jobID)
	if err != nil {
		logger.Error("Failed to get job %s from database: %v", jobID, err)
		return err
	}

	if job.Status == "active" {
		logger.Info("Job %s was marked active in database but not running - restarting", jobID)
	}

	sourceCluster, err := database.GetCluster(jm.Db, job.SourceClusterName)
	if err != nil {
		logger.Error("Failed to get source cluster %s: %v", job.SourceClusterName, err)
		return err
	}

	targetCluster, err := database.GetCluster(jm.Db, job.TargetClusterName)
	if err != nil {
		logger.Error("Failed to get target cluster %s: %v", job.TargetClusterName, err)
		return err
	}

	mappings, err := database.GetMappingsForJob(jm.Db, jobID)
	if err != nil {
		logger.Error("Failed to get mappings for job %s: %v", jobID, err)
		return err
	}

	jobConfig, err := jm.buildJobConfig(sourceCluster, targetCluster, mappings, job)
	if err != nil {
		logger.Error("Failed to build job config for job %s: %v", jobID, err)
		return err
	}

	jobConfig.Replication.JobID = jobID
	kafMirror, err := jm.KafMirrorFactory(jobConfig)
	if err != nil {
		logger.Error("Failed to create KafMirror for job %s: %v", jobID, err)
		return err
	}

	logger.Info("Starting job '%s' (%s)", job.Name, jobID)
	kafMirror.Start(jobID, jm.ProcessMetrics, jm.handleJobPanic)
	jm.KafMirrors[jobID] = kafMirror

	job.Status = "active"
	job.FailedReason = nil
	err = database.UpdateJob(jm.Db, job)
	if err != nil {
		logger.Error("Failed to update job status for job %s: %v", jobID, err)
		// Rollback by stopping the kaf-mirror instance
		kafMirror.Stop()
		delete(jm.KafMirrors, jobID)
		return err
	}

	logger.Info("Successfully started job '%s' (%s)", job.Name, jobID)

	// Trigger an inventory snapshot when the job starts
	go jm.CreateInventorySnapshot(jobID, "manual")

	// Trigger immediate mirror state update
	go func() {
		time.Sleep(2 * time.Second) // Give the job a moment to start
		jm.updateMirrorState(jobID)
	}()

	return nil
}

// ProcessMetrics is the callback function for the kaf-mirror to send metrics.
func (jm *JobManager) ProcessMetrics(metric database.ReplicationMetric) {
	if jm.metricsSink != nil {
		if err := jm.metricsSink.Send(metric); err != nil {
			logger.Error("Failed to send metric to sink: %v", err)
		}
	}

	if err := database.InsertMetrics(jm.Db, &metric); err != nil {
		logger.Error("Failed to insert metric into database: %v", err)
	}
}

// StopJob stops a replication job by its ID.
func (jm *JobManager) StopJob(jobID string) error {
	jm.Mu.Lock()
	defer jm.Mu.Unlock()

	kafMirror, ok := jm.KafMirrors[jobID]
	if !ok {
		// Check if job exists in database but not running in memory
		job, err := database.GetJob(jm.Db, jobID)
		if err != nil {
			return fmt.Errorf("job %s not found: %v", jobID, err)
		}
		if job.Status == "active" {
			// Job is marked active but not running - update database
			logger.Info("Job %s was marked active but not running - updating database status", jobID)
			job.Status = "paused"
			database.UpdateJob(jm.Db, job)
		}
		return fmt.Errorf("job %s is not running", jobID)
	}

	logger.Info("Stopping job '%s'", jobID)
	kafMirror.Stop()
	delete(jm.KafMirrors, jobID)

	// Update job status in database synchronously to avoid race conditions
	job, err := database.GetJob(jm.Db, jobID)
	if err != nil {
		logger.Error("Failed to get job %s from database for status update: %v", jobID, err)
		return nil // Don't return error - job was stopped successfully in memory
	}

	job.Status = "paused"
	job.FailedReason = nil
	err = database.UpdateJob(jm.Db, job)
	if err != nil {
		logger.Error("Failed to update job status for job %s: %v", jobID, err)
		// Don't return error - job was stopped successfully in memory
	}

	logger.Info("Successfully stopped job '%s'", jobID)
	return nil
}

// PauseJob pauses a replication job. For now, this is the same as stopping.
func (jm *JobManager) PauseJob(jobID string) error {
	return jm.StopJob(jobID)
}

func (jm *JobManager) handleJobPanic(jobID string, reason string) {
	jm.Mu.Lock()
	defer jm.Mu.Unlock()

	delete(jm.KafMirrors, jobID)

	job, err := database.GetJob(jm.Db, jobID)
	if err != nil {
		logger.Error("Failed to get job %s from database for panic update: %v", jobID, err)
		return
	}

	job.Status = "failed"
	job.FailedReason = &reason
	if err := database.UpdateJob(jm.Db, job); err != nil {
		logger.Error("Failed to update job status to failed for job %s: %v", jobID, err)
	}
}

// startAIAnalysis runs intelligent anomaly-based AI analysis every 5 minutes with cost-effective triggering
func (jm *JobManager) startAIAnalysis() {
	defer jm.wg.Done()
	if jm.AIClient == nil {
		return
	}

	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			jm.performSmartAnalysis()
		case <-jm.close:
			return
		}
	}
}

// performSmartAnalysis only triggers AI when anomalies are detected to reduce API calls by 90%
func (jm *JobManager) performSmartAnalysis() {
	jobs, err := database.ListJobs(jm.Db)
	if err != nil {
		logger.Error("Failed to get jobs for smart AI analysis: %v", err)
		return
	}

	activeJobs := make([]database.ReplicationJob, 0)
	for _, job := range jobs {
		if job.Status == "active" {
			activeJobs = append(activeJobs, job)
		}
	}

	if len(activeJobs) == 0 {
		return
	}

	// Check each job for anomalies before triggering expensive AI analysis
	for _, job := range activeJobs {
		// Perform dynamic throttling if enabled
		if jm.Config.AI.SelfHealing.DynamicThrottling {
			go jm.performDynamicThrottling(job.ID, job.Name)
		}

		if jm.shouldTriggerAIAnalysis(job.ID, job.Name) {
			go jm.analyzeJobTimeSeries(job.ID, job.Name)
		}
	}
}

// shouldTriggerAIAnalysis implements cost-effective AI triggering rules
func (jm *JobManager) shouldTriggerAIAnalysis(jobID, jobName string) bool {
	// In-memory rate limiting: max 1 AI call per job per 5 minutes
	if ts, ok := jm.lastAIAnalysis[jobID]; ok && time.Since(ts) < 5*time.Minute {
		logger.InfoAI("ai", "analysis", jobID, "Skipping analysis for %s - throttled by TTL", jobName)
		return false
	}

	// Get recent metrics to check for anomalies
	metricsQuery := `
		SELECT 
			messages_replicated_delta AS messages_replicated,
			avg_lag AS current_lag,
			error_count_delta AS error_count,
			timestamp
		FROM aggregated_metrics 
		WHERE job_id = ? AND timestamp > datetime('now', '-10 minutes')
		ORDER BY timestamp DESC LIMIT 10
	`

	var recentMetrics []database.ReplicationMetric
	err := jm.Db.Select(&recentMetrics, metricsQuery, jobID)
	if err != nil || len(recentMetrics) < 1 {
		logger.InfoAI("ai", "analysis", jobID, "Insufficient metrics for analysis: %s (got %d metrics)", jobName, len(recentMetrics))
		return false
	}

	// Skip if metrics haven’t changed meaningfully since last AI run
	latest := recentMetrics[0]
	if prev, ok := jm.lastAIMetric[jobID]; ok {
		if latest.MessagesReplicated == prev.MessagesReplicated &&
			latest.CurrentLag == prev.CurrentLag &&
			latest.ErrorCount == prev.ErrorCount {
			logger.InfoAI("ai", "analysis", jobID, "Skipping analysis for %s - no metric change", jobName)
			return false
		}
	}

	// For now, allow analysis for any active job with metrics (less restrictive for testing)
	logger.InfoAI("ai", "analysis", jobID, "Triggering AI analysis for %s with %d metrics", jobName, len(recentMetrics))
	jm.lastAIMetric[jobID] = recentMetrics[0]
	jm.lastAIAnalysis[jobID] = time.Now()

	// If zero throughput is detected, add a specific recommendation to the analysis
	if jm.detectAnomalies(recentMetrics, jobName) {
		// This is a simplified check. A more robust implementation would be in detectAnomalies.
		isZeroThroughput := true
		for _, m := range recentMetrics {
			if m.MessagesReplicated > 0 {
				isZeroThroughput = false
				break
			}
		}
		if isZeroThroughput {
			if jm.Config.AI.SelfHealing.AutomatedRemediation {
				logger.InfoAI("ai", "remediation", jobID, "Automated remediation: force-restarting job %s", jobName)
				go jm.ForceRestartJob(jobID)
			} else {
				go func() {
					time.Sleep(10 * time.Second) // Allow other analyses to complete first
					recommendation := "Zero throughput detected. Consider using 'jobs restart-force' to recover the job."
					insight := &database.AIInsight{
						JobID:          &jobID,
						InsightType:    "recommendation",
						Recommendation: recommendation,
						SeverityLevel:  "high",
						AIModel:        "system-rule",
					}
					database.InsertAIInsight(jm.Db, insight)
				}()
			}
		}
	}

	return true
}

// detectAnomalies checks for performance anomalies that warrant AI analysis
func (jm *JobManager) detectAnomalies(metrics []database.ReplicationMetric, jobName string) bool {
	if len(metrics) < 3 {
		return false
	}

	// Calculate baseline from older metrics vs recent metrics
	baseline := metrics[len(metrics)/2:] // Older half
	recent := metrics[:len(metrics)/2]   // Recent half

	// Calculate averages
	var baselineThroughput, recentThroughput float64
	var baselineLag, recentLag float64
	var baselineErrors, recentErrors int

	for _, m := range baseline {
		baselineThroughput += float64(m.MessagesReplicated)
		baselineLag += float64(m.CurrentLag)
		baselineErrors += m.ErrorCount
	}
	baselineThroughput /= float64(len(baseline))
	baselineLag /= float64(len(baseline))

	for _, m := range recent {
		recentThroughput += float64(m.MessagesReplicated)
		recentLag += float64(m.CurrentLag)
		recentErrors += m.ErrorCount
	}
	recentThroughput /= float64(len(recent))
	recentLag /= float64(len(recent))

	// Anomaly detection based on AI implementation rules:

	// 1. Throughput drops >30%
	if baselineThroughput > 0 && (baselineThroughput-recentThroughput)/baselineThroughput > 0.30 {
		logger.WarnAI("ai", "anomaly", "", "Throughput drop detected for job %s: %.0f -> %.0f (%.1f%% drop)",
			jobName, baselineThroughput, recentThroughput,
			(baselineThroughput-recentThroughput)/baselineThroughput*100)
		return true
	}

	// 2. Lag spikes (doubled or >1000 messages)
	if (baselineLag > 0 && recentLag > baselineLag*2) || recentLag > 1000 {
		logger.WarnAI("ai", "anomaly", "", "Lag spike detected for job %s: %.0f -> %.0f messages",
			jobName, baselineLag, recentLag)
		return true
	}

	// 3. Error rate >0.5%
	totalMessages := 0
	for _, m := range metrics {
		totalMessages += m.MessagesReplicated
	}
	totalErrors := recentErrors + baselineErrors
	errorRate := float64(totalErrors) / float64(totalMessages) * 100
	if errorRate > 0.5 {
		logger.WarnAI("ai", "anomaly", "", "High error rate detected for job %s: %.2f%% (%d errors)",
			jobName, errorRate, totalErrors)
		return true
	}

	// 4. Zero throughput (system issues)
	if recentThroughput == 0 && baselineThroughput > 0 {
		logger.WarnAI("ai", "anomaly", "", "Zero throughput detected for job %s", jobName)
		return true
	}

	// No anomalies detected - skip expensive AI analysis
	return false
}

// analyzeJobTimeSeries analyzes time-series patterns for a specific job using enhanced log + metrics analysis
func (jm *JobManager) analyzeJobTimeSeries(jobID, jobName string) {
	logger.InfoAI("ai", "analysis", jobID, "Starting comprehensive AI analysis for job %s", jobName)

	startTime := time.Now()

	// Generate multiple insight types for comprehensive analysis
	insightTypes := []string{"enhanced_analysis", "recommendation", "optimization"}

	successCount := 0
	for _, insightType := range insightTypes {
		err := database.GenerateEnhancedAIInsight(jm.Db, jm.AIClient, jobID, insightType)
		if err != nil {
			logger.ErrorAI("ai", "analysis", jobID, "%s AI analysis failed for job %s: %v", insightType, jobName, err)
		} else {
			successCount++
		}

		// Brief delay between analyses to avoid rate limiting
		time.Sleep(500 * time.Millisecond)
	}

	responseTime := int(time.Since(startTime).Milliseconds())

	if successCount == 0 {
		logger.ErrorAI("ai", "analysis", jobID, "All AI analyses failed for job %s after %dms", jobName, responseTime)

		// Fallback to metrics-only analysis if all enhanced analyses fail
		jm.fallbackMetricsAnalysis(jobID, jobName)
		return
	}

	logger.InfoAI("ai", "analysis", jobID, "Comprehensive AI analysis completed for job %s in %dms (%d/%d successful)", jobName, responseTime, successCount, len(insightTypes))
}

// fallbackMetricsAnalysis provides metrics-only analysis as fallback
func (jm *JobManager) fallbackMetricsAnalysis(jobID, jobName string) {
	// Get metrics from the last 10 minutes
	query := `
		SELECT 
			messages_replicated_delta AS messages_replicated,
			bytes_transferred_delta AS bytes_transferred,
			avg_lag AS current_lag,
			error_count_delta AS error_count,
			timestamp
		FROM aggregated_metrics 
		WHERE job_id = ? AND timestamp > datetime('now', '-10 minutes')
		ORDER BY timestamp ASC
	`

	var metrics []database.ReplicationMetric
	err := jm.Db.Select(&metrics, query, jobID)
	if err != nil {
		logger.ErrorAI("ai", "fallback", jobID, "Failed to fetch metrics for fallback analysis: %v", err)
		return
	}

	if len(metrics) < 2 {
		logger.WarnAI("ai", "fallback", jobID, "Insufficient metrics data for fallback analysis (%d points)", len(metrics))
		return
	}

	// Build comprehensive time-series analysis prompt
	analysisPrompt := jm.buildTimeSeriesPrompt(jobID, jobName, metrics)

	insight, err := jm.AIClient.GetAnomalyDetection(context.Background(), analysisPrompt)
	if err != nil {
		logger.ErrorAI("ai", "fallback", jobID, "Fallback AI analysis failed: %v", err)
		return
	}

	// Determine severity based on metrics analysis
	severity := jm.determineSeverity(metrics)

	aiInsight := &database.AIInsight{
		JobID:            &jobID,
		InsightType:      "fallback_analysis",
		Recommendation:   insight,
		SeverityLevel:    severity,
		ResolutionStatus: "new",
		AIModel:          jm.Config.AI.Model,
	}

	if err := database.InsertAIInsight(jm.Db, aiInsight); err != nil {
		logger.ErrorAI("ai", "fallback", jobID, "Failed to store fallback AI insight: %v", err)
	} else {
		logger.InfoAI("ai", "fallback", jobID, "Fallback analysis completed and stored")
	}
}

// buildTimeSeriesPrompt creates a comprehensive analysis prompt with time-series data
func (jm *JobManager) buildTimeSeriesPrompt(jobID, jobName string, metrics []database.ReplicationMetric) string {
	if len(metrics) == 0 {
		return ""
	}

	// Calculate statistics
	var totalThroughput, totalLag, totalErrors int
	var minThroughput, maxThroughput = metrics[0].MessagesReplicated, metrics[0].MessagesReplicated
	var minLag, maxLag = metrics[0].CurrentLag, metrics[0].CurrentLag

	for _, m := range metrics {
		totalThroughput += m.MessagesReplicated
		totalLag += m.CurrentLag
		totalErrors += m.ErrorCount

		if m.MessagesReplicated < minThroughput {
			minThroughput = m.MessagesReplicated
		}
		if m.MessagesReplicated > maxThroughput {
			maxThroughput = m.MessagesReplicated
		}
		if m.CurrentLag < minLag {
			minLag = m.CurrentLag
		}
		if m.CurrentLag > maxLag {
			maxLag = m.CurrentLag
		}
	}

	avgThroughput := totalThroughput / len(metrics)
	avgLag := totalLag / len(metrics)

	// Calculate trends
	firstHalf := metrics[:len(metrics)/2]
	secondHalf := metrics[len(metrics)/2:]

	var firstHalfThroughput, secondHalfThroughput int
	for _, m := range firstHalf {
		firstHalfThroughput += m.MessagesReplicated
	}
	for _, m := range secondHalf {
		secondHalfThroughput += m.MessagesReplicated
	}

	throughputTrend := "stable"
	expectedThroughput := float64(firstHalfThroughput) * float64(len(secondHalf)) / float64(len(firstHalf))
	if float64(secondHalfThroughput) > expectedThroughput*1.2 {
		throughputTrend = "increasing"
	} else if float64(secondHalfThroughput) < expectedThroughput*0.8 {
		throughputTrend = "decreasing"
	}

	return fmt.Sprintf(`# Kafka Replication Job Analysis

## Job Information
- **Job ID**: %s
- **Job Name**: %s
- **Analysis Period**: Last 10 minutes
- **Data Points**: %d metrics

## Performance Metrics Summary
- **Average Throughput**: %d messages/second
- **Throughput Range**: %d - %d messages/second
- **Throughput Trend**: %s
- **Average Lag**: %d messages
- **Lag Range**: %d - %d messages
- **Total Errors**: %d errors

## Time Series Pattern Analysis
Please analyze this Kafka replication job's performance patterns and identify:

1. **Performance Anomalies**: Any unusual spikes, drops, or patterns in throughput or lag
2. **Trend Analysis**: Whether performance is improving, degrading, or stable
3. **Error Correlation**: Any correlation between errors and performance degradation
4. **Optimization Recommendations**: Specific suggestions for improving performance

## Analysis Requirements
- Focus on **actionable insights** for operations teams
- Identify **critical issues** that require immediate attention
- Provide **specific recommendations** with quantifiable improvements
- Consider **Kafka-specific factors** like consumer lag, partition rebalancing, broker performance

Based on this time-series data, what are your key findings and recommendations?`,
		jobID, jobName, len(metrics), avgThroughput, minThroughput, maxThroughput,
		throughputTrend, avgLag, minLag, maxLag, totalErrors)
}

// startHistoricalAnalysis runs long-term historical analysis every 24 hours
func (jm *JobManager) startHistoricalAnalysis() {
	defer jm.wg.Done()
	if jm.AIClient == nil {
		return
	}

	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			jm.performHistoricalAnalysis()
		case <-jm.close:
			return
		}
	}
}

// performHistoricalAnalysis analyzes metrics trends over a long-term period for all active jobs
func (jm *JobManager) performHistoricalAnalysis() {
	jobs, err := database.ListJobs(jm.Db)
	if err != nil {
		logger.Error("Failed to get jobs for historical AI analysis: %v", err)
		return
	}

	for _, job := range jobs {
		if job.Status == "active" {
			go jm.AnalyzeJobHistory(job.ID, job.Name, 7, "daily") // 7 days, daily granularity
		}
	}
}

// AnalyzeJobHistory analyzes historical trends for a specific job
func (jm *JobManager) AnalyzeJobHistory(jobID, jobName string, periodDays int, granularity string) {
	metrics, err := database.GetAggregatedHistoricalMetrics(jm.Db, jobID, periodDays, granularity)
	if err != nil {
		logger.Error("Failed to fetch historical metrics for AI analysis (job %s): %v", jobID, err)
		return
	}

	if len(metrics) < 2 {
		return // Not enough data for historical analysis
	}

	analysisPrompt := jm.buildHistoricalAnalysisPrompt(jobID, jobName, metrics, periodDays)

	insight, err := jm.AIClient.GetAnomalyDetection(context.Background(), analysisPrompt)
	if err != nil {
		logger.Error("Failed to get AI historical analysis for job %s: %v", jobID, err)
		return
	}

	aiInsight := &database.AIInsight{
		JobID:            &jobID,
		InsightType:      "historical_trend",
		Recommendation:   insight,
		SeverityLevel:    "info", // Historical trends are informational
		ResolutionStatus: "new",
		AIModel:          jm.Config.AI.Model,
	}

	if err := database.InsertAIInsight(jm.Db, aiInsight); err != nil {
		logger.Error("Failed to insert AI historical insight for job %s: %v", jobID, err)
	}
}

// buildHistoricalAnalysisPrompt creates a prompt for long-term historical analysis
func (jm *JobManager) buildHistoricalAnalysisPrompt(jobID, jobName string, metrics []database.AggregatedMetric, periodDays int) string {
	var metricsSummary string
	for _, m := range metrics {
		metricsSummary += fmt.Sprintf("- **Date**: %s, **Avg Throughput**: %.0f msg/s, **Avg Lag**: %.0f, **Total Errors**: %d\n",
			m.Period, m.AvgThroughput, m.AvgLag, m.TotalErrors)
	}

	return fmt.Sprintf(`# Kafka Replication Job - Historical Trend Analysis

## Job Information
- **Job ID**: %s
- **Job Name**: %s
- **Analysis Period**: Last %d days

## Historical Performance Summary (Daily Averages)
%s
## Long-Term Trend Analysis Request
Based on the historical data provided, please analyze the long-term performance trends for this Kafka replication job. Your analysis should focus on:

1.  **Performance Trends**: Is there a gradual increase or decrease in throughput or lag over time?
2.  **Recurring Patterns**: Are there any weekly or cyclical patterns? (e.g., performance dips on weekends, or high traffic at the start of the week).
3.  **Stability Assessment**: How stable is the job's performance over this period?
4.  **Capacity Planning Insights**: Based on the trends, are there any early indicators that capacity (CPU, network, broker performance) might become an issue in the future?
5.  **Actionable Recommendations**: Suggest any proactive tuning or configuration changes to address negative trends or improve long-term stability.

Provide a concise summary of your findings, focusing on strategic insights rather than immediate alerts.`,
		jobID, jobName, periodDays, metricsSummary)
}

// determineSeverity analyzes metrics to determine insight severity level
func (jm *JobManager) determineSeverity(metrics []database.ReplicationMetric) string {
	if len(metrics) == 0 {
		return "info"
	}

	// Check for high error rates
	totalErrors := 0
	totalMessages := 0
	maxLag := 0

	for _, m := range metrics {
		totalErrors += m.ErrorCount
		totalMessages += m.MessagesReplicated
		if m.CurrentLag > maxLag {
			maxLag = m.CurrentLag
		}
	}

	errorRate := float64(totalErrors) / float64(totalMessages) * 100

	// Determine severity based on error rate and lag
	if errorRate > 5.0 || maxLag > 10000 {
		return "high"
	} else if errorRate > 1.0 || maxLag > 5000 {
		return "medium"
	}

	return "info"
}

// AnalyzeJobNow immediately triggers comprehensive AI analysis for a specific job (bypasses rate limiting)
func (jm *JobManager) AnalyzeJobNow(jobID, jobName string) {
	if jm.AIClient == nil {
		logger.Error("Cannot analyze job %s: AI client not configured", jobName)
		return
	}

	logger.InfoAI("ai", "manual", jobID, "Manual AI analysis triggered for job %s", jobName)

	// Force comprehensive analysis regardless of rate limiting
	go jm.analyzeJobTimeSeries(jobID, jobName)
}

func (jm *JobManager) CreateInventorySnapshot(jobID, snapshotType string) {
	if atomic.LoadInt32(&jm.closing) == 1 {
		return
	}
	logger.Info("Creating inventory snapshot for job %s (type: %s)", jobID, snapshotType)

	// Check if database and required tables are available
	var tablesExist int
	err := jm.Db.Get(&tablesExist, "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name IN ('job_inventory_snapshots', 'replication_jobs', 'kafka_clusters')")
	if err != nil {
		logger.Error("Database not available for inventory snapshot: %v", err)
		return
	}
	if tablesExist < 3 {
		logger.Warn("Required database tables not available, skipping inventory snapshot for job %s", jobID)
		return
	}

	// Create the snapshot record in the database
	snapshotID, err := database.CreateInventorySnapshot(jm.Db, jobID, snapshotType)
	if err != nil {
		logger.Error("Failed to create inventory snapshot for job %s: %v", jobID, err)
		return
	}

	logger.Info("Created inventory snapshot %d for job %s", snapshotID, jobID)

	// Get job details for cluster information
	job, err := database.GetJob(jm.Db, jobID)
	if err != nil {
		logger.Error("Failed to get job details for inventory snapshot: %v", err)
		return
	}

	// Create cluster inventories in background with error resilience
	if atomic.LoadInt32(&jm.closing) == 1 {
		return
	}
	if jm.beginDBOp() {
		go func() {
			defer jm.dbOpsWg.Done()
			defer func() {
				if r := recover(); r != nil {
					logger.Error("Inventory capture panicked for source cluster: %v", r)
				}
			}()

			// Check if close signal has been sent
			select {
			case <-jm.close:
				return // Don't perform database operations during shutdown
			default:
			}

			jm.captureClusterInventory(snapshotID, job.SourceClusterName, "source")
		}()
	}

	if jm.beginDBOp() {
		go func() {
			defer jm.dbOpsWg.Done()
			defer func() {
				if r := recover(); r != nil {
					logger.Error("Inventory capture panicked for target cluster: %v", r)
				}
			}()

			// Check if close signal has been sent
			select {
			case <-jm.close:
				return // Don't perform database operations during shutdown
			default:
			}

			jm.captureClusterInventory(snapshotID, job.TargetClusterName, "target")
		}()
	}

	logger.Info("Inventory snapshot capture initiated for job %s", jobID)
}

func (jm *JobManager) captureClusterInventory(snapshotID int, clusterName, clusterType string) {
	// Check if database is available and tables exist before proceeding
	var tableExists int
	err := jm.Db.Get(&tableExists, "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='kafka_clusters'")
	if err != nil {
		logger.Error("Database not available for inventory capture: %v", err)
		return
	}
	if tableExists == 0 {
		logger.Warn("Database tables not initialized, skipping inventory capture for cluster %s", clusterName)
		return
	}

	cluster, err := database.GetCluster(jm.Db, clusterName)
	if err != nil {
		logger.Error("Failed to get cluster %s for inventory: %v", clusterName, err)
		return
	}

	// Create admin client for cluster inspection
	clusterConfig := config.ClusterConfig{
		Provider: cluster.Provider,
		Brokers:  cluster.Brokers,
		Security: config.SecurityConfig{
			APIKey:    cluster.APIKey,
			APISecret: cluster.APISecret,
		},
	}

	adminClient, err := kafka.NewAdminClient(clusterConfig)
	if err != nil {
		logger.Error("Failed to create admin client for cluster %s: %v", clusterName, err)
		// Still record basic cluster info even if admin fails
		jm.recordBasicClusterInventory(snapshotID, cluster, clusterType)
		return
	}
	defer adminClient.Close()

	// Get cluster metadata
	clusterInfo, err := adminClient.GetClusterInfo(context.Background())
	if err != nil {
		logger.Error("Failed to get cluster metadata for %s: %v", clusterName, err)
		jm.recordBasicClusterInventory(snapshotID, cluster, clusterType)
		return
	}

	// Check if inventory tables exist before inserting
	var inventoryTablesExist int
	err = jm.Db.Get(&inventoryTablesExist, "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name IN ('cluster_inventory', 'connection_inventory', 'topic_inventory', 'partition_inventory')")
	if err != nil {
		logger.Error("Database error checking inventory tables: %v", err)
		return
	}
	if inventoryTablesExist < 4 {
		logger.Warn("Inventory tables not available, skipping detailed inventory for cluster %s", clusterName)
		return
	}

	// Record comprehensive cluster inventory
	controllerID := int(clusterInfo.ControllerID)
	clusterInventory := database.ClusterInventory{
		SnapshotID:   snapshotID,
		ClusterType:  clusterType,
		ClusterName:  clusterName,
		Provider:     cluster.Provider,
		Brokers:      cluster.Brokers,
		BrokerCount:  clusterInfo.BrokerCount,
		TotalTopics:  len(clusterInfo.Topics),
		ControllerID: &controllerID,
		ClusterID:    clusterInfo.ClusterID,
	}

	clusterInventoryID, err := database.InsertClusterInventory(jm.Db, clusterInventory)
	if err != nil {
		logger.Error("Failed to insert cluster inventory: %v", err)
		return
	}

	// Record connection inventory
	connectionTimeMs := 50 // Approximate connection time
	var connectionType string
	if clusterType == "source" {
		connectionType = "source_consumer"
	} else {
		connectionType = "target_producer"
	}

	connectionInventory := database.ConnectionInventory{
		SnapshotID:           snapshotID,
		ConnectionType:       connectionType,
		Provider:             cluster.Provider,
		Brokers:              cluster.Brokers,
		SecurityProtocol:     "SASL_SSL",
		SaslMechanism:        "PLAIN",
		APIKeyPrefix:         jm.maskAPIKey(cluster.APIKey),
		ConnectionSuccessful: true,
		ConnectionTimeMs:     &connectionTimeMs,
		ErrorMessage:         "",
	}

	if err := database.InsertConnectionInventory(jm.Db, connectionInventory); err != nil {
		logger.Error("Failed to insert connection inventory: %v", err)
	}

	// Record topic inventories
	for topicName, topic := range clusterInfo.Topics {
		topicInventory := database.TopicInventory{
			ClusterInventoryID: clusterInventoryID,
			TopicName:          topicName,
			PartitionCount:     int(topic.Partitions),
			ReplicationFactor:  int(topic.ReplicationFactor),
			IsInternal:         false, // Non-internal topics from cluster info
			ConfigData:         "{}",  // Topic config would be fetched separately
		}

		topicInventoryID, err := database.InsertTopicInventory(jm.Db, topicInventory)
		if err != nil {
			logger.Error("Failed to insert topic inventory for %s: %v", topicName, err)
			continue
		}

		// Record partition inventories
		for _, partition := range topic.PartitionInfo {
			leaderID := int(partition.Leader)
			partitionInventory := database.PartitionInventory{
				TopicInventoryID: topicInventoryID,
				PartitionID:      int(partition.ID),
				LeaderID:         &leaderID,
				ReplicaIDs:       jm.int32SliceToJSON(partition.Replicas),
				IsrIDs:           jm.int32SliceToJSON(partition.ISR),
				HighWaterMark:    0, // Would need separate call to get high water marks
			}

			if err := database.InsertPartitionInventory(jm.Db, partitionInventory); err != nil {
				logger.Error("Failed to insert partition inventory for %s-%d: %v", topicName, partition.ID, err)
			}
		}
	}

	logger.Info("Completed cluster inventory capture for %s (%s)", clusterName, clusterType)
}

func (jm *JobManager) recordBasicClusterInventory(snapshotID int, cluster *database.KafkaCluster, clusterType string) {
	// Record basic cluster info when admin client fails
	clusterInventory := database.ClusterInventory{
		SnapshotID:   snapshotID,
		ClusterType:  clusterType,
		ClusterName:  cluster.Name,
		Provider:     cluster.Provider,
		Brokers:      cluster.Brokers,
		BrokerCount:  0, // Unknown when admin fails
		TotalTopics:  0, // Unknown when admin fails
		ControllerID: nil,
		ClusterID:    "",
	}

	if _, err := database.InsertClusterInventory(jm.Db, clusterInventory); err != nil {
		logger.Error("Failed to insert basic cluster inventory: %v", err)
	}
}

func (jm *JobManager) maskAPIKey(apiKey string) string {
	if len(apiKey) <= 4 {
		return "****"
	}
	return apiKey[:4] + "****"
}

func (jm *JobManager) intSliceToJSON(slice []int) string {
	if len(slice) == 0 {
		return "[]"
	}
	data, _ := json.Marshal(slice)
	return string(data)
}

func (jm *JobManager) int32SliceToJSON(slice []int32) string {
	if len(slice) == 0 {
		return "[]"
	}
	data, _ := json.Marshal(slice)
	return string(data)
}

func (jm *JobManager) buildJobConfig(sourceCluster, targetCluster *database.KafkaCluster, mappings []database.TopicMapping, job *database.ReplicationJob) (*config.Config, error) {
	sourceConfig := config.ClusterConfig{
		Provider: sourceCluster.Provider,
		Brokers:  sourceCluster.Brokers,
		Security: config.SecurityConfig{
			APIKey:    sourceCluster.APIKey,
			APISecret: sourceCluster.APISecret,
		},
	}

	targetConfig := config.ClusterConfig{
		Provider: targetCluster.Provider,
		Brokers:  targetCluster.Brokers,
		Security: config.SecurityConfig{
			APIKey:    targetCluster.APIKey,
			APISecret: targetCluster.APISecret,
		},
	}

	jobConfig := &config.Config{
		Clusters: map[string]config.ClusterConfig{
			"source": sourceConfig,
			"target": targetConfig,
		},
		Replication: config.ReplicationConfig{
			BatchSize:   job.BatchSize,
			Parallelism: job.Parallelism,
			Compression: job.Compression,
		},
		Topics: make([]config.TopicMapping, len(mappings)),
	}

	for i, m := range mappings {
		jobConfig.Topics[i] = config.TopicMapping{
			Source:  m.SourceTopicPattern,
			Target:  m.TargetTopicPattern,
			Enabled: m.Enabled,
		}
	}

	return jobConfig, nil
}

func (jm *JobManager) startTopicHealthChecks() {
	defer jm.wg.Done()
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			jm.performTopicHealthChecks()
		case <-jm.close:
			return
		}
	}
}

func (jm *JobManager) performTopicHealthChecks() {
	jobs, err := database.ListJobs(jm.Db)
	if err != nil {
		logger.Error("Failed to get jobs for topic health check: %v", err)
		return
	}

	for _, job := range jobs {
		if job.Status == "active" {
			go jm.checkJobTopicHealth(&job)
		}
	}
}

func (jm *JobManager) checkJobTopicHealth(job *database.ReplicationJob) {
	sourceCluster, err := database.GetCluster(jm.Db, job.SourceClusterName)
	if err != nil {
		logger.Error("Failed to get source cluster for health check (job %s): %v", job.ID, err)
		return
	}

	mappings, err := database.GetMappingsForJob(jm.Db, job.ID)
	if err != nil {
		logger.Error("Failed to get mappings for health check (job %s): %v", job.ID, err)
		return
	}

	var topics []string
	for _, m := range mappings {
		if m.Enabled {
			topics = append(topics, m.SourceTopicPattern)
		}
	}

	if len(topics) == 0 {
		return
	}

	sourceConfig := config.ClusterConfig{
		Provider: sourceCluster.Provider,
		Brokers:  sourceCluster.Brokers,
		Security: config.SecurityConfig{
			APIKey:    sourceCluster.APIKey,
			APISecret: sourceCluster.APISecret,
		},
	}

	adminClient, err := kafka.NewAdminClient(sourceConfig)
	if err != nil {
		logger.Error("Failed to create admin client for health check (job %s): %v", job.ID, err)
		return
	}
	defer adminClient.Close()

	health, err := adminClient.CheckTopicHealth(context.Background(), topics)
	if err != nil {
		logger.Error("Failed to check topic health for job %s: %v", job.ID, err)
		return
	}

	for _, h := range health {
		if !h.IsHealthy {
			details := fmt.Sprintf("Topic %s is unhealthy. Under-replicated partitions: %d", h.Name, h.UnderReplicatedPartitions)
			event := &database.OperationalEvent{
				EventType: "topic_health_alert",
				Initiator: "system",
				Details:   details,
			}
			database.CreateOperationalEvent(jm.Db, event)
		}
	}
}

func (jm *JobManager) performDynamicThrottling(jobID, jobName string) {
	logger.InfoAI("ai", "throttling", jobID, "Starting dynamic throttling analysis for job %s", jobName)

	metricsQuery := `
		SELECT 
			messages_replicated_delta AS messages_replicated,
			bytes_transferred_delta AS bytes_transferred,
			avg_lag AS current_lag,
			error_count_delta AS error_count,
			timestamp
		FROM aggregated_metrics 
		WHERE job_id = ? AND timestamp > datetime('now', '-15 minutes')
		ORDER BY timestamp DESC LIMIT 20
	`

	var recentMetrics []database.ReplicationMetric
	err := jm.Db.Select(&recentMetrics, metricsQuery, jobID)
	if err != nil || len(recentMetrics) < 5 {
		logger.WarnAI("ai", "throttling", jobID, "Insufficient metrics for throttling analysis: %s (got %d metrics)", jobName, len(recentMetrics))
		return
	}

	job, err := database.GetJob(jm.Db, jobID)
	if err != nil {
		logger.ErrorAI("ai", "throttling", jobID, "Failed to get job for throttling: %v", err)
		return
	}

	throttlingDecision := jm.analyzeThrottlingMetrics(recentMetrics, job)
	if throttlingDecision == nil {
		logger.InfoAI("ai", "throttling", jobID, "No throttling adjustments needed for job %s", jobName)
		return
	}

	if jm.shouldApplyThrottling(jobID, throttlingDecision) {
		if err := jm.applyThrottlingChanges(jobID, jobName, job, throttlingDecision); err != nil {
			logger.ErrorAI("ai", "throttling", jobID, "Failed to apply throttling changes: %v", err)
		} else {
			logger.InfoAI("ai", "throttling", jobID, "Applied throttling changes to job %s: batch_size=%d, parallelism=%d (reason: %s)",
				jobName, throttlingDecision.NewBatchSize, throttlingDecision.NewParallelism, throttlingDecision.Reason)
		}
	} else {
		logger.InfoAI("ai", "throttling", jobID, "Throttling changes suppressed for job %s to prevent oscillation", jobName)
	}
}

type ThrottlingDecision struct {
	NewBatchSize   int
	NewParallelism int
	Reason         string
	Confidence     float64
}

func (jm *JobManager) analyzeThrottlingMetrics(metrics []database.ReplicationMetric, job *database.ReplicationJob) *ThrottlingDecision {
	if len(metrics) < 3 {
		return nil
	}

	var avgThroughput, avgLag float64
	var totalErrors, totalMessages int
	var maxLag int

	for _, m := range metrics {
		avgThroughput += float64(m.MessagesReplicated)
		avgLag += float64(m.CurrentLag)
		totalErrors += m.ErrorCount
		totalMessages += m.MessagesReplicated
		if m.CurrentLag > maxLag {
			maxLag = m.CurrentLag
		}
	}

	avgThroughput /= float64(len(metrics))
	avgLag /= float64(len(metrics))
	errorRate := float64(totalErrors) / float64(totalMessages) * 100

	currentBatchSize := job.BatchSize
	currentParallelism := job.Parallelism

	if avgLag > 5000 && errorRate < 1.0 {
		newBatchSize := jm.calculateOptimalBatchSize(currentBatchSize, avgThroughput, avgLag, "increase")
		newParallelism := jm.calculateOptimalParallelism(currentParallelism, avgThroughput, maxLag, "increase")

		if newBatchSize != currentBatchSize || newParallelism != currentParallelism {
			return &ThrottlingDecision{
				NewBatchSize:   newBatchSize,
				NewParallelism: newParallelism,
				Reason:         fmt.Sprintf("High lag detected (%.0f avg, %d max) - increasing throughput capacity", avgLag, maxLag),
				Confidence:     0.8,
			}
		}
	}

	if errorRate > 2.0 {
		newBatchSize := jm.calculateOptimalBatchSize(currentBatchSize, avgThroughput, avgLag, "decrease")
		newParallelism := jm.calculateOptimalParallelism(currentParallelism, avgThroughput, maxLag, "decrease")

		if newBatchSize != currentBatchSize || newParallelism != currentParallelism {
			return &ThrottlingDecision{
				NewBatchSize:   newBatchSize,
				NewParallelism: newParallelism,
				Reason:         fmt.Sprintf("High error rate detected (%.2f%%) - reducing load for stability", errorRate),
				Confidence:     0.9,
			}
		}
	}

	if avgThroughput < 100 && avgLag < 1000 && errorRate < 0.5 {
		newBatchSize := jm.calculateOptimalBatchSize(currentBatchSize, avgThroughput, avgLag, "optimize")

		if newBatchSize != currentBatchSize {
			return &ThrottlingDecision{
				NewBatchSize:   newBatchSize,
				NewParallelism: currentParallelism,
				Reason:         fmt.Sprintf("Low throughput (%.0f msg/s) optimization", avgThroughput),
				Confidence:     0.6,
			}
		}
	}

	return nil
}

func (jm *JobManager) calculateOptimalBatchSize(current int, throughput, lag float64, direction string) int {
	switch direction {
	case "increase":
		newSize := int(float64(current) * 1.5)
		if newSize > 10000 {
			newSize = 10000
		}
		if newSize <= current {
			newSize = current + 500
		}
		return newSize

	case "decrease":
		newSize := int(float64(current) * 0.7)
		if newSize < 100 {
			newSize = 100
		}
		if newSize >= current {
			newSize = current - 200
		}
		return newSize

	case "optimize":
		if throughput < 50 {
			return max(100, current/2)
		} else {
			return min(5000, current+300)
		}

	default:
		return current
	}
}

func (jm *JobManager) calculateOptimalParallelism(current int, throughput float64, maxLag int, direction string) int {
	switch direction {
	case "increase":
		newParallelism := current + 2
		if newParallelism > 20 {
			newParallelism = 20
		}
		return newParallelism

	case "decrease":
		newParallelism := current - 1
		if newParallelism < 1 {
			newParallelism = 1
		}
		return newParallelism

	default:
		return current
	}
}

func (jm *JobManager) shouldApplyThrottling(jobID string, decision *ThrottlingDecision) bool {
	lastThrottlingQuery := `
		SELECT timestamp FROM ai_insights 
		WHERE job_id = ? AND insight_type = 'throttling_adjustment' 
		AND timestamp > datetime('now', '-10 minutes')
		ORDER BY timestamp DESC LIMIT 1
	`

	var lastThrottling string
	err := jm.Db.Get(&lastThrottling, lastThrottlingQuery, jobID)
	if err == nil {
		return false
	}

	return decision.Confidence >= 0.7
}

func (jm *JobManager) applyThrottlingChanges(jobID, jobName string, job *database.ReplicationJob, decision *ThrottlingDecision) error {
	jm.Mu.Lock()
	defer jm.Mu.Unlock()

	kafMirror, isRunning := jm.KafMirrors[jobID]
	if !isRunning {
		return fmt.Errorf("job %s is not running, cannot apply throttling", jobID)
	}

	job.BatchSize = decision.NewBatchSize
	job.Parallelism = decision.NewParallelism

	if err := database.UpdateJob(jm.Db, job); err != nil {
		return fmt.Errorf("failed to update job configuration: %v", err)
	}

	logger.InfoAI("ai", "throttling", jobID, "Restarting job %s to apply throttling changes", jobName)

	kafMirror.Stop()
	delete(jm.KafMirrors, jobID)

	time.Sleep(2 * time.Second)

	if err := jm.startJobWithNewConfig(jobID, job); err != nil {
		return fmt.Errorf("failed to restart job with new config: %v", err)
	}

	insight := &database.AIInsight{
		JobID:            &jobID,
		InsightType:      "throttling_adjustment",
		Recommendation:   fmt.Sprintf("Applied dynamic throttling: batch_size=%d, parallelism=%d. %s", decision.NewBatchSize, decision.NewParallelism, decision.Reason),
		SeverityLevel:    "info",
		ResolutionStatus: "applied",
		AIModel:          "dynamic-throttling-system",
		AccuracyScore:    &decision.Confidence,
	}

	if err := database.InsertAIInsight(jm.Db, insight); err != nil {
		logger.ErrorAI("ai", "throttling", jobID, "Failed to log throttling insight: %v", err)
	}

	return nil
}

func (jm *JobManager) startJobWithNewConfig(jobID string, job *database.ReplicationJob) error {
	sourceCluster, err := database.GetCluster(jm.Db, job.SourceClusterName)
	if err != nil {
		return fmt.Errorf("failed to get source cluster: %v", err)
	}

	targetCluster, err := database.GetCluster(jm.Db, job.TargetClusterName)
	if err != nil {
		return fmt.Errorf("failed to get target cluster: %v", err)
	}

	mappings, err := database.GetMappingsForJob(jm.Db, jobID)
	if err != nil {
		return fmt.Errorf("failed to get mappings: %v", err)
	}

	jobConfig, err := jm.buildJobConfig(sourceCluster, targetCluster, mappings, job)
	if err != nil {
		return fmt.Errorf("failed to build job config: %v", err)
	}

	jobConfig.Replication.JobID = jobID
	kafMirror, err := jm.KafMirrorFactory(jobConfig)
	if err != nil {
		return fmt.Errorf("failed to create KafMirror: %v", err)
	}

	kafMirror.Start(jobID, jm.ProcessMetrics, jm.handleJobPanic)
	jm.KafMirrors[jobID] = kafMirror

	job.Status = "active"
	return database.UpdateJob(jm.Db, job)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func (jm *JobManager) healthCheckCluster(cluster *database.KafkaCluster, clusterType string) error {
	clusterConfig := config.ClusterConfig{
		Provider: cluster.Provider,
		Brokers:  cluster.Brokers,
		Security: config.SecurityConfig{
			APIKey:    cluster.APIKey,
			APISecret: cluster.APISecret,
		},
	}

	adminClient, err := kafka.NewAdminClient(clusterConfig)
	if err != nil {
		return fmt.Errorf("failed to create admin client for %s cluster %s: %v", clusterType, cluster.Name, err)
	}
	defer adminClient.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	clusterInfo, err := adminClient.GetClusterInfo(ctx)
	if err != nil {
		return fmt.Errorf("%s cluster %s is unreachable: %v", clusterType, cluster.Name, err)
	}

	if clusterInfo.BrokerCount == 0 {
		return fmt.Errorf("%s cluster %s has no active brokers", clusterType, cluster.Name)
	}

	logger.Info("%s cluster %s health check passed: %d brokers, %d topics",
		clusterType, cluster.Name, clusterInfo.BrokerCount, len(clusterInfo.Topics))
	return nil
}

func (jm *JobManager) validateMirrorState(jobID string, sourceCluster, targetCluster *database.KafkaCluster) error {
	mappings, err := database.GetMappingsForJob(jm.Db, jobID)
	if err != nil {
		return fmt.Errorf("failed to get mappings: %v", err)
	}

	if len(mappings) == 0 {
		return fmt.Errorf("no topic mappings configured for job")
	}

	topicMap := make(map[string]string)
	var enabledTopics []string
	for _, mapping := range mappings {
		if mapping.Enabled {
			topicMap[mapping.SourceTopicPattern] = mapping.TargetTopicPattern
			enabledTopics = append(enabledTopics, mapping.SourceTopicPattern)
		}
	}

	if len(enabledTopics) == 0 {
		return fmt.Errorf("no enabled topic mappings found")
	}

	sourceConfig := config.ClusterConfig{
		Provider: sourceCluster.Provider,
		Brokers:  sourceCluster.Brokers,
		Security: config.SecurityConfig{
			APIKey:    sourceCluster.APIKey,
			APISecret: sourceCluster.APISecret,
		},
	}

	targetConfig := config.ClusterConfig{
		Provider: targetCluster.Provider,
		Brokers:  targetCluster.Brokers,
		Security: config.SecurityConfig{
			APIKey:    targetCluster.APIKey,
			APISecret: targetCluster.APISecret,
		},
	}

	sourceAdmin, err := kafka.NewAdminClient(sourceConfig)
	if err != nil {
		return fmt.Errorf("failed to create source admin client: %v", err)
	}
	defer sourceAdmin.Close()

	targetAdmin, err := kafka.NewAdminClient(targetConfig)
	if err != nil {
		return fmt.Errorf("failed to create target admin client: %v", err)
	}
	defer targetAdmin.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	sourceTopicHealth, err := sourceAdmin.CheckTopicHealth(ctx, enabledTopics)
	if err != nil {
		return fmt.Errorf("failed to check source topics health: %v", err)
	}

	for _, health := range sourceTopicHealth {
		if !health.IsHealthy {
			return fmt.Errorf("source topic %s is unhealthy: %d under-replicated partitions",
				health.Name, health.UnderReplicatedPartitions)
		}
	}

	existingProgress, err := database.GetMirrorProgress(jm.Db, jobID)
	if err == nil && len(existingProgress) > 0 {
		logger.Info("Found existing mirror progress for job %s - validating for safe restart", jobID)

		consumerGroup := fmt.Sprintf("kaf-mirror-job-%s", jobID)
		mirrorAnalysis, err := targetAdmin.AnalyzeMirrorState(ctx, sourceAdmin, jobID, topicMap, consumerGroup)
		if err != nil {
			logger.Warn("Mirror state analysis failed, proceeding with caution: %v", err)
		} else if mirrorAnalysis.OffsetComparison != nil && len(mirrorAnalysis.OffsetComparison.CriticalIssues) > 0 {
			return fmt.Errorf("critical mirror state issues detected: %v", mirrorAnalysis.OffsetComparison.CriticalIssues)
		}
	}

	logger.Info("Mirror state validation passed for job %s: %d topics validated", jobID, len(enabledTopics))
	return nil
}

// GetJobTopicHealth retrieves the health of all topics for a given job.
func (jm *JobManager) GetJobTopicHealth(jobID string) ([]kafka.TopicHealth, error) {
	job, err := database.GetJob(jm.Db, jobID)
	if err != nil {
		return nil, fmt.Errorf("failed to get job %s: %v", jobID, err)
	}

	sourceCluster, err := database.GetCluster(jm.Db, job.SourceClusterName)
	if err != nil {
		return nil, fmt.Errorf("failed to get source cluster %s: %v", job.SourceClusterName, err)
	}

	mappings, err := database.GetMappingsForJob(jm.Db, jobID)
	if err != nil {
		return nil, fmt.Errorf("failed to get mappings for job %s: %v", jobID, err)
	}

	var topics []string
	for _, m := range mappings {
		if m.Enabled {
			topics = append(topics, m.SourceTopicPattern)
		}
	}

	if len(topics) == 0 {
		return []kafka.TopicHealth{}, nil
	}

	sourceConfig := config.ClusterConfig{
		Provider: sourceCluster.Provider,
		Brokers:  sourceCluster.Brokers,
		Security: config.SecurityConfig{
			APIKey:    sourceCluster.APIKey,
			APISecret: sourceCluster.APISecret,
		},
	}

	adminClient, err := kafka.NewAdminClient(sourceConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create admin client for health check (job %s): %v", job.ID, err)
	}
	defer adminClient.Close()

	health, err := adminClient.CheckTopicHealth(context.Background(), topics)
	if err != nil {
		return nil, fmt.Errorf("failed to check topic health for job %s: %v", job.ID, err)
	}

	return health, nil
}

func (jm *JobManager) startMirrorStateUpdates() {
	defer jm.wg.Done()
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			jm.updateAllMirrorStates()
		case <-jm.close:
			return
		}
	}
}

func (jm *JobManager) updateAllMirrorStates() {
	jobs, err := database.ListJobs(jm.Db)
	if err != nil {
		logger.Error("Failed to get jobs for mirror state update: %v", err)
		return
	}

	for _, job := range jobs {
		if job.Status == "active" {
			go jm.updateMirrorState(job.ID)
		}
	}
}

func (jm *JobManager) updateMirrorState(jobID string) {
	logger.Info("Updating comprehensive mirror state for job %s", jobID)

	job, err := database.GetJob(jm.Db, jobID)
	if err != nil {
		logger.Error("Failed to get job %s for mirror state update: %v", jobID, err)
		return
	}

	sourceCluster, err := database.GetCluster(jm.Db, job.SourceClusterName)
	if err != nil {
		logger.Error("Failed to get source cluster %s for mirror state update: %v", job.SourceClusterName, err)
		return
	}

	targetCluster, err := database.GetCluster(jm.Db, job.TargetClusterName)
	if err != nil {
		logger.Error("Failed to get target cluster %s for mirror state update: %v", job.TargetClusterName, err)
		return
	}

	mappings, err := database.GetMappingsForJob(jm.Db, jobID)
	if err != nil {
		logger.Error("Failed to get mappings for job %s for mirror state update: %v", jobID, err)
		return
	}

	topicMap := make(map[string]string)
	for _, mapping := range mappings {
		if mapping.Enabled {
			topicMap[mapping.SourceTopicPattern] = mapping.TargetTopicPattern
		}
	}

	if len(topicMap) == 0 {
		logger.Warn("No enabled topics found for job %s, skipping mirror state update", jobID)
		return
	}

	sourceConfig := config.ClusterConfig{
		Provider: sourceCluster.Provider,
		Brokers:  sourceCluster.Brokers,
		Security: config.SecurityConfig{
			APIKey:    sourceCluster.APIKey,
			APISecret: sourceCluster.APISecret,
		},
	}

	targetConfig := config.ClusterConfig{
		Provider: targetCluster.Provider,
		Brokers:  targetCluster.Brokers,
		Security: config.SecurityConfig{
			APIKey:    targetCluster.APIKey,
			APISecret: targetCluster.APISecret,
		},
	}

	sourceAdmin, err := kafka.NewAdminClient(sourceConfig)
	if err != nil {
		logger.Error("Failed to create source admin client for mirror state update: %v", err)
		return
	}
	defer sourceAdmin.Close()

	targetAdmin, err := kafka.NewAdminClient(targetConfig)
	if err != nil {
		logger.Error("Failed to create target admin client for mirror state update: %v", err)
		return
	}
	defer targetAdmin.Close()

	ctx := context.Background()
	now := time.Now()

	// Perform comprehensive cross-cluster analysis similar to validate-mirror
	offsetComparison, err := targetAdmin.CompareClusterOffsets(ctx, sourceAdmin, topicMap)
	if err != nil {
		logger.Error("Failed to compare cluster offsets for job %s: %v", jobID, err)
		return
	}

	// Analyze mirror state with consumer group data
	consumerGroup := fmt.Sprintf("kaf-mirror-job-%s", jobID)
	mirrorAnalysis, err := targetAdmin.AnalyzeMirrorState(ctx, sourceAdmin, jobID, topicMap, consumerGroup)
	if err != nil {
		logger.Error("Failed to analyze mirror state for job %s: %v", jobID, err)
		return
	}

	// Update mirror progress with comprehensive data
	for topicName, topicComparison := range offsetComparison.TopicComparisons {
		for _, partitionComparison := range topicComparison.PartitionComparisons {
			status := "active"
			if partitionComparison.Gap == 0 {
				status = "completed"
			} else if partitionComparison.Gap > 10000 {
				status = "error"
			}

			progress := database.MirrorProgress{
				JobID:                jobID,
				SourceTopic:          topicComparison.SourceTopic,
				TargetTopic:          topicComparison.TargetTopic,
				PartitionID:          int(partitionComparison.PartitionID),
				SourceOffset:         partitionComparison.SourceOffset,
				TargetOffset:         partitionComparison.TargetOffset,
				SourceHighWaterMark:  partitionComparison.SourceHighWaterMark,
				TargetHighWaterMark:  partitionComparison.TargetHighWaterMark,
				LastReplicatedOffset: partitionComparison.TargetOffset,
				ReplicationLag:       partitionComparison.Gap,
				LastUpdated:          now,
				Status:               status,
			}

			if err := database.UpdateMirrorProgress(jm.Db, progress); err != nil {
				logger.Error("Failed to update mirror progress for %s-%d: %v", topicName, partitionComparison.PartitionID, err)
			}
		}
	}

	// Store resume points from mirror analysis
	if mirrorAnalysis.ResumePoints != nil {
		resumePointsMap := make(map[string]map[int32]int64)
		for topicName, partitionMap := range mirrorAnalysis.ResumePoints {
			resumePointsMap[topicName] = make(map[int32]int64)
			for partition, offset := range partitionMap {
				resumePointsMap[topicName][partition] = offset
			}
		}

		if err := database.CalculateResumePoints(jm.Db, jobID, resumePointsMap, nil); err != nil {
			logger.Error("Failed to store resume points for job %s: %v", jobID, err)
		}
	}

	// Detect and store mirror gaps
	var gaps []database.MirrorGap
	for _, topicComparison := range offsetComparison.TopicComparisons {
		for _, partitionComparison := range topicComparison.PartitionComparisons {
			if partitionComparison.HasGap && partitionComparison.Gap > 0 {
				gap := database.MirrorGap{
					JobID:            jobID,
					SourceTopic:      topicComparison.SourceTopic,
					TargetTopic:      topicComparison.TargetTopic,
					PartitionID:      int(partitionComparison.PartitionID),
					GapStartOffset:   partitionComparison.TargetOffset,
					GapEndOffset:     partitionComparison.SourceOffset,
					GapSize:          partitionComparison.Gap,
					DetectedAt:       now,
					GapType:          "offset_mismatch",
					ResolutionStatus: "unresolved",
				}
				gaps = append(gaps, gap)
			}
		}
	}

	if len(gaps) > 0 {
		if err := database.DetectMirrorGaps(jm.Db, jobID, gaps); err != nil {
			logger.Error("Failed to detect mirror gaps for job %s: %v", jobID, err)
		}
	}

	// Store comprehensive mirror state analysis
	analysisResults := fmt.Sprintf("Comprehensive mirror analysis for job %s: %d topics analyzed, %d gaps detected, %d critical issues",
		jobID, len(topicMap), offsetComparison.TotalGapsDetected, len(offsetComparison.CriticalIssues))

	recommendations := mirrorAnalysis.Recommendations
	if len(recommendations) == 0 {
		recommendations = []string{
			"Monitor replication lag regularly",
			"Consider increasing parallelism if lag exceeds 10000 messages",
			"Verify network connectivity between clusters",
		}
	}

	recommendationsJSON, _ := json.Marshal(recommendations)

	dbMirrorAnalysis := &database.MirrorStateAnalysis{
		JobID:               jobID,
		AnalysisType:        "gap_detection",
		SourceClusterState:  fmt.Sprintf("Source cluster: %s (%d topics)", sourceCluster.Name, len(topicMap)),
		TargetClusterState:  fmt.Sprintf("Target cluster: %s (%d topics)", targetCluster.Name, len(topicMap)),
		AnalysisResults:     analysisResults,
		Recommendations:     string(recommendationsJSON),
		CriticalIssuesCount: len(offsetComparison.CriticalIssues),
		WarningIssuesCount:  len(offsetComparison.Warnings),
		AnalyzedAt:          now,
		AnalyzerVersion:     "1.0",
	}

	if _, err := database.StoreMirrorStateAnalysis(jm.Db, *dbMirrorAnalysis); err != nil {
		logger.Error("Failed to store mirror state analysis for job %s: %v", jobID, err)
	}

	logger.Info("Successfully updated comprehensive mirror state for job %s: %d topics, %d gaps, %d critical issues",
		jobID, len(topicMap), offsetComparison.TotalGapsDetected, len(offsetComparison.CriticalIssues))
}
