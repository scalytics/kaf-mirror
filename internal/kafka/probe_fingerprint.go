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

package kafka

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"

	"kaf-mirror/internal/config"
)

var (
	probeMACMu    sync.Mutex
	probeMACKey   [32]byte
	probeMACKeyOK bool
)

func init() {
	initProbeMACKey()
}

func initProbeMACKey() {
	n, err := rand.Read(probeMACKey[:])
	probeMACKeyOK = err == nil && n == len(probeMACKey)
}

func ProbeFingerprint(cfg config.ClusterConfig) (string, bool) {
	probeMACMu.Lock()
	ok := probeMACKeyOK
	var key [32]byte
	if ok {
		key = probeMACKey
	}
	probeMACMu.Unlock()
	if !ok {
		return "", false
	}
	return probeFingerprintWithKey(cfg, key[:]), true
}

func probeFingerprintWithKey(cfg config.ClusterConfig, key []byte) string {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(canonicalClusterConfig(cfg))
	return hex.EncodeToString(mac.Sum(nil))
}

func suspendProbeMACKeyForTest() func() {
	probeMACMu.Lock()
	was := probeMACKeyOK
	probeMACKeyOK = false
	probeMACMu.Unlock()
	return func() {
		probeMACMu.Lock()
		probeMACKeyOK = was
		probeMACMu.Unlock()
	}
}

func canonicalClusterConfig(cfg config.ClusterConfig) []byte {
	b, err := json.Marshal(cfg)
	if err != nil {
		return []byte(fmt.Sprintf("%#v", cfg))
	}
	return b
}
