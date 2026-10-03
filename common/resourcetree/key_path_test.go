package resourcetree

import (
	"reflect"
	"testing"

	"github.com/addp/common/engine/plugin"
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
