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

package dashboard

import (
	"fmt"
	"github.com/scalytics/kaf-mirror/v2/cmd/mirror-cli/dashboard/core"
	"github.com/scalytics/kaf-mirror/v2/cmd/mirror-cli/dashboard/layouts"
	"github.com/scalytics/kaf-mirror/v2/cmd/mirror-cli/dashboard/widgets"
	"time"

	ui "github.com/gizak/termui/v3"
	termui "github.com/gizak/termui/v3/widgets"
)

type Dashboard struct {
	context     *core.NavigationContext
	router      *core.EventRouter
	dataManager *core.DataManager

	jobsFactory       *widgets.JobsFactory
	clustersFactory   *widgets.ClustersFactory
	insightsFactory   *widgets.InsightsFactory
	complianceFactory *widgets.ComplianceFactory

	categoryLayout  *layouts.CategoryLayout
	topLevelGrid    *ui.Grid
	updateTicker    *time.Ticker
	focusedCategory core.Category
}

func NewDashboard(token string, fetchers core.DataFetchers) *Dashboard {
	context := core.NewNavigationContext()
	router := core.NewEventRouter(context)
	dataManager := core.NewDataManager(token, fetchers)

	jobsFactory := widgets.NewJobsFactory()
	clustersFactory := widgets.NewClustersFactory()
	insightsFactory := widgets.NewInsightsFactory()
	complianceFactory := widgets.NewComplianceFactory()

	dashboard := &Dashboard{
		context:           context,
		router:            router,
		dataManager:       dataManager,
		jobsFactory:       jobsFactory,
		clustersFactory:   clustersFactory,
		insightsFactory:   insightsFactory,
		complianceFactory: complianceFactory,
		focusedCategory:   core.ClustersCategory, // Default focus on first category
	}

	return dashboard
}

func (d *Dashboard) setupTopLevelLayout() {
	d.topLevelGrid = ui.NewGrid()
	d.updateTopLevelFocus()
}

func (d *Dashboard) updateTopLevelFocus() {
	if d.topLevelGrid == nil {
		return
	}

	clustersWidget := termui.NewParagraph()
	clustersWidget.Title = "1. Clusters"
	clustersWidget.Text = d.getClustersOverview()
	clustersWidget.BorderStyle = ui.NewStyle(ui.ColorCyan)
	clustersWidget.WrapText = true

	jobsWidget := termui.NewParagraph()
	jobsWidget.Title = "2. Jobs"
	jobsWidget.Text = d.getJobsOverview()
	jobsWidget.BorderStyle = ui.NewStyle(ui.ColorGreen)
	jobsWidget.WrapText = true

	insightsWidget := termui.NewParagraph()
	insightsWidget.Title = "3. Insights"
	insightsWidget.Text = d.getInsightsOverview()
	insightsWidget.BorderStyle = ui.NewStyle(ui.ColorMagenta)
	insightsWidget.WrapText = true

	complianceWidget := termui.NewParagraph()
	complianceWidget.Title = "4. Compliance"
	complianceWidget.Text = d.getComplianceOverview()
	complianceWidget.BorderStyle = ui.NewStyle(ui.ColorYellow)
	complianceWidget.WrapText = true

	// Apply focus styling - make focused widget brighter/bold
	switch d.focusedCategory {
	case core.ClustersCategory:
		clustersWidget.BorderStyle = ui.NewStyle(ui.ColorWhite)
		clustersWidget.Title = ">>> 1. Clusters <<<"
	case core.JobsCategory:
		jobsWidget.BorderStyle = ui.NewStyle(ui.ColorWhite)
		jobsWidget.Title = ">>> 2. Jobs <<<"
	case core.InsightsCategory:
		insightsWidget.BorderStyle = ui.NewStyle(ui.ColorWhite)
		insightsWidget.Title = ">>> 3. Insights <<<"
	case core.ComplianceCategory:
		complianceWidget.BorderStyle = ui.NewStyle(ui.ColorWhite)
		complianceWidget.Title = ">>> 4. Compliance <<<"
	}

	termWidth, termHeight := ui.TerminalDimensions()
	d.topLevelGrid.SetRect(0, 0, termWidth, termHeight)
	d.topLevelGrid.Set(
		ui.NewRow(1.0/2,
			ui.NewCol(1.0/2, clustersWidget),
			ui.NewCol(1.0/2, jobsWidget),
		),
		ui.NewRow(1.0/2,
			ui.NewCol(1.0/2, insightsWidget),
			ui.NewCol(1.0/2, complianceWidget),
		),
	)
}

func (d *Dashboard) Run() error {
	if err := ui.Init(); err != nil {
		return err
	}
	defer ui.Close()

	// Set up layout after UI is initialized
	d.setupTopLevelLayout()

	d.render()

	// Set up update ticker for live data
	d.updateTicker = time.NewTicker(2 * time.Second)
	defer d.updateTicker.Stop()

	uiEvents := ui.PollEvents()

	for {
		select {
		case e := <-uiEvents:
			if d.handleEvent(e) {
				return nil // Exit requested
			}
			d.render()

		case <-d.updateTicker.C:
			d.updateData()
			d.render()
		}
	}
}

