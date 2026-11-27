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
	"github.com/scalytics/kaf-mirror/v2/internal/config"
	"time"

	"github.com/jmoiron/sqlx"
)

// SaveConfig saves the entire configuration to the database.
// This is a simplified approach; a more granular approach might be better.
func SaveConfig(db *sqlx.DB, cfg *config.Config) error {
	// We'll store the entire config as a single JSON blob for simplicity.
	// A more robust solution would store individual keys.
	configJSON, err := json.Marshal(cfg)
	if err != nil {
		return err
	}

	query := `INSERT OR REPLACE INTO configuration (key, value, updated_at) VALUES (?, ?, ?)`
	_, err = db.Exec(query, "full_config", string(configJSON), time.Now())
	return err
}

// LoadConfig retrieves the configuration from the database.
func LoadConfig(db *sqlx.DB) (*config.Config, error) {
	var configJSON string
	err := db.Get(&configJSON, "SELECT value FROM configuration WHERE key = 'full_config'")
	if err != nil {
		return nil, err
	}

	var cfg config.Config
	if err := json.Unmarshal([]byte(configJSON), &cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}
