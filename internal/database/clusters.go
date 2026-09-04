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

package database

import (
	"encoding/json"
	"errors"
	"time"

	"kaf-mirror/internal/config"

	"github.com/jmoiron/sqlx"
)

func isSecretPlaceholder(s string) bool {
	return s == "" || s == config.SecretPlaceholder || s == "***MASKED***"
}

// Redacted returns a copy with credential fields replaced by placeholders.
func (c KafkaCluster) Redacted() KafkaCluster {
	out := c
	if out.APIKey != "" {
		out.APIKey = config.SecretPlaceholder
	}
	if out.APISecret != "" {
		out.APISecret = config.SecretPlaceholder
	}
	if out.ConnectionString != nil && *out.ConnectionString != "" {
		placeholder := config.SecretPlaceholder
		out.ConnectionString = &placeholder
	}
	if out.SecurityConfig != "" && out.SecurityConfig != "{}" {
		out.SecurityConfig = config.SecretPlaceholder
	}
	return out
}

// RestoreUnchangedSecrets copies credentials from existing when incoming uses a placeholder or omit.
func (c *KafkaCluster) RestoreUnchangedSecrets(existing *KafkaCluster) {
	if existing == nil {
		return
	}
	if isSecretPlaceholder(c.APIKey) {
		c.APIKey = existing.APIKey
	}
	if isSecretPlaceholder(c.APISecret) {
		c.APISecret = existing.APISecret
	}
	if c.ConnectionString == nil || isSecretPlaceholder(*c.ConnectionString) {
		c.ConnectionString = existing.ConnectionString
	}
	if isSecretPlaceholder(c.SecurityConfig) {
		c.SecurityConfig = existing.SecurityConfig
	}
}

// ClusterConfigFromRow maps a stored cluster to a runtime Kafka config, including SASL.
func ClusterConfigFromRow(cluster KafkaCluster) config.ClusterConfig {
	out := config.ClusterConfig{
		Provider:  cluster.Provider,
		ClusterID: cluster.ClusterID,
		Brokers:   cluster.Brokers,
		Security: config.SecurityConfig{
			APIKey:           cluster.APIKey,
			APISecret:        cluster.APISecret,
			ConnectionString: cluster.ConnectionString,
		},
	}
	if cluster.SecurityConfig != "" && cluster.SecurityConfig != "{}" && cluster.SecurityConfig != config.SecretPlaceholder {
		var sec config.SecurityConfig
		if err := json.Unmarshal([]byte(cluster.SecurityConfig), &sec); err == nil {
			out.Security.Enabled = sec.Enabled || out.Security.Enabled
			if sec.Protocol != "" {
				out.Security.Protocol = sec.Protocol
			}
			if sec.SASLMechanism != "" {
				out.Security.SASLMechanism = sec.SASLMechanism
			}
			if sec.Username != "" {
				out.Security.Username = sec.Username
			}
			if sec.Password != "" {
				out.Security.Password = sec.Password
			}
			if sec.APIKey != "" {
				out.Security.APIKey = sec.APIKey
			}
			if sec.APISecret != "" {
				out.Security.APISecret = sec.APISecret
			}
			if sec.ConnectionString != nil {
				out.Security.ConnectionString = sec.ConnectionString
			}
			out.Security.Kerberos = sec.Kerberos
		}
	}
	if out.Security.APIKey != "" || out.Security.Username != "" || out.Security.Password != "" {
		out.Security.Enabled = true
	}
	return out
}

// ListClusters retrieves all Kafka clusters from the database.
func ListClusters(db *sqlx.DB) ([]KafkaCluster, error) {
	var clusters []KafkaCluster
	err := db.Select(&clusters, "SELECT * FROM kafka_clusters ORDER BY name")
	return clusters, err
}

// GetCluster retrieves a single Kafka cluster by its name.
func GetCluster(db *sqlx.DB, name string) (*KafkaCluster, error) {
	var cluster KafkaCluster
	err := db.Get(&cluster, "SELECT * FROM kafka_clusters WHERE name = ?", name)
	return &cluster, err
}

