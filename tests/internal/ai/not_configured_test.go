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
	"errors"
	"strings"
	"testing"

	"kaf-mirror/internal/ai"
	"kaf-mirror/internal/config"
)

func TestUnconfiguredAIMakesNoCalls(t *testing.T) {
	cases := []config.AIConfig{
		{},
		{Token: "sk-test", Model: "gpt-4"},
		{Provider: "  ", Token: "sk-test"},
		{Provider: "custom", Token: "sk-test"},
		{Provider: "unknown", Token: "sk-test", Endpoint: "https://ai.example.com/v1"},
	}
	for _, cfg := range cases {
		client := ai.NewClient(cfg, nil)
		if client.Enabled() {
			t.Fatalf("provider %q endpoint %q: client is enabled", cfg.Provider, cfg.Endpoint)
		}
		if client.Provider != nil {
			t.Fatalf("provider %q endpoint %q: got a provider %T, want none", cfg.Provider, cfg.Endpoint, client.Provider)
		}
		if !strings.HasPrefix(ai.Status(cfg), "off: ") {
			t.Fatalf("provider %q: status %q, want off", cfg.Provider, ai.Status(cfg))
		}
		if _, err := client.GetAnomalyDetection(context.Background(), "m"); !errors.Is(err, ai.ErrNotConfigured) {
			t.Fatalf("GetAnomalyDetection error %v, want ErrNotConfigured", err)
		}
		if _, _, err := client.GetEnhancedInsightsWithResponseTime(context.Background(), "m", "l"); !errors.Is(err, ai.ErrNotConfigured) {
			t.Fatalf("GetEnhancedInsightsWithResponseTime error %v, want ErrNotConfigured", err)
		}
	}
}

func TestUnconfiguredAIStatusSaysWhy(t *testing.T) {
	cases := map[string]config.AIConfig{
		"off: ai.provider is not set":               {},
		"off: ai.provider custom needs ai.endpoint": {Provider: "custom"},
		`off: unknown ai.provider "foo"`:            {Provider: "foo"},
		"on: openai":                                {Provider: "OpenAI"},
		"on: custom":                                {Provider: "custom", Endpoint: "https://ai.example.com/v1"},
		"on: claude":                                {Provider: "claude"},
	}
	for want, cfg := range cases {
		if got := ai.Status(cfg); got != want {
			t.Errorf("Status(%+v) = %q, want %q", cfg, got, want)
		}
	}
}

func TestConfiguredAIProviderIsEnabled(t *testing.T) {
	for _, provider := range []string{"openai", "claude", "gemini", "grok"} {
		if !ai.NewClient(config.AIConfig{Provider: provider}, nil).Enabled() {
			t.Errorf("provider %s: client is not enabled", provider)
		}
	}
	if !ai.NewClient(config.AIConfig{Provider: "custom", Endpoint: "https://ai.example.com/v1"}, nil).Enabled() {
		t.Error("custom provider with an endpoint: client is not enabled")
	}
}

func TestPerformancePromptHasNoRemovedJobFields(t *testing.T) {
	provider := &recordingProvider{}
	client := ai.NewClientWithProvider(config.AIConfig{Provider: "custom"}, provider)
	if _, err := client.GetPerformanceRecommendation(context.Background(), "m"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := client.GetPerformanceRecommendationWithResponseTime(context.Background(), "m"); err != nil {
		t.Fatal(err)
	}
	for _, prompt := range provider.prompts {
		lower := strings.ToLower(prompt)
		if strings.Contains(lower, "parallelism") || strings.Contains(lower, "preserve_partitions") {
			t.Fatalf("prompt mentions a removed job field: %s", prompt)
		}
	}
}

type recordingProvider struct {
	prompts []string
}

func (r *recordingProvider) GetCompletion(ctx context.Context, prompt string) (string, error) {
	r.prompts = append(r.prompts, prompt)
	return "ok", nil
}
