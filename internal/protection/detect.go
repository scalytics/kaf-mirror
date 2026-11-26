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
	"hash/fnv"
	"math"
	"time"
)

const (
	entropySampleBytes = 1024
	minEntropyValueLen = 32
	entropyBaselineN   = 20
	maxTrackedKeys     = 20000
	ingestWindow       = time.Minute
	minEntropySamples  = 40
	highEntropyRatio   = 0.5
	minRewriteUnique   = 20
	volumeEntropyN     = 200
)

type ingestHit struct {
	at          time.Time
	highEntropy bool
	rewritten   bool
}

type topicBaseline struct {
	ewma    float64
	n       int
	lastVal map[string]uint64
}

func shannonEntropy(b []byte) float64 {
	if len(b) == 0 {
		return 0
	}
	if len(b) > entropySampleBytes {
		b = b[:entropySampleBytes]
	}
	var freq [256]int
	for _, c := range b {
		freq[c]++
	}
	n := float64(len(b))
	var h float64
	for _, f := range freq {
		if f == 0 {
			continue
		}
		p := float64(f) / n
		h -= p * math.Log2(p)
	}
	return h
}

func valueHash(b []byte) uint64 {
	h := fnv.New64a()
	_, _ = h.Write(b)
	return h.Sum64()
}

func (c *Controller) Ingest(topic string, key, value []byte) string {
	if !c.Enabled() || !c.cfg.AutoHalt.Enabled {
		return ""
	}
	if len(value) == 0 {
		return ""
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.baselines == nil {
		c.baselines = map[string]*topicBaseline{}
	}
	base := c.baselines[topic]
	if base == nil {
		base = &topicBaseline{lastVal: map[string]uint64{}}
		c.baselines[topic] = base
	}

	hit := ingestHit{at: time.Now()}
	if len(value) >= minEntropyValueLen {
		h := shannonEntropy(value)
		if base.n < entropyBaselineN {
			if base.n == 0 {
				base.ewma = h
			} else {
				base.ewma = 0.9*base.ewma + 0.1*h
			}
			base.n++
		} else {
			high := (h > base.ewma+1.2 && h > 6.5) || (base.ewma < 6.0 && h > 7.2)
			if high {
				hit.highEntropy = true
			} else {
				base.ewma = 0.95*base.ewma + 0.05*h
			}
			base.n++
		}
	}
	if len(key) > 0 {
		ks := string(key)
		vh := valueHash(value)
		if prev, ok := base.lastVal[ks]; ok {
			hit.rewritten = prev != vh
			base.lastVal[ks] = vh
		} else if len(base.lastVal) < maxTrackedKeys {
			base.lastVal[ks] = vh
		}
	}
	c.hits = append(c.hits, hit)
	return c.evaluatePayloadLocked()
}

func (c *Controller) evaluatePayloadLocked() string {
	now := time.Now()
	cutoff := now.Add(-ingestWindow)
	kept := c.hits[:0]
	var samples, high, rewritten int
	for _, h := range c.hits {
		if h.at.After(cutoff) {
			kept = append(kept, h)
			samples++
			if h.highEntropy {
				high++
			}
			if h.rewritten {
				rewritten++
			}
		}
	}
	c.hits = kept
	if samples < minEntropySamples {
		return ""
	}
	entropyStorm := float64(high)/float64(samples) >= highEntropyRatio
	rewriteStorm := rewritten >= minRewriteUnique
	if entropyStorm && rewriteStorm {
		return "auto-halt: high-entropy rewrite of existing keys (possible source encryption)"
	}
	if entropyStorm && samples >= volumeEntropyN {
		return "auto-halt: high-entropy payload storm"
	}
	return ""
}
