package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/meta/internal/metatest"
	"github.com/addp/meta/internal/models"
	"gorm.io/gorm"
)

func TestResolveNodeIdentity(t *testing.T) {
	exerciseNodeIdentity(t, metatest.OpenMetadataDB(t))
}

func exerciseNodeIdentity(t *testing.T, db *gorm.DB) {
	t.Helper()
	const tenant, engine = 7, 128
	root := models.MetaNode{TenantID: tenant, EngineID: engine, NodeType: "server", Name: "PG", Attributes: models.JSONMap{"secret": "private-sentinel"}}
	if err := db.Create(&root).Error; err != nil {
		t.Fatal(err)
	}
	name := " exact.schema/ "
	node := models.MetaNode{TenantID: tenant, EngineID: engine, ParentNodeID: &root.ID, NodeType: "schema", Name: name, FullName: name, Attributes: root.Attributes}
	if err := db.Create(&node).Error; err != nil {
		t.Fatal(err)
	}
	ignored := models.MetaNode{TenantID: tenant, EngineID: engine, ParentNodeID: &root.ID, NodeType: "directory", Name: "not-a-PG-schema", FullName: "not-a-PG-schema"}
	if err := db.Create(&ignored).Error; err != nil {
		t.Fatal(err)
	}
	svc := NewMetadataQueryService(db)
	query := authorizationfacts.NodeIdentityQuery{Target: plugin.TabularNamespacePath(engine, "schema", name)}
	read := func(q authorizationfacts.NodeIdentityQuery, want bool) {
		t.Helper()
		result, err := svc.ResolveNodeIdentity(context.Background(), tenant, q)
		if err != nil || result.Exists != want || result.Validate(tenant, q) != nil {
			t.Fatalf("node=%+v err=%v want=%v", result, err, want)
		}
		encoded, _ := json.Marshal(result)
		var shape map[string]any
		if json.Unmarshal(encoded, &shape) != nil || len(shape) != 3 {
			t.Fatal("node observation leaked nonidentity columns")
		}
	}
	pageQuery := authorizationfacts.NodeIdentityPageQuery{Target: plugin.EngineCatalogRootPath(plugin.TabularCatalogModel("schema"), engine), Page: 1, PageSize: 1}
	page, err := svc.QueryNodeIdentities(context.Background(), tenant, pageQuery)
	if err != nil || page.Total != 1 || len(page.Nodes) != 1 || page.Nodes[0].Target.Segments[1].Name != name || page.Validate(tenant, pageQuery) != nil {
		t.Fatalf("minimal immediate node list: %+v %v", page, err)
	}
	pageQuery.Page = 2
	if empty, err := svc.QueryNodeIdentities(context.Background(), tenant, pageQuery); err != nil || empty.Total != 1 || len(empty.Nodes) != 0 {
		t.Fatalf("node pagination: %+v %v", empty, err)
	}
	pageQuery.Page = 1
	pageQuery.Target = query.Target
	if children, err := svc.QueryNodeIdentities(context.Background(), tenant, pageQuery); err != nil || children.Total != 0 || len(children.Nodes) != 0 || !children.Parent.Exists {
		t.Fatalf("schema must not enumerate DataItems: %+v %v", children, err)
	}
	if foreign, err := svc.QueryNodeIdentities(context.Background(), tenant+1, pageQuery); err != nil || foreign.Total != 0 || foreign.Parent.Exists {
		t.Fatal("foreign node enumeration", foreign, err)
	}
	read(query, true)
	read(authorizationfacts.NodeIdentityQuery{Target: plugin.EngineCatalogRootPath(plugin.TabularCatalogModel("schema"), engine)}, true)
	read(authorizationfacts.NodeIdentityQuery{Target: plugin.TabularNamespacePath(engine, "schema", name+"suffix")}, false)
	read(authorizationfacts.NodeIdentityQuery{Target: plugin.TabularNamespacePath(engine+1, "schema", name)}, false)
	foreign, err := svc.ResolveNodeIdentity(context.Background(), tenant+1, query)
	if err != nil || foreign.Exists {
		t.Fatal("foreign node became visible")
	}
	if _, err := svc.ResolveNodeIdentity(context.Background(), 0, query); err == nil {
		t.Fatal("empty tenant accepted")
	}
	if err := db.Model(&node).Update("full_name", "different").Error; err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ResolveNodeIdentity(context.Background(), tenant, query); err == nil {
		t.Fatal("inconsistent typed identity accepted")
	}
	if err := db.Delete(&node).Error; err != nil {
		t.Fatal(err)
	}
	read(query, false)
	root.ID = 0
	if err := db.Create(&root).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ResolveNodeIdentity(context.Background(), tenant, query); err == nil {
		t.Fatal("ambiguous root treated as absence")
	}
}

func TestNFSOrdinaryNodeIdentityExcludesWholeContainers(t *testing.T) {
	db := metatest.OpenMetadataDB(t)
	root := models.MetaNode{TenantID: 7, EngineID: 129, NodeType: "root", Name: "NFS"}
	if e := db.Create(&root).Error; e != nil {
		t.Fatal(e)
	}
	dir := models.MetaNode{TenantID: 7, EngineID: 129, ParentNodeID: &root.ID, NodeType: "dir", Name: " a.b ", FullName: " a.b "}
	if e := db.Create(&dir).Error; e != nil {
		t.Fatal(e)
	}
	whole := models.MetaNode{TenantID: 7, EngineID: 129, ParentNodeID: &dir.ID, NodeType: "dir", Name: "one.gdb", FullName: " a.b /one.gdb"}
	if e := db.Create(&whole).Error; e != nil {
		t.Fatal(e)
	}
	item := models.MetaItem{TenantID: 7, EngineID: 129, NodeID: whole.ID, Name: whole.Name, FullName: whole.FullName, ItemType: "file", Fingerprint: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Attributes: models.JSONMap{"item": map[string]any{"layout": "whole", "format": "filegdb"}}}
	if e := db.Create(&item).Error; e != nil {
		t.Fatal(e)
	}
	svc := NewMetadataQueryService(db)
	for _, target := range []plugin.EngineCatalogPath{plugin.FileDirectoryPath(129, ""), plugin.FileDirectoryPath(129, dir.FullName)} {
		q := authorizationfacts.NodeIdentityQuery{Target: target}
		got, e := svc.ResolveNodeIdentity(context.Background(), 7, q)
		if e != nil || !got.Exists {
			t.Fatalf("ordinary node: %+v %v", got, e)
		}
	}
	q := authorizationfacts.NodeIdentityQuery{Target: plugin.FileDirectoryPath(129, whole.FullName)}
	if got, e := svc.ResolveNodeIdentity(context.Background(), 7, q); e != nil || got.Exists {
		t.Fatalf("whole became a node grant: %+v %v", got, e)
	}
	pq := authorizationfacts.NodeIdentityPageQuery{Target: plugin.FileDirectoryPath(129, dir.FullName), Page: 1, PageSize: 100}
	if got, e := svc.QueryNodeIdentities(context.Background(), 7, pq); e != nil || got.Total != 0 {
		t.Fatalf("whole candidate leaked: %+v %v", got, e)
	}
	pq.Target = plugin.FileDirectoryPath(129, "")
	if got, e := svc.QueryNodeIdentities(context.Background(), 7, pq); e != nil || got.Total != 1 || got.Nodes[0].Target.StringPath() != dir.FullName {
		t.Fatalf("direct node page: %+v %v", got, e)
	}
}
