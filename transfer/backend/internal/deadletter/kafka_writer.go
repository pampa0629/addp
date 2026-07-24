package deadletter

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/sasl/plain"
)

const topicPrefix = "__addp_dlq."

type KafkaWriterConfig struct {
	ConnectionInfo    engineplugin.ConnectionInfo
	RetentionMillis   int64
	RetentionBytes    int64
	ReplicationFactor int16
}

type KafkaPayloadWriter struct {
	client            *kgo.Client
	admin             *kadm.Client
	retentionMillis   int64
	retentionBytes    int64
	replicationFactor int16
	mu                sync.Mutex
	ensuredTopics     map[string]struct{}
}

func NewKafkaPayloadWriter(config KafkaWriterConfig) (*KafkaPayloadWriter, error) {
	if config.RetentionMillis <= 0 || config.ReplicationFactor <= 0 {
		return nil, fmt.Errorf("dead-letter Kafka retention and replication factor must be greater than zero")
	}
	client, err := newKafkaClient(config.ConnectionInfo)
	if err != nil {
		return nil, err
	}
	return &KafkaPayloadWriter{
		client: client, admin: kadm.NewClient(client), retentionMillis: config.RetentionMillis,
		retentionBytes: config.RetentionBytes, replicationFactor: config.ReplicationFactor,
		ensuredTopics: map[string]struct{}{},
	}, nil
}

func TopicName(tenantID, taskID uint) (string, error) {
	if tenantID == 0 || taskID == 0 {
		return "", fmt.Errorf("dead-letter topic requires tenant and task IDs")
	}
	return fmt.Sprintf("%s%d.%d", topicPrefix, tenantID, taskID), nil
}

func (w *KafkaPayloadWriter) Write(ctx context.Context, topic, identity string, detectedAt time.Time, payload []byte) (PayloadReference, error) {
	if w == nil || w.client == nil || w.admin == nil {
		return PayloadReference{}, fmt.Errorf("dead-letter Kafka writer is not configured")
	}
	if !strings.HasPrefix(topic, topicPrefix) || strings.TrimSpace(identity) == "" || len(payload) == 0 {
		return PayloadReference{}, fmt.Errorf("dead-letter Kafka topic, identity, and payload are required")
	}
	if err := w.ensureTopic(ctx, topic); err != nil {
		return PayloadReference{}, err
	}
	record := &kgo.Record{
		Topic: topic, Key: []byte(identity), Value: append([]byte(nil), payload...), Timestamp: detectedAt.UTC(),
		Headers: []kgo.RecordHeader{{Key: "addp-schema", Value: []byte(EnvelopeSchemaV1)}},
	}
	result := w.client.ProduceSync(ctx, record)[0]
	if result.Err != nil {
		return PayloadReference{}, fmt.Errorf("produce Infra Kafka dead-letter record: %w", result.Err)
	}
	return PayloadReference{Topic: result.Record.Topic, Partition: result.Record.Partition, Offset: result.Record.Offset}, nil
}

func (w *KafkaPayloadWriter) ensureTopic(ctx context.Context, topic string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if _, ok := w.ensuredTopics[topic]; ok {
		return nil
	}
	configs := map[string]*string{
		"cleanup.policy": stringPointer("compact,delete"),
		"retention.ms":   stringPointer(strconv.FormatInt(w.retentionMillis, 10)),
	}
	if w.retentionBytes > 0 {
		configs["retention.bytes"] = stringPointer(strconv.FormatInt(w.retentionBytes, 10))
	}
	responses, err := w.admin.CreateTopics(ctx, 1, w.replicationFactor, configs, topic)
	if err != nil {
		return fmt.Errorf("create Infra Kafka dead-letter topic: %w", err)
	}
	response, ok := responses[topic]
	if !ok {
		return fmt.Errorf("create Infra Kafka dead-letter topic returned no result")
	}
	if response.Err != nil && response.Err != kerr.TopicAlreadyExists {
		return fmt.Errorf("create Infra Kafka dead-letter topic %q: %w", topic, response.Err)
	}
	if err := w.validateTopic(ctx, topic); err != nil {
		return err
	}
	w.ensuredTopics[topic] = struct{}{}
	return nil
}

