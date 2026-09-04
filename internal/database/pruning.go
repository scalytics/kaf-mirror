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
	"time"

	"github.com/jmoiron/sqlx"
)

func PruneOldData(db *sqlx.DB, retentionDays int) error {
	if retentionDays <= 0 || retentionDays > 30 {
		retentionDays = 30
	}
	cutoff := time.Now().AddDate(0, 0, -retentionDays)

	_, err := db.Exec(`DELETE FROM aggregated_metrics WHERE timestamp < ?`, cutoff)
	if err != nil {
		return err
	}

	_, err = db.Exec(`DELETE FROM operational_events WHERE timestamp < ?`, cutoff)
	if err != nil {
		return err
	}

	mirrorStateCutoff := time.Now().AddDate(0, 0, -7)

	_, err = db.Exec(`DELETE FROM mirror_progress_history WHERE last_updated < ?`, mirrorStateCutoff)
	if err != nil {
		return err
	}

	_, err = db.Exec(`DELETE FROM mirror_gaps WHERE detected_at < ? AND resolved_at IS NOT NULL`, mirrorStateCutoff)
	if err != nil {
		return err
	}

	_, err = db.Exec(`DELETE FROM mirror_state_analysis WHERE analyzed_at < ?`, mirrorStateCutoff)
	if err != nil {
		return err
	}

	return nil
}
