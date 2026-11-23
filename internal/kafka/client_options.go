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
	"crypto/tls"
	"errors"
	"fmt"
	"strings"
	"time"

	"kaf-mirror/internal/config"
	"kaf-mirror/pkg/logger"

	krbclient "github.com/jcmturner/gokrb5/v8/client"
	krbconfig "github.com/jcmturner/gokrb5/v8/config"
	"github.com/jcmturner/gokrb5/v8/keytab"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/sasl"
	"github.com/twmb/franz-go/pkg/sasl/kerberos"
	"github.com/twmb/franz-go/pkg/sasl/plain"
	"github.com/twmb/franz-go/pkg/sasl/scram"
)

const (
	MechanismPlain       = "PLAIN"
	MechanismScramSHA256 = "SCRAM-SHA-256"
	MechanismScramSHA512 = "SCRAM-SHA-512"
	MechanismGSSAPI      = "GSSAPI"

	defaultKerberosService = "kafka"
	adminRequestOverhead   = 30 * time.Second
)

type securityPlan struct {
	tls       bool
	mechanism string
	user      string
	pass      string
	kerberos  config.KerberosConfig
}

func ClientOptions(cfg config.ClusterConfig) ([]kgo.Opt, error) {
	brokers := splitBrokers(cfg.Brokers)
	if len(brokers) == 0 {
		return nil, errors.New("no brokers configured")
	}
	plan, err := planSecurity(cfg)
	if err != nil {
		return nil, err
	}
	opts := []kgo.Opt{kgo.SeedBrokers(brokers...)}
	if plan.tls {
		opts = append(opts, kgo.DialTLSConfig(&tls.Config{MinVersion: tls.VersionTLS12}))
	}
	if plan.mechanism != "" {
		mech, err := buildMechanism(plan)
		if err != nil {
			return nil, err
		}
		opts = append(opts, kgo.SASL(mech))
	}
	logger.Debug("Kafka client security: provider=%s, tls=%t, sasl=%s", cfg.Provider, plan.tls, plan.mechanism)
	return opts, nil
}

func ValidateSecurity(cfg config.ClusterConfig) error {
	_, err := planSecurity(cfg)
	return err
}

func adminOptions(cfg config.ClusterConfig) ([]kgo.Opt, error) {
	opts, err := ClientOptions(cfg)
	if err != nil {
		return nil, err
	}
	return append(opts, kgo.RequestTimeoutOverhead(adminRequestOverhead)), nil
}

func splitBrokers(brokers string) []string {
	var out []string
	for _, b := range strings.Split(brokers, ",") {
		if b = strings.TrimSpace(b); b != "" {
			out = append(out, b)
		}
	}
	return out
}

func planSecurity(cfg config.ClusterConfig) (securityPlan, error) {
	sec := cfg.Security
	plan := securityPlan{tls: strings.Contains(strings.ToUpper(sec.Protocol), "SSL")}
	switch strings.ToLower(cfg.Provider) {
	case "confluent":
		if sec.APIKey == "" || sec.APISecret == "" {
			return plan, errors.New("confluent provider requires API key and secret")
		}
		plan.tls = true
		plan.mechanism, plan.user, plan.pass = MechanismPlain, sec.APIKey, sec.APISecret
		return plan, nil
	case "azure":
		conn := sec.APISecret
		if sec.ConnectionString != nil && *sec.ConnectionString != "" {
			conn = *sec.ConnectionString
		}
		if conn == "" {
			return plan, errors.New("azure provider requires a connection string")
		}
		plan.tls = true
		plan.mechanism, plan.user, plan.pass = MechanismPlain, "$ConnectionString", conn
		return plan, nil
	case "redpanda":
		if !(sec.Enabled && sec.SASLMechanism != "") && sec.Username != "" && sec.Password != "" {
			plan.tls = true
			plan.mechanism, plan.user, plan.pass = MechanismScramSHA256, sec.Username, sec.Password
			return plan, nil
		}
	}
	if !sec.Enabled || sec.SASLMechanism == "" {
		return plan, nil
	}
	plan.mechanism = strings.ToUpper(strings.TrimSpace(sec.SASLMechanism))
	switch plan.mechanism {
	case MechanismPlain, MechanismScramSHA256, MechanismScramSHA512:
		if sec.Username == "" || sec.Password == "" {
			return plan, fmt.Errorf("username and password are required for SASL %s", plan.mechanism)
		}
		plan.user, plan.pass = sec.Username, sec.Password
	case MechanismGSSAPI:
		krb := sec.Kerberos
		var missing []string
		if krb.Principal == "" {
			missing = append(missing, "kerberos.principal")
		}
		if krb.KeytabPath == "" {
			missing = append(missing, "kerberos.keytab_path")
		}
		if krb.Krb5ConfPath == "" {
			missing = append(missing, "kerberos.krb5_conf_path")
		}
		if len(missing) > 0 {
			return plan, fmt.Errorf("SASL GSSAPI requires %s", strings.Join(missing, ", "))
		}
		if krb.ServiceName == "" {
			krb.ServiceName = defaultKerberosService
		}
		plan.kerberos = krb
	default:
		return plan, fmt.Errorf("unsupported SASL mechanism: %s", sec.SASLMechanism)
	}
	return plan, nil
}

func buildMechanism(plan securityPlan) (sasl.Mechanism, error) {
	switch plan.mechanism {
	case MechanismPlain:
		return plain.Auth{User: plan.user, Pass: plan.pass}.AsMechanism(), nil
	case MechanismScramSHA256:
		return scram.Auth{User: plan.user, Pass: plan.pass}.AsSha256Mechanism(), nil
	case MechanismScramSHA512:
		return scram.Auth{User: plan.user, Pass: plan.pass}.AsSha512Mechanism(), nil
	case MechanismGSSAPI:
		cl, err := kerberosClient(plan.kerberos)
		if err != nil {
			return nil, err
		}
		return kerberos.Auth{Client: cl, Service: plan.kerberos.ServiceName}.AsMechanismWithClose(), nil
	}
	return nil, fmt.Errorf("unsupported SASL mechanism: %s", plan.mechanism)
}

func kerberosClient(krb config.KerberosConfig) (*krbclient.Client, error) {
	conf, err := krbconfig.Load(krb.Krb5ConfPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load krb5.conf %s: %w", krb.Krb5ConfPath, err)
	}
	kt, err := keytab.Load(krb.KeytabPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load keytab %s: %w", krb.KeytabPath, err)
	}
	user, realm := krb.Principal, ""
	if i := strings.LastIndex(user, "@"); i >= 0 {
		user, realm = user[:i], user[i+1:]
	}
	if realm == "" {
		realm = conf.LibDefaults.DefaultRealm
	}
	if user == "" || realm == "" {
		return nil, fmt.Errorf("kerberos principal %q needs a realm, either user@REALM or default_realm in krb5.conf", krb.Principal)
	}
	return krbclient.NewWithKeytab(user, realm, kt, conf, krbclient.DisablePAFXFAST(true)), nil
}
