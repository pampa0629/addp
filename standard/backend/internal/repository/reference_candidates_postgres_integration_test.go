package repository

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/addp/standard/internal/models"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestPostgresReferenceCandidatesFilterAndPaginateOwnerFacts(t *testing.T) {
	dsn := os.Getenv("STANDARD_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("STANDARD_POSTGRES_TEST_DSN is not set")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	tenantID := time.Now().UnixNano()
	codeSuffix := fmt.Sprintf("_%d", tenantID)
	rows := []any{
		&models.Domain{TenantID: tenantID, Name: "Sales", Code: "sales" + codeSuffix, CreatedBy: 1, Version: 1, LifecycleState: "active"},
		&models.Domain{TenantID: tenantID, Name: "Legacy Sales", Code: "legacy_sales" + codeSuffix, CreatedBy: 1, Version: 1, LifecycleState: "deleting"},
		&models.Glossary{TenantID: tenantID, Name: "Customer", Definition: "Approved customer term", Status: "approved", CreatedBy: 1, Version: 1},
		&models.Glossary{TenantID: tenantID, Name: "Customer draft", Definition: "Draft customer term", Status: "draft", CreatedBy: 1, Version: 1},
	}
	for _, row := range rows {
		if err := db.Create(row).Error; err != nil {
			t.Fatalf("create candidate fixture: %v", err)
		}
	}
	published := models.Element{TenantID: tenantID, Code: "customer_id" + codeSuffix, CreatedBy: 1, Version: 1, LifecycleState: "active"}
	draft := models.Element{TenantID: tenantID, Code: "customer_legacy_id" + codeSuffix, CreatedBy: 1, Version: 1, LifecycleState: "active"}
	for _, fixture := range []struct {
		identity     *models.Element
		name, status string
	}{{&published, "Customer ID", models.RevisionStatusPublished}, {&draft, "Customer legacy ID", models.RevisionStatusDraft}} {
		if err := db.Create(fixture.identity).Error; err != nil {
			t.Fatalf("create element identity: %v", err)
		}
		revision := models.ElementRevision{ElementID: fixture.identity.ID, RevisionNo: 1, Status: fixture.status, Name: fixture.name, Definition: fixture.name, DataType: "string", ValueDomainKind: models.ValueDomainUnrestricted, ChangeSummary: "initial", CreatedBy: 1}
		if fixture.status == models.RevisionStatusPublished {
			effectiveFrom := time.Now().UTC().Add(-time.Hour)
			revision.EffectiveFrom = &effectiveFrom
		}
		if err := db.Create(&revision).Error; err != nil {
			t.Fatalf("create element revision: %v", err)
		}
		if fixture.status == models.RevisionStatusDraft {
			if err := db.Model(fixture.identity).Update("draft_revision_id", revision.ID).Error; err != nil {
				t.Fatalf("link element revision: %v", err)
			}
		}
	}
	t.Cleanup(func() {
		_ = db.Exec("DELETE FROM standard.elements WHERE tenant_id = ?", tenantID).Error
		_ = db.Exec("DELETE FROM standard.glossaries WHERE tenant_id = ?", tenantID).Error
		_ = db.Exec("DELETE FROM standard.domains WHERE tenant_id = ?", tenantID).Error
	})

	repository := NewReferenceResolutionRepository(db)
	domains, total, err := repository.ListDomainCandidates(context.Background(), tenantID, "sales", 1, 20)
	if err != nil || total != 1 || len(domains) != 1 || domains[0].Name != "Sales" {
		t.Fatalf("domains=%#v total=%d err=%v", domains, total, err)
	}
	glossaries, total, err := repository.ListGlossaryCandidates(context.Background(), tenantID, "customer", 1, 20)
	if err != nil || total != 1 || len(glossaries) != 1 || glossaries[0].Status != "approved" {
		t.Fatalf("glossaries=%#v total=%d err=%v", glossaries, total, err)
	}
	elements, total, err := repository.ListElementCandidates(context.Background(), tenantID, "customer", 1, 20)
	if err != nil || total != 1 || len(elements) != 1 || elements[0].Code != "customer_id"+codeSuffix || elements[0].ScopeType != models.StandardScopeTenantCommon || elements[0].OwnerDomainID != nil {
		t.Fatalf("elements=%#v total=%d err=%v", elements, total, err)
	}
}
