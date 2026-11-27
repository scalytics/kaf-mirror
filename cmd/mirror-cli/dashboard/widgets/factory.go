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

package widgets

import (
	"fmt"
	"github.com/scalytics/kaf-mirror/v2/cmd/mirror-cli/dashboard/core"
	"strings"

	ui "github.com/gizak/termui/v3"
	"github.com/gizak/termui/v3/widgets"
)

type WidgetFactory interface {
	CreateListWidget() ui.Drawable
	CreateDetailWidget(itemID string) ui.Drawable
	UpdateListData(dataManager *core.DataManager) error
	UpdateDetailData(dataManager *core.DataManager, itemID string) error
	HandleAction(action core.EventAction, dataManager *core.DataManager) error
	GetSelectedItemID() string
	GetSelectedItemName() string
	ScrollList(direction core.Direction)
	ScrollDetail(direction core.Direction)
	SetDetailCursorVisible(visible bool)
	ResetDetailCursor()
}

type BaseWidgetFactory struct {
	listWidget      *widgets.List
	detailWidget    *widgets.List
	selectedIdx     int
	detailCursorIdx int
	items           []map[string]interface{}
}

func NewBaseWidgetFactory() *BaseWidgetFactory {
	listWidget := widgets.NewList()
	listWidget.TextStyle = ui.NewStyle(ui.ColorWhite)
	listWidget.BorderStyle = ui.NewStyle(ui.ColorCyan)
	listWidget.SelectedRowStyle = ui.NewStyle(ui.ColorBlack, ui.ColorWhite)
	listWidget.WrapText = true

	detailWidget := widgets.NewList()
	detailWidget.TextStyle = ui.NewStyle(ui.ColorWhite)
	detailWidget.BorderStyle = ui.NewStyle(ui.ColorYellow)
	detailWidget.SelectedRowStyle = ui.NewStyle(ui.ColorBlack, ui.ColorWhite)
	detailWidget.WrapText = true

	return &BaseWidgetFactory{
		listWidget:      listWidget,
		detailWidget:    detailWidget,
		selectedIdx:     0,
		detailCursorIdx: 0,
		items:           []map[string]interface{}{},
	}
}

func (bwf *BaseWidgetFactory) CreateListWidget() ui.Drawable {
	return bwf.listWidget
}

func (bwf *BaseWidgetFactory) CreateDetailWidget(itemID string) ui.Drawable {
	return bwf.detailWidget
}

func (bwf *BaseWidgetFactory) ScrollList(direction core.Direction) {
	switch direction {
	case core.Down:
		if bwf.selectedIdx < len(bwf.items)-1 {
			bwf.selectedIdx++
			bwf.listWidget.SelectedRow = bwf.selectedIdx + 1
		}
	case core.Up:
		if bwf.selectedIdx > 0 {
			bwf.selectedIdx--
			bwf.listWidget.SelectedRow = bwf.selectedIdx + 1
		}
	case core.PageDown:
		newIdx := bwf.selectedIdx + 5
		if newIdx >= len(bwf.items) {
			newIdx = len(bwf.items) - 1
		}
		if newIdx >= 0 {
			bwf.selectedIdx = newIdx
			bwf.listWidget.SelectedRow = bwf.selectedIdx + 1
		}
	case core.PageUp:
		newIdx := bwf.selectedIdx - 5
		if newIdx < 0 {
			newIdx = 0
		}
		bwf.selectedIdx = newIdx
		bwf.listWidget.SelectedRow = bwf.selectedIdx + 1
	}
}

func (bwf *BaseWidgetFactory) ScrollDetail(direction core.Direction) {
	maxRows := len(bwf.detailWidget.Rows)
	if maxRows == 0 {
		return
	}

	switch direction {
	case core.Down:
		if bwf.detailCursorIdx < maxRows-1 {
			bwf.detailCursorIdx++
		}
	case core.Up:
		if bwf.detailCursorIdx > 0 {
			bwf.detailCursorIdx--
		}
	case core.PageDown:
		bwf.detailCursorIdx += 5
		if bwf.detailCursorIdx >= maxRows {
			bwf.detailCursorIdx = maxRows - 1
		}
	case core.PageUp:
		bwf.detailCursorIdx -= 5
		if bwf.detailCursorIdx < 0 {
			bwf.detailCursorIdx = 0
		}
	}

	bwf.detailWidget.SelectedRow = bwf.detailCursorIdx
}

func (bwf *BaseWidgetFactory) ResetDetailCursor() {
	bwf.detailCursorIdx = 0
	if len(bwf.detailWidget.Rows) > 0 {
		bwf.detailWidget.SelectedRow = 0
	}
}

func (bwf *BaseWidgetFactory) SetDetailCursorVisible(visible bool) {
	if visible {
		if len(bwf.detailWidget.Rows) > 0 {
			bwf.detailWidget.SelectedRow = bwf.detailCursorIdx
		}
	} else {
		bwf.detailWidget.SelectedRow = -1
	}
}

func (bwf *BaseWidgetFactory) GetSelectedItemID() string {
	if bwf.selectedIdx < 0 || bwf.selectedIdx >= len(bwf.items) {
		return ""
	}

	item := bwf.items[bwf.selectedIdx]
	// Try id field first (handle both string and integer IDs)
	if id := item["id"]; id != nil {
		return fmt.Sprintf("%v", id)
	}
	if name, ok := item["name"].(string); ok {
		return name
	}
	return ""
}

func (bwf *BaseWidgetFactory) GetSelectedItemName() string {
	if bwf.selectedIdx < 0 || bwf.selectedIdx >= len(bwf.items) {
		return ""
	}

	item := bwf.items[bwf.selectedIdx]
	// Try name field first, fallback to id
	if name, ok := item["name"].(string); ok {
		return name
	}
	if id, ok := item["id"].(string); ok {
		return id
	}
	return "Unknown"
}

func SafeString(value interface{}, defaultValue string) string {
	if value == nil {
		return defaultValue
	}
	if str, ok := value.(string); ok {
		return str
	}
	return defaultValue
}

func SafeFloat(value interface{}, defaultValue float64) float64 {
	if value == nil {
		return defaultValue
	}
	if f, ok := value.(float64); ok {
		return f
	}
	if i, ok := value.(int); ok {
		return float64(i)
	}
	return defaultValue
}

func TruncateString(str string, maxLen int) string {
	if len(str) <= maxLen {
		return str
	}
	return str[:maxLen-3] + "..."
}

func FormatStatus(status string) string {
	switch status {
	case "active", "running", "healthy":
		return "✓ " + status
	case "inactive", "stopped", "paused":
		return "⏸ " + status
	case "error", "failed", "unhealthy", "unreachable":
		return "✗ " + status
	default:
		return status
	}
}

func AuditEventParts(log map[string]interface{}) (string, string) {
	eventType := SafeString(log["event_type"], "")
	if eventType == "" {
		return "unknown", "N/A"
	}
	method, path, found := strings.Cut(eventType, " ")
	if !found || !strings.HasPrefix(path, "/") {
		return eventType, "N/A"
	}
	path = strings.TrimPrefix(path, "/api/v1")
	path = strings.Trim(path, "/")
	if path == "" {
		path = "/"
	}
	return method, path
}

func ClusterHealthLabel(status string) string {
	switch status {
	case "active":
		return "✓ Reachable"
	case "unreachable":
		return "✗ Unreachable"
	case "inactive":
		return "⏸ Inactive"
	case "archived":
		return "⏸ Archived"
	case "unknown", "":
		return "? Not checked yet"
	default:
		return "? " + status
	}
}
