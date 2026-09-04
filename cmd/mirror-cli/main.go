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

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"kaf-mirror/cmd/mirror-cli/dashboard"
	"kaf-mirror/cmd/mirror-cli/dashboard/core"
	"kaf-mirror/pkg/utils"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/AlecAivazis/survey/v2"
	"github.com/c-bata/go-prompt"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"gopkg.in/yaml.v2"
)

var (
	BackendURL string
	Version    string
	httpClient = &http.Client{Timeout: 15 * time.Second}
	client     = httpClient
)

// ValidateBrokerString validates broker strings based on provider type
func ValidateBrokerString(brokers, provider string) error {
	switch provider {
	case "confluent":
		return ValidateConfluentBrokers(brokers)
	case "redpanda":
		return ValidateRedpandaBrokers(brokers)
	case "azure":
		return ValidateAzureBrokers(brokers)
	case "plain":
		return ValidatePlainBrokers(brokers)
	default:
		return ValidatePlainBrokers(brokers)
	}
}

// ValidateConfluentBrokers validates Confluent Cloud broker format (single or CSV)
func ValidateConfluentBrokers(brokers string) error {
	pattern := `^[A-Za-z0-9-]+(?:\\.[A-Za-z0-9-]+)*\\.confluent\\.cloud:[0-9]{1,5}$`
	return validateBrokerList(brokers, pattern, "cluster.confluent.cloud:port")
}

// ValidateRedpandaBrokers validates RedPanda Cloud broker format (single or CSV)
func ValidateRedpandaBrokers(brokers string) error {
	pattern := `^[A-Za-z0-9-]+(?:\\.[A-Za-z0-9-]+)*\\.redpanda\\.cloud:[0-9]{1,5}$`
	return validateBrokerList(brokers, pattern, "cluster.redpanda.cloud:port")
}

// ValidateAzureBrokers validates Azure Event Hub broker format (single or CSV)
func ValidateAzureBrokers(brokers string) error {
	pattern := `^[A-Za-z0-9-]+(?:\\.[A-Za-z0-9-]+)*\\.servicebus\\.windows\\.net:[0-9]{1,5}$`
	return validateBrokerList(brokers, pattern, "namespace.servicebus.windows.net:port")
}

// ValidatePlainBrokers validates plain Kafka broker format (single or CSV)
func ValidatePlainBrokers(brokers string) error {
	return validateBrokerList(brokers, `^[a-zA-Z0-9.-]+:[0-9]{1,5}$`, "hostname:port")
}

func validateBrokerList(brokers string, pattern string, example string) error {
	brokerList := strings.Split(brokers, ",")
	for _, broker := range brokerList {
		b := strings.TrimSpace(broker)
		if b == "" {
			continue
		}
		matched, err := regexp.MatchString(pattern, b)
		if err != nil {
			return fmt.Errorf("regex validation error: %v", err)
		}
		if !matched {
			return fmt.Errorf("invalid broker format '%s'. Expected example: %s (comma-separated supported)", b, example)
		}

		parts := strings.Split(b, ":")
		if len(parts) == 2 {
			var port int
			if _, err := fmt.Sscanf(parts[1], "%d", &port); err != nil || port < 1 || port > 65535 {
				return fmt.Errorf("invalid port '%s' in broker '%s'. Port must be 1-65535", parts[1], b)
			}
		}
	}
	return nil
}

type configData struct {
	Server struct {
		Host string `yaml:"host"`
		Port int    `yaml:"port"`
		Mode string `yaml:"mode"`
		TLS  struct {
			Enabled  bool   `yaml:"enabled"`
			CertFile string `yaml:"cert_file"`
			KeyFile  string `yaml:"key_file"`
		} `yaml:"tls"`
		CORS struct {
			AllowedOrigins []string `yaml:"allowed_origins"`
		} `yaml:"cors"`
	} `yaml:"server"`
	Replication struct {
		BatchSize              int    `yaml:"batch_size"`
		Parallelism            int    `yaml:"parallelism"`
		Compression            string `yaml:"compression"`
		TopicDiscoveryInterval string `yaml:"topic_discovery_interval"`
	} `yaml:"replication"`
	Clusters map[string]struct {
		Brokers  string `yaml:"brokers"`
		Security struct {
			Enabled       bool   `yaml:"enabled"`
			Protocol      string `yaml:"protocol"`
			SASLMechanism string `yaml:"sasl_mechanism"`
			Username      string `yaml:"username"`
			Password      string `yaml:"password"`
			Kerberos      struct {
				ServiceName string `yaml:"service_name"`
			} `yaml:"kerberos"`
		} `yaml:"security"`
	} `yaml:"clusters"`
	Monitoring struct {
		Enabled  bool   `yaml:"enabled"`
		Platform string `yaml:"platform"`
		Splunk   struct {
			HECEndpoint string `yaml:"hec_endpoint"`
			HECToken    string `yaml:"hec_token"`
			Index       string `yaml:"index"`
		} `yaml:"splunk"`
		Loki struct {
			Endpoint string `yaml:"endpoint"`
		} `yaml:"loki"`
		Prometheus struct {
			PushGateway string `yaml:"push_gateway"`
		} `yaml:"prometheus"`
	} `yaml:"monitoring"`
	Compliance struct {
		Schedule struct {
			Enabled bool `yaml:"enabled"`
			RunHour int  `yaml:"run_hour"`
			Daily   bool `yaml:"daily"`
			Weekly  bool `yaml:"weekly"`
			Monthly bool `yaml:"monthly"`
		} `yaml:"schedule"`
	} `yaml:"compliance"`
	AI struct {
		Provider string `yaml:"provider"`
		Endpoint string `yaml:"endpoint"`
		Token    string `yaml:"token"`
		Model    string `yaml:"model"`
		Features struct {
			AnomalyDetection        bool `yaml:"anomaly_detection"`
			PerformanceOptimization bool `yaml:"performance_optimization"`
			IncidentAnalysis        bool `yaml:"incident_analysis"`
		} `yaml:"features"`
	} `yaml:"ai"`
	TopicMappings []struct {
		Source  string `yaml:"source"`
		Target  string `yaml:"target"`
		Enabled bool   `yaml:"enabled"`
	} `yaml:"topic_mappings"`
}

