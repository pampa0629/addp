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

func TestPostgresDocumentCandidateFamilyDecision(t *testing.T) {
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
	document := models.Document{TenantID: tenantID, ScopeType: models.StandardScopeTenantCommon, Code: fmt.Sprintf("family_decision_%d", tenantID), DocType: "internal", CreatedBy: 1, Version: 1, LifecycleState: "active"}
	if err := db.Create(&document).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = db.Where("id = ? AND tenant_id = ?", document.ID, tenantID).Delete(&models.Document{}).Error
	})
	revision := models.DocumentRevision{DocumentID: document.ID, RevisionNo: 1, Status: models.RevisionStatusDraft, Name: "户外标准", ChangeSummary: "initial", CreatedBy: 1}
	if err := db.Create(&revision).Error; err != nil {
		t.Fatal(err)
	}
	extraction := models.DocumentExtraction{TenantID: tenantID, DocumentRevisionID: revision.ID, Status: "completed", RequestedBy: 1}
	if err := db.Create(&extraction).Error; err != nil {
		t.Fatal(err)
	}
	candidates := []models.DocumentExtractionCandidate{
		{ExtractionID: extraction.ID, CandidateType: "glossary", Code: "outdoor_activity", Name: "户外活动", Definition: "定义一", Status: "pending", Version: 1},
		{ExtractionID: extraction.ID, CandidateType: "glossary", Code: "outdoor_activity", Name: "户外运动", Definition: "定义二", Status: "pending", Version: 1},
	}
	if err := db.Create(&candidates).Error; err != nil {
		t.Fatal(err)
	}
	repo := NewDocumentRepository(db)
	staleMembers := []models.DocumentCandidateFamilyDecisionMember{{CandidateID: candidates[0].ID, Version: 1}, {CandidateID: candidates[1].ID, Version: 2}}
	if _, err := repo.DecideCandidateFamily(document.ID, tenantID, 9, candidates[0].ID, "过期请求不应落库", staleMembers); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("stale decision error = %v, want ErrVersionConflict", err)
	}
	assertCandidateDecisionRows(t, db, candidates, []string{"pending", "pending"}, []int64{1, 1})
	assertCandidateDecisionEventCount(t, db, document.ID, 0)

	members := []models.DocumentCandidateFamilyDecisionMember{{CandidateID: candidates[1].ID, Version: 1}, {CandidateID: candidates[0].ID, Version: 1}}
	result, err := repo.DecideCandidateFamily(document.ID, tenantID, 9, candidates[1].ID, "定义二更符合户外业务口径", members)
	if err != nil {
		t.Fatalf("DecideCandidateFamily() error = %v", err)
	}
	if result.Decision.WinnerCandidateID != candidates[1].ID || result.Decision.Reason != "定义二更符合户外业务口径" || len(result.Decision.Members) != 2 || len(result.Candidates) != 2 || result.Candidates[0].ID != candidates[0].ID || result.Candidates[1].ID != candidates[1].ID {
		t.Fatalf("result = %+v", result)
	}
	assertCandidateDecisionRows(t, db, candidates, []string{"rejected", "retained"}, []int64{2, 2})
	assertCandidateDecisionEventCount(t, db, document.ID, 1)

	secondMembers := []models.DocumentCandidateFamilyDecisionMember{{CandidateID: candidates[0].ID, Version: 2}, {CandidateID: candidates[1].ID, Version: 2}}
	second, err := repo.DecideCandidateFamily(document.ID, tenantID, 10, candidates[0].ID, "补充证据后重新选择定义一", secondMembers)
	if err != nil {
		t.Fatalf("second DecideCandidateFamily() error = %v", err)
	}
	if second.Decision.ID == result.Decision.ID || second.Decision.WinnerCandidateID != candidates[0].ID {
		t.Fatalf("second result = %+v", second)
	}
	assertCandidateDecisionRows(t, db, candidates, []string{"retained", "rejected"}, []int64{3, 3})
	decisions, total, err := repo.ListCandidateFamilyDecisions(document.ID, tenantID, "glossary", "outdoor_activity", 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 || len(decisions) != 1 || decisions[0].ID != second.Decision.ID || decisions[0].CreatedBy != 10 {
		t.Fatalf("paginated decisions = %+v, total = %d", decisions, total)
	}
	firstPage, _, err := repo.ListCandidateFamilyDecisions(document.ID, tenantID, "glossary", "outdoor_activity", 2, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(firstPage) != 1 || firstPage[0].ID != result.Decision.ID || firstPage[0].Reason != "定义二更符合户外业务口径" || firstPage[0].WinnerCandidateID != candidates[1].ID {
		t.Fatalf("first immutable decision = %+v", firstPage)
	}
	invalid := models.DocumentCandidateFamilyDecision{
		DocumentID: document.ID, CandidateType: "glossary", Code: "outdoor_activity", WinnerCandidateID: candidates[0].ID,
		Reason: "   ", Members: second.Decision.Members, CreatedBy: 10,
	}
	if err := db.Create(&invalid).Error; err == nil {
		t.Fatal("blank decision reason should violate PostgreSQL constraint")
	}
	invalid = models.DocumentCandidateFamilyDecision{
		DocumentID: document.ID, CandidateType: "unknown", Code: "outdoor_activity", WinnerCandidateID: candidates[0].ID,
		Reason: "有效理由", Members: second.Decision.Members, CreatedBy: 10,
	}
	if err := db.Create(&invalid).Error; err == nil {
		t.Fatal("unknown candidate type should violate PostgreSQL constraint")
	}
	invalid = models.DocumentCandidateFamilyDecision{
		DocumentID: document.ID, CandidateType: "glossary", Code: "outdoor_activity", WinnerCandidateID: candidates[0].ID,
		Reason: "有效理由", Members: second.Decision.Members[:1], CreatedBy: 10,
	}
	if err := db.Create(&invalid).Error; err == nil {
		t.Fatal("single-member snapshot should violate PostgreSQL constraint")
	}
}

func assertCandidateDecisionEventCount(t *testing.T, db *gorm.DB, documentID, want int64) {
	t.Helper()
	var count int64
	if err := db.Model(&models.DocumentCandidateFamilyDecision{}).Where("document_id = ?", documentID).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != want {
		t.Fatalf("decision event count = %d, want %d", count, want)
	}
}

func assertCandidateDecisionRows(t *testing.T, db *gorm.DB, candidates []models.DocumentExtractionCandidate, wantStatuses []string, wantVersions []int64) {
	t.Helper()
	var stored []models.DocumentExtractionCandidate
	ids := []int64{candidates[0].ID, candidates[1].ID}
	if err := db.Order("id ASC").Find(&stored, "id IN ?", ids).Error; err != nil {
		t.Fatal(err)
	}
	if len(stored) != 2 {
		t.Fatalf("stored candidates = %+v", stored)
	}
	for index, value := range stored {
		if value.Status != wantStatuses[index] || value.Version != wantVersions[index] {
			t.Fatalf("candidate[%d] = status %q version %d, want status %q version %d", index, value.Status, value.Version, wantStatuses[index], wantVersions[index])
		}
	}
}
