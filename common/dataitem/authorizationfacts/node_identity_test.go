package authorizationfacts

import (
	"testing"

	"github.com/addp/common/engine/plugin"
)

func TestNodeIdentityRequiresCanonicalRootOrSchema(t *testing.T) {
	root := plugin.EngineCatalogRootPath(plugin.TabularCatalogModel("schema"), 9)
	for _, target := range []plugin.EngineCatalogPath{root, plugin.TabularNamespacePath(9, "schema", " exact.schema/ ")} {
		query := NodeIdentityQuery{Target: target}
		if query.Validate() != nil {
			t.Fatal("canonical node rejected")
		}
		for _, exists := range []bool{false, true} {
			fact := NodeIdentity{TenantID: 7, Target: target, Exists: exists}
			if fact.Validate(7, query) != nil || fact.Validate(8, query) == nil {
				t.Fatal("node fact lost tenant or existence binding")
			}
		}
	}
	for _, target := range []plugin.EngineCatalogPath{
		plugin.TabularNamespacePath(0, "schema", "public"),
		plugin.TabularNamespacePath(9, "schema", ""),
		plugin.TabularNamespacePath(9, "schema", "bad\x00name"),
		plugin.TabularNamespacePath(9, "database", "public"),
		plugin.TabularItemPath(9, "schema", "public", "people"),
		plugin.FileDirectoryPath(9, "public"),
	} {
		if (NodeIdentityQuery{Target: target}).Validate() == nil {
			t.Fatal("unsupported or malformed node accepted")
		}
	}
	for _, mutate := range []func(*plugin.EngineCatalogPath){
		func(p *plugin.EngineCatalogPath) { p.Version = "" },
		func(p *plugin.EngineCatalogPath) { p.Segments[0].Name = "server-name" },
		func(p *plugin.EngineCatalogPath) { p.Segments[0].Kind = "" },
	} {
		target := plugin.EngineCatalogRootPath(plugin.TabularCatalogModel("schema"), 9)
		mutate(&target)
		if (NodeIdentityQuery{Target: target}).Validate() == nil {
			t.Fatal("noncanonical node accepted")
		}
	}
}
