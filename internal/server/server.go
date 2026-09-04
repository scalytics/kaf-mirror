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
	"fmt"
	"kaf-mirror/internal/ai"
	"kaf-mirror/internal/config"
	"kaf-mirror/internal/database"
	"kaf-mirror/internal/manager"
	"kaf-mirror/internal/server/middleware"
	"kaf-mirror/pkg/logger"
	"log"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/jmoiron/sqlx"

	_ "kaf-mirror/web/docu/swagger"
)

// Server wraps the Fiber application.
type Server struct {
	App       *fiber.App
	cfg       *config.Config
	Db        *sqlx.DB
	manager   *manager.JobManager
	aiClient  *ai.Client
	hub       *Hub
	startTime time.Time
	Version   string
}

// New creates a new server instance.
func New(cfg *config.Config, db *sqlx.DB, manager *manager.JobManager, hub *Hub, version string) *Server {
	app := fiber.New()

	app.Use(middleware.Cors(cfg.Server.CORS.AllowedOrigins))

	var aiConfig config.AIConfig
	if dbConfig, err := database.LoadConfig(db); err == nil {
		aiConfig = dbConfig.AI
	} else {
		aiConfig = cfg.AI
	}

	s := &Server{
		App:       app,
		cfg:       cfg,
		Db:        db,
		manager:   manager,
		aiClient:  ai.NewClient(aiConfig),
		hub:       hub,
		startTime: time.Now(),
		Version:   version,
	}

	go hub.Run()
	s.setupRoutes()

	if cfg.Server.Mode != "test" {
		go func() {
			time.Sleep(2 * time.Second)
			if err := manager.RestartAllJobs(); err != nil {
				log.Printf("Error restarting jobs on server startup: %v", err)
			}
		}()
	}

	return s
}

// Start runs the Fiber server.
func (s *Server) Start() error {
	if err := s.cfg.Server.RequireSecureListen(); err != nil {
		return err
	}
	addr := s.cfg.Server.ListenAddr()

	if s.cfg.Server.TLS.Enabled {
		certFile := s.cfg.Server.TLS.CertFile
		keyFile := s.cfg.Server.TLS.KeyFile

		if certFile == "" || keyFile == "" {
			return fmt.Errorf("TLS is enabled but certificate or key file path is missing")
		}

		logger.Info("Starting HTTPS server on %s with TLS certificate: %s", addr, certFile)
		return s.App.ListenTLS(addr, certFile, keyFile)
	}

	logger.Info("Starting HTTP server on %s", addr)
	return s.App.Listen(addr)
}

// getWebPath returns the configured web path with fallback to development default
func (s *Server) getWebPath() string {
	if s.cfg.Server.WebPath != "" {
		return s.cfg.Server.WebPath
	}
	return "./web"
}

// Shutdown gracefully shuts down the server.
func (s *Server) Shutdown() error {
	return s.App.Shutdown()
}
