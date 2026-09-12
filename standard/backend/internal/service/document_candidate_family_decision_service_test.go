package service

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	candidateutil "github.com/addp/standard/internal/candidate"
	"github.com/addp/standard/internal/models"
	"github.com/addp/standard/internal/repository"
	"gorm.io/gorm"
)

func TestUpdateCandidateStatusRequiresFamilyDecisionForMultipleSemanticVariants(t *testing.T) {
	db := openDocumentServiceTestDB(t)
	repo := repository.NewDocumentRepository(db)
	document, revision := seedDocumentDraft(t, repo, 7, "outdoor.md")
	extraction := models.DocumentExtraction{TenantID: document.TenantID, DocumentRevisionID: revision.ID, Status: "completed", RequestedBy: 3}
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

	_, err := (&DocumentService{repo: repo}).UpdateCandidateStatus(candidates[0].ID, document.TenantID, 11, &models.UpdateDocumentExtractionCandidateRequest{Version: 1, Status: "retained"})
	if !errors.Is(err, ErrCandidateFamilyDecisionRequired) {
		t.Fatalf("error = %v, want ErrCandidateFamilyDecisionRequired", err)
	}
	var stored []models.DocumentExtractionCandidate
	if err := db.Order("id ASC").Find(&stored, "id IN ?", []int64{candidates[0].ID, candidates[1].ID}).Error; err != nil {
		t.Fatal(err)
	}
	for _, candidate := range stored {
		if candidate.Status != models.CandidateGroupStatePending || candidate.Version != 1 || candidate.ReviewedAt != nil {
			t.Fatalf("candidate changed after blocked single decision: %+v", candidate)
		}
	}
	assertNoCandidateFamilyDecision(t, db)
}

func TestUpdateCandidateStatusAllowsOneSemanticVariant(t *testing.T) {
	db := openDocumentServiceTestDB(t)
	repo := repository.NewDocumentRepository(db)
	document, revision := seedDocumentDraft(t, repo, 7, "outdoor.md")
	extraction := models.DocumentExtraction{TenantID: document.TenantID, DocumentRevisionID: revision.ID, Status: "completed", RequestedBy: 3}
	if err := db.Create(&extraction).Error; err != nil {
		t.Fatal(err)
	}
	candidates := []models.DocumentExtractionCandidate{
		{ExtractionID: extraction.ID, CandidateType: "glossary", Code: "outdoor_activity", Name: "户外 活动", Definition: "同一定义", Status: "pending", Version: 1},
		{ExtractionID: extraction.ID, CandidateType: "glossary", Code: "outdoor_activity", Name: "户外\n活动", Definition: "同一定义", Status: "pending", Version: 1},
	}
	if err := db.Create(&candidates).Error; err != nil {
		t.Fatal(err)
	}

	result, err := (&DocumentService{repo: repo}).UpdateCandidateStatus(candidates[1].ID, document.TenantID, 11, &models.UpdateDocumentExtractionCandidateRequest{Version: 1, Status: "rejected"})
	if err != nil {
		t.Fatalf("UpdateCandidateStatus() error = %v", err)
	}
	if result.Status != models.CandidateGroupStateRejected || result.Version != 2 || result.ReviewedBy == nil || *result.ReviewedBy != 11 {
		t.Fatalf("result = %+v", result)
	}
}

