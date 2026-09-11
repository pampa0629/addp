package repository

import (
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/addp/standard/internal/models"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestPostgresDocumentCandidateFormalization(t *testing.T) {
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
	code := fmt.Sprintf("outdoor_formalization_%d", tenantID)
	document := models.Document{TenantID: tenantID, ScopeType: models.StandardScopeTenantCommon, Code: "doc_" + code, DocType: "internal", CreatedBy: 1, Version: 1, LifecycleState: "active"}
	if err := db.Create(&document).Error; err != nil {
		t.Fatal(err)
	}
	documentRevision := models.DocumentRevision{DocumentID: document.ID, RevisionNo: 1, Status: models.RevisionStatusDraft, Name: "户外标准", ChangeSummary: "initial", CreatedBy: 1}
	if err := db.Create(&documentRevision).Error; err != nil {
		t.Fatal(err)
	}
	extraction := models.DocumentExtraction{TenantID: tenantID, DocumentRevisionID: documentRevision.ID, Status: "completed", RequestedBy: 1}
	if err := db.Create(&extraction).Error; err != nil {
		t.Fatal(err)
	}
	candidate := models.DocumentExtractionCandidate{ExtractionID: extraction.ID, CandidateType: "glossary", Code: code, Name: "户外活动", Definition: "在户外开展的活动", Status: "retained", Version: 1}
	if err := db.Create(&candidate).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = db.Where("candidate_id = ?", candidate.ID).Delete(&models.DocumentCandidateFormalization{}).Error
		_ = db.Where("tenant_id = ?", tenantID).Delete(&models.Glossary{}).Error
		_ = db.Where("tenant_id = ?", tenantID).Delete(&models.Document{}).Error
	})

	repo := NewDocumentRepository(db)
	t.Run("rejects historical non-representative candidate", func(t *testing.T) {
		historicalCode := fmt.Sprintf("outdoor_historical_%d", tenantID)
		historicalDocument := models.Document{TenantID: tenantID, ScopeType: models.StandardScopeTenantCommon, Code: "doc_" + historicalCode, DocType: "internal", CreatedBy: 1, Version: 1, LifecycleState: "active"}
		if err := db.Create(&historicalDocument).Error; err != nil {
			t.Fatal(err)
		}
		historicalRevision := models.DocumentRevision{DocumentID: historicalDocument.ID, RevisionNo: 1, Status: models.RevisionStatusDraft, Name: "历史正式化校验", ChangeSummary: "initial", CreatedBy: 1}
		if err := db.Create(&historicalRevision).Error; err != nil {
			t.Fatal(err)
		}
		historicalExtraction := models.DocumentExtraction{TenantID: tenantID, DocumentRevisionID: historicalRevision.ID, Status: "completed", RequestedBy: 1}
		if err := db.Create(&historicalExtraction).Error; err != nil {
			t.Fatal(err)
		}
		firstReviewedAt := time.Now().UTC().Add(-time.Minute)
		currentReviewedAt := firstReviewedAt.Add(time.Minute)
		reviewerID := int64(9)
		historicalCandidates := []models.DocumentExtractionCandidate{
			{ExtractionID: historicalExtraction.ID, CandidateType: "glossary", Code: historicalCode, Name: "户外 活动", Definition: "同一定义", Status: "retained", Version: 1, ReviewedBy: &reviewerID, ReviewedAt: &firstReviewedAt},
			{ExtractionID: historicalExtraction.ID, CandidateType: "glossary", Code: historicalCode, Name: "户外\n活动", Definition: "同一定义", Status: "retained", Version: 1, ReviewedBy: &reviewerID, ReviewedAt: &currentReviewedAt},
		}
		if err := db.Create(&historicalCandidates).Error; err != nil {
			t.Fatal(err)
		}
		_, err := repo.FormalizeCandidate(historicalCandidates[0].ID, tenantID, 9, DocumentCandidateFormalizationPlan{
			Action: models.CandidateFormalizationCreatedIdentity, CandidateType: "glossary", CandidateVersion: 1,
			SourceDocumentVersion: 1, ChangeSummary: "历史出现记录不得正式化",
		})
		if !errors.Is(err, ErrCandidateRepresentativeStale) {
			t.Fatalf("error = %v, want ErrCandidateRepresentativeStale", err)
		}
		var formalizationCount, glossaryCount int64
		ids := []int64{historicalCandidates[0].ID, historicalCandidates[1].ID}
		if err := db.Model(&models.DocumentCandidateFormalization{}).Where("candidate_id IN ?", ids).Count(&formalizationCount).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Model(&models.Glossary{}).Where("tenant_id = ? AND code = ?", tenantID, historicalCode).Count(&glossaryCount).Error; err != nil {
			t.Fatal(err)
		}
		if formalizationCount != 0 || glossaryCount != 0 {
			t.Fatalf("formalization_count=%d glossary_count=%d, want no side effects", formalizationCount, glossaryCount)
		}
	})

	result, err := repo.FormalizeCandidate(candidate.ID, tenantID, 9, DocumentCandidateFormalizationPlan{
		Action: models.CandidateFormalizationCreatedIdentity, CandidateType: "glossary", CandidateVersion: 1,
		SourceDocumentVersion: 1, ChangeSummary: "由户外标准文档提炼",
	})
	if err != nil {
		t.Fatalf("FormalizeCandidate() error = %v", err)
	}
	if result.Action != models.CandidateFormalizationCreatedIdentity || result.RevisionNo != 1 || result.CandidateVersion != 2 {
		t.Fatalf("result = %+v", result)
	}
	var stored models.DocumentCandidateFormalization
	if err := db.First(&stored, "candidate_id = ?", candidate.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.StandardID == 0 || stored.RevisionID == 0 || stored.TargetRevisionStatus != models.RevisionStatusDraft {
		t.Fatalf("stored formalization = %+v", stored)
	}
	var retained models.DocumentExtractionCandidate
	if err := db.First(&retained, candidate.ID).Error; err != nil {
		t.Fatal(err)
	}
	if retained.Status != "retained" || retained.Version != 2 {
		t.Fatalf("candidate = %+v", retained)
	}
	if err := db.Model(&stored).Update("action", "invalid").Error; err == nil {
		t.Fatal("formalization action CHECK should reject an invalid action")
	}
	if err := db.Delete(&candidate).Error; err == nil {
		t.Fatal("candidate deletion should be restricted while formalization history exists")
	}
}