func NewRootCmd() *cobra.Command {
	var rootCmd = &cobra.Command{
		Use:   "mirror-cli",
		Short: "A CLI for managing kaf-mirror.",
		Long: `mirror-cli is a command-line tool for managing kaf-mirror.
It interacts with the kaf-mirror API to perform various tasks.`,
		Run: func(cmd *cobra.Command, args []string) {
			versionFlag, _ := cmd.Flags().GetBool("version")
			if versionFlag {
				fmt.Println(Version)
				os.Exit(0)
			}
		},
	}

	rootCmd.PersistentFlags().StringVar(&BackendURL, "mirror-url", "http://localhost:8080", "URL of the kaf-mirror backend.")
	rootCmd.Flags().BoolP("version", "v", false, "Print the version and exit")

	var loginCmd = &cobra.Command{
		Use:   "login [username]",
		Short: "Login to the kaf-mirror backend",
		Args:  cobra.MaximumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			var username string
			if len(args) > 0 {
				username = args[0]
			} else {
				prompt := &survey.Input{Message: "Username:"}
				if err := survey.AskOne(prompt, &username); err != nil {
					fmt.Println("Login cancelled.")
					return
				}
			}

			password, _ := cmd.Flags().GetString("password")
			if password == "" {
				prompt := &survey.Password{Message: "Password:"}
				if err := survey.AskOne(prompt, &password); err != nil {
					fmt.Println("Login cancelled.")
					return
				}
			}

			loginURL := fmt.Sprintf("%s/auth/token", BackendURL)
			reqBody, _ := json.Marshal(map[string]string{
				"username": username,
				"password": password,
			})

			resp, err := http.Post(loginURL, "application/json", bytes.NewBuffer(reqBody))
			if err != nil {
				fmt.Printf("Error: Failed to connect to backend: %v\n", err)
				return
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				fmt.Printf("Error: Login failed: %s\n", resp.Status)
				return
			}

			var tokenResp map[string]string
			if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
				fmt.Printf("Error: Failed to parse login response: %v\n", err)
				return
			}

			token := tokenResp["token"]
			if err := SaveToken(token); err != nil {
				fmt.Printf("Error: Failed to save token: %v\n", err)
				return
			}

			fmt.Println("Login successful. Token stored.")
		},
	}
	loginCmd.Flags().String("password", "", "User's password")

	var usersCmd = &cobra.Command{
		Use:   "users",
		Short: "Manage users.",
		Long:  `The users command allows you to list, add, and manage users.`,
	}

	var listUsersCmd = &cobra.Command{
		Use:   "list",
		Short: "List all users.",
		Long:  `This command lists all users.`,
		Run: func(cmd *cobra.Command, args []string) {
			token, err := LoadToken()
			if err != nil {
				fmt.Println("Error: You must be logged in to perform this action. Please run 'mirror-cli login'.")
				return
			}

			req, err := http.NewRequest("GET", fmt.Sprintf("%s/api/v1/users", BackendURL), nil)
			if err != nil {
				fmt.Printf("Error: Failed to build request: %v\n", err)
				return
			}
			req.Header.Set("Authorization", "Bearer "+token)

			resp, err := httpClient.Do(req)
			if err != nil {
				fmt.Printf("Error: Failed to connect to backend: %v\n", err)
				return
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				fmt.Printf("Error: Failed to list users: %s\n", resp.Status)
				return
			}

			var users []map[string]interface{}
			if err := json.NewDecoder(resp.Body).Decode(&users); err != nil {
				fmt.Printf("Error: Failed to parse response: %v\n", err)
				return
			}

			w := new(bytes.Buffer)
			writer := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
			fmt.Fprintln(writer, "ID\tUSERNAME\tROLE\tCREATED AT")
			for _, user := range users {
				fmt.Fprintf(writer, "%.0f\t%s\t%s\t%s\n", user["id"], user["username"], user["role"], user["created_at"])
			}
			writer.Flush()
			fmt.Println(w.String())
		},
	}

	var addUserCmd = &cobra.Command{
		Use:   "add",
		Short: "Add a new user.",
		Long:  `This command adds a new user with the specified username and password.`,
		Run: func(cmd *cobra.Command, args []string) {
			token, err := LoadToken()
			if err != nil {
				fmt.Println("Error: You must be logged in to perform this action. Please run 'mirror-cli login'.")
				return
			}

			user, err := fetchMe(token)
			if err != nil {
				fmt.Printf("Error: Failed to fetch user profile: %v\n", err)
				return
			}

			var username string
			promptUser := &survey.Input{Message: "Username:"}
			if err := survey.AskOne(promptUser, &username); err != nil {
				fmt.Println("Operation cancelled.")
				return
			}

			var role string
			promptRole := &survey.Select{
				Message: "Role:",
				Options: []string{"admin", "operator", "monitoring", "compliance"},
			}
			if err := survey.AskOne(promptRole, &role); err != nil {
				fmt.Println("Operation cancelled.")
				return
			}

			if user["role"] != "admin" && (role == "admin" || role == "operator") {
				fmt.Println("Error: You do not have permission to create admin or operator users.")
				return
			}

			password, err := utils.GenerateRandomPassword(16)
			if err != nil {
				log.Fatalf("Failed to generate a random password: %v", err)
			}

			reqBody, _ := json.Marshal(map[string]string{
				"username": username,
				"password": password,
				"role":     role,
			})

			req, err := http.NewRequest("POST", fmt.Sprintf("%s/api/v1/users", BackendURL), bytes.NewBuffer(reqBody))
			if err != nil {
				fmt.Printf("Error: Failed to build request: %v\n", err)
				return
			}
			req.Header.Set("Authorization", "Bearer "+token)
			req.Header.Set("Content-Type", "application/json")

			resp, err := httpClient.Do(req)
			if err != nil {
				fmt.Printf("Error: Failed to connect to backend: %v\n", err)
				return
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusCreated {
				fmt.Printf("Error: Failed to create user: %s\n", resp.Status)
				return
			}

			fmt.Println("=================================================================")
			fmt.Println("  USER CREATED")
			fmt.Println("=================================================================")
			fmt.Printf("  Username: %s\n", username)
			fmt.Println("  Password: [REDACTED]")
			fmt.Println("=================================================================")
			fmt.Println("  Deliver credentials securely to the user.")
			fmt.Println("=================================================================")
		},
	}
	addUserCmd.Flags().String("password", "", "User's password")

	var setUserRoleCmd = &cobra.Command{
		Use:   "set-role [username] [role]",
		Short: "Set a user's role.",
		Long:  `This command sets the role for the specified user.`,
		Args:  cobra.MaximumNArgs(2),
		Run: func(cmd *cobra.Command, args []string) {
			token, err := LoadToken()
			if err != nil {
				fmt.Println("Error: You must be logged in to perform this action. Please run 'mirror-cli login'.")
				return
			}

			user, err := fetchMe(token)
			if err != nil {
				fmt.Printf("Error: Failed to fetch user profile: %v\n", err)
				return
			}

			var username string
			if len(args) > 0 {
				username = args[0]
			} else {
				users, err := fetchUsers(token)
				if err != nil {
					fmt.Printf("Error: Failed to fetch users: %v\n", err)
					return
				}
				var usernames []string
				for _, u := range users {
					usernames = append(usernames, u["username"].(string))
				}
				prompt := &survey.Select{
					Message: "Select a user:",
					Options: usernames,
				}
				if err := survey.AskOne(prompt, &username); err != nil {
					fmt.Println("Operation cancelled.")
					return
				}
			}

			if user["username"] == username {
				fmt.Println("Error: You cannot change your own role.")
				return
			}

			var role string
			if len(args) > 1 {
				role = args[1]
			} else {
				prompt := &survey.Select{
					Message: "Select a role:",
					Options: []string{"admin", "operator", "monitoring", "compliance"},
				}
				if err := survey.AskOne(prompt, &role); err != nil {
					fmt.Println("Operation cancelled.")
					return
				}
			}

			reqBody, _ := json.Marshal(map[string]string{
				"role": role,
			})

			req, _ := http.NewRequest("PUT", fmt.Sprintf("%s/api/v1/users/%s/role", BackendURL, username), bytes.NewBuffer(reqBody))
			req.Header.Set("Authorization", "Bearer "+token)
			req.Header.Set("Content-Type", "application/json")

			resp, err := httpClient.Do(req)
			if err != nil {
				fmt.Printf("Error: Failed to connect to backend: %v\n", err)
				return
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				fmt.Printf("Error: Failed to set user role: %s\n", resp.Status)
				return
			}

			fmt.Println("User role updated successfully.")
		},
	}

	var resetTokenCmd = &cobra.Command{
		Use:   "reset-token",
		Short: "Reset your API token.",
		Long:  `This command revokes all of your existing API tokens and generates a new one.`,
		Run: func(cmd *cobra.Command, args []string) {
			token, err := LoadToken()
			if err != nil {
				fmt.Println("Error: You must be logged in to perform this action. Please run 'mirror-cli login'.")
				return
			}

			req, _ := http.NewRequest("POST", fmt.Sprintf("%s/api/v1/auth/reset-token", BackendURL), nil)
			req.Header.Set("Authorization", "Bearer "+token)

			resp, err := httpClient.Do(req)
			if err != nil {
				fmt.Printf("Error: Failed to connect to backend: %v\n", err)
				return
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				fmt.Printf("Error: Failed to reset token: %s\n", resp.Status)
				return
			}

			var tokenResp map[string]string
			if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
				fmt.Printf("Error: Failed to parse response: %v\n", err)
				return
			}

			newToken := tokenResp["token"]
			if err := SaveToken(newToken); err != nil {
				fmt.Printf("Error: Failed to save token: %v\n", err)
				return
			}

			fmt.Println("Token reset successfully. New token stored.")
		},
	}

	var deleteUserCmd = &cobra.Command{
		Use:   "delete [username]",
		Short: "Delete a user.",
		Long:  `This command deletes a user from the database.`,
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			token, err := LoadToken()
			if err != nil {
				fmt.Println("Error: You must be logged in to perform this action. Please run 'mirror-cli login'.")
				return
			}

			req, _ := http.NewRequest("DELETE", fmt.Sprintf("%s/api/v1/users/%s", BackendURL, args[0]), nil)
			req.Header.Set("Authorization", "Bearer "+token)

			resp, err := httpClient.Do(req)
			if err != nil {
				fmt.Printf("Error: Failed to connect to backend: %v\n", err)
				return
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusNoContent {
				fmt.Printf("Error: Failed to delete user: %s\n", resp.Status)
				return
			}

			fmt.Println("User deleted successfully.")
		},
	}

	var passwordFile string
	var resetUserPasswordCmd = &cobra.Command{
		Use:   "reset-password [username]",
		Short: "Reset a user's password (admin only).",
		Long:  `This command allows admins to reset any user's password and generates a new random password.`,
		Args:  cobra.MaximumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			token, err := LoadToken()
			if err != nil {
				fmt.Println("Error: You must be logged in to perform this action. Please run 'mirror-cli login'.")
				return
			}

			user, err := fetchMe(token)
			if err != nil {
				fmt.Printf("Error: Failed to fetch user profile: %v\n", err)
				return
			}

			if user["role"] != "admin" {
				fmt.Println("Error: You must be an admin to perform this action.")
				return
			}

			var username string
			if len(args) > 0 {
				username = args[0]
			} else {
				users, err := fetchUsers(token)
				if err != nil {
					fmt.Printf("Error: Failed to fetch users: %v\n", err)
					return
				}
				var usernames []string
				for _, u := range users {
					if u["username"].(string) != user["username"].(string) {
						usernames = append(usernames, u["username"].(string))
					}
				}
				if len(usernames) == 0 {
					fmt.Println("Error: No other users found to reset password for.")
					return
				}
				prompt := &survey.Select{
					Message: "Select a user to reset password for:",
					Options: usernames,
				}
				if err := survey.AskOne(prompt, &username); err != nil {
					fmt.Println("Operation cancelled.")
					return
				}
			}

			if user["username"] == username {
				fmt.Println("Error: You cannot reset your own password. Use 'change-password' instead.")
				return
			}

			var confirm bool
			confirmPrompt := &survey.Confirm{
				Message: fmt.Sprintf("Are you sure you want to reset the password for user '%s'?", username),
				Default: false,
			}
			if err := survey.AskOne(confirmPrompt, &confirm); err != nil {
				fmt.Println("Operation cancelled.")
				return
			}
			if !confirm {
				fmt.Println("Password reset cancelled.")
				return
			}

			req, _ := http.NewRequest("POST", fmt.Sprintf("%s/api/v1/users/%s/reset-password", BackendURL, username), nil)
			req.Header.Set("Authorization", "Bearer "+token)

			resp, err := httpClient.Do(req)
			if err != nil {
				fmt.Printf("Error: Failed to connect to backend: %v\n", err)
				return
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				fmt.Printf("Error: Failed to reset user password: %s\n", resp.Status)
				return
			}

			var resetResp map[string]string
			if err := json.NewDecoder(resp.Body).Decode(&resetResp); err != nil {
				fmt.Printf("Error: Failed to parse response: %v\n", err)
				return
			}

			newPassword := resetResp["new_password"]
			outputPath := passwordFile
			if outputPath == "" {
				file, err := os.CreateTemp("", "kaf-mirror-password-*.txt")
				if err != nil {
					fmt.Printf("Error: Failed to create password file: %v\n", err)
					return
				}
				outputPath = file.Name()
				if _, err := file.WriteString(newPassword + "\n"); err != nil {
					file.Close()
					fmt.Printf("Error: Failed to write password file: %v\n", err)
					return
				}
				if err := file.Close(); err != nil {
					fmt.Printf("Error: Failed to close password file: %v\n", err)
					return
				}
			} else {
				file, err := os.OpenFile(outputPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
				if err != nil {
					fmt.Printf("Error: Failed to open password file: %v\n", err)
					return
				}
				if _, err := file.WriteString(newPassword + "\n"); err != nil {
					file.Close()
					fmt.Printf("Error: Failed to write password file: %v\n", err)
					return
				}
				if err := file.Close(); err != nil {
					fmt.Printf("Error: Failed to close password file: %v\n", err)
					return
				}
			}
			fmt.Println("=================================================================")
			fmt.Println("  PASSWORD RESET SUCCESSFUL")
			fmt.Println("=================================================================")
			fmt.Printf("  Username: %s\n", username)
			fmt.Println("  New Password: ********")
			fmt.Println("  Password File: [REDACTED]")
			fmt.Println("=================================================================")
			fmt.Println("  Please provide this password to the user securely.")
			fmt.Println("  Delete the password file after delivery.")
			fmt.Println("  The user should change this password on first login.")
			fmt.Println("=================================================================")
		},
	}
	resetUserPasswordCmd.Flags().StringVar(&passwordFile, "password-file", "", "Write new password to file instead of stdout")

	usersCmd.AddCommand(listUsersCmd, addUserCmd, setUserRoleCmd, resetTokenCmd, deleteUserCmd, resetUserPasswordCmd)

	var clustersCmd = &cobra.Command{
		Use:   "clusters",
		Short: "Manage clusters.",
		Long:  `The clusters command allows you to list, add, remove, and purge clusters.`,
	}

	var addClusterCmd *cobra.Command
	var listClustersCmd = &cobra.Command{
		Use:   "list",
		Short: "List all clusters.",
		Long:  `This command lists all configured Kafka clusters.`,
		Run: func(cmd *cobra.Command, args []string) {
			token, err := LoadToken()
			if err != nil {
				fmt.Println("Error: You must be logged in to perform this action. Please run 'mirror-cli login'.")
				return
			}

			req, _ := http.NewRequest("GET", fmt.Sprintf("%s/api/v1/clusters", BackendURL), nil)
			req.Header.Set("Authorization", "Bearer "+token)

			resp, err := httpClient.Do(req)
			if err != nil {
				fmt.Printf("Error: Failed to connect to backend: %v\n", err)
				return
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				fmt.Printf("Error: Failed to list clusters: %s\n", resp.Status)
				return
			}

			var clusters []map[string]interface{}
			if err := json.NewDecoder(resp.Body).Decode(&clusters); err != nil {
				fmt.Printf("Error: Failed to parse response: %v\n", err)
				return
			}

			if len(clusters) == 0 {
				fmt.Println("No clusters found.")
				addCluster := false
				prompt := &survey.Confirm{
					Message: "Would you like to add a new cluster?",
				}
				if err := survey.AskOne(prompt, &addCluster); err != nil {
					fmt.Println("Operation cancelled.")
					return
				}
				if addCluster {
					addClusterCmd.Run(cmd, args)
				}
				return
			}

			w := new(bytes.Buffer)
			writer := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
			fmt.Fprintln(writer, "Name\tBrokers\tStatus")
			for _, cluster := range clusters {
				fmt.Fprintf(writer, "%s\t%s\t%s\n", cluster["name"], cluster["brokers"], cluster["status"])
			}
			writer.Flush()
			fmt.Print(w.String())
		},
	}

	addClusterCmd = &cobra.Command{
		Use:   "add",
		Short: "Add a new cluster.",
		Long:  `This command adds a new Kafka cluster with the specified name and brokers.`,
		Run: func(cmd *cobra.Command, args []string) {
			token, err := LoadToken()
			if err != nil {
				fmt.Println("Error: You must be logged in to perform this action. Please run 'mirror-cli login'.")
				return
			}

			var name, brokers, provider, apiKey, apiSecret, clusterID string
			promptName := &survey.Input{Message: "Cluster name:"}
			if err := survey.AskOne(promptName, &name); err != nil {
				fmt.Println("Operation cancelled.")
				return
			}

			promptProvider := &survey.Select{
				Message: "Select provider:",
				Options: []string{"plain", "confluent", "redpanda", "azure"},
			}
			if err := survey.AskOne(promptProvider, &provider); err != nil {
				fmt.Println("Operation cancelled.")
				return
			}

			if provider == "confluent" {
				promptClusterID := &survey.Input{Message: "Cluster ID:"}
				if err := survey.AskOne(promptClusterID, &clusterID); err != nil {
					fmt.Println("Operation cancelled.")
					return
				}
				promptBrokers := &survey.Input{Message: "Bootstrap URL:"}
				if err := survey.AskOne(promptBrokers, &brokers); err != nil {
					fmt.Println("Operation cancelled.")
					return
				}
				promptAPIKey := &survey.Input{Message: "API Key:"}
				if err := survey.AskOne(promptAPIKey, &apiKey); err != nil {
					fmt.Println("Operation cancelled.")
					return
				}
				promptAPISecret := &survey.Password{Message: "API Secret:"}
				if err := survey.AskOne(promptAPISecret, &apiSecret); err != nil {
					fmt.Println("Operation cancelled.")
					return
				}
			} else if provider == "azure" {
				var namespace string
				promptNamespace := &survey.Input{Message: "Event Hubs Namespace:"}
				if err := survey.AskOne(promptNamespace, &namespace); err != nil {
					fmt.Println("Operation cancelled.")
					return
				}
				brokers = fmt.Sprintf("%s.servicebus.windows.net:9093", namespace)

				promptConnStr := &survey.Password{Message: "Connection String:"}
				if err := survey.AskOne(promptConnStr, &clusterID); err != nil {
					fmt.Println("Operation cancelled.")
					return
				}
			} else {
				for {
					promptBrokers := &survey.Input{Message: "Brokers (comma-separated):"}
					if err := survey.AskOne(promptBrokers, &brokers); err != nil {
						fmt.Println("Operation cancelled.")
						return
					}

					// Validate broker string format
					if err := ValidateBrokerString(brokers, provider); err != nil {
						fmt.Printf("Error: %v\n", err)
						var retry bool
						promptRetry := &survey.Confirm{Message: "Would you like to re-enter the broker string?"}
						if err := survey.AskOne(promptRetry, &retry); err != nil || !retry {
							fmt.Println("Operation cancelled.")
							return
						}
						continue
					}
					break
				}
			}

			clusterData := map[string]interface{}{
				"name":       name,
				"provider":   provider,
				"cluster_id": clusterID,
				"brokers":    brokers,
				"security": map[string]string{
					"api_key":    apiKey,
					"api_secret": apiSecret,
				},
			}

			for {
				fmt.Println("Testing connection...")
				testBody, _ := json.Marshal(clusterData)
				req, err := http.NewRequest("POST", fmt.Sprintf("%s/api/v1/clusters/test", BackendURL), bytes.NewBuffer(testBody))
				if err != nil {
					fmt.Printf("Error: Failed to build request: %v\n", err)
					return
				}
				req.Header.Set("Authorization", "Bearer "+token)
				req.Header.Set("Content-Type", "application/json")

				resp, err := httpClient.Do(req)
				if err != nil {
					fmt.Printf("Error: Failed to connect to backend: %v\n", err)
					return
				}

				if resp.StatusCode != http.StatusOK {
					body, _ := ioutil.ReadAll(resp.Body)
					resp.Body.Close()
					fmt.Printf("Error: Connection test failed: %s\n%s\n", resp.Status, body)
					var reEnter bool
					prompt := &survey.Confirm{
						Message: "Would you like to re-enter the details?",
					}
					if err := survey.AskOne(prompt, &reEnter); err != nil {
						fmt.Println("Operation cancelled.")
						return
					}
					if reEnter {
						if provider == "confluent" {
							promptBrokers := &survey.Input{Message: "Bootstrap URL:"}
							survey.AskOne(promptBrokers, &brokers)
							promptClusterID := &survey.Input{Message: "Cluster ID:"}
							survey.AskOne(promptClusterID, &clusterID)
							promptAPIKey := &survey.Input{Message: "API Key:"}
							survey.AskOne(promptAPIKey, &apiKey)
							promptAPISecret := &survey.Password{Message: "API Secret:"}
							survey.AskOne(promptAPISecret, &apiSecret)
						} else if provider == "azure" {
							var namespace string
							promptNamespace := &survey.Input{Message: "Event Hubs Namespace:"}
							survey.AskOne(promptNamespace, &namespace)
							brokers = fmt.Sprintf("%s.servicebus.windows.net:9093", namespace)

							promptConnStr := &survey.Password{Message: "Connection String:"}
							survey.AskOne(promptConnStr, &clusterID)
						} else {
							promptBrokers := &survey.Input{Message: "Brokers (comma-separated):"}
							survey.AskOne(promptBrokers, &brokers)
						}
						clusterData = map[string]interface{}{
							"name":       name,
							"provider":   provider,
							"cluster_id": clusterID,
							"brokers":    brokers,
							"security": map[string]string{
								"api_key":    apiKey,
								"api_secret": apiSecret,
							},
						}
						continue
					} else {
						return
					}
				}
				fmt.Println("Connection test successful.")
				break
			}

			// Prepare cluster creation request with provider-specific fields
			clusterRequest := map[string]interface{}{
				"name":     name,
				"provider": provider,
				"brokers":  brokers,
			}

			if provider == "confluent" {
				clusterRequest["cluster_id"] = clusterID
				clusterRequest["api_key"] = apiKey
				clusterRequest["api_secret"] = apiSecret
			} else if provider == "azure" {
				clusterRequest["connection_string"] = clusterID // For Azure, clusterID contains connection string
			}

			reqBody, _ := json.Marshal(clusterRequest)

			req, _ := http.NewRequest("POST", fmt.Sprintf("%s/api/v1/clusters", BackendURL), bytes.NewBuffer(reqBody))
			req.Header.Set("Authorization", "Bearer "+token)
			req.Header.Set("Content-Type", "application/json")

			resp, err := client.Do(req)
			if err != nil {
				fmt.Printf("Error: Failed to connect to backend: %v\n", err)
				return
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusCreated {
				body, _ := ioutil.ReadAll(resp.Body)
				fmt.Printf("Error: Failed to create cluster: %s\n%s\n", resp.Status, body)
				return
			}

			fmt.Println("Cluster created successfully.")

			var listTopics bool
			prompt := &survey.Confirm{
				Message: "Would you like to list the topics in the new cluster?",
			}
			if err := survey.AskOne(prompt, &listTopics); err != nil {
				fmt.Println("Operation cancelled.")
				return
			}

			if listTopics {
				listClusterTopics(token, name)
			}

			fmt.Println("\nTo start mirroring topics, create a new replication job with 'mirror-cli jobs add'")
		},
	}

	var removeClusterCmd = &cobra.Command{
		Use:   "remove [name]",
		Short: "Mark a cluster for deletion.",
		Long:  `This command marks a cluster as inactive. It will be archived after 72 hours and then can be purged.`,
		Args:  cobra.MaximumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			token, err := LoadToken()
			if err != nil {
				fmt.Println("Error: You must be logged in to perform this action. Please run 'mirror-cli login'.")
				return
			}

			user, err := fetchMe(token)
			if err != nil {
				fmt.Printf("Error: Failed to fetch user profile: %v\n", err)
				return
			}

			if user["role"] != "admin" {
				fmt.Println("Error: You must be an admin to perform this action.")
				return
			}

			var clusterName string
			if len(args) > 0 {
				clusterName = args[0]
			} else {
				clusters, err := fetchClusters(token)
				if err != nil {
					fmt.Printf("Error: Failed to fetch clusters: %v\n", err)
					return
				}
				if len(clusters) == 0 {
					fmt.Println("No clusters found to remove.")
					return
				}
				var clusterNames []string
				for _, cluster := range clusters {
					if cluster["status"].(string) == "active" {
						clusterNames = append(clusterNames, cluster["name"].(string))
					}
				}
				if len(clusterNames) == 0 {
					fmt.Println("No active clusters found to remove.")
					return
				}
				prompt := &survey.Select{
					Message: "Select a cluster to remove:",
					Options: clusterNames,
				}
				if err := survey.AskOne(prompt, &clusterName); err != nil {
					fmt.Println("Operation cancelled.")
					return
				}
			}

			var confirm bool
			confirmPrompt := &survey.Confirm{
				Message: fmt.Sprintf("Are you sure you want to remove cluster '%s'? This action will mark it for deletion.", clusterName),
				Default: false,
			}
			if err := survey.AskOne(confirmPrompt, &confirm); err != nil {
				fmt.Println("Operation cancelled.")
				return
			}
			if !confirm {
				fmt.Println("Cluster removal cancelled.")
				return
			}

			req, _ := http.NewRequest("DELETE", fmt.Sprintf("%s/api/v1/clusters/%s", BackendURL, clusterName), nil)
			req.Header.Set("Authorization", "Bearer "+token)

			resp, err := httpClient.Do(req)
			if err != nil {
				fmt.Printf("Error: Failed to connect to backend: %v\n", err)
				return
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusAccepted {
				body, _ := ioutil.ReadAll(resp.Body)
				fmt.Printf("Error: Failed to remove cluster: %s\n%s\n", resp.Status, body)
				return
			}

			fmt.Printf("Cluster '%s' marked for deletion and will be archived after 72 hours.\n", clusterName)
		},
	}

	var purgeClustersCmd = &cobra.Command{
		Use:   "purge",
		Short: "Purge archived clusters.",
		Long:  `This command permanently deletes all archived Kafka clusters.`,
		Run: func(cmd *cobra.Command, args []string) {
			token, err := LoadToken()
			if err != nil {
				fmt.Println("Error: You must be logged in to perform this action. Please run 'mirror-cli login'.")
				return
			}

			user, err := fetchMe(token)
			if err != nil {
				fmt.Printf("Error: Failed to fetch user profile: %v\n", err)
				return
			}

			if user["role"] != "admin" {
				fmt.Println("Error: You must be an admin to perform this action.")
				return
			}

			req, _ := http.NewRequest("DELETE", fmt.Sprintf("%s/api/v1/clusters/purge", BackendURL), nil)
			req.Header.Set("Authorization", "Bearer "+token)

			resp, err := httpClient.Do(req)
			if err != nil {
				fmt.Printf("Error: Failed to connect to backend: %v\n", err)
				return
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusNoContent {
				fmt.Printf("Error: Failed to purge clusters: %s\n", resp.Status)
				return
			}

			fmt.Println("Archived clusters purged.")
		},
	}

	var restoreClusterCmd = &cobra.Command{
		Use:   "restore [name]",
		Short: "Restore an inactive cluster.",
		Long:  `This command restores an inactive cluster to active status.`,
		Args:  cobra.MaximumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			token, err := LoadToken()
			if err != nil {
				fmt.Println("Error: You must be logged in to perform this action. Please run 'mirror-cli login'.")
				return
			}

			user, err := fetchMe(token)
			if err != nil {
				fmt.Printf("Error: Failed to fetch user profile: %v\n", err)
				return
			}

			if user["role"] != "admin" {
				fmt.Println("Error: You must be an admin to perform this action.")
				return
			}

			var clusterName string
			if len(args) > 0 {
				clusterName = args[0]
			} else {
				clusters, err := fetchClusters(token)
				if err != nil {
					fmt.Printf("Error: Failed to fetch clusters: %v\n", err)
					return
				}
				if len(clusters) == 0 {
					fmt.Println("No clusters found to restore.")
					return
				}
				var clusterNames []string
				for _, cluster := range clusters {
					if cluster["status"].(string) == "inactive" {
						clusterNames = append(clusterNames, cluster["name"].(string))
					}
				}
				if len(clusterNames) == 0 {
					fmt.Println("No inactive clusters found to restore.")
					return
				}
				prompt := &survey.Select{
					Message: "Select a cluster to restore:",
					Options: clusterNames,
				}
				if err := survey.AskOne(prompt, &clusterName); err != nil {
					fmt.Println("Operation cancelled.")
					return
				}
			}

			req, _ := http.NewRequest("POST", fmt.Sprintf("%s/api/v1/clusters/%s/restore", BackendURL, clusterName), nil)
			req.Header.Set("Authorization", "Bearer "+token)

			resp, err := httpClient.Do(req)
			if err != nil {
				fmt.Printf("Error: Failed to connect to backend: %v\n", err)
				return
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				body, _ := ioutil.ReadAll(resp.Body)
				fmt.Printf("Error: Failed to restore cluster: %s\n%s\n", resp.Status, body)
				return
			}

			fmt.Printf("Cluster '%s' restored successfully.\n", clusterName)
		},
	}

	var testClusterCmd = &cobra.Command{
		Use:   "test [name]",
		Short: "Test connection to a cluster.",
		Long:  `This command tests the connection to a specified Kafka cluster and provides detailed diagnostics.`,
		Args:  cobra.MaximumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			token, err := LoadToken()
			if err != nil {
				fmt.Println("Error: You must be logged in to perform this action. Please run 'mirror-cli login'.")
				return
			}

			var clusterName string
			if len(args) > 0 {
				clusterName = args[0]
			} else {
				clusters, err := fetchClusters(token)
				if err != nil {
					fmt.Printf("Error: Failed to fetch clusters: %v\n", err)
					return
				}
				if len(clusters) == 0 {
					fmt.Println("No clusters found to test.")
					return
				}
				var clusterNames []string
				for _, cluster := range clusters {
					clusterNames = append(clusterNames, cluster["name"].(string))
				}
				clusterNames = append(clusterNames, "All")
				prompt := &survey.Select{
					Message: "Select a cluster to test:",
					Options: clusterNames,
				}
				if err := survey.AskOne(prompt, &clusterName); err != nil {
					fmt.Println("Operation cancelled.")
					return
				}
			}

			if clusterName == "All" {
				clusters, err := fetchClusters(token)
				if err != nil {
					fmt.Printf("Error: Failed to fetch clusters: %v\n", err)
					return
				}
				for _, cluster := range clusters {
					name := cluster["name"].(string)
					fmt.Printf("Testing connection to cluster '%s'...\n", name)
					if err := testClusterConnectionVerbose(token, name); err != nil {
						fmt.Printf("Error: Cluster connection test failed for '%s': %v\n", name, err)
					} else {
						fmt.Printf("✓ Connection to cluster '%s' successful.\n", name)
					}
				}
			} else {
				fmt.Printf("Testing connection to cluster '%s'...\n", clusterName)
				if err := testClusterConnectionVerbose(token, clusterName); err != nil {
					fmt.Printf("Error: Cluster connection test failed: %v\n", err)
					return
				}
				fmt.Printf("✓ Connection to cluster '%s' successful.\n", clusterName)
			}
		},
	}

	var editClusterCmd = &cobra.Command{
		Use:   "edit [name]",
		Short: "Edit an existing cluster.",
		Long:  `This command allows you to edit the name and brokers of an existing Kafka cluster.`,
		Args:  cobra.MaximumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			token, err := LoadToken()
			if err != nil {
				fmt.Println("Error: You must be logged in to perform this action. Please run 'mirror-cli login'.")
				return
			}

			var clusterName string
			if len(args) > 0 {
				clusterName = args[0]
			} else {
				clusters, err := fetchClusters(token)
				if err != nil {
					fmt.Printf("Error: Failed to fetch clusters: %v\n", err)
					return
				}
				if len(clusters) == 0 {
					fmt.Println("No clusters found to edit.")
					return
				}
				var clusterNames []string
				for _, cluster := range clusters {
					clusterNames = append(clusterNames, cluster["name"].(string))
				}
				prompt := &survey.Select{
					Message: "Select a cluster to edit:",
					Options: clusterNames,
				}
				survey.AskOne(prompt, &clusterName)
			}

			// Fetch the current cluster details
			req, _ := http.NewRequest("GET", fmt.Sprintf("%s/api/v1/clusters/%s", BackendURL, clusterName), nil)
			req.Header.Set("Authorization", "Bearer "+token)
			resp, err := httpClient.Do(req)
			if err != nil {
				fmt.Printf("Error: Failed to connect to backend: %v\n", err)
				return
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				fmt.Printf("Error: Failed to fetch cluster details: %s\n", resp.Status)
				return
			}

			var currentCluster map[string]interface{}
			if err := json.NewDecoder(resp.Body).Decode(&currentCluster); err != nil {
				fmt.Printf("Error: Failed to parse cluster details: %v\n", err)
				return
			}

			provider := safeString(currentCluster["provider"], "plain")
			if provider == "" {
				provider = "plain" // Handle legacy clusters with empty provider
			}

			// Display current cluster information including provider
			fmt.Printf("=== Editing Cluster: %s ===\n", safeString(currentCluster["name"], ""))
			fmt.Printf("Provider: %s (non-editable)\n", provider)
			fmt.Printf("Current Brokers: %s\n", safeString(currentCluster["brokers"], "not set"))
			if provider == "confluent" {
				fmt.Printf("Current Cluster ID: %s\n", safeString(currentCluster["cluster_id"], "not set"))
				fmt.Printf("Current API Key: %s\n", safeString(currentCluster["api_key"], "not set"))
			} else if provider == "azure" {
				fmt.Println("Current Connection String: [configured]")
			}
			fmt.Println()

			var newName, newBrokers, newAPIKey, newAPISecret, newClusterID string
			var newConnectionString *string

			promptName := &survey.Input{Message: "New cluster name:", Default: safeString(currentCluster["name"], "")}
			if err := survey.AskOne(promptName, &newName); err != nil {
				fmt.Println("Operation cancelled.")
				return
			}

			// Collect ALL parameters first before testing connection
			if provider == "confluent" {
				promptBrokers := &survey.Input{Message: "New brokers (comma-separated):", Default: safeString(currentCluster["brokers"], "")}
				if err := survey.AskOne(promptBrokers, &newBrokers); err != nil {
					fmt.Println("Operation cancelled.")
					return
				}

				promptClusterID := &survey.Input{Message: "New Cluster ID:", Default: safeString(currentCluster["cluster_id"], "")}
				if err := survey.AskOne(promptClusterID, &newClusterID); err != nil {
					fmt.Println("Operation cancelled.")
					return
				}

				promptAPIKey := &survey.Input{Message: "New API Key (empty keeps current):"}
				if err := survey.AskOne(promptAPIKey, &newAPIKey); err != nil {
					fmt.Println("Operation cancelled.")
					return
				}

				promptAPISecret := &survey.Password{Message: "New API Secret (empty keeps current):"}
				if err := survey.AskOne(promptAPISecret, &newAPISecret); err != nil {
					fmt.Println("Operation cancelled.")
					return
				}
			} else if provider == "azure" {
				var namespace string
				currentBrokers := safeString(currentCluster["brokers"], "")
				if currentBrokers != "" && strings.Contains(currentBrokers, ".servicebus.windows.net") {
					namespace = strings.Split(currentBrokers, ".")[0]
				}

				promptNamespace := &survey.Input{Message: "New Event Hubs Namespace:", Default: namespace}
				if err := survey.AskOne(promptNamespace, &namespace); err != nil {
					fmt.Println("Operation cancelled.")
					return
				}
				newBrokers = fmt.Sprintf("%s.servicebus.windows.net:9093", namespace)

				var connStr string
				promptConnStr := &survey.Password{Message: "New Connection String:"}
				if err := survey.AskOne(promptConnStr, &connStr); err != nil {
					fmt.Println("Operation cancelled.")
					return
				}
				newConnectionString = &connStr
			} else {
				for {
					promptBrokers := &survey.Input{Message: "New brokers (comma-separated):", Default: safeString(currentCluster["brokers"], "")}
					if err := survey.AskOne(promptBrokers, &newBrokers); err != nil {
						fmt.Println("Operation cancelled.")
						return
					}

					// Validate broker string format
					if err := ValidateBrokerString(newBrokers, provider); err != nil {
						fmt.Printf("Error: %v\n", err)
						var retry bool
						promptRetry := &survey.Confirm{Message: "Would you like to re-enter the broker string?"}
						if err := survey.AskOne(promptRetry, &retry); err != nil || !retry {
							fmt.Println("Operation cancelled.")
							return
						}
						continue
					}
					break
				}
			}

			skipTest := (provider == "confluent" && (newAPIKey == "" || newAPISecret == "")) ||
				(provider == "azure" && (newConnectionString == nil || *newConnectionString == ""))
			if skipTest {
				fmt.Println("Skipping connection test because credentials were left unchanged.")
			} else {
				fmt.Println("Testing connection to new brokers...")
				testConfig := map[string]interface{}{
					"brokers":  newBrokers,
					"provider": provider,
				}

				if provider == "confluent" {
					testConfig["cluster_id"] = newClusterID
					testConfig["security"] = map[string]interface{}{
						"api_key":    newAPIKey,
						"api_secret": newAPISecret,
					}
				} else if provider == "azure" && newConnectionString != nil {
					testConfig["security"] = map[string]interface{}{
						"connection_string": *newConnectionString,
					}
				} else if provider == "redpanda" {
					// RedPanda uses standard Kafka protocol, no special auth needed for connection test
					testConfig["security"] = map[string]interface{}{
						"enabled": false,
					}
				} else if provider != "plain" {
					testConfig["security"] = map[string]interface{}{
						"enabled": true,
					}
				}

				testBody, _ := json.Marshal(testConfig)

				req, _ = http.NewRequest("POST", fmt.Sprintf("%s/api/v1/clusters/test", BackendURL), bytes.NewBuffer(testBody))
				req.Header.Set("Authorization", "Bearer "+token)
				req.Header.Set("Content-Type", "application/json")

				resp, err = client.Do(req)
				if err != nil {
					fmt.Printf("Error: Failed to connect to backend: %v\n", err)
					return
				}
				defer resp.Body.Close()

				if resp.StatusCode != http.StatusOK {
					body, _ := ioutil.ReadAll(resp.Body)
					fmt.Printf("Error: Connection test failed: %s\n", resp.Status)
					if len(body) > 0 {
						fmt.Printf("Failed to connect to cluster: %s\n", string(body))
					}
					fmt.Println("Cluster not updated.")
					return
				}

				fmt.Println("Connection test successful.")
			}

			// Prepare the update request in the correct format matching database.KafkaCluster struct
			updateCluster := map[string]interface{}{
				"name":     newName,
				"provider": provider,
				"brokers":  newBrokers,
			}

			if provider == "confluent" {
				if newAPIKey == "" {
					newAPIKey = "***"
				}
				if newAPISecret == "" {
					newAPISecret = "***"
				}
				updateCluster["api_key"] = newAPIKey
				updateCluster["api_secret"] = newAPISecret
				updateCluster["cluster_id"] = newClusterID
			} else if provider == "azure" {
				if newConnectionString == nil || *newConnectionString == "" {
					placeholder := "***"
					newConnectionString = &placeholder
				}
				updateCluster["connection_string"] = *newConnectionString
			}

			reqBody, _ := json.Marshal(updateCluster)
			req, _ = http.NewRequest("PUT", fmt.Sprintf("%s/api/v1/clusters/%s", BackendURL, clusterName), bytes.NewBuffer(reqBody))
			req.Header.Set("Authorization", "Bearer "+token)
			req.Header.Set("Content-Type", "application/json")

			resp, err = client.Do(req)
			if err != nil {
				fmt.Printf("Error: Failed to connect to backend: %v\n", err)
				return
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				body, _ := ioutil.ReadAll(resp.Body)
				fmt.Printf("Error: Failed to update cluster: %s\n", resp.Status)
				if len(body) > 0 {
					fmt.Printf("Server response: %s\n", string(body))
				}
				return
			}

			fmt.Println("Cluster updated successfully.")
		},
	}

	clustersCmd.AddCommand(listClustersCmd, addClusterCmd, editClusterCmd, removeClusterCmd, restoreClusterCmd, purgeClustersCmd, testClusterCmd)

	configCmd := createConfigCommand()
	tlsCmd := createTLSCommand()

	var whoamiCmd = &cobra.Command{
		Use:   "whoami",
		Short: "Display the current user's information.",
		Run: func(cmd *cobra.Command, args []string) {
			token, err := LoadToken()
			if err != nil {
				fmt.Println("Error: You must be logged in to perform this action. Please run 'mirror-cli login'.")
				return
			}

			user, err := fetchMe(token)
			if err != nil {
				fmt.Printf("Error: Failed to fetch user profile: %v\n", err)
				return
			}

			fmt.Printf("Username: %s\n", user["username"])
			fmt.Printf("Token: %s\n", token)
			fmt.Printf("Role: %s\n", user["role"])
		},
	}

	var changePasswordCmd = &cobra.Command{
		Use:   "change-password",
		Short: "Change the current user's password.",
		Run: func(cmd *cobra.Command, args []string) {
			token, err := LoadToken()
			if err != nil {
				fmt.Println("Error: You must be logged in to perform this action. Please run 'mirror-cli login'.")
				return
			}

			var oldPassword, newPassword string
			promptOld := &survey.Password{Message: "Old Password:"}
			if err := survey.AskOne(promptOld, &oldPassword); err != nil {
				fmt.Println("Operation cancelled.")
				return
			}
			promptNew := &survey.Password{Message: "New Password (min 12 characters):"}
			if err := survey.AskOne(promptNew, &newPassword); err != nil {
				fmt.Println("Operation cancelled.")
				return
			}
			if len(newPassword) < 12 {
				fmt.Println("Error: password must be at least 12 characters.")
				return
			}

			reqBody, _ := json.Marshal(map[string]string{
				"old_password": oldPassword,
				"new_password": newPassword,
			})

			req, _ := http.NewRequest("PUT", fmt.Sprintf("%s/api/v1/users/change-password", BackendURL), bytes.NewBuffer(reqBody))
			req.Header.Set("Authorization", "Bearer "+token)
			req.Header.Set("Content-Type", "application/json")

			resp, err := httpClient.Do(req)
			if err != nil {
				fmt.Printf("Error: Failed to connect to backend: %v\n", err)
				return
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				fmt.Printf("Error: Failed to change password: %s\n", resp.Status)
				return
			}

			fmt.Println("Password changed successfully. Existing tokens were revoked; run 'mirror-cli login' again.")
			_ = ClearToken()
		},
	}

	usersCmd.AddCommand(changePasswordCmd)

	var logoutCmd = &cobra.Command{
		Use:   "logout",
		Short: "Logout from the kaf-mirror backend.",
		Run: func(cmd *cobra.Command, args []string) {
			home, err := os.UserHomeDir()
			if err != nil {
				fmt.Printf("Error: Failed to get home directory: %v\n", err)
				return
			}
			if err := os.Remove(filepath.Join(home, ".kaf-mirror", "token")); err != nil {
				fmt.Printf("Error: Failed to logout: %v\n", err)
				return
			}
			fmt.Println("Logout successful.")
		},
	}

	jobsCmd := createJobsCommand()
	docsCmd := createDocsCommand()
	rootCmd.AddCommand(loginCmd, logoutCmd, usersCmd, clustersCmd, jobsCmd, configCmd, tlsCmd, newDashboardCmd(), whoamiCmd, docsCmd)
	rootCmd.CompletionOptions.DisableDefaultCmd = true

	return rootCmd
}

func main() {
	rootCmd := NewRootCmd()

	rootCmd.ParseFlags(os.Args[1:])

	args := rootCmd.Flags().Args()
	hasSubcommand := len(args) > 0

	if !hasSubcommand {
		httpsUrl := strings.Replace(BackendURL, "http://", "https://", 1)
		_, err := http.Get(httpsUrl)
		if err == nil {
			BackendURL = httpsUrl
		}

		token, err := LoadToken()
		if err != nil {
			fmt.Println("Not logged in.")
		} else {
			user, err := fetchMe(token)
			if err != nil {
				fmt.Println("Not logged in.")
			} else {
				fmt.Printf("Logged in as %s\n", user["username"])
			}
		}

		fmt.Printf("Connecting to: %s\n", BackendURL)
		runShell(rootCmd)
	} else {
		if err := rootCmd.Execute(); err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
	}
}

