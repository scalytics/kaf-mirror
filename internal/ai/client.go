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

package ai

import (
	"context"
	"fmt"
	"kaf-mirror/internal/config"
	"kaf-mirror/internal/egress"
	"net/http"
	"strings"
	"time"

	"github.com/sashabaranov/go-openai"
)

// CompletionProvider abstracts a text completion backend.
type CompletionProvider interface {
	GetCompletion(context.Context, string) (string, error)
}

type openAIProvider struct {
	client OpenAIClient
	model  string
}

func (p *openAIProvider) GetCompletion(ctx context.Context, prompt string) (string, error) {
	resp, err := p.client.CreateChatCompletion(
		ctx,
		openai.ChatCompletionRequest{
			Model: p.model,
			Messages: []openai.ChatCompletionMessage{
				{
					Role:    openai.ChatMessageRoleUser,
					Content: prompt,
				},
			},
		},
	)
	if err != nil {
		return "", err
	}
	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("no completion choices returned")
	}
	return resp.Choices[0].Message.Content, nil
}

// Client is a wrapper for the AI provider.
type Client struct {
	Provider CompletionProvider
	Cfg      config.AIConfig
}

// NewClient creates a new AI client.
func NewClient(cfg config.AIConfig, allowedHosts []string) *Client {
	return &Client{
		Provider: buildProvider(cfg, allowedHosts),
		Cfg:      cfg,
	}
}

// NewClientWithProvider creates a client with a custom provider (for tests).
func NewClientWithProvider(cfg config.AIConfig, provider CompletionProvider) *Client {
	return &Client{
		Provider: provider,
		Cfg:      cfg,
	}
}

func buildProvider(cfg config.AIConfig, allowedHosts []string) CompletionProvider {
	httpClient := egress.HTTPClient(30*time.Second, allowedHosts)
	switch strings.ToLower(cfg.Provider) {
	case "claude":
		return &ClaudeProvider{
			APIKey:   cfg.Token,
			Endpoint: cfg.Endpoint,
			Model:    cfg.Model,
			HTTP:     httpClient,
		}
	case "gemini":
		return &GeminiProvider{
			APIKey:   cfg.Token,
			Endpoint: cfg.Endpoint,
			Model:    cfg.Model,
			HTTP:     httpClient,
		}
	case "grok":
		return &GrokProvider{
			APIKey:   cfg.Token,
			Endpoint: cfg.Endpoint,
			Model:    cfg.Model,
			HTTP:     httpClient,
		}
	case "custom", "openai", "":
		fallthrough
	default:
		openaiCfg := openai.DefaultConfig(cfg.Token)
		if cfg.Endpoint != "" {
			openaiCfg.BaseURL = cfg.Endpoint
		}
		openaiCfg.HTTPClient = httpClient
		if cfg.APISecret != "" {
			openaiCfg.HTTPClient = newHeaderHTTPClient(httpClient, map[string]string{
				"X-API-Secret": cfg.APISecret,
				"X-API-Key":    cfg.Token,
			})
		}
		return &openAIProvider{
			client: openai.NewClientWithConfig(openaiCfg),
			model:  cfg.Model,
		}
	}
}

// GetAnomalyDetection analyzes metrics for unusual patterns.
func (c *Client) GetAnomalyDetection(ctx context.Context, metrics string) (string, error) {
	prompt := fmt.Sprintf(`
You are an expert Kafka monitoring assistant. Analyze the following time-series metrics from a Kafka kaf-mirror job and identify any potential anomalies.
Look for sudden spikes or drops in message throughput, unusual increases in lag, or a high error rate.
Provide a concise summary of any anomalies found. If no anomalies are detected, state that the system appears stable.

Metrics:
%s
`, metrics)
	return c.getCompletion(ctx, prompt)
}

// GetAnomalyDetectionWithResponseTime analyzes metrics for unusual patterns and returns response time.
func (c *Client) GetAnomalyDetectionWithResponseTime(ctx context.Context, metrics string) (string, int, error) {
	prompt := fmt.Sprintf(`
You are an expert Kafka monitoring assistant. Analyze the following time-series metrics from a Kafka kaf-mirror job and identify any potential anomalies.
Look for sudden spikes or drops in message throughput, unusual increases in lag, or a high error rate.
Provide a concise summary of any anomalies found. If no anomalies are detected, state that the system appears stable.

Metrics:
%s
`, metrics)
	return c.getCompletionWithResponseTime(ctx, prompt)
}

