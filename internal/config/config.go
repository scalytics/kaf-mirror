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

package config

import (
	"fmt"
	"kaf-mirror/pkg/utils"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/viper"
)

// Config holds the entire application configuration
type Config struct {
	Server      ServerConfig             `mapstructure:"server"`
	Database    DatabaseConfig           `mapstructure:"database"`
	Logging     LoggingConfig            `mapstructure:"logging"`
	Clusters    map[string]ClusterConfig `mapstructure:"clusters"`
	Replication ReplicationConfig        `mapstructure:"replication"`
	Topics      []TopicMapping           `mapstructure:"topic_mappings"`
	AI          AIConfig                 `mapstructure:"ai"`
	Monitoring  MonitoringConfig         `mapstructure:"monitoring"`
	Compliance  ComplianceConfig         `mapstructure:"compliance"`
}

// ServerConfig defines server settings
type ServerConfig struct {
	Host          string `mapstructure:"host"`
	Port          int    `mapstructure:"port"`
	Mode          string `mapstructure:"mode"`
	AdminEmail    string `mapstructure:"admin_email"`
	WebPath       string `mapstructure:"web_path"`
	AllowInsecure bool   `mapstructure:"allow_insecure"`
	TLS           struct {
		Enabled  bool   `mapstructure:"enabled"`
		CertFile string `mapstructure:"cert_file"`
		KeyFile  string `mapstructure:"key_file"`
	} `mapstructure:"tls"`
	CORS struct {
		AllowedOrigins []string `mapstructure:"allowed_origins"`
	} `mapstructure:"cors"`
}

// ListenAddr is host:port, defaulting an empty host to localhost.
func (s ServerConfig) ListenAddr() string {
	host := s.Host
	if host == "" {
		host = "localhost"
	}
	return fmt.Sprintf("%s:%d", host, s.Port)
}

// RequireSecureListen fails closed in production when TLS is off.
func (s ServerConfig) RequireSecureListen() error {
	if !strings.EqualFold(s.Mode, "production") {
		return nil
	}
	if s.TLS.Enabled || s.AllowInsecure {
		return nil
	}
	return fmt.Errorf("tls is required when server.mode is production; set server.allow_insecure to override")
}

// DatabaseConfig defines database settings
type DatabaseConfig struct {
	Path          string `mapstructure:"path"`
	RetentionDays int    `mapstructure:"retention_days"`
}

type LoggingConfig struct {
	Level      string `mapstructure:"level"`
	File       string `mapstructure:"file"`
	MaxSize    int    `mapstructure:"max_size"`
	MaxBackups int    `mapstructure:"max_backups"`
	MaxAge     int    `mapstructure:"max_age"`
	Console    bool   `mapstructure:"console"`
}

// ClusterConfig defines Kafka cluster connection details
type ClusterConfig struct {
	Provider  string         `mapstructure:"provider"`
	ClusterID string         `mapstructure:"cluster_id"`
	Brokers   string         `mapstructure:"brokers"`
	Security  SecurityConfig `mapstructure:"security"`

	// DisableIdempotentWrites forces the producer into non-idempotent mode.
	// Set this to true when the target broker does not implement the
	// Kafka INIT_PRODUCER_ID API (API key 22). As of 2026-04, this applies
	// to KafScale; see OPS-004.
	DisableIdempotentWrites bool `mapstructure:"disable_idempotent_writes"`
}

// SecurityConfig defines security settings for Kafka connections
type SecurityConfig struct {
	Enabled          bool    `mapstructure:"enabled" json:"enabled"`
	Protocol         string  `mapstructure:"protocol" json:"protocol"`
	SASLMechanism    string  `mapstructure:"sasl_mechanism" json:"sasl_mechanism"`
	Username         string  `mapstructure:"username" json:"username"`
	Password         string  `mapstructure:"password" json:"password"`
	APIKey           string  `mapstructure:"api_key" json:"api_key"`
	APISecret        string  `mapstructure:"api_secret" json:"api_secret"`
	ConnectionString *string `mapstructure:"connection_string" json:"connection_string"`
	Kerberos         struct {
		ServiceName string `mapstructure:"service_name" json:"service_name"`
	} `mapstructure:"kerberos" json:"kerberos"`
}

// ReplicationConfig defines replication parameters
type ReplicationConfig struct {
	BatchSize              int    `mapstructure:"batch_size"`
	Parallelism            int    `mapstructure:"parallelism"`
	Compression            string `mapstructure:"compression"`
	JobID                  string `mapstructure:"job_id"`
	TopicDiscoveryInterval string `mapstructure:"topic_discovery_interval"`
}

// TopicMapping defines a single source-to-target topic mapping
type TopicMapping struct {
	Source  string `mapstructure:"source"`
	Target  string `mapstructure:"target"`
	Enabled bool   `mapstructure:"enabled"`
}

// AIConfig defines AI provider settings
type AIConfig struct {
	Provider    string      `mapstructure:"provider"`
	Endpoint    string      `mapstructure:"endpoint"`
	Token       string      `mapstructure:"token"`
	APISecret   string      `mapstructure:"api_secret"`
	Model       string      `mapstructure:"model"`
	Features    AIFeatures  `mapstructure:"features"`
	SelfHealing SelfHealing `mapstructure:"self_healing"`
}

