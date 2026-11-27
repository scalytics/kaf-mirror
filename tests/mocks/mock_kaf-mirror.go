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

package mocks

import (
	"github.com/scalytics/kaf-mirror/v2/internal/database"
	"github.com/scalytics/kaf-mirror/v2/internal/kafka"
)

// MockKafMirror is a mock implementation of the KafMirror interface.
type MockKafMirror struct {
	StartFunc func(jobID string, metricsCallback func(database.ReplicationMetric), onPanic func(jobID string, reason string))
	StopFunc  func()
}

// Start calls the mock StartFunc.
func (m *MockKafMirror) Start(jobID string, metricsCallback func(database.ReplicationMetric), onPanic func(jobID string, reason string)) {
	if m.StartFunc != nil {
		m.StartFunc(jobID, metricsCallback, onPanic)
	}
}

// Stop calls the mock StopFunc.
func (m *MockKafMirror) Stop() {
	if m.StopFunc != nil {
		m.StopFunc()
	}
}

// GetConsumer is a mock implementation.
func (m *MockKafMirror) GetConsumer() *kafka.Consumer {
	return nil
}

// GetProducer is a mock implementation.
func (m *MockKafMirror) GetProducer() *kafka.Producer {
	return nil
}