func runShell(rootCmd *cobra.Command) {
	var currentContext []string

	completer := func(d prompt.Document) []prompt.Suggest {
		s := []prompt.Suggest{}
		currentCmd := rootCmd

		fullArgs := append(currentContext, strings.Fields(d.TextBeforeCursor())...)

		for _, arg := range fullArgs {
			foundCmd, _, err := currentCmd.Find([]string{arg})
			if err == nil {
				currentCmd = foundCmd
			} else {
				break
			}
		}

		for _, subCmd := range currentCmd.Commands() {
			s = append(s, prompt.Suggest{Text: subCmd.Name(), Description: subCmd.Short})
		}
		return prompt.FilterHasPrefix(s, d.GetWordBeforeCursor(), true)
	}

	executor := func(in string) {
		in = strings.TrimSpace(in)
		if in == "" {
			currentCmd := rootCmd
			for _, arg := range currentContext {
				foundCmd, _, err := currentCmd.Find([]string{arg})
				if err == nil {
					currentCmd = foundCmd
				}
			}

			if len(currentContext) > 0 {
				fmt.Printf("\n=== %s ===\n", strings.Title(strings.Join(currentContext, " ")))
				if currentCmd.Long != "" {
					fmt.Printf("%s\n\n", currentCmd.Long)
				} else if currentCmd.Short != "" {
					fmt.Printf("%s\n\n", currentCmd.Short)
				}
			}

			fmt.Println("Available commands:")
			for _, subCmd := range currentCmd.Commands() {
				fmt.Printf("  %-15s %s\n", subCmd.Name(), subCmd.Short)
			}
			fmt.Println()
			fmt.Println("Navigation:")
			fmt.Println("  back / ..       Go back one level")
			fmt.Println("  root / /        Go to root menu")
			fmt.Println("  exit            Exit CLI")
			fmt.Println()
			return
		} else if in == "exit" {
			os.Exit(0)
		} else if in == "back" || in == ".." {
			if len(currentContext) > 0 {
				currentContext = currentContext[:len(currentContext)-1]
			}
			return
		} else if in == "root" || in == "/" {
			currentContext = []string{}
			return
		}

		args := strings.Fields(in)
		fullArgs := append(currentContext, args...)

		cmd, _, err := rootCmd.Find(fullArgs)
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			return
		}

		if cmd.Run != nil || cmd.RunE != nil {
			rootCmd.SetArgs(fullArgs)
			rootCmd.Execute()

			if !isTerminalCommand(fullArgs) {
				if len(fullArgs) >= 2 && fullArgs[0] == "config" && fullArgs[1] == "edit" {
					if len(fullArgs) > 2 {
						currentContext = fullArgs[:len(fullArgs)-1]
					}
				}
			} else {
				currentContext = []string{}
			}
			return
		}

		if cmd.HasSubCommands() {
			currentContext = fullArgs

			fmt.Printf("\n=== %s ===\n", strings.Title(strings.Join(fullArgs, " ")))
			if cmd.Long != "" {
				fmt.Printf("%s\n\n", cmd.Long)
			} else if cmd.Short != "" {
				fmt.Printf("%s\n\n", cmd.Short)
			}

			fmt.Println("Available commands:")
			for _, subCmd := range cmd.Commands() {
				fmt.Printf("  %-15s %s\n", subCmd.Name(), subCmd.Short)
			}
			fmt.Println()
			fmt.Println("Navigation:")
			fmt.Println("  back / ..       Go back one level")
			fmt.Println("  root / /        Go to root menu")
			fmt.Println("  exit            Exit CLI")
			fmt.Println()
			return
		}

		fmt.Printf("Error: Unknown command: %s\n", strings.Join(fullArgs, " "))

		if !isTerminalCommand(fullArgs) {
			if len(fullArgs) >= 2 && fullArgs[0] == "config" && fullArgs[1] == "edit" {
				if len(fullArgs) > 2 {
					currentContext = fullArgs[:len(fullArgs)-1]
				}
			}
		} else {
			currentContext = []string{}
		}
	}

	p := prompt.New(
		executor,
		completer,
		prompt.OptionPrefix("kaf-mirror:> "),
		prompt.OptionTitle("kaf-mirror-cli"),
		prompt.OptionLivePrefix(func() (string, bool) {
			if len(currentContext) == 0 {
				return "kaf-mirror:> ", true
			}
			return fmt.Sprintf("kaf-mirror:%s> ", strings.Join(currentContext, " ")), true
		}),
	)

	p.Run()
}

// getNestedConfigValue safely retrieves a nested configuration value
func getNestedConfigValue(config map[string]interface{}, path []string) interface{} {
	current := config
	for i, key := range path {
		if i == len(path)-1 {
			// Final key - return the value
			return current[key]
		}
		// Navigate deeper
		if nextLevel, exists := current[key].(map[string]interface{}); exists {
			current = nextLevel
		} else {
			return nil // Path doesn't exist
		}
	}
	return nil
}

func isTerminalCommand(args []string) bool {
	if len(args) == 0 {
		return true
	}

	terminalCommands := []string{"login", "logout", "whoami"}
	for _, cmd := range terminalCommands {
		if args[0] == cmd {
			return true
		}
	}

	if len(args) >= 2 && args[1] == "list" {
		return true
	}

	return false
}

func SaveToken(token string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	tokenDir := filepath.Join(home, ".kaf-mirror")
	if err := os.MkdirAll(tokenDir, 0700); err != nil {
		return err
	}
	tokenPath := filepath.Join(tokenDir, "token")
	return ioutil.WriteFile(tokenPath, []byte(token), 0600)
}

func ClearToken() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	return os.Remove(filepath.Join(home, ".kaf-mirror", "token"))
}

func LoadToken() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	tokenPath := filepath.Join(home, ".kaf-mirror", "token")

	info, err := os.Stat(tokenPath)
	if err != nil {
		return "", err
	}
	if info.Mode().Perm() != 0600 {
		return "", fmt.Errorf("token file has incorrect permissions: %s", info.Mode().Perm())
	}

	token, err := ioutil.ReadFile(tokenPath)
	if err != nil {
		return "", err
	}
	return string(token), nil
}

func fetchUsers(token string) ([]map[string]interface{}, error) {
	req, _ := http.NewRequest("GET", fmt.Sprintf("%s/api/v1/users", BackendURL), nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch users: %s", resp.Status)
	}
	var users []map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&users); err != nil {
		return nil, err
	}
	return users, nil
}

func fetchMe(token string) (map[string]interface{}, error) {
	req, _ := http.NewRequest("GET", fmt.Sprintf("%s/auth/me", BackendURL), nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch user profile: %s", resp.Status)
	}
	var user map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return nil, err
	}
	return user, nil
}

func newSystemCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "system",
		Short: "Create or update the configuration file.",
		Run:   runSystem,
	}
}

func newDashboardCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "dashboard",
		Short: "Show a live monitoring dashboard.",
		Run:   runDashboard,
	}
}

func runSystem(cmd *cobra.Command, args []string) {
	token, err := LoadToken()
	if err != nil {
		log.Fatalf("You must be logged in to perform this action. Please run 'mirror-cli login'.")
	}

	user, err := fetchMe(token)
	if err != nil {
		log.Fatalf("Failed to fetch user profile: %v", err)
	}

	if user["role"] != "admin" {
		log.Fatalf("You must be an admin to perform this action.")
	}

	var configData configData

	configureServer(&configData)
	configureClusters(&configData)
	configureReplication(&configData)
	configureTopicMappings(&configData)
	configureMonitoring(&configData)
	configureCompliance(&configData)
	configureAI(&configData)

	yamlData, err := yaml.Marshal(&configData)
	if err != nil {
		fmt.Printf("Error marshalling YAML: %v\n", err)
		return
	}

	err = ioutil.WriteFile("configs/default.yml", yamlData, 0644)
	if err != nil {
		fmt.Printf("Error writing config file: %v\n", err)
		return
	}

	fmt.Println("Configuration file `configs/default.yml` created successfully.")
}

func configureServer(configData *configData) {
	qs := []*survey.Question{
		{
			Name:     "server.host",
			Prompt:   &survey.Input{Message: "Enter server host:"},
			Validate: survey.Required,
		},
		{
			Name:     "server.port",
			Prompt:   &survey.Input{Message: "Enter server port:", Default: "8080"},
			Validate: survey.Required,
		},
		{
			Name:     "server.mode",
			Prompt:   &survey.Select{Message: "Select server mode:", Options: []string{"development", "production"}},
			Validate: survey.Required,
		},
		{
			Name:   "server.tls.enabled",
			Prompt: &survey.Confirm{Message: "Enable TLS?"},
		},
	}
	survey.Ask(qs, configData)

	if configData.Server.TLS.Enabled {
		tlsQs := []*survey.Question{
			{
				Name:     "server.tls.cert_file",
				Prompt:   &survey.Input{Message: "Enter path to TLS certificate file:", Default: "server.crt"},
				Validate: survey.Required,
			},
			{
				Name:     "server.tls.key_file",
				Prompt:   &survey.Input{Message: "Enter path to TLS key file:", Default: "server.key"},
				Validate: survey.Required,
			},
		}
		survey.Ask(tlsQs, configData)
	}

	corsQs := []*survey.Question{
		{
			Name:   "server.cors.allowed_origins",
			Prompt: &survey.Input{Message: "Enter allowed CORS origins (comma-separated):", Default: "http://localhost:3000"},
		},
	}
	survey.Ask(corsQs, configData)
}

func configureClusters(configData *configData) {
	addMoreClusters := true
	for addMoreClusters {
		var addCluster bool
		prompt := &survey.Confirm{
			Message: "Add a Kafka cluster?",
		}
		survey.AskOne(prompt, &addCluster)

		if !addCluster {
			addMoreClusters = false
			continue
		}

		var clusterName string
		promptName := &survey.Input{
			Message: "Enter cluster name:",
		}
		survey.AskOne(promptName, &clusterName, survey.WithValidator(survey.Required))

		var brokers string
		promptBrokers := &survey.Input{
			Message: "Enter Kafka brokers (comma-separated):",
		}
		survey.AskOne(promptBrokers, &brokers, survey.WithValidator(survey.Required))

		var securityEnabled bool
		promptSecurity := &survey.Confirm{
			Message: "Enable security for this cluster?",
		}
		survey.AskOne(promptSecurity, &securityEnabled)

		var saslMechanism string
		if securityEnabled {
			promptSASL := &survey.Select{
				Message: "Select SASL mechanism:",
				Options: []string{"GSSAPI", "PLAIN", "SCRAM-SHA-256", "SCRAM-SHA-512"},
			}
			survey.AskOne(promptSASL, &saslMechanism)
		}

		var username, password, serviceName string
		if saslMechanism == "PLAIN" || saslMechanism == "SCRAM-SHA-256" || saslMechanism == "SCRAM-SHA-512" {
			promptUser := &survey.Input{
				Message: "Enter username:",
			}
			survey.AskOne(promptUser, &username)
			promptPass := &survey.Password{
				Message: "Enter password:",
			}
			survey.AskOne(promptPass, &password)
		} else if saslMechanism == "GSSAPI" {
			promptService := &survey.Input{
				Message: "Enter Kerberos service name:",
			}
			survey.AskOne(promptService, &serviceName)
		}

		var tlsEnabled bool
		promptTLS := &survey.Confirm{
			Message: "Enable TLS for this cluster?",
		}
		survey.AskOne(promptTLS, &tlsEnabled)

		var caFile, certFile, keyFile string
		if tlsEnabled {
			promptCA := &survey.Input{
				Message: "Enter path to CA file:",
			}
			survey.AskOne(promptCA, &caFile)
			promptCert := &survey.Input{
				Message: "Enter path to certificate file:",
			}
			survey.AskOne(promptCert, &certFile)
			promptKey := &survey.Input{
				Message: "Enter path to key file:",
			}
			survey.AskOne(promptKey, &keyFile)
		}

		var protocol string
		if tlsEnabled {
			protocol = "SSL"
		} else {
			protocol = "PLAINTEXT"
		}
		if securityEnabled && saslMechanism != "" {
			if tlsEnabled {
				protocol = "SASL_SSL"
			} else {
				protocol = "SASL_PLAINTEXT"
			}
		}

		if configData.Clusters == nil {
			configData.Clusters = make(map[string]struct {
				Brokers  string `yaml:"brokers"`
				Security struct {
					Enabled       bool   `yaml:"enabled"`
					Protocol      string `yaml:"protocol"`
					SASLMechanism string `yaml:"sasl_mechanism"`
					Username      string `yaml:"username"`
					Password      string `yaml:"password"`
					Kerberos      struct {
						ServiceName string `yaml:"service_name"`
					} `yaml:"kerberos"`
				} `yaml:"security"`
			})
		}

		configData.Clusters[clusterName] = struct {
			Brokers  string `yaml:"brokers"`
			Security struct {
				Enabled       bool   `yaml:"enabled"`
				Protocol      string `yaml:"protocol"`
				SASLMechanism string `yaml:"sasl_mechanism"`
				Username      string `yaml:"username"`
				Password      string `yaml:"password"`
				Kerberos      struct {
					ServiceName string `yaml:"service_name"`
				} `yaml:"kerberos"`
			} `yaml:"security"`
		}{
			Brokers: brokers,
			Security: struct {
				Enabled       bool   `yaml:"enabled"`
				Protocol      string `yaml:"protocol"`
				SASLMechanism string `yaml:"sasl_mechanism"`
				Username      string `yaml:"username"`
				Password      string `yaml:"password"`
				Kerberos      struct {
					ServiceName string `yaml:"service_name"`
				} `yaml:"kerberos"`
			}{
				Enabled:       securityEnabled,
				Protocol:      protocol,
				SASLMechanism: saslMechanism,
				Username:      username,
				Password:      password,
				Kerberos: struct {
					ServiceName string `yaml:"service_name"`
				}{
					ServiceName: serviceName,
				},
			},
		}
	}
}

func configureMonitoring(configData *configData) {
	var monitoringEnabled bool
	promptMonitoring := &survey.Confirm{
		Message: "Enable monitoring?",
	}
	survey.AskOne(promptMonitoring, &monitoringEnabled)
	configData.Monitoring.Enabled = monitoringEnabled

	if monitoringEnabled {
		var platform string
		promptPlatform := &survey.Select{
			Message: "Select monitoring platform:",
			Options: []string{"splunk", "loki", "prometheus"},
		}
		survey.AskOne(promptPlatform, &platform)
		configData.Monitoring.Platform = platform

		if platform == "splunk" {
			var hecEndpoint, hecToken, index string
			promptHECEndpoint := &survey.Input{
				Message: "Enter Splunk HEC endpoint:",
			}
			survey.AskOne(promptHECEndpoint, &hecEndpoint)
			promptHECToken := &survey.Input{
				Message: "Enter Splunk HEC token:",
			}
			survey.AskOne(promptHECToken, &hecToken)
			promptIndex := &survey.Input{
				Message: "Enter Splunk index:",
			}
			survey.AskOne(promptIndex, &index)
			configData.Monitoring.Splunk.HECEndpoint = hecEndpoint
			configData.Monitoring.Splunk.HECToken = hecToken
			configData.Monitoring.Splunk.Index = index
		} else if platform == "loki" {
			var endpoint string
			promptEndpoint := &survey.Input{
				Message: "Enter Loki endpoint:",
			}
			survey.AskOne(promptEndpoint, &endpoint)
			configData.Monitoring.Loki.Endpoint = endpoint
		} else if platform == "prometheus" {
			var pushGateway string
			promptPushGateway := &survey.Input{
				Message: "Enter Prometheus Push Gateway URL:",
			}
			survey.AskOne(promptPushGateway, &pushGateway)
			configData.Monitoring.Prometheus.PushGateway = pushGateway
		}
	}

}

func configureCompliance(configData *configData) {
	var enabled bool
	promptEnabled := &survey.Confirm{
		Message: "Enable compliance report scheduler?",
		Default: true,
	}
	survey.AskOne(promptEnabled, &enabled)
	configData.Compliance.Schedule.Enabled = enabled
	if !enabled {
		return
	}

	var runHour int
	promptHour := &survey.Input{
		Message: "Enter report run hour (0-23, local time):",
		Default: "2",
	}
	survey.AskOne(promptHour, &runHour)
	configData.Compliance.Schedule.RunHour = runHour

	var daily bool
	var weekly bool
	var monthly bool
	survey.AskOne(&survey.Confirm{Message: "Generate daily reports?", Default: true}, &daily)
	survey.AskOne(&survey.Confirm{Message: "Generate weekly reports? (Monday)", Default: false}, &weekly)
	survey.AskOne(&survey.Confirm{Message: "Generate monthly reports? (1st of month)", Default: false}, &monthly)

	configData.Compliance.Schedule.Daily = daily
	configData.Compliance.Schedule.Weekly = weekly
	configData.Compliance.Schedule.Monthly = monthly
}

func configureReplication(configData *configData) {
	qs := []*survey.Question{
		{
			Name:     "replication.batch_size",
			Prompt:   &survey.Input{Message: "Enter replication batch size:", Default: "1000"},
			Validate: survey.Required,
		},
		{
			Name:     "replication.parallelism",
			Prompt:   &survey.Input{Message: "Enter replication parallelism:", Default: "4"},
			Validate: survey.Required,
		},
		{
			Name:     "replication.compression",
			Prompt:   &survey.Select{Message: "Select replication compression:", Options: []string{"none", "gzip", "snappy", "lz4", "zstd"}},
			Validate: survey.Required,
		},
		{
			Name:     "replication.topic_discovery_interval",
			Prompt:   &survey.Input{Message: "Enter topic discovery interval (0 to disable):", Default: "5m"},
			Validate: survey.Required,
		},
	}
	survey.Ask(qs, configData)
}

func configureTopicMappings(configData *configData) {
	addMoreMappings := true
	for addMoreMappings {
		var addMapping bool
		prompt := &survey.Confirm{
			Message: "Add a topic mapping?",
		}
		survey.AskOne(prompt, &addMapping)

		if !addMapping {
			addMoreMappings = false
			continue
		}

		var mapping struct {
			Source  string `yaml:"source"`
			Target  string `yaml:"target"`
			Enabled bool   `yaml:"enabled"`
		}
		qs := []*survey.Question{
			{
				Name:     "source",
				Prompt:   &survey.Input{Message: "Enter source topic pattern:"},
				Validate: survey.Required,
			},
			{
				Name:     "target",
				Prompt:   &survey.Input{Message: "Enter target topic pattern:"},
				Validate: survey.Required,
			},
			{
				Name:   "enabled",
				Prompt: &survey.Confirm{Message: "Enable this mapping?"},
			},
		}
		if err := survey.Ask(qs, &mapping); err != nil {
			log.Printf("Error creating mapping: %v", err)
			return
		}
		configData.TopicMappings = append(configData.TopicMappings, mapping)
	}
}

func runEdit(section string, args ...string) {
	configFile := "configs/default.yml"
	configBytes, err := ioutil.ReadFile(configFile)
	if err != nil {
		log.Fatalf("Failed to read config file: %v", err)
	}

	var fullConfig configData
	if err := yaml.Unmarshal(configBytes, &fullConfig); err != nil {
		log.Fatalf("Failed to unmarshal config data: %v", err)
	}

	var sectionData interface{}
	var ok bool

	switch section {
	case "server":
		sectionData = fullConfig.Server
	case "cluster":
		if len(args) == 0 {
			log.Fatalf("Cluster name is required for editing.")
		}
		clusterName := args[0]
		if sectionData, ok = fullConfig.Clusters[clusterName]; !ok {
			log.Fatalf("Cluster '%s' not found.", clusterName)
		}
	case "monitoring":
		sectionData = fullConfig.Monitoring
	case "ai":
		sectionData = fullConfig.AI
	case "topic-mapping":
		if len(args) == 0 {
			log.Fatalf("Source topic is required for editing.")
		}
		sourceTopic := args[0]
		found := false
		for i, mapping := range fullConfig.TopicMappings {
			if mapping.Source == sourceTopic {
				sectionData = fullConfig.TopicMappings[i]
				found = true
				break
			}
		}
		if !found {
			log.Fatalf("Topic mapping with source '%s' not found.", sourceTopic)
		}
	default:
		log.Fatalf("Unknown configuration section: %s", section)
	}

	yamlSection, err := yaml.Marshal(sectionData)
	if err != nil {
		log.Fatalf("Failed to marshal section to YAML: %v", err)
	}

	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vim"
	}

	tmpfile, err := ioutil.TempFile("", "config-*.yml")
	if err != nil {
		log.Fatalf("Failed to create temporary file: %v", err)
	}
	defer os.Remove(tmpfile.Name())

	if _, err := tmpfile.Write(yamlSection); err != nil {
		log.Fatalf("Failed to write to temporary file: %v", err)
	}
	if err := tmpfile.Close(); err != nil {
		log.Fatalf("Failed to close temporary file: %v", err)
	}

	cmd := exec.Command(editor, tmpfile.Name())
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		log.Fatalf("Editor command failed: %v", err)
	}

	editedYAML, err := ioutil.ReadFile(tmpfile.Name())
	if err != nil {
		log.Fatalf("Failed to read edited file: %v", err)
	}

	var updatedSection interface{}
	if err := yaml.Unmarshal(editedYAML, &updatedSection); err != nil {
		log.Fatalf("Failed to unmarshal edited YAML: %v", err)
	}

	// This is a simplified way to update the main config.
	// A more robust solution would use reflection or map manipulation.
	log.Println("Updating configuration is not fully implemented in this example.")
	log.Println("Please manually update your `configs/default.yml` and use `config set`.")
}

func configureAI(configData *configData) {
	qsAI := []*survey.Question{
		{
			Name: "ai.provider",
			Prompt: &survey.Select{
				Message: "Select AI provider:",
				Options: []string{"openai", "custom"},
				Default: "openai",
			},
		},
		{
			Name:   "ai.endpoint",
			Prompt: &survey.Input{Message: "Enter AI API endpoint:", Default: "https://api.openai.com/v1"},
		},
		{
			Name:   "ai.token",
			Prompt: &survey.Password{Message: "Enter AI API token:"},
		},
		{
			Name:   "ai.model",
			Prompt: &survey.Input{Message: "Enter AI model:", Default: "gpt-4"},
		},
	}
	survey.Ask(qsAI, configData)
}

func runDashboard(cmd *cobra.Command, args []string) {
	token, err := LoadToken()
	if err != nil {
		log.Fatalf("You must be logged in to perform this action. Please run 'mirror-cli login'.")
	}

	// Create DataFetchers struct with existing fetch functions
	fetchers := core.DataFetchers{
		FetchJobs:        func() ([]map[string]interface{}, error) { return fetchJobs(token) },
		FetchClusters:    func() ([]map[string]interface{}, error) { return fetchClusters(token) },
		FetchLogs:        func() ([]map[string]interface{}, error) { return fetchLogs(token) },
		FetchAIInsights:  func() ([]map[string]interface{}, error) { return fetchAIInsights(token) },
		FetchMe:          func() (map[string]interface{}, error) { return fetchMe(token) },
		FetchJob:         func(jobID string) (map[string]interface{}, error) { return fetchJob(token, jobID) },
		FetchCluster:     func(clusterName string) (map[string]interface{}, error) { return fetchCluster(token, clusterName) },
		FetchJobMappings: func(jobID string) ([]map[string]interface{}, error) { return fetchJobMappings(token, jobID) },
		FetchMetrics:     func(jobID string) (map[string]interface{}, error) { return fetchMetrics(token, jobID) },
	}

	// Create and run the new hierarchical dashboard
	d := dashboard.NewDashboard(token, fetchers)
	if err := d.Run(); err != nil {
		log.Fatalf("Dashboard error: %v", err)
	}
}

func fetchJobs(token string) ([]map[string]interface{}, error) {
	req, _ := http.NewRequest("GET", fmt.Sprintf("%s/api/v1/jobs", BackendURL), nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch jobs: %s", resp.Status)
	}
	var jobs []map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&jobs); err != nil {
		return nil, err
	}
	return jobs, nil
}

// safeString safely converts an interface{} to string, returning default if nil
func safeString(value interface{}, defaultValue string) string {
	if value == nil {
		return defaultValue
	}
	if str, ok := value.(string); ok {
		return str
	}
	return defaultValue
}

func fetchMetrics(token, jobID string) (map[string]interface{}, error) {
	req, _ := http.NewRequest("GET", fmt.Sprintf("%s/api/v1/jobs/%s/metrics/current", BackendURL, jobID), nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var metrics map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&metrics)
	return metrics, nil
}

func fetchLogs(token string) ([]map[string]interface{}, error) {
	req, _ := http.NewRequest("GET", fmt.Sprintf("%s/api/v1/events", BackendURL), nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var logs []map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&logs)
	return logs, nil
}

func fetchAIInsights(token string) ([]map[string]interface{}, error) {
	req, _ := http.NewRequest("GET", fmt.Sprintf("%s/api/v1/ai/insights?limit=20", BackendURL), nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var insights []map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&insights)
	return insights, nil
}

func pauseJob(token, jobID string) error {
	req, _ := http.NewRequest("POST", fmt.Sprintf("%s/api/v1/jobs/%s/pause", BackendURL, jobID), nil)
	req.Header.Set("Authorization", "Bearer "+token)
	client := &http.Client{}
	_, err := client.Do(req)
	return err
}

