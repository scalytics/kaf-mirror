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
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/scalytics/kaf-mirror/v2/internal/config"
	"github.com/scalytics/kaf-mirror/v2/internal/database"

	"github.com/jmoiron/sqlx"
)

const guardPollInterval = 200 * time.Millisecond

const (
	RoleProd  = "prod"
	RoleDR    = "dr"
	RoleOther = "other"
)

type Sample struct {
	Job        string
	Consumed   int64
	Produced   int64
	Bytes      int64
	Errors     int64
	Tombstones int64
	Lag        int64
}

type Status struct {
	Enabled           bool   `json:"enabled"`
	Halted            bool   `json:"halted"`
	HaltReason        string `json:"halt_reason,omitempty"`
	HaltedAt          string `json:"halted_at,omitempty"`
	HaltedBy          string `json:"halted_by,omitempty"`
	FileTripwire      bool   `json:"file_tripwire"`
	EnvTripwire       bool   `json:"env_tripwire"`
	RefuseWriteToProd bool   `json:"refuse_write_to_prod"`
	AutoHaltEnabled   bool   `json:"auto_halt_enabled"`
	Experimental      bool   `json:"experimental"`
}

type guardSnap struct {
	enabled           bool
	halted            bool
	haltReason        string
	refuseWriteToProd bool
	readErr           error
}

type Controller struct {
	mu          sync.Mutex
	db          *sqlx.DB
	cfg         config.ProtectionConfig
	samples     map[string][]timedSample
	hits        []ingestHit
	baselines   map[string]*topicBaseline
	pendingHalt string
	guardGen    uint64
	guard       atomic.Pointer[guardSnap]
	pollStop    chan struct{}
	pollOnce    sync.Once
	pollWG      sync.WaitGroup
}

type timedSample struct {
	at time.Time
	s  Sample
}

func New(db *sqlx.DB, cfg config.ProtectionConfig) *Controller {
	cfg = cfg.WithDefaults()
	c := &Controller{db: db, cfg: cfg, pollStop: make(chan struct{})}
	c.refreshGuard()
	c.pollWG.Add(1)
	go c.pollGuard()
	return c
}

func (c *Controller) Close() {
	c.pollOnce.Do(func() { close(c.pollStop) })
	c.pollWG.Wait()
}

func (c *Controller) pollGuard() {
	defer c.pollWG.Done()
	ticker := time.NewTicker(guardPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			c.refreshGuard()
		case <-c.pollStop:
			return
		}
	}
}

func (c *Controller) Status() Status {
	status, _ := c.status()
	return status
}

func (c *Controller) status() (Status, error) {
	st, err := database.GetProtectionState(c.db)
	fileHit, envHit := c.tripwires()
	c.mu.Lock()
	pendingHalt := c.pendingHalt
	c.mu.Unlock()
	halted, reason := haltDecision(st.Enabled, st.Halted, st.HaltReason, pendingHalt, fileHit, envHit)
	haltedAt := ""
	if st.HaltedAt != nil {
		haltedAt = st.HaltedAt.UTC().Format(time.RFC3339)
	}
	return Status{
		Enabled:           st.Enabled,
		Halted:            halted,
		HaltReason:        reason,
		HaltedAt:          haltedAt,
		HaltedBy:          st.HaltedBy,
		FileTripwire:      fileHit,
		EnvTripwire:       envHit,
		RefuseWriteToProd: st.Enabled && c.cfg.RefuseWriteToProd,
		AutoHaltEnabled:   st.Enabled && c.cfg.AutoHalt.Enabled,
		Experimental:      true,
	}, err
}

func (c *Controller) Enabled() bool {
	snap := c.guard.Load()
	return snap != nil && snap.readErr == nil && snap.enabled
}

func (c *Controller) Halted() bool {
	return c.Status().Halted
}

func (c *Controller) SetEnabled(enabled bool) error {
	err := database.SetProtectionEnabled(c.db, enabled)
	c.bumpGuard()
	c.refreshGuard()
	return err
}

func (c *Controller) Halt(reason, by string) error {
	if !c.Enabled() {
		return fmt.Errorf("protection is disabled; enable it before halting")
	}
	if reason == "" {
		reason = "admin halt"
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pendingHalt = reason
	c.guardGen++
	if prev := c.guard.Load(); prev != nil && prev.readErr == nil {
		next := *prev
		next.halted = true
		next.haltReason = reason
		c.guard.Store(&next)
	}
	return database.SetProtectionHalted(c.db, true, reason, by)
}

func (c *Controller) Resume() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := database.SetProtectionHalted(c.db, false, "", ""); err != nil {
		return err
	}
	c.pendingHalt = ""
	c.guardGen++
	st, err := database.GetProtectionState(c.db)
	fileHit, envHit := c.tripwires()
	c.publishLocked(st, err, fileHit, envHit)
	return nil
}

func (c *Controller) GuardStart(targetRole string) error {
	snap := c.guard.Load()
	if snap == nil || snap.readErr != nil {
		if snap == nil || snap.readErr == nil {
			return fmt.Errorf("cannot read replication protection state: guard not ready")
		}
		return fmt.Errorf("cannot read replication protection state: %w", snap.readErr)
	}
	if !snap.enabled {
		return nil
	}
	if snap.halted {
		return fmt.Errorf("replication halted: %s", snap.haltReason)
	}
	if snap.refuseWriteToProd && NormalizeRole(targetRole) == RoleProd {
		return fmt.Errorf("refusing to replicate into a prod cluster while protection is enabled")
	}
	return nil
}