func TestUpdateCandidateStatusRejectsHistoricalOccurrenceInsteadOfCurrentRepresentative(t *testing.T) {
	db := openDocumentServiceTestDB(t)
	repo := repository.NewDocumentRepository(db)
	document, revision := seedDocumentDraft(t, repo, 7, "outdoor.md")
	extraction := models.DocumentExtraction{TenantID: document.TenantID, DocumentRevisionID: revision.ID, Status: "completed", RequestedBy: 3}
	if err := db.Create(&extraction).Error; err != nil {
		t.Fatal(err)
	}
	candidates := []models.DocumentExtractionCandidate{
		{ExtractionID: extraction.ID, CandidateType: "glossary", Code: "outdoor_activity", Name: "户外 活动", Definition: "同一定义", Status: "pending", Version: 1},
		{ExtractionID: extraction.ID, CandidateType: "glossary", Code: "outdoor_activity", Name: "户外\n活动", Definition: "同一定义", Status: "pending", Version: 1},
	}
	if err := db.Create(&candidates).Error; err != nil {
		t.Fatal(err)
	}

	_, err := (&DocumentService{repo: repo}).UpdateCandidateStatus(candidates[0].ID, document.TenantID, 11, &models.UpdateDocumentExtractionCandidateRequest{Version: 1, Status: "retained"})
	if !errors.Is(err, ErrCandidateRepresentativeStale) {
		t.Fatalf("error = %v, want ErrCandidateRepresentativeStale", err)
	}
	var stored []models.DocumentExtractionCandidate
	if err := db.Order("id ASC").Find(&stored, "id IN ?", []int64{candidates[0].ID, candidates[1].ID}).Error; err != nil {
		t.Fatal(err)
	}
	for _, candidate := range stored {
		if candidate.Status != models.CandidateGroupStatePending || candidate.Version != 1 || candidate.ReviewedAt != nil {
			t.Fatalf("candidate changed after historical representative rejection: %+v", candidate)
		}
	}
}

func TestDecideCandidateFamilyRetainsOneVariantAndRejectsTheOthersAtomically(t *testing.T) {
	db := openDocumentServiceTestDB(t)
	repo := repository.NewDocumentRepository(db)
	document, revision := seedDocumentDraft(t, repo, 7, "outdoor.md")
	extraction := models.DocumentExtraction{TenantID: document.TenantID, DocumentRevisionID: revision.ID, Status: "completed", RequestedBy: 3}
	if err := db.Create(&extraction).Error; err != nil {
		t.Fatal(err)
	}
	candidates := []models.DocumentExtractionCandidate{
		{ExtractionID: extraction.ID, CandidateType: "glossary", Code: "outdoor_activity", Name: "户外活动", Definition: "在户外开展的活动", Status: "pending", Version: 1},
		{ExtractionID: extraction.ID, CandidateType: "glossary", Code: "outdoor_activity", Name: "户外运动", Definition: "在自然环境开展的活动", Status: "pending", Version: 1},
		{ExtractionID: extraction.ID, CandidateType: "glossary", Code: "outdoor_activity", Name: "户外活动概念", Definition: "用于户外业务治理的活动概念", Status: "pending", Version: 1},
	}
	if err := db.Create(&candidates).Error; err != nil {
		t.Fatal(err)
	}

	result, err := (&DocumentService{repo: repo}).DecideCandidateFamily(document.ID, document.TenantID, 11, &models.DecideDocumentCandidateFamilyRequest{
		WinnerCandidateID: candidates[1].ID,
		SnapshotToken:     candidateFamilySnapshotToken(candidates),
		Reason:            "  定义更准确并覆盖户外业务边界  ",
	})
	if err != nil {
		t.Fatalf("DecideCandidateFamily() error = %v", err)
	}
	if result.Decision.DocumentID != document.ID || result.Decision.CandidateType != "glossary" || result.Decision.Code != "outdoor_activity" || result.Decision.WinnerCandidateID != candidates[1].ID || result.Decision.Reason != "定义更准确并覆盖户外业务边界" || len(result.Decision.Members) != 3 || len(result.Candidates) != 3 {
		t.Fatalf("result = %+v", result)
	}
	for _, candidate := range result.Candidates {
		wantStatus := models.CandidateGroupStateRejected
		if candidate.ID == candidates[1].ID {
			wantStatus = models.CandidateGroupStateRetained
		}
		if candidate.Status != wantStatus || candidate.Version != 2 || candidate.ReviewedBy == nil || *candidate.ReviewedBy != 11 || candidate.ReviewedAt == nil {
			t.Fatalf("candidate = %+v, want status=%s version=2 reviewer=11", candidate, wantStatus)
		}
	}
	var decisions []models.DocumentCandidateFamilyDecision
	if err := db.Order("id ASC").Find(&decisions).Error; err != nil {
		t.Fatal(err)
	}
	if len(decisions) != 1 || decisions[0].ID != result.Decision.ID || len(decisions[0].Members) != 3 {
		t.Fatalf("decisions = %+v", decisions)
	}
	for _, member := range decisions[0].Members {
		if member.Version != 2 || (member.Status != models.CandidateGroupStateRetained && member.Status != models.CandidateGroupStateRejected) || member.SemanticFingerprint == "" {
			t.Fatalf("decision member = %+v", member)
		}
	}
}

