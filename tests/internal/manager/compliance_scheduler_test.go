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

package manager_test

import (
	"github.com/scalytics/kaf-mirror/v2/internal/manager"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestShouldRunComplianceReport_Daily(t *testing.T) {
	now := time.Date(2025, 2, 5, 2, 0, 0, 0, time.Local)
	lastRun := time.Date(2025, 2, 4, 2, 0, 0, 0, time.Local)

	assert.True(t, manager.ShouldRunComplianceReportForTest("daily", now, lastRun, 2))
	assert.False(t, manager.ShouldRunComplianceReportForTest("daily", now, now, 2))
	assert.False(t, manager.ShouldRunComplianceReportForTest("daily", now, lastRun, 3))
}

func TestShouldRunComplianceReport_Weekly(t *testing.T) {
	// Monday run
	now := time.Date(2025, 2, 3, 2, 0, 0, 0, time.Local)
	lastWeek := time.Date(2025, 1, 27, 2, 0, 0, 0, time.Local)
	sameWeek := time.Date(2025, 2, 3, 2, 0, 0, 0, time.Local)

	assert.True(t, manager.ShouldRunComplianceReportForTest("weekly", now, lastWeek, 2))
	assert.False(t, manager.ShouldRunComplianceReportForTest("weekly", now, sameWeek, 2))

	// Not Monday
	tuesday := time.Date(2025, 2, 4, 2, 0, 0, 0, time.Local)
	assert.False(t, manager.ShouldRunComplianceReportForTest("weekly", tuesday, lastWeek, 2))
}

func TestShouldRunComplianceReport_Monthly(t *testing.T) {
	now := time.Date(2025, 3, 1, 2, 0, 0, 0, time.Local)
	lastMonth := time.Date(2025, 2, 1, 2, 0, 0, 0, time.Local)
	sameMonth := time.Date(2025, 3, 1, 2, 0, 0, 0, time.Local)

	assert.True(t, manager.ShouldRunComplianceReportForTest("monthly", now, lastMonth, 2))
	assert.False(t, manager.ShouldRunComplianceReportForTest("monthly", now, sameMonth, 2))

	// Not first day
	notFirst := time.Date(2025, 3, 2, 2, 0, 0, 0, time.Local)
	assert.False(t, manager.ShouldRunComplianceReportForTest("monthly", notFirst, lastMonth, 2))
}
