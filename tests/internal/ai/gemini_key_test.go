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
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/scalytics/kaf-mirror/v2/internal/ai"
)

const geminiTestKey = "AIzaSy-gemini-secret-key"

func TestGeminiSendsKeyInHeaderNotURL(t *testing.T) {
	var gotQuery, gotHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		gotHeader = r.Header.Get("x-goog-api-key")
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"candidates":[{"content":{"parts":[{"text":"ok"}]}}]}`))
	}))
	defer srv.Close()

	p := &ai.GeminiProvider{APIKey: geminiTestKey, Endpoint: srv.URL, Model: "gemini-pro", HTTP: srv.Client()}
	out, err := p.GetCompletion(context.Background(), "hi")
	if err != nil {
		t.Fatal(err)
	}
	if out != "ok" {
		t.Fatalf("completion %q, want ok", out)
	}
	if strings.Contains(gotQuery, geminiTestKey) {
		t.Fatalf("API key in the URL query: %q", gotQuery)
	}
	if gotHeader != geminiTestKey {
		t.Fatalf("x-goog-api-key header %q, want the key", gotHeader)
	}
}

func TestGeminiTransportErrorHasNoKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	endpoint := srv.URL
	srv.Close()

	p := &ai.GeminiProvider{APIKey: geminiTestKey, Endpoint: endpoint, Model: "gemini-pro", HTTP: &http.Client{}}
	_, err := p.GetCompletion(context.Background(), "hi")
	if err == nil {
		t.Fatal("want a connection error")
	}
	if strings.Contains(err.Error(), geminiTestKey) {
		t.Fatalf("error leaks the API key: %v", err)
	}
}
