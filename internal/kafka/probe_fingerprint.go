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
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"kaf-mirror/internal/config"
)

func ProbeFingerprint(cfg config.ClusterConfig) string {
	sum := sha256.Sum256(canonicalClusterConfig(cfg))
	return hex.EncodeToString(sum[:])
}

func hashSecret(value string) string {
	if value == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func canonicalClusterConfig(cfg config.ClusterConfig) []byte {
	hashed := cfg
	hashed.Security.Username = hashSecret(cfg.Security.Username)
	hashed.Security.Password = hashSecret(cfg.Security.Password)
	hashed.Security.APIKey = hashSecret(cfg.Security.APIKey)
	hashed.Security.APISecret = hashSecret(cfg.Security.APISecret)
	hashed.Security.Kerberos.Principal = hashSecret(cfg.Security.Kerberos.Principal)
	if cfg.Security.ConnectionString != nil {
		h := hashSecret(*cfg.Security.ConnectionString)
		hashed.Security.ConnectionString = &h
	}
	b, err := json.Marshal(hashed)
	if err != nil {
		sum := sha256.Sum256([]byte(fmt.Sprintf("%#v", hashed)))
		return sum[:]
	}
	return b
}
