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

const SecretPlaceholder = "***"

func isSecretPlaceholder(s string) bool {
	return s == "" || s == SecretPlaceholder || s == "***MASKED***"
}

func maskSecret(s string) string {
	if s == "" {
		return ""
	}
	return SecretPlaceholder
}

// Redacted returns a copy with credential fields replaced by SecretPlaceholder.
func (c Config) Redacted() Config {
	out := c
	out.AI.Token = maskSecret(c.AI.Token)
	out.AI.APISecret = maskSecret(c.AI.APISecret)
	out.Monitoring.Splunk.HECToken = maskSecret(c.Monitoring.Splunk.HECToken)
	if c.Clusters == nil {
		return out
	}
	out.Clusters = make(map[string]ClusterConfig, len(c.Clusters))
	for name, cluster := range c.Clusters {
		cluster.Security.Password = maskSecret(cluster.Security.Password)
		cluster.Security.APIKey = maskSecret(cluster.Security.APIKey)
		cluster.Security.APISecret = maskSecret(cluster.Security.APISecret)
		if cluster.Security.ConnectionString != nil && *cluster.Security.ConnectionString != "" {
			placeholder := SecretPlaceholder
			cluster.Security.ConnectionString = &placeholder
		}
		out.Clusters[name] = cluster
	}
	return out
}

// RestoreUnchangedSecrets copies credentials from existing when incoming uses a placeholder or omit.
func (c *Config) RestoreUnchangedSecrets(existing *Config) {
	if existing == nil {
		return
	}
	if isSecretPlaceholder(c.AI.Token) {
		c.AI.Token = existing.AI.Token
	}
	if isSecretPlaceholder(c.AI.APISecret) {
		c.AI.APISecret = existing.AI.APISecret
	}
	if isSecretPlaceholder(c.Monitoring.Splunk.HECToken) {
		c.Monitoring.Splunk.HECToken = existing.Monitoring.Splunk.HECToken
	}
	if c.Clusters == nil {
		return
	}
	for name, cluster := range c.Clusters {
		old, ok := existing.Clusters[name]
		if !ok {
			continue
		}
		if isSecretPlaceholder(cluster.Security.Password) {
			cluster.Security.Password = old.Security.Password
		}
		if isSecretPlaceholder(cluster.Security.APIKey) {
			cluster.Security.APIKey = old.Security.APIKey
		}
		if isSecretPlaceholder(cluster.Security.APISecret) {
			cluster.Security.APISecret = old.Security.APISecret
		}
		if cluster.Security.ConnectionString == nil || isSecretPlaceholder(*cluster.Security.ConnectionString) {
			cluster.Security.ConnectionString = old.Security.ConnectionString
		}
		c.Clusters[name] = cluster
	}
}
