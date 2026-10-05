package resourcetree

import (
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/models"
	"testing"
)

func TestKeyspaceIdentitySeparatesMembers(t *testing.T) {
	model := plugin.EngineCatalogModelSpec{PathVersion: plugin.EngineCatalogPathVersion, RootTerm: "server", Levels: []plugin.EngineCatalogLevelSpec{{Term: "keyspace", Kinds: []string{"keyspace"}, Role: plugin.EngineCatalogRoleLeaf}}}
	builder := NewTreeBuilder()
	nodes := builder.ConvertMetaItemsForEngine("redis", []models.MetaItem{{ID: 9, EngineID: 42, NodeID: 7, Name: "keyspace", FullName: "keyspace", ItemType: "keyspace", Attributes: map[string]interface{}{"item": map[string]interface{}{"layout": "single", "data_type": "key_value"}}}})
	tree := builder.convertMetaNode(&models.Engine{ID: 42, EngineType: "redis"}, nodes[0])
	loc, err := ParseURI(tree.Locator)
	if err != nil || loc.Type != TypeKeyspace || loc.ItemID == nil || *loc.ItemID != 9 {
		t.Fatal(tree, err)
	}
	path, err := EngineCatalogPathFromLocator(model, loc)
	if err != nil || path.StringPath() != "keyspace" || tree.Metadata["data_type"] != "key_value" {
		t.Fatal(tree, err)
	}
	old := &ResourceLocator{EngineID: 42, Type: "key", Path: []string{"k:YQ"}}
	if _, err := EngineCatalogPathFromLocator(model, old); err == nil {
		t.Fatal("old per-key locator accepted")
	}
}