func TestDecideCandidateFamilyRejectsSingleSemanticVariantWithoutSideEffects(t *testing.T) {
	db := openDocumentServiceTestDB(t)
	repo := repository.NewDocumentRepository(db)
	document, revision := seedDocumentDraft(t, repo, 7, "outdoor.md")
	extraction := models.DocumentExtraction{TenantID: document.TenantID, DocumentRevisionID: revision.ID, Status: "completed", RequestedBy: 3}
	if err := db.Create(&extraction).Error; err != nil {
		t.Fatal(err)
	}
	candidates := []models.DocumentExtractionCandidate{
		{ExtractionID: extraction.ID, CandidateType: "glossary", Code: "outdoor_activity", Name: "户外 活动", Definition: "在户外开展的活动", Status: "pending", Version: 1},
		{ExtractionID: extraction.ID, CandidateType: "glossary", Code: "outdoor_activity", Name: "户外\n活动", Definition: "在户外开展的活动", Status: "pending", Version: 1},
	}
	if err := db.Create(&candidates).Error; err != nil {
		t.Fatal(err)
	}
	request := &models.DecideDocumentCandidateFamilyRequest{
		WinnerCandidateID: candidates[1].ID,
		SnapshotToken:     candidateFamilySnapshotToken(candidates[1:]),
		Reason:            "单语义变体必须使用单项处置",
	}
	if _, err := (&DocumentService{repo: repo}).DecideCandidateFamily(document.ID, document.TenantID, 11, request); !errors.Is(err, ErrCandidateFamilyDecisionInvalid) {
		t.Fatalf("error = %v, want ErrCandidateFamilyDecisionInvalid", err)
	}
	assertNoCandidateFamilyDecision(t, db)
}

func TestDecideCandidateFamilyRejectsHistoricalOccurrenceInsteadOfCurrentRepresentative(t *testing.T) {
	db := openDocumentServiceTestDB(t)
	repo := repository.NewDocumentRepository(db)
	document, revision := seedDocumentDraft(t, repo, 7, "outdoor.md")
	extraction := models.DocumentExtraction{TenantID: document.TenantID, DocumentRevisionID: revision.ID, Status: "completed", RequestedBy: 3}
	if err := db.Create(&extraction).Error; err != nil {
		t.Fatal(err)
	}
	candidates := []models.DocumentExtractionCandidate{
		{ExtractionID: extraction.ID, CandidateType: "glossary", Code: "outdoor_activity", Name: "户外 活动", Definition: "定义一", Status: "pending", Version: 1},
		{ExtractionID: extraction.ID, CandidateType: "glossary", Code: "outdoor_activity", Name: "户外运动", Definition: "定义二", Status: "pending", Version: 1},
		{ExtractionID: extraction.ID, CandidateType: "glossary", Code: "outdoor_activity", Name: "户外\n活动", Definition: "定义一", Status: "pending", Version: 1},
	}
	if err := db.Create(&candidates).Error; err != nil {
		t.Fatal(err)
	}
	request := &models.DecideDocumentCandidateFamilyRequest{
		WinnerCandidateID: candidates[0].ID,
		SnapshotToken:     candidateFamilySnapshotToken([]models.DocumentExtractionCandidate{candidates[2], candidates[1]}),
		Reason:            "旧出现记录不能代替当前代表候选",
	}

	if _, err := (&DocumentService{repo: repo}).DecideCandidateFamily(document.ID, document.TenantID, 11, request); !errors.Is(err, ErrCandidateFamilyDecisionInvalid) {
		t.Fatalf("error = %v, want ErrCandidateFamilyDecisionInvalid", err)
	}
	var stored []models.DocumentExtractionCandidate
	if err := db.Order("id ASC").Find(&stored, "id IN ?", []int64{candidates[0].ID, candidates[1].ID, candidates[2].ID}).Error; err != nil {
		t.Fatal(err)
	}
	for _, candidate := range stored {
		if candidate.Status != models.CandidateGroupStatePending || candidate.Version != 1 || candidate.ReviewedAt != nil {
			t.Fatalf("candidate changed after historical representative rejection: %+v", candidate)
		}
	}
	assertNoCandidateFamilyDecision(t, db)
}

