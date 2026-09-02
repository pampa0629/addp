package service

import (
	"testing"
	"time"

	"github.com/addp/common/datatype"
	commonjson "github.com/addp/common/jsonmap"
	"github.com/addp/meta/internal/metatest"
	"github.com/addp/meta/internal/models"
)

func TestGetDataItemSecurityFactsReturnsOnlyCanonicalStructure(t *testing.T) {
	db := metatest.OpenMetadataDB(t)
	now := time.Now().UTC().Truncate(time.Second)
	fields := []datatype.FieldInfo{
		{Name: "userInfo", Path: []string{"userInfo"}, Type: datatype.FieldTypeJSON, Nullable: true},
		{Name: "userInfo.phone", Path: []string{"userInfo", "phone"}, Type: datatype.FieldTypeString, Nullable: true, Comment: "手机号"},
	}
	item := models.MetaItem{TenantID: 7, EngineID: 11, NodeID: 2, ItemType: "collection", Name: "Persons", FullName: "Outdoor.Persons", Fingerprint: "outdoor-persons", ScannedAt: &now, Attributes: models.JSONMap{
		"type_info":  map[string]interface{}{"table": commonjson.MapFromStruct(&datatype.TableInfo{Name: "Persons", Fields: fields})},
		"connection": map[string]interface{}{"password": "must-not-leak"},
	}}
	if err := db.Create(&item).Error; err != nil {
		t.Fatal(err)
	}
	facts, err := NewMetadataQueryService(db).GetDataItemSecurityFacts(7, item.Fingerprint)
	if err != nil {
		t.Fatal(err)
	}
	if facts.ItemFingerprint != item.Fingerprint || len(facts.Fields) != 2 || facts.Fields[1].Comment != "手机号" || facts.SourceSnapshotHash == "" {
		t.Fatalf("facts = %#v", facts)
	}
}

func TestGetDocumentSecurityFactsDoesNotFabricateEmptyTableSnapshot(t *testing.T) {
	db := metatest.OpenMetadataDB(t)
	now := time.Now().UTC().Truncate(time.Second)
	item := models.MetaItem{
		TenantID: 7, EngineID: 11, NodeID: 2, ItemType: "file", Name: "contacts.docx",
		FullName: "docs/contacts.docx", Fingerprint: "document-1", ScannedAt: &now,
		Attributes: models.JSONMap{"item": map[string]interface{}{"data_type": "document", "format": "docx"}},
	}
	if err := db.Create(&item).Error; err != nil {
		t.Fatal(err)
	}
	facts, err := NewMetadataQueryService(db).GetDataItemSecurityFacts(7, item.Fingerprint)
	if err != nil {
		t.Fatal(err)
	}
	if len(facts.Fields) != 0 || facts.SourceSnapshotHash != "" {
		t.Fatalf("document facts = %#v", facts)
	}
}
