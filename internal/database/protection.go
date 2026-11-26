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
	"database/sql"
	"errors"
	"time"

	"github.com/jmoiron/sqlx"
)

type ProtectionState struct {
	Enabled    bool       `db:"enabled" json:"enabled"`
	Halted     bool       `db:"halted" json:"halted"`
	HaltReason string     `db:"halt_reason" json:"halt_reason"`
	HaltedAt   *time.Time `db:"halted_at" json:"halted_at,omitempty"`
	HaltedBy   string     `db:"halted_by" json:"halted_by"`
}

func GetProtectionState(db *sqlx.DB) (*ProtectionState, error) {
	var st ProtectionState
	err := db.Get(&st, `SELECT enabled, halted, COALESCE(halt_reason, '') as halt_reason, halted_at, COALESCE(halted_by, '') as halted_by FROM protection_state WHERE id = 1`)
	if errors.Is(err, sql.ErrNoRows) {
		return &ProtectionState{}, nil
	}
	return &st, err
}

func SetProtectionEnabled(db *sqlx.DB, enabled bool) error {
	_, err := db.Exec(`INSERT INTO protection_state (id, enabled, halted) VALUES (1, ?, 0)
		ON CONFLICT(id) DO UPDATE SET enabled = excluded.enabled`, boolToInt(enabled))
	return err
}

func SetProtectionHalted(db *sqlx.DB, halted bool, reason, by string) error {
	if !halted {
		_, err := db.Exec(`UPDATE protection_state SET halted = 0, halt_reason = '', halted_at = NULL, halted_by = '' WHERE id = 1`)
		return err
	}
	now := time.Now().UTC()
	_, err := db.Exec(`INSERT INTO protection_state (id, enabled, halted, halt_reason, halted_at, halted_by)
		VALUES (1, 1, 1, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET halted = 1, halt_reason = excluded.halt_reason, halted_at = excluded.halted_at, halted_by = excluded.halted_by`,
		reason, now, by)
	return err
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
