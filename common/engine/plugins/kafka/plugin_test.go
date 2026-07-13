package kafka

import (
	"reflect"
	"testing"

	"github.com/addp/common/engine/plugin"
)

func TestKafkaCapabilitiesMatchProviders(t *testing.T) {
	p := &KafkaPlugin{}
	if err := plugin.ValidatePluginCapabilities(p); err != nil {
		t.Fatalf("ValidatePluginCapabilities() error = %v", err)
	}
	caps := p.Capabilities()
	if caps.Storage == nil || caps.Storage.Store == nil || caps.Storage.Store.ChangeStreamRead == nil || !caps.Storage.Store.ChangeStreamRead.Supported {
		t.Fatalf("Kafka capabilities do not declare change_stream_read: %#v", caps)
	}
}

func TestKafkaCatalogModelIsServiceTopic(t *testing.T) {
	model := (&KafkaPlugin{}).CatalogModel()
	if model.RootTerm != plugin.CatalogTermService || len(model.Levels) != 1 {
		t.Fatalf("CatalogModel() = %#v, want service -> topic", model)
	}
	level := model.Levels[0]
	if level.Term != CatalogTermTopic || level.Role != plugin.CatalogRoleLeaf || !reflect.DeepEqual(level.Kinds, []string{CatalogKindTopic}) {
		t.Fatalf("topic level = %#v", level)
	}
}

func TestKafkaTopicPathPreservesDotsAsSingleSegment(t *testing.T) {
	root := plugin.CatalogRootPath((&KafkaPlugin{}).CatalogModel(), 30)
	entry := kafkaTopicEntry(root, "orders.events")
	topic, err := kafkaTopicFromPath(entry.Path)
	if err != nil {
		t.Fatalf("kafkaTopicFromPath() error = %v", err)
	}
	if topic != "orders.events" || len(plugin.CatalogPathWithoutRoot(entry.Path).Segments) != 1 {
		t.Fatalf("topic=%q path=%#v", topic, entry.Path)
	}
}

func TestKafkaConnectionValidation(t *testing.T) {
	p := &KafkaPlugin{}
	if err := p.ValidateConnectionInfo(plugin.ConnectionInfo{}); err == nil {
		t.Fatal("ValidateConnectionInfo() succeeded without bootstrap_servers")
	}
	if err := p.ValidateConnectionInfo(plugin.ConnectionInfo{"bootstrap_servers": "localhost:9092", "security_protocol": "sasl_ssl", "sasl_mechanism": "plain"}); err == nil {
		t.Fatal("ValidateConnectionInfo() succeeded without SASL credentials")
	}
	if err := p.ValidateConnectionInfo(plugin.ConnectionInfo{"bootstrap_servers": "localhost:9092"}); err != nil {
		t.Fatalf("ValidateConnectionInfo(plaintext) error = %v", err)
	}
}

func TestParseKafkaPartitions(t *testing.T) {
	got, err := parseKafkaPartitions([]string{"0", "12"})
	if err != nil || !reflect.DeepEqual(got, []int32{0, 12}) {
		t.Fatalf("parseKafkaPartitions() = %#v, %v", got, err)
	}
	if _, err := parseKafkaPartitions([]string{"partition-0"}); err == nil {
		t.Fatal("parseKafkaPartitions() accepted non-numeric partition")
	}
}
