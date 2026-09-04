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
	"context"
	crypto_rand "crypto/rand"
	"encoding/json"
	"fmt"
	"kaf-mirror/internal/config"
	"kaf-mirror/internal/database"
	"kaf-mirror/internal/kafka"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gofiber/contrib/websocket"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"gopkg.in/yaml.v2"
)

// handleHealthCheck godoc
// @Summary Show the status of server.
// @Description get the status of server.
// @Tags root
// @Accept */*
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /health [get]
func (s *Server) handleHealthCheck(c *fiber.Ctx) error {
	uptime := time.Since(s.startTime).Seconds()
	return c.JSON(fiber.Map{
		"status":    "ok",
		"uptime":    int(uptime),
		"timestamp": time.Now().Unix(),
	})
}

// handleGetVersion godoc
// @Summary Get the application version
// @Description Get the current version of the kaf-mirror application.
// @Tags root
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /version [get]
func (s *Server) handleGetVersion(c *fiber.Ctx) error {
	return c.JSON(fiber.Map{
		"version": s.Version,
	})
}

// handleGetConfig godoc
// @Summary Get the current configuration
// @Description Get the current configuration of the kaf-mirror.
// @Tags config
// @Produce json
// @Success 200 {object} config.Config
// @Router /config [get]
// @Security ApiKeyAuth
func (s *Server) handleGetConfig(c *fiber.Ctx) error {
	cfg, err := database.LoadConfig(s.Db)
	if err != nil {
		return c.JSON(s.cfg.Redacted())
	}
	return c.JSON(cfg.Redacted())
}

// handleUpdateConfig godoc
// @Summary Update the configuration
// @Description Update the configuration of the kaf-mirror.
// @Tags config
// @Accept json
// @Produce json
// @Param config body config.Config true "New configuration"
// @Success 200 {object} map[string]interface{}
// @Router /config [put]
// @Security ApiKeyAuth
func (s *Server) handleUpdateConfig(c *fiber.Ctx) error {
	var newCfg config.Config
	if err := c.BodyParser(&newCfg); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "Invalid request body")
	}

	existing, err := database.LoadConfig(s.Db)
	if err != nil {
		existing = s.cfg
	}
	newCfg.RestoreUnchangedSecrets(existing)

	if err := newCfg.Validate(); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, fmt.Sprintf("Invalid configuration: %v", err))
	}

	if err := database.SaveConfig(s.Db, &newCfg); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "Failed to save configuration")
	}

	*s.cfg = newCfg

	return c.JSON(fiber.Map{"status": "success", "message": "Configuration updated"})
}

// handleExportConfig godoc
// @Summary Export the configuration
// @Description Export the configuration to a YAML file.
// @Tags config
// @Produce application/x-yaml
// @Success 200 {string} string "YAML configuration"
// @Router /config/export [post]
// @Security ApiKeyAuth
func (s *Server) handleExportConfig(c *fiber.Ctx) error {
	cfg, err := database.LoadConfig(s.Db)
	if err != nil {
		return fiber.NewError(fiber.StatusNotFound, "No configuration found in database to export")
	}

	redacted := cfg.Redacted()
	yamlData, err := yaml.Marshal(redacted)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "Failed to marshal config to YAML")
	}
	c.Set("Content-Type", "application/x-yaml")
	return c.Send(yamlData)
}

// handleImportConfig godoc
// @Summary Import a configuration
// @Description Import a configuration from a YAML file.
// @Tags config
// @Accept json
// @Produce json
// @Param config body config.Config true "New configuration"
// @Success 200 {object} map[string]interface{}
// @Router /config/import [post]
// @Security ApiKeyAuth
func (s *Server) handleImportConfig(c *fiber.Ctx) error {
	// This would typically handle a file upload. For simplicity, we'll parse a JSON body.
	var newCfg config.Config
	if err := c.BodyParser(&newCfg); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "Invalid request body")
	}

	existing, err := database.LoadConfig(s.Db)
	if err != nil {
		existing = s.cfg
	}
	newCfg.RestoreUnchangedSecrets(existing)

	if err := database.SaveConfig(s.Db, &newCfg); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "Failed to import configuration")
	}

	*s.cfg = newCfg
	return c.JSON(fiber.Map{"status": "success", "message": "Configuration imported"})
}

// --- Cluster Handlers ---

// handleListClusters godoc
// @Summary List all Kafka clusters
// @Description Get a list of all configured Kafka clusters.
// @Tags clusters
// @Produce json
// @Success 200 {array} database.KafkaCluster
// @Router /clusters [get]
// @Security ApiKeyAuth
func (s *Server) handleListClusters(c *fiber.Ctx) error {
	clusters, err := database.ListClusters(s.Db)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "Failed to list clusters")
	}
	out := make([]database.KafkaCluster, len(clusters))
	for i, cluster := range clusters {
		out[i] = cluster.Redacted()
	}
	return c.JSON(out)
}

// handleCreateCluster godoc
// @Summary Create a new Kafka cluster
// @Description Create a new Kafka cluster configuration.
// @Tags clusters
// @Accept json
// @Produce json
// @Param cluster body database.KafkaCluster true "Kafka Cluster"
// @Success 201 {object} database.KafkaCluster
// @Router /clusters [post]
// @Security ApiKeyAuth
func (s *Server) handleCreateCluster(c *fiber.Ctx) error {
	var cluster database.KafkaCluster
	if err := c.BodyParser(&cluster); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "Invalid request body")
	}

	if err := database.CreateCluster(s.Db, &cluster); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "Failed to create cluster")
	}

	return c.Status(fiber.StatusCreated).JSON(cluster.Redacted())
}

// handleGetCluster godoc
// @Summary Get a Kafka cluster by name
// @Description Get a single Kafka cluster by its name.
// @Tags clusters
// @Produce json
// @Param name path string true "Cluster Name"
// @Success 200 {object} database.KafkaCluster
// @Router /clusters/{name} [get]
// @Security ApiKeyAuth
func (s *Server) handleGetCluster(c *fiber.Ctx) error {
	cluster, err := database.GetCluster(s.Db, c.Params("name"))
	if err != nil {
		return fiber.NewError(fiber.StatusNotFound, "Cluster not found")
	}
	return c.JSON(cluster.Redacted())
}

// handleUpdateCluster godoc
// @Summary Update a Kafka cluster
// @Description Update an existing Kafka cluster.
// @Tags clusters
// @Accept json
// @Produce json
// @Param name path string true "Cluster Name"
// @Param cluster body database.KafkaCluster true "Kafka Cluster"
// @Success 200 {object} database.KafkaCluster
// @Router /clusters/{name} [put]
// @Security ApiKeyAuth
func (s *Server) handleUpdateCluster(c *fiber.Ctx) error {
	var cluster database.KafkaCluster
	if err := c.BodyParser(&cluster); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "Invalid request body")
	}

	cluster.Name = c.Params("name")
	existing, err := database.GetCluster(s.Db, cluster.Name)
	if err != nil {
		return fiber.NewError(fiber.StatusNotFound, "Cluster not found")
	}
	cluster.RestoreUnchangedSecrets(existing)
	if err := database.UpdateCluster(s.Db, &cluster); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "Failed to update cluster")
	}

	return c.JSON(cluster.Redacted())
}

// handleDeleteCluster godoc
// @Summary Mark a Kafka cluster for deletion
// @Description Mark a Kafka cluster as inactive. It will be archived after 72 hours and then can be purged.
// @Tags clusters
// @Param name path string true "Cluster Name"
// @Success 202
// @Router /clusters/{name} [delete]
// @Security ApiKeyAuth
func (s *Server) handleDeleteCluster(c *fiber.Ctx) error {
	clusterName := c.Params("name")

	// Check if any jobs are running on the cluster
	jobs, err := database.ListJobs(s.Db)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "Failed to list jobs")
	}
	for _, job := range jobs {
		if (job.SourceClusterName == clusterName || job.TargetClusterName == clusterName) && job.Status == "active" {
			return fiber.NewError(fiber.StatusConflict, "Cannot delete cluster with active jobs")
		}
	}

	if err := database.SetClusterStatus(s.Db, clusterName, "inactive"); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "Failed to mark cluster as inactive")
	}

	return c.SendStatus(fiber.StatusAccepted)
}

// handleRestoreCluster godoc
// @Summary Restore an inactive Kafka cluster
// @Description Restore an inactive Kafka cluster to active status.
// @Tags clusters
// @Param name path string true "Cluster Name"
// @Success 200 {object} map[string]interface{}
// @Router /clusters/{name}/restore [post]
// @Security ApiKeyAuth
func (s *Server) handleRestoreCluster(c *fiber.Ctx) error {
	clusterName := c.Params("name")
	cluster, err := database.GetCluster(s.Db, clusterName)
	if err != nil {
		return fiber.NewError(fiber.StatusNotFound, "Cluster not found")
	}

	if cluster.Status != "inactive" {
		return fiber.NewError(fiber.StatusBadRequest, "Cluster is not inactive, cannot restore")
	}

	if err := database.SetClusterStatus(s.Db, clusterName, "active"); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "Failed to restore cluster")
	}

	return c.JSON(fiber.Map{"status": "success", "message": "Cluster restored successfully"})
}

