package service

import (
	"encoding/json"
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
		Definition: "购买产品或服务的主体", EffectiveFrom: &effectiveFrom,
	}, 7, 9, "初始创建")
	if err != nil {
		t.Fatalf("CreateGlossary() error = %v", err)
	}
	if aggregate.DraftRevision == nil || aggregate.DraftRevision.Status != models.RevisionStatusDraft || aggregate.Code != "customer" {
		t.Fatalf("created aggregate = %#v", aggregate)
	}
	if aggregate.DraftRevision.RevisionNo != 1 || aggregate.DraftRevision.ChangeSummary != "初始创建" {
		t.Fatalf("initial revision = %#v", aggregate.DraftRevision)
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
	for _, summary := range []string{"", "   "} {
		if _, err := svc.CreateRevision(aggregate.ID, 7, 9, &models.CreateGlossaryRevisionRequest{Version: aggregate.Version, ChangeSummary: summary}); !errors.Is(err, ErrInvalidStandardRevision) {
			t.Fatalf("CreateRevision(%q) error = %v, want invalid revision", summary, err)
		}
	}
	aggregate, err = svc.CreateRevision(aggregate.ID, 7, 9, &models.CreateGlossaryRevisionRequest{Version: aggregate.Version, ChangeSummary: "补充客户定义"})
	if err != nil || aggregate.DraftRevision.RevisionNo != 2 || aggregate.DraftRevision.ChangeSummary != "补充客户定义" {
		t.Fatalf("CreateRevision() aggregate = %#v, error = %v", aggregate, err)
	}
	if err := svc.DeleteGlossary(aggregate.ID, 7); !errors.Is(err, ErrGlossaryPublicationHistory) {
		t.Fatalf("DeleteGlossary() error = %v, want publication history conflict", err)
	}
}

func TestGlossaryServiceRejectsInvalidScopeAndSelfRelation(t *testing.T) {
	db := openGlossaryServiceTestDB(t)
	svc := NewGlossaryService(repository.NewGlossaryRepository(db), repository.NewTenantReferenceRepository(db))
	if _, err := svc.CreateGlossary(&models.CreateGlossaryRequest{ScopeType: models.StandardScopeDomain, Code: "customer", Name: "客户", Definition: "定义"}, 7, 9, "初始创建"); !errors.Is(err, ErrInvalidStandardScope) {
		t.Fatalf("CreateGlossary() error = %v, want invalid scope", err)
	}
	aggregate, err := svc.CreateGlossary(&models.CreateGlossaryRequest{ScopeType: models.StandardScopeTenantCommon, Code: "customer", Name: "客户", Definition: "定义"}, 7, 9, "初始创建")
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
		Definition: "尚未发布的临时定义",
	}, 7, 9, "初始创建")
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

func TestGlossaryServiceListPreservesWithdrawnContent(t *testing.T) {
	db := openGlossaryServiceTestDB(t)
	repo := repository.NewGlossaryRepository(db)
	svc := NewGlossaryService(repo, repository.NewTenantReferenceRepository(db))
	from := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	aggregate, err := svc.CreateGlossary(&models.CreateGlossaryRequest{
		ScopeType: models.StandardScopeTenantCommon, Code: "withdrawn_term", Name: "已撤回术语", Definition: "保留的业务定义", EffectiveFrom: &from,
	}, 7, 9, "初始创建")
	if err != nil {
		t.Fatal(err)
	}
	revisionID := aggregate.DraftRevision.ID
	aggregate, err = svc.SubmitRevision(aggregate.ID, revisionID, 7, 9, aggregate.Version)
	if err != nil {
		t.Fatal(err)
	}
	aggregate, err = svc.PublishRevision(aggregate.ID, revisionID, 7, 9, aggregate.Version)
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.WithdrawRevision(aggregate.ID, revisionID, 7, 9, aggregate.Version)
	if err != nil {
		t.Fatal(err)
	}
	items, total, err := svc.ListGlossaries(7, repository.ListGlossaryOptions{Status: models.RevisionStatusWithdrawn})
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(items) != 1 || items[0].CurrentRevision != nil || items[0].DraftRevision != nil || !items[0].HasPublicationHistory {
		t.Fatalf("withdrawn aggregate = %#v, total = %d", items, total)
	}
	payload, err := json.Marshal(items[0])
	if err != nil {
		t.Fatal(err)
	}
	var projection struct {
		LatestRevision *models.GlossaryRevision `json:"latest_revision"`
	}
	if err := json.Unmarshal(payload, &projection); err != nil {
		t.Fatal(err)
	}
	if projection.LatestRevision == nil || projection.LatestRevision.Status != models.RevisionStatusWithdrawn || projection.LatestRevision.Name != "已撤回术语" || projection.LatestRevision.Definition != "保留的业务定义" {
		t.Fatalf("latest history missing from list response: %s", payload)
	}
	if err := svc.DeleteGlossary(aggregate.ID, 7); !errors.Is(err, ErrGlossaryPublicationHistory) {
		t.Fatalf("withdrawn deletion = %v, want publication history conflict", err)
	}
}

