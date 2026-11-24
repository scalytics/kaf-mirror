package widgets

import (
	"fmt"
	"kaf-mirror/cmd/mirror-cli/dashboard/core"
)

type ComplianceFactory struct {
	*BaseWidgetFactory
}

func NewComplianceFactory() *ComplianceFactory {
	base := NewBaseWidgetFactory()

	cf := &ComplianceFactory{
		BaseWidgetFactory: base,
	}

	cf.listWidget.Title = "Compliance Logs (Press Enter for details, R to refresh)"
	cf.detailWidget.Title = "Audit Log Details"

	return cf
}

func (cf *ComplianceFactory) UpdateListData(dataManager *core.DataManager) error {
	logs, err := dataManager.GetLogs()
	if err != nil {
		cf.listWidget.Rows = []string{
			"Timestamp | User | Action | Resource | Details",
			fmt.Sprintf("Error fetching compliance logs: %v", err),
		}
		return err
	}

	cf.items = logs

	rows := []string{
		"Timestamp | User | Action | Resource | Details",
	}

	if len(logs) == 0 {
		rows = append(rows, "No audit logs found")
	} else {
		for _, log := range logs {
			timestamp := SafeString(log["timestamp"], "N/A")
			user := SafeString(log["initiator"], "system")
			action, resource := AuditEventParts(log)
			details := SafeString(log["details"], "")

			row := fmt.Sprintf("%s | %s | %s | %s | %s",
				TruncateString(timestamp, 16),
				TruncateString(user, 12),
				TruncateString(action, 15),
				TruncateString(resource, 24),
				TruncateString(details, 40),
			)

			rows = append(rows, row)
		}
	}

	cf.listWidget.Rows = rows

	if cf.selectedIdx >= len(cf.items) {
		cf.selectedIdx = len(cf.items) - 1
	}
	if cf.selectedIdx < 0 && len(cf.items) > 0 {
		cf.selectedIdx = 0
	}
	if len(cf.items) > 0 {
		cf.listWidget.SelectedRow = cf.selectedIdx + 1
	}

	return nil
}

func (cf *ComplianceFactory) UpdateDetailData(dataManager *core.DataManager, itemID string) error {
	if itemID == "" {
		cf.detailWidget.Rows = []string{"Select an audit log entry to view details"}
		return nil
	}

	logs, err := dataManager.GetLogs()
	if err != nil {
		cf.detailWidget.Rows = []string{
			"Error fetching log details:",
			fmt.Sprintf("%v", err),
		}
		return err
	}

	var selectedLog map[string]interface{}
	for _, log := range logs {
		if SafeString(log["id"], "") == itemID {
			selectedLog = log
			break
		}
	}

	if selectedLog == nil {
		cf.detailWidget.Rows = []string{"Log entry not found"}
		return nil
	}

	var rows []string

	rows = append(rows, "=== AUDIT LOG ENTRY ===")
	rows = append(rows, fmt.Sprintf("Timestamp: %s", SafeString(selectedLog["timestamp"], "N/A")))
	rows = append(rows, fmt.Sprintf("Event Type: %s", SafeString(selectedLog["event_type"], "N/A")))
	rows = append(rows, fmt.Sprintf("Initiator: %s", SafeString(selectedLog["initiator"], "N/A")))
	action, resource := AuditEventParts(selectedLog)
	rows = append(rows, fmt.Sprintf("Action: %s", action))
	rows = append(rows, fmt.Sprintf("Resource: %s", resource))

	rows = append(rows, "")
	rows = append(rows, "=== EVENT DETAILS ===")

	details := SafeString(selectedLog["details"], "No additional details available")
	// Split long details into multiple lines
	for len(details) > 0 {
		if len(details) <= 70 {
			rows = append(rows, details)
			break
		}
		breakPoint := 70
		for breakPoint > 50 && details[breakPoint] != ' ' {
			breakPoint--
		}
		if breakPoint <= 50 {
			breakPoint = 70
		}
		rows = append(rows, details[:breakPoint])
		details = details[breakPoint:]
		if len(details) > 0 && details[0] == ' ' {
			details = details[1:]
		}
	}

	rows = append(rows, "")
	rows = append(rows, "=== COMPLIANCE CONTEXT ===")

	eventType := SafeString(selectedLog["event_type"], "")
	switch eventType {
	case "job_created", "job_updated", "job_deleted":
		rows = append(rows, "Category: Job Management")
		rows = append(rows, "Compliance Impact: Configuration changes tracked")
		rows = append(rows, "Retention: 7 years per data governance policy")
	case "cluster_added", "cluster_removed", "cluster_updated":
		rows = append(rows, "Category: Infrastructure Changes")
		rows = append(rows, "Compliance Impact: Security boundary modifications")
		rows = append(rows, "Retention: 10 years per infrastructure audit requirements")
	case "user_login", "user_logout", "authentication_failed":
		rows = append(rows, "Category: Access Control")
		rows = append(rows, "Compliance Impact: Security access tracking")
		rows = append(rows, "Retention: 3 years per security policy")
	case "configuration_changed":
		rows = append(rows, "Category: System Configuration")
		rows = append(rows, "Compliance Impact: Operational changes documented")
		rows = append(rows, "Retention: 5 years per operational audit policy")
	default:
		rows = append(rows, "Category: General Operations")
		rows = append(rows, "Compliance Impact: Standard operational logging")
		rows = append(rows, "Retention: 1 year per default policy")
	}

	rows = append(rows, "")
	rows = append(rows, "=== AUDIT TRAIL ===")
	if sourceIP := SafeString(selectedLog["source_ip"], ""); sourceIP != "" {
		rows = append(rows, fmt.Sprintf("Source IP: %s", sourceIP))
	}
	if userAgent := SafeString(selectedLog["user_agent"], ""); userAgent != "" {
		rows = append(rows, fmt.Sprintf("User Agent: %s", TruncateString(userAgent, 50)))
	}
	if sessionID := SafeString(selectedLog["session_id"], ""); sessionID != "" {
		rows = append(rows, fmt.Sprintf("Session ID: %s", sessionID))
	}

	rows = append(rows, "")
	rows = append(rows, "=== NAVIGATION ===")
	rows = append(rows, "Press 'R' to refresh audit logs")
	rows = append(rows, "Press 'B' or Escape to go back")
	rows = append(rows, "Use audit export tools for compliance reporting")

	cf.detailWidget.Rows = rows
	cf.detailWidget.Title = fmt.Sprintf("Audit Log: %s", SafeString(selectedLog["event_type"], itemID))

	return nil
}

func (cf *ComplianceFactory) HandleAction(action core.EventAction, dataManager *core.DataManager) error {
	return nil
}
