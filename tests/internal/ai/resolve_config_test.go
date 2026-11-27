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
	"testing"

	"github.com/scalytics/kaf-mirror/v2/internal/ai"
	"github.com/scalytics/kaf-mirror/v2/internal/config"
)

func TestResolveConfigPrefersFileWhenProviderTokenOrEndpointIsSet(t *testing.T) {
	db := &config.AIConfig{Provider: "openai", Token: "db-token", Model: "gpt-4"}
	file := config.AIConfig{Provider: "custom", Endpoint: "http://127.0.0.1:11434/v1", Token: "file-token"}
	got := ai.ResolveConfig(file, db)
	if got != file {
		t.Fatalf("got %+v, want file %+v", got, file)
	}

	file = config.AIConfig{Token: "env-token"}
	got = ai.ResolveConfig(file, db)
	if got.Token != "env-token" || got.Provider != "" {
		t.Fatalf("token in the file was not used: %+v", got)
	}
}

func TestResolveConfigUsesDatabaseWhenFileAIIsEmpty(t *testing.T) {
	db := &config.AIConfig{Provider: "claude", Token: "db-token"}
	got := ai.ResolveConfig(config.AIConfig{}, db)
	if got != *db {
		t.Fatalf("got %+v, want db %+v", got, *db)
	}
	got = ai.ResolveConfig(config.AIConfig{}, nil)
	if got != (config.AIConfig{}) {
		t.Fatalf("got %+v, want empty", got)
	}
}