func stopJob(token, jobID string) error {
	req, _ := http.NewRequest("POST", fmt.Sprintf("%s/api/v1/jobs/%s/stop", BackendURL, jobID), nil)
	req.Header.Set("Authorization", "Bearer "+token)
	client := &http.Client{}
	_, err := client.Do(req)
	return err
}

func startJob(token, jobID string) error {
	req, _ := http.NewRequest("POST", fmt.Sprintf("%s/api/v1/jobs/%s/start", BackendURL, jobID), nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := ioutil.ReadAll(resp.Body)
		return fmt.Errorf("server returned status %s: %s", resp.Status, string(body))
	}

	return nil
}

// createDocsCommand creates the documentation generation command using Cobra's doc generation
func createDocsCommand() *cobra.Command {
	docsCmd := &cobra.Command{
		Use:   "docs",
		Short: "Generate CLI documentation",
		Long:  "Generate CLI documentation in various formats using Cobra's built-in documentation generation",
	}

	generateCmd := &cobra.Command{
		Use:   "generate",
		Short: "Generate single comprehensive Markdown documentation file",
		Long:  "Generate a single comprehensive Markdown file with all CLI commands and subcommands",
		Run: func(cmd *cobra.Command, args []string) {
			filename, _ := cmd.Flags().GetString("file")
			if filename == "" {
				filename = "docs/cli-commands.md"
			}

			// Create output directory
			dir := filepath.Dir(filename)
			if err := os.MkdirAll(dir, 0755); err != nil {
				fmt.Printf("Error: Failed to create directory: %v\n", err)
				return
			}

			// Create the output file
			file, err := os.Create(filename)
			if err != nil {
				fmt.Printf("Error: Failed to create file: %v\n", err)
				return
			}
			defer file.Close()

			rootCmd := NewRootCmd()

			// Generate consolidated documentation
			if err := generateConsolidatedDocs(rootCmd, file); err != nil {
				fmt.Printf("Error generating documentation: %v\n", err)
				return
			}

			fmt.Printf("Documentation generated successfully: %s\n", filename)
		},
	}

	generateCmd.Flags().String("file", "docs/cli-commands.md", "Output file for documentation")

	docsCmd.AddCommand(generateCmd)
	return docsCmd
}

// generateConsolidatedDocs generates a single consolidated markdown file with all commands
func generateConsolidatedDocs(cmd *cobra.Command, w *os.File) error {
	fmt.Fprintf(w, "# %s\n\n", cmd.Name())
	fmt.Fprintf(w, "%s\n\n", cmd.Short)

	if cmd.Long != "" {
		fmt.Fprintf(w, "## Description\n\n%s\n\n", cmd.Long)
	}

	if cmd.Example != "" {
		fmt.Fprintf(w, "## Examples\n\n```\n%s\n```\n\n", cmd.Example)
	}

	// Global flags
	if cmd.HasAvailableFlags() || cmd.HasAvailablePersistentFlags() {
		fmt.Fprintf(w, "## Global Options\n\n")
		fmt.Fprintf(w, "```\n")
		cmd.InheritedFlags().VisitAll(func(flag *pflag.Flag) {
			if !flag.Hidden {
				fmt.Fprintf(w, "  -%s, --%s", flag.Shorthand, flag.Name)
				if flag.Value.Type() != "bool" {
					fmt.Fprintf(w, " %s", flag.Value.Type())
				}
				fmt.Fprintf(w, "   %s", flag.Usage)
				if flag.DefValue != "" && flag.DefValue != "false" {
					fmt.Fprintf(w, " (default %q)", flag.DefValue)
				}
				fmt.Fprintf(w, "\n")
			}
		})
		cmd.Flags().VisitAll(func(flag *pflag.Flag) {
			if !flag.Hidden {
				fmt.Fprintf(w, "  -%s, --%s", flag.Shorthand, flag.Name)
				if flag.Value.Type() != "bool" {
					fmt.Fprintf(w, " %s", flag.Value.Type())
				}
				fmt.Fprintf(w, "   %s", flag.Usage)
				if flag.DefValue != "" && flag.DefValue != "false" {
					fmt.Fprintf(w, " (default %q)", flag.DefValue)
				}
				fmt.Fprintf(w, "\n")
			}
		})
		fmt.Fprintf(w, "```\n\n")
	}

	// Generate docs for all subcommands recursively
	if len(cmd.Commands()) > 0 {
		fmt.Fprintf(w, "## Available Commands\n\n")
		for _, subCmd := range cmd.Commands() {
			if !subCmd.IsAvailableCommand() || subCmd.IsAdditionalHelpTopicCommand() {
				continue
			}
			if err := generateCommandDocs(subCmd, w, 3); err != nil {
				return err
			}
		}
	}

	fmt.Fprintf(w, "---\n\n*Auto generated by spf13/cobra on %s*\n", time.Now().Format("2-Jan-2006"))

	return nil
}

// generateCommandDocs generates documentation for a command and its subcommands recursively
func generateCommandDocs(cmd *cobra.Command, w *os.File, headerLevel int) error {
	// Generate header
	headerPrefix := strings.Repeat("#", headerLevel)
	fmt.Fprintf(w, "%s %s\n\n", headerPrefix, cmd.CommandPath())

	// Short description
	if cmd.Short != "" {
		fmt.Fprintf(w, "**%s**\n\n", cmd.Short)
	}

	// Long description
	if cmd.Long != "" && cmd.Long != cmd.Short {
		fmt.Fprintf(w, "%s\n\n", cmd.Long)
	}

	// Usage
	fmt.Fprintf(w, "### Usage\n\n")
	fmt.Fprintf(w, "```\n%s\n```\n\n", cmd.UseLine())

	// Examples
	if cmd.Example != "" {
		fmt.Fprintf(w, "### Examples\n\n")
		fmt.Fprintf(w, "```\n%s\n```\n\n", cmd.Example)
	}

	// Flags specific to this command
	if cmd.HasAvailableLocalFlags() {
		fmt.Fprintf(w, "### Options\n\n")
		fmt.Fprintf(w, "```\n")
		cmd.LocalFlags().VisitAll(func(flag *pflag.Flag) {
			if !flag.Hidden {
				fmt.Fprintf(w, "  -%s, --%s", flag.Shorthand, flag.Name)
				if flag.Value.Type() != "bool" {
					fmt.Fprintf(w, " %s", flag.Value.Type())
				}
				fmt.Fprintf(w, "   %s", flag.Usage)
				if flag.DefValue != "" && flag.DefValue != "false" {
					fmt.Fprintf(w, " (default %q)", flag.DefValue)
				}
				fmt.Fprintf(w, "\n")
			}
		})
		fmt.Fprintf(w, "```\n\n")
	}

	// Subcommands
	if len(cmd.Commands()) > 0 {
		availableSubCommands := make([]*cobra.Command, 0)
		for _, subCmd := range cmd.Commands() {
			if subCmd.IsAvailableCommand() && !subCmd.IsAdditionalHelpTopicCommand() {
				availableSubCommands = append(availableSubCommands, subCmd)
			}
		}

		if len(availableSubCommands) > 0 {
			fmt.Fprintf(w, "### Available Subcommands\n\n")
			for _, subCmd := range availableSubCommands {
				fmt.Fprintf(w, "- **%s** - %s\n", subCmd.Name(), subCmd.Short)
			}
			fmt.Fprintf(w, "\n")

			for _, subCmd := range availableSubCommands {
				if err := generateCommandDocs(subCmd, w, headerLevel+1); err != nil {
					return err
				}
			}
		}
	}

	return nil
}

func restartJob(token, jobID string) error {
	req, _ := http.NewRequest("POST", fmt.Sprintf("%s/api/v1/jobs/%s/restart", BackendURL, jobID), nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := ioutil.ReadAll(resp.Body)
		return fmt.Errorf("server returned status %s: %s", resp.Status, string(body))
	}

	return nil
}

func createNewCluster(token string) {
	var cluster struct {
		Name           string `json:"name"`
		Brokers        string `json:"brokers"`
		SecurityConfig string `json:"security_config"`
	}

	qs := []*survey.Question{
		{
			Name:     "name",
			Prompt:   &survey.Input{Message: "Enter cluster name:"},
			Validate: survey.Required,
		},
		{
			Name:     "brokers",
			Prompt:   &survey.Input{Message: "Enter broker list (comma-separated):"},
			Validate: survey.Required,
		},
		{
			Name:     "security_config",
			Prompt:   &survey.Editor{Message: "Enter security configuration (JSON):"},
			Validate: survey.Required,
		},
	}

	if err := survey.Ask(qs, &cluster); err != nil {
		log.Printf("Error creating cluster: %v", err)
		return
	}

	reqBody, _ := json.Marshal(cluster)
	req, _ := http.NewRequest("POST", fmt.Sprintf("%s/api/v1/clusters", BackendURL), bytes.NewBuffer(reqBody))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		log.Printf("Failed to create cluster: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		log.Printf("Failed to create cluster: %s", resp.Status)
		return
	}

	log.Println("Cluster created successfully.")
}

func createNewJob(token string) {
	var job struct {
		Name          string `json:"name"`
		SourceCluster string `json:"source_cluster"`
		TargetCluster string `json:"target_cluster"`
	}

	clusters, err := fetchClusters(token)
	if err != nil {
		log.Printf("Error fetching clusters: %v", err)
		return
	}
	var clusterNames []string
	for _, c := range clusters {
		clusterNames = append(clusterNames, c["name"].(string))
	}

	qs := []*survey.Question{
		{
			Name:     "name",
			Prompt:   &survey.Input{Message: "Enter job name:"},
			Validate: survey.Required,
		},
		{
			Name: "source_cluster",
			Prompt: &survey.Select{
				Message: "Select source cluster:",
				Options: clusterNames,
			},
			Validate: survey.Required,
		},
		{
			Name: "target_cluster",
			Prompt: &survey.Select{
				Message: "Select target cluster:",
				Options: clusterNames,
			},
			Validate: survey.Required,
		},
	}

	if err := survey.Ask(qs, &job); err != nil {
		log.Printf("Error creating job: %v", err)
		return
	}

	fmt.Println("Fetching topics from source cluster...")
	topics, err := fetchTopics(token, job.SourceCluster)
	if err != nil {
		log.Printf("Error fetching topics: %v", err)
		return
	}

	var selectedTopics []string
	prompt := &survey.MultiSelect{
		Message: "Select topics to mirror:",
		Options: topics,
	}
	survey.AskOne(prompt, &selectedTopics)

	// Configure replication settings for this job
	fmt.Println("\n=== Replication Settings ===")

	var batchSize string
	promptBatch := &survey.Input{
		Message: "Batch size (messages per batch):",
		Default: "1000",
	}
	survey.AskOne(promptBatch, &batchSize)

	var parallelism string
	promptParallelism := &survey.Input{
		Message: "Parallelism (consumer threads):",
		Default: "4",
	}
	survey.AskOne(promptParallelism, &parallelism)

	var compression string
	promptCompression := &survey.Select{
		Message: "Compression algorithm:",
		Options: []string{"none", "gzip", "snappy", "lz4", "zstd"},
		Default: "none",
	}
	survey.AskOne(promptCompression, &compression)

	var enablePartitionMapping bool
	promptPartitions := &survey.Confirm{
		Message: "Preserve partition mapping?",
		Default: true,
	}
	survey.AskOne(promptPartitions, &enablePartitionMapping)

	// Topic mappings with custom target names option
	var mappings []map[string]interface{}
	for _, topic := range selectedTopics {
		var targetTopic string
		var customTarget bool
		promptCustom := &survey.Confirm{
			Message: fmt.Sprintf("Use custom target name for topic '%s'?", topic),
			Default: false,
		}
		survey.AskOne(promptCustom, &customTarget)

		if customTarget {
			promptTarget := &survey.Input{
				Message: fmt.Sprintf("Target topic name for '%s':", topic),
				Default: topic,
			}
			survey.AskOne(promptTarget, &targetTopic)
		} else {
			targetTopic = topic
		}

		mappings = append(mappings, map[string]interface{}{
			"source_topic_pattern": topic,
			"target_topic_pattern": targetTopic,
			"enabled":              true,
		})
	}

	reqBody, _ := json.Marshal(map[string]interface{}{
		"name":           job.Name,
		"source_cluster": job.SourceCluster,
		"target_cluster": job.TargetCluster,
		"mappings":       mappings,
		"replication_settings": map[string]interface{}{
			"batch_size":          batchSize,
			"parallelism":         parallelism,
			"compression":         compression,
			"preserve_partitions": enablePartitionMapping,
		},
	})
	req, _ := http.NewRequest("POST", fmt.Sprintf("%s/api/v1/jobs", BackendURL), bytes.NewBuffer(reqBody))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		log.Printf("Failed to create job: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		body, _ := ioutil.ReadAll(resp.Body)
		log.Printf("Failed to create job: %s\n%s", resp.Status, body)
		return
	}

	log.Println("Job created successfully.")
}

func fetchClusters(token string) ([]map[string]interface{}, error) {
	req, _ := http.NewRequest("GET", fmt.Sprintf("%s/api/v1/clusters", BackendURL), nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var clusters []map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&clusters)
	return clusters, nil
}

func fetchTopics(token, clusterName string) ([]string, error) {
	req, _ := http.NewRequest("GET", fmt.Sprintf("%s/api/v1/clusters/%s/topics", BackendURL, clusterName), nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := ioutil.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to fetch topics: %s\n%s", resp.Status, body)
	}
	var topics []string
	if err := json.NewDecoder(resp.Body).Decode(&topics); err != nil {
		return nil, err
	}
	return topics, nil
}

func listClusterTopics(token, clusterName string) {
	req, _ := http.NewRequest("GET", fmt.Sprintf("%s/api/v1/clusters/%s/topics", BackendURL, clusterName), nil)
	req.Header.Set("Authorization", "Bearer "+token)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("Error: Failed to connect to backend: %v\n", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := ioutil.ReadAll(resp.Body)
		fmt.Printf("Error: Failed to list topics: %s\n%s\n", resp.Status, body)
		return
	}

	var topics []string
	if err := json.NewDecoder(resp.Body).Decode(&topics); err != nil {
		fmt.Printf("Error: Failed to parse response: %v\n", err)
		return
	}

	fmt.Println("Topics:")
	for _, topic := range topics {
		fmt.Printf("- %s\n", topic)
	}
}

func testClusterConnectionVerbose(token, clusterName string) error {
	// Step 1: Fetch topic names as a basic connectivity and permissions test.
	topics, err := fetchTopics(token, clusterName)
	if err != nil {
		return fmt.Errorf("failed to list topics: %v", err)
	}

	// If the cluster is reachable but has no topics, we consider it healthy.
	// A deeper check is only needed if topics exist.
	if len(topics) > 0 {
		// Step 2: Fetch details for all topics to ensure deeper access.
		_, err := fetchTopicDetails(token, clusterName, topics)
		if err != nil {
			return fmt.Errorf("failed to get topic details: %v", err)
		}
	}

	return nil
}

// fetchTopicDetails fetches detailed information for a list of topics.
func fetchTopicDetails(token, clusterName string, topics []string) ([]map[string]interface{}, error) {
	req, _ := http.NewRequest("GET", fmt.Sprintf("%s/api/v1/clusters/%s/topic-details?topics=%s", BackendURL, clusterName, strings.Join(topics, ",")), nil)
	req.Header.Set("Authorization", "Bearer "+token)
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := ioutil.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to fetch topic details: %s\n%s", resp.Status, body)
	}
	var details []map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&details); err != nil {
		return nil, err
	}
	return details, nil
}

// createReplicationJob creates a new replication job with improved topic selection
func createReplicationJob(token string) {
	clusters, err := fetchClusters(token)
	if err != nil {
		fmt.Printf("Error: Failed to fetch clusters: %v\n", err)
		return
	}

	if len(clusters) < 2 {
		fmt.Println("Error: At least two clusters are required to create a replication job.")
		return
	}

	var clusterNames []string
	for _, c := range clusters {
		clusterNames = append(clusterNames, c["name"].(string))
	}

	var jobName string
	promptName := &survey.Input{
		Message: "Enter job name:",
	}
	survey.AskOne(promptName, &jobName, survey.WithValidator(survey.Required))

	var sourceCluster string
	promptSource := &survey.Select{
		Message: "Select source cluster:",
		Options: clusterNames,
	}
	survey.AskOne(promptSource, &sourceCluster)

	var targetCluster string
	promptTarget := &survey.Select{
		Message: "Select target cluster:",
		Options: clusterNames,
	}
	survey.AskOne(promptTarget, &targetCluster)

	if sourceCluster == targetCluster {
		fmt.Println("Error: Source and target clusters cannot be the same.")
		return
	}

	// Health checks
	fmt.Println("Performing cluster health checks...")
	if err := testClusterConnectionVerbose(token, sourceCluster); err != nil {
		fmt.Printf("Error: Source cluster '%s' is unreachable: %v\n", sourceCluster, err)
		return
	}
	fmt.Printf("✓ Source cluster '%s' is healthy.\n", sourceCluster)

	if err := testClusterConnectionVerbose(token, targetCluster); err != nil {
		fmt.Printf("Error: Target cluster '%s' is unreachable: %v\n", targetCluster, err)
		return
	}
	fmt.Printf("✓ Target cluster '%s' is healthy.\n", targetCluster)

	// Fetch topics from source cluster
	fmt.Printf("Fetching topics from source cluster '%s'...\n", sourceCluster)
	topics, err := fetchTopics(token, sourceCluster)
	if err != nil {
		fmt.Printf("Error: Failed to fetch topics from source cluster: %v\n", err)
		return
	}

	if len(topics) == 0 {
		fmt.Println("No topics found in source cluster.")
		return
	}

	fmt.Printf("Found %d topics in source cluster.\n", len(topics))

	// Allow user to select topics or patterns
	var selectedTopics []string
	promptTopics := &survey.MultiSelect{
		Message: "Select topics to replicate:",
		Options: topics,
	}
	survey.AskOne(promptTopics, &selectedTopics)

	if len(selectedTopics) == 0 {
		fmt.Println("No topics selected. Job creation cancelled.")
		return
	}

	// Fetch and display topic details
	details, err := fetchTopicDetails(token, sourceCluster, selectedTopics)
	if err != nil {
		fmt.Printf("Warning: Could not fetch topic details: %v\n", err)
	} else {
		fmt.Println("\n--- Topic Details ---")
		w := new(bytes.Buffer)
		writer := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintln(writer, "Topic\tPartitions\tReplicas\tCompression")
		for _, d := range details {
			fmt.Fprintf(writer, "%s\t%v\t%v\t%v\n", d["name"], d["partitions"], d["replication_factor"], d["compression_type"])
		}
		writer.Flush()
		fmt.Println(w.String())
	}

	// Configure replication settings
	fmt.Println("\n=== Replication Settings ===")

	var batchSize int
	promptBatch := &survey.Input{
		Message: "Batch size (messages per batch):",
		Default: "1000",
	}
	var batchStr string
	survey.AskOne(promptBatch, &batchStr)
	fmt.Sscanf(batchStr, "%d", &batchSize)

	var parallelism int
	promptParallelism := &survey.Input{
		Message: "Parallelism (consumer threads):",
		Default: "4",
	}
	var parallelismStr string
	survey.AskOne(promptParallelism, &parallelismStr)
	fmt.Sscanf(parallelismStr, "%d", &parallelism)

	var compression string
	promptCompression := &survey.Select{
		Message: "Compression algorithm:",
		Options: []string{"none", "gzip", "snappy", "lz4", "zstd"},
		Default: "none",
	}
	survey.AskOne(promptCompression, &compression)

	var preservePartitions bool
	promptPartitions := &survey.Confirm{
		Message: "Preserve partition mapping?",
		Default: true,
	}
	survey.AskOne(promptPartitions, &preservePartitions)

	// Topic mappings with custom target names option
	fmt.Println("\n=== Topic Mapping Configuration ===")
	var mappings []map[string]interface{}

	for _, topic := range selectedTopics {
		var targetTopic string
		var customTarget bool

		promptCustom := &survey.Confirm{
			Message: fmt.Sprintf("Use custom target name for topic '%s'?", topic),
			Default: false,
		}
		survey.AskOne(promptCustom, &customTarget)

		if customTarget {
			promptTarget := &survey.Input{
				Message: fmt.Sprintf("Target topic name for '%s':", topic),
				Default: topic,
			}
			survey.AskOne(promptTarget, &targetTopic)
		} else {
			targetTopic = topic
		}

		mappings = append(mappings, map[string]interface{}{
			"source_topic_pattern": topic,
			"target_topic_pattern": targetTopic,
			"enabled":              true,
		})
	}

	jobRequest := map[string]interface{}{
		"name":                jobName,
		"source_cluster_name": sourceCluster,
		"target_cluster_name": targetCluster,
		"topic_mappings":      mappings,
		"batch_size":          batchSize,
		"parallelism":         parallelism,
		"compression":         compression,
		"preserve_partitions": preservePartitions,
	}

	fmt.Println("\n=== Job Summary ===")
	fmt.Printf("Name: %s\n", jobName)
	fmt.Printf("Source: %s\n", sourceCluster)
	fmt.Printf("Target: %s\n", targetCluster)
	fmt.Printf("Topics/Patterns: %d\n", len(selectedTopics))
	fmt.Printf("Batch Size: %d\n", batchSize)
	fmt.Printf("Parallelism: %d\n", parallelism)
	fmt.Printf("Compression: %s\n", compression)
	fmt.Printf("Preserve Partitions: %t\n", preservePartitions)

	var confirm bool
	promptConfirm := &survey.Confirm{
		Message: "Create this replication job?",
		Default: true,
	}
	survey.AskOne(promptConfirm, &confirm)

	if !confirm {
		fmt.Println("Job creation cancelled.")
		return
	}

	reqBody, _ := json.Marshal(jobRequest)
	req, _ := http.NewRequest("POST", fmt.Sprintf("%s/api/v1/jobs", BackendURL), bytes.NewBuffer(reqBody))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("Error: Failed to connect to backend: %v\n", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		body, _ := ioutil.ReadAll(resp.Body)
		fmt.Printf("Error: Failed to create job: %s\n", resp.Status)
		fmt.Printf("Response: %s\n", string(body))
		return
	}

	fmt.Printf("Replication job '%s' created successfully!\n", jobName)
	fmt.Println("Use 'mirror-cli jobs start' to start the job.")
}

