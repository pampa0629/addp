package kafka

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"strings"

	"github.com/addp/common/engine/plugin"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/sasl"
	"github.com/twmb/franz-go/pkg/sasl/plain"
	"github.com/twmb/franz-go/pkg/sasl/scram"
)

func newKafkaClient(connInfo plugin.ConnectionInfo, extra ...kgo.Opt) (*kgo.Client, error) {
	return NewClient(connInfo, extra...)
}

// NewClient builds a Kafka API client from the shared connection contract.
func NewClient(connInfo plugin.ConnectionInfo, extra ...kgo.Opt) (*kgo.Client, error) {
	opts, err := kafkaClientOptions(connInfo)
	if err != nil {
		return nil, err
	}
	opts = append(opts, extra...)
	client, err := kgo.NewClient(opts...)
	if err != nil {
		return nil, fmt.Errorf("create kafka client: %w", err)
	}
	return client, nil
}

func kafkaClientOptions(connInfo plugin.ConnectionInfo) ([]kgo.Opt, error) {
	brokers := splitKafkaBootstrapServers(plugin.GetString(connInfo, "bootstrap_servers"))
	if len(brokers) == 0 {
		return nil, fmt.Errorf("kafka bootstrap_servers is required")
	}
	opts := []kgo.Opt{kgo.SeedBrokers(brokers...)}
	if clientID := strings.TrimSpace(plugin.GetString(connInfo, "client_id")); clientID != "" {
		opts = append(opts, kgo.ClientID(clientID))
	}

	protocol := strings.ToLower(strings.TrimSpace(plugin.GetString(connInfo, "security_protocol")))
	if protocol == "" {
		protocol = "plaintext"
	}
	if protocol != "plaintext" && protocol != "ssl" && protocol != "sasl_plaintext" && protocol != "sasl_ssl" {
		return nil, fmt.Errorf("unsupported kafka security_protocol %q", protocol)
	}
	if protocol == "ssl" || protocol == "sasl_ssl" {
		tlsConfig, err := kafkaTLSConfig(connInfo)
		if err != nil {
			return nil, err
		}
		opts = append(opts, kgo.DialTLSConfig(tlsConfig))
	}
	if protocol == "sasl_plaintext" || protocol == "sasl_ssl" {
		mechanism, err := kafkaSASLMechanism(connInfo)
		if err != nil {
			return nil, err
		}
		opts = append(opts, kgo.SASL(mechanism))
	}
	return opts, nil
}

func splitKafkaBootstrapServers(value string) []string {
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	seen := map[string]struct{}{}
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if _, ok := seen[part]; ok {
			continue
		}
		seen[part] = struct{}{}
		result = append(result, part)
	}
	return result
}

func kafkaTLSConfig(connInfo plugin.ConnectionInfo) (*tls.Config, error) {
	config := &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: plugin.GetBool(connInfo, "tls_insecure_skip_verify")} //nolint:gosec -- explicit engine configuration
	if caPEM := strings.TrimSpace(plugin.GetString(connInfo, "tls_ca_cert")); caPEM != "" {
		pool, err := x509.SystemCertPool()
		if err != nil || pool == nil {
			pool = x509.NewCertPool()
		}
		if !pool.AppendCertsFromPEM([]byte(caPEM)) {
			return nil, fmt.Errorf("kafka tls_ca_cert does not contain a valid certificate")
		}
		config.RootCAs = pool
	}
	certPEM := strings.TrimSpace(plugin.GetString(connInfo, "tls_client_cert"))
	keyPEM := strings.TrimSpace(plugin.GetString(connInfo, "tls_client_key"))
	if (certPEM == "") != (keyPEM == "") {
		return nil, fmt.Errorf("kafka tls_client_cert and tls_client_key must be provided together")
	}
	if certPEM != "" {
		certificate, err := tls.X509KeyPair([]byte(certPEM), []byte(keyPEM))
		if err != nil {
			return nil, fmt.Errorf("parse kafka client certificate: %w", err)
		}
		config.Certificates = []tls.Certificate{certificate}
	}
	return config, nil
}

func kafkaSASLMechanism(connInfo plugin.ConnectionInfo) (sasl.Mechanism, error) {
	username := strings.TrimSpace(plugin.GetString(connInfo, "username"))
	password := plugin.GetString(connInfo, "password")
	if username == "" || password == "" {
		return nil, fmt.Errorf("kafka SASL requires username and password")
	}
	mechanism := strings.ToLower(strings.TrimSpace(plugin.GetString(connInfo, "sasl_mechanism")))
	switch mechanism {
	case "plain":
		return plain.Plain(func(context.Context) (plain.Auth, error) { return plain.Auth{User: username, Pass: password}, nil }), nil
	case "scram-sha-256":
		return scram.Sha256(func(context.Context) (scram.Auth, error) { return scram.Auth{User: username, Pass: password}, nil }), nil
	case "scram-sha-512":
		return scram.Sha512(func(context.Context) (scram.Auth, error) { return scram.Auth{User: username, Pass: password}, nil }), nil
	default:
		return nil, fmt.Errorf("unsupported kafka sasl_mechanism %q", mechanism)
	}
}
