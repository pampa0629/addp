package scanruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/addp/common/datatype"
	es "github.com/addp/common/engine/plugins/elasticsearch"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"

	"github.com/addp/common/engine/plugin"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/metatest"
	"github.com/addp/meta/internal/models"
	metaRepo "github.com/addp/meta/internal/repository"
)

func TestDirectLeafRuntimeScansRootLeavesAndDeletesMissingItems(t *testing.T) {
	db := metatest.OpenMetadataDB(t)
	repo := metaRepo.NewScanRepository(db)
	enginePlugin := &directLeafRuntimeTestPlugin{
		entries: []plugin.EngineCatalogEntry{
			directLeafRuntimeTestEntry(41, "orders", "topic"),
			directLeafRuntimeTestEntry(41, "events", ""),
			{Name: "ignored", Role: plugin.EngineCatalogRoleBranch},
		},
	}
	runtime := NewDirectLeafRuntime(slog.New(slog.NewTextHandler(io.Discard, nil)), repo)
	resource := &commonModels.Engine{ID: 41, Name: "Business Kafka", EngineType: enginePlugin.Type()}

	items, err := runtime.ScanRoot(context.Background(), enginePlugin, resource, 1, models.ScannedDepthBasic, true)
	if err != nil {
		t.Fatalf("ScanRoot() error = %v", err)
	}
	if items != 2 {
		t.Fatalf("items = %d, want 2", items)
	}

	var root models.MetaNode
	if err := db.Where("tenant_id = ? AND engine_id = ? AND parent_node_id IS NULL", 1, resource.ID).First(&root).Error; err != nil {
		t.Fatalf("query root node: %v", err)
	}
	if root.NodeType != plugin.EngineCatalogTermService || root.FullName != "" {
		t.Fatalf("root type/full_name = %q/%q, want service/empty", root.NodeType, root.FullName)
	}
	stats, err := metaRepo.QueryNodeStatistics(db, 1, resource.ID, []uint{root.ID})
	if err != nil || len(stats) != 1 {
		t.Fatalf("statistics: %v, %v", stats, err)
	}
	if root.ScanStatus != "completed" || stats[0].ItemCount != 2 || root.ScannedDepth != models.ScannedDepthBasic {
		t.Fatalf("root status/count/depth = %q/%d/%q, want completed/2/basic", root.ScanStatus, stats[0].ItemCount, root.ScannedDepth)
	}

	orders, ok, err := repo.FindItemByFullName(1, resource.ID, "orders")
	if err != nil || !ok {
		t.Fatalf("orders item lookup = %#v/%v/%v", orders, ok, err)
	}
	if orders.NodeID != root.ID || orders.ItemType != "topic" || orders.Name != "orders" {
		t.Fatalf("orders identity = %#v", orders)
	}
	itemAttrs, ok := orders.Attributes["item"].(map[string]interface{})
	if !ok || itemAttrs["layout"] != "single" || itemAttrs["data_type"] != "unknown" {
		t.Fatalf("orders item attributes = %#v, want single/unknown", orders.Attributes)
	}

	events, ok, err := repo.FindItemByFullName(1, resource.ID, "events")
	if err != nil || !ok {
		t.Fatalf("events item lookup = %#v/%v/%v", events, ok, err)
	}
	if events.ItemType != "topic" {
		t.Fatalf("events item_type = %q, want fallback kind topic", events.ItemType)
	}

	enginePlugin.entries = []plugin.EngineCatalogEntry{directLeafRuntimeTestEntry(41, "orders", "topic")}
	items, err = runtime.ScanRoot(context.Background(), enginePlugin, resource, 1, models.ScannedDepthDeep, true)
	if err != nil {
		t.Fatalf("second ScanRoot() error = %v", err)
	}
	if items != 1 {
		t.Fatalf("second items = %d, want 1", items)
	}
	if _, ok, err := repo.FindItemByFullName(1, resource.ID, "events"); err != nil || ok {
		t.Fatalf("missing events lookup ok/error = %v/%v, want false/nil", ok, err)
	}
	if err := db.Where("tenant_id = ? AND engine_id = ? AND parent_node_id IS NULL", 1, resource.ID).First(&root).Error; err != nil {
		t.Fatalf("query second root node: %v", err)
	}
	stats, err = metaRepo.QueryNodeStatistics(db, 1, resource.ID, []uint{root.ID})
	if err != nil || len(stats) != 1 {
		t.Fatalf("statistics: %v, %v", stats, err)
	}
	if stats[0].ItemCount != 1 || root.ScannedDepth != models.ScannedDepthDeep {
		t.Fatalf("second root count/depth = %d/%q, want 1/deep", stats[0].ItemCount, root.ScannedDepth)
	}
}

type directLeafRuntimeTestPlugin struct {
	entries []plugin.EngineCatalogEntry
}

