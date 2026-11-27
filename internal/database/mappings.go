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
	"github.com/scalytics/kaf-mirror/v2/internal/config"

	"github.com/jmoiron/sqlx"
)

func ConfigTopicMappings(mappings []TopicMapping) []config.TopicMapping {
	out := make([]config.TopicMapping, len(mappings))
	for i, m := range mappings {
		out[i] = config.TopicMapping{
			Source:  m.SourceTopicPattern,
			Target:  m.TargetTopicPattern,
			Enabled: m.Enabled,
		}
	}
	return out
}

// GetMappingsForJob retrieves all topic mappings for a given job ID.
func GetMappingsForJob(db *sqlx.DB, jobID string) ([]TopicMapping, error) {
	var mappings []TopicMapping
	err := db.Select(&mappings, "SELECT * FROM topic_mappings WHERE job_id = ?", jobID)
	return mappings, err
}

// UpdateMappingsForJob replaces all topic mappings for a given job ID.
func UpdateMappingsForJob(db *sqlx.DB, jobID string, mappings []TopicMapping) error {
	tx, err := db.Beginx()
	if err != nil {
		return err
	}

	// It's often easiest to delete all existing mappings and re-insert them.
	// For very large sets, a more sophisticated diff-and-update would be better.
	_, err = tx.Exec("DELETE FROM topic_mappings WHERE job_id = ?", jobID)
	if err != nil {
		tx.Rollback()
		return err
	}

	for _, m := range mappings {
		query := `INSERT INTO topic_mappings (job_id, source_topic_pattern, target_topic_pattern, enabled)
				  VALUES (?, ?, ?, ?)`
		_, err = tx.Exec(query, jobID, m.SourceTopicPattern, m.TargetTopicPattern, m.Enabled)
		if err != nil {
			tx.Rollback()
			return err
		}
	}

	return tx.Commit()
}
