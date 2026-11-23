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
	"crypto/tls"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"kaf-mirror/internal/config"
	"kaf-mirror/internal/kafka"
	"kaf-mirror/pkg/logger"

	"github.com/jcmturner/gokrb5/v8/iana/etypeID"
	"github.com/jcmturner/gokrb5/v8/keytab"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/sasl"
)

const testPassword = "s3cret-pass-do-not-log"

func writeKerberosFiles(t *testing.T, defaultRealm string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	kt := keytab.New()
	require.NoError(t, kt.AddEntry("kaf-mirror", "EXAMPLE.COM", "keytab-password", time.Unix(0, 0), 1, etypeID.AES256_CTS_HMAC_SHA1_96))
	raw, err := kt.Marshal()
	require.NoError(t, err)
	keytabPath := filepath.Join(dir, "kafka.keytab")
	require.NoError(t, os.WriteFile(keytabPath, raw, 0o600))

	conf := "[libdefaults]\n"
	if defaultRealm != "" {
		conf += "  default_realm = " + defaultRealm + "\n"
	}
	conf += "[realms]\n  EXAMPLE.COM = {\n    kdc = kdc.example.com:88\n  }\n"
	krb5Path := filepath.Join(dir, "krb5.conf")
	require.NoError(t, os.WriteFile(krb5Path, []byte(conf), 0o600))
	return keytabPath, krb5Path
}

func gssapiSecurity(keytabPath, krb5Path, principal string) config.SecurityConfig {
	return config.SecurityConfig{
		Enabled:       true,
		Protocol:      "SASL_PLAINTEXT",
		SASLMechanism: "GSSAPI",
		Kerberos: config.KerberosConfig{
			ServiceName:  "kafka",
			Principal:    principal,
			KeytabPath:   keytabPath,
			Krb5ConfPath: krb5Path,
		},
	}
}

func saslNames(t *testing.T, cl *kgo.Client) []string {
	t.Helper()
	mechs, _ := cl.OptValue(kgo.SASL).([]sasl.Mechanism)
	names := make([]string, 0, len(mechs))
	for _, m := range mechs {
		names = append(names, m.Name())
	}
	return names
}

func usesTLS(cl *kgo.Client) bool {
	cfg, _ := cl.OptValue(kgo.DialTLSConfig).(*tls.Config)
	return cfg != nil
}

func clientFromOptions(t *testing.T, cfg config.ClusterConfig) *kgo.Client {
	t.Helper()
	opts, err := kafka.ClientOptions(cfg)
	require.NoError(t, err)
	cl, err := kgo.NewClient(opts...)
	require.NoError(t, err)
	t.Cleanup(cl.Close)
	return cl
}

