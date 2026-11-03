package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/dataitem/authorizationfacts"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/metatest"
	"github.com/addp/meta/internal/models"
	"gorm.io/gorm"
)

func seedAuthorizationFacts(t *testing.T, db *gorm.DB, namespaceTerm, itemType string, engineID uint) models.MetaItem {
	t.Helper()
	root := models.MetaNode{TenantID: 7, EngineID: engineID, NodeType: "server", Name: "PG", Depth: 0, FullName: ""}
	if err := db.Create(&root).Error; err != nil {
		t.Fatal(err)
	}
	parent := models.MetaNode{TenantID: 7, EngineID: engineID, ParentNodeID: &root.ID, NodeType: namespaceTerm, Name: "Out.door", FullName: "Out.door", Depth: 1}
	if err := db.Create(&parent).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Now().Add(-time.Hour)
	item := models.MetaItem{TenantID: 7, EngineID: engineID, NodeID: parent.ID, ItemType: itemType, Name: " active.table ", FullName: "Out.door. active.table ", ScannedDepth: "basic", ScannedAt: &now,
		Attributes: models.JSONMap{"schema_version": 1, "item": map[string]any{"layout": "single", "data_type": "table"}, "storage": map[string]any{"schema_name": parent.Name}, "password": "must-not-leak"}}
	item.Fingerprint = commonModels.GenerateItemFingerprint(item.EngineID, item.FullName)
	if err := db.Create(&item).Error; err != nil {
		t.Fatal(err)
	}
	return item
}

func exerciseAuthorizationFacts(t *testing.T, db *gorm.DB) {
	t.Helper()
	exerciseMetadataItemFacts(t, db)
	for index, target := range []struct{ namespace, itemType string }{{"schema", "table"}, {"database", "table"}, {"database", "collection"}} {
		t.Run(target.namespace+"/"+target.itemType, func(t *testing.T) {
			exerciseSingleAuthorizationFacts(t, db, target.namespace, target.itemType, uint(index+2))
		})
	}
}

