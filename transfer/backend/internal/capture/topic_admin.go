package capture

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	engineplugin "github.com/addp/common/engine/plugin"
	kafkaplugin "github.com/addp/common/engine/plugins/kafka"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kgo"
)

type TopicSpec struct {
	Name              string
	Partitions        int32
	ReplicationFactor int16
	CleanupPolicy     string
	RetentionMillis   int64
	RetentionBytes    int64
}

type TopicControl interface {
	EnsureTopic(ctx context.Context, spec TopicSpec) error
	EnsureAccess(ctx context.Context, topic, group string) error
	EnsureSchemaHistoryAccess(ctx context.Context, topic string) error
	DeleteTopic(ctx context.Context, topic string) error
	DeleteConsumerGroup(ctx context.Context, group string) error
	DeleteAccess(ctx context.Context, topic, group string) error
	DeleteSchemaHistoryAccess(ctx context.Context, topic string) error
	Close()
}

type KafkaAdminConfig struct {
	BootstrapServers  string
	Username          string
	Password          string
	SecurityProtocol  string
	SASLMechanism     string
	TLSCACertFile     string
	TLSInsecure       bool
	ConnectPrincipal  string
	TransferPrincipal string
}

type KafkaTopicAdmin struct {
	client            *kgo.Client
	admin             *kadm.Client
	connectPrincipal  string
	transferPrincipal string
}

func NewKafkaTopicAdmin(config KafkaAdminConfig) (*KafkaTopicAdmin, error) {
	brokers := splitNonEmpty(config.BootstrapServers)
	if len(brokers) == 0 {
		return nil, fmt.Errorf("Infra Kafka bootstrap servers are required")
	}
	info := engineplugin.ConnectionInfo{
		"bootstrap_servers": strings.Join(brokers, ","),
		"security_protocol": config.SecurityProtocol,
		"username":          config.Username,
		"password":          config.Password,
		"sasl_mechanism":    config.SASLMechanism,
		"client_id":         "addp-transfer-capture-control",
	}
	protocol := strings.ToLower(strings.TrimSpace(config.SecurityProtocol))
	if protocol == "" {
		protocol = "sasl_plaintext"
		info["security_protocol"] = protocol
	}
	if (protocol == "sasl_plaintext" || protocol == "sasl_ssl") && strings.TrimSpace(config.SASLMechanism) == "" {
		info["sasl_mechanism"] = "scram-sha-256"
	}
	if protocol == "ssl" || protocol == "sasl_ssl" {
		if strings.TrimSpace(config.TLSCACertFile) != "" {
			pem, err := os.ReadFile(config.TLSCACertFile)
			if err != nil {
				return nil, fmt.Errorf("read Infra Kafka CA certificate: %w", err)
			}
			info["tls_ca_cert"] = string(pem)
		}
		info["tls_insecure_skip_verify"] = config.TLSInsecure
	}
	client, err := kafkaplugin.NewClient(info)
	if err != nil {
		return nil, err
	}
	connectPrincipal := strings.TrimSpace(config.ConnectPrincipal)
	if connectPrincipal == "" {
		connectPrincipal = "connect"
	}
	transferPrincipal := strings.TrimSpace(config.TransferPrincipal)
	if transferPrincipal == "" {
		transferPrincipal = "transfer"
	}
	return &KafkaTopicAdmin{
		client: client, admin: kadm.NewClient(client),
		connectPrincipal: connectPrincipal, transferPrincipal: transferPrincipal,
	}, nil
}

