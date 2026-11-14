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

func TestNFSNodeScopeUsesOpaqueDirectorySegments(t *testing.T) {
	scope := SourceNodeScope{Target: plugin.FileDirectoryPath(9, "data/ a.b ")}
	for _, p := range []plugin.EngineCatalogPath{plugin.FileItemPath(9, "data/ a.b /new.csv"), plugin.FileDirectoryPath(9, "data/ a.b /new.gdb")} {
		if !scope.Covers(p) {
			t.Fatalf("direct NFS item not covered: %+v", p)
		}
	}
	nested := plugin.FileItemPath(9, "data/ a.b /child/new.csv")
	if scope.Covers(nested) {
		t.Fatal("nonrecursive directory crossed a child node")
	}
	scope.Recursive = true
	if !scope.Covers(nested) || scope.Covers(plugin.FileItemPath(9, "data/ a.b suffix/new.csv")) || scope.Covers(plugin.FileItemPath(10, "data/ a.b /new.csv")) {
		t.Fatal("typed NFS scope mismatch")
	}
}

func TestMinIONodeScopeCoversTypedCurrentAndFutureObjects(t *testing.T) {
	bucket := plugin.ObjectDirectoryPath(9, "bucket", "")
	scope := SourceNodeScope{Target: plugin.ObjectDirectoryPath(9, "bucket", "a/ exact.prefix ")}
	for _, name := range []string{"current.ifc", "future.ifc"} {
		target := plugin.ObjectItemPath(9, "bucket", "a/ exact.prefix /"+name)
		if !scope.Covers(target) || !(SourceNodeScope{Target: bucket, Recursive: true}).Covers(target) || (SourceNodeScope{Target: bucket}).Covers(target) {
			t.Fatal("current/future depth mismatch", target)
		}
	}
	nested := plugin.ObjectItemPath(9, "bucket", "a/ exact.prefix /child/file.ifc")
	if scope.Covers(nested) {
		t.Fatal("nonrecursive crossed child prefix")
	}
	scope.Recursive = true
	if !scope.Covers(nested) || !scope.Covers(plugin.ObjectDirectoryPath(9, "bucket", "a/ exact.prefix /whole.parquet")) {
		t.Fatal("recursive scope failed")
	}
	for _, path := range []plugin.EngineCatalogPath{scope.Target, plugin.ObjectItemPath(10, "bucket", "a/ exact.prefix /file.ifc"), plugin.ObjectItemPath(9, "other", "a/ exact.prefix /file.ifc"), plugin.ObjectItemPath(9, "bucket", "a/ exact.prefix suffix/file.ifc")} {
		if scope.Covers(path) {
			t.Fatal("foreign/self target covered", path)
		}
	}
	root := SourceNodeScope{Target: plugin.ObjectRootPath(9), Recursive: true}
	if root.Validate() != nil || !root.Covers(nested) || !root.Covers(plugin.ObjectItemPath(9, "future-bucket", "future.ifc")) || root.Covers(plugin.ObjectItemPath(10, "bucket", "future.ifc")) {
		t.Fatal("recursive root must cover objects in this engine only")
	}
	if (SourceNodeScope{Target: root.Target}).Validate() == nil {
		t.Fatal("nonrecursive service root is not a whole-engine grant")
	}
}