func TestGlossaryAggregateLatestHistoryDoesNotBecomeEffective(t *testing.T) {
	for _, scenario := range []string{"withdrawn", "future", "expired"} {
		t.Run(scenario, func(t *testing.T) {
			db := openGlossaryServiceTestDB(t)
			identity := models.Glossary{TenantID: 7, ScopeType: models.StandardScopeTenantCommon, Code: "history_only", CreatedBy: 9, Version: 1, LifecycleState: "active"}
			if err := db.Create(&identity).Error; err != nil {
				t.Fatal(err)
			}
			past := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
			end := past.AddDate(1, 0, 0)
			old := models.GlossaryRevision{GlossaryID: identity.ID, RevisionNo: 1, Name: "旧定义", Definition: "已失效的旧内容", Status: models.RevisionStatusPublished, EffectiveFrom: &past, EffectiveTo: &end, ChangeSummary: "initial", CreatedBy: 9}
			if err := db.Create(&old).Error; err != nil {
				t.Fatal(err)
			}
			latest := old
			latest.ID, latest.RevisionNo, latest.Name = 0, 2, "最新历史定义"
			switch scenario {
			case "withdrawn":
				latest.Status = models.RevisionStatusWithdrawn
			case "future":
				future := past.AddDate(10, 0, 0)
				latest.EffectiveFrom, latest.EffectiveTo = &future, nil
			case "expired":
				laterEnd := end.AddDate(1, 0, 0)
				latest.EffectiveFrom, latest.EffectiveTo = &end, &laterEnd
			}
			if err := db.Create(&latest).Error; err != nil {
				t.Fatal(err)
			}
			aggregate, err := repository.NewGlossaryRepository(db).GetAggregateAt(identity.ID, 7, past.AddDate(6, 0, 0))
			if err != nil {
				t.Fatal(err)
			}
			if aggregate.CurrentRevision != nil || aggregate.DraftRevision != nil || aggregate.LatestRevision == nil || aggregate.LatestRevision.ID != latest.ID || !aggregate.HasPublicationHistory {
				t.Fatalf("history confused with effective content: %#v", aggregate)
			}
		})
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
		`CREATE TABLE standard.glossaries (id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL, scope_type TEXT NOT NULL, owner_domain_id INTEGER, code TEXT NOT NULL, tags TEXT, draft_revision_id INTEGER, created_by INTEGER NOT NULL, updated_by INTEGER, created_at DATETIME, updated_at DATETIME, version INTEGER NOT NULL DEFAULT 1, lifecycle_state TEXT NOT NULL)`,
		`CREATE UNIQUE INDEX standard.uq_glossary_test_code ON glossaries (tenant_id, code)`,
		`CREATE TABLE standard.glossary_revisions (id INTEGER PRIMARY KEY AUTOINCREMENT, glossary_id INTEGER NOT NULL REFERENCES glossaries(id) ON DELETE CASCADE, revision_no INTEGER NOT NULL, status TEXT NOT NULL, name TEXT NOT NULL, alias TEXT, definition TEXT NOT NULL, example TEXT, note TEXT, related_ids TEXT, change_summary TEXT NOT NULL, effective_from DATETIME, effective_to DATETIME, submitted_by INTEGER, submitted_at DATETIME, published_by INTEGER, published_at DATETIME, created_by INTEGER NOT NULL, updated_by INTEGER, created_at DATETIME, updated_at DATETIME)`,
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	}
	return db
}
