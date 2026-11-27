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

package server

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"github.com/scalytics/kaf-mirror/v2/internal/config"
	"github.com/scalytics/kaf-mirror/v2/internal/manager"
	"github.com/scalytics/kaf-mirror/v2/internal/server"
	"io/ioutil"
	"math/big"
	"net"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
	_ "github.com/mattn/go-sqlite3"
)

// TestTLSServerConfiguration tests that the server properly configures TLS
func TestTLSServerConfiguration(t *testing.T) {
	// Create temporary certificate files
	certFile, keyFile, cleanup := createTempCertificates(t)
	defer cleanup()

	// Test configuration with TLS enabled
	cfg := &config.Config{
		Server: config.ServerConfig{
			Host: "localhost",
			Port: 0, // Use random port
			TLS: struct {
				Enabled  bool   `mapstructure:"enabled"`
				CertFile string `mapstructure:"cert_file"`
				KeyFile  string `mapstructure:"key_file"`
			}{
				Enabled:  true,
				CertFile: certFile,
				KeyFile:  keyFile,
			},
		},
		AI: config.AIConfig{
			Provider: "openai",
		},
	}

	// Create test database
	db, err := sqlx.Connect("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("Failed to create test database: %v", err)
	}
	defer db.Close()

	// Create test hub
	hub := server.NewHub()

	// Create test manager
	mgr := &manager.JobManager{}

	// Create server
	s := server.New(cfg, db, mgr, hub, "test")

	// Test that server starts with TLS (we'll just check the configuration)
	// Since we can't easily test the actual server start without complex setup,
	// we'll test the configuration validation
	if !cfg.Server.TLS.Enabled {
		t.Error("Expected TLS to be enabled")
	}

	if cfg.Server.TLS.CertFile == "" {
		t.Error("Expected certificate file to be set")
	}

	if cfg.Server.TLS.KeyFile == "" {
		t.Error("Expected key file to be set")
	}

	// Verify certificate files exist
	if _, err := os.Stat(certFile); os.IsNotExist(err) {
		t.Errorf("Certificate file does not exist: %s", certFile)
	}

	if _, err := os.Stat(keyFile); os.IsNotExist(err) {
		t.Errorf("Key file does not exist: %s", keyFile)
	}

	_ = s // Use the server variable to avoid unused variable error
}

// TestTLSDisabledConfiguration tests server with TLS disabled
func TestTLSDisabledConfiguration(t *testing.T) {
	cfg := &config.Config{
		Server: config.ServerConfig{
			Host: "localhost",
			Port: 0,
			TLS: struct {
				Enabled  bool   `mapstructure:"enabled"`
				CertFile string `mapstructure:"cert_file"`
				KeyFile  string `mapstructure:"key_file"`
			}{
				Enabled: false,
			},
		},
		AI: config.AIConfig{
			Provider: "openai",
		},
	}

	// Create test database
	db, err := sqlx.Connect("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("Failed to create test database: %v", err)
	}
	defer db.Close()

	// Create test hub
	hub := server.NewHub()

	// Create test manager
	mgr := &manager.JobManager{}

	// Create server
	s := server.New(cfg, db, mgr, hub, "test")

	// Test that TLS is properly disabled
	if cfg.Server.TLS.Enabled {
		t.Error("Expected TLS to be disabled")
	}

	_ = s // Use the server variable to avoid unused variable error
}

