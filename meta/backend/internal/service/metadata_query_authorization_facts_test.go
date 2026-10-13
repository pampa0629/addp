package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/dataitem"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/metatest"
	"github.com/addp/meta/internal/models"
	"gorm.io/gorm"
)

func seedAuthorizationFacts(t *testing.T, db *gorm.DB) models.MetaItem {
	t.Helper()
	root := models.MetaNode{TenantID: 7, EngineID: 2, NodeType: "server", Name: "PG", Depth: 0, FullName: ""}
	if err := db.Create(&root).Error; err != nil {
		t.Fatal(err)
	}
	parent := models.MetaNode{TenantID: 7, EngineID: 2, ParentNodeID: &root.ID, NodeType: "schema", Name: "Out.door", FullName: "Out.door", Depth: 1}
	if err := db.Create(&parent).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Now().Add(-time.Hour)
	item := models.MetaItem{TenantID: 7, EngineID: 2, NodeID: parent.ID, ItemType: "table", Name: " active.table ", FullName: "Out.door. active.table ", ScannedDepth: "basic", ScannedAt: &now,
		Attributes: models.JSONMap{"schema_version": 1, "item": map[string]any{"layout": "single", "data_type": "table"}, "storage": map[string]any{"schema_name": parent.Name}, "password": "must-not-leak"}}
	item.Fingerprint = commonModels.GenerateItemFingerprint(item.EngineID, item.FullName)
	if err := db.Create(&item).Error; err != nil {
		t.Fatal(err)
	}
	return item
}

func exerciseAuthorizationFacts(t *testing.T, db *gorm.DB) {
	t.Helper()
	item := seedAuthorizationFacts(t, db)
	s := NewMetadataQueryService(db)
	facts, err := s.GetDataItemAuthorizationFacts(context.Background(), 7, item.Fingerprint)
	if err != nil || facts.BasisState != dataitem.AuthorizationBasisComplete {
		t.Fatalf("facts=%+v err=%v", facts, err)
	}
	if facts.Boundary.NativeTarget.Segments[1].Name != "Out.door" || facts.Boundary.NativeTarget.Segments[2].Name != item.Name {
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
	item.Attributes["item"] = map[string]any{"data_type": "table"}
	if err := db.Model(&item).Update("attributes", item.Attributes).Error; err != nil {
		t.Fatal(err)
	}
	facts, err = s.GetDataItemAuthorizationFacts(context.Background(), 7, item.Fingerprint)
	if err != nil || facts.BasisState != dataitem.AuthorizationBasisIncomplete || facts.BoundaryHash != nil || facts.Boundary != nil {
		t.Fatal("deep flag implied complete boundary", err)
	}
	item.Attributes["item"] = map[string]any{"layout": "whole"}
	if err := db.Model(&item).Update("attributes", item.Attributes).Error; err != nil {
		t.Fatal(err)
	}
	facts, err = s.GetDataItemAuthorizationFacts(context.Background(), 7, item.Fingerprint)
	if err != nil || facts.BasisState != dataitem.AuthorizationBasisUnsupported {
		t.Fatal("unimplemented whole allowed", err)
	}
	item.Attributes["item"] = map[string]any{"layout": "single"}
	item.Attributes["storage"] = map[string]any{"schema_name": "other"}
	if err := db.Model(&item).Update("attributes", item.Attributes).Error; err != nil {
		t.Fatal(err)
	}
	facts, err = s.GetDataItemAuthorizationFacts(context.Background(), 7, item.Fingerprint)
	if err != nil || facts.BasisState != dataitem.AuthorizationBasisNeedsRefresh {
		t.Fatal("inconsistent stored coordinates allowed", err)
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
