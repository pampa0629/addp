package service

import (
	"errors"
	"testing"

	"github.com/addp/standard/internal/models"
	"github.com/addp/standard/internal/repository"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestFormalizeCandidateCreatesGlossaryIdentityAtomically(t *testing.T) {
	db := openCandidateFormalizationTestDB(t)
	candidate := seedCandidateFormalizationContext(t, db, "candidate_term", "候选术语", "候选定义")
	svc := NewDocumentService(repository.NewDocumentRepository(db), nil, nil, DocumentStorageOptions{})
	defer svc.Stop()

	result, err := svc.FormalizeCandidate(candidate.ID, 7, 11, &models.FormalizeDocumentExtractionCandidateRequest{Version: 1, ChangeSummary: "由户外标准文档提炼"}, CandidateFormalizationAuthorization{Create: map[string]bool{"glossary": true}})
	if err != nil {
		t.Fatalf("FormalizeCandidate() error = %v", err)
	}
	if result.Action != models.CandidateFormalizationCreatedIdentity || result.RevisionNo != 1 || result.TargetRevisionStatus != models.RevisionStatusDraft || result.CandidateVersion != 2 {
		t.Fatalf("result = %+v", result)
	}
	var identity models.Glossary
	if err := db.Where("tenant_id = ? AND code = ?", 7, candidate.Code).First(&identity).Error; err != nil {
		t.Fatalf("load glossary identity: %v", err)
	}
	if identity.DraftRevisionID == nil || identity.ScopeType != models.StandardScopeDomain || identity.OwnerDomainID == nil || *identity.OwnerDomainID != 3 {
		t.Fatalf("identity = %+v", identity)
	}
	var revision models.GlossaryRevision
	if err := db.First(&revision, result.RevisionID).Error; err != nil {
		t.Fatalf("load glossary revision: %v", err)
	}
	if revision.Name != candidate.Name || revision.Definition != candidate.Definition || revision.Status != models.RevisionStatusDraft {
		t.Fatalf("revision = %+v", revision)
	}

	_, err = svc.FormalizeCandidate(candidate.ID, 7, 11, &models.FormalizeDocumentExtractionCandidateRequest{Version: 2, ChangeSummary: "duplicate"}, CandidateFormalizationAuthorization{Create: map[string]bool{"glossary": true}, Update: map[string]bool{"glossary": true}})
	if !errors.Is(err, ErrCandidateAlreadyFormalized) {
		t.Fatalf("duplicate error = %v, want ErrCandidateAlreadyFormalized", err)
	}
	if _, err := svc.UpdateCandidateStatus(candidate.ID, 7, 11, &models.UpdateDocumentExtractionCandidateRequest{Version: 2, Status: "rejected"}); !errors.Is(err, ErrCandidateAlreadyFormalized) {
		t.Fatalf("post-formalization review error = %v, want ErrCandidateAlreadyFormalized", err)
	}
	if err := svc.DeleteDocument(1, 7); !errors.Is(err, ErrDocumentCandidateFormalizationHistory) {
		t.Fatalf("source document delete error = %v, want ErrDocumentCandidateFormalizationHistory", err)
	}
}

func TestFormalizeCandidateLinksExactRevisionAndRequiresUpdatePermission(t *testing.T) {
	db := openCandidateFormalizationTestDB(t)
	candidate := seedCandidateFormalizationContext(t, db, "existing_term", "户外活动", "户外活动定义")
	identity := models.Glossary{TenantID: 7, ScopeType: models.StandardScopeDomain, OwnerDomainID: int64Pointer(3), Code: candidate.Code, CreatedBy: 1, LifecycleState: "active"}
	if err := db.Create(&identity).Error; err != nil {
		t.Fatal(err)
	}
	revision := models.GlossaryRevision{GlossaryID: identity.ID, RevisionNo: 1, Status: models.RevisionStatusPublished, Name: candidate.Name, Definition: candidate.Definition, ChangeSummary: "initial", CreatedBy: 1}
	if err := db.Create(&revision).Error; err != nil {
		t.Fatal(err)
	}

	svc := NewDocumentService(repository.NewDocumentRepository(db), nil, nil, DocumentStorageOptions{})
	defer svc.Stop()
	request := &models.FormalizeDocumentExtractionCandidateRequest{Version: 1, ChangeSummary: "确认来源"}
	if _, err := svc.FormalizeCandidate(candidate.ID, 7, 11, request, CandidateFormalizationAuthorization{Create: map[string]bool{"glossary": true}}); !errors.Is(err, ErrCandidateFormalizationDenied) {
		t.Fatalf("permission error = %v, want ErrCandidateFormalizationDenied", err)
	}
	result, err := svc.FormalizeCandidate(candidate.ID, 7, 11, request, CandidateFormalizationAuthorization{Update: map[string]bool{"glossary": true}})
	if err != nil {
		t.Fatalf("FormalizeCandidate() error = %v", err)
	}
	if result.Action != models.CandidateFormalizationLinkedExisting || result.RevisionID != revision.ID {
		t.Fatalf("result = %+v", result)
	}
	var count int64
	if err := db.Model(&models.GlossaryRevision{}).Where("glossary_id = ?", identity.ID).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("revision count = %d, want 1", count)
	}
}