func createJobsCommand() *cobra.Command {
	jobsCmd := &cobra.Command{
		Use:   "jobs",
		Short: "Manage replication jobs",
		Long:  "Create, start, stop, and manage Kafka replication jobs",
	}

	listJobsCmd := &cobra.Command{
		Use:   "list",
		Short: "List all replication jobs",
		Long:  "Display all configured replication jobs with their status",
		Run: func(cmd *cobra.Command, args []string) {
			token, err := LoadToken()
			if err != nil {
				fmt.Println("Error: You must be logged in to perform this action. Please run 'mirror-cli login'.")
				return
			}

			jobs, err := fetchJobs(token)
			if err != nil {
				fmt.Printf("Error: Failed to fetch jobs: %v\n", err)
				return
			}

			if len(jobs) == 0 {
				fmt.Println("No replication jobs found.")
				fmt.Println("Create a new job with: mirror-cli jobs add")
				return
			}

			w := new(bytes.Buffer)
			writer := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
			fmt.Fprintln(writer, "ID\tNAME\tSOURCE\tTARGET\tSTATUS\tLAST ACTIVE\tCREATED")

			for _, job := range jobs {
				createdAt := "N/A"
				if job["created_at"] != nil {
					createdAt = job["created_at"].(string)
				}

				sourceCluster := "N/A"
				if job["source_cluster_name"] != nil {
					sourceCluster = job["source_cluster_name"].(string)
				}

				targetCluster := "N/A"
				if job["target_cluster_name"] != nil {
					targetCluster = job["target_cluster_name"].(string)
				}

				lastActive := "Never"
				if job["status"].(string) == "active" {
					metrics, err := fetchMetrics(token, job["id"].(string))
					if err == nil && metrics["timestamp"] != nil {
						if timestamp, ok := metrics["timestamp"].(string); ok {
							if t, err := time.Parse(time.RFC3339, timestamp); err == nil {
								lastActive = formatLastActive(t)
							}
						}
					}
				}

				fmt.Fprintf(writer, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
					job["id"].(string),
					job["name"].(string),
					sourceCluster,
					targetCluster,
					job["status"].(string),
					lastActive,
					createdAt,
				)
			}
			writer.Flush()
			fmt.Print(w.String())
		},
	}

	addJobCmd := &cobra.Command{
		Use:   "add",
		Short: "Create a new replication job",
		Long:  "Create a new replication job with interactive configuration",
		Run: func(cmd *cobra.Command, args []string) {
			token, err := LoadToken()
			if err != nil {
				fmt.Println("Error: You must be logged in to perform this action. Please run 'mirror-cli login'.")
				return
			}

			user, err := fetchMe(token)
			if err != nil {
				fmt.Printf("Error: Failed to fetch user profile: %v\n", err)
				return
			}

			if user["role"] != "admin" && user["role"] != "operator" {
				fmt.Println("Error: You must be an admin or operator to perform this action.")
				return
			}

			createReplicationJob(token)
		},
	}

	startJobCmd := &cobra.Command{
		Use:   "start [job-id]",
		Short: "Start a replication job",
		Long:  "Start a paused or stopped replication job",
		Args:  cobra.MaximumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			token, err := LoadToken()
			if err != nil {
				fmt.Println("Error: You must be logged in to perform this action. Please run 'mirror-cli login'.")
				return
			}

			user, err := fetchMe(token)
			if err != nil {
				fmt.Printf("Error: Failed to fetch user profile: %v\n", err)
				return
			}

			if user["role"] != "admin" && user["role"] != "operator" {
				fmt.Println("Error: You must be an admin or operator to perform this action.")
				return
			}

			var jobID string
			if len(args) > 0 {
				jobID = args[0]
			} else {
				jobs, err := fetchJobs(token)
				if err != nil {
					fmt.Printf("Error: Failed to fetch jobs: %v\n", err)
					return
				}

				if len(jobs) == 0 {
					fmt.Println("No jobs found.")
					return
				}

				var jobOptions []string
				for _, job := range jobs {
					if job["status"].(string) != "active" {
						jobOptions = append(jobOptions, fmt.Sprintf("%s (%s)", job["name"].(string), job["id"].(string)))
					}
				}

				if len(jobOptions) == 0 {
					fmt.Println("No startable jobs found. All jobs are already active.")
					return
				}
				jobOptions = append(jobOptions, "All")

				var selectedJob string
				prompt := &survey.Select{
					Message: "Select job to start:",
					Options: jobOptions,
				}
				survey.AskOne(prompt, &selectedJob)

				if selectedJob == "All" {
					req, _ := http.NewRequest("POST", fmt.Sprintf("%s/api/v1/jobs/start-all", BackendURL), nil)
					req.Header.Set("Authorization", "Bearer "+token)
					resp, err := httpClient.Do(req)
					if err != nil {
						fmt.Printf("Error: Failed to connect to backend: %v\n", err)
						return
					}
					defer resp.Body.Close()
					if resp.StatusCode != http.StatusOK {
						fmt.Printf("Error: Failed to start all jobs: %s\n", resp.Status)
						return
					}
					fmt.Println("All jobs started successfully.")
					return
				}

				parts := strings.Split(selectedJob, " (")
				if len(parts) == 2 {
					jobID = strings.TrimSuffix(parts[1], ")")
				}
			}

			if err := startJob(token, jobID); err != nil {
				fmt.Printf("Error: Failed to start job: %v\n", err)
				return
			}

			fmt.Printf("Job %s started successfully.\n", jobID)
		},
	}

	stopJobCmd := &cobra.Command{
		Use:   "stop [job-id]",
		Short: "Stop a replication job",
		Long:  "Stop a running replication job",
		Args:  cobra.MaximumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			token, err := LoadToken()
			if err != nil {
				fmt.Println("Error: You must be logged in to perform this action. Please run 'mirror-cli login'.")
				return
			}

			user, err := fetchMe(token)
			if err != nil {
				fmt.Printf("Error: Failed to fetch user profile: %v\n", err)
				return
			}

			if user["role"] != "admin" && user["role"] != "operator" {
				fmt.Println("Error: You must be an admin or operator to perform this action.")
				return
			}

			var jobID string
			if len(args) > 0 {
				jobID = args[0]
			} else {
				jobs, err := fetchJobs(token)
				if err != nil {
					fmt.Printf("Error: Failed to fetch jobs: %v\n", err)
					return
				}

				if len(jobs) == 0 {
					fmt.Println("No jobs found.")
					return
				}

				var jobOptions []string
				for _, job := range jobs {
					if job["status"].(string) == "active" {
						jobOptions = append(jobOptions, fmt.Sprintf("%s (%s)", job["name"].(string), job["id"].(string)))
					}
				}

				if len(jobOptions) == 0 {
					fmt.Println("No running jobs found.")
					return
				}
				jobOptions = append(jobOptions, "All")

				var selectedJob string
				prompt := &survey.Select{
					Message: "Select job to stop:",
					Options: jobOptions,
				}
				survey.AskOne(prompt, &selectedJob)

				if selectedJob == "All" {
					req, _ := http.NewRequest("POST", fmt.Sprintf("%s/api/v1/jobs/stop-all", BackendURL), nil)
					req.Header.Set("Authorization", "Bearer "+token)
					resp, err := httpClient.Do(req)
					if err != nil {
						fmt.Printf("Error: Failed to connect to backend: %v\n", err)
						return
					}
					defer resp.Body.Close()
					if resp.StatusCode != http.StatusOK {
						fmt.Printf("Error: Failed to stop all jobs: %s\n", resp.Status)
						return
					}
					fmt.Println("All jobs stopped successfully.")
					return
				}

				// Extract job ID from selection
				parts := strings.Split(selectedJob, " (")
				if len(parts) == 2 {
					jobID = strings.TrimSuffix(parts[1], ")")
				}
			}

			if err := stopJob(token, jobID); err != nil {
				fmt.Printf("Error: Failed to stop job: %v\n", err)
				return
			}

			fmt.Printf("Job %s stopped successfully.\n", jobID)
		},
	}

	pauseJobCmd := &cobra.Command{
		Use:   "pause [job-id]",
		Short: "Pause a replication job",
		Long:  "Pause a running replication job (can be resumed later)",
		Args:  cobra.MaximumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			token, err := LoadToken()
			if err != nil {
				fmt.Println("Error: You must be logged in to perform this action. Please run 'mirror-cli login'.")
				return
			}

			user, err := fetchMe(token)
			if err != nil {
				fmt.Printf("Error: Failed to fetch user profile: %v\n", err)
				return
			}

			if user["role"] != "admin" && user["role"] != "operator" {
				fmt.Println("Error: You must be an admin or operator to perform this action.")
				return
			}

			var jobID string
			if len(args) > 0 {
				jobID = args[0]
			} else {
				jobs, err := fetchJobs(token)
				if err != nil {
					fmt.Printf("Error: Failed to fetch jobs: %v\n", err)
					return
				}

				if len(jobs) == 0 {
					fmt.Println("No jobs found.")
					return
				}

				var jobOptions []string
				for _, job := range jobs {
					if job["status"].(string) == "active" {
						jobOptions = append(jobOptions, fmt.Sprintf("%s (%s)", job["name"].(string), job["id"].(string)))
					}
				}

				if len(jobOptions) == 0 {
					fmt.Println("No running jobs found.")
					return
				}

				var selectedJob string
				prompt := &survey.Select{
					Message: "Select job to pause:",
					Options: jobOptions,
				}
				survey.AskOne(prompt, &selectedJob)

				// Extract job ID from selection
				parts := strings.Split(selectedJob, " (")
				if len(parts) == 2 {
					jobID = strings.TrimSuffix(parts[1], ")")
				}
			}

			if err := pauseJob(token, jobID); err != nil {
				fmt.Printf("Error: Failed to pause job: %v\n", err)
				return
			}

			fmt.Printf("Job %s paused successfully.\n", jobID)
		},
	}

	restartJobCmd := &cobra.Command{
		Use:   "restart [job-id]",
		Short: "Restart a replication job",
		Long:  "Restart a replication job (stop and start it)",
		Args:  cobra.MaximumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			token, err := LoadToken()
			if err != nil {
				fmt.Println("Error: You must be logged in to perform this action. Please run 'mirror-cli login'.")
				return
			}

			user, err := fetchMe(token)
			if err != nil {
				fmt.Printf("Error: Failed to fetch user profile: %v\n", err)
				return
			}

			if user["role"] != "admin" && user["role"] != "operator" {
				fmt.Println("Error: You must be an admin or operator to perform this action.")
				return
			}

			var jobID string
			if len(args) > 0 {
				jobID = args[0]
			} else {
				jobs, err := fetchJobs(token)
				if err != nil {
					fmt.Printf("Error: Failed to fetch jobs: %v\n", err)
					return
				}

				if len(jobs) == 0 {
					fmt.Println("No jobs found.")
					return
				}

				var jobOptions []string
				for _, job := range jobs {
					jobOptions = append(jobOptions, fmt.Sprintf("%s (%s)", job["name"].(string), job["id"].(string)))
				}
				jobOptions = append(jobOptions, "All")

				var selectedJob string
				prompt := &survey.Select{
					Message: "Select job to restart:",
					Options: jobOptions,
				}
				survey.AskOne(prompt, &selectedJob)

				if selectedJob == "All" {
					req, _ := http.NewRequest("POST", fmt.Sprintf("%s/api/v1/jobs/restart-all", BackendURL), nil)
					req.Header.Set("Authorization", "Bearer "+token)
					resp, err := httpClient.Do(req)
					if err != nil {
						fmt.Printf("Error: Failed to connect to backend: %v\n", err)
						return
					}
					defer resp.Body.Close()
					if resp.StatusCode != http.StatusOK {
						body, _ := ioutil.ReadAll(resp.Body)
						fmt.Printf("Error: Failed to restart all jobs: %s - %s\n", resp.Status, string(body))
						return
					}
					fmt.Println("All jobs restarted successfully.")
					return
				}

				// Extract job ID from selection
				parts := strings.Split(selectedJob, " (")
				if len(parts) == 2 {
					jobID = strings.TrimSuffix(parts[1], ")")
				}
			}

			if err := restartJob(token, jobID); err != nil {
				fmt.Printf("Error: Failed to restart job: %v\n", err)
				return
			}

			fmt.Printf("Job %s restarted successfully.\n", jobID)
		},
	}

	deleteJobCmd := &cobra.Command{
		Use:   "delete [job-id]",
		Short: "Delete a replication job",
		Long:  "Permanently delete a replication job (admin only)",
		Args:  cobra.MaximumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			token, err := LoadToken()
			if err != nil {
				fmt.Println("Error: You must be logged in to perform this action. Please run 'mirror-cli login'.")
				return
			}

			user, err := fetchMe(token)
			if err != nil {
				fmt.Printf("Error: Failed to fetch user profile: %v\n", err)
				return
			}

			if user["role"] != "admin" {
				fmt.Println("Error: You must be an admin to perform this action.")
				return
			}

			var jobID string
			if len(args) > 0 {
				jobID = args[0]
			} else {
				jobs, err := fetchJobs(token)
				if err != nil {
					fmt.Printf("Error: Failed to fetch jobs: %v\n", err)
					return
				}

				if len(jobs) == 0 {
					fmt.Println("No jobs found.")
					return
				}

				var jobOptions []string
				for _, job := range jobs {
					jobOptions = append(jobOptions, fmt.Sprintf("%s (%s)", job["name"].(string), job["id"].(string)))
				}

				var selectedJob string
				prompt := &survey.Select{
					Message: "Select job to delete:",
					Options: jobOptions,
				}
				survey.AskOne(prompt, &selectedJob)

				// Extract job ID from selection
				parts := strings.Split(selectedJob, " (")
				if len(parts) == 2 {
					jobID = strings.TrimSuffix(parts[1], ")")
				}
			}

			var confirm bool
			confirmPrompt := &survey.Confirm{
				Message: fmt.Sprintf("Are you sure you want to delete job '%s'? This action cannot be undone.", jobID),
				Default: false,
			}
			survey.AskOne(confirmPrompt, &confirm)
			if !confirm {
				fmt.Println("Job deletion cancelled.")
				return
			}

			req, _ := http.NewRequest("DELETE", fmt.Sprintf("%s/api/v1/jobs/%s", BackendURL, jobID), nil)
			req.Header.Set("Authorization", "Bearer "+token)

			resp, err := httpClient.Do(req)
			if err != nil {
				fmt.Printf("Error: Failed to connect to backend: %v\n", err)
				return
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusNoContent {
				body, _ := ioutil.ReadAll(resp.Body)
				fmt.Printf("Error: Failed to delete job: %s - %s\n", resp.Status, string(body))
				return
			}

			fmt.Printf("Job %s deleted successfully.\n", jobID)
		},
	}

	statusJobCmd := &cobra.Command{
		Use:   "status",
		Short: "Show detailed job status and metrics",
		Long:  "Display detailed status and performance metrics for a replication job",
	}

	statusMetricsCmd := &cobra.Command{
		Use:   "metrics [job-id]",
		Short: "Show detailed job status and metrics",
		Long:  "Display detailed status and performance metrics for a replication job",
		Args:  cobra.MaximumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			token, err := LoadToken()
			if err != nil {
				fmt.Println("Error: You must be logged in to perform this action. Please run 'mirror-cli login'.")
				return
			}

			var jobID string
			if len(args) > 0 {
				jobID = args[0]
			} else {
				jobs, err := fetchJobs(token)
				if err != nil {
					fmt.Printf("Error: Failed to fetch jobs: %v\n", err)
					return
				}

				if len(jobs) == 0 {
					fmt.Println("No jobs found.")
					return
				}

				var jobOptions []string
				for _, job := range jobs {
					jobOptions = append(jobOptions, fmt.Sprintf("%s (%s)", job["name"].(string), job["id"].(string)))
				}

				var selectedJob string
				prompt := &survey.Select{
					Message: "Select job to view status:",
					Options: jobOptions,
				}
				survey.AskOne(prompt, &selectedJob)

				// Extract job ID from selection
				parts := strings.Split(selectedJob, " (")
				if len(parts) == 2 {
					jobID = strings.TrimSuffix(parts[1], ")")
				}
			}

			req, _ := http.NewRequest("GET", fmt.Sprintf("%s/api/v1/jobs/%s", BackendURL, jobID), nil)
			req.Header.Set("Authorization", "Bearer "+token)

			client := &http.Client{}
			resp, err := client.Do(req)
			if err != nil {
				fmt.Printf("Error: Failed to connect to backend: %v\n", err)
				return
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				body, _ := ioutil.ReadAll(resp.Body)
				fmt.Printf("Error: Failed to get job status: %s - %s\n", resp.Status, string(body))
				return
			}

			var job map[string]interface{}
			if err := json.NewDecoder(resp.Body).Decode(&job); err != nil {
				fmt.Printf("Error: Failed to parse job response: %v\n", err)
				return
			}

			fmt.Printf("=== Job Status: %s ===\n", job["name"].(string))
			fmt.Printf("ID: %s\n", job["id"].(string))
			fmt.Printf("Status: %s\n", job["status"].(string))

			sourceCluster := "N/A"
			if job["source_cluster_name"] != nil {
				sourceCluster = job["source_cluster_name"].(string)
			}

			targetCluster := "N/A"
			if job["target_cluster_name"] != nil {
				targetCluster = job["target_cluster_name"].(string)
			}

			fmt.Printf("Source Cluster: %s\n", sourceCluster)
			fmt.Printf("Target Cluster: %s\n", targetCluster)
			fmt.Printf("Created: %s\n", job["created_at"].(string))

			metrics, err := fetchMetrics(token, jobID)
			if err != nil {
				fmt.Printf("Metrics: Unable to fetch - %v\n", err)
			} else {
				fmt.Println("\n=== Current Metrics ===")
				if lag, ok := metrics["current_lag"]; ok {
					fmt.Printf("Current Lag: %.0f messages\n", lag)
				}
				if throughput, ok := metrics["messages_replicated"]; ok {
					fmt.Printf("Messages/sec: %.0f\n", throughput)
				}
				if processed, ok := metrics["total_messages_processed"]; ok {
					fmt.Printf("Total Processed: %.0f\n", processed)
				}
			}
		},
	}

	statusFullCmd := &cobra.Command{
		Use:   "full [job-id]",
		Short: "Show full mirror state for a job",
		Long:  "Display the full mirror state for a replication job, including progress, gaps, and resume points.",
		Args:  cobra.MaximumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			token, err := LoadToken()
			if err != nil {
				fmt.Println("Error: You must be logged in to perform this action. Please run 'mirror-cli login'.")
				return
			}

			var jobID string
			if len(args) > 0 {
				jobID = args[0]
			} else {
				jobs, err := fetchJobs(token)
				if err != nil {
					fmt.Printf("Error: Failed to fetch jobs: %v\n", err)
					return
				}

				if len(jobs) == 0 {
					fmt.Println("No jobs found.")
					return
				}

				var jobOptions []string
				for _, job := range jobs {
					jobOptions = append(jobOptions, fmt.Sprintf("%s (%s)", job["name"].(string), job["id"].(string)))
				}

				var selectedJob string
				prompt := &survey.Select{
					Message: "Select job to view status:",
					Options: jobOptions,
				}
				survey.AskOne(prompt, &selectedJob)

				// Extract job ID from selection
				parts := strings.Split(selectedJob, " (")
				if len(parts) == 2 {
					jobID = strings.TrimSuffix(parts[1], ")")
				}
			}

			req, _ := http.NewRequest("GET", fmt.Sprintf("%s/api/v1/jobs/%s/mirror/state", BackendURL, jobID), nil)
			req.Header.Set("Authorization", "Bearer "+token)

			client := &http.Client{}
			resp, err := client.Do(req)
			if err != nil {
				fmt.Printf("Error: Failed to connect to backend: %v\n", err)
				return
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				body, _ := ioutil.ReadAll(resp.Body)
				fmt.Printf("Error: Failed to get job status: %s - %s\n", resp.Status, string(body))
				return
			}

			var state map[string]interface{}
			if err := json.NewDecoder(resp.Body).Decode(&state); err != nil {
				fmt.Printf("Error: Failed to parse job response: %v\n", err)
				return
			}

			prettyJSON, err := json.MarshalIndent(state, "", "  ")
			if err != nil {
				fmt.Printf("Error: Failed to format JSON: %v\n", err)
				return
			}
			fmt.Println(string(prettyJSON))
		},
	}

	statusJobCmd.AddCommand(statusMetricsCmd, statusFullCmd)

	analyzeJobCmd := &cobra.Command{
		Use:   "analyze [job-id]",
		Short: "Trigger historical AI analysis for a job",
		Long:  "Trigger a long-term historical AI analysis for a replication job",
		Args:  cobra.MaximumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			token, err := LoadToken()
			if err != nil {
				fmt.Println("Error: You must be logged in to perform this action. Please run 'mirror-cli login'.")
				return
			}

			var jobID string
			if len(args) > 0 {
				jobID = args[0]
			} else {
				jobs, err := fetchJobs(token)
				if err != nil {
					fmt.Printf("Error: Failed to fetch jobs: %v\n", err)
					return
				}

				if len(jobs) == 0 {
					fmt.Println("No jobs found.")
					return
				}

				var jobOptions []string
				for _, job := range jobs {
					jobOptions = append(jobOptions, fmt.Sprintf("%s (%s)", job["name"].(string), job["id"].(string)))
				}

				var selectedJob string
				prompt := &survey.Select{
					Message: "Select job to analyze:",
					Options: jobOptions,
				}
				survey.AskOne(prompt, &selectedJob)

				// Extract job ID from selection
				parts := strings.Split(selectedJob, " (")
				if len(parts) == 2 {
					jobID = strings.TrimSuffix(parts[1], ")")
				}
			}

			period, _ := cmd.Flags().GetString("period")

			req, _ := http.NewRequest("POST", fmt.Sprintf("%s/api/v1/jobs/%s/ai/historical-analysis?period=%s", BackendURL, jobID, period), nil)
			req.Header.Set("Authorization", "Bearer "+token)

			client := &http.Client{}
			resp, err := client.Do(req)
			if err != nil {
				fmt.Printf("Error: Failed to connect to backend: %v\n", err)
				return
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusAccepted {
				body, _ := ioutil.ReadAll(resp.Body)
				fmt.Printf("Error: Failed to trigger analysis: %s - %s\n", resp.Status, string(body))
				return
			}

			fmt.Printf("Historical AI analysis triggered for job %s.\n", jobID)
		},
	}
	analyzeJobCmd.Flags().String("period", "7d", "Analysis period (e.g., 7d, 30d)")

	healthcheckJobCmd := &cobra.Command{
		Use:   "healthcheck [job-id]",
		Short: "Perform health checks on clusters associated with a job",
		Long:  "Performs health checks on the source and target clusters for a specific job or all jobs.",
		Args:  cobra.MaximumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			token, err := LoadToken()
			if err != nil {
				fmt.Println("Error: You must be logged in to perform this action. Please run 'mirror-cli login'.")
				return
			}

			jobs, err := fetchJobs(token)
			if err != nil {
				fmt.Printf("Error: Failed to fetch jobs: %v\n", err)
				return
			}

			if len(jobs) == 0 {
				fmt.Println("No jobs found.")
				return
			}

			var jobID string
			if len(args) > 0 {
				jobID = args[0]
			} else {
				jobMap := make(map[string]string)
				var jobOptions []string
				for _, job := range jobs {
					displayName := fmt.Sprintf("%s (%s)", job["name"].(string), job["id"].(string))
					jobOptions = append(jobOptions, displayName)
					jobMap[displayName] = job["id"].(string)
				}
				jobOptions = append(jobOptions, "All")

				var selectedJob string
				prompt := &survey.Select{
					Message: "Select job to perform health check on:",
					Options: jobOptions,
				}
				survey.AskOne(prompt, &selectedJob)

				if selectedJob == "All" {
					jobID = "All"
				} else {
					jobID = jobMap[selectedJob]
				}
			}

			if jobID == "All" {
				fmt.Println("Performing health checks for all jobs...")
				allJobsHealthy := true
				for _, job := range jobs {
					jobID := job["id"].(string)
					jobName := job["name"].(string)
					fmt.Printf("\n--- Health Check for Job: %s (%s) ---\n", jobName, jobID)

					jobDetails, err := fetchJob(token, jobID)
					if err != nil {
						fmt.Printf("Error fetching details for job %s: %v\n", jobName, err)
						allJobsHealthy = false
						continue
					}

					sourceCluster := jobDetails["source_cluster_name"].(string)
					targetCluster := jobDetails["target_cluster_name"].(string)

					fmt.Printf("Checking source cluster '%s'...\n", sourceCluster)
					if err := testClusterConnectionVerbose(token, sourceCluster); err != nil {
						fmt.Printf("  Error: Source cluster '%s' is unreachable: %v\n", sourceCluster, err)
						allJobsHealthy = false
					} else {
						fmt.Printf("  ✓ Source cluster '%s' is healthy.\n", sourceCluster)
					}

					fmt.Printf("Checking target cluster '%s'...\n", targetCluster)
					if err := testClusterConnectionVerbose(token, targetCluster); err != nil {
						fmt.Printf("  Error: Target cluster '%s' is unreachable: %v\n", targetCluster, err)
						allJobsHealthy = false
					} else {
						fmt.Printf("  ✓ Target cluster '%s' is healthy.\n", targetCluster)
					}
				}
				if allJobsHealthy {
					fmt.Println("\nAll jobs passed health checks.")
				} else {
					fmt.Println("\nSome jobs failed health checks.")
				}
			} else {
				jobDetails, err := fetchJob(token, jobID)
				if err != nil {
					fmt.Printf("Error fetching details for job %s: %v\n", jobID, err)
					return
				}
				jobName := jobDetails["name"].(string)
				fmt.Printf("--- Health Check for Job: %s (%s) ---\n", jobName, jobID)

				sourceCluster := jobDetails["source_cluster_name"].(string)
				targetCluster := jobDetails["target_cluster_name"].(string)

				fmt.Printf("Checking source cluster '%s'...\n", sourceCluster)
				if err := testClusterConnectionVerbose(token, sourceCluster); err != nil {
					fmt.Printf("Error: Source cluster '%s' is unreachable: %v\n", sourceCluster, err)
				} else {
					fmt.Printf("✓ Source cluster '%s' is healthy.\n", sourceCluster)
				}

				fmt.Printf("Checking target cluster '%s'...\n", targetCluster)
				if err := testClusterConnectionVerbose(token, targetCluster); err != nil {
					fmt.Printf("Error: Target cluster '%s' is unreachable: %v\n", targetCluster, err)
				} else {
					fmt.Printf("✓ Target cluster '%s' is healthy.\n", targetCluster)
				}
			}
		},
	}

	forceRestartJobCmd := &cobra.Command{
		Use:   "force-restart [job-id]",
		Short: "Forcefully restart a replication job",
		Long:  "Forcefully restarts a job, performing pre-flight checks to ensure a clean start.",
		Args:  cobra.MaximumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			token, err := LoadToken()
			if err != nil {
				fmt.Println("Error: You must be logged in to perform this action. Please run 'mirror-cli login'.")
				return
			}

			var jobID string
			if len(args) > 0 {
				jobID = args[0]
			} else {
				jobs, err := fetchJobs(token)
				if err != nil {
					fmt.Printf("Error: Failed to fetch jobs: %v\n", err)
					return
				}
				if len(jobs) == 0 {
					fmt.Println("No jobs found to restart.")
					return
				}
				var jobOptions []string
				jobMap := make(map[string]string)
				for _, job := range jobs {
					displayName := fmt.Sprintf("%s (%s)", job["name"].(string), job["id"].(string))
					jobOptions = append(jobOptions, displayName)
					jobMap[displayName] = job["id"].(string)
				}

				var selectedJob string
				prompt := &survey.Select{
					Message: "Select job to force-restart:",
					Options: jobOptions,
				}
				survey.AskOne(prompt, &selectedJob)
				jobID = jobMap[selectedJob]
			}

			req, _ := http.NewRequest("POST", fmt.Sprintf("%s/api/v1/jobs/%s/force-restart", BackendURL, jobID), nil)
			req.Header.Set("Authorization", "Bearer "+token)

			client := &http.Client{}
			resp, err := client.Do(req)
			if err != nil {
				fmt.Printf("Error: Failed to connect to backend: %v\n", err)
				return
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				body, _ := ioutil.ReadAll(resp.Body)
				fmt.Printf("Error: Failed to force-restart job: %s\n%s\n", resp.Status, body)
				return
			}

			fmt.Printf("Job %s forcefully restarted successfully.\n", jobID)
		},
	}

	jobsCmd.AddCommand(listJobsCmd, addJobCmd, startJobCmd, stopJobCmd, pauseJobCmd, restartJobCmd, forceRestartJobCmd, deleteJobCmd, statusJobCmd, analyzeJobCmd, healthcheckJobCmd)
	return jobsCmd
}

