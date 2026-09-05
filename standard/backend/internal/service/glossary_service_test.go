package service

import (
	"errors"
	"testing"
	"time"

	"github.com/addp/standard/internal/models"
	"github.com/addp/standard/internal/repository"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestGlossaryServiceCreatesAndPublishesRevision(t *testing.T) {
	db := openGlossaryServiceTestDB(t)
	svc := NewGlossaryService(repository.NewGlossaryRepository(db), repository.NewTenantReferenceRepository(db))
	effectiveFrom := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	aggregate, err := svc.CreateGlossary(&models.CreateGlossaryRequest{
		ScopeType: models.StandardScopeTenantCommon, Code: "customer", Name: "客户",
		Definition: "购买产品或服务的主体", ChangeSummary: "初始定义", EffectiveFrom: &effectiveFrom,
	}, 7, 9)
	if err != nil {
		t.Fatalf("CreateGlossary() error = %v", err)
	}
	if aggregate.DraftRevision == nil || aggregate.DraftRevision.Status != models.RevisionStatusDraft || aggregate.Code != "customer" {
		t.Fatalf("created aggregate = %#v", aggregate)
	}
	if aggregate.HasPublicationHistory {
		t.Fatal("new draft glossary must not report publication history")
	}
	aggregate, err = svc.SubmitRevision(aggregate.ID, aggregate.DraftRevision.ID, 7, 9, aggregate.Version)
	if err != nil {
		t.Fatalf("SubmitRevision() error = %v", err)
	}
	aggregate, err = svc.PublishRevision(aggregate.ID, aggregate.DraftRevision.ID, 7, 10, aggregate.Version)
	if err != nil {
		t.Fatalf("PublishRevision() error = %v", err)
	}
	if aggregate.DraftRevision != nil || aggregate.CurrentRevision == nil || aggregate.CurrentRevision.Status != models.RevisionStatusPublished {
		t.Fatalf("published aggregate = %#v", aggregate)
	}
	if !aggregate.HasPublicationHistory {
		t.Fatal("published glossary must report publication history")
	}
	if err := svc.DeleteGlossary(aggregate.ID, 7); !errors.Is(err, ErrGlossaryPublicationHistory) {
		t.Fatalf("DeleteGlossary() error = %v, want publication history conflict", err)
	}
}

func TestGlossaryServiceRejectsInvalidScopeAndSelfRelation(t *testing.T) {
	db := openGlossaryServiceTestDB(t)
	svc := NewGlossaryService(repository.NewGlossaryRepository(db), repository.NewTenantReferenceRepository(db))
	if _, err := svc.CreateGlossary(&models.CreateGlossaryRequest{ScopeType: models.StandardScopeDomain, Code: "customer", Name: "客户", Definition: "定义", ChangeSummary: "初始"}, 7, 9); !errors.Is(err, ErrInvalidStandardScope) {
		t.Fatalf("CreateGlossary() error = %v, want invalid scope", err)
	}
	aggregate, err := svc.CreateGlossary(&models.CreateGlossaryRequest{ScopeType: models.StandardScopeTenantCommon, Code: "customer", Name: "客户", Definition: "定义", ChangeSummary: "初始"}, 7, 9)
	if err != nil {
		t.Fatalf("CreateGlossary() error = %v", err)
	}
	_, err = svc.UpdateRevision(aggregate.ID, aggregate.DraftRevision.ID, 7, 9, &models.UpdateGlossaryRevisionRequest{
		Name: "客户", Definition: "定义", RelatedIDs: []int64{aggregate.ID}, ChangeSummary: "关联自身", Version: aggregate.Version,
	})
	if !errors.Is(err, ErrInvalidStandardRevision) {
		t.Fatalf("UpdateRevision() error = %v, want invalid self relation", err)
	}
}

func TestGlossaryServiceDeletesNeverPublishedIdentityAndDraft(t *testing.T) {
	db := openGlossaryServiceTestDB(t)
	svc := NewGlossaryService(repository.NewGlossaryRepository(db), repository.NewTenantReferenceRepository(db))
	aggregate, err := svc.CreateGlossary(&models.CreateGlossaryRequest{
		ScopeType: models.StandardScopeTenantCommon, Code: "temporary", Name: "临时术语",
		Definition: "尚未发布的临时定义", ChangeSummary: "初始定义",
	}, 7, 9)
	if err != nil {
		t.Fatalf("CreateGlossary() error = %v", err)
	}
	if err := svc.DeleteGlossary(aggregate.ID, 7); err != nil {
		t.Fatalf("DeleteGlossary() error = %v", err)
	}
	var identityCount, revisionCount int64
	if err := db.Model(&models.Glossary{}).Where("id = ?", aggregate.ID).Count(&identityCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&models.GlossaryRevision{}).Where("glossary_id = ?", aggregate.ID).Count(&revisionCount).Error; err != nil {
		t.Fatal(err)
	}
	if identityCount != 0 || revisionCount != 0 {
		t.Fatalf("remaining identity=%d revisions=%d, want both zero", identityCount, revisionCount)
	}
}

func openGlossaryServiceTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared&_foreign_keys=1"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("ATTACH DATABASE ':memory:' AS standard").Error; err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`CREATE TABLE standard.domains (id INTEGER PRIMARY KEY, tenant_id INTEGER NOT NULL, lifecycle_state TEXT NOT NULL)`,
		`CREATE TABLE standard.glossaries (id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL, scope_type TEXT NOT NULL, owner_domain_id INTEGER, code TEXT NOT NULL, steward_id INTEGER, tags TEXT, draft_revision_id INTEGER, created_by INTEGER NOT NULL, updated_by INTEGER, created_at DATETIME, updated_at DATETIME, version INTEGER NOT NULL DEFAULT 1, lifecycle_state TEXT NOT NULL)`,
		`CREATE UNIQUE INDEX standard.uq_glossary_test_code ON glossaries (tenant_id, code)`,
		`CREATE TABLE standard.glossary_revisions (id INTEGER PRIMARY KEY AUTOINCREMENT, glossary_id INTEGER NOT NULL REFERENCES glossaries(id) ON DELETE CASCADE, revision_no INTEGER NOT NULL, status TEXT NOT NULL, name TEXT NOT NULL, alias TEXT, definition TEXT NOT NULL, example TEXT, note TEXT, related_ids TEXT, change_summary TEXT NOT NULL, effective_from DATETIME, effective_to DATETIME, submitted_by INTEGER, submitted_at DATETIME, published_by INTEGER, published_at DATETIME, created_by INTEGER NOT NULL, updated_by INTEGER, created_at DATETIME, updated_at DATETIME)`,
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	}
	return db
}