// GetPerformanceRecommendation provides a performance recommendation based on current metrics.
func (c *Client) GetPerformanceRecommendation(ctx context.Context, metrics string) (string, error) {
	prompt := fmt.Sprintf(`
You are an expert Kafka performance tuning assistant. Based on the following metrics from a Kafka kaf-mirror job, provide a concrete recommendation for optimization.
Consider parameters like batch size, parallelism, and compression. Explain your reasoning.

Metrics:
%s
`, metrics)
	return c.getCompletion(ctx, prompt)
}

// GetPerformanceRecommendationWithResponseTime provides a performance recommendation and returns response time.
func (c *Client) GetPerformanceRecommendationWithResponseTime(ctx context.Context, metrics string) (string, int, error) {
	prompt := fmt.Sprintf(`
You are an expert Kafka performance tuning assistant. Based on the following metrics from a Kafka kaf-mirror job, provide a concrete recommendation for optimization.
Consider parameters like batch size, parallelism, and compression. Explain your reasoning.

Metrics:
%s
`, metrics)
	return c.getCompletionWithResponseTime(ctx, prompt)
}

// ExplainEvent provides an explanation for a given event.
func (c *Client) ExplainEvent(ctx context.Context, event string) (string, error) {
	prompt := "Explain the likely cause of this Kafka event:\n" + event
	return c.getCompletion(ctx, prompt)
}

// GetIncidentAnalysis provides a detailed analysis of an operational incident.
func (c *Client) GetIncidentAnalysis(ctx context.Context, eventDetails string) (string, error) {
	prompt := fmt.Sprintf(`
You are a senior Site Reliability Engineer (SRE) specializing in Kafka. An operational event has occurred in the kaf-mirror replication system.
Analyze the following event details and provide a full incident report.

The report must include:
1.  **Root Cause Analysis:** What is the most likely technical cause of this event?
2.  **Impact Assessment:** What is the potential impact on data replication, system stability, and data integrity?
3.  **Recommended Mitigation:** What are the immediate, concrete steps that should be taken to resolve this issue?
4.  **Preventative Measures:** What long-term changes could be made to prevent this type of incident in the future?

Event Details:
%s
`, eventDetails)
	return c.getCompletion(ctx, prompt)
}

// GetIncidentAnalysisWithResponseTime provides a detailed analysis of an operational incident and returns response time.
func (c *Client) GetIncidentAnalysisWithResponseTime(ctx context.Context, eventDetails string) (string, int, error) {
	prompt := fmt.Sprintf(`
You are a senior Site Reliability Engineer (SRE) specializing in Kafka. An operational event has occurred in the kaf-mirror replication system.
Analyze the following event details and provide a full incident report.

The report must include:
1.  **Root Cause Analysis:** What is the most likely technical cause of this event?
2.  **Impact Assessment:** What is the potential impact on data replication, system stability, and data integrity?
3.  **Recommended Mitigation:** What are the immediate, concrete steps that should be taken to resolve this issue?
4.  **Preventative Measures:** What long-term changes could be made to prevent this type of incident in the future?

Event Details:
%s
`, eventDetails)
	return c.getCompletionWithResponseTime(ctx, prompt)
}

// GetEnhancedInsights analyzes both metrics and operation logs together for comprehensive insights.
func (c *Client) GetEnhancedInsights(ctx context.Context, metrics string, logs string) (string, error) {
	prompt := fmt.Sprintf(`
You are an expert Kafka operations analyst with deep experience in high-throughput streaming systems. 
Analyze the following metrics and operational logs from a Kafka kaf-mirror replication job to provide comprehensive insights.

Consider both the quantitative metrics and qualitative log patterns to identify:
1. **Performance Issues**: Throughput bottlenecks, latency spikes, resource constraints
2. **Error Patterns**: Recurring failures, connection issues, authentication problems
3. **Operational Health**: System stability, error recovery patterns, configuration issues
4. **Optimization Opportunities**: Performance tuning recommendations, configuration adjustments
5. **Predictive Insights**: Potential future issues based on current trends

Provide specific, actionable recommendations with priority levels (Critical, High, Medium, Low).

METRICS DATA:
%s

OPERATION LOGS:
%s

Focus on correlating metrics patterns with log events to provide deeper insights than either data source alone.
`, metrics, logs)
	return c.getCompletion(ctx, prompt)
}