func exerciseSingleAuthorizationFacts(t *testing.T, db *gorm.DB, namespaceTerm, itemType string, engineID uint) {
	t.Helper()
	item := seedAuthorizationFacts(t, db, namespaceTerm, itemType, engineID)
	if itemType == "collection" {
		delete(item.Attributes, "storage")
		if err := db.Model(&item).Update("attributes", item.Attributes).Error; err != nil {
			t.Fatal(err)
		}
	}
	s := NewMetadataQueryService(db)
	items, branchErr := s.ListItemsByBranch(engineID, 7, "Out.door")
	if branchErr != nil || len(items) != 1 || items[0].Fingerprint != item.Fingerprint {
		t.Fatalf("branch lookup must use the structural root's direct child: items=%+v err=%v", items, branchErr)
	}
	// A legacy orphan and a nested same-name branch must not broaden the filter.
	orphan := models.MetaNode{TenantID: 7, EngineID: engineID, NodeType: namespaceTerm, Name: "Out.door", FullName: "Out.door"}
	if err := db.Create(&orphan).Error; err != nil {
		t.Fatal(err)
	}
	nested := models.MetaNode{TenantID: 7, EngineID: engineID, ParentNodeID: &item.NodeID, NodeType: namespaceTerm, Name: "Out.door", FullName: "Out.door.Out.door"}
	if err := db.Create(&nested).Error; err != nil {
		t.Fatal(err)
	}
	for index, decoy := range []models.MetaItem{
		{TenantID: 7, EngineID: engineID, NodeID: orphan.ID, Name: "orphan"},
		{TenantID: 7, EngineID: engineID, NodeID: nested.ID, Name: "nested"},
		{TenantID: 8, EngineID: engineID, NodeID: item.NodeID, Name: "other_tenant"},
		{TenantID: 7, EngineID: engineID + 100, NodeID: item.NodeID, Name: "other_engine"},
		{TenantID: 7, EngineID: engineID, NodeID: item.NodeID, Name: "deleted"},
	} {
		decoy.ItemType, decoy.FullName = itemType, "Out.door."+decoy.Name
		decoy.Fingerprint = commonModels.GenerateItemFingerprint(decoy.EngineID, decoy.FullName)
		if err := db.Create(&decoy).Error; err != nil {
			t.Fatal(err)
		}
		if index == 4 {
			if err := db.Delete(&decoy).Error; err != nil {
				t.Fatal(err)
			}
		}
	}
	items, branchErr = s.ListItemsByBranch(engineID, 7, "Out.door")
	if branchErr != nil || len(items) != 1 || items[0].Fingerprint != item.Fingerprint {
		t.Fatalf("branch filter included a different owner, nested, legacy or deleted item: %+v %v", items, branchErr)
	}
	facts, err := s.GetDataItemAuthorizationFacts(context.Background(), 7, item.Fingerprint)
	if err != nil || facts.BasisState != authorizationfacts.AuthorizationBasisComplete {
		t.Fatalf("facts=%+v err=%v", facts, err)
	}
	if facts.ItemType != itemType || facts.Boundary.NativeTarget.Segments[1].Term != namespaceTerm || facts.Boundary.NativeTarget.Segments[2].Term != itemType || facts.Boundary.NativeTarget.Segments[1].Name != "Out.door" || facts.Boundary.NativeTarget.Segments[2].Name != item.Name {
		t.Fatal("native names were split or trimmed")
	}
	encoded, _ := json.Marshal(facts)
	if strings.Contains(string(encoded), "password") || strings.Contains(string(encoded), "scanned_depth") {
		t.Fatal("non-boundary facts leaked")
	}
	hash := *facts.BoundaryHash
	if err := db.Model(&item).Updates(map[string]any{"row_count": 500, "size_bytes": 1000, "scanned_depth": "deep", "scanned_at": time.Now()}).Error; err != nil {
		t.Fatal(err)
	}
	facts, err = s.GetDataItemAuthorizationFacts(context.Background(), 7, item.Fingerprint)
	if err != nil || *facts.BoundaryHash != hash {
		t.Fatal("content/deep scan changed boundary", err)
	}
	if _, err := s.GetDataItemAuthorizationFacts(context.Background(), 8, item.Fingerprint); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatal("cross tenant not hidden", err)
	}
	// Parent ownership is part of the committed boundary, not inferred from the name.
	if err := db.Model(&models.MetaNode{}).Where("id = ?", item.NodeID).Update("engine_id", engineID+100).Error; err != nil {
		t.Fatal(err)
	}
	facts, err = s.GetDataItemAuthorizationFacts(context.Background(), 7, item.Fingerprint)
	if err != nil || facts.BasisState != authorizationfacts.AuthorizationBasisIncomplete || facts.Boundary != nil {
		t.Fatal("cross-engine parent accepted", err)
	}
	if err := db.Model(&models.MetaNode{}).Where("id = ?", item.NodeID).Update("engine_id", engineID).Error; err != nil {
		t.Fatal(err)
	}
	if itemType == "table" {
		item.Attributes["type_info"] = map[string]any{"table": map[string]any{"kind": "view"}}
		if err := db.Model(&item).Update("attributes", item.Attributes).Error; err != nil {
			t.Fatal(err)
		}
		facts, err = s.GetDataItemAuthorizationFacts(context.Background(), 7, item.Fingerprint)
		if err != nil || facts.BasisState != authorizationfacts.AuthorizationBasisComplete || facts.Boundary.MetadataTarget == nil {
			t.Fatal("view metadata identity missing", err)
		}
		if _, err := facts.ApprovedBinding(); err == nil {
			t.Fatal("view metadata became ordinary content proof")
		}
		delete(item.Attributes, "type_info")
		if err := db.Model(&item).Update("attributes", item.Attributes).Error; err != nil {
			t.Fatal(err)
		}
	}
	if itemType == "collection" {
		if err := db.Model(&models.MetaNode{}).Where("id = ?", item.NodeID).Update("node_type", "schema").Error; err != nil {
			t.Fatal(err)
		}
		facts, err = s.GetDataItemAuthorizationFacts(context.Background(), 7, item.Fingerprint)
		if err != nil || facts.BasisState != authorizationfacts.AuthorizationBasisComplete || facts.Boundary.MetadataTarget == nil {
			t.Fatal("metadata identity missing for typed collection", err)
		}
		if err := db.Model(&models.MetaNode{}).Where("id = ?", item.NodeID).Update("node_type", namespaceTerm).Error; err != nil {
			t.Fatal(err)
		}
	}
	item.Attributes["item"] = map[string]any{"data_type": "table"}
	if err := db.Model(&item).Update("attributes", item.Attributes).Error; err != nil {
		t.Fatal(err)
	}
	facts, err = s.GetDataItemAuthorizationFacts(context.Background(), 7, item.Fingerprint)
	if err != nil || facts.BasisState != authorizationfacts.AuthorizationBasisIncomplete || facts.BoundaryHash != nil || facts.Boundary != nil {
		t.Fatal("deep flag implied complete boundary", err)
	}
	item.Attributes["item"] = map[string]any{"layout": "whole"}
	if err := db.Model(&item).Update("attributes", item.Attributes).Error; err != nil {
		t.Fatal(err)
	}
	facts, err = s.GetDataItemAuthorizationFacts(context.Background(), 7, item.Fingerprint)
	if err != nil || facts.BasisState != authorizationfacts.AuthorizationBasisUnsupported {
		t.Fatal("unimplemented whole allowed", err)
	}
	item.Attributes["item"] = map[string]any{"layout": "single"}
	if itemType == "collection" {
		if err := db.Model(&models.MetaNode{}).Where("id = ?", item.NodeID).Update("full_name", "other").Error; err != nil {
			t.Fatal(err)
		}
	} else {
		item.Attributes["storage"] = map[string]any{"schema_name": "other"}
	}
	if err := db.Model(&item).Update("attributes", item.Attributes).Error; err != nil {
		t.Fatal(err)
	}
	facts, err = s.GetDataItemAuthorizationFacts(context.Background(), 7, item.Fingerprint)
	if itemType == "collection" {
		if err != nil || facts.BasisState != authorizationfacts.AuthorizationBasisNeedsRefresh {
			t.Fatal("inconsistent parent identity accepted", err)
		}
	} else {
		if err != nil || facts.BasisState != authorizationfacts.AuthorizationBasisComplete || facts.Boundary.MetadataTarget == nil {
			t.Fatal("metadata identity depends on content entrypoint", err)
		}
		if _, err := facts.ApprovedBinding(); err == nil {
			t.Fatal("inconsistent content entrypoint became readable")
		}
	}
	if err := db.Delete(&item).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetDataItemAuthorizationFacts(context.Background(), 7, item.Fingerprint); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatal("deleted target not hidden", err)
	}
}

