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

// SplunkSink sends metrics to a Splunk HTTP Event Collector (HEC).
type SplunkSink struct {
	client *http.Client
	cfg    config.SplunkConfig
}

// NewSplunkSink creates a new Splunk sink.
func NewSplunkSink(cfg config.SplunkConfig, allowedHosts []string) (*SplunkSink, error) {
	if err := egress.Check(cfg.HECEndpoint, allowedHosts); err != nil {
		return nil, err
	}
	return &SplunkSink{
		client: egress.HTTPClient(15*time.Second, allowedHosts),
		cfg:    cfg,
	}, nil
}

// Send sends a metric to Splunk.
func (s *SplunkSink) Send(metric database.ReplicationMetric) error {
	payload := map[string]interface{}{
		"event":      metric,
		"source":     "kaf-mirror",
		"sourcetype": "_json",
		"index":      s.cfg.Index,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequest("POST", s.cfg.HECEndpoint, bytes.NewBuffer(body))
	if err != nil {
		return err
	}

	req.Header.Set("Authorization", "Splunk "+s.cfg.HECToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to send metric to Splunk: %s", resp.Status)
	}

	return nil
}
