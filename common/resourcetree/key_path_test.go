package resourcetree

import (
	"reflect"
	"testing"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/models"
)

func TestKeyPathIdentityAndDisplayAreSeparate(t *testing.T) {
	model := plugin.EngineCatalogModelSpec{PathVersion: plugin.EngineCatalogPathVersion, RootTerm: "server", Levels: []plugin.EngineCatalogLevelSpec{{Term: "key", Kinds: []string{"key"}, Role: plugin.EngineCatalogRoleLeaf}}}
	for _, raw := range []string{"", "a:b/c.d", " \n", "\x00\xff"} {
		name, err := plugin.EncodeKeyName([]byte(raw))
		if err != nil {
			t.Fatal(err)
		}
		loc := LocatorFromFullName(42, "redis", "key", name, nil)
		parsed, err := ParseURI(loc.ToURI())
		if err != nil || !reflect.DeepEqual(parsed.Path, []string{name}) {
			t.Fatal(parsed, err)
		}
		path, err := EngineCatalogPathFromLocator(model, parsed)
		if err != nil || len(path.Segments) != 2 || path.StringPath() != name {
			t.Fatal(path, err)
		}
		label := resourceLabel("key", name)
		if label != plugin.KeyDisplayName(name) || label == name {
			t.Fatal("encoded identity leaked as label", label)
		}
	}
}

func TestRedisMetaKeyTreeRetainsCatalogTypeWithUnknownDataType(t *testing.T) {
	builder := NewTreeBuilder()
	for _, raw := range []string{"addp:sample:counter", "addp:sample:binary\x00\xff"} {
		name, err := plugin.EncodeKeyName([]byte(raw))
		if err != nil {
			t.Fatal(err)
		}
		nodes := builder.ConvertMetaItemsForEngine("redis", []models.MetaItem{{
			ID: 9, EngineID: 42, NodeID: 7, Name: name, FullName: name, ItemType: "key",
			Attributes: map[string]interface{}{"item": map[string]interface{}{"layout": "single", "data_type": "unknown"}},
		}})
		tree := builder.convertMetaNode(&models.Engine{ID: 42, EngineType: "redis"}, nodes[0])
		locator, err := ParseURI(tree.Locator)
		if err != nil || locator.Type != TypeKey || locator.ItemID == nil || *locator.ItemID != 9 || !reflect.DeepEqual(locator.Path, []string{name}) {
			t.Fatalf("Redis key catalog identity changed: %s (%v)", tree.Locator, err)
		}
		if tree.Type != "key" || tree.Label != plugin.KeyDisplayName(name) || tree.Metadata["data_type"] != "unknown" {
			t.Fatalf("catalog type, label and content semantics were conflated: %#v", tree)
		}
	}
}