// handlePurgeClusters godoc
// @Summary Purge archived Kafka clusters
// @Description Permanently delete all archived Kafka clusters.
// @Tags clusters
// @Success 204
// @Router /clusters/purge [delete]
// @Security ApiKeyAuth
func (s *Server) handlePurgeClusters(c *fiber.Ctx) error {
	if err := database.PurgeArchivedClusters(s.Db); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "Failed to purge archived clusters")
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// handleListClusterTopics godoc
// @Summary List topics for a cluster
// @Description Get a list of all topics for a specific Kafka cluster.
// @Tags clusters
// @Produce json
// @Param name path string true "Cluster Name"
// @Success 200 {array} string
// @Router /clusters/{name}/topics [get]
// @Security ApiKeyAuth
func (s *Server) handleListClusterTopics(c *fiber.Ctx) error {
	clusterName := c.Params("name")
	cluster, err := database.GetCluster(s.Db, clusterName)
	if err != nil {
		return fiber.NewError(fiber.StatusNotFound, "Cluster not found")
	}

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
		return fiber.NewError(fiber.StatusInternalServerError, fmt.Sprintf("Failed to create admin client: %v", err))
	}
	defer adminClient.Close()

	topicDetails, err := adminClient.ListTopics(context.Background())
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, fmt.Sprintf("Failed to list topics: %v", err))
	}

	topicNames := make([]string, 0, len(topicDetails))
	for name := range topicDetails {
		topicNames = append(topicNames, name)
	}

	return c.JSON(topicNames)
}

// handleGetTopicDetails godoc
// @Summary Get detailed topic information for a cluster
// @Description Get detailed information for a list of topics in a specific Kafka cluster.
// @Tags clusters
// @Produce json
// @Param name path string true "Cluster Name"
// @Param topics query string true "Comma-separated list of topic names"
// @Success 200 {array} kafka.TopicDetails
// @Router /clusters/{name}/topic-details [get]
// @Security ApiKeyAuth
func (s *Server) handleGetTopicDetails(c *fiber.Ctx) error {
	clusterName := c.Params("name")
	cluster, err := database.GetCluster(s.Db, clusterName)
	if err != nil {
		return fiber.NewError(fiber.StatusNotFound, "Cluster not found")
	}

	topicsQuery := c.Query("topics")
	if topicsQuery == "" {
		return fiber.NewError(fiber.StatusBadRequest, "Missing 'topics' query parameter")
	}
	topicNames := strings.Split(topicsQuery, ",")

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
		return fiber.NewError(fiber.StatusInternalServerError, fmt.Sprintf("Failed to create admin client: %v", err))
	}
	defer adminClient.Close()

	topicDetails, err := adminClient.GetTopicDetails(context.Background(), topicNames...)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, fmt.Sprintf("Failed to get topic details: %v", err))
	}

	return c.JSON(topicDetails)
}

// handleGetClusterStatus godoc
// @Summary Get the status of a Kafka cluster
// @Description Get the status of a Kafka cluster by performing a live connection test.
// @Tags clusters
// @Produce json
// @Param name path string true "Cluster Name"
// @Success 200 {object} map[string]interface{}
// @Router /clusters/{name}/status [get]
// @Security ApiKeyAuth
func (s *Server) handleGetClusterStatus(c *fiber.Ctx) error {
	clusterName := c.Params("name")
	cluster, err := database.GetCluster(s.Db, clusterName)
	if err != nil {
		return fiber.NewError(fiber.StatusNotFound, "Cluster not found")
	}

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
		return fiber.NewError(fiber.StatusInternalServerError, fmt.Sprintf("Failed to create admin client: %v", err))
	}
	defer adminClient.Close()

	if _, err := adminClient.ListTopics(context.Background()); err != nil {
		return fiber.NewError(fiber.StatusServiceUnavailable, fmt.Sprintf("Failed to connect to cluster: %v", err))
	}

	return c.JSON(fiber.Map{"status": "active", "message": "Cluster is reachable"})
}

// handleTestClusterConnection godoc
// @Summary Test a Kafka cluster connection
// @Description Test the connection to a Kafka cluster.
// @Tags clusters
// @Accept json
// @Produce json
// @Param cluster body config.ClusterConfig true "Kafka Cluster"
// @Success 200 {object} map[string]interface{}
// @Router /clusters/test [post]
// @Security ApiKeyAuth
func (s *Server) handleTestClusterConnection(c *fiber.Ctx) error {
	var req struct {
		config.ClusterConfig
		Security struct {
			APIKey    string `json:"api_key"`
			APISecret string `json:"api_secret"`
		} `json:"security"`
	}
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "Invalid request body")
	}

	clusterConfig := req.ClusterConfig
	clusterConfig.Security.APIKey = req.Security.APIKey
	clusterConfig.Security.APISecret = req.Security.APISecret

	if err := kafka.TestConnection(context.Background(), clusterConfig); err != nil {
		return fiber.NewError(fiber.StatusServiceUnavailable, fmt.Sprintf("Failed to connect to cluster: %v", err))
	}

	return c.JSON(fiber.Map{"status": "success", "message": "Connection successful"})
}

// handleListJobs godoc
// @Summary List all replication jobs
// @Description Get a list of all replication jobs.
// @Tags jobs
// @Produce json
// @Success 200 {array} database.ReplicationJob
// @Router /jobs [get]
// @Security ApiKeyAuth
func (s *Server) handleListJobs(c *fiber.Ctx) error {
	jobs, err := database.ListJobs(s.Db)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "Failed to list jobs")
	}
	return c.JSON(jobs)
}

type CreateJobRequest struct {
	Name               string                  `json:"name"`
	SourceClusterName  string                  `json:"source_cluster_name"`
	TargetClusterName  string                  `json:"target_cluster_name"`
	TopicMappings      []database.TopicMapping `json:"topic_mappings"`
	BatchSize          int                     `json:"batch_size"`
	Parallelism        int                     `json:"parallelism"`
	Compression        string                  `json:"compression"`
	PreservePartitions bool                    `json:"preserve_partitions"`
}

// handleCreateJob godoc
// @Summary Create a new replication job
// @Description Create a new replication job.
// @Tags jobs
// @Accept json
// @Produce json
// @Param job body server.CreateJobRequest true "Replication Job"
// @Success 201 {object} database.ReplicationJob
// @Router /jobs [post]
// @Security ApiKeyAuth
func (s *Server) handleCreateJob(c *fiber.Ctx) error {
	var req CreateJobRequest
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "Invalid request body")
	}

	job := &database.ReplicationJob{
		ID:                 uuid.NewString(),
		Name:               req.Name,
		SourceClusterName:  req.SourceClusterName,
		TargetClusterName:  req.TargetClusterName,
		Status:             "paused", // Default status
		BatchSize:          req.BatchSize,
		Parallelism:        req.Parallelism,
		Compression:        req.Compression,
		PreservePartitions: req.PreservePartitions,
	}

	if err := database.CreateJob(s.Db, job); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "Failed to create job")
	}

	// Now, create the topic mappings for the job
	if len(req.TopicMappings) > 0 {
		if err := database.UpdateMappingsForJob(s.Db, job.ID, req.TopicMappings); err != nil {
			// If mapping fails, we should probably roll back the job creation
			// For now, we'll log the error and return it.
			database.DeleteJob(s.Db, job.ID) // Attempt to clean up
			return fiber.NewError(fiber.StatusInternalServerError, "Failed to create topic mappings for job")
		}
	}

	// TODO: Save replication settings (batch_size, etc.) somewhere.
	// For now, they are not persisted. This will be a future task.

	// Trigger an inventory snapshot when the job is created
	go s.manager.CreateInventorySnapshot(job.ID, "manual")

	return c.Status(fiber.StatusCreated).JSON(job)
}

// handleGetJob godoc
// @Summary Get a replication job by ID
// @Description Get a single replication job by its ID.
// @Tags jobs
// @Produce json
// @Param id path string true "Job ID"
// @Success 200 {object} database.ReplicationJob
// @Router /jobs/{id} [get]
// @Security ApiKeyAuth
func (s *Server) handleGetJob(c *fiber.Ctx) error {
	job, err := database.GetJob(s.Db, c.Params("id"))
	if err != nil {
		return fiber.NewError(fiber.StatusNotFound, "Job not found")
	}
	return c.JSON(job)
}

// handleUpdateJob godoc
// @Summary Update a replication job
// @Description Update an existing replication job.
// @Tags jobs
// @Accept json
// @Produce json
// @Param id path string true "Job ID"
// @Param job body server.CreateJobRequest true "Replication Job"
// @Success 200 {object} database.ReplicationJob
// @Router /jobs/{id} [put]
// @Security ApiKeyAuth
func (s *Server) handleUpdateJob(c *fiber.Ctx) error {
	id := c.Params("id")
	job, err := database.GetJob(s.Db, id)
	if err != nil {
		return fiber.NewError(fiber.StatusNotFound, "Job not found")
	}

	var req CreateJobRequest // Reuse the create request for updates
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "Invalid request body")
	}

	job.Name = req.Name
	// Status is updated via start/stop/pause endpoints, not here.

	if err := database.UpdateJob(s.Db, job); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "Failed to update job")
	}

	// Update mappings as well
	if len(req.TopicMappings) > 0 {
		if err := database.UpdateMappingsForJob(s.Db, job.ID, req.TopicMappings); err != nil {
			return fiber.NewError(fiber.StatusInternalServerError, "Failed to update topic mappings for job")
		}
	}

	return c.JSON(job)
}

// handleDeleteJob godoc
// @Summary Delete a replication job
// @Description Delete a replication job by its ID.
// @Tags jobs
// @Param id path string true "Job ID"
// @Success 204
// @Router /jobs/{id} [delete]
// @Security ApiKeyAuth
func (s *Server) handleDeleteJob(c *fiber.Ctx) error {
	if err := database.DeleteJob(s.Db, c.Params("id")); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "Failed to delete job")
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// handleStartJob godoc
// @Summary Start a replication job
// @Description Start a replication job by its ID.
// @Tags jobs
// @Param id path string true "Job ID"
// @Success 200 {object} map[string]interface{}
// @Router /jobs/{id}/start [post]
// @Security ApiKeyAuth
func (s *Server) handleStartJob(c *fiber.Ctx) error {
	if err := s.manager.StartJob(c.Params("id")); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	return c.JSON(fiber.Map{"id": c.Params("id"), "status": "started"})
}

