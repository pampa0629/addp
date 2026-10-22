package repository

import (
	"context"
	"fmt"
	"github.com/addp/model/internal/models"
	"testing"
	"time"
)

func TestPostgresDomainStandardReferencesKeepFrozenEvidenceAndTenantBoundary(t *testing.T) {
	db := openStandardReferenceGuardPostgres(t)
	tx := db.Begin()
	defer tx.Rollback()
	tenant := time.Now().UnixNano()
	domain := int64(31)
	element := int64(9007199254740993)
	revision := int64(9007199254740995)
	entity := models.Entity{TenantID: tenant, DomainID: &domain, Name: "Reference entity", Code: fmt.Sprintf("domain_ref_%d", tenant), Status: "draft", Version: 1, CreatedBy: 1}
	if err := tx.Create(&entity).Error; err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		attribute := models.EntityAttribute{EntityID: entity.ID, ElementID: &element, Name: fmt.Sprintf("Field %d", i), ColumnName: fmt.Sprintf("field_%d", i), DataType: "string", IsPK: i == 0}
		if err := tx.Create(&attribute).Error; err != nil {
			t.Fatal(err)
		}
	}
	repo := NewEntityRepository(tx)
	page, err := repo.ListDomainStandardReferences(context.Background(), tenant, domain, "entity", 1, 5)
	if err != nil || page.Total != 1 || len(page.Data) != 1 || len(page.Data[0].Evidence) != 2 || page.Data[0].SourceIdentity != "9007199254740993" {
		t.Fatalf("page=%+v err=%v", page, err)
	}
	if err := tx.Model(&models.EntityAttribute{}).Where("entity_id=?", entity.ID).Update("element_revision_id", revision).Error; err != nil {
		t.Fatal(err)
	}
	if err := tx.Model(&entity).Update("status", "approved").Error; err != nil {
		t.Fatal(err)
	}
	page, err = repo.ListDomainStandardReferences(context.Background(), tenant, domain, "entity", 1, 5)
	if err != nil || page.Data[0].Evidence[0].RevisionID != "9007199254740995" || page.Data[0].Evidence[0].State != "approved" {
		t.Fatalf("frozen evidence=%+v err=%v", page, err)
	}
	for i := 2; i < 105; i++ {
		attribute := models.EntityAttribute{EntityID: entity.ID, ElementID: &element, ElementRevisionID: &revision, Name: fmt.Sprintf("Extra %d", i), ColumnName: fmt.Sprintf("extra_%d", i), DataType: "string"}
		if err := tx.Create(&attribute).Error; err != nil {
			t.Fatal(err)
		}
	}
	page, err = repo.ListDomainStandardReferences(context.Background(), tenant, domain, "entity", 1, 5)
	if err != nil || page.Total != 1 || len(page.Data[0].Evidence) != 100 || !page.Data[0].EvidenceTruncated {
		t.Fatalf("bounded evidence=%+v err=%v", page, err)
	}
	for _, kind := range []string{"logical_table", "metric_implementation"} {
		empty, err := repo.ListDomainStandardReferences(context.Background(), tenant, domain, kind, 1, 5)
		if err != nil || empty.Total != 0 {
			t.Fatalf("%s empty reference query=%+v err=%v", kind, empty, err)
		}
	}
	for _, ids := range [][2]int64{{tenant + 1, domain}, {tenant, domain + 1}} {
		page, err = repo.ListDomainStandardReferences(context.Background(), ids[0], ids[1], "entity", 1, 5)
		if err != nil || page.Total != 0 {
			t.Fatalf("cross-boundary page=%+v err=%v", page, err)
		}
	}
}
