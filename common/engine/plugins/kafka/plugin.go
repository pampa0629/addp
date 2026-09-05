package kafka

import (
	"context"
	"fmt"
	"strings"

	"github.com/addp/common/engine/plugin"
	"github.com/twmb/franz-go/pkg/kadm"
)

const (
	EngineCatalogTermTopic = "topic"
	EngineCatalogKindTopic = "topic"
)

type KafkaPlugin struct{}

func init() {
	plugin.Register(&KafkaPlugin{})
}

func (p *KafkaPlugin) Type() string { return "kafka" }

func (p *KafkaPlugin) DisplayName() string { return "Apache Kafka" }

func (p *KafkaPlugin) EngineOrigin() string { return "general" }

func (p *KafkaPlugin) ConnectionSpec() plugin.ConnectionSpec {
	sasl := &plugin.ConnectionFieldCondition{Field: "security_protocol", Values: []string{"sasl_plaintext", "sasl_ssl"}}
	tls := &plugin.ConnectionFieldCondition{Field: "security_protocol", Values: []string{"ssl", "sasl_ssl"}}
	spec := plugin.NewConnectionSpec(
		plugin.ConnectionFieldSpec{Key: "bootstrap_servers", LabelKey: "storageEngine.bootstrapServers", Input: plugin.ConnectionFieldText, Required: true, Identity: true, PlaceholderKey: "storageEngine.bootstrapServersPlaceholder"},
		plugin.ConnectionFieldSpec{Key: "client_id", LabelKey: "storageEngine.clientIdOptional", Input: plugin.ConnectionFieldText, Default: "addp-system", PlaceholderKey: "storageEngine.clientIdPlaceholder"},
		plugin.ConnectionFieldSpec{Key: "security_protocol", LabelKey: "storageEngine.securityProtocol", Input: plugin.ConnectionFieldSelect, Default: "plaintext", Options: []plugin.ConnectionFieldOption{
			{Value: "plaintext", Label: "PLAINTEXT"}, {Value: "ssl", Label: "SSL"},
			{Value: "sasl_plaintext", Label: "SASL_PLAINTEXT"}, {Value: "sasl_ssl", Label: "SASL_SSL"},
		}},
		plugin.ConnectionFieldSpec{Key: "sasl_mechanism", LabelKey: "storageEngine.saslMechanism", Input: plugin.ConnectionFieldSelect, Required: true, Default: "scram-sha-256", VisibleWhen: sasl, Options: []plugin.ConnectionFieldOption{
			{Value: "plain", Label: "PLAIN"}, {Value: "scram-sha-256", Label: "SCRAM-SHA-256"}, {Value: "scram-sha-512", Label: "SCRAM-SHA-512"},
		}},
		plugin.ConnectionFieldSpec{Key: "username", LabelKey: "storageEngine.username", Input: plugin.ConnectionFieldText, Required: true, VisibleWhen: sasl},
		plugin.ConnectionFieldSpec{Key: "password", LabelKey: "storageEngine.password", Input: plugin.ConnectionFieldPassword, Required: true, Sensitive: true, VisibleWhen: sasl},
		plugin.ConnectionFieldSpec{Key: "tls_ca_cert", LabelKey: "storageEngine.tlsCaCertOptional", Input: plugin.ConnectionFieldTextarea, Rows: 3, PlaceholderKey: "storageEngine.pemPlaceholder", VisibleWhen: tls},
		plugin.ConnectionFieldSpec{Key: "tls_client_cert", LabelKey: "storageEngine.tlsClientCertOptional", Input: plugin.ConnectionFieldTextarea, Rows: 3, PlaceholderKey: "storageEngine.pemPlaceholder", VisibleWhen: tls},
		plugin.ConnectionFieldSpec{Key: "tls_client_key", LabelKey: "storageEngine.tlsClientKeyOptional", Input: plugin.ConnectionFieldTextarea, Sensitive: true, Rows: 3, PlaceholderKey: "storageEngine.pemPrivateKeyPlaceholder", VisibleWhen: tls},
		plugin.ConnectionFieldSpec{Key: "tls_insecure_skip_verify", LabelKey: "storageEngine.tlsInsecureSkipVerify", Input: plugin.ConnectionFieldBoolean, Default: false, VisibleWhen: tls},
	)
	spec.Constraints = []plugin.ConnectionConstraintSpec{{
		Kind: plugin.ConnectionConstraintAllOrNone, Fields: []string{"tls_client_cert", "tls_client_key"},
		MessageKey: "storageEngine.valid.inputTlsClientPair", When: tls,
	}}
	spec.DefaultPort = 9092
	return spec
}

