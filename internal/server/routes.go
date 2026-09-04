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

package server

import (
	"kaf-mirror/internal/server/middleware"

	"github.com/gofiber/fiber/v2/middleware/adaptor"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func (s *Server) setupRoutes() {
	s.App.Get("/health", s.handleHealthCheck)
	s.App.Get("/api/v1/version", s.handleGetVersion)
	// BUG-0011: Prometheus scrape endpoint. Unauthenticated by design so the
	// ServiceMonitor scrapes without a token; exposes only Go runtime +
	// process metrics, no business data.
	s.App.Get("/metrics", adaptor.HTTPHandler(promhttp.Handler()))

	// WebSocket route - MUST be before API group to avoid auth middleware conflict
	s.App.Get("/ws", s.handleWebSocketAuth)

	authGroup := s.App.Group("/auth")
	authGroup.Post("/token", s.handleGenerateToken)
	authGroup.Get("/me", middleware.AuthRequired(s.Db), s.handleGetMe)

	api := s.App.Group("/api/v1", middleware.AuthRequired(s.Db), middleware.AuditLog(s.Db))

	configGroup := api.Group("/config", middleware.AuthRequired(s.Db))
	configGroup.Get("/", middleware.PermissionRequired(s.Db, "config:view"), s.handleGetConfig)
	configGroup.Put("/", middleware.PermissionRequired(s.Db, "config:edit"), s.handleUpdateConfig)
	configGroup.Post("/export", middleware.PermissionRequired(s.Db, "config:view"), s.handleExportConfig)
	configGroup.Post("/import", middleware.PermissionRequired(s.Db, "config:edit"), s.handleImportConfig)

	clustersGroup := api.Group("/clusters")
	clustersGroup.Post("/test", middleware.PermissionRequired(s.Db, "clusters:create"), s.handleTestClusterConnection)
	clustersGroup.Get("/", middleware.PermissionRequired(s.Db, "clusters:view"), s.handleListClusters)
	clustersGroup.Post("/", middleware.PermissionRequired(s.Db, "clusters:create"), s.handleCreateCluster)
	clustersGroup.Get("/:name", middleware.PermissionRequired(s.Db, "clusters:view"), s.handleGetCluster)
	clustersGroup.Get("/:name/status", middleware.PermissionRequired(s.Db, "clusters:view"), s.handleGetClusterStatus)
	clustersGroup.Get("/:name/topics", middleware.PermissionRequired(s.Db, "clusters:view"), s.handleListClusterTopics)
	clustersGroup.Get("/:name/topic-details", middleware.PermissionRequired(s.Db, "clusters:view"), s.handleGetTopicDetails)
	clustersGroup.Put("/:name", middleware.PermissionRequired(s.Db, "clusters:edit"), s.handleUpdateCluster)
	clustersGroup.Delete("/:name", middleware.PermissionRequired(s.Db, "clusters:delete"), s.handleDeleteCluster)
	clustersGroup.Post("/:name/restore", middleware.PermissionRequired(s.Db, "clusters:delete"), s.handleRestoreCluster)
	clustersGroup.Delete("/purge", middleware.PermissionRequired(s.Db, "clusters:delete"), s.handlePurgeClusters)

	jobsGroup := api.Group("/jobs")
	jobsGroup.Get("/", middleware.PermissionRequired(s.Db, "jobs:view"), s.handleListJobs)
	jobsGroup.Post("/", middleware.PermissionRequired(s.Db, "jobs:create"), s.handleCreateJob)
	jobsGroup.Get("/:id", middleware.PermissionRequired(s.Db, "jobs:view"), s.handleGetJob)
	jobsGroup.Put("/:id", middleware.PermissionRequired(s.Db, "jobs:edit"), s.handleUpdateJob)
	jobsGroup.Delete("/:id", middleware.PermissionRequired(s.Db, "jobs:delete"), s.handleDeleteJob)
	jobsGroup.Post("/:id/start", middleware.PermissionRequired(s.Db, "jobs:start"), s.handleStartJob)
	jobsGroup.Post("/:id/stop", middleware.PermissionRequired(s.Db, "jobs:stop"), s.handleStopJob)
	jobsGroup.Post("/:id/pause", middleware.PermissionRequired(s.Db, "jobs:pause"), s.handlePauseJob)
	jobsGroup.Post("/:id/restart", middleware.PermissionRequired(s.Db, "jobs:start"), s.handleRestartJob)
	jobsGroup.Post("/:id/force-restart", middleware.PermissionRequired(s.Db, "jobs:start"), s.handleForceRestartJob)
	jobsGroup.Post("/start-all", middleware.PermissionRequired(s.Db, "jobs:start"), s.handleStartAllJobs)
	jobsGroup.Post("/stop-all", middleware.PermissionRequired(s.Db, "jobs:stop"), s.handleStopAllJobs)
	jobsGroup.Post("/restart-all", middleware.PermissionRequired(s.Db, "jobs:start"), s.handleRestartAllJobs)

	jobsGroup.Get("/:id/mappings", middleware.PermissionRequired(s.Db, "jobs:view"), s.handleGetMappings)
	jobsGroup.Put("/:id/mappings", middleware.PermissionRequired(s.Db, "jobs:edit"), s.handleUpdateMappings)

	jobsGroup.Get("/:id/metrics/current", middleware.PermissionRequired(s.Db, "metrics:view"), s.handleGetCurrentMetrics)
	jobsGroup.Get("/:id/metrics/history", middleware.PermissionRequired(s.Db, "metrics:view"), s.handleGetHistoricalMetrics)
	jobsGroup.Get("/:id/lag", middleware.PermissionRequired(s.Db, "metrics:view"), s.handleGetLag)
	jobsGroup.Get("/:id/topic-health", middleware.PermissionRequired(s.Db, "jobs:view"), s.handleGetJobTopicHealth)

	api.Get("/topics/source", middleware.PermissionRequired(s.Db, "clusters:view"), s.handleListSourceTopics)
	api.Get("/topics/target", middleware.PermissionRequired(s.Db, "clusters:view"), s.handleListTargetTopics)

	aiGroup := api.Group("/ai")
	aiGroup.Get("/insights", middleware.PermissionRequired(s.Db, "ai:insights:view"), s.handleGetAIInsights)
	aiGroup.Get("/metrics", middleware.PermissionRequired(s.Db, "ai:insights:view"), s.handleGetAIMetrics)
	aiGroup.Post("/test", middleware.PermissionRequired(s.Db, "ai:analysis:trigger"), s.handleTestAIIntegration)
	aiGroup.Get("/anomalies", middleware.PermissionRequired(s.Db, "ai:insights:view"), s.handleGetAnomalies)
	aiGroup.Get("/recommendations", middleware.PermissionRequired(s.Db, "ai:insights:view"), s.handleGetRecommendations)
	aiGroup.Post("/explain/:event", middleware.PermissionRequired(s.Db, "ai:insights:view"), s.handleExplainEvent)
	aiGroup.Post("/incidents/:event_id/analyze", middleware.PermissionRequired(s.Db, "ai:analysis:trigger"), s.handleAnalyzeIncident)

	jobsGroup.Post("/:id/ai/analyze", middleware.PermissionRequired(s.Db, "ai:analysis:trigger"), s.handleTriggerJobAIAnalysis)
	jobsGroup.Get("/:id/ai/insights", middleware.PermissionRequired(s.Db, "ai:insights:view"), s.handleGetJobAIInsights)
	jobsGroup.Post("/:id/ai/historical-analysis", middleware.PermissionRequired(s.Db, "ai:analysis:trigger"), s.handleTriggerHistoricalAnalysis)

	jobsGroup.Get("/:id/inventory/snapshots", middleware.PermissionRequired(s.Db, "inventory:view"), s.handleGetJobInventorySnapshots)
	jobsGroup.Post("/:id/inventory/snapshots", middleware.PermissionRequired(s.Db, "inventory:create"), s.handleCreateManualInventorySnapshot)

	inventoryGroup := api.Group("/inventory")
	inventoryGroup.Get("/snapshots/:snapshot_id", middleware.PermissionRequired(s.Db, "inventory:view"), s.handleGetInventorySnapshot)
	inventoryGroup.Get("/snapshots/:snapshot_id/cluster", middleware.PermissionRequired(s.Db, "inventory:view"), s.handleGetClusterInventory)
	inventoryGroup.Get("/snapshots/:snapshot_id/topics", middleware.PermissionRequired(s.Db, "inventory:view"), s.handleGetTopicInventory)
	inventoryGroup.Get("/snapshots/:snapshot_id/consumer-groups", middleware.PermissionRequired(s.Db, "inventory:view"), s.handleGetConsumerGroupInventory)
	inventoryGroup.Get("/snapshots/:snapshot_id/connections", middleware.PermissionRequired(s.Db, "inventory:view"), s.handleGetConnectionInventory)

	mirrorGroup := api.Group("/jobs/:id/mirror")
	mirrorGroup.Use(middleware.PermissionRequired(s.Db, "jobs:view"))
	{
		mirrorGroup.Get("/state", s.handleGetMirrorState)
		mirrorGroup.Get("/progress", s.handleGetMirrorProgress)
		mirrorGroup.Get("/resume-points", s.handleGetResumePoints)
		mirrorGroup.Post("/resume-points", middleware.PermissionRequired(s.Db, "jobs:edit"), s.handleCalculateResumePoints)
		mirrorGroup.Get("/gaps", s.handleGetMirrorGaps)
		mirrorGroup.Post("/validate-mirror", middleware.PermissionRequired(s.Db, "jobs:edit"), s.handleValidateMigration)
		mirrorGroup.Post("/checkpoint", middleware.PermissionRequired(s.Db, "jobs:edit"), s.handleCreateMigrationCheckpoint)
	}

	api.Get("/events", middleware.PermissionRequired(s.Db, "events:view"), s.handleGetOperationalEvents)

	usersGroup := api.Group("/users")
	usersGroup.Get("/", middleware.PermissionRequired(s.Db, "users:list"), s.handleListUsers)
	usersGroup.Post("/", middleware.PermissionRequired(s.Db, "users:create"), s.handleCreateUser)
	usersGroup.Put("/:username/role", middleware.PermissionRequired(s.Db, "users:assign-roles"), s.handleUpdateUserRole)
	usersGroup.Delete("/:username", middleware.PermissionRequired(s.Db, "users:delete"), s.handleDeleteUser)
	usersGroup.Put("/change-password", s.handleChangePassword)
	usersGroup.Post("/:username/reset-password", middleware.PermissionRequired(s.Db, "users:create"), s.handleResetUserPassword)
	api.Post("/auth/reset-token", s.handleResetOwnToken)

	complianceGroup := api.Group("/compliance")
	complianceGroup.Post("/report/:period", middleware.PermissionRequired(s.Db, "compliance:generate"), s.handleGenerateComplianceReport)
	complianceGroup.Get("/reports", middleware.PermissionRequired(s.Db, "compliance:view"), s.handleListComplianceReports)
	complianceGroup.Get("/report/:id", middleware.PermissionRequired(s.Db, "compliance:view"), s.handleGetComplianceReport)

	// Dashboard routes - HTML files served without auth (JS handles redirect)
	s.App.Get("/", s.handleDashboard)
	s.App.Get("/login", s.handleLoginPage)
	s.App.Get("/login.html", s.handleLoginPage)

	// Static assets (CSS, JS, images) - no auth required
	webPath := s.getWebPath()
	s.App.Static("/assets", webPath+"/assets")
	s.App.Static("/docu", webPath+"/docu")
}