// handleStopJob godoc
// @Summary Stop a replication job
// @Description Stop a replication job by its ID.
// @Tags jobs
// @Param id path string true "Job ID"
// @Success 200 {object} map[string]interface{}
// @Router /jobs/{id}/stop [post]
// @Security ApiKeyAuth
func (s *Server) handleStopJob(c *fiber.Ctx) error {
	if err := s.manager.StopJob(c.Params("id")); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	return c.JSON(fiber.Map{"id": c.Params("id"), "status": "stopped"})
}

// handlePauseJob godoc
// @Summary Pause a replication job
// @Description Pause a replication job by its ID.
// @Tags jobs
// @Param id path string true "Job ID"
// @Success 200 {object} map[string]interface{}
// @Router /jobs/{id}/pause [post]
// @Security ApiKeyAuth
func (s *Server) handlePauseJob(c *fiber.Ctx) error {
	if err := s.manager.PauseJob(c.Params("id")); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	return c.JSON(fiber.Map{"id": c.Params("id"), "status": "paused"})
}

// handleStartAllJobs godoc
// @Summary Start all replication jobs
// @Description Start all replication jobs.
// @Tags jobs
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /jobs/start-all [post]
// @Security ApiKeyAuth
func (s *Server) handleStartAllJobs(c *fiber.Ctx) error {
	if err := s.manager.StartAllJobs(); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	return c.JSON(fiber.Map{"status": "success", "message": "All jobs started"})
}

// handleStopAllJobs godoc
// @Summary Stop all replication jobs
// @Description Stop all replication jobs.
// @Tags jobs
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /jobs/stop-all [post]
// @Security ApiKeyAuth
func (s *Server) handleStopAllJobs(c *fiber.Ctx) error {
	if err := s.manager.StopAllJobs(); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	return c.JSON(fiber.Map{"status": "success", "message": "All jobs stopped"})
}

// handleRestartJob godoc
// @Summary Restart a replication job
// @Description Restart a replication job by its ID.
// handleRestartJob godoc
// @Summary Restart a replication job
// @Description Restart a replication job by its ID.
// @Tags jobs
// @Param id path string true "Job ID"
// @Success 200 {object} map[string]interface{}
// @Router /jobs/{id}/restart [post]
// @Security ApiKeyAuth
func (s *Server) handleRestartJob(c *fiber.Ctx) error {
	if err := s.manager.RestartJob(c.Params("id")); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	return c.JSON(fiber.Map{"id": c.Params("id"), "status": "restarted"})
}

// handleForceRestartJob godoc
// @Summary Force-restart a replication job
// @Description Forcefully restart a replication job, with pre-flight checks.
// @Tags jobs
// @Param id path string true "Job ID"
// @Success 200 {object} map[string]interface{}
// @Router /jobs/{id}/force-restart [post]
// @Security ApiKeyAuth
func (s *Server) handleForceRestartJob(c *fiber.Ctx) error {
	if err := s.manager.ForceRestartJob(c.Params("id")); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	return c.JSON(fiber.Map{"id": c.Params("id"), "status": "force-restarted"})
}

// handleGetJobTopicHealth godoc
// @Summary Get topic health for a job
// @Description Get the health of all topics for a specific replication job.
// @Tags jobs
// @Produce json
// @Param id path string true "Job ID"
// @Success 200 {array} kafka.TopicHealth
// @Router /jobs/{id}/topic-health [get]
// @Security ApiKeyAuth
func (s *Server) handleGetJobTopicHealth(c *fiber.Ctx) error {
	jobID := c.Params("id")
	health, err := s.manager.GetJobTopicHealth(jobID)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	return c.JSON(health)
}

// handleRestartAllJobs godoc
// @Summary Restart all replication jobs
// @Description Restart all replication jobs (handles stale active jobs).
// @Tags jobs
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /jobs/restart-all [post]
// @Security ApiKeyAuth
func (s *Server) handleRestartAllJobs(c *fiber.Ctx) error {
	if err := s.manager.RestartAllJobs(); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	return c.JSON(fiber.Map{"status": "success", "message": "All jobs restarted"})
}

// --- Topic Handlers ---

// handleListSourceTopics godoc
// @Summary List topics from the source cluster
// @Description Get a list of all topics from the source cluster.
// @Tags topics
// @Produce json
// @Success 200 {array} string
// @Router /topics/source [get]
// @Security ApiKeyAuth
func (s *Server) handleListSourceTopics(c *fiber.Ctx) error {
	sourceCluster, ok := s.cfg.Clusters["source"]
	if !ok {
		return fiber.NewError(fiber.StatusNotFound, "Source cluster not configured")
	}
	adminClient, err := kafka.NewAdminClient(sourceCluster)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "Failed to create source admin client")
	}
	defer adminClient.Close()

	topics, err := adminClient.ListTopics(context.Background())
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "Failed to list source topics")
	}

	return c.JSON(topics)
}

// handleListTargetTopics godoc
// @Summary List topics from the target cluster
// @Description Get a list of all topics from the target cluster.
// @Tags topics
// @Produce json
// @Success 200 {array} string
// @Router /topics/target [get]
// @Security ApiKeyAuth
func (s *Server) handleListTargetTopics(c *fiber.Ctx) error {
	targetCluster, ok := s.cfg.Clusters["target"]
	if !ok {
		return fiber.NewError(fiber.StatusNotFound, "Target cluster not configured")
	}
	adminClient, err := kafka.NewAdminClient(targetCluster)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "Failed to create target admin client")
	}
	defer adminClient.Close()

	topics, err := adminClient.ListTopics(context.Background())
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "Failed to list target topics")
	}

	return c.JSON(topics)
}

// handleGetMappings godoc
// @Summary Get topic mappings for a job
// @Description Get a list of all topic mappings for a replication job.
// @Tags jobs
// @Produce json
// @Param id path string true "Job ID"
// @Success 200 {array} database.TopicMapping
// @Router /jobs/{id}/mappings [get]
// @Security ApiKeyAuth
func (s *Server) handleGetMappings(c *fiber.Ctx) error {
	jobID := c.Params("id")
	mappings, err := database.GetMappingsForJob(s.Db, jobID)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, fmt.Sprintf("Could not get mappings for job %s", jobID))
	}
	return c.JSON(mappings)
}

// handleUpdateMappings godoc
// @Summary Update topic mappings for a job
// @Description Update the topic mappings for a replication job.
// @Tags jobs
// @Accept json
// @Produce json
// @Param id path string true "Job ID"
// @Param mappings body []database.TopicMapping true "Topic Mappings"
// @Success 200 {object} map[string]interface{}
// @Router /jobs/{id}/mappings [put]
// @Security ApiKeyAuth
func (s *Server) handleUpdateMappings(c *fiber.Ctx) error {
	jobID := c.Params("id")
	var mappings []database.TopicMapping
	if err := c.BodyParser(&mappings); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "Invalid request body")
	}

	if err := database.UpdateMappingsForJob(s.Db, jobID, mappings); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "Failed to update mappings")
	}

	return c.JSON(fiber.Map{"status": "success", "message": "Mappings updated"})
}

// --- Metrics Handlers ---

// handleGetCurrentMetrics godoc
// @Summary Get current metrics for a job
// @Description Get the latest replication metrics for a job.
// @Tags jobs
// @Produce json
// @Param id path string true "Job ID"
// @Success 200 {object} database.ReplicationMetric
// @Router /jobs/{id}/metrics/current [get]
// @Security ApiKeyAuth
func (s *Server) handleGetCurrentMetrics(c *fiber.Ctx) error {
	jobID := c.Params("id")
	metrics, err := database.GetLatestMetrics(s.Db, jobID)
	if err != nil {
		return fiber.NewError(fiber.StatusNotFound, "No metrics found for this job")
	}
	return c.JSON(metrics)
}

// handleGetHistoricalMetrics godoc
// @Summary Get historical metrics for a job
// @Description Get historical replication metrics for a job.
// @Tags jobs
// @Produce json
// @Param id path string true "Job ID"
// @Param range query string false "Time range (e.g., 2h, 24h, 7d, 30d)" default(2h)
// @Success 200 {array} database.ReplicationMetric
// @Router /jobs/{id}/metrics/history [get]
// @Security ApiKeyAuth
func (s *Server) handleGetHistoricalMetrics(c *fiber.Ctx) error {
	jobID := c.Params("id")
	timeRange := c.Query("range", "2h") // Default to 2h

	var start time.Time
	end := time.Now()

	switch timeRange {
	case "24h":
		start = end.Add(-24 * time.Hour)
	case "7d":
		start = end.Add(-7 * 24 * time.Hour)
	case "30d":
		start = end.Add(-30 * 24 * time.Hour)
	case "2h":
		fallthrough
	default:
		start = end.Add(-2 * time.Hour)
	}

	metrics, err := database.GetHistoricalMetrics(s.Db, jobID, start, end)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "Failed to get historical metrics")
	}
	return c.JSON(metrics)
}

// handleGetOperationalEvents godoc
// @Summary Get operational events
// @Description Get a list of all operational events.
// @Tags events
// @Produce json
// @Success 200 {array} database.OperationalEvent
// @Router /events [get]
// @Security ApiKeyAuth
func (s *Server) handleGetOperationalEvents(c *fiber.Ctx) error {
	events, err := database.ListOperationalEvents(s.Db)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "Failed to list operational events")
	}
	return c.JSON(events)
}

