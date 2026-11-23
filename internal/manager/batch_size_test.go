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

package manager

import (
	"testing"

	"kaf-mirror/internal/database"
)

func TestBuildJobConfigUsesTheDefaultForAStoredZeroBatchSize(t *testing.T) {
	jm := &JobManager{}
	src := &database.KafkaCluster{Name: "src", Brokers: "localhost:9092", SecurityConfig: "{}"}
	tgt := &database.KafkaCluster{Name: "tgt", Brokers: "localhost:9093", SecurityConfig: "{}"}
	for stored, want := range map[int]int{0: database.DefaultBatchSizeKB, -3: database.DefaultBatchSizeKB, 64: 64} {
		cfg, err := jm.buildJobConfig(src, tgt, nil, &database.ReplicationJob{ID: "job-67", BatchSize: stored})
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Replication.BatchSize != want {
			t.Fatalf("stored batch_size %d gave %d, want %d", stored, cfg.Replication.BatchSize, want)
		}
	}
}

func TestThrottlingKeepsBatchSizeInTheValidRange(t *testing.T) {
	jm := &JobManager{}
	cases := []struct {
		current   int
		direction string
		want      int
	}{
		{100, "decrease", 100},
		{50, "decrease", 50},
		{1000, "decrease", 700},
		{10000, "increase", 10000},
		{9000, "increase", 10000},
		{1, "increase", 501},
		{1000, "increase", 1500},
		{1000, "optimize", 1300},
		{4800, "optimize", 5000},
		{5000, "optimize", 5000},
		{10000, "optimize", 10000},
	}
	for _, c := range cases {
		got := jm.calculateOptimalBatchSize(c.current, 100, 10, c.direction)
		if got != c.want {
			t.Fatalf("%s from %d gave %d, want %d", c.direction, c.current, got, c.want)
		}
		if _, err := database.ResolveBatchSize(got); err != nil {
			t.Fatalf("%s from %d gave invalid batch_size %d", c.direction, c.current, got)
		}
	}
	current := 10000
	for range 50 {
		current = jm.calculateOptimalBatchSize(current, 100, 10, "increase")
	}
	if current != 10000 {
		t.Fatalf("repeated increases reached %d, want 10000", current)
	}
	if got := jm.calculateOptimalBatchSize(200, 10, 10, "optimize"); got != 100 {
		t.Fatalf("optimize from 200 at low throughput gave %d, want 100", got)
	}
	if got := jm.calculateOptimalBatchSize(102400, 10, 10, "optimize"); got != 102400 {
		t.Fatalf("optimize lowered a user batch_size of 102400 to %d", got)
	}
}
