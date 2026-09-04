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
	"errors"
	"time"

	"github.com/jmoiron/sqlx"
)

// ListJobs retrieves all replication jobs from the database.
func ListJobs(db *sqlx.DB) ([]ReplicationJob, error) {
	var jobs []ReplicationJob
	err := db.Select(&jobs, "SELECT * FROM replication_jobs ORDER BY created_at DESC")
	return jobs, err
}

// GetJob retrieves a single replication job by its ID.
func GetJob(db *sqlx.DB, id string) (*ReplicationJob, error) {
	var job ReplicationJob
	err := db.Get(&job, "SELECT * FROM replication_jobs WHERE id = ?", id)
	return &job, err
}

// CreateJob inserts a new replication job into the database.
func CreateJob(db *sqlx.DB, job *ReplicationJob) error {
	// Check for duplicate name
	var count int
	err := db.Get(&count, "SELECT COUNT(*) FROM replication_jobs WHERE name = ?", job.Name)
	if err != nil {
		return err
	}
	if count > 0 {
		return errors.New("a job with this name already exists")
	}

	query := `INSERT INTO replication_jobs (id, name, source_cluster_name, target_cluster_name, status, batch_size, parallelism, compression, preserve_partitions, created_at, updated_at)
              VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	_, err = db.Exec(query, job.ID, job.Name, job.SourceClusterName, job.TargetClusterName, job.Status, job.BatchSize, job.Parallelism, job.Compression, job.PreservePartitions, time.Now(), time.Now())
	return err
}

// UpdateJob updates an existing replication job in the database.
func UpdateJob(db *sqlx.DB, job *ReplicationJob) error {
	// Check for duplicate name
	var count int
	err := db.Get(&count, "SELECT COUNT(*) FROM replication_jobs WHERE name = ? AND id != ?", job.Name, job.ID)
	if err != nil {
		return err
	}
	if count > 0 {
		return errors.New("a job with this name already exists")
	}

	query := `UPDATE replication_jobs 
              SET name = ?, source_cluster_name = ?, target_cluster_name = ?, status = ?, failed_reason = ?, batch_size = ?, parallelism = ?, compression = ?, preserve_partitions = ?, updated_at = ?
              WHERE id = ?`
	_, err = db.Exec(query, job.Name, job.SourceClusterName, job.TargetClusterName, job.Status, job.FailedReason, job.BatchSize, job.Parallelism, job.Compression, job.PreservePartitions, time.Now(), job.ID)
	return err
}

// DeleteJob removes a replication job from the database.
func DeleteJob(db *sqlx.DB, id string) error {
	_, err := db.Exec("DELETE FROM replication_jobs WHERE id = ?", id)
	return err
}
