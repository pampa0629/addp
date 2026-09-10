package service

import (
	"errors"
	"strings"
	"testing"

	"github.com/addp/standard/internal/models"
	"github.com/addp/standard/internal/repository"
	"gorm.io/gorm"
)

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
		Reason:            "  定义更准确并覆盖户外业务边界  ",
		Members: []models.DocumentCandidateFamilyDecisionMember{
			{CandidateID: candidates[2].ID, Version: 1},
			{CandidateID: candidates[0].ID, Version: 1},
			{CandidateID: candidates[1].ID, Version: 1},
		},
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

func TestDecideCandidateFamilyRejectsInvalidMembershipWithoutSideEffects(t *testing.T) {
	tests := []struct {
		name       string
		candidates []models.DocumentExtractionCandidate
	}{
		{
			name: "duplicate semantic variant",
			candidates: []models.DocumentExtractionCandidate{
				{CandidateType: "glossary", Code: "outdoor_activity", Name: "户外 活动", Definition: "在户外开展的活动", Status: "pending", Version: 1},
				{CandidateType: "glossary", Code: "outdoor_activity", Name: "户外\n活动", Definition: "在户外开展的活动", Status: "pending", Version: 1},
			},
		},
		{
			name: "cross family member",
			candidates: []models.DocumentExtractionCandidate{
				{CandidateType: "glossary", Code: "outdoor_activity", Name: "户外活动", Definition: "在户外开展的活动", Status: "pending", Version: 1},
				{CandidateType: "glossary", Code: "outdoor_member", Name: "户外成员", Definition: "参加户外活动的人", Status: "pending", Version: 1},
			},
		},
		{
			name: "omitted current variant",
			candidates: []models.DocumentExtractionCandidate{
				{CandidateType: "glossary", Code: "outdoor_activity", Name: "户外活动", Definition: "定义一", Status: "pending", Version: 1},
				{CandidateType: "glossary", Code: "outdoor_activity", Name: "户外运动", Definition: "定义二", Status: "pending", Version: 1},
				{CandidateType: "glossary", Code: "outdoor_activity", Name: "户外活动概念", Definition: "定义三", Status: "pending", Version: 1},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db := openDocumentServiceTestDB(t)
			repo := repository.NewDocumentRepository(db)
			document, revision := seedDocumentDraft(t, repo, 7, "outdoor.md")
			extraction := models.DocumentExtraction{TenantID: document.TenantID, DocumentRevisionID: revision.ID, Status: "completed", RequestedBy: 3}
			if err := db.Create(&extraction).Error; err != nil {
				t.Fatal(err)
			}
			for index := range test.candidates {
				test.candidates[index].ExtractionID = extraction.ID
			}
			if err := db.Create(&test.candidates).Error; err != nil {
				t.Fatal(err)
			}
			request := &models.DecideDocumentCandidateFamilyRequest{
				WinnerCandidateID: test.candidates[0].ID,
				Reason:            "选择定义更完整的变体",
				Members: []models.DocumentCandidateFamilyDecisionMember{
					{CandidateID: test.candidates[0].ID, Version: 1},
					{CandidateID: test.candidates[1].ID, Version: 1},
				},
			}
			if _, err := (&DocumentService{repo: repo}).DecideCandidateFamily(document.ID, document.TenantID, 11, request); !errors.Is(err, ErrCandidateFamilyDecisionInvalid) {
				t.Fatalf("error = %v, want ErrCandidateFamilyDecisionInvalid", err)
			}
			var stored []models.DocumentExtractionCandidate
			ids := make([]int64, len(test.candidates))
			for index := range test.candidates {
				ids[index] = test.candidates[index].ID
			}
			if err := db.Order("id ASC").Find(&stored, "id IN ?", ids).Error; err != nil {
				t.Fatal(err)
			}
			for _, candidate := range stored {
				if candidate.Status != models.CandidateGroupStatePending || candidate.Version != 1 || candidate.ReviewedAt != nil {
					t.Fatalf("candidate changed after rejected decision: %+v", candidate)
				}
			}
			assertNoCandidateFamilyDecision(t, db)
		})
	}
}

func TestDecideCandidateFamilyRejectsStaleMemberAndRollsBackAllMembers(t *testing.T) {
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
	request := &models.DecideDocumentCandidateFamilyRequest{
		WinnerCandidateID: candidates[0].ID,
		Reason:            "选择定义更完整的变体",
		Members: []models.DocumentCandidateFamilyDecisionMember{
			{CandidateID: candidates[0].ID, Version: 1},
			{CandidateID: candidates[1].ID, Version: 1},
		},
	}
	if _, err := (&DocumentService{repo: repo}).DecideCandidateFamily(document.ID, document.TenantID, 11, request); !errors.Is(err, repository.ErrVersionConflict) {
		t.Fatalf("error = %v, want version conflict", err)
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
	requests := []*models.DecideDocumentCandidateFamilyRequest{
		nil,
		{WinnerCandidateID: 1, Reason: "理由", Members: []models.DocumentCandidateFamilyDecisionMember{{CandidateID: 1, Version: 1}}},
		{WinnerCandidateID: 2, Reason: "理由", Members: []models.DocumentCandidateFamilyDecisionMember{{CandidateID: 1, Version: 1}, {CandidateID: 1, Version: 1}}},
		{WinnerCandidateID: 3, Reason: "理由", Members: []models.DocumentCandidateFamilyDecisionMember{{CandidateID: 1, Version: 1}, {CandidateID: 2, Version: 1}}},
		{WinnerCandidateID: 1, Reason: "   ", Members: []models.DocumentCandidateFamilyDecisionMember{{CandidateID: 1, Version: 1}, {CandidateID: 2, Version: 1}}},
		{WinnerCandidateID: 1, Reason: strings.Repeat("理", 1001), Members: []models.DocumentCandidateFamilyDecisionMember{{CandidateID: 1, Version: 1}, {CandidateID: 2, Version: 1}}},
	}
	for index, request := range requests {
		if _, err := svc.DecideCandidateFamily(1, 7, 11, request); !errors.Is(err, ErrCandidateFamilyDecisionInvalid) {
			t.Fatalf("request[%d] error = %v, want ErrCandidateFamilyDecisionInvalid", index, err)
		}
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
		Reason:            "选择未正式化的变体",
		Members: []models.DocumentCandidateFamilyDecisionMember{
			{CandidateID: candidates[0].ID, Version: 2},
			{CandidateID: candidates[1].ID, Version: 1},
		},
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
		Reason:            "选择新语义变体",
		Members: []models.DocumentCandidateFamilyDecisionMember{
			{CandidateID: candidates[1].ID, Version: 1},
			{CandidateID: candidates[2].ID, Version: 1},
		},
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
