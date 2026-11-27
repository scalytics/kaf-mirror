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
	"github.com/scalytics/kaf-mirror/v2/internal/database"

	"github.com/gofiber/fiber/v2"
)

func jsonList[T any](c *fiber.Ctx, items []T) error {
	if items == nil {
		items = []T{}
	}
	return c.JSON(items)
}

func jsonMap[K comparable, V any](c *fiber.Ctx, items map[K]V) error {
	if items == nil {
		items = map[K]V{}
	}
	return c.JSON(items)
}

func withEmptyStateLists(d *database.MirrorStateData) *database.MirrorStateData {
	if d == nil {
		return d
	}
	if d.MirrorProgress == nil {
		d.MirrorProgress = []database.MirrorProgress{}
	}
	if d.ResumePoints == nil {
		d.ResumePoints = []database.ResumePoint{}
	}
	if d.MirrorGaps == nil {
		d.MirrorGaps = []database.MirrorGap{}
	}
	if d.StateAnalysis == nil {
		d.StateAnalysis = []database.MirrorStateAnalysis{}
	}
	return d
}
