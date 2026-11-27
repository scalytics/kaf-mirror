package widgets

import (
	"fmt"
	"github.com/scalytics/kaf-mirror/v2/cmd/mirror-cli/dashboard/core"
	"strings"
)

type JobsFactory struct {
	*BaseWidgetFactory
}

func NewJobsFactory() *JobsFactory {
	base := NewBaseWidgetFactory()

	jf := &JobsFactory{
		BaseWidgetFactory: base,
	}

	jf.listWidget.Title = "Jobs (Press Enter for details, S/T/P/X for start/stop/pause/restart)"
	jf.detailWidget.Title = "Job Details"

	return jf
}

func (jf *JobsFactory) UpdateListData(dataManager *core.DataManager) error {
	jobs, err := dataManager.GetJobs()
	if err != nil {
		jf.listWidget.Rows = []string{
			"ID | Name | Status | Source→Target | Lag | Msgs/sec",
			fmt.Sprintf("Error fetching jobs: %v", err),
		}
		return err
	}

	jf.items = jobs

	rows := []string{
		"ID | Name | Status | Source→Target | Lag | Msgs/sec",
	}

	if len(jobs) == 0 {
		rows = append(rows, "No replication jobs found")
	} else {
		for _, job := range jobs {
			jobID := SafeString(job["id"], "unknown")
			jobName := SafeString(job["name"], "unnamed")
			jobStatus := SafeString(job["status"], "unknown")
			sourceCluster := SafeString(job["source_cluster_name"], "N/A")
			targetCluster := SafeString(job["target_cluster_name"], "N/A")

			var lag, throughput string
			metrics, err := dataManager.GetJobMetrics(jobID)
			if err != nil {
				lag = "N/A"
				throughput = "N/A"
			} else {
				lag = fmt.Sprintf("%.0f", SafeFloat(metrics["current_lag"], 0))
				throughput = fmt.Sprintf("%.0f", SafeFloat(metrics["messages_replicated"], 0))
			}

			jobName = TruncateString(jobName, 15)
			sourceTarget := fmt.Sprintf("%s→%s",
				TruncateString(sourceCluster, 8),
				TruncateString(targetCluster, 8))

			row := fmt.Sprintf("%s | %s | %s | %s | %s | %s",
				TruncateString(jobID, 8),
				jobName,
				FormatStatus(jobStatus),
				sourceTarget,
				lag,
				throughput,
			)

			rows = append(rows, row)
		}
	}

	jf.listWidget.Rows = rows

	// Maintain selection within bounds
	if jf.selectedIdx >= len(jf.items) {
		jf.selectedIdx = len(jf.items) - 1
	}
	if jf.selectedIdx < 0 && len(jf.items) > 0 {
		jf.selectedIdx = 0
	}
	if len(jf.items) > 0 {
		jf.listWidget.SelectedRow = jf.selectedIdx + 1 // +1 for header
	}

	return nil
}