// CreateCluster inserts a new Kafka cluster into the database.
func CreateCluster(db *sqlx.DB, cluster *KafkaCluster) error {
	// Check for duplicate name
	var count int
	err := db.Get(&count, "SELECT COUNT(*) FROM kafka_clusters WHERE name = ?", cluster.Name)
	if err != nil {
		return err
	}
	if count > 0 {
		return errors.New("a cluster with this name already exists")
	}

	// Provider-aware uniqueness check
	if cluster.Provider == "confluent" && cluster.ClusterID != "" {
		err = db.Get(&count, "SELECT COUNT(*) FROM kafka_clusters WHERE cluster_id = ?", cluster.ClusterID)
		if err != nil {
			return err
		}
		if count > 0 {
			return errors.New("a confluent cluster with this cluster_id already exists")
		}
	}

	query := `INSERT INTO kafka_clusters (name, provider, cluster_id, brokers, security_config, api_key, api_secret, connection_string)
              VALUES (?, ?, ?, ?, ?, ?, ?, ?)`
	_, err = db.Exec(query, cluster.Name, cluster.Provider, cluster.ClusterID, cluster.Brokers, cluster.SecurityConfig, cluster.APIKey, cluster.APISecret, cluster.ConnectionString)
	return err
}

// UpdateCluster updates an existing Kafka cluster in the database.
func UpdateCluster(db *sqlx.DB, cluster *KafkaCluster) error {
	// Check for duplicate name
	var count int
	err := db.Get(&count, "SELECT COUNT(*) FROM kafka_clusters WHERE name = ? AND name != ?", cluster.Name, cluster.Name)
	if err != nil {
		return err
	}
	if count > 0 {
		return errors.New("a cluster with this name already exists")
	}

	// Provider-aware uniqueness check
	if cluster.Provider == "confluent" && cluster.ClusterID != "" {
		err = db.Get(&count, "SELECT COUNT(*) FROM kafka_clusters WHERE cluster_id = ? AND name != ?", cluster.ClusterID, cluster.Name)
		if err != nil {
			return err
		}
		if count > 0 {
			return errors.New("a confluent cluster with this cluster_id already exists")
		}
	}

	query := `UPDATE kafka_clusters 
              SET provider = ?, cluster_id = ?, brokers = ?, security_config = ?, api_key = ?, api_secret = ?, connection_string = ?
              WHERE name = ?`
	_, err = db.Exec(query, cluster.Provider, cluster.ClusterID, cluster.Brokers, cluster.SecurityConfig, cluster.APIKey, cluster.APISecret, cluster.ConnectionString, cluster.Name)
	return err
}

// DeleteCluster removes a Kafka cluster from the database.
func DeleteCluster(db *sqlx.DB, name string) error {
	_, err := db.Exec("DELETE FROM kafka_clusters WHERE name = ?", name)
	return err
}

// SetClusterStatus updates the status of a Kafka cluster.
func SetClusterStatus(db *sqlx.DB, name, status string) error {
	query := `UPDATE kafka_clusters SET status = ?, updated_at = CURRENT_TIMESTAMP WHERE name = ?`
	_, err := db.Exec(query, status, name)
	return err
}

// PurgeArchivedClusters permanently deletes all archived Kafka clusters.
func PurgeArchivedClusters(db *sqlx.DB) error {
	_, err := db.Exec("DELETE FROM kafka_clusters WHERE status = 'archived'")
	return err
}

// ArchiveInactiveClusters moves clusters from inactive to archived after a certain duration.
func ArchiveInactiveClusters(db *sqlx.DB, duration time.Duration) error {
	cutoff := time.Now().Add(-duration)
	query := `UPDATE kafka_clusters SET status = 'archived' WHERE status = 'inactive' AND updated_at < ?`
	_, err := db.Exec(query, cutoff)
	return err
}
