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

package ai_test

import (
	"context"
	"github.com/scalytics/kaf-mirror/v2/internal/ai"
	"github.com/scalytics/kaf-mirror/v2/internal/config"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAIClient(t *testing.T) {
	mock := &fakeProvider{response: "Test response"}

	client := ai.NewClientWithProvider(config.AIConfig{Model: "test-model"}, mock)

	t.Run("GetAnomalyDetection", func(t *testing.T) {
		resp, err := client.GetAnomalyDetection(context.Background(), "some metrics")
		assert.NoError(t, err)
		assert.Equal(t, "Test response", resp)
	})

	t.Run("GetPerformanceRecommendation", func(t *testing.T) {
		resp, err := client.GetPerformanceRecommendation(context.Background(), "some metrics")
		assert.NoError(t, err)
		assert.Equal(t, "Test response", resp)
	})

	t.Run("ExplainEvent", func(t *testing.T) {
		resp, err := client.ExplainEvent(context.Background(), "some event")
		assert.NoError(t, err)
		assert.Equal(t, "Test response", resp)
	})
}

type fakeProvider struct {
	response string
}

func (f *fakeProvider) GetCompletion(ctx context.Context, prompt string) (string, error) {
	return f.response, nil
}
