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
	"runtime"
	"strings"
	"testing"
	"time"

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

func protectionPollers() int {
	buf := make([]byte, 1<<20)
	n := runtime.Stack(buf, true)
	return strings.Count(string(buf[:n]), "(*Controller).pollGuard")
}

func waitForProtectionPollers(t *testing.T, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	var stable time.Time
	for time.Now().Before(deadline) {
		got := protectionPollers()
		if got > want {
			t.Fatalf("pollers = %d, want %d", got, want)
		}
		if got == want {
			if stable.IsZero() {
				stable = time.Now()
			} else if time.Since(stable) >= 200*time.Millisecond {
				return
			}
		} else {
			stable = time.Time{}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("pollers = %d, want %d", protectionPollers(), want)
}

func TestReplacedProtectionControllerStopsThePreviousPoller(t *testing.T) {
	before := protectionPollers()
	db, jm, _ := setupManagerTest(t)
	waitForProtectionPollers(t, before+1)
	useProtection(t, jm, db)
	waitForProtectionPollers(t, before+1)
}
