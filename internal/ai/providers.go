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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"
	"time"
)

// ClaudeProvider implements Claude AI integration
type ClaudeProvider struct {
	APIKey   string
	Endpoint string
	Model    string
	HTTP     *http.Client
}

// GeminiProvider implements Google Gemini integration
type GeminiProvider struct {
	APIKey   string
	Endpoint string
	Model    string
	HTTP     *http.Client
}

// GrokProvider implements xAI Grok integration
type GrokProvider struct {
	APIKey   string
	Endpoint string
	Model    string
	HTTP     *http.Client
}

// ClaudeRequest represents a Claude API request
type ClaudeRequest struct {
	Model     string          `json:"model"`
	MaxTokens int             `json:"max_tokens"`
	Messages  []ClaudeMessage `json:"messages"`
}

type ClaudeMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ClaudeResponse struct {
	Content []struct {
		Text string `json:"text"`
	} `json:"content"`
}

// GeminiRequest represents a Gemini API request
type GeminiRequest struct {
	Contents []GeminiContent `json:"contents"`
}

type GeminiContent struct {
	Parts []GeminiPart `json:"parts"`
}

type GeminiPart struct {
	Text string `json:"text"`
}

type GeminiResponse struct {
	Candidates []struct {
		Content struct {
			Parts []struct {
				Text string `json:"text"`
			} `json:"parts"`
		} `json:"content"`
	} `json:"candidates"`
}

// GetCompletion implements AI completion for Claude
func (c *ClaudeProvider) GetCompletion(ctx context.Context, prompt string) (string, error) {
	endpoint := normalizeClaudeEndpoint(c.Endpoint)
	reqBody := ClaudeRequest{
		Model:     c.Model,
		MaxTokens: 2048,
		Messages: []ClaudeMessage{
			{Role: "user", Content: prompt},
		},
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewBuffer(jsonData))
	if err != nil {
		return "", err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", c.APIKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := httpClient(c.HTTP).Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Claude API error %d: %s", resp.StatusCode, string(body))
	}

	var response ClaudeResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return "", err
	}

	if len(response.Content) > 0 {
		return response.Content[0].Text, nil
	}

	return "", fmt.Errorf("no response content from Claude")
}

// GetCompletion implements AI completion for Gemini
func (g *GeminiProvider) GetCompletion(ctx context.Context, prompt string) (string, error) {
	endpoint := normalizeGeminiEndpoint(g.Endpoint, g.Model)
	reqBody := GeminiRequest{
		Contents: []GeminiContent{
			{
				Parts: []GeminiPart{
					{Text: prompt},
				},
			},
		},
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}

	url := fmt.Sprintf("%s?key=%s", endpoint, g.APIKey)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return "", err
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient(g.HTTP).Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Gemini API error %d: %s", resp.StatusCode, string(body))
	}

	var response GeminiResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return "", err
	}

	if len(response.Candidates) > 0 && len(response.Candidates[0].Content.Parts) > 0 {
		return response.Candidates[0].Content.Parts[0].Text, nil
	}

	return "", fmt.Errorf("no response content from Gemini")
}

// GetCompletion implements AI completion for Grok (using OpenAI-compatible API)
func (g *GrokProvider) GetCompletion(ctx context.Context, prompt string) (string, error) {
	endpoint := normalizeChatCompletionsEndpoint(g.Endpoint, "https://api.x.ai/v1/chat/completions")
	reqBody := map[string]interface{}{
		"model": g.Model,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
		"max_tokens": 2048,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewBuffer(jsonData))
	if err != nil {
		return "", err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+g.APIKey)

	resp, err := httpClient(g.HTTP).Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Grok API error %d: %s", resp.StatusCode, string(body))
	}

	var response struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}

	if err := json.Unmarshal(body, &response); err != nil {
		return "", err
	}

	if len(response.Choices) > 0 {
		return response.Choices[0].Message.Content, nil
	}

	return "", fmt.Errorf("no response content from Grok")
}

func httpClient(c *http.Client) *http.Client {
	if c != nil {
		return c
	}
	return &http.Client{Timeout: 30 * time.Second}
}

func normalizeClaudeEndpoint(endpoint string) string {
	if endpoint == "" {
		return "https://api.anthropic.com/v1/messages"
	}
	trimmed := strings.TrimSuffix(endpoint, "/")
	if strings.Contains(trimmed, "/messages") {
		return trimmed
	}
	return trimmed + "/messages"
}

func normalizeGeminiEndpoint(endpoint, model string) string {
	if model == "" {
		model = "gemini-pro"
	}
	if endpoint == "" {
		endpoint = "https://generativelanguage.googleapis.com/v1beta"
	}
	trimmed := strings.TrimSuffix(endpoint, "/")
	if strings.Contains(trimmed, ":generateContent") {
		return trimmed
	}
	return fmt.Sprintf("%s/models/%s:generateContent", trimmed, model)
}

func normalizeChatCompletionsEndpoint(endpoint, fallback string) string {
	if endpoint == "" {
		return fallback
	}
	trimmed := strings.TrimSuffix(endpoint, "/")
	if strings.HasSuffix(trimmed, "/chat/completions") {
		return trimmed
	}
	return trimmed + "/chat/completions"
}