func (d *Dashboard) handleEvent(event ui.Event) bool {
	action := d.router.RouteEvent(event)

	switch a := action.(type) {
	case core.ExitAction:
		return true

	case core.BackAction:
		if d.context.IsAtTopLevel() {
			// Exit dashboard when at top level
			return true
		} else {
			d.context.NavigateBack()
			d.setupCurrentView()
		}

	case core.NavigateToCategoryAction:
		d.context.NavigateToCategory(a.Category)
		d.setupCurrentView()

	case core.ScrollAction:
		d.handleScroll(a.Direction)

	case core.SelectItemAction:
		d.handleSelection()

	case core.RefreshAction:
		d.dataManager.InvalidateAllCache()
		d.updateData()

	case core.JobControlAction:
		if d.context.Category == core.JobsCategory {
			d.jobsFactory.HandleAction(action, d.dataManager)
		}

	case core.MoveFocusAction:
		d.handleMoveFocus(a.Direction)

	case core.SwitchPaneFocusAction:
		d.handleSwitchPaneFocus(a.Direction)

	case core.NoAction:
		// Do nothing
	}

	return false
}

func (d *Dashboard) handleScroll(direction core.Direction) {
	if d.context.IsInCategory() && d.categoryLayout != nil {
		d.categoryLayout.HandleScroll(direction)
	} else if d.context.IsInDetail() {
		// Handle detail view scrolling
		factory := d.getCurrentFactory()
		if factory != nil {
			factory.ScrollDetail(direction)
		}
	}
}

func (d *Dashboard) handleMoveFocus(direction core.Direction) {
	if !d.context.IsAtTopLevel() {
		return
	}

	// Navigate between categories based on grid layout
	switch direction {
	case core.Up:
		if d.focusedCategory == core.InsightsCategory {
			d.focusedCategory = core.ClustersCategory
		} else if d.focusedCategory == core.ComplianceCategory {
			d.focusedCategory = core.JobsCategory
		}
	case core.Down:
		if d.focusedCategory == core.ClustersCategory {
			d.focusedCategory = core.InsightsCategory
		} else if d.focusedCategory == core.JobsCategory {
			d.focusedCategory = core.ComplianceCategory
		}
	case core.Left:
		if d.focusedCategory == core.JobsCategory {
			d.focusedCategory = core.ClustersCategory
		} else if d.focusedCategory == core.ComplianceCategory {
			d.focusedCategory = core.InsightsCategory
		}
	case core.Right:
		if d.focusedCategory == core.ClustersCategory {
			d.focusedCategory = core.JobsCategory
		} else if d.focusedCategory == core.InsightsCategory {
			d.focusedCategory = core.ComplianceCategory
		}
	}

	d.updateTopLevelFocus()
}

func (d *Dashboard) handleSwitchPaneFocus(direction core.Direction) {
	if !d.context.IsInCategory() {
		return
	}

	switch direction {
	case core.Right:
		if d.context.PaneFocus == core.ListPaneFocus {
			d.context.PaneFocus = core.DetailPaneFocus
		}
	case core.Left:
		if d.context.PaneFocus == core.DetailPaneFocus {
			d.context.PaneFocus = core.ListPaneFocus
		}
	}

	// Update visual focus in category layout
	if d.categoryLayout != nil {
		d.categoryLayout.UpdatePaneFocus(d.context.PaneFocus)
	}
}

func (d *Dashboard) handleSelection() {
	if d.context.IsAtTopLevel() {
		// Navigate to the currently focused category
		d.context.NavigateToCategory(d.focusedCategory)
		d.setupCurrentView()
	} else if d.context.IsInCategory() && d.categoryLayout != nil {
		d.categoryLayout.HandleSelection()
		// Don't call setupCurrentView() - stay in two-pane layout
		// The UpdateData will refresh the right pane with selected item details
	}
}

func (d *Dashboard) setupCurrentView() {
	termWidth, termHeight := ui.TerminalDimensions()

	switch d.context.State {
	case core.TopLevelState:
		// Already set up in setupTopLevelLayout

	case core.CategoryViewState:
		factory := d.getCurrentFactory()
		if factory != nil {
			d.categoryLayout = layouts.NewCategoryLayout(factory, d.context)
			d.categoryLayout.Setup(termWidth, termHeight)
			// Apply initial pane focus styling
			d.categoryLayout.UpdatePaneFocus(d.context.PaneFocus)
		}

	case core.DetailViewState:
		factory := d.getCurrentFactory()
		if factory != nil {
			// Update detail data first
			factory.UpdateDetailData(d.dataManager, d.context.ItemID)

			// Set up full-screen detail view
			detailWidget := factory.CreateDetailWidget(d.context.ItemID)
			d.topLevelGrid = ui.NewGrid()
			d.topLevelGrid.SetRect(0, 0, termWidth, termHeight)
			d.topLevelGrid.Set(ui.NewRow(1.0, ui.NewCol(1.0, detailWidget)))

			// Clear category layout as we're using topLevelGrid for detail
			d.categoryLayout = nil
		}
	}
}

