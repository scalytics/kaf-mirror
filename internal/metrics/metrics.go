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
	"kaf-mirror/internal/config"
	"kaf-mirror/internal/database"
)

// Sink is an interface for sending metrics to a monitoring platform.
type Sink interface {
	Send(metric database.ReplicationMetric) error
}

// NewSink creates a new metrics sink based on the provided configuration.
func NewSink(cfg config.MonitoringConfig, allowedHosts []string) (Sink, error) {
	if !cfg.Enabled {
		return nil, nil
	}

	switch cfg.Platform {
	case "splunk":
		return NewSplunkSink(cfg.Splunk, allowedHosts)
	case "loki":
		return NewLokiSink(cfg.Loki, allowedHosts)
	case "prometheus":
		return NewPrometheusSink(cfg.Prometheus, allowedHosts)
	default:
		return nil, nil
	}
}