func TestFormalizeCandidateCreatesRevisionAndRejectsWorkRevisionConflict(t *testing.T) {
	db := openCandidateFormalizationTestDB(t)
	candidate := seedCandidateFormalizationContext(t, db, "changed_term", "新名称", "新定义")
	identity := models.Glossary{TenantID: 7, ScopeType: models.StandardScopeDomain, OwnerDomainID: int64Pointer(3), Code: candidate.Code, CreatedBy: 1, LifecycleState: "active"}
	if err := db.Create(&identity).Error; err != nil {
		t.Fatal(err)
	}
	revision := models.GlossaryRevision{GlossaryID: identity.ID, RevisionNo: 1, Status: models.RevisionStatusWithdrawn, Name: "旧名称", Definition: "旧定义", ChangeSummary: "initial", CreatedBy: 1}
	if err := db.Create(&revision).Error; err != nil {
		t.Fatal(err)
	}

	svc := NewDocumentService(repository.NewDocumentRepository(db), nil, nil, DocumentStorageOptions{})
	defer svc.Stop()
	result, err := svc.FormalizeCandidate(candidate.ID, 7, 11, &models.FormalizeDocumentExtractionCandidateRequest{Version: 1, ChangeSummary: "候选更新"}, CandidateFormalizationAuthorization{Update: map[string]bool{"glossary": true}})
	if err != nil {
		t.Fatalf("FormalizeCandidate() error = %v", err)
	}
	if result.Action != models.CandidateFormalizationCreatedRevision || result.RevisionNo != 2 {
		t.Fatalf("result = %+v", result)
	}
	var created models.GlossaryRevision
	if err := db.First(&created, result.RevisionID).Error; err != nil {
		t.Fatal(err)
	}
	if created.Name != candidate.Name || created.Definition != candidate.Definition || created.Status != models.RevisionStatusDraft {
		t.Fatalf("created revision = %+v", created)
	}

	second := seedCandidateFormalizationContext(t, db, candidate.Code, "又一名称", "又一定义")
	_, err = svc.FormalizeCandidate(second.ID, 7, 11, &models.FormalizeDocumentExtractionCandidateRequest{Version: 1, ChangeSummary: "conflict"}, CandidateFormalizationAuthorization{Update: map[string]bool{"glossary": true}})
	if !errors.Is(err, ErrCandidateTargetDraftExists) {
		t.Fatalf("work revision error = %v, want ErrCandidateTargetDraftExists", err)
	}
}

func TestFormalizeCandidateCopiesBaselineCodeItemsWhenCandidateDoesNotReplaceThem(t *testing.T) {
	db := openCandidateFormalizationTestDB(t)
	candidate := seedTypedCandidateFormalizationContext(t, db, "code_set", "activity_status", "活动状态新名称", "活动状态新定义", models.DocumentExtractionCandidatePayload{DataType: stringPointer("string")})
	identity := models.CodeSet{TenantID: 7, ScopeType: models.StandardScopeDomain, OwnerDomainID: int64Pointer(3), Code: candidate.Code, Origin: models.CodeSetOriginTenant, CreatedBy: 1, LifecycleState: "active"}
	if err := db.Create(&identity).Error; err != nil {
		t.Fatal(err)
	}
	revision := models.CodeSetRevision{CodeSetID: identity.ID, RevisionNo: 1, Status: models.RevisionStatusWithdrawn, Name: "活动状态", Description: "旧定义", ValueType: "string", ChangeSummary: "initial", CreatedBy: 1}
	if err := db.Create(&revision).Error; err != nil {
		t.Fatal(err)
	}
	for index, code := range []string{"planned", "completed"} {
		if err := db.Create(&models.CodeSetRevisionItem{CodeSetRevisionID: revision.ID, Code: code, Label: code, SortOrder: index, Status: models.CodeItemStatusActive}).Error; err != nil {
			t.Fatal(err)
		}
	}

	svc := NewDocumentService(repository.NewDocumentRepository(db), nil, nil, DocumentStorageOptions{})
	defer svc.Stop()
	result, err := svc.FormalizeCandidate(candidate.ID, 7, 11, &models.FormalizeDocumentExtractionCandidateRequest{Version: 1, ChangeSummary: "更新定义"}, CandidateFormalizationAuthorization{Update: map[string]bool{"code_set": true}})
	if err != nil {
		t.Fatalf("FormalizeCandidate() error = %v", err)
	}
	if result.Action != models.CandidateFormalizationCreatedRevision {
		t.Fatalf("result = %+v", result)
	}
	var items []models.CodeSetRevisionItem
	if err := db.Where("code_set_revision_id = ?", result.RevisionID).Order("sort_order ASC").Find(&items).Error; err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].Code != "planned" || items[1].Code != "completed" {
		t.Fatalf("copied items = %+v", items)
	}
}

func openCandidateFormalizationTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("ATTACH DATABASE ':memory:' AS standard").Error; err != nil {
		t.Fatal(err)
	}
	statements := []string{
		`CREATE TABLE standard.documents (id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL, scope_type TEXT NOT NULL, owner_domain_id INTEGER, code TEXT NOT NULL, doc_type TEXT NOT NULL, source_org TEXT, steward_id INTEGER, tags TEXT, draft_revision_id INTEGER, created_by INTEGER NOT NULL, updated_by INTEGER, created_at DATETIME, updated_at DATETIME, version INTEGER NOT NULL DEFAULT 1, lifecycle_state TEXT NOT NULL)`,
		`CREATE UNIQUE INDEX standard.uq_candidate_documents_tenant_code ON documents (tenant_id, code)`,
		`CREATE TABLE standard.document_revisions (id INTEGER PRIMARY KEY AUTOINCREMENT, document_id INTEGER NOT NULL, revision_no INTEGER NOT NULL, status TEXT NOT NULL, name TEXT NOT NULL, version_label TEXT, publish_date DATETIME, description TEXT, file_key TEXT, file_name TEXT, file_size INTEGER, media_type TEXT, content_sha256 TEXT, change_summary TEXT NOT NULL, effective_from DATETIME, effective_to DATETIME, submitted_by INTEGER, submitted_at DATETIME, published_by INTEGER, published_at DATETIME, created_by INTEGER NOT NULL, updated_by INTEGER, created_at DATETIME, updated_at DATETIME)`,
		`CREATE TABLE standard.document_extractions (id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL, document_revision_id INTEGER NOT NULL, status TEXT NOT NULL, requested_by INTEGER NOT NULL, created_at DATETIME)`,
		`CREATE TABLE standard.document_extraction_candidates (id INTEGER PRIMARY KEY AUTOINCREMENT, extraction_id INTEGER NOT NULL, candidate_type TEXT NOT NULL, code TEXT NOT NULL, name TEXT NOT NULL, definition TEXT NOT NULL, payload TEXT, status TEXT NOT NULL, version INTEGER NOT NULL DEFAULT 1, reviewed_by INTEGER, reviewed_at DATETIME, created_at DATETIME, updated_at DATETIME)`,
		`CREATE TABLE standard.document_candidate_formalizations (candidate_id INTEGER PRIMARY KEY, action TEXT NOT NULL, standard_id INTEGER NOT NULL, standard_code TEXT NOT NULL, revision_id INTEGER NOT NULL, revision_no INTEGER NOT NULL, target_revision_status TEXT NOT NULL, change_summary TEXT NOT NULL, created_by INTEGER NOT NULL, created_at DATETIME)`,
		`CREATE TABLE standard.glossaries (id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL, scope_type TEXT NOT NULL, owner_domain_id INTEGER, code TEXT NOT NULL, steward_id INTEGER, tags TEXT, draft_revision_id INTEGER, created_by INTEGER NOT NULL, updated_by INTEGER, created_at DATETIME, updated_at DATETIME, version INTEGER NOT NULL DEFAULT 1, lifecycle_state TEXT NOT NULL)`,
		`CREATE UNIQUE INDEX standard.uq_candidate_glossaries_tenant_code ON glossaries (tenant_id, code)`,
		`CREATE TABLE standard.glossary_revisions (id INTEGER PRIMARY KEY AUTOINCREMENT, glossary_id INTEGER NOT NULL, revision_no INTEGER NOT NULL, status TEXT NOT NULL, name TEXT NOT NULL, alias TEXT, definition TEXT NOT NULL, example TEXT, note TEXT, related_ids TEXT, change_summary TEXT NOT NULL, effective_from DATETIME, effective_to DATETIME, submitted_by INTEGER, submitted_at DATETIME, published_by INTEGER, published_at DATETIME, created_by INTEGER NOT NULL, updated_by INTEGER, created_at DATETIME, updated_at DATETIME)`,
		`CREATE TABLE standard.code_sets (id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL, scope_type TEXT NOT NULL, owner_domain_id INTEGER, code TEXT NOT NULL, origin TEXT NOT NULL, steward_id INTEGER, tags TEXT, draft_revision_id INTEGER, created_by INTEGER NOT NULL, updated_by INTEGER, created_at DATETIME, updated_at DATETIME, version INTEGER NOT NULL DEFAULT 1, lifecycle_state TEXT NOT NULL)`,
		`CREATE UNIQUE INDEX standard.uq_candidate_code_sets_tenant_code ON code_sets (tenant_id, code)`,
		`CREATE TABLE standard.code_set_revisions (id INTEGER PRIMARY KEY AUTOINCREMENT, code_set_id INTEGER NOT NULL, revision_no INTEGER NOT NULL, status TEXT NOT NULL, name TEXT NOT NULL, description TEXT NOT NULL, value_type TEXT NOT NULL, change_summary TEXT NOT NULL, effective_from DATETIME, effective_to DATETIME, submitted_by INTEGER, submitted_at DATETIME, published_by INTEGER, published_at DATETIME, created_by INTEGER NOT NULL, updated_by INTEGER, created_at DATETIME, updated_at DATETIME)`,
		`CREATE TABLE standard.code_set_revision_items (id INTEGER PRIMARY KEY AUTOINCREMENT, code_set_revision_id INTEGER NOT NULL, code TEXT NOT NULL, label TEXT NOT NULL, definition TEXT, sort_order INTEGER NOT NULL, status TEXT NOT NULL, replacement_item_id INTEGER, created_at DATETIME, updated_at DATETIME)`,
		`CREATE UNIQUE INDEX standard.uq_candidate_code_items_revision_code ON code_set_revision_items (code_set_revision_id, code)`,
	}
	for _, statement := range statements {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("create candidate formalization test schema: %v", err)
		}
	}
	return db
}

