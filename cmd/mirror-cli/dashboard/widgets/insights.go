package widgets

import (
	"fmt"
	"github.com/scalytics/kaf-mirror/v2/cmd/mirror-cli/dashboard/core"
)

type InsightsFactory struct {
	*BaseWidgetFactory
}

func NewInsightsFactory() *InsightsFactory {
	base := NewBaseWidgetFactory()

	inf := &InsightsFactory{
		BaseWidgetFactory: base,
	}

	inf.listWidget.Title = "AI Insights (Press Enter for details, R to refresh)"
	inf.detailWidget.Title = "Insight Details"

	return inf
}

func (inf *InsightsFactory) UpdateListData(dataManager *core.DataManager) error {
	insights, err := dataManager.GetAIInsights()
	if err != nil {
		inf.listWidget.Rows = []string{
			"Type | Timestamp | Severity | Job ID | Status",
			fmt.Sprintf("Error fetching AI insights: %v", err),
		}
		return err
	}

	inf.items = insights

	rows := []string{
		"Type | Timestamp | Severity | Job ID | Status",
	}

	if len(insights) == 0 {
		rows = append(rows, "No AI insights available")
	} else {
		for _, insight := range insights {
			insightType := SafeString(insight["insight_type"], "unknown")
			timestamp := SafeString(insight["timestamp"], "N/A")
			severity := SafeString(insight["severity_level"], "normal")
			jobID := SafeString(insight["job_id"], "N/A")
			status := SafeString(insight["resolution_status"], "pending")

			row := fmt.Sprintf("%s | %s | %s | %s | %s",
				TruncateString(insightType, 12),
				TruncateString(timestamp, 16),
				TruncateString(severity, 8),
				TruncateString(jobID, 12),
				FormatStatus(status),
			)

			rows = append(rows, row)
		}
	}

	inf.listWidget.Rows = rows

	if inf.selectedIdx >= len(inf.items) {
		inf.selectedIdx = len(inf.items) - 1
	}
	if inf.selectedIdx < 0 && len(inf.items) > 0 {
		inf.selectedIdx = 0
	}
	if len(inf.items) > 0 {
		inf.listWidget.SelectedRow = inf.selectedIdx + 1
	}

	return nil
}

func (inf *InsightsFactory) UpdateDetailData(dataManager *core.DataManager, itemID string) error {
	if itemID == "" {
		inf.detailWidget.Rows = []string{"Select an AI insight to view details"}
		return nil
	}

	insights, err := dataManager.GetAIInsights()
	if err != nil {
		inf.detailWidget.Rows = []string{
			"Error fetching insight details:",
			fmt.Sprintf("%v", err),
		}
		return err
	}

	var selectedInsight map[string]interface{}
	for _, insight := range insights {
		insightID := fmt.Sprintf("%v", insight["id"])
		if insightID == itemID {
			selectedInsight = insight
			break
		}
	}

	if selectedInsight == nil {
		inf.detailWidget.Rows = []string{"Insight not found"}
		return nil
	}

	var rows []string

	rows = append(rows, "=== AI INSIGHT DETAILS ===")
	rows = append(rows, fmt.Sprintf("ID: %v", selectedInsight["id"]))
	rows = append(rows, fmt.Sprintf("Type: %s", SafeString(selectedInsight["insight_type"], "N/A")))
	rows = append(rows, fmt.Sprintf("Severity: %s", SafeString(selectedInsight["severity_level"], "N/A")))
	rows = append(rows, fmt.Sprintf("Status: %s", FormatStatus(SafeString(selectedInsight["resolution_status"], "N/A"))))
	rows = append(rows, fmt.Sprintf("Job ID: %s", SafeString(selectedInsight["job_id"], "N/A")))
	rows = append(rows, fmt.Sprintf("Created: %s", SafeString(selectedInsight["timestamp"], "N/A")))

	if resolvedAt := SafeString(selectedInsight["resolved_at"], ""); resolvedAt != "" {
		rows = append(rows, fmt.Sprintf("Resolved: %s", resolvedAt))
	}

	rows = append(rows, "")
	rows = append(rows, "=== AI MODEL & PERFORMANCE ===")
	rows = append(rows, fmt.Sprintf("AI Model: %s", SafeString(selectedInsight["ai_model"], "N/A")))
	rows = append(rows, fmt.Sprintf("Response Time: %v ms", selectedInsight["response_time_ms"]))
	rows = append(rows, fmt.Sprintf("Accuracy Score: %.2f", SafeFloat(selectedInsight["accuracy_score"], 0.0)))

	rows = append(rows, "")
	rows = append(rows, "=== RECOMMENDATION ===")

	recommendation := SafeString(selectedInsight["recommendation"], "No recommendations available")
	for len(recommendation) > 0 {
		if len(recommendation) <= 70 {
			rows = append(rows, recommendation)
			break
		}
		breakPoint := 70
		for breakPoint > 50 && recommendation[breakPoint] != ' ' {
			breakPoint--
		}
		if breakPoint <= 50 {
			breakPoint = 70
		}
		rows = append(rows, recommendation[:breakPoint])
		recommendation = recommendation[breakPoint:]
		if len(recommendation) > 0 && recommendation[0] == ' ' {
			recommendation = recommendation[1:]
		}
	}

	if userFeedback := SafeString(selectedInsight["user_feedback"], ""); userFeedback != "" {
		rows = append(rows, "")
		rows = append(rows, "=== USER FEEDBACK ===")
		rows = append(rows, userFeedback)
	}

	inf.detailWidget.Rows = rows
	inf.detailWidget.Title = fmt.Sprintf("AI Insight: %s", SafeString(selectedInsight["insight_type"], itemID))

	return nil
}

func (inf *InsightsFactory) HandleAction(action core.EventAction, dataManager *core.DataManager) error {
	return nil
}
