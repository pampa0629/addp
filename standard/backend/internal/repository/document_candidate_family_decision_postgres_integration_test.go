package repository

import (
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	candidateutil "github.com/addp/standard/internal/candidate"
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
	t.Run("rejects historical occurrence instead of current representative", func(t *testing.T) {
		historicalDocument := models.Document{TenantID: tenantID, ScopeType: models.StandardScopeTenantCommon, Code: fmt.Sprintf("family_representative_%d", tenantID), DocType: "internal", CreatedBy: 1, Version: 1, LifecycleState: "active"}
		if err := db.Create(&historicalDocument).Error; err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			_ = db.Where("id = ? AND tenant_id = ?", historicalDocument.ID, tenantID).Delete(&models.Document{}).Error
		})
		historicalRevision := models.DocumentRevision{DocumentID: historicalDocument.ID, RevisionNo: 1, Status: models.RevisionStatusDraft, Name: "历史代表校验", ChangeSummary: "initial", CreatedBy: 1}
		if err := db.Create(&historicalRevision).Error; err != nil {
			t.Fatal(err)
		}
		historicalExtraction := models.DocumentExtraction{TenantID: tenantID, DocumentRevisionID: historicalRevision.ID, Status: "completed", RequestedBy: 1}
		if err := db.Create(&historicalExtraction).Error; err != nil {
			t.Fatal(err)
		}
		historicalCandidates := []models.DocumentExtractionCandidate{
			{ExtractionID: historicalExtraction.ID, CandidateType: "glossary", Code: "outdoor_activity", Name: "户外 活动", Definition: "定义一", Status: "pending", Version: 1},
			{ExtractionID: historicalExtraction.ID, CandidateType: "glossary", Code: "outdoor_activity", Name: "户外运动", Definition: "定义二", Status: "pending", Version: 1},
			{ExtractionID: historicalExtraction.ID, CandidateType: "glossary", Code: "outdoor_activity", Name: "户外\n活动", Definition: "定义一", Status: "pending", Version: 1},
		}
		if err := db.Create(&historicalCandidates).Error; err != nil {
			t.Fatal(err)
		}
		snapshotToken := candidateutil.FamilySnapshotToken("glossary", "outdoor_activity", []models.DocumentExtractionCandidate{historicalCandidates[2], historicalCandidates[1]})
		if _, err := repo.DecideCandidateFamily(historicalDocument.ID, tenantID, 9, historicalCandidates[0].ID, snapshotToken, "历史出现记录不能代替当前代表候选"); !errors.Is(err, ErrCandidateFamilyDecisionInvalid) {
			t.Fatalf("historical representative error = %v, want ErrCandidateFamilyDecisionInvalid", err)
		}
		var stored []models.DocumentExtractionCandidate
		ids := []int64{historicalCandidates[0].ID, historicalCandidates[1].ID, historicalCandidates[2].ID}
		if err := db.Order("id ASC").Find(&stored, "id IN ?", ids).Error; err != nil {
			t.Fatal(err)
		}
		if len(stored) != 3 {
			t.Fatalf("stored historical candidates = %+v", stored)
		}
		for _, value := range stored {
			if value.Status != models.CandidateGroupStatePending || value.Version != 1 || value.ReviewedAt != nil {
				t.Fatalf("historical candidate changed after rejected decision: %+v", value)
			}
		}
		assertCandidateDecisionEventCount(t, db, historicalDocument.ID, 0)
	})
	t.Run("single decision rejects historical occurrence", func(t *testing.T) {
		singleDocument := models.Document{TenantID: tenantID, ScopeType: models.StandardScopeTenantCommon, Code: fmt.Sprintf("single_representative_%d", tenantID), DocType: "internal", CreatedBy: 1, Version: 1, LifecycleState: "active"}
		if err := db.Create(&singleDocument).Error; err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			_ = db.Where("id = ? AND tenant_id = ?", singleDocument.ID, tenantID).Delete(&models.Document{}).Error
		})
		singleRevision := models.DocumentRevision{DocumentID: singleDocument.ID, RevisionNo: 1, Status: models.RevisionStatusDraft, Name: "单变体历史代表校验", ChangeSummary: "initial", CreatedBy: 1}
		if err := db.Create(&singleRevision).Error; err != nil {
			t.Fatal(err)
		}
		singleExtraction := models.DocumentExtraction{TenantID: tenantID, DocumentRevisionID: singleRevision.ID, Status: "completed", RequestedBy: 1}
		if err := db.Create(&singleExtraction).Error; err != nil {
			t.Fatal(err)
		}
		singleCandidates := []models.DocumentExtractionCandidate{
			{ExtractionID: singleExtraction.ID, CandidateType: "glossary", Code: "outdoor_route", Name: "户外 路线", Definition: "同一定义", Status: "pending", Version: 1},
			{ExtractionID: singleExtraction.ID, CandidateType: "glossary", Code: "outdoor_route", Name: "户外\n路线", Definition: "同一定义", Status: "pending", Version: 1},
		}
		if err := db.Create(&singleCandidates).Error; err != nil {
			t.Fatal(err)
		}
		if _, err := repo.UpdateCandidateStatus(singleCandidates[0].ID, tenantID, 9, 1, models.CandidateGroupStateRetained); !errors.Is(err, ErrCandidateRepresentativeStale) {
			t.Fatalf("historical single decision error = %v, want ErrCandidateRepresentativeStale", err)
		}
		assertCandidateDecisionRows(t, db, singleCandidates, []string{"pending", "pending"}, []int64{1, 1})
		result, err := repo.UpdateCandidateStatus(singleCandidates[1].ID, tenantID, 9, 1, models.CandidateGroupStateRetained)
		if err != nil {
			t.Fatalf("current representative decision error = %v", err)
		}
		if result.ID != singleCandidates[1].ID || result.Status != models.CandidateGroupStateRetained || result.Version != 2 {
			t.Fatalf("current representative result = %+v", result)
		}
	})

	if _, err := repo.UpdateCandidateStatus(candidates[0].ID, tenantID, 9, 1, "retained"); !errors.Is(err, ErrCandidateFamilyDecisionRequired) {
		t.Fatalf("single decision error = %v, want ErrCandidateFamilyDecisionRequired", err)
	}
	assertCandidateDecisionRows(t, db, candidates, []string{"pending", "pending"}, []int64{1, 1})
	assertCandidateDecisionEventCount(t, db, document.ID, 0)

	staleCandidates := append([]models.DocumentExtractionCandidate(nil), candidates...)
	staleCandidates[1].Version = 2
	staleToken := candidateutil.FamilySnapshotToken("glossary", "outdoor_activity", staleCandidates)
	if _, err := repo.DecideCandidateFamily(document.ID, tenantID, 9, candidates[0].ID, staleToken, "过期请求不应落库"); !errors.Is(err, ErrCandidateFamilySnapshotStale) {
		t.Fatalf("stale decision error = %v, want ErrCandidateFamilySnapshotStale", err)
	}
	assertCandidateDecisionRows(t, db, candidates, []string{"pending", "pending"}, []int64{1, 1})
	assertCandidateDecisionEventCount(t, db, document.ID, 0)

	snapshotToken := candidateutil.FamilySnapshotToken("glossary", "outdoor_activity", candidates)
	result, err := repo.DecideCandidateFamily(document.ID, tenantID, 9, candidates[1].ID, snapshotToken, "定义二更符合户外业务口径")
	if err != nil {
		t.Fatalf("DecideCandidateFamily() error = %v", err)
	}
	if result.Decision.WinnerCandidateID != candidates[1].ID || result.Decision.Reason != "定义二更符合户外业务口径" || len(result.Decision.Members) != 2 || len(result.Candidates) != 2 || result.Candidates[0].ID != candidates[0].ID || result.Candidates[1].ID != candidates[1].ID {
		t.Fatalf("result = %+v", result)
	}
	assertCandidateDecisionRows(t, db, candidates, []string{"rejected", "retained"}, []int64{2, 2})
	assertCandidateDecisionEventCount(t, db, document.ID, 1)

	secondToken := candidateutil.FamilySnapshotToken("glossary", "outdoor_activity", result.Candidates)
	second, err := repo.DecideCandidateFamily(document.ID, tenantID, 10, candidates[0].ID, secondToken, "补充证据后重新选择定义一")
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
	t.Run("supports more than one hundred semantic variants", func(t *testing.T) {
		largeDocument := models.Document{TenantID: tenantID, ScopeType: models.StandardScopeTenantCommon, Code: fmt.Sprintf("large_family_%d", tenantID), DocType: "internal", CreatedBy: 1, Version: 1, LifecycleState: "active"}
		if err := db.Create(&largeDocument).Error; err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			_ = db.Where("id = ? AND tenant_id = ?", largeDocument.ID, tenantID).Delete(&models.Document{}).Error
		})
		largeRevision := models.DocumentRevision{DocumentID: largeDocument.ID, RevisionNo: 1, Status: models.RevisionStatusDraft, Name: "大候选族裁决", ChangeSummary: "initial", CreatedBy: 1}
		if err := db.Create(&largeRevision).Error; err != nil {
			t.Fatal(err)
		}
		largeExtraction := models.DocumentExtraction{TenantID: tenantID, DocumentRevisionID: largeRevision.ID, Status: "completed", RequestedBy: 1}
		if err := db.Create(&largeExtraction).Error; err != nil {
			t.Fatal(err)
		}
		largeCandidates := make([]models.DocumentExtractionCandidate, 101)
		for index := range largeCandidates {
			largeCandidates[index] = models.DocumentExtractionCandidate{
				ExtractionID: largeExtraction.ID, CandidateType: "glossary", Code: "outdoor_large_family",
				Name: fmt.Sprintf("户外活动变体%d", index+1), Definition: fmt.Sprintf("独立业务定义%d", index+1),
				Status: models.CandidateGroupStatePending, Version: 1,
			}
		}
		if err := db.Create(&largeCandidates).Error; err != nil {
			t.Fatal(err)
		}
		largeToken := candidateutil.FamilySnapshotToken("glossary", "outdoor_large_family", largeCandidates)
		largeResult, err := repo.DecideCandidateFamily(largeDocument.ID, tenantID, 9, largeCandidates[0].ID, largeToken, "完整治理超过一百个语义变体")
		if err != nil {
			t.Fatalf("large DecideCandidateFamily() error = %v", err)
		}
		if len(largeResult.Candidates) != 101 || len(largeResult.Decision.Members) != 101 || largeResult.Decision.WinnerCandidateID != largeCandidates[0].ID {
			t.Fatalf("large decision sizes = candidates %d, members %d, winner %d", len(largeResult.Candidates), len(largeResult.Decision.Members), largeResult.Decision.WinnerCandidateID)
		}
		retained, rejected := 0, 0
		for _, value := range largeResult.Candidates {
			if value.Version != 2 {
				t.Fatalf("large candidate %d version = %d, want 2", value.ID, value.Version)
			}
			switch value.Status {
			case models.CandidateGroupStateRetained:
				retained++
			case models.CandidateGroupStateRejected:
				rejected++
			default:
				t.Fatalf("large candidate %d status = %q", value.ID, value.Status)
			}
		}
		if retained != 1 || rejected != 100 {
			t.Fatalf("large decision statuses = retained %d, rejected %d", retained, rejected)
		}
		assertCandidateDecisionEventCount(t, db, largeDocument.ID, 1)
	})
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
