package preview

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/manager/internal/models"
)

func TestEventStreamTopicPreviewReadsTailWithoutCommittedConsumerPosition(t *testing.T) {
	const engineType = "event-stream-topic-preview-test"
	previous, previousErr := plugin.Get(engineType)
	t.Cleanup(func() {
		if previousErr == nil {
			plugin.Register(previous)
			return
		}
		plugin.Unregister(engineType)
	})

	enginePlugin := &recordingEventStreamPreviewPlugin{
		engineType: engineType,
		facts: &plugin.EngineCatalogFacts{
			Kind: "topic",
			Topic: &plugin.TopicFacts{
				PartitionCount: 2,
				Partitions: []plugin.TopicPartitionFacts{
					{Partition: 0, EarliestOffset: 10, LatestOffset: 30},
					{Partition: 1, EarliestOffset: 20, LatestOffset: 25},
				},
			},
		},
		reader: &recordingEventStreamReader{batch: &plugin.ChangeRecordBatch{
			Records: []plugin.ChangeRecord{{
				Topic:     "orders",
				Partition: "0",
				Offset:    29,
				Timestamp: time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC),
				Key:       []byte(`{"id": 7}`),
				Value:     []byte(`{"status":"paid"}`),
				Headers:   []plugin.ChangeRecordHeader{{Key: "source", Value: []byte("checkout")}},
			}},
		}},
	}
	plugin.Register(enginePlugin)

	provider := NewEventStreamTopicPreviewProvider()
	path := eventStreamPreviewTestPath(42, "orders")
	result, err := provider.Preview(context.Background(), &PreviewRequest{
		Engine: &models.Engine{
			ID:             42,
			EngineType:     engineType,
			ConnectionInfo: models.ConnectionInfo{"bootstrap_servers": "kafka:9092"},
		},
		ItemType:     "topic",
		PageSize:     10,
		ProviderPath: path,
	})
	if err != nil {
		t.Fatalf("Preview() error = %v", err)
	}
	if result.PreviewKind != "event_stream_topic" || result.Mode != PreviewModeTable {
		t.Fatalf("preview identity = %q/%q", result.Mode, result.PreviewKind)
	}
	if !reflect.DeepEqual(result.Columns, []string{"partition", "offset", "timestamp", "key", "value", "headers"}) {
		t.Fatalf("columns = %#v", result.Columns)
	}
	if len(result.Rows) != 1 || result.Rows[0]["offset"] != int64(29) {
		t.Fatalf("rows = %#v", result.Rows)
	}
	if value, ok := result.Rows[0]["value"].(map[string]interface{}); !ok || value["status"] != "paid" {
		t.Fatalf("decoded value = %#v", result.Rows[0]["value"])
	}
	if len(enginePlugin.openOptions) != 1 {
		t.Fatalf("OpenChangeStream calls = %d, want 1", len(enginePlugin.openOptions))
	}
	opts := enginePlugin.openOptions[0]
	if !strings.HasPrefix(opts.ConsumerGroup, "addp-manager-preview-") {
		t.Fatalf("consumer group = %q", opts.ConsumerGroup)
	}
	if opts.InitialPosition != plugin.ChangeStreamInitialEarliest {
		t.Fatalf("initial position = %q", opts.InitialPosition)
	}
	if got := opts.CommittedPositions["0"].Values["next_offset"]; got != "25" {
		t.Fatalf("partition 0 start offset = %q, want 25", got)
	}
	if got := opts.CommittedPositions["1"].Values["next_offset"]; got != "20" {
		t.Fatalf("partition 1 start offset = %q, want 20", got)
	}
	if !enginePlugin.reader.closed {
		t.Fatal("preview reader was not closed")
	}
}

type recordingEventStreamPreviewPlugin struct {
	engineType  string
	facts       *plugin.EngineCatalogFacts
	reader      *recordingEventStreamReader
	openOptions []plugin.ChangeStreamReadOptions
}

func (p *recordingEventStreamPreviewPlugin) Type() string         { return p.engineType }
func (p *recordingEventStreamPreviewPlugin) DisplayName() string  { return "event stream preview test" }
func (p *recordingEventStreamPreviewPlugin) EngineOrigin() string { return "general" }
func (p *recordingEventStreamPreviewPlugin) DefaultPort() int     { return 0 }
func (p *recordingEventStreamPreviewPlugin) RequiredFields() []string {
	return nil
}
func (p *recordingEventStreamPreviewPlugin) SensitiveFields() []string { return nil }
func (p *recordingEventStreamPreviewPlugin) ValidateConnectionInfo(plugin.ConnectionInfo) error {
	return nil
}
func (p *recordingEventStreamPreviewPlugin) TestConnection(context.Context, plugin.ConnectionInfo) error {
	return nil
}
func (p *recordingEventStreamPreviewPlugin) Capabilities() plugin.EngineCapabilities {
	return plugin.EngineCapabilities{SchemaVersion: plugin.CapabilitiesSchemaVersion, EngineType: p.Type()}
}
func (p *recordingEventStreamPreviewPlugin) StoreSemantics() plugin.StoreSemantics {
	return plugin.StoreSemantics{}
}
func (p *recordingEventStreamPreviewPlugin) DescribeEngineCatalogFacts(context.Context, plugin.ConnectionInfo, plugin.EngineCatalogPath, plugin.EngineCatalogFactsOptions) (*plugin.EngineCatalogFacts, error) {
	return p.facts, nil
}
func (p *recordingEventStreamPreviewPlugin) OpenChangeStream(_ context.Context, _ plugin.ConnectionInfo, _ plugin.EngineCatalogPath, opts plugin.ChangeStreamReadOptions) (plugin.ChangeStreamReader, error) {
	p.openOptions = append(p.openOptions, opts)
	return p.reader, nil
}

type recordingEventStreamReader struct {
	batch  *plugin.ChangeRecordBatch
	closed bool
}

func (r *recordingEventStreamReader) Poll(context.Context, int) (*plugin.ChangeRecordBatch, error) {
	return r.batch, nil
}
func (r *recordingEventStreamReader) PositionRanges(context.Context) ([]plugin.ChangeStreamPositionRange, error) {
	return nil, nil
}
func (r *recordingEventStreamReader) Assignments() []string { return nil }
func (r *recordingEventStreamReader) Pause(context.Context, []string) error {
	return nil
}
func (r *recordingEventStreamReader) Resume(context.Context, []string) error {
	return nil
}
func (r *recordingEventStreamReader) Close(context.Context) error {
	r.closed = true
	return nil
}

func eventStreamPreviewTestPath(engineID uint, topic string) plugin.EngineCatalogPath {
	model := plugin.EngineCatalogModelSpec{
		PathVersion: plugin.EngineCatalogPathVersion,
		RootTerm:    plugin.EngineCatalogTermService,
		Levels: []plugin.EngineCatalogLevelSpec{{
			Term: "topic", Kinds: []string{"topic"}, Role: plugin.EngineCatalogRoleLeaf,
		}},
	}
	path := plugin.EngineCatalogRootPath(model, engineID)
	path.Segments = append(path.Segments, plugin.EngineCatalogSegment{Term: "topic", Kind: "topic", Name: topic})
	return path
}
