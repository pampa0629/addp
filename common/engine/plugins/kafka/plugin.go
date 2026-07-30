package kafka

import (
	"context"
	"fmt"
	"strings"

	"github.com/addp/common/engine/plugin"
	"github.com/twmb/franz-go/pkg/kadm"
)

const (
	CatalogTermTopic = "topic"
	CatalogKindTopic = "topic"
)

type KafkaPlugin struct{}

func init() {
	plugin.Register(&KafkaPlugin{})
}

func (p *KafkaPlugin) Type() string { return "kafka" }

func (p *KafkaPlugin) DisplayName() string { return "Apache Kafka" }

func (p *KafkaPlugin) EngineOrigin() string { return "general" }

func (p *KafkaPlugin) DefaultPort() int { return 9092 }

func (p *KafkaPlugin) RequiredFields() []string { return []string{"bootstrap_servers"} }

func (p *KafkaPlugin) SensitiveFields() []string {
	return []string{"password", "tls_client_key"}
}

func (p *KafkaPlugin) ConnectionIdentityFields() []string {
	return []string{"bootstrap_servers"}
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
			CatalogModel: plugin.PtrCatalogModel(p.CatalogModel()),
			Catalog: &plugin.CatalogCapability{
				Supported: true,
				RealTime:  true,
				NodeKinds: []string{CatalogKindTopic},
			},
			Facts: &plugin.CatalogFactsCapability{Supported: true, NativeFacts: true},
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

func (p *KafkaPlugin) CatalogModel() plugin.CatalogModelSpec {
	return plugin.CatalogModelSpec{
		PathVersion: plugin.CatalogPathVersion,
		RootTerm:    plugin.CatalogTermService,
		Levels: []plugin.CatalogLevelSpec{
			{Term: CatalogTermTopic, Kinds: []string{CatalogKindTopic}, Role: plugin.CatalogRoleLeaf, I18nKey: plugin.CatalogTermI18nKey(CatalogTermTopic)},
		},
	}
}