// Validate checks the configuration for basic correctness.
func (c *Config) Validate() error {
	if c.Server.Port == 0 {
		return fmt.Errorf("server port must be set")
	}
	if len(c.Clusters) == 0 {
		return fmt.Errorf("at least one cluster must be defined")
	}
	if c.Database.RetentionDays <= 0 {
		c.Database.RetentionDays = 30
	}
	if c.Database.RetentionDays > 30 {
		return fmt.Errorf("database retention_days must be between 1 and 30")
	}
	if c.Replication.TopicDiscoveryInterval == "" {
		c.Replication.TopicDiscoveryInterval = "5m"
	}
	interval, err := time.ParseDuration(c.Replication.TopicDiscoveryInterval)
	if err != nil {
		return fmt.Errorf("replication topic_discovery_interval must be a valid duration: %v", err)
	}
	if interval < 0 {
		return fmt.Errorf("replication topic_discovery_interval must be non-negative")
	}
	if c.Compliance.Schedule.RunHour < 0 || c.Compliance.Schedule.RunHour > 23 {
		return fmt.Errorf("compliance schedule run_hour must be between 0 and 23")
	}
	if c.Compliance.Schedule.Enabled {
		if !c.Compliance.Schedule.Daily && !c.Compliance.Schedule.Weekly && !c.Compliance.Schedule.Monthly {
			return fmt.Errorf("compliance schedule must enable at least one period")
		}
	}
	return nil
}

// AIFeatures defines which AI features are enabled
type AIFeatures struct {
	AnomalyDetection        bool `mapstructure:"anomaly_detection"`
	PerformanceOptimization bool `mapstructure:"performance_optimization"`
	IncidentAnalysis        bool `mapstructure:"incident_analysis"`
}

// SelfHealing defines automated remediation and optimization features
type SelfHealing struct {
	AutomatedRemediation bool `mapstructure:"automated_remediation"`
	DynamicThrottling    bool `mapstructure:"dynamic_throttling"`
}

// MonitoringConfig defines monitoring and alerting settings
type MonitoringConfig struct {
	Enabled    bool             `mapstructure:"enabled"`
	Platform   string           `mapstructure:"platform"` // "splunk", "loki", "prometheus"
	Splunk     SplunkConfig     `mapstructure:"splunk"`
	Loki       LokiConfig       `mapstructure:"loki"`
	Prometheus PrometheusConfig `mapstructure:"prometheus"`
}

// ComplianceConfig defines compliance reporting schedule settings
type ComplianceConfig struct {
	Schedule ComplianceSchedule `mapstructure:"schedule"`
}

// ComplianceSchedule controls automated report generation
type ComplianceSchedule struct {
	Enabled bool `mapstructure:"enabled"`
	RunHour int  `mapstructure:"run_hour"` // 0-23 local time
	Daily   bool `mapstructure:"daily"`
	Weekly  bool `mapstructure:"weekly"`
	Monthly bool `mapstructure:"monthly"`
}

// SplunkConfig defines Splunk-specific settings
type SplunkConfig struct {
	HECEndpoint string `mapstructure:"hec_endpoint"`
	HECToken    string `mapstructure:"hec_token"`
	Index       string `mapstructure:"index"`
}

// LokiConfig defines Grafana Loki-specific settings
type LokiConfig struct {
	Endpoint string `mapstructure:"endpoint"`
}

// PrometheusConfig defines Prometheus-specific settings
type PrometheusConfig struct {
	PushGateway string `mapstructure:"push_gateway"`
}

var AppConfig Config

// LoadConfig loads configuration from standard paths.
func LoadConfig() (*Config, error) {
	viper.AddConfigPath("/etc/kaf-mirror")                             // Production
	viper.AddConfigPath(filepath.Join(utils.ProjectRoot(), "configs")) // Development
	viper.SetConfigName("default")
	viper.SetConfigType("yml")

	viper.AutomaticEnv()
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	viper.SetEnvPrefix("KAF_MIRROR")

	// Read the default configuration file
	if err := viper.ReadInConfig(); err != nil {
		return nil, err
	}

	// Merge in the production configuration file if it exists
	viper.SetConfigName("prod")
	if err := viper.MergeInConfig(); err != nil {
		// Ignore if the file is not found, as it is optional
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, err
		}
	}

	if err := viper.Unmarshal(&AppConfig); err != nil {
		return nil, err
	}
	if AppConfig.Database.RetentionDays <= 0 {
		AppConfig.Database.RetentionDays = 30
	}
	if AppConfig.Database.RetentionDays > 30 {
		AppConfig.Database.RetentionDays = 30
	}
	if AppConfig.Replication.TopicDiscoveryInterval == "" {
		AppConfig.Replication.TopicDiscoveryInterval = "5m"
	}
	applyComplianceDefaults(&AppConfig)

	// Dynamically set log file path with date if not already set
	if !strings.Contains(AppConfig.Logging.File, "20") { // Basic check for a date
		timestamp := time.Now().Format("2006-01-02")
		ext := filepath.Ext(AppConfig.Logging.File)
		base := strings.TrimSuffix(AppConfig.Logging.File, ext)
		AppConfig.Logging.File = fmt.Sprintf("%s-%s%s", base, timestamp, ext)
	}

	return &AppConfig, nil
}

func applyComplianceDefaults(cfg *Config) {
	if cfg.Compliance.Schedule.RunHour < 0 || cfg.Compliance.Schedule.RunHour > 23 {
		cfg.Compliance.Schedule.RunHour = 2
	}
	if !cfg.Compliance.Schedule.Enabled {
		return
	}
	if !cfg.Compliance.Schedule.Daily && !cfg.Compliance.Schedule.Weekly && !cfg.Compliance.Schedule.Monthly {
		cfg.Compliance.Schedule.Daily = true
	}
}
