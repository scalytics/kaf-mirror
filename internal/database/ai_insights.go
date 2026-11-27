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

package database

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/scalytics/kaf-mirror/v2/internal/analysis"
	"github.com/scalytics/kaf-mirror/v2/pkg/logger"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
)

// InsertAIInsight stores a new AI insight in the database with response time tracking.
func InsertAIInsight(db *sqlx.DB, insight *AIInsight) error {
	query := `INSERT INTO ai_insights (job_id, insight_type, severity_level, ai_model, recommendation, timestamp, resolution_status, response_time_ms)
			  VALUES (?, ?, ?, ?, ?, ?, ?, ?)`
	_, err := db.Exec(query, insight.JobID, insight.InsightType, insight.SeverityLevel, insight.AIModel, insight.Recommendation, time.Now(), "new", insight.ResponseTimeMs)
	return err
}

// GetAIMetrics calculates aggregated AI performance metrics.
func GetAIMetrics(db *sqlx.DB) (*AIMetrics, error) {
	var metrics AIMetrics

	// Get total insights count
	err := db.Get(&metrics.TotalInsights, "SELECT COUNT(*) FROM ai_insights")
	if err != nil {
		return nil, err
	}

	// Get average response time (only for records with response_time_ms > 0)
	err = db.Get(&metrics.AvgResponseTimeMs,
		"SELECT COALESCE(AVG(response_time_ms), 0) FROM ai_insights WHERE response_time_ms > 0")
	if err != nil {
		return nil, err
	}

	// Calculate accuracy rate based on resolved insights with positive feedback
	var resolvedCount, accurateCount int
	err = db.Get(&resolvedCount, "SELECT COUNT(*) FROM ai_insights WHERE resolution_status = 'resolved'")
	if err != nil {
		return nil, err
	}

	if resolvedCount > 0 {
		err = db.Get(&accurateCount,
			"SELECT COUNT(*) FROM ai_insights WHERE resolution_status = 'resolved' AND (accuracy_score >= 0.7 OR user_feedback LIKE '%helpful%' OR user_feedback LIKE '%accurate%')")
		if err != nil {
			return nil, err
		}
		metrics.AccuracyRate = float64(accurateCount) / float64(resolvedCount) * 100
	}

	// Get anomaly and recommendation counts
	err = db.Get(&metrics.AnomalyCount, "SELECT COUNT(*) FROM ai_insights WHERE insight_type = 'anomaly'")
	if err != nil {
		return nil, err
	}

	err = db.Get(&metrics.RecommendationCount,
		"SELECT COUNT(*) FROM ai_insights WHERE insight_type IN ('optimization', 'recommendation')")
	if err != nil {
		return nil, err
	}

	return &metrics, nil
}