func TestClientOptionsPerMechanism(t *testing.T) {
	keytabPath, krb5Path := writeKerberosFiles(t, "")
	conn := "Endpoint=sb://example.servicebus.windows.net/;SharedAccessKey=abc"
	cases := []struct {
		name     string
		cfg      config.ClusterConfig
		wantSASL []string
		wantTLS  bool
	}{
		{"no security", config.ClusterConfig{Provider: "plain"}, nil, false},
		{"tls only", config.ClusterConfig{Security: config.SecurityConfig{Protocol: "SSL"}}, nil, true},
		{"plain over plaintext", config.ClusterConfig{Provider: "plain", Security: config.SecurityConfig{Enabled: true, Protocol: "SASL_PLAINTEXT", SASLMechanism: "PLAIN", Username: "u", Password: testPassword}}, []string{"PLAIN"}, false},
		{"plain over ssl", config.ClusterConfig{Provider: "plain", Security: config.SecurityConfig{Enabled: true, Protocol: "SASL_SSL", SASLMechanism: "PLAIN", Username: "u", Password: testPassword}}, []string{"PLAIN"}, true},
		{"scram sha 256", config.ClusterConfig{Provider: "plain", Security: config.SecurityConfig{Enabled: true, Protocol: "SASL_SSL", SASLMechanism: "SCRAM-SHA-256", Username: "u", Password: testPassword}}, []string{"SCRAM-SHA-256"}, true},
		{"scram sha 512", config.ClusterConfig{Provider: "plain", Security: config.SecurityConfig{Enabled: true, Protocol: "SASL_PLAINTEXT", SASLMechanism: "SCRAM-SHA-512", Username: "u", Password: testPassword}}, []string{"SCRAM-SHA-512"}, false},
		{"mechanism is case insensitive", config.ClusterConfig{Security: config.SecurityConfig{Enabled: true, SASLMechanism: "scram-sha-512", Username: "u", Password: testPassword}}, []string{"SCRAM-SHA-512"}, false},
		{"gssapi", config.ClusterConfig{Provider: "plain", Security: gssapiSecurity(keytabPath, krb5Path, "kaf-mirror@EXAMPLE.COM")}, []string{"GSSAPI"}, false},
		{"disabled security ignores mechanism", config.ClusterConfig{Security: config.SecurityConfig{SASLMechanism: "PLAIN"}}, nil, false},
		{"confluent preset", config.ClusterConfig{Provider: "confluent", Security: config.SecurityConfig{APIKey: "key", APISecret: testPassword}}, []string{"PLAIN"}, true},
		{"confluent preset with SASL_SSL protocol", config.ClusterConfig{Provider: "confluent", Security: config.SecurityConfig{Enabled: true, Protocol: "SASL_SSL", SASLMechanism: "PLAIN", APIKey: "key", APISecret: testPassword}}, []string{"PLAIN"}, true},
		{"azure preset", config.ClusterConfig{Provider: "azure", Security: config.SecurityConfig{ConnectionString: &conn}}, []string{"PLAIN"}, true},
		{"redpanda preset", config.ClusterConfig{Provider: "redpanda", Security: config.SecurityConfig{Username: "u", Password: testPassword}}, []string{"SCRAM-SHA-256"}, true},
		{"redpanda with explicit mechanism", config.ClusterConfig{Provider: "redpanda", Security: config.SecurityConfig{Enabled: true, Protocol: "SASL_PLAINTEXT", SASLMechanism: "SCRAM-SHA-512", Username: "u", Password: testPassword}}, []string{"SCRAM-SHA-512"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.cfg.Brokers = "broker-1:9092, broker-2:9092"
			cl := clientFromOptions(t, tc.cfg)
			assert.Equal(t, tc.wantSASL, nilIfEmpty(saslNames(t, cl)))
			assert.Equal(t, tc.wantTLS, usesTLS(cl))
			assert.Equal(t, []string{"broker-1:9092", "broker-2:9092"}, cl.OptValue(kgo.SeedBrokers))
		})
	}
}

func nilIfEmpty(s []string) []string {
	if len(s) == 0 {
		return nil
	}
	return s
}