func testAIConnection(endpoint, apiKey, model, provider string) bool {
	fmt.Printf("Sending test prompt to %s...\n", provider)

	testPrompt := "Respond with 'Connection successful' if you can process this message."

	var reqBody []byte
	var err error

	if provider == "custom" || provider == "openai" {
		reqBody, err = json.Marshal(map[string]interface{}{
			"model": model,
			"messages": []map[string]string{
				{
					"role":    "user",
					"content": testPrompt,
				},
			},
			"max_tokens": 50,
		})
	} else {
		reqBody, err = json.Marshal(map[string]interface{}{
			"model": model,
			"messages": []map[string]string{
				{
					"role":    "user",
					"content": testPrompt,
				},
			},
			"max_tokens": 50,
		})
	}

	if err != nil {
		fmt.Printf("Error creating request: %v\n", err)
		return false
	}

	chatEndpoint := endpoint + "/chat/completions"
	req, err := http.NewRequest("POST", chatEndpoint, bytes.NewBuffer(reqBody))
	if err != nil {
		fmt.Printf("Error creating HTTP request: %v\n", err)
		return false
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("Connection failed: %v\n", err)
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := ioutil.ReadAll(resp.Body)
		fmt.Printf("API returned status %d: %s\n", resp.StatusCode, string(body))
		return false
	}

	fmt.Println("Connection test successful!")
	return true
}

func testCustomAIConnection(endpoint, apiKey, apiSecret string) ([]string, error) {
	modelsURL := fmt.Sprintf("%s/models", endpoint)

	req, err := http.NewRequest("GET", modelsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %v", err)
	}

	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	if apiSecret != "" {
		req.Header.Set("X-API-Secret", apiSecret)
		req.Header.Set("X-API-Key", apiKey)
		req.SetBasicAuth(apiKey, apiSecret)
	}

	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("connection failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := ioutil.ReadAll(resp.Body)
		return nil, fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(body))
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %v", err)
	}

	var modelsResponse struct {
		Data []struct {
			ID     string `json:"id"`
			Object string `json:"object"`
		} `json:"data"`
	}

	if err := json.Unmarshal(body, &modelsResponse); err == nil && len(modelsResponse.Data) > 0 {
		var models []string
		for _, model := range modelsResponse.Data {
			if model.Object == "model" || model.Object == "" {
				models = append(models, model.ID)
			}
		}
		return models, nil
	}

	var modelList []string
	if err := json.Unmarshal(body, &modelList); err == nil && len(modelList) > 0 {
		return modelList, nil
	}

	var modelsWithName []struct {
		Name  string `json:"name"`
		Model string `json:"model"`
	}
	if err := json.Unmarshal(body, &modelsWithName); err == nil && len(modelsWithName) > 0 {
		var models []string
		for _, model := range modelsWithName {
			if model.Name != "" {
				models = append(models, model.Name)
			} else if model.Model != "" {
				models = append(models, model.Model)
			}
		}
		return models, nil
	}

	return []string{}, nil
}

func manageAIConfiguration() {
	token, err := LoadToken()
	if err != nil {
		fmt.Println("Error: You must be logged in to perform this action. Please run 'mirror-cli login'.")
		return
	}

	user, err := fetchMe(token)
	if err != nil {
		fmt.Printf("Error: Failed to fetch user profile: %v\n", err)
		return
	}

	if user["role"] != "admin" {
		fmt.Println("Error: You must be an admin to perform this action.")
		return
	}

	currentConfig, err := fetchCurrentConfig(token)
	if err != nil {
		fmt.Printf("Error: Failed to fetch current configuration: %v\n", err)
		return
	}

	aiConfig, exists := currentConfig["ai"].(map[string]interface{})
	if !exists {
		aiConfig = make(map[string]interface{})
	}

	fmt.Println("=== Current AI Configuration ===")
	fmt.Printf("Provider: %v\n", getConfigValue(aiConfig, "provider", "not set"))
	fmt.Printf("Endpoint: %v\n", getConfigValue(aiConfig, "endpoint", "not set"))
	fmt.Printf("Model: %v\n", getConfigValue(aiConfig, "model", "not set"))
	fmt.Printf("Token: %s\n", maskSensitiveValue(getConfigValue(aiConfig, "token", "not set")))
	fmt.Println("")

	var action string
	promptAction := &survey.Select{
		Message: "Select action:",
		Options: []string{
			"Change provider",
			"Update endpoint",
			"Set API token",
			"Change model",
			"View full configuration",
			"Exit",
		},
	}
	survey.AskOne(promptAction, &action)

	switch action {
	case "Change provider":
		var provider string
		prompt := &survey.Select{
			Message: "Select AI provider:",
			Options: []string{"openai", "claude", "gemini", "grok", "custom"},
			Default: getConfigValueAsString(aiConfig, "provider"),
		}
		survey.AskOne(prompt, &provider)
		updateConfigParameter("ai.provider", provider)
		configureAIProvider(provider)

	case "Update endpoint":
		var endpoint string
		prompt := &survey.Input{
			Message: "Enter AI API endpoint:",
			Default: getConfigValueAsString(aiConfig, "endpoint"),
		}
		survey.AskOne(prompt, &endpoint)
		updateConfigParameter("ai.endpoint", endpoint)

	case "Set API token":
		var token string
		prompt := &survey.Password{
			Message: "Enter AI API token:",
		}
		survey.AskOne(prompt, &token)
		updateConfigParameter("ai.token", token)

	case "Change model":
		var model string
		prompt := &survey.Input{
			Message: "Enter AI model:",
			Default: getConfigValueAsString(aiConfig, "model"),
		}
		survey.AskOne(prompt, &model)
		updateConfigParameter("ai.model", model)

	case "View full configuration":
		fmt.Println("=== Full AI Configuration ===")
		configJSON, _ := json.MarshalIndent(aiConfig, "", "  ")
		fmt.Println(string(configJSON))

	case "Exit":
		return
	}
}

func manageServerConfiguration() {
	token, err := LoadToken()
	if err != nil {
		fmt.Println("Error: You must be logged in to perform this action. Please run 'mirror-cli login'.")
		return
	}

	user, err := fetchMe(token)
	if err != nil {
		fmt.Printf("Error: Failed to fetch user profile: %v\n", err)
		return
	}

	if user["role"] != "admin" {
		fmt.Println("Error: You must be an admin to perform this action.")
		return
	}

	currentConfig, err := fetchCurrentConfig(token)
	if err != nil {
		fmt.Printf("Error: Failed to fetch current configuration: %v\n", err)
		return
	}

	serverConfig, exists := currentConfig["server"].(map[string]interface{})
	if !exists {
		serverConfig = make(map[string]interface{})
	}

	tlsConfig, _ := serverConfig["tls"].(map[string]interface{})
	if tlsConfig == nil {
		tlsConfig = make(map[string]interface{})
	}

	fmt.Println("=== Current Server Configuration ===")
	fmt.Printf("Host: %v\n", getConfigValue(serverConfig, "host", "localhost"))
	fmt.Printf("Port: %v\n", getConfigValue(serverConfig, "port", "8080"))
	fmt.Printf("Mode: %v\n", getConfigValue(serverConfig, "mode", "production"))
	fmt.Printf("TLS Enabled: %v\n", getConfigValue(tlsConfig, "enabled", "false"))
	if getConfigValue(tlsConfig, "enabled", false) == true {
		fmt.Printf("TLS Cert File: %v\n", getConfigValue(tlsConfig, "cert_file", "not set"))
		fmt.Printf("TLS Key File: %v\n", getConfigValue(tlsConfig, "key_file", "not set"))
	}
	fmt.Println("")

	var action string
	promptAction := &survey.Select{
		Message: "Select action:",
		Options: []string{
			"Change host",
			"Change port",
			"Change mode",
			"Configure TLS",
			"Configure CORS",
			"View full configuration",
			"Exit",
		},
	}
	survey.AskOne(promptAction, &action)

	switch action {
	case "Change host":
		var host string
		prompt := &survey.Input{
			Message: "Enter server host:",
			Default: getConfigValueAsString(serverConfig, "host"),
		}
		survey.AskOne(prompt, &host)
		updateConfigParameter("server.host", host)

	case "Change port":
		var port string
		prompt := &survey.Input{
			Message: "Enter server port:",
			Default: fmt.Sprintf("%v", getConfigValue(serverConfig, "port", 8080)),
		}
		survey.AskOne(prompt, &port)
		updateConfigParameter("server.port", port)

	case "Change mode":
		var mode string
		prompt := &survey.Select{
			Message: "Select server mode:",
			Options: []string{"development", "production"},
			Default: getConfigValueAsString(serverConfig, "mode"),
		}
		survey.AskOne(prompt, &mode)
		updateConfigParameter("server.mode", mode)

	case "Configure TLS":
		var enableTLS bool
		prompt := &survey.Confirm{
			Message: "Enable TLS/HTTPS?",
			Default: getConfigValue(tlsConfig, "enabled", false) == true,
		}
		survey.AskOne(prompt, &enableTLS)
		updateConfigParameter("server.tls.enabled", fmt.Sprintf("%t", enableTLS))

		if enableTLS {
			var certFile string
			promptCert := &survey.Input{
				Message: "Enter TLS certificate file path:",
				Default: getConfigValueAsString(tlsConfig, "cert_file"),
			}
			survey.AskOne(promptCert, &certFile)
			updateConfigParameter("server.tls.cert_file", certFile)

			var keyFile string
			promptKey := &survey.Input{
				Message: "Enter TLS key file path:",
				Default: getConfigValueAsString(tlsConfig, "key_file"),
			}
			survey.AskOne(promptKey, &keyFile)
			updateConfigParameter("server.tls.key_file", keyFile)
		}

	case "Configure CORS":
		corsConfig, _ := serverConfig["cors"].(map[string]interface{})
		if corsConfig == nil {
			corsConfig = make(map[string]interface{})
		}

		var origins string
		origins = strings.Join(getConfigValueAsStringArray(corsConfig, "allowed_origins"), ",")

		prompt := &survey.Input{
			Message: "Enter allowed CORS origins (comma-separated):",
			Default: origins,
		}
		survey.AskOne(prompt, &origins)
		updateConfigParameter("server.cors.allowed_origins", origins)

	case "View full configuration":
		fmt.Println("=== Full Server Configuration ===")
		configJSON, _ := json.MarshalIndent(serverConfig, "", "  ")
		fmt.Println(string(configJSON))

	case "Exit":
		return
	}
}

// manageMonitoringConfiguration provides interactive monitoring configuration management
func manageMonitoringConfiguration() {
	token, err := LoadToken()
	if err != nil {
		fmt.Println("Error: You must be logged in to perform this action. Please run 'mirror-cli login'.")
		return
	}

	user, err := fetchMe(token)
	if err != nil {
		fmt.Printf("Error: Failed to fetch user profile: %v\n", err)
		return
	}

	if user["role"] != "admin" {
		fmt.Println("Error: You must be an admin to perform this action.")
		return
	}

	currentConfig, err := fetchCurrentConfig(token)
	if err != nil {
		fmt.Printf("Error: Failed to fetch current configuration: %v\n", err)
		return
	}

	monitoringConfig, exists := currentConfig["monitoring"].(map[string]interface{})
	if !exists {
		monitoringConfig = make(map[string]interface{})
	}

	fmt.Println("=== Current Monitoring Configuration ===")
	fmt.Printf("Enabled: %v\n", getConfigValue(monitoringConfig, "enabled", "false"))
	fmt.Printf("Platform: %v\n", getConfigValue(monitoringConfig, "platform", "not set"))

	if getConfigValue(monitoringConfig, "enabled", false) == true {
		platform := getConfigValueAsString(monitoringConfig, "platform")
		switch platform {
		case "splunk":
			splunkConfig, _ := monitoringConfig["splunk"].(map[string]interface{})
			if splunkConfig != nil {
				fmt.Printf("Splunk HEC Endpoint: %v\n", getConfigValue(splunkConfig, "hec_endpoint", "not set"))
				fmt.Printf("Splunk Index: %v\n", getConfigValue(splunkConfig, "index", "not set"))
				fmt.Printf("Splunk HEC Token: %s\n", maskSensitiveValue(getConfigValue(splunkConfig, "hec_token", "not set")))
			}
		case "loki":
			lokiConfig, _ := monitoringConfig["loki"].(map[string]interface{})
			if lokiConfig != nil {
				fmt.Printf("Loki Endpoint: %v\n", getConfigValue(lokiConfig, "endpoint", "not set"))
			}
		case "prometheus":
			prometheusConfig, _ := monitoringConfig["prometheus"].(map[string]interface{})
			if prometheusConfig != nil {
				fmt.Printf("Prometheus Push Gateway: %v\n", getConfigValue(prometheusConfig, "push_gateway", "not set"))
			}
		}
	}
	fmt.Println("")

	var action string
	promptAction := &survey.Select{
		Message: "Select action:",
		Options: []string{
			"Enable/disable monitoring",
			"Change platform",
			"Configure platform settings",
			"View full configuration",
			"Exit",
		},
	}
	survey.AskOne(promptAction, &action)

	switch action {
	case "Enable/disable monitoring":
		var enabled bool
		prompt := &survey.Confirm{
			Message: "Enable monitoring?",
			Default: getConfigValue(monitoringConfig, "enabled", false) == true,
		}
		survey.AskOne(prompt, &enabled)
		updateConfigParameter("monitoring.enabled", fmt.Sprintf("%t", enabled))

	case "Change platform":
		var platform string
		prompt := &survey.Select{
			Message: "Select monitoring platform:",
			Options: []string{"splunk", "loki", "prometheus"},
			Default: getConfigValueAsString(monitoringConfig, "platform"),
		}
		survey.AskOne(prompt, &platform)
		updateConfigParameter("monitoring.platform", platform)

	case "Configure platform settings":
		platform := getConfigValueAsString(monitoringConfig, "platform")
		if platform == "" {
			fmt.Println("Please set a monitoring platform first.")
			return
		}

		switch platform {
		case "splunk":
			splunkConfig, _ := monitoringConfig["splunk"].(map[string]interface{})
			if splunkConfig == nil {
				splunkConfig = make(map[string]interface{})
			}

			var endpoint string
			promptEndpoint := &survey.Input{
				Message: "Enter Splunk HEC endpoint:",
				Default: getConfigValueAsString(splunkConfig, "hec_endpoint"),
			}
			survey.AskOne(promptEndpoint, &endpoint)
			updateConfigParameter("monitoring.splunk.hec_endpoint", endpoint)

			var token string
			promptToken := &survey.Password{
				Message: "Enter Splunk HEC token:",
			}
			survey.AskOne(promptToken, &token)
			updateConfigParameter("monitoring.splunk.hec_token", token)

			var index string
			promptIndex := &survey.Input{
				Message: "Enter Splunk index:",
				Default: getConfigValueAsString(splunkConfig, "index"),
			}
			survey.AskOne(promptIndex, &index)
			updateConfigParameter("monitoring.splunk.index", index)

		case "loki":
			lokiConfig, _ := monitoringConfig["loki"].(map[string]interface{})
			if lokiConfig == nil {
				lokiConfig = make(map[string]interface{})
			}

			var endpoint string
			promptEndpoint := &survey.Input{
				Message: "Enter Loki endpoint:",
				Default: getConfigValueAsString(lokiConfig, "endpoint"),
			}
			survey.AskOne(promptEndpoint, &endpoint)
			updateConfigParameter("monitoring.loki.endpoint", endpoint)

		case "prometheus":
			prometheusConfig, _ := monitoringConfig["prometheus"].(map[string]interface{})
			if prometheusConfig == nil {
				prometheusConfig = make(map[string]interface{})
			}

			var pushGateway string
			promptGateway := &survey.Input{
				Message: "Enter Prometheus Push Gateway URL:",
				Default: getConfigValueAsString(prometheusConfig, "push_gateway"),
			}
			survey.AskOne(promptGateway, &pushGateway)
			updateConfigParameter("monitoring.prometheus.push_gateway", pushGateway)
		}

	case "View full configuration":
		fmt.Println("=== Full Monitoring Configuration ===")
		configJSON, _ := json.MarshalIndent(monitoringConfig, "", "  ")
		fmt.Println(string(configJSON))

	case "Exit":
		return
	}
}

func fetchCurrentConfig(token string) (map[string]interface{}, error) {
	req, _ := http.NewRequest("GET", fmt.Sprintf("%s/api/v1/config", BackendURL), nil)
	req.Header.Set("Authorization", "Bearer "+token)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch config: %s", resp.Status)
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var config map[string]interface{}
	if err := json.Unmarshal(body, &config); err != nil {
		return nil, err
	}

	return config, nil
}

func getConfigValue(config map[string]interface{}, key string, defaultValue interface{}) interface{} {
	if val, exists := config[key]; exists {
		return val
	}
	return defaultValue
}

func getConfigValueAsString(config map[string]interface{}, key string) string {
	val := getConfigValue(config, key, "")
	return fmt.Sprintf("%v", val)
}

func getConfigValueAsStringArray(config map[string]interface{}, key string) []string {
	val, exists := config[key]
	if !exists {
		return []string{}
	}

	if arr, ok := val.([]interface{}); ok {
		result := make([]string, len(arr))
		for i, v := range arr {
			result[i] = fmt.Sprintf("%v", v)
		}
		return result
	}

	return []string{fmt.Sprintf("%v", val)}
}

func maskSensitiveValue(value interface{}) string {
	val := fmt.Sprintf("%v", value)
	if val == "" || val == "not set" {
		return val
	}
	if len(val) <= 4 {
		return "****"
	}
	return val[:4] + "****"
}

func configureAIProvider(provider string) {
	switch provider {
	case "openai":
		configureOpenAI()
	case "claude":
		configureClaude()
	case "gemini":
		configureGemini()
	case "grok":
		configureGrok()
	case "custom":
		configureCustomAI()
	default:
		fmt.Printf("Unknown AI provider: %s\n", provider)
	}
}

func configureOpenAI() {
	fmt.Println("Configuring OpenAI settings...")

	var apiKey string
	promptKey := &survey.Password{
		Message: "Enter OpenAI API key:",
	}
	survey.AskOne(promptKey, &apiKey)
	updateConfigParameter("ai.token", apiKey)
	fmt.Printf("Successfully updated ai.token = %s\n", maskSensitiveValue(apiKey))

	var model string
	promptModel := &survey.Select{
		Message: "Select OpenAI model:",
		Options: []string{"gpt-4", "gpt-4-turbo", "gpt-3.5-turbo"},
		Default: "gpt-4",
	}
	survey.AskOne(promptModel, &model)
	updateConfigParameter("ai.model", model)

	updateConfigParameter("ai.endpoint", "https://api.openai.com/v1")

	fmt.Println("OpenAI configuration completed")
}

func configureClaude() {
	fmt.Println("Configuring Anthropic Claude settings...")

	var apiKey string
	promptKey := &survey.Password{
		Message: "Enter Anthropic API key:",
	}
	survey.AskOne(promptKey, &apiKey)
	updateConfigParameter("ai.token", apiKey)

	var model string
	promptModel := &survey.Select{
		Message: "Select Claude model:",
		Options: []string{"claude-3-opus-20240229", "claude-3-sonnet-20240229", "claude-3-haiku-20240307"},
		Default: "claude-3-sonnet-20240229",
	}
	survey.AskOne(promptModel, &model)
	updateConfigParameter("ai.model", model)

	updateConfigParameter("ai.endpoint", "https://api.anthropic.com/v1")

	fmt.Println("Claude configuration completed")
}

func configureGemini() {
	fmt.Println("Configuring Google Gemini settings...")

	var apiKey string
	promptKey := &survey.Password{
		Message: "Enter Google AI API key:",
	}
	survey.AskOne(promptKey, &apiKey)
	updateConfigParameter("ai.token", apiKey)

	var model string
	promptModel := &survey.Select{
		Message: "Select Gemini model:",
		Options: []string{"gemini-pro", "gemini-pro-vision"},
		Default: "gemini-pro",
	}
	survey.AskOne(promptModel, &model)
	updateConfigParameter("ai.model", model)

	updateConfigParameter("ai.endpoint", "https://generativelanguage.googleapis.com/v1")

	fmt.Println("Gemini configuration completed")
}

func configureGrok() {
	fmt.Println("Configuring xAI Grok settings...")

	var apiKey string
	promptKey := &survey.Password{
		Message: "Enter xAI API key:",
	}
	survey.AskOne(promptKey, &apiKey)
	updateConfigParameter("ai.token", apiKey)

	var model string
	promptModel := &survey.Select{
		Message: "Select Grok model:",
		Options: []string{"grok-beta", "grok-vision-beta"},
		Default: "grok-beta",
	}
	survey.AskOne(promptModel, &model)
	updateConfigParameter("ai.model", model)

	updateConfigParameter("ai.endpoint", "https://api.x.ai/v1")

	fmt.Println("Grok configuration completed")
}

func configureCustomAI() {
	fmt.Println("Configuring custom AI provider settings...")

	var endpoint string
	promptEndpoint := &survey.Input{
		Message: "Enter custom AI API base URL (without /chat/completions):",
		Default: "https://connect.scalytics.io/v1",
		Help:    "Examples: https://api.yourprovider.com/v1, https://connect.scalytics.io/v1",
	}
	survey.AskOne(promptEndpoint, &endpoint)

	endpoint = strings.TrimSuffix(endpoint, "/")
	updateConfigParameter("ai.endpoint", endpoint)

	var apiKey string
	promptKey := &survey.Password{
		Message: "Enter API key:",
	}
	survey.AskOne(promptKey, &apiKey)
	updateConfigParameter("ai.token", apiKey)

	var needsSecret bool
	promptSecret := &survey.Confirm{
		Message: "Does this provider require an API secret (in addition to API key)?",
		Default: false,
	}
	survey.AskOne(promptSecret, &needsSecret)

	var apiSecret string
	if needsSecret {
		promptSecretValue := &survey.Password{
			Message: "Enter API secret:",
		}
		survey.AskOne(promptSecretValue, &apiSecret)
		updateConfigParameter("ai.api_secret", apiSecret)
	}

	fmt.Println("Testing connection and discovering available models...")
	models, err := testCustomAIConnection(endpoint, apiKey, apiSecret)
	if err != nil {
		fmt.Printf("Warning: Connection test failed: %v\n", err)
		fmt.Println("You can still configure manually, but please verify the settings.")

		var model string
		promptModel := &survey.Input{
			Message: "Enter model name manually:",
			Default: "gpt-3.5-turbo",
		}
		survey.AskOne(promptModel, &model)
		updateConfigParameter("ai.model", model)
	} else {
		fmt.Printf("Connection successful! Found %d available models.\n", len(models))

		if len(models) > 0 {
			var selectedModel string
			if len(models) == 1 {
				selectedModel = models[0]
				fmt.Printf("Using model: %s\n", selectedModel)
			} else {
				promptModel := &survey.Select{
					Message: "Select model:",
					Options: models,
					Default: models[0],
				}
				survey.AskOne(promptModel, &selectedModel)
			}
			updateConfigParameter("ai.model", selectedModel)
		} else {
			var model string
			promptModel := &survey.Input{
				Message: "No models discovered. Enter model name manually:",
				Default: "gpt-3.5-turbo",
			}
			survey.AskOne(promptModel, &model)
			updateConfigParameter("ai.model", model)
		}
	}

	fmt.Println("Custom AI provider configuration completed")
}