// GenerateEnhancedAIInsight creates AI insights using both metrics and operation logs.
func GenerateEnhancedAIInsight(db *sqlx.DB, aiClient interface{}, jobID string, insightType string) error {
	// Get recent metrics (last 30 minutes)
	metrics, err := GetAggregatedMetricsForAI(db, jobID, 30)
	if err != nil {
		return fmt.Errorf("failed to get metrics for AI analysis: %v", err)
	}

	// Get recent operation logs (last 30 minutes)
	logEntries, err := analysis.GetLogsForJob(logger.GetProductionLogDir(), jobID, time.Now().Add(-30*time.Minute))
	if err != nil {
		return fmt.Errorf("failed to get logs for AI analysis: %v", err)
	}
	logs, err := formatLogsForAI(logEntries)
	if err != nil {
		return fmt.Errorf("failed to format logs for AI analysis: %v", err)
	}

	// Type assert the AI client for different analysis types with response time tracking
	type enhancedAIClient interface {
		GetEnhancedInsightsWithResponseTime(ctx context.Context, metrics string, logs string) (string, int, error)
		GetLogPatternAnalysisWithResponseTime(ctx context.Context, logs string) (string, int, error)
		GetAnomalyDetectionWithResponseTime(ctx context.Context, metrics string) (string, int, error)
		// Fallback methods without response time for compatibility
		GetEnhancedInsights(ctx context.Context, metrics string, logs string) (string, error)
		GetLogPatternAnalysis(ctx context.Context, logs string) (string, error)
		GetAnomalyDetection(ctx context.Context, metrics string) (string, error)
	}

	type incidentAIClient interface {
		GetIncidentAnalysisWithResponseTime(ctx context.Context, eventDetails string) (string, int, error)
		GetIncidentAnalysis(ctx context.Context, eventDetails string) (string, error)
	}

	type perfAIClient interface {
		GetPerformanceRecommendationWithResponseTime(ctx context.Context, metrics string) (string, int, error)
		GetPerformanceRecommendation(ctx context.Context, metrics string) (string, error)
	}

	ctx := context.Background()
	var recommendation string
	var severityLevel string
	var responseTimeMs int

	switch insightType {
	case "enhanced_analysis":
		client, ok := aiClient.(enhancedAIClient)
		if !ok {
			return fmt.Errorf("AI client does not support enhanced insights")
		}
		recommendation, responseTimeMs, err = client.GetEnhancedInsightsWithResponseTime(ctx, metrics, logs)
		severityLevel = "medium"
	case "log_analysis":
		client, ok := aiClient.(enhancedAIClient)
		if !ok {
			return fmt.Errorf("AI client does not support log analysis")
		}
		recommendation, responseTimeMs, err = client.GetLogPatternAnalysisWithResponseTime(ctx, logs)
		severityLevel = determineSeverityFromLogs(logs)
	case "anomaly":
		client, ok := aiClient.(enhancedAIClient)
		if !ok {
			return fmt.Errorf("AI client does not support anomaly detection")
		}
		recommendation, responseTimeMs, err = client.GetAnomalyDetectionWithResponseTime(ctx, metrics)
		severityLevel = determineSeverityFromMetrics(metrics)
	case "recommendation":
		client, ok := aiClient.(perfAIClient)
		if !ok {
			return fmt.Errorf("AI client does not support performance recommendations")
		}
		recommendation, responseTimeMs, err = client.GetPerformanceRecommendationWithResponseTime(ctx, metrics)
		severityLevel = "low"
	case "optimization":
		client, ok := aiClient.(perfAIClient)
		if !ok {
			return fmt.Errorf("AI client does not support performance optimization")
		}
		recommendation, responseTimeMs, err = client.GetPerformanceRecommendationWithResponseTime(ctx, metrics)
		severityLevel = "medium"
	case "incident_report":
		client, ok := aiClient.(incidentAIClient)
		if !ok {
			return fmt.Errorf("AI client does not support incident analysis")
		}
		eventDetails := fmt.Sprintf("INCIDENT EVENT\nMetrics: %s\nOperation Logs: %s", metrics, logs)
		recommendation, responseTimeMs, err = client.GetIncidentAnalysisWithResponseTime(ctx, eventDetails)
		severityLevel = "high"
	case "incident_analysis":
		client, ok := aiClient.(incidentAIClient)
		if !ok {
			return fmt.Errorf("AI client does not support incident analysis")
		}
		eventDetails := fmt.Sprintf("INCIDENT ANALYSIS\nMetrics: %s\nOperation Logs: %s", metrics, logs)
		recommendation, responseTimeMs, err = client.GetIncidentAnalysisWithResponseTime(ctx, eventDetails)
		severityLevel = "high"
	default:
		return fmt.Errorf("unsupported insight type: %s", insightType)
	}

	if err != nil {
		return fmt.Errorf("failed to generate AI insight: %v", err)
	}

	// Store the insight with response time
	insight := &AIInsight{
		JobID:          &jobID,
		InsightType:    insightType,
		SeverityLevel:  severityLevel,
		AIModel:        "enhanced-analysis",
		Recommendation: recommendation,
		ResponseTimeMs: responseTimeMs,
	}

	return InsertAIInsight(db, insight)
}

func formatLogsForAI(entries []analysis.LogEntry) (string, error) {
	if len(entries) == 0 {
		return "No recent operation logs found for analysis.", nil
	}

	// Format logs for AI analysis
	type LogSummary struct {
		ProducerLogs []analysis.LogEntry `json:"producer_logs"`
		ConsumerLogs []analysis.LogEntry `json:"consumer_logs"`
		ErrorCount   int                 `json:"error_count"`
		WarnCount    int                 `json:"warn_count"`
	}

	summary := LogSummary{
		ProducerLogs: make([]analysis.LogEntry, 0),
		ConsumerLogs: make([]analysis.LogEntry, 0),
	}

	for _, entry := range entries {
		if entry.Component == "producer" {
			summary.ProducerLogs = append(summary.ProducerLogs, entry)
		} else {
			summary.ConsumerLogs = append(summary.ConsumerLogs, entry)
		}
		if entry.Level == "ERROR" {
			summary.ErrorCount++
		} else if entry.Level == "WARN" {
			summary.WarnCount++
		}
	}

	// Limit to most recent entries to avoid token overflow
	if len(summary.ProducerLogs) > 50 {
		summary.ProducerLogs = summary.ProducerLogs[:50]
	}
	if len(summary.ConsumerLogs) > 50 {
		summary.ConsumerLogs = summary.ConsumerLogs[:50]
	}

	summaryBytes, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		return "", err
	}

	return string(summaryBytes), nil
}

