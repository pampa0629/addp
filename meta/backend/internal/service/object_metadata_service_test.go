package service

import (
	"strings"
	"testing"

	"github.com/addp/meta/internal/models"
)

func TestObjectMetadataServiceGetsObjectMetadata(t *testing.T) {
	db := openObjectCatalogScanTestDB(t)
	svc := NewObjectMetadataService(db)

	bucket := models.MetaNode{
		TenantID:   1,
		EngineID:   7,
		NodeType:   "bucket",
		Name:       "addp",
		Depth:      1,
		FullName:   "addp",
		Attributes: models.JSONMap{"schema_version": 1, "storage": map[string]interface{}{"bucket": "addp"}},
	}
	if err := db.Create(&bucket).Error; err != nil {
		t.Fatalf("create bucket node: %v", err)
	}

	item := models.MetaItem{
		TenantID:    1,
		EngineID:    7,
		NodeID:      bucket.ID,
		ItemType:    "object",
		Name:        "cities.csv",
		FullName:    "addp/cities.csv",
		Fingerprint: strings.Repeat("c", 64),
		Attributes: models.JSONMap{
			"item":    map[string]interface{}{"format": "csv", "data_type": "table"},
			"storage": map[string]interface{}{"bucket": "addp", "name": "cities.csv"},
		},
	}
	if err := db.Create(&item).Error; err != nil {
		t.Fatalf("create meta item: %v", err)
	}

	got, err := svc.GetObjectMetadata(1, 7, "addp/cities.csv")
	if err != nil {
		t.Fatalf("GetObjectMetadata() error = %v", err)
	}
	if got.ID != item.ID || got.FullName != "addp/cities.csv" {
		t.Fatalf("item = %#v, want id %d full_name addp/cities.csv", got, item.ID)
	}
}
