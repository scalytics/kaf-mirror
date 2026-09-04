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

package metrics

import (
	"bytes"
	"encoding/json"
	"fmt"
	"kaf-mirror/internal/config"
	"kaf-mirror/internal/database"
	"kaf-mirror/internal/egress"
	"net/http"
	"time"
)

// LokiSink sends metrics to a Grafana Loki instance.
type LokiSink struct {
	client *http.Client
	cfg    config.LokiConfig
}

// NewLokiSink creates a new Loki sink.
func NewLokiSink(cfg config.LokiConfig, allowedHosts []string) (*LokiSink, error) {
	if err := egress.Check(cfg.Endpoint, allowedHosts); err != nil {
		return nil, err
	}
	return &LokiSink{
		client: egress.HTTPClient(15*time.Second, allowedHosts),
		cfg:    cfg,
	}, nil
}

// Send sends a metric to Loki.
func (s *LokiSink) Send(metric database.ReplicationMetric) error {
	logEntry := map[string]interface{}{
		"streams": []map[string]interface{}{
			{
				"stream": map[string]string{
					"job_id": metric.JobID,
				},
				"values": [][]string{
					{
						fmt.Sprintf("%d", time.Now().UnixNano()),
						fmt.Sprintf("messages_replicated=%d bytes_transferred=%d messages_consumed=%d bytes_consumed=%d current_lag=%d error_count=%d source_stalled=%t target_stalled=%t critical_lag=%t high_error_rate=%t error_spike=%t",
							metric.MessagesReplicated,
							metric.BytesTransferred,
							metric.MessagesConsumed,
							metric.BytesConsumed,
							metric.CurrentLag,
							metric.ErrorCount,
							metric.SourceStalled,
							metric.TargetStalled,
							metric.CriticalLag,
							metric.HighErrorRate,
							metric.ErrorSpike,
						),
					},
				},
			},
		},
	}

	body, err := json.Marshal(logEntry)
	if err != nil {
		return err
	}

	req, err := http.NewRequest("POST", s.cfg.Endpoint, bytes.NewBuffer(body))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("failed to send metric to Loki: %s", resp.Status)
	}

	return nil
}
