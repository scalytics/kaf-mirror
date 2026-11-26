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
	"testing"

	"kaf-mirror/internal/config"
	"kaf-mirror/internal/manager"
	"kaf-mirror/internal/protection"

	"github.com/jmoiron/sqlx"
)

func useProtection(t *testing.T, jm *manager.JobManager, db *sqlx.DB) {
	t.Helper()
	if jm.Protection != nil {
		jm.Protection.Close()
	}
	jm.Protection = protection.New(db, config.ProtectionConfig{AutoHalt: config.AutoHaltConfig{Enabled: true}})
}
