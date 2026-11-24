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

package kafka_test

import (
	"reflect"
	"strings"
	"testing"

	"kaf-mirror/internal/config"
	"kaf-mirror/internal/database"
	"kaf-mirror/internal/kafka"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var probeFingerprintFields = []string{
	"Provider",
	"ClusterID",
	"Brokers",
	"Security.Enabled",
	"Security.Protocol",
	"Security.SASLMechanism",
	"Security.Username",
	"Security.Password",
	"Security.APIKey",
	"Security.APISecret",
	"Security.ConnectionString",
	"Security.Kerberos.ServiceName",
	"Security.Kerberos.Principal",
	"Security.Kerberos.KeytabPath",
	"Security.Kerberos.Krb5ConfPath",
	"DisableIdempotentWrites",
}

func clusterConfigFieldPaths(typ reflect.Type, prefix string) []string {
	if typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	if typ.Kind() != reflect.Struct {
		return nil
	}
	var paths []string
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if field.PkgPath != "" {
			continue
		}
		path := field.Name
		if prefix != "" {
			path = prefix + "." + field.Name
		}
		child := field.Type
		if child.Kind() == reflect.Pointer {
			child = child.Elem()
		}
		if child.Kind() == reflect.Struct && child.PkgPath() == typ.PkgPath() {
			paths = append(paths, clusterConfigFieldPaths(field.Type, path)...)
			continue
		}
		paths = append(paths, path)
	}
	return paths
}

func baseClusterConfig() config.ClusterConfig {
	return config.ClusterConfig{
		Provider:  "plain",
		ClusterID: "id-1",
		Brokers:   "localhost:9092",
	}
}

func setClusterConfigPath(cfg *config.ClusterConfig, path, value string) {
	v := reflect.ValueOf(cfg).Elem()
	parts := strings.Split(path, ".")
	for i, part := range parts {
		if v.Kind() == reflect.Pointer {
			if v.IsNil() {
				v.Set(reflect.New(v.Type().Elem()))
			}
			v = v.Elem()
		}
		v = v.FieldByName(part)
		if !v.IsValid() {
			panic("missing field " + path)
		}
		if i < len(parts)-1 {
			continue
		}
		switch v.Kind() {
		case reflect.String:
			v.SetString(value)
		case reflect.Bool:
			v.SetBool(true)
		case reflect.Pointer:
			if v.Type().Elem().Kind() == reflect.String {
				v.Set(reflect.ValueOf(&value))
				return
			}
			panic("unsupported pointer field " + path)
		default:
			panic("unsupported field kind " + path + " " + v.Kind().String())
		}
	}
}

func probeFP(t *testing.T, cfg config.ClusterConfig) string {
	t.Helper()
	fp, ok := kafka.ProbeFingerprint(cfg)
	require.True(t, ok)
	require.NotEmpty(t, fp)
	return fp
}

func TestProbeFingerprintCoversEveryClusterConfigField(t *testing.T) {
	got := clusterConfigFieldPaths(reflect.TypeOf(config.ClusterConfig{}), "")
	require.Equal(t, probeFingerprintFields, got, "ClusterConfig fields changed; confirm ProbeFingerprint still hashes every connection field and update this list")

	base := baseClusterConfig()
	baseFP := probeFP(t, base)

	for _, path := range got {
		cfg := baseClusterConfig()
		secret := "fp-secret-value/" + path
		setClusterConfigPath(&cfg, path, secret)
		fp := probeFP(t, cfg)
		assert.NotEqual(t, baseFP, fp, "%s must change the probe fingerprint", path)
		assert.NotContains(t, fp, secret, "%s must not appear in the fingerprint", path)
		assert.NotContains(t, fp, "fp-secret-value")
	}
}

func TestProbeFingerprintHidesCredentialValues(t *testing.T) {
	conn := "Endpoint=sb://hub.servicebus.windows.net/;SharedAccessKey=azure-secret-key"
	cfg := config.ClusterConfig{
		Provider: "confluent",
		Brokers:  "pkc.example:9092",
		Security: config.SecurityConfig{
			APIKey:           "confluent-api-key",
			APISecret:        "confluent-api-secret",
			Username:         "sasl-user",
			Password:         "sasl-password",
			ConnectionString: &conn,
			Kerberos:         config.KerberosConfig{Principal: "kafka/broker@REALM"},
		},
	}
	fp := probeFP(t, cfg)
	for _, secret := range []string{
		"confluent-api-key",
		"confluent-api-secret",
		"sasl-user",
		"sasl-password",
		conn,
		"azure-secret-key",
		"kafka/broker@REALM",
	} {
		assert.NotContains(t, fp, secret)
	}

	other := cfg
	other.Security.APISecret = "rotated-confluent-secret"
	assert.NotEqual(t, fp, probeFP(t, other))
	other = cfg
	other.Security.ConnectionString = strPtr("Endpoint=sb://hub.servicebus.windows.net/;SharedAccessKey=rotated-azure")
	assert.NotEqual(t, fp, probeFP(t, other))
	other = cfg
	other.Provider = "azure"
	assert.NotEqual(t, fp, probeFP(t, other))
}

func TestProbeFingerprintFollowsClusterConfigFromRow(t *testing.T) {
	conn := "Endpoint=sb://hub.servicebus.windows.net/;SharedAccessKey=azure-secret-key"
	row := database.KafkaCluster{
		Provider:         "confluent",
		Brokers:          "pkc.example:9092",
		SecurityConfig:   `{"protocol":"SASL_SSL"}`,
		APIKey:           "confluent-api-key",
		APISecret:        "confluent-api-secret",
		ConnectionString: &conn,
	}
	fp := probeFP(t, database.ClusterConfigFromRow(row))
	rotated := row
	rotated.APISecret = "rotated-confluent-secret"
	assert.NotEqual(t, fp, probeFP(t, database.ClusterConfigFromRow(rotated)))
	assert.NotContains(t, fp, "confluent-api-secret")
	assert.NotContains(t, fp, conn)
}

func strPtr(s string) *string { return &s }
