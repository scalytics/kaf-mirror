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

package server_test

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/scalytics/kaf-mirror/v2/internal/database"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func mePermissions(t *testing.T, ctx *TestContext, token string) []string {
	t.Helper()
	req := httptest.NewRequest("GET", "/auth/me", nil)
	addAuthHeader(req, token)
	resp, err := ctx.Server.App.Test(req)
	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode)
	var me struct {
		Role        string   `json:"role"`
		Permissions []string `json:"permissions"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&me))
	require.NotNil(t, me.Permissions)
	return me.Permissions
}

func TestMeListsThePermissionsOfTheRole(t *testing.T) {
	ctx := setupTestServer(t)
	admin := mePermissions(t, ctx, ctx.Token)
	assert.Contains(t, admin, "compliance:generate")
	assert.Contains(t, admin, "jobs:edit")

	db := ctx.Server.Db
	user, err := database.CreateUser(db, "watcher", "watcher-password-1", false)
	require.NoError(t, err)
	var roleID int
	require.NoError(t, db.Get(&roleID, "SELECT id FROM roles WHERE name = 'monitoring'"))
	require.NoError(t, database.AssignRoleToUser(db, user.ID, roleID))
	token, _, err := database.CreateApiToken(db, user.ID, "watcher", time.Now().Add(time.Hour))
	require.NoError(t, err)

	monitoring := mePermissions(t, ctx, token)
	assert.Contains(t, monitoring, "jobs:view")
	assert.NotContains(t, monitoring, "compliance:generate")
	assert.NotContains(t, monitoring, "jobs:edit")
	assert.NotContains(t, monitoring, "users:list")
}
