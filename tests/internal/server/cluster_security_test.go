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

package server_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/scalytics/kaf-mirror/v2/internal/kafka"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kfake"
)

func postJSON(t *testing.T, ctx *TestContext, method, path string, body interface{}) (int, string) {
	t.Helper()
	raw, err := json.Marshal(body)
	require.NoError(t, err)
	req := httptest.NewRequest(method, path, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	addAuthHeader(req, ctx.Token)
	resp, err := ctx.Server.App.Test(req, -1)
	require.NoError(t, err)
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(out)
}

func TestClusterTestEndpointPassesFullSecurityConfig(t *testing.T) {
	for _, mechanism := range []string{"PLAIN", "SCRAM-SHA-256", "SCRAM-SHA-512"} {
		t.Run(mechanism, func(t *testing.T) {
			c, err := kfake.NewCluster(kfake.NumBrokers(1), kfake.EnableSASL(), kfake.Superuser(mechanism, "mirror", "kfake-pass"))
			require.NoError(t, err)
			defer c.Close()
			ctx := setupTestServer(t)
			brokers := strings.Join(c.ListenAddrs(), ",")
			security := map[string]interface{}{
				"enabled":        true,
				"protocol":       "SASL_PLAINTEXT",
				"sasl_mechanism": mechanism,
				"username":       "mirror",
				"password":       "kfake-pass",
			}

			status, body := postJSON(t, ctx, "POST", "/api/v1/clusters/test", map[string]interface{}{"provider": "plain", "brokers": brokers, "security": security})
			assert.Equal(t, 200, status, body)

			raw, err := json.Marshal(security)
			require.NoError(t, err)
			status, body = postJSON(t, ctx, "POST", "/api/v1/clusters/test", map[string]interface{}{"provider": "plain", "brokers": brokers, "security_config": string(raw)})
			assert.Equal(t, 200, status, body)

			security["password"] = "wrong-password"
			status, body = postJSON(t, ctx, "POST", "/api/v1/clusters/test", map[string]interface{}{"provider": "plain", "brokers": brokers, "security": security})
			assert.Equal(t, 503, status, body)
			assert.NotContains(t, body, "wrong-password")
		})
	}
}

func TestClusterSaveValidatesGSSAPIPaths(t *testing.T) {
	ctx := setupTestServer(t)
	missing := map[string]interface{}{
		"name":            "krb",
		"provider":        "plain",
		"brokers":         "broker-1:9092",
		"security_config": `{"enabled":true,"protocol":"SASL_SSL","sasl_mechanism":"GSSAPI","kerberos":{"service_name":"kafka"}}`,
	}
	status, body := postJSON(t, ctx, "POST", "/api/v1/clusters", missing)
	assert.Equal(t, 400, status, body)
	assert.Contains(t, body, "kerberos.principal, kerberos.keytab_path, kerberos.krb5_conf_path")

	status, body = postJSON(t, ctx, "POST", "/api/v1/clusters/test", missing)
	assert.Equal(t, 400, status, body)
	assert.Contains(t, body, "kerberos.keytab_path")

	complete := map[string]interface{}{
		"name":            "krb",
		"provider":        "plain",
		"brokers":         "broker-1:9092",
		"security_config": `{"enabled":true,"protocol":"SASL_SSL","sasl_mechanism":"GSSAPI","kerberos":{"service_name":"kafka","principal":"kaf-mirror@EXAMPLE.COM","keytab_path":"/app/kerberos/keytab/kafka.keytab","krb5_conf_path":"/app/kerberos/krb5/krb5.conf"}}`,
	}
	status, body = postJSON(t, ctx, "POST", "/api/v1/clusters", complete)
	assert.Equal(t, 201, status, body)

	status, body = postJSON(t, ctx, "PUT", "/api/v1/clusters/krb", missing)
	assert.Equal(t, 400, status, body)
	assert.Contains(t, body, "kerberos.principal")
}

func TestClusterSaveRejectsBadSecurityConfig(t *testing.T) {
	ctx := setupTestServer(t)
	cases := map[string]string{
		"not json":        `{"enabled":true`,
		"plain no secret": `{"enabled":true,"sasl_mechanism":"PLAIN","username":"u"}`,
		"unknown":         `{"enabled":true,"sasl_mechanism":"NOPE"}`,
	}
	for name, sec := range cases {
		t.Run(name, func(t *testing.T) {
			status, body := postJSON(t, ctx, "POST", "/api/v1/clusters", map[string]interface{}{"name": "bad", "brokers": "broker-1:9092", "security_config": sec})
			assert.Equal(t, 400, status, body)
		})
	}
}

func TestClusterSaveValidatesGSSAPIWhenEnabledOmitted(t *testing.T) {
	ctx := setupTestServer(t)
	missing := map[string]interface{}{
		"name":            "krb-no-enabled",
		"provider":        "plain",
		"brokers":         "broker-1:9092",
		"security_config": `{"protocol":"SASL_SSL","sasl_mechanism":"GSSAPI","kerberos":{"service_name":"kafka"}}`,
	}
	status, body := postJSON(t, ctx, "POST", "/api/v1/clusters", missing)
	assert.Equal(t, 400, status, body)
	assert.Contains(t, body, "kerberos.principal")

	status, body = postJSON(t, ctx, "POST", "/api/v1/clusters/test", map[string]interface{}{
		"provider": "plain",
		"brokers":  "broker-1:9092",
		"security": map[string]interface{}{
			"protocol":       "SASL_SSL",
			"sasl_mechanism": "GSSAPI",
			"kerberos":       map[string]string{"service_name": "kafka"},
		},
	})
	assert.Equal(t, 400, status, body)
	assert.Contains(t, body, "kerberos.principal")

	complete := map[string]interface{}{
		"name":            "krb-no-enabled",
		"provider":        "plain",
		"brokers":         "broker-1:9092",
		"security_config": `{"protocol":"SASL_SSL","sasl_mechanism":"GSSAPI","kerberos":{"service_name":"kafka","principal":"kaf-mirror@EXAMPLE.COM","keytab_path":"/app/kerberos/keytab/kafka.keytab","krb5_conf_path":"/app/kerberos/krb5/krb5.conf"}}`,
	}
	status, body = postJSON(t, ctx, "POST", "/api/v1/clusters", complete)
	assert.Equal(t, 201, status, body)
}

func TestClusterSaveValidatesTLSFiles(t *testing.T) {
	ctx := setupTestServer(t)
	cases := map[string]string{
		`{"protocol":"SSL","cert_file":"/app/kafka-tls/tls.crt"}`:    "cert_file and key_file must be set together",
		`{"protocol":"SSL","key_file":"/app/kafka-tls/tls.key"}`:     "cert_file and key_file must be set together",
		`{"protocol":"PLAINTEXT","ca_file":"/app/kafka-tls/ca.crt"}`: "need protocol SSL or SASL_SSL",
	}
	for sec, want := range cases {
		body := map[string]interface{}{"name": "tls", "provider": "plain", "brokers": "broker-1:9093", "security_config": sec}
		status, out := postJSON(t, ctx, "POST", "/api/v1/clusters", body)
		assert.Equal(t, 400, status, out)
		assert.Contains(t, out, want)
	}
	ok := map[string]interface{}{"name": "tls", "provider": "plain", "brokers": "broker-1:9093", "security_config": `{"protocol":"SSL","ca_file":"/app/kafka-tls/ca.crt","cert_file":"/app/kafka-tls/tls.crt","key_file":"/app/kafka-tls/tls.key"}`}
	status, out := postJSON(t, ctx, "POST", "/api/v1/clusters", ok)
	assert.Equal(t, 201, status, out)
}

func TestClusterSaveRejectsNonRegularOrOversizedTLSFiles(t *testing.T) {
	ctx := setupTestServer(t)
	fifo := filepath.Join(t.TempDir(), "ca.fifo")
	require.NoError(t, syscall.Mkfifo(fifo, 0600))
	sec, err := json.Marshal(map[string]string{"protocol": "SSL", "ca_file": fifo})
	require.NoError(t, err)
	done := make(chan struct {
		status int
		body   string
	}, 1)
	go func() {
		status, body := postJSON(t, ctx, "POST", "/api/v1/clusters", map[string]interface{}{
			"name": "tls-fifo", "provider": "plain", "brokers": "broker-1:9093", "security_config": string(sec),
		})
		done <- struct {
			status int
			body   string
		}{status, body}
	}()
	select {
	case got := <-done:
		assert.Equal(t, 400, got.status, got.body)
		assert.Contains(t, got.body, "ca_file")
		assert.Contains(t, got.body, "not a regular file")
	case <-time.After(time.Second):
		t.Fatal("POST /clusters blocked on a FIFO ca_file")
	}

	over := filepath.Join(t.TempDir(), "ca.crt")
	require.NoError(t, os.WriteFile(over, make([]byte, kafka.MaxTLSFileSize+1), 0600))
	sec, err = json.Marshal(map[string]string{"protocol": "SSL", "ca_file": over})
	require.NoError(t, err)
	status, body := postJSON(t, ctx, "POST", "/api/v1/clusters", map[string]interface{}{
		"name": "tls-huge", "provider": "plain", "brokers": "broker-1:9093", "security_config": string(sec),
	})
	assert.Equal(t, 400, status, body)
	assert.Contains(t, body, "ca_file")
	assert.Contains(t, body, "1 MiB")
}