func TestClientOptionsRejectsBadSecurity(t *testing.T) {
	keytabPath, krb5Path := writeKerberosFiles(t, "")
	noRealmKeytab, noRealmKrb5 := writeKerberosFiles(t, "")
	cases := []struct {
		name string
		cfg  config.ClusterConfig
		want string
	}{
		{"no brokers", config.ClusterConfig{Brokers: " , "}, "no brokers configured"},
		{"plain without password", config.ClusterConfig{Security: config.SecurityConfig{Enabled: true, SASLMechanism: "PLAIN", Username: "u"}}, "username and password are required for SASL PLAIN"},
		{"scram without user", config.ClusterConfig{Security: config.SecurityConfig{Enabled: true, SASLMechanism: "SCRAM-SHA-256", Password: testPassword}}, "username and password are required for SASL SCRAM-SHA-256"},
		{"unknown mechanism", config.ClusterConfig{Security: config.SecurityConfig{Enabled: true, SASLMechanism: "OAUTHBEARER"}}, "unsupported SASL mechanism: OAUTHBEARER"},
		{"gssapi without paths", config.ClusterConfig{Security: config.SecurityConfig{Enabled: true, SASLMechanism: "GSSAPI"}}, "SASL GSSAPI requires kerberos.principal, kerberos.keytab_path, kerberos.krb5_conf_path"},
		{"gssapi without keytab", config.ClusterConfig{Security: gssapiSecurity("", krb5Path, "kaf-mirror@EXAMPLE.COM")}, "SASL GSSAPI requires kerberos.keytab_path"},
		{"gssapi missing keytab file", config.ClusterConfig{Security: gssapiSecurity(filepath.Join(t.TempDir(), "missing.keytab"), krb5Path, "kaf-mirror@EXAMPLE.COM")}, "failed to load keytab"},
		{"gssapi missing krb5.conf file", config.ClusterConfig{Security: gssapiSecurity(keytabPath, filepath.Join(t.TempDir(), "missing.conf"), "kaf-mirror@EXAMPLE.COM")}, "failed to load krb5.conf"},
		{"gssapi principal without realm", config.ClusterConfig{Security: gssapiSecurity(noRealmKeytab, noRealmKrb5, "kaf-mirror")}, "needs a realm"},
		{"confluent without secret", config.ClusterConfig{Provider: "confluent", Security: config.SecurityConfig{APIKey: "key"}}, "confluent provider requires API key and secret"},
		{"azure without connection string", config.ClusterConfig{Provider: "azure"}, "azure provider requires a connection string"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.cfg.Brokers == "" {
				tc.cfg.Brokers = "broker-1:9092"
			}
			_, err := kafka.ClientOptions(tc.cfg)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
			assert.NotContains(t, err.Error(), testPassword)
		})
	}
}

func TestGSSAPIUsesDefaultRealmFromKrb5Conf(t *testing.T) {
	keytabPath, krb5Path := writeKerberosFiles(t, "EXAMPLE.COM")
	cl := clientFromOptions(t, config.ClusterConfig{Brokers: "broker-1:9092", Security: gssapiSecurity(keytabPath, krb5Path, "kaf-mirror")})
	assert.Equal(t, []string{"GSSAPI"}, saslNames(t, cl))
}

func TestValidateSecurityDoesNotReadFiles(t *testing.T) {
	cfg := config.ClusterConfig{Brokers: "broker-1:9092", Security: gssapiSecurity("/nonexistent/kafka.keytab", "/nonexistent/krb5.conf", "kaf-mirror@EXAMPLE.COM")}
	assert.NoError(t, kafka.ValidateSecurity(cfg))
	cfg.Security.Kerberos.Principal = ""
	assert.ErrorContains(t, kafka.ValidateSecurity(cfg), "kerberos.principal")
}

func plainProviderSASL(mechanism string) config.ClusterConfig {
	return config.ClusterConfig{
		Provider: "plain",
		Brokers:  "broker-1:9092",
		Security: config.SecurityConfig{Enabled: true, Protocol: "SASL_SSL", SASLMechanism: mechanism, Username: "u", Password: testPassword},
	}
}

func TestEveryClientTypeUsesSharedSASL(t *testing.T) {
	keytabPath, krb5Path := writeKerberosFiles(t, "")
	for _, mechanism := range []string{"PLAIN", "SCRAM-SHA-256", "SCRAM-SHA-512", "GSSAPI"} {
		cfg := plainProviderSASL(mechanism)
		if mechanism == "GSSAPI" {
			cfg.Security = gssapiSecurity(keytabPath, krb5Path, "kaf-mirror@EXAMPLE.COM")
			cfg.Security.Protocol = "SASL_SSL"
		}
		t.Run(mechanism, func(t *testing.T) {
			consumer, err := kafka.NewConsumer(cfg, "group", config.ReplicationConfig{BatchSize: 100}, "job", "topic")
			require.NoError(t, err)
			defer consumer.Client.Close()
			producer, err := kafka.NewProducer(cfg, config.ReplicationConfig{BatchSize: 100}, "job")
			require.NoError(t, err)
			defer producer.Client.Close()
			admin, err := kafka.NewAdminClient(cfg)
			require.NoError(t, err)
			defer admin.Close()

			clients := map[string]*kgo.Client{
				"consumer": consumer.Client.(*kgo.Client),
				"producer": producer.Client.(*kgo.Client),
				"admin":    admin.KgoClientForTest(),
			}
			for name, cl := range clients {
				assert.Equal(t, []string{mechanism}, saslNames(t, cl), name)
				assert.True(t, usesTLS(cl), name)
			}
		})
	}
}