func createConfigCommand() *cobra.Command {
	configCmd := &cobra.Command{
		Use:   "config",
		Short: "Manage application configuration",
		Long:  "Configure and manage kaf-mirror settings",
	}

	getCmd := &cobra.Command{
		Use:   "get",
		Short: "Display current configuration",
		Long:  "Show the current configuration from the server",
		Run: func(cmd *cobra.Command, args []string) {
			token, err := LoadToken()
			if err != nil {
				fmt.Println("Error: You must be logged in to perform this action. Please run 'mirror-cli login'.")
				return
			}

			req, _ := http.NewRequest("GET", fmt.Sprintf("%s/api/v1/config", BackendURL), nil)
			req.Header.Set("Authorization", "Bearer "+token)

			client := &http.Client{}
			resp, err := client.Do(req)
			if err != nil {
				fmt.Printf("Error: Failed to connect to backend: %v\n", err)
				return
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				body, _ := ioutil.ReadAll(resp.Body)
				fmt.Printf("Error: Failed to get config: %s - %s\n", resp.Status, string(body))
				return
			}

			body, err := ioutil.ReadAll(resp.Body)
			if err != nil {
				fmt.Printf("Error: Failed to read response body: %v\n", err)
				return
			}

			fmt.Println(string(body))
		},
	}

	systemCmd := &cobra.Command{
		Use:   "system",
		Short: "Initial system configuration wizard",
		Long:  "Run the initial configuration wizard to set up kaf-mirror",
		Run: func(cmd *cobra.Command, args []string) {
			token, err := LoadToken()
			if err != nil {
				fmt.Println("Error: You must be logged in to perform this action. Please run 'mirror-cli login'.")
				return
			}

			user, err := fetchMe(token)
			if err != nil {
				fmt.Printf("Error: Failed to fetch user profile: %v\n", err)
				return
			}

			if user["role"] != "admin" {
				fmt.Println("Error: You must be an admin to perform this action.")
				return
			}

			req, _ := http.NewRequest("GET", fmt.Sprintf("%s/api/v1/config", BackendURL), nil)
			req.Header.Set("Authorization", "Bearer "+token)
			client := &http.Client{}
			resp, err := client.Do(req)

			if err == nil && resp.StatusCode == http.StatusOK {
				fmt.Println("⚠️  WARNING: Existing configuration found in database!")
				fmt.Println("This wizard will OVERWRITE the current configuration.")

				var proceed bool
				prompt := &survey.Confirm{
					Message: "Do you want to proceed and overwrite existing configuration?",
					Default: false,
				}
				survey.AskOne(prompt, &proceed)

				if !proceed {
					fmt.Println("Configuration wizard cancelled.")
					return
				}
			}
			if resp != nil {
				resp.Body.Close()
			}

			fmt.Println("🚀 Starting kaf-mirror system configuration wizard...")
			fmt.Println("This will guide you through the initial setup process.")
			fmt.Println()

			runSystem(cmd, args)
		},
	}

	editCmd := createConfigEditCommand()

	saveCmd := &cobra.Command{
		Use:   "save [file]",
		Short: "Save configuration from YAML file to server",
		Long:  "Load and apply configuration from a YAML file, replacing server configuration",
		Args:  cobra.MaximumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			token, err := LoadToken()
			if err != nil {
				fmt.Println("Error: You must be logged in to perform this action. Please run 'mirror-cli login'.")
				return
			}

			user, err := fetchMe(token)
			if err != nil {
				fmt.Printf("Error: Failed to fetch user profile: %v\n", err)
				return
			}

			if user["role"] != "admin" {
				fmt.Println("Error: You must be an admin to perform this action.")
				return
			}

			fmt.Println("Verifying server connectivity and current configuration...")
			currentConfig, err := fetchCurrentConfig(token)
			if err != nil {
				fmt.Printf("Error: Failed to fetch current running configuration: %v\n", err)
				fmt.Println("Make sure the server is running and accessible.")
				return
			}
			fmt.Println("✓ Successfully connected to server and verified current configuration")

			var configBytes []byte
			var configFile string
			if len(args) > 0 {
				configFile = args[0]
			} else {
				configFile = "configs/default.yml"
			}

			configBytes, err = ioutil.ReadFile(configFile)
			if err != nil {
				fmt.Printf("Error: Failed to read config file: %v\n", err)
				return
			}

			var tempConfig configData
			if err := yaml.Unmarshal(configBytes, &tempConfig); err != nil {
				fmt.Printf("Error: Invalid YAML configuration: %v\n", err)
				return
			}

			fmt.Println("Creating backup of current running configuration...")
			currentConfigYAML, err := json.Marshal(currentConfig)
			if err != nil {
				fmt.Printf("Error: Failed to marshal current config: %v\n", err)
				return
			}

			var currentConfigMap map[string]interface{}
			json.Unmarshal(currentConfigYAML, &currentConfigMap)
			backupYAML, err := yaml.Marshal(currentConfigMap)
			if err != nil {
				fmt.Printf("Error: Failed to convert current config to YAML: %v\n", err)
				return
			}

			timestamp := time.Now().Format("2006-01-02_15-04-05")
			backupFile := fmt.Sprintf("%s-backup-%s.yml", strings.TrimSuffix(configFile, ".yml"), timestamp)

			if err := ioutil.WriteFile(backupFile, backupYAML, 0644); err != nil {
				fmt.Printf("Error: Failed to create backup file: %v\n", err)
				return
			}
			fmt.Printf("✓ Current configuration backed up to: %s\n", backupFile)

			fmt.Println("=================================================================")
			fmt.Println("  WARNING: SERVER CONFIGURATION REPLACEMENT")
			fmt.Println("=================================================================")
			fmt.Println("This operation will:")
			fmt.Println("  - Replace the server's YAML configuration file")
			fmt.Println("  - Overwrite ALL current server settings")
			fmt.Println("  - Require server restart to take effect")
			fmt.Printf("  - Current config backed up to: %s\n", backupFile)
			fmt.Println("")
			fmt.Println("IMPORTANT: The kaf-mirror server must be restarted after this")
			fmt.Println("operation to apply the new configuration.")
			fmt.Println("=================================================================")

			var proceed bool
			prompt := &survey.Confirm{
				Message: "Do you want to proceed with replacing the server configuration?",
				Default: false,
			}
			survey.AskOne(prompt, &proceed)

			if !proceed {
				fmt.Println("Configuration replacement cancelled.")
				fmt.Printf("Current configuration backup retained at: %s\n", backupFile)
				return
			}

			fmt.Printf("Applying configuration from: %s\n", configFile)

			var yamlConfig map[string]interface{}
			if err := yaml.Unmarshal(configBytes, &yamlConfig); err != nil {
				fmt.Printf("Error: Failed to parse YAML config: %v\n", err)
				return
			}

			jsonBytes, err := json.Marshal(yamlConfig)
			if err != nil {
				fmt.Printf("Error: Failed to convert config to JSON: %v\n", err)
				return
			}

			req, _ := http.NewRequest("PUT", fmt.Sprintf("%s/api/v1/config", BackendURL), bytes.NewBuffer(jsonBytes))
			req.Header.Set("Authorization", "Bearer "+token)
			req.Header.Set("Content-Type", "application/json")

			client := &http.Client{}
			resp, err := client.Do(req)
			if err != nil {
				fmt.Printf("Error: Failed to connect to backend: %v\n", err)
				return
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				body, _ := ioutil.ReadAll(resp.Body)
				fmt.Printf("Error: Failed to save config: %s - %s\n", resp.Status, string(body))
				fmt.Printf("Current configuration backup available at: %s\n", backupFile)
				return
			}

			fmt.Println("Configuration saved successfully.")
			fmt.Printf("Previous configuration backed up to: %s\n", backupFile)
			fmt.Println("")
			fmt.Println("=================================================================")
			fmt.Println("  NEXT STEPS REQUIRED")
			fmt.Println("=================================================================")
			fmt.Println("1. Restart the kaf-mirror server to apply the new configuration:")
			fmt.Println("   - Using systemd: sudo systemctl restart kaf-mirror")
			fmt.Println("   - Using Makefile: make restart")
			fmt.Println("   - Manual: Stop current server and restart")
			fmt.Println("")
			fmt.Println("2. Verify server startup with new configuration")
			fmt.Println("3. Check server logs for any configuration errors")
			fmt.Printf("4. If rollback needed, use: config save %s\n", backupFile)
			fmt.Println("=================================================================")
		},
	}

	configCmd.AddCommand(getCmd, systemCmd, editCmd, saveCmd)
	return configCmd
}

func createConfigEditCommand() *cobra.Command {
	editCmd := &cobra.Command{
		Use:   "edit",
		Short: "Edit configuration parameters",
		Long:  "Hierarchical configuration editing with validation",
	}

	serverCmd := createServerEditCommands()

	replicationCmd := createReplicationEditCommands()

	monitoringCmd := createMonitoringEditCommands()

	complianceCmd := createComplianceEditCommands()

	aiCmd := createAIEditCommands()

	editCmd.AddCommand(serverCmd, replicationCmd, monitoringCmd, complianceCmd, aiCmd)
	return editCmd
}

func createServerEditCommands() *cobra.Command {
	serverCmd := &cobra.Command{
		Use:   "server",
		Short: "Configure server settings",
		Long:  "Configure server host, port, mode, TLS, and CORS settings",
	}

	hostCmd := &cobra.Command{
		Use:   "host [address]",
		Short: "Set server host address",
		Args:  cobra.MaximumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			var host string
			if len(args) > 0 {
				host = args[0]
			} else {
				prompt := &survey.Input{
					Message: "Enter server host address:",
					Default: "localhost",
				}
				survey.AskOne(prompt, &host)
			}
			updateConfigParameter("server.host", host)
		},
	}

	// config edit server port [port]
	portCmd := &cobra.Command{
		Use:   "port [port]",
		Short: "Set server port",
		Args:  cobra.MaximumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			var port string
			if len(args) > 0 {
				port = args[0]
			} else {
				prompt := &survey.Input{
					Message: "Enter server port:",
					Default: "8080",
				}
				survey.AskOne(prompt, &port)
			}
			updateConfigParameter("server.port", port)
		},
	}

	// config edit server mode [mode]
	modeCmd := &cobra.Command{
		Use:       "mode [mode]",
		Short:     "Set server mode",
		Args:      cobra.MaximumNArgs(1),
		ValidArgs: []string{"development", "production"},
		Run: func(cmd *cobra.Command, args []string) {
			var mode string
			if len(args) > 0 {
				mode = args[0]
			} else {
				prompt := &survey.Select{
					Message: "Select server mode:",
					Options: []string{"development", "production"},
					Default: "production",
				}
				survey.AskOne(prompt, &mode)
			}
			updateConfigParameter("server.mode", mode)
		},
	}

	// config edit server tls [enabled]
	tlsCmd := &cobra.Command{
		Use:   "tls [enabled]",
		Short: "Configure server TLS/HTTPS settings",
		Args:  cobra.MaximumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			var enabled string
			if len(args) > 0 {
				enabled = args[0]
			} else {
				var enableBool bool
				prompt := &survey.Confirm{
					Message: "Enable HTTPS/TLS for the server?",
					Default: false,
				}
				survey.AskOne(prompt, &enableBool)
				enabled = fmt.Sprintf("%t", enableBool)

				if enableBool {
					var certFile string
					promptCert := &survey.Input{
						Message: "Enter path to TLS certificate file:",
						Default: "/etc/kaf-mirror/server.crt",
					}
					survey.AskOne(promptCert, &certFile)
					updateConfigParameter("server.tls.cert_file", certFile)

					var keyFile string
					promptKey := &survey.Input{
						Message: "Enter path to TLS private key file:",
						Default: "/etc/kaf-mirror/server.key",
					}
					survey.AskOne(promptKey, &keyFile)
					updateConfigParameter("server.tls.key_file", keyFile)
				}
			}
			updateConfigParameter("server.tls.enabled", enabled)
		},
	}

	// config edit server cors [origins]
	corsCmd := &cobra.Command{
		Use:   "cors [origins]",
		Short: "Configure CORS allowed origins",
		Args:  cobra.MaximumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			var origins string
			if len(args) > 0 {
				origins = args[0]
			} else {
				prompt := &survey.Input{
					Message: "Enter allowed CORS origins (comma-separated):",
					Default: "http://localhost:3000,https://localhost:3000",
				}
				survey.AskOne(prompt, &origins)
			}
			updateConfigParameter("server.cors.allowed_origins", origins)
		},
	}

	// config edit server manage
	manageCmd := &cobra.Command{
		Use:   "manage",
		Short: "Manage server configuration interactively",
		Long:  "View current server configuration and modify settings interactively",
		Run: func(cmd *cobra.Command, args []string) {
			manageServerConfiguration()
		},
	}

	serverCmd.AddCommand(hostCmd, portCmd, modeCmd, tlsCmd, corsCmd, manageCmd)
	return serverCmd
}

// createReplicationEditCommands creates replication configuration commands
func createReplicationEditCommands() *cobra.Command {
	replicationCmd := &cobra.Command{
		Use:   "replication",
		Short: "Configure replication settings",
		Long:  "Configure batch size, parallelism, compression, and topic discovery interval",
	}

	// config edit replication batch-size [size]
	batchSizeCmd := &cobra.Command{
		Use:   "batch-size [size]",
		Short: "Set replication batch size (KB)",
		Args:  cobra.MaximumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			var size string
			if len(args) > 0 {
				size = args[0]
			} else {
				prompt := &survey.Input{
					Message: "Enter batch size (messages):",
					Default: "1000",
				}
				survey.AskOne(prompt, &size)
			}
			updateConfigParameter("replication.batch_size", size)
		},
	}

	// config edit replication parallelism [count]
	parallelismCmd := &cobra.Command{
		Use:   "parallelism [count]",
		Short: "Set replication parallelism",
		Args:  cobra.MaximumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			var count string
			if len(args) > 0 {
				count = args[0]
			} else {
				prompt := &survey.Input{
					Message: "Enter parallelism count:",
					Default: "4",
				}
				survey.AskOne(prompt, &count)
			}
			updateConfigParameter("replication.parallelism", count)
		},
	}

	// config edit replication compression [type]
	compressionCmd := &cobra.Command{
		Use:       "compression [type]",
		Short:     "Set compression algorithm",
		Args:      cobra.MaximumNArgs(1),
		ValidArgs: []string{"none", "gzip", "snappy", "lz4", "zstd"},
		Run: func(cmd *cobra.Command, args []string) {
			var compression string
			if len(args) > 0 {
				compression = args[0]
			} else {
				prompt := &survey.Select{
					Message: "Select compression algorithm:",
					Options: []string{"none", "gzip", "snappy", "lz4", "zstd"},
					Default: "none",
				}
				survey.AskOne(prompt, &compression)
			}
			updateConfigParameter("replication.compression", compression)
		},
	}

	// config edit replication discovery-interval [duration]
	discoveryIntervalCmd := &cobra.Command{
		Use:   "discovery-interval [duration]",
		Short: "Set topic discovery interval",
		Args:  cobra.MaximumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			var interval string
			if len(args) > 0 {
				interval = args[0]
			} else {
				prompt := &survey.Input{
					Message: "Enter topic discovery interval (e.g., 5m, 30s, 0 to disable):",
					Default: "5m",
				}
				survey.AskOne(prompt, &interval)
			}
			updateConfigParameter("replication.topic_discovery_interval", interval)
		},
	}

	replicationCmd.AddCommand(batchSizeCmd, parallelismCmd, compressionCmd, discoveryIntervalCmd)
	return replicationCmd
}

// createMonitoringEditCommands creates monitoring configuration commands
func createMonitoringEditCommands() *cobra.Command {
	monitoringCmd := &cobra.Command{
		Use:   "monitoring",
		Short: "Configure monitoring settings",
		Long:  "Configure monitoring platform and endpoints",
	}

	// config edit monitoring enabled [value]
	enabledCmd := &cobra.Command{
		Use:       "enabled [value]",
		Short:     "Enable or disable monitoring",
		Args:      cobra.MaximumNArgs(1),
		ValidArgs: []string{"true", "false"},
		Run: func(cmd *cobra.Command, args []string) {
			var enabled string
			if len(args) > 0 {
				enabled = args[0]
			} else {
				var enableBool bool
				prompt := &survey.Confirm{
					Message: "Enable monitoring?",
					Default: false,
				}
				survey.AskOne(prompt, &enableBool)
				if enableBool {
					enabled = "true"
				} else {
					enabled = "false"
				}
			}
			updateConfigParameter("monitoring.enabled", enabled)
		},
	}

	// config edit monitoring platform [platform]
	platformCmd := &cobra.Command{
		Use:       "platform [platform]",
		Short:     "Set monitoring platform",
		Args:      cobra.MaximumNArgs(1),
		ValidArgs: []string{"splunk", "loki", "prometheus"},
		Run: func(cmd *cobra.Command, args []string) {
			var platform string
			if len(args) > 0 {
				platform = args[0]
			} else {
				prompt := &survey.Select{
					Message: "Select monitoring platform:",
					Options: []string{"splunk", "loki", "prometheus"},
					Default: "prometheus",
				}
				survey.AskOne(prompt, &platform)
			}
			updateConfigParameter("monitoring.platform", platform)
		},
	}

	// config edit monitoring manage
	manageCmd := &cobra.Command{
		Use:   "manage",
		Short: "Manage monitoring configuration interactively",
		Long:  "View current monitoring configuration and modify settings interactively",
		Run: func(cmd *cobra.Command, args []string) {
			manageMonitoringConfiguration()
		},
	}

	monitoringCmd.AddCommand(enabledCmd, platformCmd, manageCmd)
	return monitoringCmd
}

// createComplianceEditCommands creates compliance scheduling configuration commands
func createComplianceEditCommands() *cobra.Command {
	complianceCmd := &cobra.Command{
		Use:   "compliance",
		Short: "Configure compliance reporting schedule",
		Long:  "Configure automated compliance report scheduling",
	}

	enabledCmd := &cobra.Command{
		Use:   "enabled [value]",
		Short: "Enable or disable compliance scheduling",
		Args:  cobra.MaximumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			var enabled string
			if len(args) > 0 {
				enabled = args[0]
			} else {
				var enabledBool bool
				prompt := &survey.Confirm{
					Message: "Enable compliance report scheduling?",
					Default: true,
				}
				survey.AskOne(prompt, &enabledBool)
				enabled = fmt.Sprintf("%t", enabledBool)
			}
			updateConfigParameter("compliance.schedule.enabled", enabled)
		},
	}

	runHourCmd := &cobra.Command{
		Use:   "run-hour [hour]",
		Short: "Set report run hour (0-23 local time)",
		Args:  cobra.MaximumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			var hour string
			if len(args) > 0 {
				hour = args[0]
			} else {
				prompt := &survey.Input{
					Message: "Enter report run hour (0-23):",
					Default: "2",
				}
				survey.AskOne(prompt, &hour)
			}
			updateConfigParameter("compliance.schedule.run_hour", hour)
		},
	}

	dailyCmd := &cobra.Command{
		Use:   "daily [value]",
		Short: "Enable or disable daily compliance reports",
		Args:  cobra.MaximumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			var enabled string
			if len(args) > 0 {
				enabled = args[0]
			} else {
				var enabledBool bool
				prompt := &survey.Confirm{
					Message: "Generate daily compliance reports?",
					Default: true,
				}
				survey.AskOne(prompt, &enabledBool)
				enabled = fmt.Sprintf("%t", enabledBool)
			}
			updateConfigParameter("compliance.schedule.daily", enabled)
		},
	}

	weeklyCmd := &cobra.Command{
		Use:   "weekly [value]",
		Short: "Enable or disable weekly compliance reports (Monday)",
		Args:  cobra.MaximumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			var enabled string
			if len(args) > 0 {
				enabled = args[0]
			} else {
				var enabledBool bool
				prompt := &survey.Confirm{
					Message: "Generate weekly compliance reports? (Monday)",
					Default: false,
				}
				survey.AskOne(prompt, &enabledBool)
				enabled = fmt.Sprintf("%t", enabledBool)
			}
			updateConfigParameter("compliance.schedule.weekly", enabled)
		},
	}

	monthlyCmd := &cobra.Command{
		Use:   "monthly [value]",
		Short: "Enable or disable monthly compliance reports (1st of month)",
		Args:  cobra.MaximumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			var enabled string
			if len(args) > 0 {
				enabled = args[0]
			} else {
				var enabledBool bool
				prompt := &survey.Confirm{
					Message: "Generate monthly compliance reports? (1st of month)",
					Default: false,
				}
				survey.AskOne(prompt, &enabledBool)
				enabled = fmt.Sprintf("%t", enabledBool)
			}
			updateConfigParameter("compliance.schedule.monthly", enabled)
		},
	}

	manageCmd := &cobra.Command{
		Use:   "manage",
		Short: "Manage compliance scheduling interactively",
		Run: func(cmd *cobra.Command, args []string) {
			token, err := LoadToken()
			if err != nil {
				fmt.Println("Error: You must be logged in to perform this action. Please run 'mirror-cli login'.")
				return
			}

			cfg, err := fetchCurrentConfig(token)
			if err != nil {
				fmt.Printf("Error: Failed to fetch config: %v\n", err)
				return
			}

			complianceCfg, ok := cfg["compliance"].(map[string]interface{})
			if !ok {
				complianceCfg = map[string]interface{}{}
			}
			scheduleCfg, ok := complianceCfg["schedule"].(map[string]interface{})
			if !ok {
				scheduleCfg = map[string]interface{}{}
			}

			fmt.Println("Current compliance schedule:")
			fmt.Printf("  Enabled: %v\n", getConfigValue(scheduleCfg, "enabled", "false"))
			fmt.Printf("  Run Hour: %v\n", getConfigValue(scheduleCfg, "run_hour", "2"))
			fmt.Printf("  Daily: %v\n", getConfigValue(scheduleCfg, "daily", "true"))
			fmt.Printf("  Weekly: %v\n", getConfigValue(scheduleCfg, "weekly", "false"))
			fmt.Printf("  Monthly: %v\n", getConfigValue(scheduleCfg, "monthly", "false"))

			fmt.Println("\nUse subcommands to update specific values.")
		},
	}

	complianceCmd.AddCommand(enabledCmd, runHourCmd, dailyCmd, weeklyCmd, monthlyCmd, manageCmd)
	return complianceCmd
}

// createAIEditCommands creates AI configuration commands
func createAIEditCommands() *cobra.Command {
	aiCmd := &cobra.Command{
		Use:   "ai",
		Short: "Configure AI settings",
		Long:  "Configure AI provider, model, and features",
	}

	// config edit ai provider [provider]
	providerCmd := &cobra.Command{
		Use:       "provider [provider]",
		Short:     "Set AI provider and configure endpoints",
		Args:      cobra.MaximumNArgs(1),
		ValidArgs: []string{"openai", "claude", "gemini", "grok", "custom"},
		Run: func(cmd *cobra.Command, args []string) {
			var provider string
			if len(args) > 0 {
				provider = args[0]
			} else {
				prompt := &survey.Select{
					Message: "Select AI provider:",
					Options: []string{"openai", "claude", "gemini", "grok", "custom"},
					Default: "openai",
				}
				survey.AskOne(prompt, &provider)
			}

			// Update provider first
			updateConfigParameter("ai.provider", provider)

			// Configure provider-specific settings
			configureAIProvider(provider)
		},
	}

	// config edit ai model [model]
	modelCmd := &cobra.Command{
		Use:   "model [model]",
		Short: "Set AI model",
		Args:  cobra.MaximumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			var model string
			if len(args) > 0 {
				model = args[0]
			} else {
				prompt := &survey.Input{
					Message: "Enter AI model:",
					Default: "gpt-4",
				}
				survey.AskOne(prompt, &model)
			}
			updateConfigParameter("ai.model", model)
		},
	}

	// config edit ai features [feature] [enabled]
	featuresCmd := &cobra.Command{
		Use:   "features",
		Short: "Enable/disable AI features",
		Long:  "Configure which AI features are enabled: anomaly_detection, performance_optimization, incident_analysis",
		Run: func(cmd *cobra.Command, args []string) {
			token, err := LoadToken()
			if err != nil {
				fmt.Println("Error: You must be logged in to perform this action. Please run 'mirror-cli login'.")
				return
			}

			user, err := fetchMe(token)
			if err != nil {
				fmt.Printf("Error: Failed to fetch user profile: %v\n", err)
				return
			}

			if user["role"] != "admin" {
				fmt.Println("Error: You must be an admin to perform this action.")
				return
			}

			// Fetch current AI configuration
			currentConfig, err := fetchCurrentConfig(token)
			if err != nil {
				fmt.Printf("Error: Failed to fetch current configuration: %v\n", err)
				return
			}

			aiConfig, exists := currentConfig["AI"].(map[string]interface{})
			if !exists {
				aiConfig = make(map[string]interface{})
			}

			features, _ := aiConfig["Features"].(map[string]interface{})
			if features == nil {
				features = make(map[string]interface{})
			}

			selfHealing, _ := aiConfig["SelfHealing"].(map[string]interface{})
			if selfHealing == nil {
				selfHealing = make(map[string]interface{})
			}

			fmt.Println("=== Current AI Features ===")
			fmt.Printf("Anomaly Detection: %v\n", getConfigValue(features, "AnomalyDetection", false))
			fmt.Printf("Performance Optimization: %v\n", getConfigValue(features, "PerformanceOptimization", false))
			fmt.Printf("Incident Analysis: %v\n", getConfigValue(features, "IncidentAnalysis", false))
			fmt.Println("")
			fmt.Println("=== Self-Healing Features ===")
			fmt.Printf("Automated Remediation: %v\n", getConfigValue(selfHealing, "AutomatedRemediation", false))
			fmt.Printf("Dynamic Throttling: %v\n", getConfigValue(selfHealing, "DynamicThrottling", false))
			fmt.Println("")

			var enableAll bool
			prompt := &survey.Confirm{
				Message: "Enable all AI features?",
				Default: false,
			}
			survey.AskOne(prompt, &enableAll)

			if enableAll {
				updateConfigParameter("AI.Features.AnomalyDetection", "true")
				updateConfigParameter("AI.Features.PerformanceOptimization", "true")
				updateConfigParameter("AI.Features.IncidentAnalysis", "true")
				fmt.Println("All AI features enabled successfully!")
			} else {
				var anomalyDetection bool
				promptAnomalies := &survey.Confirm{
					Message: "Enable anomaly detection?",
					Default: getConfigValue(features, "AnomalyDetection", false) == true,
				}
				survey.AskOne(promptAnomalies, &anomalyDetection)
				updateConfigParameter("AI.Features.AnomalyDetection", fmt.Sprintf("%t", anomalyDetection))

				var performanceOpt bool
				promptPerf := &survey.Confirm{
					Message: "Enable performance optimization recommendations?",
					Default: getConfigValue(features, "PerformanceOptimization", false) == true,
				}
				survey.AskOne(promptPerf, &performanceOpt)
				updateConfigParameter("AI.Features.PerformanceOptimization", fmt.Sprintf("%t", performanceOpt))

				var incidentAnalysis bool
				promptIncident := &survey.Confirm{
					Message: "Enable incident analysis?",
					Default: getConfigValue(features, "IncidentAnalysis", false) == true,
				}
				survey.AskOne(promptIncident, &incidentAnalysis)
				updateConfigParameter("AI.Features.IncidentAnalysis", fmt.Sprintf("%t", incidentAnalysis))

				fmt.Println("AI features configuration updated!")
			}
		},
	}

	// config edit ai test-and-enable - Test AI connection and auto-enable features
	testEnableCmd := &cobra.Command{
		Use:   "test-and-enable",
		Short: "Test AI connection and automatically enable all features",
		Long:  "Test the configured AI provider and automatically enable all AI features if connection is successful",
		Run: func(cmd *cobra.Command, args []string) {
			token, err := LoadToken()
			if err != nil {
				fmt.Println("Error: You must be logged in to perform this action. Please run 'mirror-cli login'.")
				return
			}

			user, err := fetchMe(token)
			if err != nil {
				fmt.Printf("Error: Failed to fetch user profile: %v\n", err)
				return
			}

			if user["role"] != "admin" {
				fmt.Println("Error: You must be an admin to perform this action.")
				return
			}

			// Fetch current AI configuration
			currentConfig, err := fetchCurrentConfig(token)
			if err != nil {
				fmt.Printf("Error: Failed to fetch current configuration: %v\n", err)
				return
			}

			aiConfig, exists := currentConfig["AI"].(map[string]interface{})
			if !exists {
				fmt.Println("Error: AI configuration not found. Please configure AI provider first.")
				return
			}

			provider := getConfigValueAsString(aiConfig, "Provider")
			endpoint := getConfigValueAsString(aiConfig, "Endpoint")
			token_val := getConfigValueAsString(aiConfig, "Token")
			model := getConfigValueAsString(aiConfig, "Model")

			if provider == "" || endpoint == "" || token_val == "" || model == "" {
				fmt.Println("Error: AI configuration incomplete. Please configure provider, endpoint, token, and model first.")
				fmt.Printf("Current values - Provider: %s, Endpoint: %s, Model: %s, Token: %s\n",
					provider, endpoint, model, maskSensitiveValue(token_val))
				return
			}

			fmt.Printf("Testing AI connection to %s with model %s...\n", provider, model)

			// Test AI connection with a simple prompt
			success := testAIConnection(endpoint, token_val, model, provider)

			if success {
				fmt.Println("AI connection successful! Auto-enabling all AI features...")

				updateConfigParameter("AI.Features.AnomalyDetection", "true")
				updateConfigParameter("AI.Features.PerformanceOptimization", "true")
				updateConfigParameter("AI.Features.IncidentAnalysis", "true")

				fmt.Println("All AI features have been enabled successfully!")
				fmt.Println("")
				fmt.Println("=================================================================")
				fmt.Println("  NEXT STEPS REQUIRED")
				fmt.Println("=================================================================")
				fmt.Println("The AI configuration has been updated, but the server must be")
				fmt.Println("restarted to activate the new AI features:")
				fmt.Println("")
				fmt.Println("1. Restart the kaf-mirror server:")
				fmt.Println("   - Using systemd: sudo systemctl restart kaf-mirror")
				fmt.Println("   - Using Makefile: make restart")
				fmt.Println("   - Manual: Stop current server and restart")
				fmt.Println("")
				fmt.Println("2. After restart, AI will provide:")
				fmt.Println("   - Anomaly Detection: Identify unusual patterns in replication metrics")
				fmt.Println("   - Performance Optimization: Recommendations for tuning replication settings")
				fmt.Println("   - Incident Analysis: Detailed analysis of operational events")
				fmt.Println("")
				fmt.Println("3. AI insights will appear in the dashboard within a few minutes")
				fmt.Println("   after the server restart is complete.")
				fmt.Println("=================================================================")
			} else {
				fmt.Println("AI connection failed. Please check your AI configuration and try again.")
				fmt.Println("Use 'config edit ai manage' to review and update your AI settings.")
			}
		},
	}

	// config edit ai manage - Interactive AI configuration management
	manageCmd := &cobra.Command{
		Use:   "manage",
		Short: "Manage AI configuration interactively",
		Long:  "View current AI configuration and modify settings interactively",
		Run: func(cmd *cobra.Command, args []string) {
			manageAIConfiguration()
		},
	}

	selfHealingCmd := &cobra.Command{
		Use:   "self-healing",
		Short: "Configure AI self-healing features",
		Long:  "Enable or disable automated remediation and dynamic throttling.",
		Run: func(cmd *cobra.Command, args []string) {
			token, err := LoadToken()
			if err != nil {
				fmt.Println("Error: You must be logged in to perform this action.")
				return
			}
			currentConfig, err := fetchCurrentConfig(token)
			if err != nil {
				fmt.Printf("Error: Failed to fetch current configuration: %v\n", err)
				return
			}

			aiConfig, _ := currentConfig["AI"].(map[string]interface{})
			selfHealingConfig, _ := aiConfig["SelfHealing"].(map[string]interface{})

			var autoRemediation bool
			promptAutoRemediation := &survey.Confirm{
				Message: "Enable automated remediation (e.g., auto force-restart on failure)?",
				Default: getConfigValue(selfHealingConfig, "AutomatedRemediation", false).(bool),
			}
			survey.AskOne(promptAutoRemediation, &autoRemediation)
			updateConfigParameter("AI.SelfHealing.AutomatedRemediation", fmt.Sprintf("%t", autoRemediation))

			var dynamicThrottling bool
			promptDynamicThrottling := &survey.Confirm{
				Message: "Enable dynamic throttling (e.g., auto-adjust batch size)?",
				Default: getConfigValue(selfHealingConfig, "DynamicThrottling", false).(bool),
			}
			survey.AskOne(promptDynamicThrottling, &dynamicThrottling)
			updateConfigParameter("AI.SelfHealing.DynamicThrottling", fmt.Sprintf("%t", dynamicThrottling))
		},
	}

	aiCmd.AddCommand(providerCmd, modelCmd, featuresCmd, selfHealingCmd, testEnableCmd, manageCmd)
	return aiCmd
}