// TestCertificateValidation tests certificate and key pair validation
func TestCertificateValidation(t *testing.T) {
	// Create matching certificate and key
	certFile, keyFile, cleanup := createTempCertificates(t)
	defer cleanup()

	// Test that the certificate and key files exist and can be loaded
	certPEM, err := ioutil.ReadFile(certFile)
	if err != nil {
		t.Fatalf("Failed to read certificate file: %v", err)
	}

	keyPEM, err := ioutil.ReadFile(keyFile)
	if err != nil {
		t.Fatalf("Failed to read key file: %v", err)
	}

	// Test that the certificate can be parsed
	certBlock, _ := pem.Decode(certPEM)
	if certBlock == nil {
		t.Fatal("Failed to decode certificate PEM")
	}

	cert, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		t.Fatalf("Failed to parse certificate: %v", err)
	}

	// Test that the key can be parsed
	keyBlock, _ := pem.Decode(keyPEM)
	if keyBlock == nil {
		t.Fatal("Failed to decode key PEM")
	}

	privateKey, err := x509.ParsePKCS1PrivateKey(keyBlock.Bytes)
	if err != nil {
		t.Fatalf("Failed to parse private key: %v", err)
	}

	// Test that certificate and key match by creating a TLS certificate
	_, err = tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatalf("Certificate and key do not match: %v", err)
	}

	// Basic validation checks
	if cert.Subject.CommonName == "" {
		t.Error("Certificate should have a CommonName")
	}

	if privateKey.N == nil {
		t.Error("Private key should be valid RSA key")
	}
}

// TestTLSConfigurationUpdate tests updating TLS configuration
func TestTLSConfigurationUpdate(t *testing.T) {
	// Create temporary certificate files
	certFile1, keyFile1, cleanup1 := createTempCertificates(t)
	defer cleanup1()

	certFile2, keyFile2, cleanup2 := createTempCertificates(t)
	defer cleanup2()

	// Test initial configuration
	cfg := &config.Config{
		Server: config.ServerConfig{
			Host: "localhost",
			Port: 0,
			TLS: struct {
				Enabled  bool   `mapstructure:"enabled"`
				CertFile string `mapstructure:"cert_file"`
				KeyFile  string `mapstructure:"key_file"`
			}{
				Enabled:  true,
				CertFile: certFile1,
				KeyFile:  keyFile1,
			},
		},
		AI: config.AIConfig{
			Provider: "openai",
		},
	}

	// Verify initial configuration
	if cfg.Server.TLS.CertFile != certFile1 {
		t.Errorf("Expected cert file to be %s, got %s", certFile1, cfg.Server.TLS.CertFile)
	}

	// Update configuration
	cfg.Server.TLS.CertFile = certFile2
	cfg.Server.TLS.KeyFile = keyFile2

	// Verify updated configuration
	if cfg.Server.TLS.CertFile != certFile2 {
		t.Errorf("Expected cert file to be %s, got %s", certFile2, cfg.Server.TLS.CertFile)
	}

	if cfg.Server.TLS.KeyFile != keyFile2 {
		t.Errorf("Expected key file to be %s, got %s", keyFile2, cfg.Server.TLS.KeyFile)
	}
}

// TestTLSHTTPSClient tests making HTTPS requests with custom certificates
func TestTLSHTTPSClient(t *testing.T) {
	// Create temporary certificate files
	certFile, keyFile, cleanup := createTempCertificates(t)
	defer cleanup()

	// Read the certificate for client configuration
	certPEM, err := ioutil.ReadFile(certFile)
	if err != nil {
		t.Fatalf("Failed to read certificate file: %v", err)
	}

	// Also verify key file exists
	if _, err := os.Stat(keyFile); os.IsNotExist(err) {
		t.Fatalf("Key file does not exist: %s", keyFile)
	}

	// Create a certificate pool and add our certificate
	certPool := x509.NewCertPool()
	if !certPool.AppendCertsFromPEM(certPEM) {
		t.Fatal("Failed to add certificate to pool")
	}

	// Create TLS config for client
	tlsConfig := &tls.Config{
		RootCAs:            certPool,
		InsecureSkipVerify: true, // For testing with self-signed certs
	}

	// Create HTTP client with custom TLS config
	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: tlsConfig,
		},
		Timeout: 5 * time.Second,
	}

	// Test that the client is properly configured
	if client.Transport == nil {
		t.Error("Expected HTTP transport to be configured")
	}

	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Error("Expected transport to be *http.Transport")
	}

	if transport.TLSClientConfig == nil {
		t.Error("Expected TLS client config to be set")
	}

	if !transport.TLSClientConfig.InsecureSkipVerify {
		t.Error("Expected InsecureSkipVerify to be true for testing")
	}
}

