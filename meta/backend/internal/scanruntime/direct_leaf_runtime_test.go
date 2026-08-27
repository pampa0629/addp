package scanruntime

import (
	"context"
	"io"
	"log/slog"
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
	if root.ScanStatus != "completed" || root.ItemCount != 2 || root.ScannedDepth != models.ScannedDepthBasic {
		t.Fatalf("root status/count/depth = %q/%d/%q, want completed/2/basic", root.ScanStatus, root.ItemCount, root.ScannedDepth)
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
	if root.ItemCount != 1 || root.ScannedDepth != models.ScannedDepthDeep {
		t.Fatalf("second root count/depth = %d/%q, want 1/deep", root.ItemCount, root.ScannedDepth)
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
