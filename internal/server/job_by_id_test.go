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

package server

import (
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/scalytics/kaf-mirror/v2/internal/database"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestJobByIDDistinguishesMissingRowsFromDatabaseErrors(t *testing.T) {
	db, err := database.InitDB(":memory:")
	require.NoError(t, err)
	s := &Server{Db: db}
	app := fiber.New()
	app.Get("/jobs/:id", func(c *fiber.Ctx) error {
		_, err := s.jobByID(c)
		return err
	})

	id := uuid.NewString()
	resp, err := app.Test(httptest.NewRequest("GET", "/jobs/"+id, nil), -1)
	require.NoError(t, err)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, 404, resp.StatusCode, string(body))
	assert.Contains(t, strings.ToLower(string(body)), "job not found")

	require.NoError(t, db.Close())
	resp, err = app.Test(httptest.NewRequest("GET", "/jobs/"+id, nil), -1)
	require.NoError(t, err)
	body, err = io.ReadAll(resp.Body)
	require.NoError(t, err)
	text := string(body)
	lower := strings.ToLower(text)
	assert.Equal(t, 500, resp.StatusCode, text)
	assert.Contains(t, lower, "failed to get job")
	assert.NotContains(t, lower, "job not found")
	assert.NotContains(t, lower, "database is closed")
	assert.NotContains(t, lower, "sql:")
}
