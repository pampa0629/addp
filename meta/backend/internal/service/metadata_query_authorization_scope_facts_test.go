package service

import (
	"context"
	"testing"

	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/meta/internal/metatest"
	"github.com/addp/meta/internal/models"
	"gorm.io/gorm"
)

func TestWholeContainerAuthorizationFacts(t *testing.T) {
	exerciseWholeContainerAuthorizationFacts(t, metatest.OpenMetadataDB(t))
}
func exerciseWholeContainerAuthorizationFacts(t *testing.T, db *gorm.DB) {
	t.Helper()
	item := seedFileAuthorizationFacts(t, db, 910, "gdb")
	parentID := item.NodeID
	scope := models.MetaNode{TenantID: item.TenantID, EngineID: item.EngineID, ParentNodeID: &parentID, NodeType: "dir", Name: item.Name, FullName: item.FullName}
	if err := db.Create(&scope).Error; err != nil {
		t.Fatal(err)
	}
	item.NodeID = scope.ID
	item.Attributes = models.JSONMap{"item": map[string]any{"layout": "whole", "data_type": "container", "format": "filegdb", "scope_exclusive": true, "claim_policy": "whole_scope"}, "storage": map[string]any{"physical_path": item.FullName}, "type_info": map[string]any{"container": map[string]any{"child_count": 0, "children": []any{}}}}
	save := func() {
		t.Helper()
		if err := db.Save(&item).Error; err != nil {
			t.Fatal(err)
		}
	}
	save()
	s := NewMetadataQueryService(db)
	read := func() *authorizationfacts.AuthorizationFacts {
		t.Helper()
		f, err := s.GetDataItemAuthorizationFacts(context.Background(), item.TenantID, item.Fingerprint)
		if err != nil {
			t.Fatal(err)
		}
		return f
	}
	basic := read()
	if basic.BasisState != authorizationfacts.AuthorizationBasisComplete || basic.Boundary.Scope == nil {
		t.Fatalf("basic whole facts incomplete: %+v", basic)
	}
	item.ScannedDepth = "deep"
	item.Attributes["type_info"] = map[string]any{"container": map[string]any{"children": []any{map[string]any{"name": "new_feature_class"}}, "child_count": 1}}
	save()
	deep := read()
	if deep.BoundaryHash == nil || *deep.BoundaryHash != *basic.BoundaryHash {
		t.Fatal("adding child tables changed the whole scope boundary")
	}
	identity := item.Attributes["item"].(map[string]any)
	identity["refs"] = []any{map[string]any{"path": "outside.gdb"}}
	save()
	if f := read(); f.BasisState != authorizationfacts.AuthorizationBasisNeedsRefresh || f.Boundary != nil {
		t.Fatal("outside composition became part of whole scope")
	}
	delete(identity, "refs")
	identity["claim_policy"] = "other"
	save()
	if f := read(); f.BasisState != authorizationfacts.AuthorizationBasisIncomplete {
		t.Fatal("missing exclusive claim accepted")
	}
	identity["claim_policy"] = "whole_scope"
	item.NodeID = *scope.ParentNodeID
	save()
	if f := read(); f.BasisState != authorizationfacts.AuthorizationBasisNeedsRefresh {
		t.Fatal("non-scope parent accepted")
	}
	item.NodeID = scope.ID
	item.Attributes["storage"].(map[string]any)["physical_path"] = "models/replaced.gdb"
	save()
	if f := read(); f.BasisState != authorizationfacts.AuthorizationBasisNeedsRefresh {
		t.Fatal("changed physical root inherited the boundary")
	}
}