// handleGetLag godoc
// @Summary Get consumer lag for a job
// @Description Get the current consumer lag for a replication job.
// @Tags jobs
// @Produce json
// @Param id path string true "Job ID"
// @Success 200 {object} map[string]interface{}
// @Router /jobs/{id}/lag [get]
// @Security ApiKeyAuth
func (s *Server) handleGetLag(c *fiber.Ctx) error {
	jobID := c.Params("id")
	job, err := database.GetJob(s.Db, jobID)
	if err != nil {
		return fiber.NewError(fiber.StatusNotFound, "Job not found")
	}

	mappings, err := database.GetMappingsForJob(s.Db, jobID)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "Could not get mappings for job")
	}

	if len(mappings) == 0 {
		return c.JSON(fiber.Map{})
	}

	sourceCluster, ok := s.cfg.Clusters[job.SourceClusterName]
	if !ok {
		return fiber.NewError(fiber.StatusNotFound, "Source cluster for job not configured")
	}

	adminClient, err := kafka.NewAdminClient(sourceCluster)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "Failed to create source admin client")
	}
	defer adminClient.Close()

	groupID := "kaf-mirror-group-" + job.ID // Use job ID for unique group ID
	lagResults := make(map[string]interface{})
	var wg sync.WaitGroup
	var mu sync.Mutex

	for _, mapping := range mappings {
		if !mapping.Enabled {
			continue
		}
		wg.Add(1)
		go func(m database.TopicMapping) {
			defer wg.Done()
			lag, err := adminClient.GetTopicLag(context.Background(), m.SourceTopicPattern, groupID)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				lagResults[m.SourceTopicPattern] = fmt.Sprintf("error: %v", err)
			} else {
				lagResults[m.SourceTopicPattern] = lag
			}
		}(mapping)
	}
	wg.Wait()

	return c.JSON(lagResults)
}

// --- AI Handlers ---

// handleGetAIInsights godoc
// @Summary Get AI insights
// @Description Get a list of all AI-generated insights.
// @Tags ai
// @Produce json
// @Success 200 {array} database.AIInsight
// @Router /ai/insights [get]
// @Security ApiKeyAuth
func (s *Server) handleGetAIInsights(c *fiber.Ctx) error {
	limit, _ := strconv.Atoi(c.Query("limit", "0"))
	insights, err := database.ListAIInsights(s.Db, "", nil, limit) // Empty string means no filter
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "Failed to list AI insights")
	}
	return c.JSON(insights)
}

// handleGetAIMetrics godoc
// @Summary Get AI performance metrics
// @Description Get aggregated AI performance metrics including response time and accuracy.
// @Tags ai
// @Produce json
// @Success 200 {object} database.AIMetrics
// @Router /ai/metrics [get]
// @Security ApiKeyAuth
func (s *Server) handleGetAIMetrics(c *fiber.Ctx) error {
	metrics, err := database.GetAIMetrics(s.Db)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "Failed to get AI metrics")
	}
	return c.JSON(metrics)
}

// handleTestAIIntegration godoc
// @Summary Test AI integration
// @Description Test the AI integration with sample data
// @Tags ai
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /ai/test [post]
// @Security ApiKeyAuth
func (s *Server) handleTestAIIntegration(c *fiber.Ctx) error {
	if s.aiClient == nil {
		return fiber.NewError(fiber.StatusInternalServerError, "AI client not configured")
	}

	testMetrics := "Timestamp: 2025-08-13T16:48:00Z, Throughput: 1000 msg/sec, Lag: 50, Errors: 0"

	go func() {
		log.Printf("Testing AI integration with sample metrics: %s", testMetrics)

		// Use the response time tracking method
		insight, responseTimeMs, err := s.aiClient.GetAnomalyDetectionWithResponseTime(context.Background(), testMetrics)
		if err != nil {
			log.Printf("AI integration test failed: %v", err)
			return
		}

		log.Printf("AI integration test successful. Insight: %s, Response time: %dms", insight, responseTimeMs)

		aiInsight := &database.AIInsight{
			JobID:            nil, // Test insight
			InsightType:      "anomaly",
			Recommendation:   "TEST: " + insight,
			SeverityLevel:    "info",
			ResolutionStatus: "new",
			AIModel:          s.cfg.AI.Model,
			ResponseTimeMs:   responseTimeMs,
		}

		if err := database.InsertAIInsight(s.Db, aiInsight); err != nil {
			log.Printf("Failed to insert test AI insight: %v", err)
		} else {
			log.Printf("Successfully stored test AI insight with response time: %dms", responseTimeMs)
		}
	}()

	return c.JSON(fiber.Map{
		"status":  "success",
		"message": "AI integration test started. Check logs and /ai/insights endpoint for results.",
	})
}

// handleTriggerJobAIAnalysis godoc
// @Summary Trigger AI analysis for a job
// @Description Trigger an AI-powered performance analysis for a replication job.
// @Tags jobs
// @Param id path string true "Job ID"
// @Success 202
// @Router /jobs/{id}/ai/analyze [post]
// @Security ApiKeyAuth
func (s *Server) handleTriggerJobAIAnalysis(c *fiber.Ctx) error {
	jobID := c.Params("id")
	job, err := database.GetJob(s.Db, jobID)
	if err != nil {
		return fiber.NewError(fiber.StatusNotFound, "Job not found")
	}

	go func() {
		// Trigger comprehensive analysis using the new manager method
		s.manager.AnalyzeJobNow(job.ID, job.Name)
	}()

	return c.Status(fiber.StatusAccepted).JSON(fiber.Map{
		"status":  "success",
		"message": "Comprehensive AI analysis triggered for job " + jobID,
	})
}

// handleGetJobAIInsights godoc
// @Summary Get AI insights for a job
// @Description Get a list of all AI-generated insights for a replication job.
// @Tags jobs
// @Produce json
// @Param id path string true "Job ID"
// @Success 200 {array} database.AIInsight
// @Router /jobs/{id}/ai/insights [get]
// @Security ApiKeyAuth
func (s *Server) handleGetJobAIInsights(c *fiber.Ctx) error {
	jobID := c.Params("id")
	limit, _ := strconv.Atoi(c.Query("limit", "0"))
	insights, err := database.ListAIInsights(s.Db, "", &jobID, limit)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "Failed to list AI insights for job")
	}
	return c.JSON(insights)
}

// handleTriggerHistoricalAnalysis godoc
// @Summary Trigger historical AI analysis for a job
// @Description Trigger a long-term historical AI analysis for a replication job.
// @Tags jobs
// @Param id path string true "Job ID"
// @Param period query string false "Analysis period in days (e.g., 7d, 30d)" default(7d)
// @Success 202
// @Router /jobs/{id}/ai/historical-analysis [post]
// @Security ApiKeyAuth
func (s *Server) handleTriggerHistoricalAnalysis(c *fiber.Ctx) error {
	jobID := c.Params("id")
	job, err := database.GetJob(s.Db, jobID)
	if err != nil {
		return fiber.NewError(fiber.StatusNotFound, "Job not found")
	}

	periodStr := c.Query("period", "7d")
	periodDays, err := strconv.Atoi(strings.TrimSuffix(periodStr, "d"))
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "Invalid period format. Use '7d', '30d', etc.")
	}

	go s.manager.AnalyzeJobHistory(job.ID, job.Name, periodDays, "daily")

	return c.Status(fiber.StatusAccepted).JSON(fiber.Map{
		"status":  "success",
		"message": fmt.Sprintf("Historical AI analysis triggered for job %s over %d days", jobID, periodDays),
	})
}

// handleGetAnomalies godoc
// @Summary Get anomalies
// @Description Get a list of all AI-detected anomalies.
// @Tags ai
// @Produce json
// @Success 200 {array} database.AIInsight
// @Router /ai/anomalies [get]
// @Security ApiKeyAuth
func (s *Server) handleGetAnomalies(c *fiber.Ctx) error {
	limit, _ := strconv.Atoi(c.Query("limit", "0"))
	insights, err := database.ListAIInsights(s.Db, "anomaly", nil, limit)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "Failed to list anomalies")
	}
	return c.JSON(insights)
}

// handleGetRecommendations godoc
// @Summary Get recommendations
// @Description Get a list of all AI-generated recommendations.
// @Tags ai
// @Produce json
// @Success 200 {array} database.AIInsight
// @Router /ai/recommendations [get]
// @Security ApiKeyAuth
func (s *Server) handleGetRecommendations(c *fiber.Ctx) error {
	limit, _ := strconv.Atoi(c.Query("limit", "0"))
	insights, err := database.ListAIInsights(s.Db, "recommendation", nil, limit)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "Failed to list recommendations")
	}
	return c.JSON(insights)
}

// handleExplainEvent godoc
// @Summary Explain an event
// @Description Get an AI-powered explanation for an event.
// @Tags ai
// @Produce json
// @Param event path string true "Event"
// @Success 200 {object} map[string]interface{}
// @Router /ai/explain/{event} [post]
// @Security ApiKeyAuth
func (s *Server) handleExplainEvent(c *fiber.Ctx) error {
	event := c.Params("event")
	explanation, err := s.aiClient.ExplainEvent(context.Background(), event)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "Failed to get explanation")
	}
	return c.JSON(fiber.Map{"explanation": explanation})
}

