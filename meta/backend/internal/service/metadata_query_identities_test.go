package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/addp/common/dataitem/authorizationfacts"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/meta/internal/metatest"
	"github.com/addp/meta/internal/models"
	"gorm.io/gorm"
)

func TestDataItemIdentityQuery(t *testing.T) {
	exerciseDataItemIdentityQuery(t, metatest.OpenMetadataDB(t))
}

func exerciseDataItemIdentityQuery(t *testing.T, db *gorm.DB) {
	t.Helper()
	const engine = 119
	root := models.MetaNode{TenantID: 7, EngineID: engine, NodeType: "server", Name: ""}
	if err := db.Create(&root).Error; err != nil {
		t.Fatal(err)
	}
	parent := models.MetaNode{TenantID: 7, EngineID: engine, ParentNodeID: &root.ID, NodeType: "schema", Name: "schema.with.dot"}
	if err := db.Create(&parent).Error; err != nil {
		t.Fatal(err)
	}
	var items []models.MetaItem
	for i, name := range []string{"A_100%", "B", "C", "other-tenant", "other-engine", "deleted"} {
		item := models.MetaItem{TenantID: 7, EngineID: engine, NodeID: parent.ID, Name: name, FullName: "schema." + name, ItemType: "table", Attributes: models.JSONMap{"secret": "must-not-leak"}}
		if i == 3 {
			item.TenantID = 8
		}
		if i == 4 {
			item.EngineID++
		}
		item.Fingerprint = commonmodels.GenerateItemFingerprint(item.EngineID, item.FullName)
		if err := db.Create(&item).Error; err != nil {
			t.Fatal(err)
		}
		if i == 5 {
			if err := db.Delete(&item).Error; err != nil {
				t.Fatal(err)
			}
		}
		items = append(items, item)
	}
	s := NewMetadataQueryService(db)
	query := authorizationfacts.IdentityQuery{EngineID: engine, Page: 1, PageSize: 2}
	first, err := s.QueryDataItemIdentities(context.Background(), 7, query)
	if err != nil || first.Total != 3 || len(first.Data) != 2 || first.TotalPages != 2 {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	if len(first.Data[0].Ancestors) != 2 || first.Data[0].Ancestors[1].Name != "schema.with.dot" || first.Data[0].Ancestors[1].ID != parent.ID {
		t.Fatalf("lost authoritative ancestry: %+v", first.Data)
	}
	encoded, _ := json.Marshal(first)
	if strings.Contains(string(encoded), "secret") || strings.Contains(string(encoded), "attributes") {
		t.Fatal("nonidentity columns leaked")
	}
	query.Page = 2
	second, err := s.QueryDataItemIdentities(context.Background(), 7, query)
	if err != nil || second.Total != 3 || len(second.Data) != 1 || second.Data[0].ItemFingerprint <= first.Data[1].ItemFingerprint {
		t.Fatalf("second=%+v err=%v", second, err)
	}
	query.Page, query.PageSize = 1, 100
	for _, mode := range []string{"include", "exclude"} {
		query.FingerprintFilter, query.Fingerprints = mode, []string{items[0].Fingerprint, items[3].Fingerprint}
		page, err := s.QueryDataItemIdentities(context.Background(), 7, query)
		want := int64(1)
		if mode == "exclude" {
			want = 2
		}
		if err != nil || page.Total != want || int64(len(page.Data)) != want {
			t.Fatalf("%s page=%+v err=%v", mode, page, err)
		}
	}
	query.FingerprintFilter, query.Fingerprints = "include", nil
	empty, err := s.QueryDataItemIdentities(context.Background(), 7, query)
	if err != nil || empty.Total != 0 || len(empty.Data) != 0 {
		t.Fatalf("empty include broadened query: %+v %v", empty, err)
	}
	query.FingerprintFilter, query.Search = "", "_100%"
	found, err := s.QueryDataItemIdentities(context.Background(), 7, query)
	if err != nil || found.Total != 1 || found.Data[0].ItemFingerprint != items[0].Fingerprint {
		t.Fatalf("literal search=%+v %v", found, err)
	}
	query.Search, query.Page = "", 10
	empty, err = s.QueryDataItemIdentities(context.Background(), 7, query)
	if err != nil || empty.Total != 3 || len(empty.Data) != 0 {
		t.Fatalf("out-of-range page=%+v %v", empty, err)
	}
	if _, err := s.QueryDataItemIdentities(context.Background(), 0, query); err == nil {
		t.Fatal("missing tenant accepted")
	}
	var count int64
	if err := db.Unscoped().Model(&models.MetaItem{}).Where("engine_id IN ?", []uint{engine, engine + 1}).Count(&count).Error; err != nil || count != 6 {
		t.Fatalf("query changed metadata: %d %v", count, err)
	}
	// Reject broken, foreign and cyclic parent edges instead of inventing paths.
	query.Page, query.Search = 1, ""
	for _, update := range []map[string]interface{}{{"tenant_id": 8}, {"tenant_id": 7, "engine_id": engine + 1}, {"engine_id": engine, "parent_node_id": parent.ID}} {
		if err := db.Model(&models.MetaNode{}).Where("id=?", parent.ID).Updates(update).Error; err != nil {
			t.Fatal(err)
		}
		if _, err := s.QueryDataItemIdentities(context.Background(), 7, query); err == nil {
			t.Fatal("invalid ancestry accepted")
		}
	}
	if err := db.Model(&models.MetaNode{}).Where("id=?", parent.ID).Update("parent_node_id", root.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Delete(&parent).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := s.QueryDataItemIdentities(context.Background(), 7, query); err == nil {
		t.Fatal("deleted ancestry accepted")
	}

}