func (a *KafkaTopicAdmin) EnsureTopic(ctx context.Context, spec TopicSpec) error {
	if spec.Partitions != 1 {
		return fmt.Errorf("database CDC v1 topic must have exactly one partition")
	}
	cleanupPolicy := strings.ToLower(strings.TrimSpace(spec.CleanupPolicy))
	if cleanupPolicy == "" {
		cleanupPolicy = "delete"
	}
	if cleanupPolicy != "delete" && cleanupPolicy != "compact" {
		return fmt.Errorf("unsupported database CDC topic cleanup policy %q", cleanupPolicy)
	}
	configs := map[string]*string{
		"cleanup.policy": stringPtr(cleanupPolicy),
	}
	if cleanupPolicy == "delete" {
		if spec.RetentionMillis == 0 || spec.RetentionMillis < -1 {
			return fmt.Errorf("database CDC topic retention must be positive or -1")
		}
		configs["retention.ms"] = stringPtr(strconv.FormatInt(spec.RetentionMillis, 10))
	}
	if cleanupPolicy == "delete" && spec.RetentionBytes > 0 {
		configs["retention.bytes"] = stringPtr(strconv.FormatInt(spec.RetentionBytes, 10))
	}
	responses, err := a.admin.CreateTopics(ctx, spec.Partitions, spec.ReplicationFactor, configs, spec.Name)
	if err != nil {
		return fmt.Errorf("create Infra Kafka CDC topic: %w", err)
	}
	response, ok := responses[spec.Name]
	if !ok {
		return fmt.Errorf("create Infra Kafka CDC topic returned no result")
	}
	if response.Err != nil && response.Err != kerr.TopicAlreadyExists {
		return fmt.Errorf("create Infra Kafka CDC topic %q: %w", spec.Name, response.Err)
	}
	return a.validateTopic(ctx, spec)
}

func (a *KafkaTopicAdmin) validateTopic(ctx context.Context, spec TopicSpec) error {
	details, err := a.admin.ListTopics(ctx, spec.Name)
	if err != nil {
		return fmt.Errorf("describe Infra Kafka CDC topic: %w", err)
	}
	detail, ok := details[spec.Name]
	if !ok || detail.Err != nil || len(detail.Partitions) != int(spec.Partitions) {
		return fmt.Errorf("Infra Kafka CDC topic %q does not match required one-partition contract", spec.Name)
	}
	configs, err := a.admin.DescribeTopicConfigs(ctx, spec.Name)
	if err != nil {
		return fmt.Errorf("describe Infra Kafka CDC topic config: %w", err)
	}
	resource, err := configs.On(spec.Name, nil)
	if err != nil || resource.Err != nil {
		return fmt.Errorf("describe Infra Kafka CDC topic %q config: %v", spec.Name, firstError(err, resource.Err))
	}
	values := make(map[string]string, len(resource.Configs))
	for _, config := range resource.Configs {
		values[config.Key] = config.MaybeValue()
	}
	cleanupPolicy := strings.ToLower(strings.TrimSpace(spec.CleanupPolicy))
	if cleanupPolicy == "" {
		cleanupPolicy = "delete"
	}
	if values["cleanup.policy"] != cleanupPolicy || (cleanupPolicy == "delete" && values["retention.ms"] != strconv.FormatInt(spec.RetentionMillis, 10)) {
		return fmt.Errorf("Infra Kafka CDC topic %q has incompatible cleanup or retention config", spec.Name)
	}
	if cleanupPolicy == "delete" && spec.RetentionBytes > 0 && values["retention.bytes"] != strconv.FormatInt(spec.RetentionBytes, 10) {
		return fmt.Errorf("Infra Kafka CDC topic %q has incompatible retention.bytes", spec.Name)
	}
	return nil
}

func (a *KafkaTopicAdmin) EnsureSchemaHistoryAccess(ctx context.Context, topic string) error {
	builder := kadm.NewACLs().Topics(topic).ResourcePatternType(kadm.ACLPatternLiteral).
		Allow(a.connectPrincipal).Operations(kadm.OpRead, kadm.OpWrite, kadm.OpDescribe)
	builder.PrefixUser()
	results, err := a.admin.CreateACLs(ctx, builder)
	if err != nil {
		return fmt.Errorf("create MySQL schema history ACL: %w", err)
	}
	for _, result := range results {
		if result.Err != nil {
			return fmt.Errorf("create MySQL schema history ACL: %w", result.Err)
		}
	}
	return nil
}