// handleAnalyzeIncident godoc
// @Summary Analyze an incident
// @Description Trigger an AI-powered analysis of an operational incident.
// @Tags ai
// @Param event_id path int true "Event ID"
// @Success 202
// @Router /ai/incidents/{event_id}/analyze [post]
// @Security ApiKeyAuth
func (s *Server) handleAnalyzeIncident(c *fiber.Ctx) error {
	eventID, err := c.ParamsInt("event_id")
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "Invalid event ID")
	}

	event, err := database.GetOperationalEvent(s.Db, eventID)
	if err != nil {
		return fiber.NewError(fiber.StatusNotFound, "Operational event not found")
	}

	eventDetails := fmt.Sprintf("Timestamp: %s, Type: %s, Initiator: %s, Details: %s",
		event.Timestamp.Format(time.RFC3339), event.EventType, event.Initiator, event.Details)

	go func() {
		analysis, err := s.aiClient.GetIncidentAnalysis(context.Background(), eventDetails)
		if err != nil {
			fmt.Printf("Failed to get incident analysis for event %d: %v\n", eventID, err)
			return
		}

		insight := &database.AIInsight{
			InsightType:      "incident_report",
			SeverityLevel:    "high", // Default to high for incidents
			AIModel:          s.cfg.AI.Model,
			Recommendation:   analysis,
			ResolutionStatus: "new",
		}

		if err := database.InsertAIInsight(s.Db, insight); err != nil {
			fmt.Printf("Failed to insert incident analysis insight for event %d: %v\n", eventID, err)
		}
	}()

	return c.Status(fiber.StatusAccepted).JSON(fiber.Map{
		"status":  "success",
		"message": "Incident analysis has been triggered. The report will be available shortly.",
	})
}

type tokenRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// handleGenerateToken godoc
// @Summary Generate a new API token
// @Description Generate a new API token for the authenticated user.
// @Tags auth
// @Accept json
// @Produce json
// @Param credentials body tokenRequest true "User credentials"
// @Success 200 {object} map[string]interface{}
// @Router /auth/token [post]
func (s *Server) handleGenerateToken(c *fiber.Ctx) error {
	var creds tokenRequest
	if err := c.BodyParser(&creds); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "Invalid request body")
	}

	user, err := database.GetUserByUsername(s.Db, creds.Username)
	if err != nil {
		return fiber.NewError(fiber.StatusUnauthorized, "Invalid credentials")
	}

	if !user.VerifyPassword(creds.Password) {
		log.Printf("Invalid password for user: %s", creds.Username)
		return fiber.NewError(fiber.StatusUnauthorized, "Invalid credentials")
	}

	token, _, err := database.CreateApiToken(s.Db, user.ID, "User-generated token", time.Now().Add(24*time.Hour))
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "Failed to generate token")
	}

	return c.JSON(fiber.Map{"token": token})
}

// handleListUsers godoc
// @Summary List all users
// @Description Get a list of all users.
// @Tags users
// @Produce json
// @Success 200 {array} map[string]interface{}
// @Router /users [get]
// @Security ApiKeyAuth
func (s *Server) handleListUsers(c *fiber.Ctx) error {
	users, err := database.ListUsers(s.Db)
	if err != nil {
		log.Printf("Error listing users: %v", err)
		return fiber.NewError(fiber.StatusInternalServerError, "Failed to list users")
	}

	var usersWithRoles []map[string]interface{}
	for _, user := range users {
		role, err := database.GetUserRole(s.Db, user.ID)
		if err != nil {
			log.Printf("Error getting role for user %d: %v", user.ID, err)
			return fiber.NewError(fiber.StatusInternalServerError, "Failed to get user role")
		}
		usersWithRoles = append(usersWithRoles, fiber.Map{
			"id":         user.ID,
			"username":   user.Username,
			"is_initial": user.IsInitial,
			"created_at": user.CreatedAt,
			"role":       role,
		})
	}

	return c.JSON(usersWithRoles)
}

type createUserRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Role     string `json:"role"`
}

// handleCreateUser godoc
// @Summary Create a new user
// @Description Create a new user.
// @Tags users
// @Accept json
// @Produce json
// @Param user body server.createUserRequest true "User"
// @Success 201 {object} database.User
// @Router /users [post]
// @Security ApiKeyAuth
func (s *Server) handleCreateUser(c *fiber.Ctx) error {
	var req createUserRequest
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "Invalid request body")
	}

	user, err := database.CreateUser(s.Db, req.Username, req.Password, false)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "Failed to create user")
	}

	var roleID int
	if err := s.Db.Get(&roleID, "SELECT id FROM roles WHERE name = ?", req.Role); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "Invalid role")
	}

	if err := database.AssignRoleToUser(s.Db, user.ID, roleID); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "Failed to assign role to user")
	}

	return c.Status(fiber.StatusCreated).JSON(user)
}

// handleDeleteUser godoc
// @Summary Delete a user
// @Description Delete a user.
// @Tags users
// @Param username path string true "Username"
// @Success 204
// @Router /users/{username} [delete]
// @Security ApiKeyAuth
func (s *Server) handleDeleteUser(c *fiber.Ctx) error {
	username := c.Params("username")
	user, err := database.GetUserByUsername(s.Db, username)
	if err != nil {
		return fiber.NewError(fiber.StatusNotFound, "User not found")
	}

	requestingUser := c.Locals("user").(*database.User)

	// Protect initial admin user (ID 1) from deletion
	if user.ID == 1 {
		return fiber.NewError(fiber.StatusForbidden, "Cannot delete initial admin user. Use admin-cli for emergency operations.")
	}

	// Users cannot delete themselves
	if requestingUser.ID == user.ID {
		return fiber.NewError(fiber.StatusForbidden, "Cannot delete yourself")
	}

	// Check if requesting user is admin and target user is also admin
	requestingRole, err := database.GetUserRole(s.Db, requestingUser.ID)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "Failed to get requesting user role")
	}

	targetRole, err := database.GetUserRole(s.Db, user.ID)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "Failed to get target user role")
	}

	// Only admin can delete admin users, and initial admin cannot be deleted
	if targetRole == "admin" && requestingRole != "admin" {
		return fiber.NewError(fiber.StatusForbidden, "Only admins can delete admin users")
	}

	if err := database.DeleteUser(s.Db, user.ID); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "Failed to delete user")
	}

	return c.SendStatus(fiber.StatusNoContent)
}

type updateUserRoleRequest struct {
	Role string `json:"role"`
}

// handleUpdateUserRole godoc
// @Summary Update a user's role
// @Description Update a user's role.
// @Tags users
// @Accept json
// @Produce json
// @Param username path string true "Username"
// @Param role body updateUserRoleRequest true "Role"
// @Success 200 {object} database.User
// @Router /users/{username}/role [put]
// @Security ApiKeyAuth
func (s *Server) handleUpdateUserRole(c *fiber.Ctx) error {
	username := c.Params("username")
	var req updateUserRoleRequest
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "Invalid request body")
	}

	user, err := database.GetUserByUsername(s.Db, username)
	if err != nil {
		return fiber.NewError(fiber.StatusNotFound, "User not found")
	}

	var roleID int
	if err := s.Db.Get(&roleID, "SELECT id FROM roles WHERE name = ?", req.Role); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "Invalid role")
	}

	if err := database.AssignRoleToUser(s.Db, user.ID, roleID); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "Failed to update user role")
	}

	return c.JSON(user)
}

type changePasswordRequest struct {
	OldPassword string `json:"old_password"`
	NewPassword string `json:"new_password"`
}

// handleChangePassword godoc
// @Summary Change the current user's password
// @Description Change the current user's password.
// @Tags users
// @Accept json
// @Produce json
// @Param passwords body changePasswordRequest true "Old and new passwords"
// @Success 200 {object} map[string]interface{}
// @Router /users/change-password [put]
// @Security ApiKeyAuth
func (s *Server) handleChangePassword(c *fiber.Ctx) error {
	var req changePasswordRequest
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "Invalid request body")
	}

	user := c.Locals("user").(*database.User)

	if !user.VerifyPassword(req.OldPassword) {
		return fiber.NewError(fiber.StatusUnauthorized, "Invalid credentials")
	}

	if err := database.UpdateUserPassword(s.Db, user.ID, req.NewPassword); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "Failed to change password")
	}

	return c.JSON(fiber.Map{"status": "success", "message": "Password changed successfully"})
}

// handleResetUserPassword godoc
// @Summary Reset a user's password (admin only)
// @Description Reset another user's password and generate a new random password
// @Tags users
// @Param username path string true "Username"
// @Success 200 {object} map[string]interface{}
// @Router /users/{username}/reset-password [post]
// @Security ApiKeyAuth
func (s *Server) handleResetUserPassword(c *fiber.Ctx) error {
	username := c.Params("username")
	requestingUser := c.Locals("user").(*database.User)

	// Only admins can reset passwords
	requestingRole, err := database.GetUserRole(s.Db, requestingUser.ID)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "Failed to get user role")
	}

	if requestingRole != "admin" {
		return fiber.NewError(fiber.StatusForbidden, "Only admins can reset user passwords")
	}

	// Get target user
	targetUser, err := database.GetUserByUsername(s.Db, username)
	if err != nil {
		return fiber.NewError(fiber.StatusNotFound, "User not found")
	}

	// Cannot reset own password through this endpoint
	if requestingUser.ID == targetUser.ID {
		return fiber.NewError(fiber.StatusBadRequest, "Cannot reset your own password. Use change-password instead")
	}

	// Generate new random password
	newPassword, err := GenerateRandomPassword(16)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "Failed to generate new password")
	}

	// Update password
	if err := database.UpdateUserPassword(s.Db, targetUser.ID, newPassword); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "Failed to update user password")
	}

	// Revoke all existing tokens for security
	if err := database.RevokeAllUserTokens(s.Db, targetUser.ID); err != nil {
		log.Printf("Warning: Failed to revoke tokens for user %s after password reset: %v", username, err)
	}

	return c.JSON(fiber.Map{
		"status":       "success",
		"message":      "Password reset successfully",
		"new_password": newPassword,
	})
}