func TestDecideCandidateFamilyRejectsStaleSnapshotAndRollsBackAllMembers(t *testing.T) {
	db := openDocumentServiceTestDB(t)
	repo := repository.NewDocumentRepository(db)
	document, revision := seedDocumentDraft(t, repo, 7, "outdoor.md")
	extraction := models.DocumentExtraction{TenantID: document.TenantID, DocumentRevisionID: revision.ID, Status: "completed", RequestedBy: 3}
	if err := db.Create(&extraction).Error; err != nil {
		t.Fatal(err)
	}
	candidates := []models.DocumentExtractionCandidate{
		{ExtractionID: extraction.ID, CandidateType: "glossary", Code: "outdoor_activity", Name: "户外活动", Definition: "定义一", Status: "pending", Version: 1},
		{ExtractionID: extraction.ID, CandidateType: "glossary", Code: "outdoor_activity", Name: "户外运动", Definition: "定义二", Status: "pending", Version: 2},
	}
	if err := db.Create(&candidates).Error; err != nil {
		t.Fatal(err)
	}
	staleCandidates := append([]models.DocumentExtractionCandidate(nil), candidates...)
	staleCandidates[1].Version = 1
	request := &models.DecideDocumentCandidateFamilyRequest{
		WinnerCandidateID: candidates[0].ID,
		SnapshotToken:     candidateFamilySnapshotToken(staleCandidates),
		Reason:            "选择定义更完整的变体",
	}
	if _, err := (&DocumentService{repo: repo}).DecideCandidateFamily(document.ID, document.TenantID, 11, request); !errors.Is(err, ErrCandidateFamilySnapshotStale) {
		t.Fatalf("error = %v, want ErrCandidateFamilySnapshotStale", err)
	}
	var stored []models.DocumentExtractionCandidate
	if err := db.Order("id ASC").Find(&stored, "id IN ?", []int64{candidates[0].ID, candidates[1].ID}).Error; err != nil {
		t.Fatal(err)
	}
	if stored[0].Status != "pending" || stored[0].Version != 1 || stored[1].Status != "pending" || stored[1].Version != 2 {
		t.Fatalf("candidates changed after stale decision: %+v", stored)
	}
	assertNoCandidateFamilyDecision(t, db)
}

func TestDecideCandidateFamilyRejectsInvalidRequestShape(t *testing.T) {
	svc := &DocumentService{}
	validToken := strings.Repeat("a", 64)
	requests := []*models.DecideDocumentCandidateFamilyRequest{
		nil,
		{WinnerCandidateID: 0, SnapshotToken: validToken, Reason: "理由"},
		{WinnerCandidateID: 1, SnapshotToken: "", Reason: "理由"},
		{WinnerCandidateID: 1, SnapshotToken: strings.Repeat("A", 64), Reason: "理由"},
		{WinnerCandidateID: 1, SnapshotToken: strings.Repeat("z", 64), Reason: "理由"},
		{WinnerCandidateID: 1, SnapshotToken: validToken, Reason: "   "},
		{WinnerCandidateID: 1, SnapshotToken: validToken, Reason: strings.Repeat("理", 1001)},
	}
	for index, request := range requests {
		if _, err := svc.DecideCandidateFamily(1, 7, 11, request); !errors.Is(err, ErrCandidateFamilyDecisionInvalid) {
			t.Fatalf("request[%d] error = %v, want ErrCandidateFamilyDecisionInvalid", index, err)
		}
	}
}

