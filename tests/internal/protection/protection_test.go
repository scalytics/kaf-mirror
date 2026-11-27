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

package protection_test

import (
	"crypto/rand"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/scalytics/kaf-mirror/v2/internal/config"
	"github.com/scalytics/kaf-mirror/v2/internal/database"
	"github.com/scalytics/kaf-mirror/v2/internal/protection"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGuardStart(t *testing.T) {
	db, err := database.InitDB(":memory:")
	require.NoError(t, err)
	defer db.Close()

	ctrl := protection.New(db, config.ProtectionConfig{AutoHalt: config.AutoHaltConfig{Enabled: true}})
	assert.NoError(t, ctrl.GuardStart(protection.RoleProd))

	require.NoError(t, ctrl.SetEnabled(true))
	assert.Error(t, ctrl.GuardStart(protection.RoleProd))
	assert.NoError(t, ctrl.GuardStart(protection.RoleDR))

	require.NoError(t, ctrl.Halt("ransom", "admin"))
	assert.Error(t, ctrl.GuardStart(protection.RoleDR))
	require.NoError(t, ctrl.Resume())
	assert.NoError(t, ctrl.GuardStart(protection.RoleDR))
}

func TestHaltFileTripwire(t *testing.T) {
	db, err := database.InitDB(":memory:")
	require.NoError(t, err)
	defer db.Close()

	dir := t.TempDir()
	haltPath := filepath.Join(dir, "HALT")
	ctrl := protection.New(db, config.ProtectionConfig{HaltFile: haltPath})
	require.NoError(t, ctrl.SetEnabled(true))
	assert.False(t, ctrl.Halted())

	require.NoError(t, os.WriteFile(haltPath, []byte("stop"), 0600))
	assert.True(t, ctrl.Halted())
	st := ctrl.Status()
	assert.True(t, st.FileTripwire)
	assert.True(t, st.Experimental)
	require.Eventually(t, func() bool {
		return ctrl.GuardStart(protection.RoleDR) != nil
	}, 2*time.Second, 10*time.Millisecond)
}

func TestAutoHaltTombstoneStorm(t *testing.T) {
	db, err := database.InitDB(":memory:")
	require.NoError(t, err)
	defer db.Close()

	ctrl := protection.New(db, config.ProtectionConfig{
		AutoHalt: config.AutoHaltConfig{Enabled: true, TombstoneMin: 10, TombstoneRatio: 0.5},
	})
	require.NoError(t, ctrl.SetEnabled(true))
	assert.Equal(t, "", ctrl.Observe(protection.Sample{Consumed: 0, Tombstones: 0}))
	reason := ctrl.Observe(protection.Sample{Consumed: 20, Tombstones: 15})
	assert.Contains(t, reason, "tombstone")
}

func TestAutoHaltEntropyAndKeyRewrite(t *testing.T) {
	db, err := database.InitDB(":memory:")
	require.NoError(t, err)
	defer db.Close()

	ctrl := protection.New(db, config.ProtectionConfig{
		AutoHalt: config.AutoHaltConfig{Enabled: true},
	})
	require.NoError(t, ctrl.SetEnabled(true))

	jsonVal := []byte(`{"user":"alice","status":"active","city":"berlin","ok":true}`)
	for i := 0; i < 25; i++ {
		key := []byte{byte(i % 20)}
		assert.Equal(t, "", ctrl.Ingest("events", key, jsonVal))
	}

	enc := make([]byte, 256)
	_, err = rand.Read(enc)
	require.NoError(t, err)
	var reason string
	for i := 0; i < 50; i++ {
		key := []byte{byte(i % 20)}
		reason = ctrl.Ingest("events", key, enc)
		if reason != "" {
			break
		}
	}
	t.Logf("status=%+v reason=%q", ctrl.Status(), reason)
	assert.Contains(t, reason, "high-entropy")
}

func TestAutoHaltEntropyIgnoredWhenDisabled(t *testing.T) {
	db, err := database.InitDB(":memory:")
	require.NoError(t, err)
	defer db.Close()

	ctrl := protection.New(db, config.ProtectionConfig{
		AutoHalt: config.AutoHaltConfig{Enabled: true},
	})
	enc := make([]byte, 256)
	_, err = rand.Read(enc)
	require.NoError(t, err)
	for i := 0; i < 80; i++ {
		assert.Equal(t, "", ctrl.Ingest("events", []byte{byte(i)}, enc))
	}
}

func TestNormalizeRole(t *testing.T) {
	assert.Equal(t, protection.RoleProd, protection.NormalizeRole("production"))
	assert.Equal(t, protection.RoleDR, protection.NormalizeRole("replica"))
	assert.Equal(t, protection.RoleOther, protection.NormalizeRole(""))
}

func TestAutoHaltComparesSamplesOfOneJobOnly(t *testing.T) {
	db, err := database.InitDB(":memory:")
	require.NoError(t, err)
	defer db.Close()

	ctrl := protection.New(db, config.ProtectionConfig{
		AutoHalt: config.AutoHaltConfig{Enabled: true, TombstoneMin: 10, TombstoneRatio: 0.5},
	})
	require.NoError(t, ctrl.SetEnabled(true))
	assert.Equal(t, "", ctrl.Observe(protection.Sample{Job: "a", Consumed: 0, Tombstones: 0}))
	assert.Equal(t, "", ctrl.Observe(protection.Sample{Job: "b", Consumed: 30, Tombstones: 20}))
	assert.Equal(t, "", ctrl.Observe(protection.Sample{Job: "a", Consumed: 5, Tombstones: 0}))
	assert.Equal(t, "", ctrl.Observe(protection.Sample{Job: "b", Consumed: 60, Tombstones: 20}))
	assert.Contains(t, ctrl.Observe(protection.Sample{Job: "a", Consumed: 25, Tombstones: 15}), "tombstone")
}

func TestAutoHaltStartsANewWindowWhenAJobRestarts(t *testing.T) {
	db, err := database.InitDB(":memory:")
	require.NoError(t, err)
	defer db.Close()

	ctrl := protection.New(db, config.ProtectionConfig{
		AutoHalt: config.AutoHaltConfig{Enabled: true, TombstoneMin: 10, TombstoneRatio: 0.5},
	})
	require.NoError(t, ctrl.SetEnabled(true))
	assert.Equal(t, "", ctrl.Observe(protection.Sample{Job: "a", Consumed: 1000, Tombstones: 0}))
	assert.Equal(t, "", ctrl.Observe(protection.Sample{Job: "a", Consumed: 40, Tombstones: 30}))
	assert.Equal(t, "", ctrl.Observe(protection.Sample{Job: "a", Consumed: 1000, Tombstones: 30}))
}