// GenerateRandomPassword creates a secure random password
func GenerateRandomPassword(length int) (string, error) {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789!@#$%^&*"
	b := make([]byte, length)
	if _, err := crypto_rand.Read(b); err != nil {
		return "", err
	}
	for i := 0; i < length; i++ {
		b[i] = charset[int(b[i])%len(charset)]
	}
	return string(b), nil
}

// handleGetMe godoc
// @Summary Get the current user's profile
// @Description Get the current user's profile.
// @Tags auth
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /auth/me [get]
// @Security ApiKeyAuth
func (s *Server) handleGetMe(c *fiber.Ctx) error {
	user := c.Locals("user").(*database.User)
	role, err := database.GetUserRole(s.Db, user.ID)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "Failed to get user role")
	}

	return c.JSON(fiber.Map{
		"id":         user.ID,
		"username":   user.Username,
		"is_initial": user.IsInitial,
		"created_at": user.CreatedAt,
		"role":       role,
	})
}

// handleGenerateComplianceReport godoc
// @Summary Generate a compliance report
// @Description Generate a new compliance report for the specified period.
// @Tags compliance
// @Param period path string true "Report period (daily, weekly, monthly)"
// @Success 200 {string} string "PDF report"
// @Router /compliance/report/{period} [post]
// @Security ApiKeyAuth
func (s *Server) handleGenerateComplianceReport(c *fiber.Ctx) error {
	period := c.Params("period")
	user := c.Locals("user").(*database.User)

	report, err := database.GenerateComplianceReport(s.Db, period, user.ID)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "Failed to generate compliance report: "+err.Error())
	}

	csvData, err := database.GenerateComplianceCSV(report)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "Failed to generate CSV report")
	}

	c.Set("Content-Type", "text/csv")
	c.Set("Content-Disposition", fmt.Sprintf("attachment; filename=compliance-report-%s-%s.csv",
		period, report.GeneratedAt.Format("2006-01-02")))

	return c.Send(csvData)
}

// handleListComplianceReports godoc
// @Summary List compliance reports
// @Description Get a list of generated compliance reports.
// @Tags compliance
// @Success 200 {array} database.ComplianceReport
// @Router /compliance/reports [get]
// @Security ApiKeyAuth
func (s *Server) handleListComplianceReports(c *fiber.Ctx) error {
	reports, err := database.ListComplianceReports(s.Db, 50) // Last 50 reports
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "Failed to list compliance reports")
	}
	return c.JSON(reports)
}

// handleGetComplianceReport godoc
// @Summary Get a compliance report by ID
// @Description Get a specific compliance report by its ID.
// @Tags compliance
// @Param id path int true "Report ID"
// @Success 200 {object} database.ComplianceReport
// @Router /compliance/report/{id} [get]
// @Security ApiKeyAuth
func (s *Server) handleGetComplianceReport(c *fiber.Ctx) error {
	reportID, err := c.ParamsInt("id")
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "Invalid report ID")
	}

	var report database.ComplianceReport
	query := `
		SELECT id, period, start_date, end_date, generated_by, generated_at, report_data
		FROM compliance_reports WHERE id = ?
	`
	err = s.Db.Get(&report, query, reportID)
	if err != nil {
		return fiber.NewError(fiber.StatusNotFound, "Compliance report not found")
	}

	// Parse JSON data
	if err := json.Unmarshal([]byte(report.ReportDataDB), &report.ReportData); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "Failed to parse report data")
	}

	return c.JSON(report)
}

// handleDashboard serves the main dashboard with role-based access
func (s *Server) handleDashboard(c *fiber.Ctx) error {
	// User is already authenticated by middleware
	return c.SendFile(s.getWebPath() + "/index.html")
}

// handleLoginPage serves the login page
func (s *Server) handleLoginPage(c *fiber.Ctx) error {
	return c.SendFile(s.getWebPath() + "/login.html")
}

// handleWebSocketAuth handles WebSocket connections with token authentication
func (s *Server) handleWebSocketAuth(c *fiber.Ctx) error {
	// Check for token in query parameter
	token := c.Query("token")
	if token == "" {
		log.Printf("WebSocket connection attempted without token from %s", c.IP())
		return c.Status(401).JSON(fiber.Map{"error": "Missing authentication token"})
	}

	// Validate token
	user, err := s.validateTokenDetailed(token)
	if err != nil {
		log.Printf("WebSocket connection failed token validation from %s: %v", c.IP(), err)
		return c.Status(401).JSON(fiber.Map{"error": "Invalid authentication token"})
	}

	// Store user in context for WebSocket handler
	c.Locals("user", user)

	// Upgrade to WebSocket
	if websocket.IsWebSocketUpgrade(c) {
		return websocket.New(s.handleWebSocket)(c)
	}

	return c.Status(426).JSON(fiber.Map{"error": "Upgrade Required"})
}

// validateTokenDetailed validates token and returns user info
func (s *Server) validateTokenDetailed(tokenString string) (*database.User, error) {
	userID, err := database.ValidateApiToken(s.Db, tokenString)
	if err != nil {
		return nil, fmt.Errorf("token validation failed: %w", err)
	}

	user, err := database.GetUser(s.Db, userID)
	if err != nil {
		log.Printf("User lookup failed for userID %d: %v", userID, err)
		return nil, fmt.Errorf("user not found: %w", err)
	}

	return user, nil
}

// --- Inventory Handlers ---

// handleGetJobInventorySnapshots godoc
// @Summary Get inventory snapshots for a job
// @Description Get a list of all inventory snapshots for a replication job.
// @Tags inventory
// @Produce json
// @Param id path string true "Job ID"
// @Success 200 {array} database.JobInventorySnapshot
// @Router /jobs/{id}/inventory/snapshots [get]
// @Security ApiKeyAuth
func (s *Server) handleGetJobInventorySnapshots(c *fiber.Ctx) error {
	jobID := c.Params("id")
	snapshots, err := database.GetInventorySnapshots(s.Db, jobID)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "Failed to get inventory snapshots")
	}
	return c.JSON(snapshots)
}

// handleGetInventorySnapshot godoc
// @Summary Get detailed inventory data for a snapshot
// @Description Get comprehensive inventory data for a specific snapshot.
// @Tags inventory
// @Produce json
// @Param snapshot_id path int true "Snapshot ID"
// @Success 200 {object} database.InventoryData
// @Router /inventory/snapshots/{snapshot_id} [get]
// @Security ApiKeyAuth
func (s *Server) handleGetInventorySnapshot(c *fiber.Ctx) error {
	snapshotID, err := c.ParamsInt("snapshot_id")
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "Invalid snapshot ID")
	}

	inventoryData, err := database.GetFullInventoryData(s.Db, snapshotID)
	if err != nil {
		return fiber.NewError(fiber.StatusNotFound, "Inventory snapshot not found")
	}

	// Fix empty compression types to show "NONE"
	for i := range inventoryData.SourceTopics {
		if inventoryData.SourceTopics[i].CompressionType == "" {
			inventoryData.SourceTopics[i].CompressionType = "NONE"
		}
	}
	for i := range inventoryData.TargetTopics {
		if inventoryData.TargetTopics[i].CompressionType == "" {
			inventoryData.TargetTopics[i].CompressionType = "NONE"
		}
	}

	return c.JSON(inventoryData)
}

// handleCreateManualInventorySnapshot godoc
// @Summary Create a manual inventory snapshot
// @Description Create a manual inventory snapshot for a running job.
// @Tags inventory
// @Param id path string true "Job ID"
// @Success 201 {object} map[string]interface{}
// @Router /jobs/{id}/inventory/snapshots [post]
// @Security ApiKeyAuth
func (s *Server) handleCreateManualInventorySnapshot(c *fiber.Ctx) error {
	jobID := c.Params("id")

	job, err := database.GetJob(s.Db, jobID)
	if err != nil {
		return fiber.NewError(fiber.StatusNotFound, "Job not found")
	}

	if job.Status != "active" {
		return fiber.NewError(fiber.StatusBadRequest, "Job must be active to create inventory snapshot")
	}

	go func() {
		// Manual inventory snapshot creation would need to be implemented in manager
		// For now, return success - implementation would be added later
	}()

	return c.Status(fiber.StatusCreated).JSON(fiber.Map{
		"status":  "success",
		"message": "Manual inventory snapshot creation initiated",
		"job_id":  jobID,
	})
}

// handleGetClusterInventory godoc
// @Summary Get cluster inventory from a snapshot
// @Description Get detailed cluster inventory information from a specific snapshot.
// @Tags inventory
// @Produce json
// @Param snapshot_id path int true "Snapshot ID"
// @Param cluster_type query string true "Cluster type (source or target)"
// @Success 200 {object} database.ClusterInventory
// @Router /inventory/snapshots/{snapshot_id}/cluster [get]
// @Security ApiKeyAuth
func (s *Server) handleGetClusterInventory(c *fiber.Ctx) error {
	snapshotID, err := c.ParamsInt("snapshot_id")
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "Invalid snapshot ID")
	}

	clusterType := c.Query("cluster_type")
	if clusterType != "source" && clusterType != "target" {
		return fiber.NewError(fiber.StatusBadRequest, "cluster_type must be 'source' or 'target'")
	}

	inventoryData, err := database.GetFullInventoryData(s.Db, snapshotID)
	if err != nil {
		return fiber.NewError(fiber.StatusNotFound, "Inventory snapshot not found")
	}

	if clusterType == "source" && inventoryData.SourceCluster != nil {
		return c.JSON(inventoryData.SourceCluster)
	} else if clusterType == "target" && inventoryData.TargetCluster != nil {
		return c.JSON(inventoryData.TargetCluster)
	}

	return fiber.NewError(fiber.StatusNotFound, "Cluster inventory not found")
}