// determineSeverityFromLogs analyzes log content to determine severity level
func determineSeverityFromLogs(logs string) string {
	if strings.Contains(logs, `"error_count":`) && !strings.Contains(logs, `"error_count": 0`) {
		errorCount := extractErrorCount(logs)
		if errorCount > 10 {
			return "high"
		} else if errorCount > 0 {
			return "medium"
		}
	}
	return "low"
}

// determineSeverityFromMetrics analyzes metrics to determine severity level
func determineSeverityFromMetrics(metrics string) string {
	if strings.Contains(metrics, "error_count") || strings.Contains(metrics, "anomaly") {
		return "high"
	} else if strings.Contains(metrics, "warning") || strings.Contains(metrics, "elevated") {
		return "medium"
	}
	return "low"
}

// extractErrorCount extracts error count from log JSON (simple implementation)
func extractErrorCount(logs string) int {
	// Simple regex or string parsing could be used here
	// For now, return a basic heuristic based on error mentions
	errorMentions := strings.Count(logs, "ERROR")
	return errorMentions
}

// GetAggregatedMetricsForAI retrieves and formats metrics data for AI analysis
func GetAggregatedMetricsForAI(db *sqlx.DB, jobID string, minutes int) (string, error) {
	since := time.Now().Add(-time.Duration(minutes) * time.Minute)

	query := `SELECT 
				messages_replicated_delta AS messages_replicated,
				bytes_transferred_delta AS bytes_transferred,
				avg_lag AS current_lag,
				error_count_delta AS error_count,
				timestamp
			  FROM aggregated_metrics 
			  WHERE job_id = ? AND timestamp >= ? 
			  ORDER BY timestamp DESC LIMIT 100`

	var metrics []ReplicationMetric
	err := db.Select(&metrics, query, jobID, since)
	if err != nil {
		return "", err
	}

	if len(metrics) == 0 {
		return "No recent metrics found for analysis.", nil
	}

	// Format metrics for AI analysis
	type MetricsSummary struct {
		JobID           string  `json:"job_id"`
		TimeWindow      string  `json:"time_window"`
		TotalMessages   int64   `json:"total_messages"`
		TotalBytes      int64   `json:"total_bytes"`
		AvgThroughput   float64 `json:"avg_throughput_msg_per_sec"`
		MaxLag          int     `json:"max_lag"`
		TotalErrors     int64   `json:"total_errors"`
		DataPoints      int     `json:"data_points"`
		LatestTimestamp string  `json:"latest_timestamp"`
	}

	var totalMessages, totalBytes, totalErrors int64
	var maxLag int

	for _, metric := range metrics {
		totalMessages += int64(metric.MessagesReplicated)
		totalBytes += int64(metric.BytesTransferred)
		totalErrors += int64(metric.ErrorCount)
		if metric.CurrentLag > maxLag {
			maxLag = metric.CurrentLag
		}
	}

	// Calculate average throughput (messages per second over time window)
	timeWindowSeconds := float64(minutes * 60)
	avgThroughput := float64(totalMessages) / timeWindowSeconds

	summary := MetricsSummary{
		JobID:           jobID,
		TimeWindow:      fmt.Sprintf("%d minutes", minutes),
		TotalMessages:   totalMessages,
		TotalBytes:      totalBytes,
		AvgThroughput:   avgThroughput,
		MaxLag:          maxLag,
		TotalErrors:     totalErrors,
		DataPoints:      len(metrics),
		LatestTimestamp: metrics[0].Timestamp.Format("2006-01-02 15:04:05"),
	}

	summaryBytes, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		return "", err
	}

	return string(summaryBytes), nil
}

// ListAIInsights retrieves AI insights from the database, with optional filters.
func ListAIInsights(db *sqlx.DB, insightType string, jobID *string, limit int) ([]AIInsight, error) {
	var insights []AIInsight
	query := "SELECT * FROM ai_insights"
	var conditions []string
	var args []interface{}

	if insightType != "" {
		conditions = append(conditions, "insight_type = ?")
		args = append(args, insightType)
	}

	if jobID != nil {
		conditions = append(conditions, "job_id = ?")
		args = append(args, *jobID)
	}

	if len(conditions) > 0 {
		query += " WHERE " + conditions[0]
		for i := 1; i < len(conditions); i++ {
			query += " AND " + conditions[i]
		}
	}

	query += " ORDER BY timestamp DESC"

	if limit > 0 {
		query += " LIMIT ?"
		args = append(args, limit)
	}

	err := db.Select(&insights, query, args...)
	return insights, err
}