func (c *Controller) bumpGuard() {
	c.mu.Lock()
	c.guardGen++
	c.mu.Unlock()
}

func (c *Controller) refreshGuard() {
	c.mu.Lock()
	gen := c.guardGen
	c.mu.Unlock()

	st, err := database.GetProtectionState(c.db)
	fileHit, envHit := c.tripwires()

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.guardGen != gen {
		return
	}
	c.publishLocked(st, err, fileHit, envHit)
}

func (c *Controller) publishLocked(st *database.ProtectionState, readErr error, fileHit, envHit bool) {
	if readErr != nil || st == nil {
		if readErr == nil {
			readErr = fmt.Errorf("missing protection state")
		}
		c.guard.Store(&guardSnap{readErr: readErr})
		return
	}
	halted, reason := haltDecision(st.Enabled, st.Halted, st.HaltReason, c.pendingHalt, fileHit, envHit)
	c.guard.Store(&guardSnap{
		enabled:           st.Enabled,
		halted:            halted,
		haltReason:        reason,
		refuseWriteToProd: st.Enabled && c.cfg.RefuseWriteToProd,
	})
}

func haltDecision(enabled, dbHalted bool, dbReason, pending string, fileHit, envHit bool) (bool, string) {
	reason := dbReason
	if pending != "" {
		reason = pending
	}
	halted := enabled && (dbHalted || pending != "" || fileHit || envHit)
	if halted && reason == "" {
		if fileHit {
			reason = "halt file present"
		} else if envHit {
			reason = "halt env set"
		}
	}
	return halted, reason
}

func (c *Controller) Observe(s Sample) (haltReason string) {
	if !c.Enabled() || !c.cfg.AutoHalt.Enabled {
		return ""
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	if c.samples == nil {
		c.samples = make(map[string][]timedSample)
	}
	samples := c.samples[s.Job]
	if n := len(samples); n > 0 && countersDropped(samples[n-1].s, s) {
		samples = nil
	}
	samples = append(samples, timedSample{at: now, s: s})
	cutoff := now.Add(-time.Minute)
	kept := samples[:0]
	for _, sample := range samples {
		if sample.at.After(cutoff) {
			kept = append(kept, sample)
		}
	}
	c.samples[s.Job] = kept
	if len(kept) < 2 {
		return ""
	}
	first, last := kept[0].s, kept[len(kept)-1].s
	consumed := last.Consumed - first.Consumed
	tombstones := last.Tombstones - first.Tombstones
	errors := last.Errors - first.Errors
	bytes := last.Bytes - first.Bytes
	produced := last.Produced - first.Produced

	if consumed < 0 {
		consumed = last.Consumed
	}
	if tombstones < 0 {
		tombstones = last.Tombstones
	}
	if errors < 0 {
		errors = last.Errors
	}
	if bytes < 0 {
		bytes = last.Bytes
	}
	if produced < 0 {
		produced = last.Produced
	}

	minTomb := c.cfg.AutoHalt.TombstoneMin
	if minTomb <= 0 {
		minTomb = 100
	}
	ratio := c.cfg.AutoHalt.TombstoneRatio
	if ratio <= 0 {
		ratio = 0.5
	}
	if consumed > 0 && tombstones >= int64(minTomb) && float64(tombstones)/float64(consumed) >= ratio {
		return fmt.Sprintf("auto-halt: tombstone storm (%d/%d records empty in 60s)", tombstones, consumed)
	}
	burst := c.cfg.AutoHalt.AuthErrorBurst
	if burst <= 0 {
		burst = 50
	}
	if errors >= int64(burst) {
		return fmt.Sprintf("auto-halt: produce/auth error storm (%d errors in 60s)", errors)
	}
	if produced > 20 && last.Lag+int64(len(kept)) < first.Lag && first.Lag > 100 {
		avgOld := float64(first.Bytes+1) / float64(first.Produced+1)
		avgNew := float64(bytes+1) / float64(produced+1)
		if avgNew > avgOld*8 {
			return "auto-halt: payload inflation while source lag collapsed"
		}
	}
	return ""
}

func countersDropped(prev, next Sample) bool {
	return next.Consumed < prev.Consumed || next.Produced < prev.Produced || next.Bytes < prev.Bytes ||
		next.Errors < prev.Errors || next.Tombstones < prev.Tombstones
}

func (c *Controller) tripwires() (fileHit, envHit bool) {
	path := c.cfg.HaltFile
	if path == "" {
		path = "data/HALT"
	}
	if _, err := os.Stat(path); err == nil {
		fileHit = true
	}
	envName := c.cfg.HaltEnv
	if envName == "" {
		envName = "KAF_MIRROR_HALT"
	}
	v := strings.TrimSpace(strings.ToLower(os.Getenv(envName)))
	envHit = v == "1" || v == "true" || v == "yes"
	return fileHit, envHit
}

func NormalizeRole(role string) string {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case RoleProd, "production":
		return RoleProd
	case RoleDR, "disaster-recovery", "replica":
		return RoleDR
	case "":
		return RoleOther
	default:
		return RoleOther
	}
}