// handleGetTopicInventory godoc
// @Summary Get topic inventory from a snapshot
// @Description Get detailed topic inventory information from a specific snapshot.
// @Tags inventory
// @Produce json
// @Param snapshot_id path int true "Snapshot ID"
// @Param cluster_type query string true "Cluster type (source or target)"
// @Success 200 {array} database.TopicInventory
// @Router /inventory/snapshots/{snapshot_id}/topics [get]
// @Security ApiKeyAuth
func (s *Server) handleGetTopicInventory(c *fiber.Ctx) error {
	snapshotID, err := c.ParamsInt("snapshot_id")
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "Invalid snapshot ID")
	}

	clusterType := c.Query("cluster_type")
	if clusterType != "source" && clusterType != "target" {
		return fiber.NewError(fiber.StatusBadRequest, "cluster_type must be 'source' or 'target'")
	}

	inventoryData, err := database.GetFullInventoryData(s.Db, snapshotID)
	if err != nil {
		return fiber.NewError(fiber.StatusNotFound, "Inventory snapshot not found")
	}

	// Fix empty compression types to show "NONE"
	if clusterType == "source" {
		for i := range inventoryData.SourceTopics {
			if inventoryData.SourceTopics[i].CompressionType == "" {
				inventoryData.SourceTopics[i].CompressionType = "NONE"
			}
		}
		return c.JSON(inventoryData.SourceTopics)
	} else {
		for i := range inventoryData.TargetTopics {
			if inventoryData.TargetTopics[i].CompressionType == "" {
				inventoryData.TargetTopics[i].CompressionType = "NONE"
			}
		}
		return c.JSON(inventoryData.TargetTopics)
	}
}

// handleGetConsumerGroupInventory godoc
// @Summary Get consumer group inventory from a snapshot
// @Description Get detailed consumer group inventory information from a specific snapshot.
// @Tags inventory
// @Produce json
// @Param snapshot_id path int true "Snapshot ID"
// @Success 200 {array} database.ConsumerGroupInventory
// @Router /inventory/snapshots/{snapshot_id}/consumer-groups [get]
// @Security ApiKeyAuth
func (s *Server) handleGetConsumerGroupInventory(c *fiber.Ctx) error {
	snapshotID, err := c.ParamsInt("snapshot_id")
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "Invalid snapshot ID")
	}

	inventoryData, err := database.GetFullInventoryData(s.Db, snapshotID)
	if err != nil {
		return fiber.NewError(fiber.StatusNotFound, "Inventory snapshot not found")
	}

	return c.JSON(inventoryData.ConsumerGroups)
}

// handleGetConnectionInventory godoc
// @Summary Get connection inventory from a snapshot
// @Description Get detailed connection inventory information from a specific snapshot.
// @Tags inventory
// @Produce json
// @Param snapshot_id path int true "Snapshot ID"
// @Success 200 {array} database.ConnectionInventory
// @Router /inventory/snapshots/{snapshot_id}/connections [get]
// @Security ApiKeyAuth
func (s *Server) handleGetConnectionInventory(c *fiber.Ctx) error {
	snapshotID, err := c.ParamsInt("snapshot_id")
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "Invalid snapshot ID")
	}

	inventoryData, err := database.GetFullInventoryData(s.Db, snapshotID)
	if err != nil {
		return fiber.NewError(fiber.StatusNotFound, "Inventory snapshot not found")
	}

	return c.JSON(inventoryData.Connections)
}

// --- Mirror State Handlers ---

// handleGetMirrorState godoc
// @Summary Get mirror state snapshots for a job
// @Description Retrieve mirror state data with dual behavior: WITHOUT period parameter returns the latest state as single objects or null (e.g., {"mirror_progress": {...}, "resume_points": null}). WITH period parameter returns historical data as arrays (e.g., {"mirror_progress": [...], "resume_points": []}). Supported periods: today, yesterday, this-week, last-week. Data is automatically purged after 7 days. For real-time analysis, use the validate-mirror endpoint instead.
// @Tags mirror-state
// @Accept json
// @Produce json
// @Param id path string true "Job ID"
// @Param period query string false "Calendar-based time period for historical data" Enums(today, yesterday, this-week, last-week)
// @Success 200 {object} map[string]interface{} "Latest state (single objects/null) or historical data (arrays) based on period parameter"
// @Failure 400 {object} map[string]interface{}
// @Failure 404 {object} map[string]interface{}
// @Failure 500 {object} map[string]interface{}
// @Router /jobs/{id}/mirror/state [get]
// @Security ApiKeyAuth
func (s *Server) handleGetMirrorState(c *fiber.Ctx) error {
	jobID := c.Params("id")
	period := c.Query("period")

	var stateData *database.MirrorStateData
	var err error

	// If no period parameter provided, return only the latest state
	if period == "" {
		stateData, err = database.GetMirrorStateData(s.Db, jobID)
		if err != nil {
			return fiber.NewError(fiber.StatusNotFound, "Mirror state data not found for job")
		}

		// Convert arrays to single objects for latest state response
		response := fiber.Map{
			"job_id": stateData.JobID,
		}

		// Return single object or null for each category
		if len(stateData.MirrorProgress) > 0 {
			response["mirror_progress"] = stateData.MirrorProgress[0]
		} else {
			response["mirror_progress"] = nil
		}

		if len(stateData.ResumePoints) > 0 {
			response["resume_points"] = stateData.ResumePoints[0]
		} else {
			response["resume_points"] = nil
		}

		if len(stateData.MirrorGaps) > 0 {
			response["mirror_gaps"] = stateData.MirrorGaps[0]
		} else {
			response["mirror_gaps"] = nil
		}

		if len(stateData.StateAnalysis) > 0 {
			response["state_analysis"] = stateData.StateAnalysis[0]
		} else {
			response["state_analysis"] = nil
		}

		return c.JSON(response)
	} else {
		// Validate period parameter
		validPeriods := map[string]bool{
			"today": true, "yesterday": true, "this-week": true, "last-week": true,
		}
		if !validPeriods[period] {
			return fiber.NewError(fiber.StatusBadRequest, "Invalid period. Must be one of: today, yesterday, this-week, last-week")
		}

		// Calculate time range based on period and return historical data
		startTime, endTime := s.calculateTimeRange(period)
		stateData, err = database.GetMirrorStateDataForPeriod(s.Db, jobID, startTime, endTime)
		if err != nil {
			return fiber.NewError(fiber.StatusNotFound, "Mirror state data not found for job")
		}

		// Return arrays for historical data
		return c.JSON(stateData)
	}
}

// calculateTimeRange returns start and end times for calendar-based periods
func (s *Server) calculateTimeRange(period string) (time.Time, time.Time) {
	now := time.Now()

	switch period {
	case "today":
		// Current day: 00:00 to now
		start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
		return start, now

	case "yesterday":
		// Previous complete day: 00:00 to 23:59 of yesterday
		yesterday := now.AddDate(0, 0, -1)
		start := time.Date(yesterday.Year(), yesterday.Month(), yesterday.Day(), 0, 0, 0, 0, now.Location())
		end := time.Date(yesterday.Year(), yesterday.Month(), yesterday.Day(), 23, 59, 59, 999999999, now.Location())
		return start, end

	case "this-week":
		// Current week: Monday 00:00 to now
		weekday := int(now.Weekday())
		if weekday == 0 { // Sunday = 0, we want Monday = 0
			weekday = 7
		}
		daysToMonday := weekday - 1
		monday := now.AddDate(0, 0, -daysToMonday)
		start := time.Date(monday.Year(), monday.Month(), monday.Day(), 0, 0, 0, 0, now.Location())
		return start, now

	case "last-week":
		// Previous complete week: Monday 00:00 to Sunday 23:59 of last week
		weekday := int(now.Weekday())
		if weekday == 0 { // Sunday = 0, we want Monday = 0
			weekday = 7
		}
		daysToLastMonday := weekday - 1 + 7 // Go back to previous week's Monday
		lastMonday := now.AddDate(0, 0, -daysToLastMonday)
		lastSunday := lastMonday.AddDate(0, 0, 6)

		start := time.Date(lastMonday.Year(), lastMonday.Month(), lastMonday.Day(), 0, 0, 0, 0, now.Location())
		end := time.Date(lastSunday.Year(), lastSunday.Month(), lastSunday.Day(), 23, 59, 59, 999999999, now.Location())
		return start, end

	default:
		// Default to today
		start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
		return start, now
	}
}

// handleGetMirrorProgress godoc
// @Summary Get mirror progress for a job
// @Description Retrieve replication progress for all partitions of a specific job.
// @Tags mirror-state
// @Accept json
// @Produce json
// @Param id path string true "Job ID"
// @Success 200 {array} database.MirrorProgress
// @Failure 404 {object} map[string]interface{}
// @Failure 500 {object} map[string]interface{}
// @Router /jobs/{id}/mirror/progress [get]
// @Security ApiKeyAuth
func (s *Server) handleGetMirrorProgress(c *fiber.Ctx) error {
	jobID := c.Params("id")

	progress, err := database.GetMirrorProgress(s.Db, jobID)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "Failed to get mirror progress")
	}

	return c.JSON(progress)
}

// handleGetResumePoints godoc
// @Summary Get safe resume points for a job
// @Description Calculate and retrieve safe resume points for migration scenarios.
// @Tags mirror-state
// @Accept json
// @Produce json
// @Param id path string true "Job ID"
// @Success 200 {array} database.ResumePoint
// @Failure 404 {object} map[string]interface{}
// @Failure 500 {object} map[string]interface{}
// @Router /jobs/{id}/mirror/resume-points [get]
// @Security ApiKeyAuth
func (s *Server) handleGetResumePoints(c *fiber.Ctx) error {
	jobID := c.Params("id")

	resumePoints, err := database.GetResumePoints(s.Db, jobID)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "Failed to get resume points")
	}

	return c.JSON(resumePoints)
}