func TestDataItemAuthorizationFacts(t *testing.T) {
	exerciseAuthorizationFacts(t, metatest.OpenMetadataDB(t))
}

// Runs in both SQLite T1 and the existing authorization-facts PostgreSQL gate.
func exerciseMetadataItemFacts(t *testing.T, db *gorm.DB) {
	t.Helper()
	for index, label := range []string{"file", "object", "view", "index", "keyspace", "topic", "file-whole", "object-whole", "catalog-table"} {
		t.Run("metadata-only/"+label, func(t *testing.T) {
			kind := strings.TrimSuffix(label, "-whole")
			if label == "catalog-table" {
				kind = "table"
			}
			engineID := uint(7800 + index)
			rootTerm, branchTerm, branchKind, sep := "server", "schema", "namespace", "."
			if kind == "file" {
				rootTerm, branchTerm, branchKind, sep = "root", "dir", "directory", "/"
			}
			if kind == "object" {
				rootTerm, branchTerm, branchKind, sep = "service", "bucket", "bucket", "/"
			}
			if kind == "index" || kind == "topic" {
				rootTerm = "service"
			}
			root := models.MetaNode{TenantID: 7, EngineID: engineID, NodeType: rootTerm, Name: "Engine", FullName: ""}
			if err := db.Create(&root).Error; err != nil {
				t.Fatal(err)
			}
			nodeID := root.ID
			fullName := "a.b 空格"
			parent := models.MetaNode{}
			if kind == "file" || kind == "object" || kind == "view" || kind == "table" {
				parent = models.MetaNode{TenantID: 7, EngineID: engineID, ParentNodeID: &root.ID, NodeType: branchTerm, Name: "out.put", FullName: "out.put"}
				if label == "catalog-table" {
					catalog := models.MetaNode{TenantID: 7, EngineID: engineID, ParentNodeID: &root.ID, NodeType: "catalog", Name: "cat.alog", FullName: "cat.alog"}
					if err := db.Create(&catalog).Error; err != nil {
						t.Fatal(err)
					}
					parent.ParentNodeID = &catalog.ID
					parent.FullName = catalog.FullName + "." + parent.Name
				}
				if err := db.Create(&parent).Error; err != nil {
					t.Fatal(err)
				}
				nodeID = parent.ID
				fullName = parent.FullName + sep + fullName
			}
			item := models.MetaItem{TenantID: 7, EngineID: engineID, NodeID: nodeID, ItemType: kind, Name: "a.b 空格", FullName: fullName, Attributes: models.JSONMap{"item": map[string]any{"layout": "single", "data_type": "container", "format": "geopackage"}, "statistics": map[string]any{"private": "never return"}}}
			if strings.HasSuffix(label, "-whole") {
				scope := models.MetaNode{TenantID: 7, EngineID: engineID, ParentNodeID: &nodeID, NodeType: "dir", Name: item.Name, FullName: item.FullName}
				if kind == "object" {
					scope.NodeType = "prefix"
				}
				if err := db.Create(&scope).Error; err != nil {
					t.Fatal(err)
				}
				nodeID = scope.ID
				item.NodeID = nodeID
				item.Attributes["item"] = map[string]any{"layout": "whole", "data_type": "table", "format": "parquet"}
			}
			if kind == "view" {
				item.ItemType = "table"
				item.Attributes["type_info"] = map[string]any{"table": map[string]any{"kind": "view"}}
			}
			item.Fingerprint = commonModels.GenerateItemFingerprint(engineID, fullName)
			if err := db.Create(&item).Error; err != nil {
				t.Fatal(err)
			}
			service := NewMetadataQueryService(db)
			facts, err := service.GetDataItemAuthorizationFacts(t.Context(), 7, item.Fingerprint)
			if err != nil || facts.BasisState != authorizationfacts.AuthorizationBasisComplete || facts.Boundary.MetadataTarget == nil {
				t.Fatalf("metadata identity: %+v %v", facts, err)
			}
			b, err := facts.MetadataBinding()
			if err != nil {
				t.Fatal(err)
			}
			target, err := b.PrimaryTarget()
			if err != nil {
				t.Fatal(err)
			}
			if target.Segments[len(target.Segments)-1].Name != item.Name {
				t.Fatal("opaque item name was split")
			}
			if parent.ID != 0 && target.Segments[1].Kind != branchKind {
				t.Fatal("persisted directory/namespace type was not normalized")
			}
			if _, err := facts.ApprovedBinding(); err == nil {
				t.Fatal("metadata-only identity authorized content")
			}
			raw, err := json.Marshal(facts)
			if err != nil || strings.Contains(string(raw), "never return") {
				t.Fatal("private facts leaked", err)
			}
			hash := *facts.BoundaryHash
			if err := db.Model(&item).Updates(map[string]any{"row_count": 99, "size_bytes": 123, "scanned_depth": "deep"}).Error; err != nil {
				t.Fatal(err)
			}
			again, err := service.GetDataItemAuthorizationFacts(t.Context(), 7, item.Fingerprint)
			if err != nil || *again.BoundaryHash != hash {
				t.Fatal("content statistics changed metadata identity", err)
			}
			if _, err := service.GetDataItemAuthorizationFacts(t.Context(), 8, item.Fingerprint); !errors.Is(err, gorm.ErrRecordNotFound) {
				t.Fatal("cross tenant exposed metadata", err)
			}
			if err := db.Model(&models.MetaNode{}).Where("id=?", nodeID).Update("tenant_id", 8).Error; err != nil {
				t.Fatal(err)
			}
			denied, err := service.GetDataItemAuthorizationFacts(t.Context(), 7, item.Fingerprint)
			if err != nil || denied.BasisState == authorizationfacts.AuthorizationBasisComplete || denied.Boundary != nil {
				t.Fatal("foreign parent accepted", err)
			}
		})
	}
}