func seedCandidateFormalizationContext(t *testing.T, db *gorm.DB, code, name, definition string) models.DocumentExtractionCandidate {
	return seedTypedCandidateFormalizationContext(t, db, "glossary", code, name, definition, models.DocumentExtractionCandidatePayload{})
}

func seedTypedCandidateFormalizationContext(t *testing.T, db *gorm.DB, candidateType, code, name, definition string, payload models.DocumentExtractionCandidatePayload) models.DocumentExtractionCandidate {
	t.Helper()
	var documentCount int64
	if err := db.Table("standard.documents").Count(&documentCount).Error; err != nil {
		t.Fatal(err)
	}
	document := models.Document{TenantID: 7, ScopeType: models.StandardScopeDomain, OwnerDomainID: int64Pointer(3), Code: "doc_" + code + "_" + string(rune('a'+documentCount)), DocType: "internal", CreatedBy: 1, LifecycleState: "active"}
	if err := db.Create(&document).Error; err != nil {
		t.Fatal(err)
	}
	revision := models.DocumentRevision{DocumentID: document.ID, RevisionNo: 1, Status: models.RevisionStatusDraft, Name: "户外文档", ChangeSummary: "initial", CreatedBy: 1}
	if err := db.Create(&revision).Error; err != nil {
		t.Fatal(err)
	}
	extraction := models.DocumentExtraction{TenantID: 7, DocumentRevisionID: revision.ID, Status: "completed", RequestedBy: 1}
	if err := db.Create(&extraction).Error; err != nil {
		t.Fatal(err)
	}
	candidate := models.DocumentExtractionCandidate{ExtractionID: extraction.ID, CandidateType: candidateType, Code: code, Name: name, Definition: definition, Payload: payload, Status: "retained", Version: 1}
	if err := db.Create(&candidate).Error; err != nil {
		t.Fatal(err)
	}
	return candidate
}

func int64Pointer(value int64) *int64 { return &value }
