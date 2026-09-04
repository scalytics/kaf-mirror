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

package egress_test

import (
	"net"
	"testing"

	"kaf-mirror/internal/egress"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCheckEmptyURL(t *testing.T) {
	assert.NoError(t, egress.Check("", nil))
}

func TestCheckRejectsLoopbackAndMetadata(t *testing.T) {
	assert.Error(t, egress.Check("http://127.0.0.1/latest", nil))
	assert.Error(t, egress.Check("http://169.254.169.254/latest/meta-data", nil))
	assert.Error(t, egress.Check("http://100.100.100.200/", nil))
	assert.Error(t, egress.Check("ftp://example.com/x", nil))
	assert.Error(t, egress.Check("https://user:pass@example.com/", nil))
}

func TestCheckAllowlist(t *testing.T) {
	assert.Error(t, egress.Check("https://evil.example/x", []string{"api.openai.com"}))
	require.NoError(t, egress.Check("https://192.0.2.1/v1", []string{"192.0.2.1"}))
}

func TestCheckResolvesBlockedName(t *testing.T) {
	restore := egress.SetLookupIPForTest(func(host string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("127.0.0.1")}, nil
	})
	defer restore()
	assert.Error(t, egress.Check("https://metadata.internal/latest", nil))
}

func TestCheckResolvesPublicName(t *testing.T) {
	restore := egress.SetLookupIPForTest(func(host string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("203.0.113.10")}, nil
	})
	defer restore()
	assert.NoError(t, egress.Check("https://api.openai.com/v1", nil))
	assert.NoError(t, egress.Check("https://api.openai.com/v1", []string{"api.openai.com", ".x.ai"}))
}