func (p *directLeafRuntimeTestPlugin) Type() string         { return "direct-leaf-runtime-test" }
func (p *directLeafRuntimeTestPlugin) DisplayName() string  { return "Direct Leaf Runtime Test" }
func (p *directLeafRuntimeTestPlugin) EngineOrigin() string { return "general" }
func (p *directLeafRuntimeTestPlugin) DefaultPort() int     { return 0 }
func (p *directLeafRuntimeTestPlugin) RequiredFields() []string {
	return nil
}
func (p *directLeafRuntimeTestPlugin) SensitiveFields() []string {
	return nil
}
func (p *directLeafRuntimeTestPlugin) ValidateConnectionInfo(plugin.ConnectionInfo) error {
	return nil
}
func (p *directLeafRuntimeTestPlugin) TestConnection(context.Context, plugin.ConnectionInfo) error {
	return nil
}
func (p *directLeafRuntimeTestPlugin) Capabilities() plugin.EngineCapabilities {
	return plugin.EngineCapabilities{}
}
func (p *directLeafRuntimeTestPlugin) EngineCatalogModel() plugin.EngineCatalogModelSpec {
	return plugin.EngineCatalogModelSpec{
		PathVersion: plugin.EngineCatalogPathVersion,
		RootTerm:    plugin.EngineCatalogTermService,
		Levels: []plugin.EngineCatalogLevelSpec{
			{Term: "topic", Kinds: []string{"topic"}, Role: plugin.EngineCatalogRoleLeaf},
		},
	}
}
func (p *directLeafRuntimeTestPlugin) ListChildren(context.Context, plugin.ConnectionInfo, plugin.EngineCatalogPath, plugin.ListOptions) ([]plugin.EngineCatalogEntry, error) {
	return append([]plugin.EngineCatalogEntry(nil), p.entries...), nil
}
func (p *directLeafRuntimeTestPlugin) ResolvePath(context.Context, plugin.ConnectionInfo, plugin.EngineCatalogPath) (*plugin.EngineCatalogEntry, error) {
	return nil, nil
}

func directLeafRuntimeTestEntry(engineID uint, name, term string) plugin.EngineCatalogEntry {
	path := plugin.EngineCatalogRootPath(plugin.EngineCatalogModelSpec{
		PathVersion: plugin.EngineCatalogPathVersion,
		RootTerm:    plugin.EngineCatalogTermService,
	}, engineID)
	path.Segments = append(path.Segments, plugin.EngineCatalogSegment{Term: "topic", Kind: "topic", Name: name})
	return plugin.EngineCatalogEntry{
		Name: name,
		Path: path,
		Term: term,
		Kind: "topic",
		Role: plugin.EngineCatalogRoleLeaf,
	}
}

var _ plugin.EngineCatalogModelProvider = (*directLeafRuntimeTestPlugin)(nil)
var _ plugin.EngineCatalogProvider = (*directLeafRuntimeTestPlugin)(nil)

func TestIntegrationElasticsearchMappingScan(t *testing.T) {
	if os.Getenv("ADDP_ELASTICSEARCH_INTEGRATION") != "1" {
		t.Skip("owned T2 required")
	}
	db := metatest.OpenMetadataDB(t)
	repo := metaRepo.NewScanRepository(db)
	p := &es.ElasticsearchPlugin{}
	resource := &commonModels.Engine{ID: 91, Name: "ES T2", EngineType: p.Type(), ConnectionInfo: commonModels.ConnectionInfo{"endpoint": os.Getenv("ELASTICSEARCH_ENDPOINT"), "user": os.Getenv("ELASTICSEARCH_READER_USER"), "password": os.Getenv("ELASTICSEARCH_READER_PASSWORD")}}
	runtime := NewDirectLeafRuntime(slog.New(slog.NewTextHandler(io.Discard, nil)), repo)
	count, err := runtime.ScanRoot(context.Background(), p, resource, 1, models.ScannedDepthDeep, true)
	if err != nil || count != 2 {
		t.Fatal(count, err)
	}
	item, exists, err := repo.FindItemByFullName(1, 91, "addp_empty.v1")
	if err != nil || !exists || item.ItemType != "index" {
		t.Fatal(item, exists, err)
	}
	encoded, _ := json.Marshal(item.Attributes)
	if !strings.Contains(string(encoded), `"schema_type":"mapping"`) || !strings.Contains(string(encoded), `"is_sampled":false`) || !strings.Contains(string(encoded), `"fields"`) {
		t.Fatal(string(encoded))
	}
}

type directLeafFactsTestPlugin struct {
	directLeafRuntimeTestPlugin
	fail bool
}

func (p *directLeafFactsTestPlugin) DescribeEngineCatalogFacts(context.Context, plugin.ConnectionInfo, plugin.EngineCatalogPath, plugin.EngineCatalogFactsOptions) (*plugin.EngineCatalogFacts, error) {
	if p.fail {
		return nil, fmt.Errorf("facts unavailable")
	}
	return &plugin.EngineCatalogFacts{Table: &datatype.TableInfo{Fields: []datatype.FieldInfo{{Name: "id", Type: datatype.FieldTypeBigInt}}, Native: map[string]interface{}{"schema_type": "mapping", "is_sampled": false}}}, nil
}
func TestDirectLeafFactsFailurePreservesPreviouslyScannedItems(t *testing.T) {
	db := metatest.OpenMetadataDB(t)
	repo := metaRepo.NewScanRepository(db)
	p := &directLeafFactsTestPlugin{directLeafRuntimeTestPlugin: directLeafRuntimeTestPlugin{entries: []plugin.EngineCatalogEntry{directLeafRuntimeTestEntry(41, "orders", "topic"), directLeafRuntimeTestEntry(41, "events", "topic")}}}
	runtime := NewDirectLeafRuntime(slog.New(slog.NewTextHandler(io.Discard, nil)), repo)
	resource := &commonModels.Engine{ID: 41, Name: "facts test", EngineType: p.Type()}
	if _, err := runtime.ScanRoot(context.Background(), p, resource, 1, models.ScannedDepthDeep, true); err != nil {
		t.Fatal(err)
	}
	p.entries = p.entries[:1]
	p.fail = true
	if _, err := runtime.ScanRoot(context.Background(), p, resource, 1, models.ScannedDepthDeep, true); err == nil {
		t.Fatal("facts failure accepted")
	}
	for _, name := range []string{"orders", "events"} {
		if _, exists, err := repo.FindItemByFullName(1, 41, name); err != nil || !exists {
			t.Fatal("existing item deleted during failed scan", name, err)
		}
	}
}