func (p *KafkaPlugin) DefaultPort() int { return p.ConnectionSpec().DefaultPortValue() }

func (p *KafkaPlugin) RequiredFields() []string {
	return p.ConnectionSpec().UnconditionalRequiredFields()
}

func (p *KafkaPlugin) SensitiveFields() []string {
	return p.ConnectionSpec().SensitiveFields()
}

func (p *KafkaPlugin) ConnectionIdentityFields() []string {
	return p.ConnectionSpec().IdentityFields()
}

func (p *KafkaPlugin) ValidateConnectionInfo(connInfo plugin.ConnectionInfo) error {
	if strings.TrimSpace(plugin.GetString(connInfo, "bootstrap_servers")) == "" {
		return fmt.Errorf("missing required fields: bootstrap_servers")
	}
	_, err := kafkaClientOptions(connInfo)
	return err
}

func (p *KafkaPlugin) TestConnection(ctx context.Context, connInfo plugin.ConnectionInfo) error {
	client, err := newKafkaClient(connInfo)
	if err != nil {
		return err
	}
	defer client.Close()
	if err := client.Ping(ctx); err != nil {
		return fmt.Errorf("ping kafka brokers: %w", err)
	}
	if _, err := kadm.NewClient(client).ListTopics(ctx); err != nil {
		return fmt.Errorf("list kafka topics: %w", err)
	}
	return nil
}

func (p *KafkaPlugin) Capabilities() plugin.EngineCapabilities {
	return plugin.EngineCapabilities{
		SchemaVersion: plugin.CapabilitiesSchemaVersion,
		EngineType:    p.Type(),
		EngineFamily:  "event_stream",
		Storage: &plugin.StorageCapabilities{
			CatalogModel: plugin.PtrEngineCatalogModel(p.EngineCatalogModel()),
			Catalog: &plugin.EngineCatalogCapability{
				Supported: true,
				RealTime:  true,
				NodeKinds: []string{EngineCatalogKindTopic},
			},
			Facts: &plugin.EngineCatalogFactsCapability{Supported: true, NativeFacts: true},
			Store: &plugin.StoreCapability{
				ChangeStreamRead: &plugin.ChangeStreamReadCapability{
					Supported:     true,
					Partitioned:   true,
					Seek:          true,
					PauseResume:   true,
					PositionTypes: []string{"kafka_offset/v1"},
				},
			},
		},
	}
}

func (p *KafkaPlugin) StoreSemantics() plugin.StoreSemantics {
	return plugin.StoreSemanticsFromCapabilities(p.Capabilities())
}

func (p *KafkaPlugin) EngineCatalogModel() plugin.EngineCatalogModelSpec {
	return plugin.EngineCatalogModelSpec{
		PathVersion: plugin.EngineCatalogPathVersion,
		RootTerm:    plugin.EngineCatalogTermService,
		Levels: []plugin.EngineCatalogLevelSpec{
			{Term: EngineCatalogTermTopic, Kinds: []string{EngineCatalogKindTopic}, Role: plugin.EngineCatalogRoleLeaf, I18nKey: plugin.EngineCatalogTermI18nKey(EngineCatalogTermTopic)},
		},
	}
}
