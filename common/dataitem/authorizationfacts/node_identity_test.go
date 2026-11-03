package authorizationfacts

import (
	"encoding/json"
	"testing"

	"github.com/addp/common/engine/plugin"
)

func TestNodeIdentityRequiresCanonicalRootOrSchema(t *testing.T) {
	root := plugin.EngineCatalogRootPath(plugin.TabularCatalogModel("schema"), 9)
	for _, target := range []plugin.EngineCatalogPath{root, plugin.TabularNamespacePath(9, "schema", " exact.schema/ "), plugin.FileRootPath(9), plugin.FileDirectoryPath(9, "public/ a.b ")} {
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
		plugin.FileItemPath(9, "public/file.csv"),
	} {
		if (NodeIdentityQuery{Target: target}).Validate() == nil {
			t.Fatal("unsupported or malformed node accepted")
		}
	}
	for _, name := range []string{"", ".", "..", "a/b", "a\\b", "bad\x00name"} {
		target := plugin.FileDirectoryPath(9, "public")
		target.Segments[1].Name = name
		if (NodeIdentityQuery{Target: target}).Validate() == nil {
			t.Fatalf("malformed directory segment accepted: %q", name)
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

func TestNodeIdentityPageRejectsScopeAndPaginationDrift(t *testing.T) {
	root := plugin.EngineCatalogRootPath(plugin.TabularCatalogModel("schema"), 9)
	query := NodeIdentityPageQuery{Target: root, Page: 1, PageSize: 1}
	original := NodeIdentityPage{Parent: NodeIdentity{TenantID: 7, Target: root, Exists: true}, Nodes: []NodeIdentity{{TenantID: 7, Target: plugin.TabularNamespacePath(9, "schema", "a.schema/"), Exists: true}}, Total: 2, Page: 1, PageSize: 1}
	if original.Validate(7, query) != nil {
		t.Fatal("valid partial node page rejected")
	}
	for name, mutate := range map[string]func(*NodeIdentityPage){
		"tenant":         func(p *NodeIdentityPage) { p.Nodes[0].TenantID++ },
		"engine":         func(p *NodeIdentityPage) { p.Nodes[0].Target.EngineID++ },
		"missing parent": func(p *NodeIdentityPage) { p.Parent.Exists = false },
		"missing child":  func(p *NodeIdentityPage) { p.Nodes[0].Exists = false },
		"item":           func(p *NodeIdentityPage) { p.Nodes[0].Target = plugin.TabularItemPath(9, "schema", "s", "t") },
		"page drift":     func(p *NodeIdentityPage) { p.Page++ },
		"size drift":     func(p *NodeIdentityPage) { p.PageSize++ },
		"count drift":    func(p *NodeIdentityPage) { p.Total = 0 },
		"nil nodes":      func(p *NodeIdentityPage) { p.Nodes = nil },
	} {
		t.Run(name, func(t *testing.T) {
			var changed NodeIdentityPage
			encoded, _ := json.Marshal(original)
			_ = json.Unmarshal(encoded, &changed)
			mutate(&changed)
			if changed.Validate(7, query) == nil {
				t.Fatal("invalid page accepted")
			}
		})
	}
	schema := plugin.TabularNamespacePath(9, "schema", "s")
	schemaPage := NodeIdentityPage{Parent: NodeIdentity{TenantID: 7, Target: schema, Exists: true}, Nodes: []NodeIdentity{}, Page: 1, PageSize: 1}
	if schemaPage.Validate(7, NodeIdentityPageQuery{Target: schema, Page: 1, PageSize: 1}) != nil {
		t.Fatal("empty schema node page rejected")
	}
	schemaPage.Total = 1
	schemaPage.Nodes = original.Nodes
	if schemaPage.Validate(7, NodeIdentityPageQuery{Target: schema, Page: 1, PageSize: 1}) == nil {
		t.Fatal("schema cannot enumerate items or peer nodes")
	}
	query.Page = 3
	original.Page = 3
	original.Nodes = []NodeIdentity{}
	if original.Validate(7, query) != nil {
		t.Fatal("past-end empty page rejected")
	}
}

func TestMinIONodeIdentitiesBindBucketAndPrefix(t *testing.T) {
	for _, path := range []plugin.EngineCatalogPath{plugin.ObjectRootPath(9), plugin.ObjectDirectoryPath(9, "bucket", ""), plugin.ObjectDirectoryPath(9, "bucket", "a/ exact.prefix ")} {
		if !ValidNodeIdentityTarget(path) || !NodeIdentityMatchesEngine("minio", path) || NodeIdentityMatchesEngine("nfs", path) {
			t.Fatal("wrong MinIO profile", path)
		}
	}
	for _, path := range []plugin.EngineCatalogPath{plugin.ObjectItemPath(9, "bucket", "a/file.ifc"), plugin.FileDirectoryPath(9, "bucket/a")} {
		if NodeIdentityMatchesEngine("minio", path) {
			t.Fatal("non-node or wrong profile", path)
		}
	}
	path := plugin.ObjectDirectoryPath(9, "bucket", "a")
	for _, name := range []string{"", ".", "..", "bad/name", "bad\\name", "bad\x00name"} {
		changed := path
		changed.Segments = append([]plugin.EngineCatalogSegment(nil), path.Segments...)
		changed.Segments[2].Name = name
		if ValidNodeIdentityTarget(changed) {
			t.Fatal("ambiguous object prefix", name)
		}
	}
	page := NodeIdentityPage{Parent: NodeIdentity{TenantID: 7, Target: plugin.ObjectRootPath(9), Exists: true}, Nodes: []NodeIdentity{{TenantID: 7, Target: plugin.ObjectDirectoryPath(9, "bucket", ""), Exists: true}}, Total: 1, Page: 1, PageSize: 100}
	q := NodeIdentityPageQuery{Target: page.Parent.Target, Page: 1, PageSize: 100}
	if err := page.Validate(7, q); err != nil {
		t.Fatal(err)
	}
	page.Nodes[0].Target = path
	if page.Validate(7, q) == nil {
		t.Fatal("root skipped bucket ancestry")
	}
}
