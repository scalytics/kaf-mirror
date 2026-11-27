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
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/scalytics/kaf-mirror/v2/internal/config"
	"github.com/scalytics/kaf-mirror/v2/internal/database"
	"github.com/scalytics/kaf-mirror/v2/internal/kafka"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kfake"
)

type testPKI struct {
	caFile     string
	certFile   string
	keyFile    string
	serverCert tls.Certificate
	caPool     *x509.CertPool
}

func writePEM(t *testing.T, path, typ string, der []byte) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: typ, Bytes: der}), 0600))
}

func issue(t *testing.T, tmpl, parent *x509.Certificate, parentKey *ecdsa.PrivateKey) (*x509.Certificate, *ecdsa.PrivateKey, []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	if parent == nil {
		parent, parentKey = tmpl, key
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, parent, &key.PublicKey, parentKey)
	require.NoError(t, err)
	cert, err := x509.ParseCertificate(der)
	require.NoError(t, err)
	return cert, key, der
}

func newTestPKI(t *testing.T) testPKI {
	t.Helper()
	dir := t.TempDir()
	now := time.Now()
	ca, caKey, caDER := issue(t, &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "kaf-mirror test CA"},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
	}, nil, nil)
	_, serverKey, serverDER := issue(t, &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "broker"},
		NotBefore:    now.Add(-time.Hour),
		NotAfter:     now.Add(time.Hour),
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		DNSNames:     []string{"localhost"},
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}, ca, caKey)
	_, clientKey, clientDER := issue(t, &x509.Certificate{
		SerialNumber: big.NewInt(3),
		Subject:      pkix.Name{CommonName: "kaf-mirror"},
		NotBefore:    now.Add(-time.Hour),
		NotAfter:     now.Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}, ca, caKey)

	p := testPKI{
		caFile:   filepath.Join(dir, "ca.crt"),
		certFile: filepath.Join(dir, "client.crt"),
		keyFile:  filepath.Join(dir, "client.key"),
		caPool:   x509.NewCertPool(),
	}
	writePEM(t, p.caFile, "CERTIFICATE", caDER)
	writePEM(t, p.certFile, "CERTIFICATE", clientDER)
	clientKeyDER, err := x509.MarshalECPrivateKey(clientKey)
	require.NoError(t, err)
	writePEM(t, p.keyFile, "EC PRIVATE KEY", clientKeyDER)
	p.caPool.AddCert(ca)
	p.serverCert = tls.Certificate{Certificate: [][]byte{serverDER}, PrivateKey: serverKey}
	return p
}

func startMTLSCluster(t *testing.T, p testPKI) *kfake.Cluster {
	t.Helper()
	c, err := kfake.NewCluster(
		kfake.NumBrokers(1),
		kfake.SeedTopics(1, "tls-topic"),
		kfake.TLS(&tls.Config{
			MinVersion:   tls.VersionTLS12,
			Certificates: []tls.Certificate{p.serverCert},
			ClientAuth:   tls.RequireAndVerifyClientCert,
			ClientCAs:    p.caPool,
		}),
	)
	require.NoError(t, err)
	t.Cleanup(c.Close)
	return c
}

func tlsClusterConfig(c *kfake.Cluster, sec config.SecurityConfig) config.ClusterConfig {
	sec.Protocol = "SSL"
	return config.ClusterConfig{Provider: "plain", Brokers: strings.Join(c.ListenAddrs(), ","), Security: sec}
}

func testConnection(cfg config.ClusterConfig) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return kafka.TestConnection(ctx, cfg)
}

func TestTLSUsesCAFileAndClientCertificate(t *testing.T) {
	p := newTestPKI(t)
	c := startMTLSCluster(t, p)

	assert.NoError(t, testConnection(tlsClusterConfig(c, config.SecurityConfig{CAFile: p.caFile, CertFile: p.certFile, KeyFile: p.keyFile})))
	assert.Error(t, testConnection(tlsClusterConfig(c, config.SecurityConfig{CertFile: p.certFile, KeyFile: p.keyFile})), "a private CA must not be trusted without ca_file")
	assert.Error(t, testConnection(tlsClusterConfig(c, config.SecurityConfig{CAFile: p.caFile})), "a broker that requires mTLS must reject a client without a certificate")
}