func TestDecideCandidateFamilySupportsMoreThanOneHundredVariants(t *testing.T) {
	db := openDocumentServiceTestDB(t)
	repo := repository.NewDocumentRepository(db)
	document, revision := seedDocumentDraft(t, repo, 7, "outdoor.md")
	extraction := models.DocumentExtraction{TenantID: document.TenantID, DocumentRevisionID: revision.ID, Status: "completed", RequestedBy: 3}
	if err := db.Create(&extraction).Error; err != nil {
		t.Fatal(err)
	}
	candidates := make([]models.DocumentExtractionCandidate, 101)
	for index := range candidates {
		candidates[index] = models.DocumentExtractionCandidate{
			ExtractionID: extraction.ID, CandidateType: "glossary", Code: "outdoor_activity",
			Name: "户外活动", Definition: fmt.Sprintf("语义定义 %03d", index), Status: "pending", Version: 1,
		}
	}
	if err := db.Create(&candidates).Error; err != nil {
		t.Fatal(err)
	}

	result, err := (&DocumentService{repo: repo}).DecideCandidateFamily(document.ID, document.TenantID, 11, &models.DecideDocumentCandidateFamilyRequest{
		WinnerCandidateID: candidates[100].ID,
		SnapshotToken:     candidateFamilySnapshotToken(candidates),
		Reason:            "候选族不能因为超过一百个语义变体而失去治理路径",
	})
	if err != nil {
		t.Fatalf("DecideCandidateFamily() error = %v", err)
	}
	if len(result.Candidates) != 101 || len(result.Decision.Members) != 101 {
		t.Fatalf("candidates=%d decision_members=%d, want 101", len(result.Candidates), len(result.Decision.Members))
	}
}

func TestListCandidateFamilyDecisionsRejectsInvalidQuery(t *testing.T) {
	svc := &DocumentService{}
	tests := []struct {
		candidateType string
		code          string
		page          int
		pageSize      int
	}{
		{"", "outdoor_activity", 1, 20},
		{"unknown", "outdoor_activity", 1, 20},
		{"glossary", "   ", 1, 20},
		{"glossary", "outdoor_activity", 0, 20},
		{"glossary", "outdoor_activity", 1, 101},
	}
	for index, test := range tests {
		if _, _, err := svc.ListCandidateFamilyDecisions(1, 7, test.candidateType, test.code, test.page, test.pageSize); !errors.Is(err, ErrCandidateFamilyDecisionInvalid) {
			t.Fatalf("test[%d] error = %v, want ErrCandidateFamilyDecisionInvalid", index, err)
		}
	}
}

func TestDecideCandidateFamilyRejectsFormalizedMemberWithoutSideEffects(t *testing.T) {
	db := openDocumentServiceTestDB(t)
	repo := repository.NewDocumentRepository(db)
	document, revision := seedDocumentDraft(t, repo, 7, "outdoor.md")
	extraction := models.DocumentExtraction{TenantID: document.TenantID, DocumentRevisionID: revision.ID, Status: "completed", RequestedBy: 3}
	if err := db.Create(&extraction).Error; err != nil {
		t.Fatal(err)
	}
	candidates := []models.DocumentExtractionCandidate{
		{ExtractionID: extraction.ID, CandidateType: "glossary", Code: "outdoor_activity", Name: "户外活动", Definition: "定义一", Status: "retained", Version: 2},
		{ExtractionID: extraction.ID, CandidateType: "glossary", Code: "outdoor_activity", Name: "户外运动", Definition: "定义二", Status: "pending", Version: 1},
	}
	if err := db.Create(&candidates).Error; err != nil {
		t.Fatal(err)
	}
	formalization := models.DocumentCandidateFormalization{CandidateID: candidates[0].ID, Action: models.CandidateFormalizationLinkedExisting, StandardID: 9, StandardCode: "outdoor_activity", RevisionID: 10, RevisionNo: 1, TargetRevisionStatus: models.RevisionStatusPublished, ChangeSummary: "linked", CreatedBy: 11}
	if err := db.Create(&formalization).Error; err != nil {
		t.Fatal(err)
	}
	request := &models.DecideDocumentCandidateFamilyRequest{
		WinnerCandidateID: candidates[1].ID,
		SnapshotToken:     candidateFamilySnapshotToken(candidates),
		Reason:            "选择未正式化的变体",
	}
	if _, err := (&DocumentService{repo: repo}).DecideCandidateFamily(document.ID, document.TenantID, 11, request); !errors.Is(err, ErrCandidateAlreadyFormalized) {
		t.Fatalf("error = %v, want ErrCandidateAlreadyFormalized", err)
	}
	var stored []models.DocumentExtractionCandidate
	if err := db.Order("id ASC").Find(&stored, "id IN ?", []int64{candidates[0].ID, candidates[1].ID}).Error; err != nil {
		t.Fatal(err)
	}
	if stored[0].Status != "retained" || stored[0].Version != 2 || stored[1].Status != "pending" || stored[1].Version != 1 {
		t.Fatalf("candidates changed after formalization conflict: %+v", stored)
	}
	assertNoCandidateFamilyDecision(t, db)
}