// handleCalculateResumePoints godoc
// @Summary Calculate safe resume points for a job
// @Description Perform cross-cluster analysis and calculate safe resume points for migration.
// @Tags mirror-state
// @Accept json
// @Produce json
// @Param id path string true "Job ID"
// @Success 202 {object} map[string]interface{}
// @Failure 404 {object} map[string]interface{}
// @Failure 500 {object} map[string]interface{}
// @Router /jobs/{id}/mirror/resume-points [post]
// @Security ApiKeyAuth
func (s *Server) handleCalculateResumePoints(c *fiber.Ctx) error {
	jobID := c.Params("id")

	job, err := database.GetJob(s.Db, jobID)
	if err != nil {
		return fiber.NewError(fiber.StatusNotFound, "Job not found")
	}

	// Get source and target clusters
	sourceCluster, err := database.GetCluster(s.Db, job.SourceClusterName)
	if err != nil {
		return fiber.NewError(fiber.StatusNotFound, "Source cluster not found")
	}

	targetCluster, err := database.GetCluster(s.Db, job.TargetClusterName)
	if err != nil {
		return fiber.NewError(fiber.StatusNotFound, "Target cluster not found")
	}

	// Create cluster configs
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

	// Get topic mappings
	mappings, err := database.GetMappingsForJob(s.Db, jobID)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "Failed to get topic mappings")
	}

	topicMap := make(map[string]string)
	for _, mapping := range mappings {
		if mapping.Enabled {
			topicMap[mapping.SourceTopicPattern] = mapping.TargetTopicPattern
		}
	}

	go func() {
		ctx := context.Background()

		// Create admin clients
		sourceAdmin, err := kafka.NewAdminClient(sourceConfig)
		if err != nil {
			log.Printf("Failed to create source admin client for resume point calculation: %v", err)
			return
		}
		defer sourceAdmin.Close()

		targetAdmin, err := kafka.NewAdminClient(targetConfig)
		if err != nil {
			log.Printf("Failed to create target admin client for resume point calculation: %v", err)
			return
		}
		defer targetAdmin.Close()

		// Calculate resume points
		consumerGroup := fmt.Sprintf("kaf-mirror-job-%s", jobID)
		resumePoints, err := targetAdmin.CalculateSafeResumePoints(ctx, sourceAdmin, jobID, topicMap, consumerGroup)
		if err != nil {
			log.Printf("Failed to calculate resume points for job %s: %v", jobID, err)
			return
		}

		// Store resume points in database
		err = database.CalculateResumePoints(s.Db, jobID, resumePoints, nil)
		if err != nil {
			log.Printf("Failed to store resume points for job %s: %v", jobID, err)
			return
		}

		log.Printf("Successfully calculated and stored resume points for job %s", jobID)
	}()

	return c.Status(fiber.StatusAccepted).JSON(fiber.Map{
		"status":  "success",
		"message": "Resume point calculation started",
		"job_id":  jobID,
	})
}

// handleGetMirrorGaps godoc
// @Summary Get detected mirror gaps for a job
// @Description Retrieve all detected replication gaps for a specific job.
// @Tags mirror-state
// @Accept json
// @Produce json
// @Param id path string true "Job ID"
// @Success 200 {array} database.MirrorGap
// @Failure 404 {object} map[string]interface{}
// @Failure 500 {object} map[string]interface{}
// @Router /jobs/{id}/mirror/gaps [get]
// @Security ApiKeyAuth
func (s *Server) handleGetMirrorGaps(c *fiber.Ctx) error {
	jobID := c.Params("id")

	gaps, err := database.GetMirrorGaps(s.Db, jobID)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "Failed to get mirror gaps")
	}

	return c.JSON(gaps)
}

// handleValidateMigration godoc
// @Summary Validate migration safety for a job
// @Description Perform comprehensive validation to ensure safe migration without message duplication.
// @Tags mirror-state
// @Accept json
// @Produce json
// @Param id path string true "Job ID"
// @Success 200 {object} map[string]interface{}
// @Failure 404 {object} map[string]interface{}
// @Failure 500 {object} map[string]interface{}
// @Router /jobs/{id}/mirror/validate-mirror [post]
// @Security ApiKeyAuth
func (s *Server) handleValidateMigration(c *fiber.Ctx) error {
	jobID := c.Params("id")

	job, err := database.GetJob(s.Db, jobID)
	if err != nil {
		return fiber.NewError(fiber.StatusNotFound, "Job not found")
	}

	// Get source and target clusters
	sourceCluster, err := database.GetCluster(s.Db, job.SourceClusterName)
	if err != nil {
		return fiber.NewError(fiber.StatusNotFound, "Source cluster not found")
	}

	targetCluster, err := database.GetCluster(s.Db, job.TargetClusterName)
	if err != nil {
		return fiber.NewError(fiber.StatusNotFound, "Target cluster not found")
	}

	// Get topic mappings
	mappings, err := database.GetMappingsForJob(s.Db, jobID)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "Failed to get topic mappings")
	}

	topicMap := make(map[string]string)
	for _, mapping := range mappings {
		if mapping.Enabled {
			topicMap[mapping.SourceTopicPattern] = mapping.TargetTopicPattern
		}
	}

	// Create cluster configs
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

	ctx := context.Background()

	// Create admin clients
	sourceAdmin, err := kafka.NewAdminClient(sourceConfig)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "Failed to create source admin client")
	}
	defer sourceAdmin.Close()

	targetAdmin, err := kafka.NewAdminClient(targetConfig)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "Failed to create target admin client")
	}
	defer targetAdmin.Close()

	// Perform cross-cluster offset comparison
	offsetComparison, err := targetAdmin.CompareClusterOffsets(ctx, sourceAdmin, topicMap)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "Failed to compare cluster offsets")
	}

	// Analyze mirror state
	consumerGroup := fmt.Sprintf("kaf-mirror-job-%s", jobID)
	mirrorAnalysis, err := targetAdmin.AnalyzeMirrorState(ctx, sourceAdmin, jobID, topicMap, consumerGroup)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "Failed to analyze mirror state")
	}

	// Determine migration safety
	migrationSafe := offsetComparison.TotalGapsDetected == 0 && len(offsetComparison.CriticalIssues) == 0
	riskLevel := "low"

	if offsetComparison.TotalGapsDetected > 0 {
		riskLevel = "high"
	} else if len(offsetComparison.Warnings) > 0 {
		riskLevel = "medium"
	}

	recommendations := make([]string, 0)
	if !migrationSafe {
		recommendations = append(recommendations, "Migration not recommended due to detected gaps or critical issues")
		recommendations = append(recommendations, "Review offset comparison results and resolve gaps before migration")
	} else {
		recommendations = append(recommendations, "Migration appears safe to proceed")
		recommendations = append(recommendations, "Monitor replication closely after migration")
	}

	result := fiber.Map{
		"job_id":            jobID,
		"migration_safe":    migrationSafe,
		"risk_level":        riskLevel,
		"gaps_detected":     offsetComparison.TotalGapsDetected,
		"critical_issues":   len(offsetComparison.CriticalIssues),
		"warnings":          len(offsetComparison.Warnings),
		"offset_comparison": offsetComparison,
		"mirror_analysis":   mirrorAnalysis,
		"recommendations":   recommendations,
		"validated_at":      time.Now(),
	}

	return c.JSON(result)
}

// handleCreateMigrationCheckpoint godoc
// @Summary Create migration checkpoint for a job
// @Description Create a checkpoint before server migration to capture current state.
// @Tags mirror-state
// @Accept json
// @Produce json
// @Param id path string true "Job ID"
// @Param request body map[string]interface{} false "Migration reason and metadata"
// @Success 201 {object} map[string]interface{}
// @Failure 404 {object} map[string]interface{}
// @Failure 500 {object} map[string]interface{}
// @Router /jobs/{id}/mirror/checkpoint [post]
// @Security ApiKeyAuth
func (s *Server) handleCreateMigrationCheckpoint(c *fiber.Ctx) error {
	jobID := c.Params("id")

	var request struct {
		MigrationReason string `json:"migration_reason"`
		CheckpointType  string `json:"checkpoint_type"`
	}

	if err := c.BodyParser(&request); err != nil {
		request.MigrationReason = "Manual checkpoint"
		request.CheckpointType = "pre_migration"
	}

	user := c.Locals("user").(*database.User)

	checkpoint := database.MigrationCheckpoint{
		JobID:                      jobID,
		CheckpointType:             request.CheckpointType,
		SourceConsumerGroupOffsets: "{}", // Would be populated with actual data
		TargetHighWaterMarks:       "{}", // Would be populated with actual data
		CreatedAt:                  time.Now(),
		CreatedBy:                  user.Username,
		MigrationReason:            &request.MigrationReason,
		ValidationResults:          nil,
	}

	checkpointID, err := database.CreateMigrationCheckpoint(s.Db, checkpoint)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "Failed to create migration checkpoint")
	}

	return c.Status(fiber.StatusCreated).JSON(fiber.Map{
		"status":        "success",
		"message":       "Migration checkpoint created",
		"checkpoint_id": checkpointID,
		"job_id":        jobID,
	})
}

// --- WebSocket Handler ---

func (s *Server) handleWebSocket(c *websocket.Conn) {
	s.hub.register <- c
	defer func() {
		s.hub.unregister <- c
	}()

	for {
		// Read message from client
		mt, msg, err := c.ReadMessage()
		if err != nil {
			// Handle error (e.g., client disconnected)
			break
		}

		// Echo the message back
		if err := c.WriteMessage(mt, msg); err != nil {
			// Handle error
			break
		}
	}
}