func TestTLSFileSettingsFromStoredCluster(t *testing.T) {
	p := newTestPKI(t)
	c := startMTLSCluster(t, p)
	raw, err := json.Marshal(map[string]string{"protocol": "SSL", "ca_file": p.caFile, "cert_file": p.certFile, "key_file": p.keyFile})
	require.NoError(t, err)
	cfg := database.ClusterConfigFromRow(database.KafkaCluster{Name: "tls", Provider: "plain", Brokers: strings.Join(c.ListenAddrs(), ","), SecurityConfig: string(raw)})
	assert.Equal(t, p.caFile, cfg.Security.CAFile)
	assert.Equal(t, p.certFile, cfg.Security.CertFile)
	assert.Equal(t, p.keyFile, cfg.Security.KeyFile)
	assert.NoError(t, testConnection(cfg))
}

func TestTLSFileSettingsAreValidated(t *testing.T) {
	p := newTestPKI(t)
	base := config.ClusterConfig{Provider: "plain", Brokers: "127.0.0.1:9093"}

	certOnly := base
	certOnly.Security = config.SecurityConfig{Protocol: "SSL", CertFile: p.certFile}
	assert.ErrorContains(t, kafka.ValidateSecurity(certOnly), "cert_file and key_file must be set together")

	keyOnly := base
	keyOnly.Security = config.SecurityConfig{Protocol: "SSL", KeyFile: p.keyFile}
	assert.ErrorContains(t, kafka.ValidateSecurity(keyOnly), "cert_file and key_file must be set together")

	plaintext := base
	plaintext.Security = config.SecurityConfig{Protocol: "PLAINTEXT", CAFile: p.caFile}
	assert.ErrorContains(t, kafka.ValidateSecurity(plaintext), "need protocol SSL or SASL_SSL")

	missing := base
	missing.Security = config.SecurityConfig{Protocol: "SSL", CAFile: filepath.Join(t.TempDir(), "missing.crt")}
	assert.NoError(t, kafka.ValidateSecurity(missing))
	_, err := kafka.ClientOptions(missing)
	assert.ErrorContains(t, err, "failed to read ca_file")

	notPEM := filepath.Join(t.TempDir(), "ca.crt")
	require.NoError(t, os.WriteFile(notPEM, []byte("not a certificate"), 0600))
	bad := base
	bad.Security = config.SecurityConfig{Protocol: "SSL", CAFile: notPEM}
	_, err = kafka.ClientOptions(bad)
	assert.ErrorContains(t, err, "ca_file has no PEM certificate")

	wrongKey := base
	wrongKey.Security = config.SecurityConfig{Protocol: "SSL", CertFile: p.certFile, KeyFile: p.caFile}
	_, err = kafka.ClientOptions(wrongKey)
	assert.ErrorContains(t, err, "failed to load cert_file and key_file")

	confluent := config.ClusterConfig{Provider: "confluent", Brokers: "pkc-1.confluent.cloud:9092", Security: config.SecurityConfig{APIKey: "k", APISecret: "s", CAFile: p.caFile}}
	_, err = kafka.ClientOptions(confluent)
	assert.NoError(t, err)
}

func TestTLSFileSymlinkToPEMIsAccepted(t *testing.T) {
	p := newTestPKI(t)
	dir := t.TempDir()
	linkCA := filepath.Join(dir, "ca.crt")
	linkCert := filepath.Join(dir, "client.crt")
	linkKey := filepath.Join(dir, "client.key")
	require.NoError(t, os.Symlink(p.caFile, linkCA))
	require.NoError(t, os.Symlink(p.certFile, linkCert))
	require.NoError(t, os.Symlink(p.keyFile, linkKey))
	cfg := config.ClusterConfig{Provider: "plain", Brokers: "127.0.0.1:9093", Security: config.SecurityConfig{Protocol: "SSL", CAFile: linkCA, CertFile: linkCert, KeyFile: linkKey}}
	assert.NoError(t, kafka.ValidateSecurity(cfg))
	_, err := kafka.ClientOptions(cfg)
	assert.NoError(t, err)
	fp, ok := kafka.ProbeFingerprint(cfg)
	require.True(t, ok)
	require.NotEmpty(t, fp)
}
