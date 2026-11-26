// Copyright 2025 Alexander Alten (2pk03) and Scalytics
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"errors"
	"fmt"
	"io"
	"kaf-mirror/internal/config"
	"kaf-mirror/internal/database"
	"kaf-mirror/pkg/utils"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var db *sqlx.DB

var errOut io.Writer = os.Stderr

func configFileNotFound(err error) bool {
	var missing viper.ConfigFileNotFoundError
	return errors.As(err, &missing)
}

func copyFile(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	if _, err := os.Stat(dst); err == nil {
		if err := os.Chmod(dst, privateFileMode); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}

	destFile, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, privateFileMode)
	if err != nil {
		return err
	}
	if err := destFile.Chmod(privateFileMode); err != nil {
		destFile.Close()
		return err
	}
	if _, err := io.Copy(destFile, sourceFile); err != nil {
		destFile.Close()
		return err
	}
	if err := destFile.Sync(); err != nil {
		destFile.Close()
		return err
	}
	return destFile.Close()
}

func writeSecretTempFile(prefix string, secret string) (string, error) {
	file, err := os.CreateTemp("", prefix)
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

	return file.Name(), nil
}

func newImportDatabaseCmd(dbPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "database [source-path]",
		Short: "Import database from external kaf-mirror instance.",
		Long:  `Replace the current database with the database of another kaf-mirror instance. Stop kaf-mirror first. Without a terminal, pass --yes.`,
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			sourcePath := args[0]
			if _, err := os.Stat(sourcePath); os.IsNotExist(err) {
				log.Fatalf("Source database not found: %s", sourcePath)
			}

			confirm, err := confirmAction(cmd, "This will replace the current database. Continue?")
			if err != nil {
				log.Fatalf("Import cancelled: %v", err)
			}
			if !confirm {
				fmt.Println("Import cancelled.")
				return
			}

			closeDB()
			if err := restoreDatabase(sourcePath, *dbPath); err != nil {
				log.Fatalf("Failed to import database: %v", err)
			}
			fmt.Printf("Database imported from: %s\n", sourcePath)
		},
	}
}

func newAddUserCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "add [username]",
		Short: "Add a new user to the database.",
		Long:  `This command adds a new user directly to the database. It asks for the password without echo, or reads the first line of stdin when there is no terminal.`,
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			password, err := readPassword("Password:")
			if err != nil {
				log.Fatalf("Password is required: %v", err)
			}
			isInitial, _ := cmd.Flags().GetBool("initial")
			user, err := database.CreateUser(db, args[0], password, isInitial)
			if err != nil {
				log.Fatalf("Failed to create user: %v", err)
			}
			fmt.Printf("User created successfully: %s (ID: %d)\n", user.Username, user.ID)
		},
	}
	cmd.Flags().Bool("initial", false, "Set user as initial admin")
	return cmd
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func run(args []string) error {
	var dbPath string
	var loadedConfigPath string

	var rootCmd = &cobra.Command{
		Use:   "admin-cli",
		Short: "A CLI for bootstrapping and emergency maintenance of kaf-mirror.",
		Long: `admin-cli is a command-line tool for bootstrapping the application
and for emergency maintenance. It interacts directly with the database.`,
		SilenceErrors: true,
		SilenceUsage:  true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			if cmd.Name() == "help" {
				return nil
			}
			loadedConfigPath = ""
			dbFlag := cmd.Flags().Lookup("db-path")
			dbSet := dbFlag != nil && dbFlag.Changed
			if os.Getenv("CONFIG_PATH") != "" || !dbSet {
				viper.Reset()
				cfg, err := config.LoadConfig()
				if err != nil {
					if os.Getenv("CONFIG_PATH") != "" || !configFileNotFound(err) {
						return fmt.Errorf("failed to load configuration: %w", err)
					}
				} else {
					loadedConfigPath = viper.ConfigFileUsed()
					if !dbSet && cfg.Database.Path != "" {
						dbPath = cfg.Database.Path
					}
				}
			}
			if dbPath == "" {
				return fmt.Errorf("database path is required: pass --db-path or provide a config file")
			}
			var err error
			db, err = database.InitDB(dbPath)
			if err != nil {
				return fmt.Errorf("failed to connect to database: %w", err)
			}
			return nil
		},
		PersistentPostRun: func(cmd *cobra.Command, args []string) {
			if db != nil {
				db.Close()
				db = nil
			}
		},
	}
	rootCmd.PersistentFlags().StringVar(&dbPath, "db-path", "", "Path to the SQLite database file. Defaults to database.path from the config file.")

	var usersCmd = &cobra.Command{
		Use:   "users",
		Short: "Manage users directly in the database.",
		Long:  `The users command allows you to add and list users directly in the database. This is useful for initial setup and recovery.`,
	}

	addUserCmd := newAddUserCmd()

	var listUsersCmd = &cobra.Command{
		Use:   "list",
		Short: "List all users in the database.",
		Long:  `This command lists all users directly from the database.`,
		Run: func(cmd *cobra.Command, args []string) {
			users, err := database.ListUsers(db)
			if err != nil {
				log.Fatalf("Failed to list users: %v", err)
			}
			fmt.Println("ID\tUsername\tCreated At")
			for _, user := range users {
				fmt.Printf("%d\t%s\t%s\n", user.ID, user.Username, user.CreatedAt.Format(time.RFC3339))
			}
		},
	}

	usersCmd.AddCommand(addUserCmd, listUsersCmd)

	var tokensCmd = &cobra.Command{
		Use:   "tokens",
		Short: "Manage API tokens directly in the database.",
		Long:  `The tokens command allows you to generate API tokens for users directly in the database.`,
	}

	var generateTokenCmd = &cobra.Command{
		Use:   "generate [username]",
		Short: "Generate a new API token for a user.",
		Long:  `This command generates a new API token for the specified user directly in the database.`,
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			description, _ := cmd.Flags().GetString("description")
			duration, _ := cmd.Flags().GetDuration("duration")

			user, err := database.GetUserByUsername(db, args[0])
			if err != nil {
				log.Fatalf("Failed to find user: %v", err)
			}

			expiresAt := time.Now().Add(duration)
			token, _, err := database.CreateApiToken(db, user.ID, description, expiresAt)
			if err != nil {
				log.Fatalf("Failed to generate token: %v", err)
			}

			fmt.Println("Generated API Token (this will only be shown once):")
			fmt.Println(token)
		},
	}
	generateTokenCmd.Flags().String("description", "New API Token", "Description for the token")
	generateTokenCmd.Flags().Duration("duration", 365*24*time.Hour, "Duration for which the token is valid")

	var resetTokenCmd = &cobra.Command{
		Use:   "reset [username]",
		Short: "Reset a user's API token.",
		Long:  `This command revokes all existing API tokens for the specified user and generates a new one.`,
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			description, _ := cmd.Flags().GetString("description")
			duration, _ := cmd.Flags().GetDuration("duration")

			user, err := database.GetUserByUsername(db, args[0])
			if err != nil {
				log.Fatalf("Failed to find user: %v", err)
			}

			confirm, err := confirmAction(cmd, "You are about to reset ALL tokens for this user. Are you sure?")
			if err != nil {
				log.Fatalf("Operation cancelled: %v", err)
			}
			if !confirm {
				fmt.Println("Operation cancelled.")
				return
			}

			confirm, err = confirmAction(cmd, "Last Reminder: This will invalidate all existing tokens for this user. Are you really sure?")
			if err != nil {
				log.Fatalf("Operation cancelled: %v", err)
			}
			if !confirm {
				fmt.Println("Operation cancelled.")
				return
			}

			if err := database.RevokeAllUserTokens(db, user.ID); err != nil {
				log.Fatalf("Failed to revoke existing tokens: %v", err)
			}

			expiresAt := time.Now().Add(duration)
			token, _, err := database.CreateApiToken(db, user.ID, description, expiresAt)
			if err != nil {
				log.Fatalf("Failed to generate token: %v", err)
			}

			fmt.Println("Generated API Token (this will only be shown once):")
			fmt.Println(token)
		},
	}
	resetTokenCmd.Flags().String("description", "New API Token", "Description for the token")
	resetTokenCmd.Flags().Duration("duration", 365*24*time.Hour, "Duration for which the token is valid")
	addYesFlag(resetTokenCmd)

	tokensCmd.AddCommand(generateTokenCmd, resetTokenCmd)

	var resetAdminPasswordCmd = &cobra.Command{
		Use:   "reset-admin-password [username]",
		Short: "Reset an admin user's password.",
		Long:  `This command resets the password for the initial admin user, generating a new random password.`,
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			user, err := database.GetUserByUsername(db, args[0])
			if err != nil {
				log.Fatalf("Failed to find user: %v", err)
			}

			if !user.IsInitial {
				log.Fatalf("Password can only be reset for the initial admin user.")
			}

			password, err := utils.GenerateRandomPassword(16)
			if err != nil {
				log.Fatalf("Failed to generate a random password: %v", err)
			}

			if err := database.UpdateUserPassword(db, user.ID, password); err != nil {
				log.Fatalf("Failed to reset password: %v", err)
			}
			secretFile, err := writeSecretTempFile("kaf-mirror-admin-reset-*", password)
			if err != nil {
				log.Fatalf("Failed to write password file: %v", err)
			}

			fmt.Println("=================================================================")
			fmt.Println("  ADMIN PASSWORD RESET")
			fmt.Println("=================================================================")
			fmt.Printf("  Username: %s\n", user.Username)
			fmt.Println("  New Password: [REDACTED]")
			fmt.Printf("  Password File: %s\n", secretFile)
			fmt.Println("=================================================================")
			fmt.Println("  Deliver the password securely, then delete the file.")
			fmt.Println("=================================================================")
		},
	}

	var repairCmd = &cobra.Command{
		Use:   "repair",
		Short: "Repair RBAC seeds and bootstrap an admin if none exists.",
		Long:  `Re-seeds default roles/permissions if missing and creates an initial admin user with a random password when no users exist.`,
		Run: func(cmd *cobra.Command, args []string) {
			if err := database.SeedDefaultRolesAndPermissions(db); err != nil {
				log.Fatalf("Failed to seed default roles and permissions: %v", err)
			}
			fmt.Println("Default roles and permissions ensured.")

			users, err := database.ListUsers(db)
			if err != nil {
				log.Fatalf("Failed to list users: %v", err)
			}
			if len(users) > 0 {
				fmt.Println("Users already exist; no admin bootstrap performed.")
				return
			}

			password, err := utils.GenerateRandomPassword(16)
			if err != nil {
				log.Fatalf("Failed to generate a random password: %v", err)
			}

			user, err := database.CreateUser(db, "admin@localhost", password, true)
			if err != nil {
				log.Fatalf("Failed to create admin user: %v", err)
			}
			secretFile, err := writeSecretTempFile("kaf-mirror-bootstrap-*", password)
			if err != nil {
				log.Fatalf("Failed to write password file: %v", err)
			}

			var adminRoleID int
			if err := db.Get(&adminRoleID, "SELECT id FROM roles WHERE name = 'admin'"); err != nil {
				log.Fatalf("Failed to find admin role: %v", err)
			}
			if err := database.AssignRoleToUser(db, user.ID, adminRoleID); err != nil {
				log.Fatalf("Failed to assign admin role to user: %v", err)
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
		},
	}

	var backupCmd = &cobra.Command{
		Use:   "backup",
		Short: "Backup database and configuration files.",
		Long:  `Create backups of the database, configuration files, or complete system backups.`,
	}

	var backupDatabaseCmd = &cobra.Command{
		Use:   "database [output-path]",
		Short: "Backup the SQLite database.",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			outputPath := args[0]
			timestamp := time.Now().Format("20060102-150405")

			if !strings.HasSuffix(outputPath, ".db") {
				outputPath = fmt.Sprintf("%s-backup-%s.db", strings.TrimSuffix(outputPath, filepath.Ext(outputPath)), timestamp)
			}

			if err := backupDatabase(db, outputPath); err != nil {
				log.Fatalf("Failed to backup database: %v", err)
			}

			fmt.Printf("Database backup created: %s\n", outputPath)
		},
	}

	var backupConfigCmd = &cobra.Command{
		Use:   "config [output-path]",
		Short: "Backup configuration file.",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			configPath, _ := cmd.Flags().GetString("config-path")
			outputPath := args[0]
			timestamp := time.Now().Format("20060102-150405")

			if _, err := os.Stat(configPath); os.IsNotExist(err) {
				log.Fatalf("Configuration file not found: %s", configPath)
			}

			if !strings.HasSuffix(outputPath, ".yml") {
				outputPath = fmt.Sprintf("%s-config-backup-%s.yml", strings.TrimSuffix(outputPath, filepath.Ext(outputPath)), timestamp)
			}

			if err := copyPrivateFile(configPath, outputPath); err != nil {
				log.Fatalf("Failed to backup configuration: %v", err)
			}

			fmt.Printf("Configuration backup created: %s\n", outputPath)
		},
	}
	backupConfigCmd.Flags().String("config-path", "configs/default.yml", "Path to configuration file")

	var backupFullCmd = &cobra.Command{
		Use:   "full [output-directory]",
		Short: "Create complete system backup.",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			outputDir := args[0]
			configPath, _ := cmd.Flags().GetString("config-path")
			if !cmd.Flags().Changed("config-path") && loadedConfigPath != "" {
				configPath = loadedConfigPath
			}
			timestamp := time.Now().Format("20060102-150405")
			backupDir := filepath.Join(outputDir, fmt.Sprintf("kaf-mirror-backup-%s", timestamp))

			if err := os.MkdirAll(backupDir, 0700); err != nil {
				log.Fatalf("Failed to create backup directory: %v", err)
			}
			if err := os.Chmod(backupDir, 0700); err != nil {
				log.Fatalf("Failed to set backup directory permissions: %v", err)
			}

			dbBackupPath := filepath.Join(backupDir, "database.db")
			if err := backupDatabase(db, dbBackupPath); err != nil {
				log.Fatalf("Failed to backup database: %v", err)
			}
			fmt.Printf("Database backed up to: %s\n", dbBackupPath)

			if _, err := os.Stat(configPath); err != nil {
				fmt.Fprintf(errOut, "Warning: configuration %s was not included in the backup: %v\n", configPath, err)
			} else {
				configBackupPath := filepath.Join(backupDir, "config.yml")
				if err := copyPrivateFile(configPath, configBackupPath); err != nil {
					log.Fatalf("Failed to backup configuration: %v", err)
				}
				fmt.Printf("Configuration backed up to: %s\n", configBackupPath)
			}

			fmt.Printf("Full backup created in: %s\n", backupDir)
		},
	}
	backupFullCmd.Flags().String("config-path", "configs/default.yml", "Path to configuration file")

	var restoreCmd = &cobra.Command{
		Use:   "restore",
		Short: "Restore database and configuration from backups.",
		Long:  `Restore the database, configuration files, or complete system from backups.`,
	}

	var restoreDatabaseCmd = &cobra.Command{
		Use:   "database [backup-path]",
		Short: "Restore database from backup.",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			backupPath := args[0]

			if _, err := os.Stat(backupPath); os.IsNotExist(err) {
				log.Fatalf("Backup file not found: %s", backupPath)
			}

			confirm, err := confirmAction(cmd, "This will overwrite the current database. Are you sure?")
			if err != nil {
				log.Fatalf("Restore cancelled: %v", err)
			}
			if !confirm {
				fmt.Println("Restore cancelled.")
				return
			}

			closeDB()
			if err := restoreDatabase(backupPath, dbPath); err != nil {
				log.Fatalf("Failed to restore database: %v", err)
			}

			fmt.Printf("Database restored from: %s\n", backupPath)
		},
	}

	var restoreConfigCmd = &cobra.Command{
		Use:   "config [backup-path]",
		Short: "Restore configuration from backup.",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			backupPath := args[0]
			configPath, _ := cmd.Flags().GetString("config-path")

			if _, err := os.Stat(backupPath); os.IsNotExist(err) {
				log.Fatalf("Backup file not found: %s", backupPath)
			}

			confirm, err := confirmAction(cmd, "This will overwrite the current configuration. Are you sure?")
			if err != nil {
				log.Fatalf("Restore cancelled: %v", err)
			}
			if !confirm {
				fmt.Println("Restore cancelled.")
				return
			}

			if err := copyFile(backupPath, configPath); err != nil {
				log.Fatalf("Failed to restore configuration: %v", err)
			}

			fmt.Printf("Configuration restored from: %s\n", backupPath)
		},
	}
	restoreConfigCmd.Flags().String("config-path", "configs/default.yml", "Path to configuration file")

	var importCmd = &cobra.Command{
		Use:   "import",
		Short: "Import database and configuration from external sources.",
		Long:  `Import data from external kaf-mirror instances or other sources.`,
	}

	importDatabaseCmd := newImportDatabaseCmd(&dbPath)

	var importConfigCmd = &cobra.Command{
		Use:   "config [source-path]",
		Short: "Import configuration from external source.",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			sourcePath := args[0]
			configPath, _ := cmd.Flags().GetString("config-path")

			if _, err := os.Stat(sourcePath); os.IsNotExist(err) {
				log.Fatalf("Source configuration not found: %s", sourcePath)
			}

			confirm, err := confirmAction(cmd, "This will overwrite the current configuration. Are you sure?")
			if err != nil {
				log.Fatalf("Import cancelled: %v", err)
			}
			if !confirm {
				fmt.Println("Import cancelled.")
				return
			}

			if err := copyFile(sourcePath, configPath); err != nil {
				log.Fatalf("Failed to import configuration: %v", err)
			}

			fmt.Printf("Configuration imported from: %s\n", sourcePath)
		},
	}
	importConfigCmd.Flags().String("config-path", "configs/default.yml", "Path to configuration file")

	var importFullCmd = &cobra.Command{
		Use:   "full [source-directory]",
		Short: "Import complete system from external source.",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			sourceDir := args[0]
			configPath, _ := cmd.Flags().GetString("config-path")

			dbSourcePath := filepath.Join(sourceDir, "database.db")
			configSourcePath := filepath.Join(sourceDir, "config.yml")

			if _, err := os.Stat(sourceDir); os.IsNotExist(err) {
				log.Fatalf("Source directory not found: %s", sourceDir)
			}

			confirm, err := confirmAction(cmd, "This will overwrite database and configuration. Are you sure?")
			if err != nil {
				log.Fatalf("Import cancelled: %v", err)
			}
			if !confirm {
				fmt.Println("Import cancelled.")
				return
			}

			if _, err := os.Stat(dbSourcePath); err == nil {
				closeDB()
				if err := restoreDatabase(dbSourcePath, dbPath); err != nil {
					log.Fatalf("Failed to import database: %v", err)
				}
				fmt.Printf("Database imported from: %s\n", dbSourcePath)
			} else {
				fmt.Println("No database found in source directory, skipping.")
			}

			if _, err := os.Stat(configSourcePath); err == nil {
				if err := copyFile(configSourcePath, configPath); err != nil {
					log.Fatalf("Failed to import configuration: %v", err)
				}
				fmt.Printf("Configuration imported from: %s\n", configSourcePath)
			} else {
				fmt.Println("No configuration found in source directory, skipping.")
			}

			fmt.Printf("Full import completed from: %s\n", sourceDir)
		},
	}
	importFullCmd.Flags().String("config-path", "configs/default.yml", "Path to configuration file")

	addYesFlag(restoreDatabaseCmd, restoreConfigCmd, importDatabaseCmd, importConfigCmd, importFullCmd)

	backupCmd.AddCommand(backupDatabaseCmd, backupConfigCmd, backupFullCmd)
	restoreCmd.AddCommand(restoreDatabaseCmd, restoreConfigCmd)
	importCmd.AddCommand(importDatabaseCmd, importConfigCmd, importFullCmd)

	rootCmd.AddCommand(usersCmd, tokensCmd, repairCmd, resetAdminPasswordCmd, backupCmd, restoreCmd, importCmd)
	rootCmd.CompletionOptions.DisableDefaultCmd = true
	rootCmd.SetArgs(args)
	return rootCmd.Execute()
}
