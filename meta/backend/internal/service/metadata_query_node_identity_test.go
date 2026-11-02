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
