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
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"kaf-mirror/internal/database"

	"github.com/stretchr/testify/require"
)

var rbacRoles = []string{"admin", "operator", "monitoring", "compliance"}

var rbacPathParams = strings.NewReplacer(
	":snapshot_id", "999999",
	":event_id", "999999",
	":username", "no-such-user",
	":period", "daily",
	":event", "no-such-event",
	":name", "no-such-cluster",
	":id", "999999",
)

func roleToken(t *testing.T, ctx *TestContext, role string) string {
	t.Helper()
	db := ctx.Server.Db
	user, err := database.CreateUser(db, "rbac-"+role, "rbac-password-123", false)
	require.NoError(t, err)
	var roleID int
	require.NoError(t, db.Get(&roleID, "SELECT id FROM roles WHERE name = ?", role))
	require.NoError(t, database.AssignRoleToUser(db, user.ID, roleID))
	token, _, err := database.CreateApiToken(db, user.ID, role, time.Now().Add(time.Hour))
	require.NoError(t, err)
	return token
}

func TestRBACDocMatchesThePermissionsInCode(t *testing.T) {
	raw, err := os.ReadFile("../../../docs/rbac.md")
	require.NoError(t, err)
	row := regexp.MustCompile("(?m)^\\| `(GET|POST|PUT|DELETE) (/[^`]*)` \\| (.+) \\|$")
	roleList := regexp.MustCompile("^`(admin|operator|monitoring|compliance)`(, `(admin|operator|monitoring|compliance)`)*$")

	ctx := setupTestServer(t)
	tokens := map[string]string{}
	for _, role := range rbacRoles {
		tokens[role] = roleToken(t, ctx, role)
	}

	checked := 0
	for _, m := range row.FindAllStringSubmatch(string(raw), -1) {
		method, path, cell := m[1], m[2], strings.TrimSpace(m[3])
		if !roleList.MatchString(cell) {
			continue
		}
		allowed := map[string]bool{}
		for _, r := range strings.Split(cell, ",") {
			allowed[strings.Trim(strings.TrimSpace(r), "`")] = true
		}
		target := rbacPathParams.Replace(path)
		for _, role := range rbacRoles {
			req := httptest.NewRequest(method, target, strings.NewReader("{}"))
			req.Header.Set("Content-Type", "application/json")
			addAuthHeader(req, tokens[role])
			resp, err := ctx.Server.App.Test(req, -1)
			require.NoError(t, err, "%s %s as %s", method, path, role)
			resp.Body.Close()
			denied := resp.StatusCode == 403
			if denied == allowed[role] {
				t.Errorf("docs/rbac.md: %s %s lists %s, but %s gets HTTP %d", method, path, cell, role, resp.StatusCode)
			}
		}
		checked++
	}
	if checked < 60 {
		t.Fatalf("checked only %d rows of docs/rbac.md", checked)
	}
}
