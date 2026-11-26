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

package protection

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"kaf-mirror/internal/config"
	"kaf-mirror/internal/database"

	"github.com/stretchr/testify/require"
)

func attackController(t *testing.T, auto bool) *Controller {
	t.Helper()
	db, err := database.InitDB(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	c := New(db, config.ProtectionConfig{AutoHalt: config.AutoHaltConfig{Enabled: auto}})
	t.Cleanup(c.Close)
	require.NoError(t, c.SetEnabled(true))
	return c
}

func encryptedValue() []byte {
	value := make([]byte, 256)
	for i := range value {
		value[i] = byte(i)
	}
	return value
}

func TestPayloadAttackMatrix(t *testing.T) {
	plain := []byte(`{"customer":"alice","status":"active","city":"berlin"}`)
	for _, tc := range []struct {
		name                            string
		auto                            bool
		keyed, encrypted, warmEncrypted bool
		want                            string
	}{
		{name: "encryption of existing keys", auto: true, keyed: true, encrypted: true, want: "rewrite"},
		{name: "encryption with unique or missing keys", auto: true, encrypted: true, want: "payload storm"},
		{name: "ordinary rewrites", auto: true, keyed: true},
		{name: "ordinary encrypted stream", auto: true, keyed: true, encrypted: true, warmEncrypted: true},
		{name: "automatic detection disabled", keyed: true, encrypted: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := attackController(t, tc.auto)
			baseline := plain
			if tc.warmEncrypted {
				baseline = encryptedValue()
			}
			for i := 0; i < 25; i++ {
				require.Empty(t, c.Ingest("events", []byte{byte(i % 20)}, baseline))
			}
			var reason string
			for i := 0; i < 250; i++ {
				value := plain
				if tc.encrypted {
					value = encryptedValue()
				}
				var key []byte
				if tc.keyed {
					key = []byte{byte(i % 20)}
				}
				if reason = c.Ingest("events", key, value); reason != "" {
					break
				}
			}
			if tc.want == "" {
				require.Empty(t, reason)
			} else {
				require.Contains(t, reason, tc.want)
			}
		})
	}
}

func TestTrackedKeysStillDetectRewritesWhenCapacityIsFull(t *testing.T) {
	c := attackController(t, true)
	plain := []byte(`{"customer":"alice","status":"active","city":"berlin"}`)
	base := &topicBaseline{ewma: shannonEntropy(plain), n: entropyBaselineN, lastVal: make(map[string]uint64, maxTrackedKeys)}
	for i := 0; i < maxTrackedKeys; i++ {
		base.lastVal[fmt.Sprint(i)] = valueHash(plain)
	}
	c.baselines = map[string]*topicBaseline{"events": base}
	for i := 0; i < minRewriteUnique; i++ {
		c.Ingest("events", []byte(fmt.Sprint(i)), encryptedValue())
	}
	for i := 0; i < minRewriteUnique; i++ {
		c.Ingest("events", nil, encryptedValue())
	}
	require.Contains(t, c.evaluatePayloadLocked(), "rewrite")
	require.Equal(t, maxTrackedKeys, len(base.lastVal))
	require.Equal(t, valueHash(encryptedValue()), base.lastVal["0"])
	c.Ingest("events", []byte("untracked"), plain)
	require.Equal(t, maxTrackedKeys, len(base.lastVal), "capacity must remain bounded")
	_, tracked := base.lastVal["untracked"]
	require.False(t, tracked)
}

func TestExpiredPayloadEvidenceDoesNotTriggerAHalt(t *testing.T) {
	c := attackController(t, true)
	for i := 0; i < 250; i++ {
		c.hits = append(c.hits, ingestHit{at: time.Now().Add(-2 * time.Minute), highEntropy: true, rewritten: true})
	}
	require.Empty(t, c.Ingest("events", nil, []byte("normal")))
	require.Len(t, c.hits, 1)
}

func TestGuardFailsClosedWhenProtectionStateCannotBeRead(t *testing.T) {
	c := attackController(t, true)
	c.Close()
	require.NoError(t, c.db.Close())
	require.NoError(t, c.GuardStart(RoleDR), "a send must keep the last snapshot instead of reading the database")
	c.refreshGuard()
	require.ErrorContains(t, c.GuardStart(RoleDR), "cannot read replication protection state")
}

func TestGuardStartDoesNotStatTheHaltFile(t *testing.T) {
	db, err := database.InitDB(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	haltPath := filepath.Join(t.TempDir(), "HALT")
	c := New(db, config.ProtectionConfig{HaltFile: haltPath})
	t.Cleanup(c.Close)
	require.NoError(t, c.SetEnabled(true))
	c.Close()

	require.NoError(t, os.WriteFile(haltPath, []byte("stop"), 0o600))
	require.NoError(t, c.GuardStart(RoleDR), "a send must not stat the halt file")
	c.refreshGuard()
	require.ErrorContains(t, c.GuardStart(RoleDR), "halt file present")

	require.NoError(t, os.Remove(haltPath))
	require.ErrorContains(t, c.GuardStart(RoleDR), "halt file present")
	c.refreshGuard()
	require.NoError(t, c.GuardStart(RoleDR))
}

func TestHaltStaysEffectiveWhenPersistenceFails(t *testing.T) {
	c := attackController(t, true)
	_, err := c.db.Exec(`CREATE TRIGGER reject_halt BEFORE UPDATE ON protection_state BEGIN SELECT RAISE(FAIL, 'storage failure'); END`)
	require.NoError(t, err)
	require.Error(t, c.Halt("source encryption", "auto"))
	require.True(t, c.Halted())
	require.ErrorContains(t, c.GuardStart(RoleDR), "source encryption")
	require.Error(t, c.Resume(), "a failed resume must not clear the in-process halt")
	require.True(t, c.Halted())
	_, err = c.db.Exec(`DROP TRIGGER reject_halt`)
	require.NoError(t, err)
	require.NoError(t, c.Resume())
	require.False(t, c.Halted())
	require.NoError(t, c.GuardStart(RoleDR))
}

func TestProtectionStateWithoutARowIsDisabled(t *testing.T) {
	c := attackController(t, true)
	c.Close()
	_, err := c.db.Exec(`DELETE FROM protection_state`)
	require.NoError(t, err)
	state, err := database.GetProtectionState(c.db)
	require.NoError(t, err)
	require.False(t, state.Enabled)
	require.ErrorContains(t, c.GuardStart(RoleProd), "refusing to replicate")
	c.refreshGuard()
	require.NoError(t, c.GuardStart(RoleProd))
}

func TestPayloadEdgesAndManualHalt(t *testing.T) {
	c := attackController(t, true)
	require.Empty(t, c.Ingest("events", nil, nil), "tombstones are handled by the counter detector")
	require.Empty(t, c.hits)
	require.Zero(t, shannonEntropy(nil))
	long := make([]byte, 2*entropySampleBytes)
	require.Zero(t, shannonEntropy(long), "entropy sampling is bounded")
	require.NoError(t, c.Halt("", "admin"))
	require.ErrorContains(t, c.GuardStart(RoleDR), "admin halt")
	require.NoError(t, c.Resume())
	require.NoError(t, c.SetEnabled(false))
	require.ErrorContains(t, c.Halt("test", "admin"), "protection is disabled")
}