func (d *Dashboard) getCurrentFactory() widgets.WidgetFactory {
	switch d.context.Category {
	case core.ClustersCategory:
		return d.clustersFactory
	case core.JobsCategory:
		return d.jobsFactory
	case core.InsightsCategory:
		return d.insightsFactory
	case core.ComplianceCategory:
		return d.complianceFactory
	}
	return nil
}

func (d *Dashboard) updateData() {
	switch d.context.State {
	case core.TopLevelState:
		// Refresh top-level overview data
		d.updateTopLevelFocus()
	case core.CategoryViewState, core.DetailViewState:
		if d.categoryLayout != nil {
			d.categoryLayout.UpdateData(d.dataManager)
		}
	}
}

func (d *Dashboard) render() {
	switch d.context.State {
	case core.TopLevelState:
		ui.Render(d.topLevelGrid)

	case core.CategoryViewState:
		if d.categoryLayout != nil {
			ui.Render(d.categoryLayout.GetGrid())
		}

	case core.DetailViewState:
		// Use topLevelGrid for detail view (set up in setupCurrentView)
		ui.Render(d.topLevelGrid)
	}
}

func RunDashboard(token string, userInfo map[string]interface{}, fetchers core.DataFetchers) error {
	dashboard := NewDashboard(token, fetchers)
	return dashboard.Run()
}

func (d *Dashboard) getClustersOverview() string {
	clusters, err := d.dataManager.GetClusters()
	if err != nil {
		return "Error loading clusters"
	}

	if len(clusters) == 0 {
		return "No clusters configured"
	}

	text := ""
	for i, cluster := range clusters {
		if i >= 3 { // Show max 3 clusters
			text += "...\n"
			break
		}
		name := widgets.SafeString(cluster["name"], "Unknown")
		status := widgets.SafeString(cluster["status"], "unknown")
		text += fmt.Sprintf("• %s (%s)\n", widgets.TruncateString(name, 20), status)
	}

	if len(clusters) > 3 {
		text += fmt.Sprintf("Total: %d clusters", len(clusters))
	}

	return text
}

func (d *Dashboard) getJobsOverview() string {
	jobs, err := d.dataManager.GetJobs()
	if err != nil {
		return "Error loading jobs"
	}

	if len(jobs) == 0 {
		return "No replication jobs"
	}

	text := ""
	for i, job := range jobs {
		if i >= 3 { // Show max 3 jobs
			text += "...\n"
			break
		}
		name := widgets.SafeString(job["name"], "Unknown")
		status := widgets.SafeString(job["status"], "unknown")
		text += fmt.Sprintf("• %s (%s)\n", widgets.TruncateString(name, 20), status)
	}

	if len(jobs) > 3 {
		text += fmt.Sprintf("Total: %d jobs", len(jobs))
	}

	return text
}

func (d *Dashboard) getInsightsOverview() string {
	insights, err := d.dataManager.GetAIInsights()
	if err != nil {
		return "Error loading insights"
	}

	if len(insights) == 0 {
		return "No AI insights available"
	}

	text := ""
	for i, insight := range insights {
		if i >= 5 { // Show max 5 insights
			text += "...\n"
			break
		}
		insightType := widgets.SafeString(insight["insight_type"], "Unknown")
		severity := widgets.SafeString(insight["severity_level"], "normal")
		text += fmt.Sprintf("• %s (%s)\n", widgets.TruncateString(insightType, 20), severity)
	}

	if len(insights) > 5 {
		text += fmt.Sprintf("Total: %d insights", len(insights))
	}

	return text
}

func (d *Dashboard) getComplianceOverview() string {
	logs, err := d.dataManager.GetLogs()
	if err != nil {
		return "Error loading compliance logs"
	}

	if len(logs) == 0 {
		return "No compliance events"
	}

	text := ""
	for i, log := range logs {
		if i >= 3 { // Show max 3 compliance entries
			text += "...\n"
			break
		}
		action, resource := widgets.AuditEventParts(log)
		user := widgets.SafeString(log["initiator"], "system")
		text += fmt.Sprintf("• %s %s by %s\n", widgets.TruncateString(action, 20), widgets.TruncateString(resource, 20), widgets.TruncateString(user, 12))
	}

	if len(logs) > 3 {
		text += fmt.Sprintf("Total: %d events", len(logs))
	}

	return text
}

func NewParagraph() *termui.Paragraph {
	p := termui.NewParagraph()
	p.TextStyle = ui.NewStyle(ui.ColorWhite)
	p.BorderStyle = ui.NewStyle(ui.ColorWhite)
	return p
}