func (w *KafkaPayloadWriter) validateTopic(ctx context.Context, topic string) error {
	details, err := w.admin.ListTopics(ctx, topic)
	if err != nil {
		return fmt.Errorf("describe Infra Kafka dead-letter topic: %w", err)
	}
	detail, ok := details[topic]
	if !ok || detail.Err != nil || len(detail.Partitions) != 1 {
		return fmt.Errorf("Infra Kafka dead-letter topic %q must have exactly one partition", topic)
	}
	configs, err := w.admin.DescribeTopicConfigs(ctx, topic)
	if err != nil {
		return fmt.Errorf("describe Infra Kafka dead-letter topic config: %w", err)
	}
	resource, err := configs.On(topic, nil)
	if err != nil || resource.Err != nil {
		return fmt.Errorf("describe Infra Kafka dead-letter topic %q config: %v", topic, firstNonNil(err, resource.Err))
	}
	values := make(map[string]string, len(resource.Configs))
	for _, config := range resource.Configs {
		values[config.Key] = config.MaybeValue()
	}
	if !samePolicy(values["cleanup.policy"], "compact", "delete") || values["retention.ms"] != strconv.FormatInt(w.retentionMillis, 10) {
		return fmt.Errorf("Infra Kafka dead-letter topic %q has incompatible cleanup or retention config", topic)
	}
	if w.retentionBytes > 0 && values["retention.bytes"] != strconv.FormatInt(w.retentionBytes, 10) {
		return fmt.Errorf("Infra Kafka dead-letter topic %q has incompatible retention.bytes", topic)
	}
	return nil
}

func (w *KafkaPayloadWriter) Close() {
	if w != nil && w.client != nil {
		w.client.Close()
	}
}

func newKafkaClient(info engineplugin.ConnectionInfo, extra ...kgo.Opt) (*kgo.Client, error) {
	brokers := splitNonEmpty(engineplugin.GetString(info, "bootstrap_servers"))
	if len(brokers) == 0 {
		return nil, fmt.Errorf("Infra Kafka bootstrap servers are required")
	}
	opts := []kgo.Opt{kgo.SeedBrokers(brokers...), kgo.RequiredAcks(kgo.AllISRAcks())}
	if clientID := strings.TrimSpace(engineplugin.GetString(info, "client_id")); clientID != "" {
		opts = append(opts, kgo.ClientID(clientID+"-dead-letter"))
	}
	protocol := strings.ToLower(strings.TrimSpace(engineplugin.GetString(info, "security_protocol")))
	if protocol == "" {
		protocol = "sasl_plaintext"
	}
	if protocol == "sasl_plaintext" || protocol == "sasl_ssl" {
		username := strings.TrimSpace(engineplugin.GetString(info, "username"))
		password := engineplugin.GetString(info, "password")
		if username == "" || password == "" || strings.ToLower(strings.TrimSpace(engineplugin.GetString(info, "sasl_mechanism"))) != "plain" {
			return nil, fmt.Errorf("Infra Kafka dead-letter client requires SASL PLAIN credentials")
		}
		opts = append(opts, kgo.SASL(plain.Plain(func(context.Context) (plain.Auth, error) {
			return plain.Auth{User: username, Pass: password}, nil
		})))
	}
	if protocol == "ssl" || protocol == "sasl_ssl" {
		tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: engineplugin.GetBool(info, "tls_insecure_skip_verify")} //nolint:gosec -- explicit deployment option
		if caPEM := strings.TrimSpace(engineplugin.GetString(info, "tls_ca_cert")); caPEM != "" {
			pool, err := x509.SystemCertPool()
			if err != nil || pool == nil {
				pool = x509.NewCertPool()
			}
			if !pool.AppendCertsFromPEM([]byte(caPEM)) {
				return nil, fmt.Errorf("Infra Kafka CA certificate is invalid")
			}
			tlsConfig.RootCAs = pool
		}
		opts = append(opts, kgo.DialTLSConfig(tlsConfig))
	}
	if protocol != "plaintext" && protocol != "ssl" && protocol != "sasl_plaintext" && protocol != "sasl_ssl" {
		return nil, fmt.Errorf("unsupported Infra Kafka security protocol %q", protocol)
	}
	opts = append(opts, extra...)
	client, err := kgo.NewClient(opts...)
	if err != nil {
		return nil, fmt.Errorf("create Infra Kafka dead-letter client: %w", err)
	}
	return client, nil
}

func splitNonEmpty(value string) []string {
	var values []string
	for _, item := range strings.Split(value, ",") {
		if item = strings.TrimSpace(item); item != "" {
			values = append(values, item)
		}
	}
	return values
}

func samePolicy(actual string, expected ...string) bool {
	set := map[string]struct{}{}
	for _, item := range strings.Split(actual, ",") {
		set[strings.TrimSpace(item)] = struct{}{}
	}
	if len(set) != len(expected) {
		return false
	}
	for _, item := range expected {
		if _, ok := set[item]; !ok {
			return false
		}
	}
	return true
}

func stringPointer(value string) *string { return &value }

func firstNonNil(values ...error) error {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}
