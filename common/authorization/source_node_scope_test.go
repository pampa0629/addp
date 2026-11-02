package authorization

import (
	"testing"

	"github.com/addp/common/engine/plugin"
)

func TestNodeScopeCoversCurrentAndFutureTypedItems(t *testing.T) {
	schema := plugin.TabularNamespacePath(9, "schema", " exact.schema/ ")
	root := plugin.EngineCatalogRootPath(plugin.TabularCatalogModel("schema"), 9)
	for _, name := range []string{"current", "newly.discovered", "table/with/slash"} {
		item := plugin.TabularItemPath(9, "schema", " exact.schema/ ", name)
		if !(SourceNodeScope{Target: schema}).Covers(item) || !(SourceNodeScope{Target: root, Recursive: true}).Covers(item) || (SourceNodeScope{Target: root}).Covers(item) {
			t.Fatal("scope expanded or depended on a frozen item list")
		}
	}
	for _, item := range []plugin.EngineCatalogPath{
		plugin.TabularItemPath(10, "schema", " exact.schema/ ", "current"),
		plugin.TabularItemPath(9, "schema", " exact.schema/ suffix", "current"),
		schema, root,
	} {
		if (SourceNodeScope{Target: schema, Recursive: true}).Covers(item) {
			t.Fatal("scope admitted itself, a sibling or a foreign engine")
		}
	}
}
