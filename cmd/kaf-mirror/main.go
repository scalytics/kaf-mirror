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

// @title kaf-mirror API
// @version 1.3.1
// @description This is the API for kaf-mirror, a high-performance Kafka replication tool.
// @host localhost:8080
// @BasePath /api/v1
// @securityDefinitions.apikey ApiKeyAuth
// @in header
// @name Authorization
package main

import (
	"context"
	"errors"
	"fmt"
	"kaf-mirror/internal/config"
	"kaf-mirror/internal/database"
	"kaf-mirror/internal/manager"
	"kaf-mirror/internal/server"
	"kaf-mirror/pkg/logger"
	"kaf-mirror/pkg/utils"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

var (
	Version string
)

func writeSecretFile(dir string, prefix string, secret string) (string, error) {
	file, err := os.CreateTemp(dir, prefix)
	if err != nil {
		return "", err
	}
	defer file.Close()

	if err := file.Chmod(0600); err != nil {
		return "", err
	}
	if _, err := file.WriteString(secret + "\n"); err != nil {
		return "", err
	}
	if err := file.Sync(); err != nil {
		return "", err
	}

	return filepath.Abs(file.Name())
}

func main() {
	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}
	fmt.Println("Configuration loaded successfully.")

	if err := logger.InitializeFromConfig(cfg.Logging.File, cfg.Logging.Level, cfg.Logging.Console); err != nil {
		log.Fatalf("Failed to initialize logger: %v", err)
	}
	logger.Info("Logger initialized with level %s, console=%t", cfg.Logging.Level, cfg.Logging.Console)

	shutdownTimeout, err := cfg.Server.ShutdownTimeoutDuration()
	if err != nil {
		log.Fatalf("Invalid configuration: %v", err)
	}

	db, dbLock, err := database.OpenLockedDB(cfg.Database.Path)
	if err != nil {
		log.Fatalf("Refusing to start: %v. If a restore or import is running, wait for it to finish. If another kaf-mirror process holds the database, stop it first.", err)
	}
	defer db.Close()
	if dbLock != nil {
		defer dbLock.Close()
	}

	// Check if there are any users in the database
	users, err := database.ListUsers(db)
	if err != nil {
		log.Fatalf("Failed to check for users: %v", err)
	}
	if len(users) == 0 {
		fmt.Println("No users found in the database. Seeding default roles and creating initial admin user...")
		if err := database.SeedDefaultRolesAndPermissions(db); err != nil {
			log.Fatalf("Failed to seed default roles and permissions: %v", err)
		}

		password, err := utils.GenerateRandomPassword(16)
		if err != nil {
			log.Fatalf("Failed to generate password for initial admin: %v", err)
		}

		user, err := database.CreateUser(db, "admin@localhost", password, true)
		if err != nil {
			log.Fatalf("Failed to create initial admin user: %v", err)
		}
		secretFile, err := writeSecretFile(filepath.Dir(cfg.Database.Path), "kaf-mirror-bootstrap-*", password)
		if err != nil {
			log.Fatalf("Failed to write password file: %v", err)
		}

		var adminRoleID int
		if err := db.Get(&adminRoleID, "SELECT id FROM roles WHERE name = 'admin'"); err != nil {
			log.Fatalf("Failed to find admin role: %v", err)
		}
		if err := database.AssignRoleToUser(db, user.ID, adminRoleID); err != nil {
			log.Fatalf("Failed to assign admin role: %v", err)
		}

		fmt.Println("=================================================================")
		fmt.Println("  INITIAL ADMIN USER CREATED")
		fmt.Println("=================================================================")
		fmt.Printf("  Username: %s\n", user.Username)
		fmt.Println("  Password: [REDACTED]")
		fmt.Printf("  Password File: %s\n", secretFile)
		fmt.Println("=================================================================")
		fmt.Println("  Deliver the password securely, then delete the file.")
		fmt.Println("=================================================================")
	}
	fmt.Println("Database initialized successfully.")

	// Initialize the Hub and JobManager
	hub := server.NewHub()
	jobManager := manager.New(db, cfg, hub)

	if err := cfg.Server.RequireSecureListen(); err != nil {
		log.Fatal(err)
	}

	srv := server.New(cfg, db, jobManager, hub, Version)
	go func() {
		fmt.Println("Starting API server on", cfg.Server.ListenAddr())
		if err := srv.Start(); err != nil {
			log.Printf("API server error: %v", err)
		}
	}()
	fmt.Println("API server started successfully.")

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	if err := waitAndShutdown(signals, shutdownTimeout, srv.ShutdownWithContext, jobManager.Shutdown); err != nil {
		log.Printf("Shutdown incomplete: %v", err)
	}
	fmt.Println("Shutdown complete.")
}

func waitAndShutdown(signals <-chan os.Signal, timeout time.Duration, steps ...func(context.Context) error) error {
	sig := <-signals
	fmt.Printf("Received %s, shutting down within %s...\n", sig, timeout)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	errs := make([]error, len(steps))
	var wg sync.WaitGroup
	for i, step := range steps {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = step(ctx)
		}()
	}
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return errors.Join(errs...)
	case <-ctx.Done():
		return fmt.Errorf("shutdown did not finish within %s: %w", timeout, ctx.Err())
	}
}