func (jf *JobsFactory) UpdateDetailData(dataManager *core.DataManager, itemID string) error {
	if itemID == "" {
		jf.detailWidget.Rows = []string{"Select a job from the list to view details"}
		return nil
	}

	job, err := dataManager.GetJobDetails(itemID)
	if err != nil {
		jf.detailWidget.Rows = []string{
			"Error fetching job details:",
			fmt.Sprintf("%v", err),
		}
		return err
	}

	metrics, metricsErr := dataManager.GetJobMetrics(itemID)
	mappings, mappingsErr := dataManager.GetJobMappings(itemID)

	var rows []string

	rows = append(rows, "=== JOB INFORMATION ===")
	rows = append(rows, fmt.Sprintf("ID: %s", SafeString(job["id"], "N/A")))
	rows = append(rows, fmt.Sprintf("Name: %s", SafeString(job["name"], "N/A")))
	rows = append(rows, fmt.Sprintf("Status: %s", FormatStatus(SafeString(job["status"], "N/A"))))
	rows = append(rows, fmt.Sprintf("Source Cluster: %s", SafeString(job["source_cluster_name"], "N/A")))
	rows = append(rows, fmt.Sprintf("Target Cluster: %s", SafeString(job["target_cluster_name"], "N/A")))
	rows = append(rows, fmt.Sprintf("Created: %s", SafeString(job["created_at"], "N/A")))
	rows = append(rows, fmt.Sprintf("Updated: %s", SafeString(job["updated_at"], "N/A")))

	rows = append(rows, "")
	rows = append(rows, "=== CURRENT METRICS ===")

	if metricsErr != nil {
		rows = append(rows, fmt.Sprintf("Metrics Error: %v", metricsErr))
	} else if metrics != nil {
		currentLag := SafeFloat(metrics["current_lag"], 0)
		throughput := SafeFloat(metrics["messages_replicated"], 0)
		totalProcessed := SafeFloat(metrics["total_messages_processed"], 0)
		errorCount := SafeFloat(metrics["error_count"], 0)

		rows = append(rows, fmt.Sprintf("Current Lag: %.0f messages", currentLag))
		rows = append(rows, fmt.Sprintf("Messages/sec: %.0f", throughput))
		rows = append(rows, fmt.Sprintf("Total Processed: %.0f", totalProcessed))
		rows = append(rows, fmt.Sprintf("Errors: %.0f", errorCount))
		rows = append(rows, fmt.Sprintf("Last Updated: %s", SafeString(metrics["timestamp"], "N/A")))

		// Health assessment based on error count and lag thresholds
		var healthStatus string
		if errorCount > 0 {
			healthStatus = "⚠️ ERRORS DETECTED - May need intervention"
		} else if currentLag > 10000 {
			healthStatus = "⚠️ HIGH LAG - Monitoring recommended"
		} else {
			healthStatus = "✅ HEALTHY - Operating normally"
		}
		rows = append(rows, fmt.Sprintf("Health Status: %s", healthStatus))
	} else {
		rows = append(rows, "No metrics available")
	}

	rows = append(rows, "")
	rows = append(rows, "=== TOPIC MAPPINGS ===")

	if mappingsErr != nil {
		rows = append(rows, fmt.Sprintf("Mappings Error: %v", mappingsErr))
	} else if len(mappings) == 0 {
		rows = append(rows, "No topic mappings configured")
	} else {
		for i, mapping := range mappings {
			prefix := fmt.Sprintf("Mapping %d:", i+1)
			rows = append(rows, fmt.Sprintf("%s Source: %s", prefix, SafeString(mapping["source_topic_pattern"], "N/A")))
			rows = append(rows, fmt.Sprintf("%s Target: %s", strings.Repeat(" ", len(prefix)-7), SafeString(mapping["target_topic_pattern"], "N/A")))
			rows = append(rows, fmt.Sprintf("%s Enabled: %v", strings.Repeat(" ", len(prefix)-7), mapping["enabled"]))
			if i < len(mappings)-1 {
				rows = append(rows, "")
			}
		}
	}

	rows = append(rows, "")
	rows = append(rows, "=== REPLICATION SETTINGS ===")
	if batchSize := job["batch_size"]; batchSize != nil {
		rows = append(rows, fmt.Sprintf("Batch Size: %.0f KB", SafeFloat(batchSize, 0)))
	}
	if compression := job["compression"]; compression != nil {
		rows = append(rows, fmt.Sprintf("Compression: %s", SafeString(compression, "N/A")))
	}

	rows = append(rows, "")
	rows = append(rows, "=== CONTROLS ===")
	rows = append(rows, "Press 'S' to start job")
	rows = append(rows, "Press 'T' to stop job")
	rows = append(rows, "Press 'P' to pause job")
	rows = append(rows, "Press 'X' to restart job")
	rows = append(rows, "Press 'R' to refresh data")
	rows = append(rows, "Press 'B' or Escape to go back")

	jf.detailWidget.Rows = rows
	jf.detailWidget.Title = fmt.Sprintf("Job Details: %s", SafeString(job["name"], itemID))

	return nil
}

func (jf *JobsFactory) HandleAction(action core.EventAction, dataManager *core.DataManager) error {
	jobAction, ok := action.(core.JobControlAction)
	if !ok {
		return nil // Not a job control action
	}

	jobID := jf.GetSelectedItemID()
	if jobID == "" {
		return fmt.Errorf("no job selected")
	}

	// This would typically call job control functions
	// For now, we'll simulate the action
	switch jobAction.Action {
	case "start":
		// Call startJob(dataManager.token, jobID)
		jf.listWidget.Title = fmt.Sprintf("Jobs (Starting job %s...)", jobID)
	case "stop":
		// Call stopJob(dataManager.token, jobID)
		jf.listWidget.Title = fmt.Sprintf("Jobs (Stopping job %s...)", jobID)
	case "pause":
		// Call pauseJob(dataManager.token, jobID)
		jf.listWidget.Title = fmt.Sprintf("Jobs (Pausing job %s...)", jobID)
	case "restart":
		// Call restartJob(dataManager.token, jobID)
		jf.listWidget.Title = fmt.Sprintf("Jobs (Restarting job %s...)", jobID)
	}

	// Invalidate cache to force refresh of job data
	dataManager.InvalidateCache("jobs")
	dataManager.InvalidateCache("metrics_" + jobID)
	dataManager.InvalidateCache("job_details_" + jobID)

	return nil
}

func GetJobControlFunctions() map[string]interface{} {
	return map[string]interface{}{
		"startJob":   nil, // These will be injected from main.go
		"stopJob":    nil,
		"pauseJob":   nil,
		"restartJob": nil,
	}
}