func TestPresetProvidersApplyToEveryClientType(t *testing.T) {
	conn := "Endpoint=sb://example.servicebus.windows.net/;SharedAccessKey=abc"
	cases := map[string]config.ClusterConfig{
		"confluent": {Provider: "confluent", Brokers: "broker-1:9092", Security: config.SecurityConfig{Enabled: true, Protocol: "SASL_SSL", APIKey: "key", APISecret: testPassword}},
		"azure":     {Provider: "azure", Brokers: "broker-1:9093", Security: config.SecurityConfig{ConnectionString: &conn}},
	}
	for name, cfg := range cases {
		t.Run(name, func(t *testing.T) {
			consumer, err := kafka.NewConsumer(cfg, "group", config.ReplicationConfig{BatchSize: 100}, "job", "topic")
			require.NoError(t, err)
			defer consumer.Client.Close()
			producer, err := kafka.NewProducer(cfg, config.ReplicationConfig{BatchSize: 100}, "job")
			require.NoError(t, err)
			defer producer.Client.Close()
			for _, cl := range []*kgo.Client{consumer.Client.(*kgo.Client), producer.Client.(*kgo.Client)} {
				assert.Equal(t, []string{"PLAIN"}, saslNames(t, cl))
				assert.True(t, usesTLS(cl))
			}
		})
	}
}

func TestClientConstructionNeverLogsSecrets(t *testing.T) {
	logFile := filepath.Join(t.TempDir(), "kaf-mirror.log")
	out, err := os.Create(logFile)
	require.NoError(t, err)
	logger.SetOutput(out)
	logger.SetLevel(logger.DEBUG)
	t.Cleanup(func() {
		logger.SetOutput(io.Discard)
		logger.SetLevel(logger.INFO)
		out.Close()
	})

	conn := "Endpoint=sb://example.servicebus.windows.net/;SharedAccessKey=" + testPassword
	cfgs := []config.ClusterConfig{
		plainProviderSASL("PLAIN"),
		plainProviderSASL("SCRAM-SHA-512"),
		{Provider: "confluent", Brokers: "broker-1:9092", Security: config.SecurityConfig{APIKey: "key", APISecret: testPassword}},
		{Provider: "azure", Brokers: "broker-1:9093", Security: config.SecurityConfig{ConnectionString: &conn}},
		{Provider: "redpanda", Brokers: "broker-1:9092", Security: config.SecurityConfig{Username: "u", Password: testPassword}},
	}
	for _, cfg := range cfgs {
		consumer, err := kafka.NewConsumer(cfg, "group", config.ReplicationConfig{BatchSize: 100}, "job", "topic")
		require.NoError(t, err)
		consumer.Client.Close()
		producer, err := kafka.NewProducer(cfg, config.ReplicationConfig{BatchSize: 100}, "job")
		require.NoError(t, err)
		producer.Client.Close()
		admin, err := kafka.NewAdminClient(cfg)
		require.NoError(t, err)
		admin.Close()
	}

	raw, err := os.ReadFile(logFile)
	require.NoError(t, err)
	logged := string(raw)
	assert.Contains(t, logged, "Kafka client security")
	assert.False(t, strings.Contains(logged, testPassword), "a secret was written to the log")
}