// GetEnhancedInsightsWithResponseTime analyzes both metrics and operation logs and returns response time.
func (c *Client) GetEnhancedInsightsWithResponseTime(ctx context.Context, metrics string, logs string) (string, int, error) {
	prompt := fmt.Sprintf(`
You are an expert Kafka operations analyst with deep experience in high-throughput streaming systems. 
Analyze the following metrics and operational logs from a Kafka kaf-mirror replication job to provide comprehensive insights.

Consider both the quantitative metrics and qualitative log patterns to identify:
1. **Performance Issues**: Throughput bottlenecks, latency spikes, resource constraints
2. **Error Patterns**: Recurring failures, connection issues, authentication problems
3. **Operational Health**: System stability, error recovery patterns, configuration issues
4. **Optimization Opportunities**: Performance tuning recommendations, configuration adjustments
5. **Predictive Insights**: Potential future issues based on current trends

Provide specific, actionable recommendations with priority levels (Critical, High, Medium, Low).

METRICS DATA:
%s

OPERATION LOGS:
%s

Focus on correlating metrics patterns with log events to provide deeper insights than either data source alone.
`, metrics, logs)
	return c.getCompletionWithResponseTime(ctx, prompt)
}

// GetLogPatternAnalysis analyzes operation logs for patterns and anomalies.
func (c *Client) GetLogPatternAnalysis(ctx context.Context, logs string) (string, error) {
	prompt := fmt.Sprintf(`
You are a Kafka operations expert analyzing system logs for patterns and anomalies.
Review the following producer and consumer operation logs to identify:

1. **Error Patterns**: Recurring errors, failure sequences, escalation patterns
2. **Performance Indicators**: Batch processing efficiency, connection stability
3. **Configuration Issues**: Authentication problems, network connectivity issues
4. **Recovery Patterns**: How the system handles and recovers from errors
5. **Operational Insights**: System behavior patterns, resource utilization indicators

Provide clear, actionable insights with specific recommendations for improvements.

OPERATION LOGS:
%s
`, logs)
	return c.getCompletion(ctx, prompt)
}

// GetLogPatternAnalysisWithResponseTime analyzes operation logs and returns response time.
func (c *Client) GetLogPatternAnalysisWithResponseTime(ctx context.Context, logs string) (string, int, error) {
	prompt := fmt.Sprintf(`
You are a Kafka operations expert analyzing system logs for patterns and anomalies.
Review the following producer and consumer operation logs to identify:

1. **Error Patterns**: Recurring errors, failure sequences, escalation patterns
2. **Performance Indicators**: Batch processing efficiency, connection stability
3. **Configuration Issues**: Authentication problems, network connectivity issues
4. **Recovery Patterns**: How the system handles and recovers from errors
5. **Operational Insights**: System behavior patterns, resource utilization indicators

Provide clear, actionable insights with specific recommendations for improvements.

OPERATION LOGS:
%s
`, logs)
	return c.getCompletionWithResponseTime(ctx, prompt)
}

func (c *Client) getCompletion(ctx context.Context, prompt string) (string, error) {
	return c.Provider.GetCompletion(ctx, prompt)
}

// getCompletionWithResponseTime returns both the completion and the response time in milliseconds
func (c *Client) getCompletionWithResponseTime(ctx context.Context, prompt string) (string, int, error) {
	startTime := time.Now()

	resp, err := c.Provider.GetCompletion(ctx, prompt)
	responseTime := int(time.Since(startTime).Milliseconds())
	if err != nil {
		return "", responseTime, err
	}
	return resp, responseTime, nil
}

type headerRoundTripper struct {
	base    http.RoundTripper
	headers map[string]string
}

func (h *headerRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	for key, value := range h.headers {
		if req.Header.Get(key) == "" {
			req.Header.Set(key, value)
		}
	}
	return h.base.RoundTrip(req)
}

func newHeaderHTTPClient(base *http.Client, headers map[string]string) *http.Client {
	rt := http.DefaultTransport
	timeout := 30 * time.Second
	if base != nil {
		timeout = base.Timeout
		if base.Transport != nil {
			rt = base.Transport
		}
	}
	return &http.Client{
		Timeout: timeout,
		Transport: &headerRoundTripper{
			base:    rt,
			headers: headers,
		},
	}
}