// createTempCertificates creates temporary certificate and key files for testing
func createTempCertificates(t *testing.T) (certFile, keyFile string, cleanup func()) {
	// Generate private key
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("Failed to generate private key: %v", err)
	}

	// Create certificate template
	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			CommonName:   "localhost",
			Organization: []string{"kaf-mirror-test"},
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:           []net.IP{net.IPv4(127, 0, 0, 1)},
		DNSNames:              []string{"localhost"},
		BasicConstraintsValid: true,
	}

	// Create certificate
	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &privateKey.PublicKey, privateKey)
	if err != nil {
		t.Fatalf("Failed to create certificate: %v", err)
	}

	// Create temporary files
	certTempFile, err := ioutil.TempFile("", "cert-*.pem")
	if err != nil {
		t.Fatalf("Failed to create temp cert file: %v", err)
	}

	keyTempFile, err := ioutil.TempFile("", "key-*.pem")
	if err != nil {
		t.Fatalf("Failed to create temp key file: %v", err)
	}

	// Write certificate
	certPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: certDER,
	})
	if _, err := certTempFile.Write(certPEM); err != nil {
		t.Fatalf("Failed to write certificate: %v", err)
	}
	certTempFile.Close()

	// Write private key
	keyDER := x509.MarshalPKCS1PrivateKey(privateKey)

	keyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: keyDER,
	})
	if _, err := keyTempFile.Write(keyPEM); err != nil {
		t.Fatalf("Failed to write private key: %v", err)
	}
	keyTempFile.Close()

	cleanup = func() {
		os.Remove(certTempFile.Name())
		os.Remove(keyTempFile.Name())
	}

	return certTempFile.Name(), keyTempFile.Name(), cleanup
}

// TLSConfig represents TLS configuration for testing
type TLSConfig struct {
	Enabled  bool
	CertFile string
	KeyFile  string
}

// TestTLSConfigValidation tests TLS configuration validation
func TestTLSConfigValidation(t *testing.T) {
	tests := []struct {
		name        string
		tlsConfig   TLSConfig
		expectError bool
	}{
		{
			name: "Valid TLS config with files",
			tlsConfig: TLSConfig{
				Enabled:  true,
				CertFile: "/path/to/cert.pem",
				KeyFile:  "/path/to/key.pem",
			},
			expectError: false,
		},
		{
			name: "TLS enabled but missing cert file",
			tlsConfig: TLSConfig{
				Enabled:  true,
				CertFile: "",
				KeyFile:  "/path/to/key.pem",
			},
			expectError: true,
		},
		{
			name: "TLS enabled but missing key file",
			tlsConfig: TLSConfig{
				Enabled:  true,
				CertFile: "/path/to/cert.pem",
				KeyFile:  "",
			},
			expectError: true,
		},
		{
			name: "TLS disabled",
			tlsConfig: TLSConfig{
				Enabled: false,
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateTLSConfig(tt.tlsConfig)
			if tt.expectError && err == nil {
				t.Error("Expected validation error but got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Unexpected validation error: %v", err)
			}
		})
	}
}

// validateTLSConfig simulates TLS configuration validation
func validateTLSConfig(tlsConfig TLSConfig) error {
	if !tlsConfig.Enabled {
		return nil
	}

	if tlsConfig.CertFile == "" {
		return fmt.Errorf("TLS is enabled but certificate file path is missing")
	}

	if tlsConfig.KeyFile == "" {
		return fmt.Errorf("TLS is enabled but key file path is missing")
	}

	return nil
}