func TestDecideCandidateFamilyRejectsFormalizedHistoricalOccurrenceWithoutSideEffects(t *testing.T) {
	db := openDocumentServiceTestDB(t)
	repo := repository.NewDocumentRepository(db)
	document, revision := seedDocumentDraft(t, repo, 7, "outdoor.md")
	extraction := models.DocumentExtraction{TenantID: document.TenantID, DocumentRevisionID: revision.ID, Status: "completed", RequestedBy: 3}
	if err := db.Create(&extraction).Error; err != nil {
		t.Fatal(err)
	}
	candidates := []models.DocumentExtractionCandidate{
		{ExtractionID: extraction.ID, CandidateType: "glossary", Code: "outdoor_activity", Name: "户外活动", Definition: "定义一", Status: "retained", Version: 2},
		{ExtractionID: extraction.ID, CandidateType: "glossary", Code: "outdoor_activity", Name: "户外活动", Definition: "定义一", Status: "pending", Version: 1},
		{ExtractionID: extraction.ID, CandidateType: "glossary", Code: "outdoor_activity", Name: "户外运动", Definition: "定义二", Status: "pending", Version: 1},
	}
	if err := db.Create(&candidates).Error; err != nil {
		t.Fatal(err)
	}
	formalization := models.DocumentCandidateFormalization{CandidateID: candidates[0].ID, Action: models.CandidateFormalizationLinkedExisting, StandardID: 9, StandardCode: "outdoor_activity", RevisionID: 10, RevisionNo: 1, TargetRevisionStatus: models.RevisionStatusPublished, ChangeSummary: "linked", CreatedBy: 11}
	if err := db.Create(&formalization).Error; err != nil {
		t.Fatal(err)
	}
	request := &models.DecideDocumentCandidateFamilyRequest{
		WinnerCandidateID: candidates[2].ID,
		SnapshotToken:     candidateFamilySnapshotToken([]models.DocumentExtractionCandidate{candidates[0], candidates[2]}),
		Reason:            "选择新语义变体",
	}
	if _, err := (&DocumentService{repo: repo}).DecideCandidateFamily(document.ID, document.TenantID, 11, request); !errors.Is(err, ErrCandidateAlreadyFormalized) {
		t.Fatalf("error = %v, want ErrCandidateAlreadyFormalized", err)
	}
	var stored []models.DocumentExtractionCandidate
	if err := db.Order("id ASC").Find(&stored, "id IN ?", []int64{candidates[0].ID, candidates[1].ID, candidates[2].ID}).Error; err != nil {
		t.Fatal(err)
	}
	if stored[0].Status != "retained" || stored[0].Version != 2 || stored[1].Status != "pending" || stored[1].Version != 1 || stored[2].Status != "pending" || stored[2].Version != 1 {
		t.Fatalf("candidates changed after historical formalization conflict: %+v", stored)
	}
	assertNoCandidateFamilyDecision(t, db)
}

func assertNoCandidateFamilyDecision(t *testing.T, db *gorm.DB) {
	t.Helper()
	var count int64
	if err := db.Model(&models.DocumentCandidateFamilyDecision{}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("candidate family decision count = %d, want 0", count)
	}
}

func candidateFamilySnapshotToken(candidates []models.DocumentExtractionCandidate) string {
	if len(candidates) == 0 {
		return ""
	}
	return candidateutil.FamilySnapshotToken(candidates[0].CandidateType, candidates[0].Code, candidates)
}