// updateConfigParameter updates a specific configuration parameter
func updateConfigParameter(path, value string) {
	token, err := LoadToken()
	if err != nil {
		fmt.Println("Error: You must be logged in to perform this action. Please run 'mirror-cli login'.")
		return
	}

	user, err := fetchMe(token)
	if err != nil {
		fmt.Printf("Error: Failed to fetch user profile: %v\n", err)
		return
	}

	if user["role"] != "admin" {
		fmt.Println("Error: You must be an admin to perform this action.")
		return
	}

	// Get current config
	req, _ := http.NewRequest("GET", fmt.Sprintf("%s/api/v1/config", BackendURL), nil)
	req.Header.Set("Authorization", "Bearer "+token)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("Error: Failed to connect to backend: %v\n", err)
		return
	}
	defer resp.Body.Close()

	var currentConfig map[string]interface{}
	if resp.StatusCode == http.StatusOK {
		body, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			fmt.Printf("Error: Failed to read current config: %v\n", err)
			return
		}

		if err := json.Unmarshal(body, &currentConfig); err != nil {
			fmt.Printf("Error: Failed to parse current config: %v\n", err)
			return
		}
	} else {
		currentConfig = make(map[string]interface{})
	}

	// Update the specific parameter using map navigation
	parts := strings.Split(path, ".")
	current := currentConfig

	for i := 0; i < len(parts)-1; i++ {
		if _, exists := current[parts[i]]; !exists {
			current[parts[i]] = make(map[string]interface{})
		}
		current = current[parts[i]].(map[string]interface{})
	}

	// Convert value to appropriate type
	finalKey := parts[len(parts)-1]

	switch {
	case value == "true":
		current[finalKey] = true
	case value == "false":
		current[finalKey] = false
	case strings.Contains(value, ","):
		// Handle comma-separated values (like CORS origins)
		current[finalKey] = strings.Split(value, ",")
	case shouldConvertToInt(path):
		// Only convert specific numeric fields to integers
		if num, err := fmt.Sscanf(value, "%d", new(int)); err == nil && num == 1 {
			var intVal int
			fmt.Sscanf(value, "%d", &intVal)
			current[finalKey] = intVal
		} else {
			current[finalKey] = value
		}
	default:
		// Keep as string for all other cases (including AI models)
		current[finalKey] = value
	}

	// Convert back to JSON for sending
	jsonData, err := json.Marshal(currentConfig)
	if err != nil {
		fmt.Printf("Error: Failed to marshal config: %v\n", err)
		return
	}

	req, _ = http.NewRequest("PUT", fmt.Sprintf("%s/api/v1/config", BackendURL), bytes.NewBuffer(jsonData))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err = client.Do(req)
	if err != nil {
		fmt.Printf("Error: Failed to connect to backend: %v\n", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := ioutil.ReadAll(resp.Body)
		fmt.Printf("Error: Failed to update config: %s - %s\n", resp.Status, string(body))
		return
	}

	fmt.Printf("Successfully updated %s = %s\n", path, value)
}

// shouldConvertToInt determines if a config path should have its value converted to integer
func shouldConvertToInt(path string) bool {
	integerFields := []string{
		"server.port",
		"replication.batch_size",
		"replication.parallelism",
		"monitoring.retention_days",
	}

	for _, field := range integerFields {
		if path == field {
			return true
		}
	}
	return false
}

// createTLSCommand creates TLS certificate management commands
func createTLSCommand() *cobra.Command {
	tlsCmd := &cobra.Command{
		Use:   "tls",
		Short: "Manage TLS certificates and secure communication",
		Long:  "Generate certificates and configure secure TLS communication between CLI and server",
	}

	// tls generate-certs
	generateCmd := &cobra.Command{
		Use:   "generate-certs",
		Short: "Generate TLS certificates for server and client",
		Long:  "Generate self-signed TLS certificates for secure CLI-to-server communication",
		Run: func(cmd *cobra.Command, args []string) {
			token, err := LoadToken()
			if err != nil {
				fmt.Println("Error: You must be logged in to perform this action. Please run 'mirror-cli login'.")
				return
			}

			user, err := fetchMe(token)
			if err != nil {
				fmt.Printf("Error: Failed to fetch user profile: %v\n", err)
				return
			}

			if user["role"] != "admin" {
				fmt.Println("Error: You must be an admin to perform this action.")
				return
			}

			var outputDir string
			promptDir := &survey.Input{
				Message: "Enter output directory for certificates:",
				Default: "./certs",
			}
			survey.AskOne(promptDir, &outputDir)

			var serverHost string
			promptHost := &survey.Input{
				Message: "Enter server hostname/IP for certificate:",
				Default: "localhost",
			}
			survey.AskOne(promptHost, &serverHost)

			var organization string
			promptOrg := &survey.Input{
				Message: "Enter organization name:",
				Default: "kaf-mirror",
			}
			survey.AskOne(promptOrg, &organization)

			// Create output directory
			if err := os.MkdirAll(outputDir, 0755); err != nil {
				fmt.Printf("Error: Failed to create output directory: %v\n", err)
				return
			}

			fmt.Println("Generating TLS certificates...")

			// Generate CA certificate
			caScript := fmt.Sprintf(`#!/bin/bash
set -e

# Create CA private key
openssl genrsa -out %s/ca-key.pem 4096

# Create CA certificate
openssl req -new -x509 -days 365 -key %s/ca-key.pem -sha256 -out %s/ca.pem -subj "/C=US/ST=CA/L=San Francisco/O=%s/CN=kaf-mirror-ca"

# Create server private key
openssl genrsa -out %s/server-key.pem 4096

# Create server certificate request
openssl req -subj "/C=US/ST=CA/L=San Francisco/O=%s/CN=%s" -new -key %s/server-key.pem -out %s/server.csr

# Create server certificate
echo "subjectAltName = DNS:%s,IP:127.0.0.1,IP:0.0.0.0" > %s/extfile.cnf
echo "extendedKeyUsage = serverAuth" >> %s/extfile.cnf
openssl x509 -req -days 365 -in %s/server.csr -CA %s/ca.pem -CAkey %s/ca-key.pem -out %s/server-cert.pem -extensions v3_req -extfile %s/extfile.cnf -CAcreateserial

# Create client private key
openssl genrsa -out %s/client-key.pem 4096

# Create client certificate request
openssl req -subj "/C=US/ST=CA/L=San Francisco/O=%s/CN=kaf-mirror-client" -new -key %s/client-key.pem -out %s/client.csr

# Create client certificate
echo "extendedKeyUsage = clientAuth" > %s/client-extfile.cnf
openssl x509 -req -days 365 -in %s/client.csr -CA %s/ca.pem -CAkey %s/ca-key.pem -out %s/client-cert.pem -extfile %s/client-extfile.cnf -CAcreateserial

# Set proper permissions
chmod 400 %s/ca-key.pem %s/server-key.pem %s/client-key.pem
chmod 444 %s/ca.pem %s/server-cert.pem %s/client-cert.pem

# Clean up CSR and extension files
rm %s/server.csr %s/client.csr %s/extfile.cnf %s/client-extfile.cnf

echo "TLS certificates generated successfully in %s/"
echo "Generated files:"
echo "   - ca.pem (Certificate Authority)"
echo "   - ca-key.pem (CA Private Key)"
echo "   - server-cert.pem (Server Certificate)"
echo "   - server-key.pem (Server Private Key)"
echo "   - client-cert.pem (Client Certificate)"
echo "   - client-key.pem (Client Private Key)"
echo ""
echo "Next steps:"
echo "   1. Configure server TLS: mirror-cli config edit server tls"
echo "   2. Set server certificate paths to: %s/server-cert.pem and %s/server-key.pem"
echo "   3. Configure CLI client certificates: mirror-cli tls configure-client"
`, outputDir, outputDir, outputDir, organization, outputDir, organization, serverHost, outputDir, outputDir, serverHost, outputDir, outputDir, outputDir, outputDir, outputDir, outputDir, outputDir, outputDir, organization, outputDir, outputDir, outputDir, outputDir, outputDir, outputDir, outputDir, outputDir, outputDir, outputDir, outputDir, outputDir, outputDir, outputDir, outputDir, outputDir, outputDir, outputDir, outputDir, outputDir, outputDir)

			// Write script to temporary file
			scriptFile, err := ioutil.TempFile("", "gen-certs-*.sh")
			if err != nil {
				fmt.Printf("Error: Failed to create script file: %v\n", err)
				return
			}
			defer os.Remove(scriptFile.Name())

			if _, err := scriptFile.WriteString(caScript); err != nil {
				fmt.Printf("Error: Failed to write script: %v\n", err)
				return
			}
			scriptFile.Close()

			// Make script executable
			if err := os.Chmod(scriptFile.Name(), 0755); err != nil {
				fmt.Printf("Error: Failed to make script executable: %v\n", err)
				return
			}

			// Execute script
			execCmd := exec.Command("/bin/bash", scriptFile.Name())
			execCmd.Stdout = os.Stdout
			execCmd.Stderr = os.Stderr

			if err := execCmd.Run(); err != nil {
				fmt.Printf("Error: Failed to generate certificates: %v\n", err)
				fmt.Println("Make sure OpenSSL is installed on your system.")
				return
			}
		},
	}

	// tls configure-client
	configClientCmd := &cobra.Command{
		Use:   "configure-client",
		Short: "Configure CLI client for TLS communication",
		Long:  "Configure the CLI to use client certificates for secure communication",
		Run: func(cmd *cobra.Command, args []string) {
			var certDir string
			promptDir := &survey.Input{
				Message: "Enter directory containing client certificates:",
				Default: "./certs",
			}
			survey.AskOne(promptDir, &certDir)

			var serverURL string
			promptURL := &survey.Input{
				Message: "Enter HTTPS server URL:",
				Default: "https://localhost:8080",
			}
			survey.AskOne(promptURL, &serverURL)

			// Update the global BackendURL to use HTTPS
			BackendURL = serverURL

			// Store TLS configuration in user home directory
			home, err := os.UserHomeDir()
			if err != nil {
				fmt.Printf("Error: Failed to get home directory: %v\n", err)
				return
			}

			configDir := filepath.Join(home, ".kaf-mirror")
			if err := os.MkdirAll(configDir, 0700); err != nil {
				fmt.Printf("Error: Failed to create config directory: %v\n", err)
				return
			}

			tlsConfig := map[string]string{
				"server_url":  serverURL,
				"ca_cert":     filepath.Join(certDir, "ca.pem"),
				"client_cert": filepath.Join(certDir, "client-cert.pem"),
				"client_key":  filepath.Join(certDir, "client-key.pem"),
			}

			configBytes, err := yaml.Marshal(tlsConfig)
			if err != nil {
				fmt.Printf("Error: Failed to marshal TLS config: %v\n", err)
				return
			}

			configFile := filepath.Join(configDir, "tls-config.yml")
			if err := ioutil.WriteFile(configFile, configBytes, 0600); err != nil {
				fmt.Printf("Error: Failed to write TLS config: %v\n", err)
				return
			}

			fmt.Println("CLI TLS configuration saved successfully!")
			fmt.Printf("Configuration file: %s\n", configFile)
			fmt.Printf("Server URL: %s\n", serverURL)
			fmt.Println("")
			fmt.Println("TLS client configuration complete. All CLI communication will now use HTTPS with client certificates.")
		},
	}

	// tls verify
	verifyCmd := &cobra.Command{
		Use:   "verify",
		Short: "Verify TLS connection to server",
		Long:  "Test the TLS connection and certificate validation",
		Run: func(cmd *cobra.Command, args []string) {
			token, err := LoadToken()
			if err != nil {
				fmt.Println("Error: You must be logged in to perform this action. Please run 'mirror-cli login'.")
				return
			}

			fmt.Printf("Testing TLS connection to: %s\n", BackendURL)

			// Test health endpoint with TLS
			req, _ := http.NewRequest("GET", fmt.Sprintf("%s/health", BackendURL), nil)
			req.Header.Set("Authorization", "Bearer "+token)

			client := &http.Client{
				Timeout: 10 * time.Second,
			}

			resp, err := client.Do(req)
			if err != nil {
				fmt.Printf("TLS connection failed: %v\n", err)
				fmt.Println("Make sure:")
				fmt.Println("   - Server is running with TLS enabled")
				fmt.Println("   - Certificates are valid and not expired")
				fmt.Println("   - Client certificates are configured correctly")
				return
			}
			defer resp.Body.Close()

			if resp.StatusCode == http.StatusOK {
				fmt.Println("TLS connection successful!")
				fmt.Printf("TLS Version: %d\n", resp.TLS.Version)
				fmt.Printf("Cipher Suite: %d\n", resp.TLS.CipherSuite)
				fmt.Println("Certificate chain verified successfully")
			} else {
				fmt.Printf("TLS connection established but server returned: %s\n", resp.Status)
			}
		},
	}

	// tls import-cert
	importCmd := &cobra.Command{
		Use:   "import-cert",
		Short: "Import custom TLS certificate and key",
		Long:  "Import your own TLS certificate and private key for the server",
		Run: func(cmd *cobra.Command, args []string) {
			token, err := LoadToken()
			if err != nil {
				fmt.Println("Error: You must be logged in to perform this action. Please run 'mirror-cli login'.")
				return
			}

			user, err := fetchMe(token)
			if err != nil {
				fmt.Printf("Error: Failed to fetch user profile: %v\n", err)
				return
			}

			if user["role"] != "admin" {
				fmt.Println("Error: You must be an admin to perform this action.")
				return
			}

			var certFile string
			promptCert := &survey.Input{
				Message: "Enter path to your TLS certificate file (.crt or .pem):",
			}
			survey.AskOne(promptCert, &certFile)

			var keyFile string
			promptKey := &survey.Input{
				Message: "Enter path to your TLS private key file (.key or .pem):",
			}
			survey.AskOne(promptKey, &keyFile)

			// Validate files exist and are readable
			if _, err := os.Stat(certFile); os.IsNotExist(err) {
				fmt.Printf("Error: Certificate file does not exist: %s\n", certFile)
				return
			}

			if _, err := os.Stat(keyFile); os.IsNotExist(err) {
				fmt.Printf("Error: Key file does not exist: %s\n", keyFile)
				return
			}

			// Test certificate and key compatibility
			fmt.Println("Validating certificate and key compatibility...")
			if err := validateCertificateKeyPair(certFile, keyFile); err != nil {
				fmt.Printf("Error: Certificate and key validation failed: %v\n", err)
				return
			}

			fmt.Println("Certificate and key validation successful.")

			// Configure server to use these certificates
			fmt.Println("Configuring server TLS settings...")
			updateConfigParameter("server.tls.enabled", "true")
			updateConfigParameter("server.tls.cert_file", certFile)
			updateConfigParameter("server.tls.key_file", keyFile)

			// Configure CLI client
			var configureClient bool
			promptClient := &survey.Confirm{
				Message: "Configure CLI client for HTTPS communication?",
				Default: true,
			}
			survey.AskOne(promptClient, &configureClient)

			if configureClient {
				var serverURL string
				promptURL := &survey.Input{
					Message: "Enter HTTPS server URL:",
					Default: "https://localhost:8080",
				}
				survey.AskOne(promptURL, &serverURL)

				// Update the global BackendURL to use HTTPS
				BackendURL = serverURL

				// Store TLS configuration
				home, err := os.UserHomeDir()
				if err != nil {
					fmt.Printf("Error: Failed to get home directory: %v\n", err)
					return
				}

				configDir := filepath.Join(home, ".kaf-mirror")
				if err := os.MkdirAll(configDir, 0700); err != nil {
					fmt.Printf("Error: Failed to create config directory: %v\n", err)
					return
				}

				tlsConfig := map[string]string{
					"server_url":  serverURL,
					"server_cert": certFile, // Store server cert for validation
					"insecure":    "false",  // Set to true for self-signed certs
				}

				// Ask about insecure mode for self-signed certificates
				var allowInsecure bool
				promptInsecure := &survey.Confirm{
					Message: "Allow insecure connections (for self-signed certificates)?",
					Default: false,
				}
				survey.AskOne(promptInsecure, &allowInsecure)

				if allowInsecure {
					tlsConfig["insecure"] = "true"
				}

				configBytes, err := yaml.Marshal(tlsConfig)
				if err != nil {
					fmt.Printf("Error: Failed to marshal TLS config: %v\n", err)
					return
				}

				configFile := filepath.Join(configDir, "tls-config.yml")
				if err := ioutil.WriteFile(configFile, configBytes, 0600); err != nil {
					fmt.Printf("Error: Failed to write TLS config: %v\n", err)
					return
				}

				fmt.Println("TLS configuration completed successfully!")
				fmt.Printf("Server certificate: %s\n", certFile)
				fmt.Printf("Server private key: %s\n", keyFile)
				fmt.Printf("CLI configuration: %s\n", configFile)
				fmt.Println("")
				fmt.Println("Next steps:")
				fmt.Println("1. Restart the kaf-mirror server to apply TLS settings")
				fmt.Println("2. Access the web dashboard at: " + serverURL)
				fmt.Println("3. For self-signed certificates, accept the browser security warning")
			}

			fmt.Println("Certificate import completed.")
		},
	}

	tlsCmd.AddCommand(generateCmd, importCmd, configClientCmd, verifyCmd)
	return tlsCmd
}

// formatLastActive formats the last active time in a human-readable way
func formatLastActive(t time.Time) string {
	now := time.Now()
	duration := now.Sub(t)

	if duration < time.Minute {
		return "Just now"
	} else if duration < time.Hour {
		minutes := int(duration.Minutes())
		return fmt.Sprintf("%dm ago", minutes)
	} else if duration < 24*time.Hour {
		hours := int(duration.Hours())
		return fmt.Sprintf("%dh ago", hours)
	} else {
		days := int(duration.Hours() / 24)
		return fmt.Sprintf("%dd ago", days)
	}
}

// validateCertificateKeyPair validates that a certificate and private key are compatible
func validateCertificateKeyPair(certFile, keyFile string) error {
	// Create temporary test script to validate cert/key pair
	testScript := `#!/bin/bash
set -e

CERT_FILE="` + certFile + `"
KEY_FILE="` + keyFile + `"

# Test that certificate and key match
CERT_MODULUS=$(openssl x509 -noout -modulus -in "$CERT_FILE" 2>/dev/null | openssl md5)
KEY_MODULUS=$(openssl rsa -noout -modulus -in "$KEY_FILE" 2>/dev/null | openssl md5)

if [ "$CERT_MODULUS" != "$KEY_MODULUS" ]; then
    echo "Error: Certificate and private key do not match"
    exit 1
fi

# Check certificate validity
if ! openssl x509 -checkend 86400 -noout -in "$CERT_FILE" >/dev/null 2>&1; then
    echo "Warning: Certificate will expire within 24 hours"
fi

echo "Certificate and private key validation successful"
`

	// Write script to temporary file
	scriptFile, err := ioutil.TempFile("", "validate-cert-*.sh")
	if err != nil {
		return fmt.Errorf("failed to create validation script: %v", err)
	}
	defer os.Remove(scriptFile.Name())

	if _, err := scriptFile.WriteString(testScript); err != nil {
		return fmt.Errorf("failed to write validation script: %v", err)
	}
	scriptFile.Close()

	// Make script executable
	if err := os.Chmod(scriptFile.Name(), 0755); err != nil {
		return fmt.Errorf("failed to make script executable: %v", err)
	}

	// Execute validation script
	cmd := exec.Command("/bin/bash", scriptFile.Name())
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("validation failed: %v - %s", err, string(output))
	}

	return nil
}

func fetchCluster(token, clusterName string) (map[string]interface{}, error) {
	req, _ := http.NewRequest("GET", fmt.Sprintf("%s/api/v1/clusters/%s", BackendURL, clusterName), nil)
	req.Header.Set("Authorization", "Bearer "+token)
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var cluster map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&cluster)
	return cluster, nil
}

func fetchJob(token, jobID string) (map[string]interface{}, error) {
	req, _ := http.NewRequest("GET", fmt.Sprintf("%s/api/v1/jobs/%s", BackendURL, jobID), nil)
	req.Header.Set("Authorization", "Bearer "+token)
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var job map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&job)
	return job, nil
}

func fetchJobMappings(token, jobID string) ([]map[string]interface{}, error) {
	req, _ := http.NewRequest("GET", fmt.Sprintf("%s/api/v1/jobs/%s/mappings", BackendURL, jobID), nil)
	req.Header.Set("Authorization", "Bearer "+token)
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var mappings []map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&mappings)
	return mappings, nil
}