func (a *KafkaTopicAdmin) EnsureAccess(ctx context.Context, topic, group string) error {
	builders := []*kadm.ACLBuilder{
		kadm.NewACLs().Topics(topic).ResourcePatternType(kadm.ACLPatternLiteral).Allow(a.connectPrincipal).Operations(kadm.OpWrite, kadm.OpDescribe),
		kadm.NewACLs().Topics(topic).ResourcePatternType(kadm.ACLPatternLiteral).Allow(a.transferPrincipal).Operations(kadm.OpRead, kadm.OpDescribe),
		kadm.NewACLs().Groups(group).ResourcePatternType(kadm.ACLPatternLiteral).Allow(a.transferPrincipal).Operations(kadm.OpRead, kadm.OpDescribe),
	}
	for _, builder := range builders {
		builder.PrefixUser()
		results, err := a.admin.CreateACLs(ctx, builder)
		if err != nil {
			return fmt.Errorf("create CDC resource ACL: %w", err)
		}
		for _, result := range results {
			if result.Err != nil {
				return fmt.Errorf("create CDC resource ACL for %s: %w", result.Name, result.Err)
			}
		}
	}
	return nil
}

func (a *KafkaTopicAdmin) DeleteTopic(ctx context.Context, topic string) error {
	responses, err := a.admin.DeleteTopics(ctx, topic)
	if err != nil {
		return err
	}
	response, ok := responses[topic]
	if !ok || response.Err == kerr.UnknownTopicOrPartition {
		return nil
	}
	return response.Err
}

func (a *KafkaTopicAdmin) DeleteConsumerGroup(ctx context.Context, group string) error {
	responses, err := a.admin.DeleteGroups(ctx, group)
	if err != nil {
		return err
	}
	response, ok := responses[group]
	if !ok || response.Err == kerr.GroupIDNotFound {
		return nil
	}
	return response.Err
}

func (a *KafkaTopicAdmin) DeleteAccess(ctx context.Context, topic, group string) error {
	builders := []*kadm.ACLBuilder{
		kadm.NewACLs().Topics(topic).ResourcePatternType(kadm.ACLPatternLiteral).Allow(a.connectPrincipal).AllowHosts().Operations(kadm.OpAny),
		kadm.NewACLs().Topics(topic).ResourcePatternType(kadm.ACLPatternLiteral).Allow(a.transferPrincipal).AllowHosts().Operations(kadm.OpAny),
		kadm.NewACLs().Groups(group).ResourcePatternType(kadm.ACLPatternLiteral).Allow(a.transferPrincipal).AllowHosts().Operations(kadm.OpAny),
	}
	for _, builder := range builders {
		builder.PrefixUser()
		results, err := a.admin.DeleteACLs(ctx, builder)
		if err != nil {
			return fmt.Errorf("delete CDC resource ACL: %w", err)
		}
		for _, result := range results {
			if result.Err != nil {
				return fmt.Errorf("delete CDC resource ACL: %w", result.Err)
			}
		}
	}
	return nil
}

func (a *KafkaTopicAdmin) DeleteSchemaHistoryAccess(ctx context.Context, topic string) error {
	builder := kadm.NewACLs().Topics(topic).ResourcePatternType(kadm.ACLPatternLiteral).
		Allow(a.connectPrincipal).AllowHosts().Operations(kadm.OpAny)
	builder.PrefixUser()
	results, err := a.admin.DeleteACLs(ctx, builder)
	if err != nil {
		return fmt.Errorf("delete MySQL schema history ACL: %w", err)
	}
	for _, result := range results {
		if result.Err != nil {
			return fmt.Errorf("delete MySQL schema history ACL: %w", result.Err)
		}
	}
	return nil
}

func (a *KafkaTopicAdmin) Close() {
	if a != nil && a.client != nil {
		a.client.Close()
	}
}

func splitNonEmpty(value string) []string {
	var result []string
	for _, item := range strings.Split(value, ",") {
		if item = strings.TrimSpace(item); item != "" {
			result = append(result, item)
		}
	}
	return result
}

func stringPtr(value string) *string { return &value }

func firstError(values ...error) error {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}
