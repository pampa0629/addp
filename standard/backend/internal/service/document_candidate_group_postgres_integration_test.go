package service

import (
	"fmt"
	"strings"
	"testing"
	"time"

	candidateutil "github.com/addp/standard/internal/candidate"
	"github.com/addp/standard/internal/models"
	"github.com/addp/standard/internal/repository"
)

func TestPostgresDocumentCandidateGroupsPreserveOccurrences(t *testing.T) {
	db := openStandardReferenceDeletionPostgres(t)
	tenantID := time.Now().UnixNano()
	repo := repository.NewDocumentRepository(db)
	document, revision := seedDocumentDraft(t, repo, tenantID, fmt.Sprintf("candidate-groups-%d.md", tenantID))
	t.Cleanup(func() {
		_ = db.Where("id = ? AND tenant_id = ?", document.ID, tenantID).Delete(&models.Document{}).Error
		_ = db.Where("tenant_id = ?", tenantID).Delete(&models.Glossary{}).Error
	})

	first := models.DocumentExtraction{TenantID: tenantID, DocumentRevisionID: revision.ID, Status: "completed", RequestedBy: 1}
	second := models.DocumentExtraction{TenantID: tenantID, DocumentRevisionID: revision.ID, Status: "completed", RequestedBy: 2}
	if err := db.Create(&first).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&second).Error; err != nil {
		t.Fatal(err)
	}
	reviewedAt := time.Now().UTC()
	reviewer := int64(3)
	code := fmt.Sprintf("outdoor_group_%d", tenantID)
	candidates := []models.DocumentExtractionCandidate{
		{ExtractionID: first.ID, CandidateType: "glossary", Code: code, Name: "户外 活动", Definition: "在户外开展的活动", Status: "retained", Version: 2, ReviewedBy: &reviewer, ReviewedAt: &reviewedAt},
		{ExtractionID: second.ID, CandidateType: "glossary", Code: code, Name: "户外\n活动", Definition: "在户外开展的活动", Status: "pending", Version: 1},
		{ExtractionID: second.ID, CandidateType: "glossary", Code: code, Name: "户外活动冲突", Definition: "不同定义", Status: "rejected", Version: 2, ReviewedBy: &reviewer, ReviewedAt: &reviewedAt},
	}
	for index := range candidates {
		if err := db.Create(&candidates[index]).Error; err != nil {
			t.Fatal(err)
		}
	}
	glossary := models.Glossary{TenantID: tenantID, ScopeType: models.StandardScopeTenantCommon, Code: code, CreatedBy: 1, Version: 1, LifecycleState: "active"}
	if err := db.Create(&glossary).Error; err != nil {
		t.Fatal(err)
	}
	glossaryRevision := models.GlossaryRevision{GlossaryID: glossary.ID, RevisionNo: 1, Status: models.RevisionStatusDraft, Name: candidates[0].Name, Definition: candidates[0].Definition, ChangeSummary: "initial", CreatedBy: 1}
	if err := db.Create(&glossaryRevision).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&glossary).Update("draft_revision_id", glossaryRevision.ID).Error; err != nil {
		t.Fatal(err)
	}
	for index := range candidates {
		evidence := models.DocumentExtractionEvidence{CandidateID: candidates[index].ID, DocumentRevisionID: revision.ID, SectionPath: "标准", StartLine: index + 1, EndLine: index + 1, Excerpt: fmt.Sprintf("evidence-%d", index), ExcerptHash: fmt.Sprintf("hash-%d", index)}
		if err := db.Create(&evidence).Error; err != nil {
			t.Fatal(err)
		}
	}
	identities, err := repo.ListCandidateIdentities(document.ID, tenantID)
	if err != nil || len(identities) != len(candidates) {
		t.Fatalf("candidate identities=%+v err=%v", identities, err)
	}
	knownCandidates := buildCopilotKnownCandidates(identities, nil)
	if len(knownCandidates) != 1 || knownCandidates[0].Name != candidates[0].Name || knownCandidates[0].Definition != candidates[0].Definition {
		t.Fatalf("known candidates=%+v", knownCandidates)
	}
	decision := models.DocumentCandidateFamilyDecision{
		DocumentID: document.ID, CandidateType: "glossary", Code: code, WinnerCandidateID: candidates[0].ID, Reason: "定义更符合当前标准",
		Members: []models.DocumentCandidateFamilyDecisionMemberSnapshot{
			{CandidateID: candidates[0].ID, SemanticFingerprint: "exact", Name: candidates[0].Name, Version: 2, Status: models.CandidateGroupStateRetained},
			{CandidateID: candidates[2].ID, SemanticFingerprint: "conflict", Name: candidates[2].Name, Version: 2, Status: models.CandidateGroupStateRejected},
		},
		CreatedBy: reviewer,
	}
	if err := db.Create(&decision).Error; err != nil {
		t.Fatal(err)
	}

	svc := &DocumentService{repo: repo}
	response, err := svc.ListCandidateFamilies(document.ID, tenantID, DocumentCandidateFamilyListOptions{Page: 1, PageSize: 1})
	if err != nil {
		t.Fatal(err)
	}
	if response.Total != 1 || response.VariantTotal != 2 || response.TotalPages != 1 || response.FamilyComparisonCounts.All != 1 || response.FamilyComparisonCounts.Exact != 1 || response.FamilyComparisonCounts.ContentConflict != 1 || len(response.Data) != 1 {
		t.Fatalf("response=%+v", response)
	}
	if response.VariantStatusCounts.Retained != 1 || response.VariantStatusCounts.Rejected != 1 || response.VariantStatusCounts.Pending != 0 {
		t.Fatalf("variant status counts=%+v", response.VariantStatusCounts)
	}
	family := response.Data[0]
	if family.FamilyKey != "glossary:"+code || family.VariantCount != 2 || family.TotalVariantCount != 2 || family.OccurrenceCount != 3 || family.DecisionCount != 1 || len(family.Variants) != 2 {
		t.Fatalf("family=%+v", family)
	}
	expectedSnapshotToken := candidateutil.FamilySnapshotToken("glossary", code, []models.DocumentExtractionCandidate{candidates[0], candidates[2]})
	if family.SnapshotToken != expectedSnapshotToken || len(family.SnapshotToken) != 64 {
		t.Fatalf("family snapshot_token=%q, want %q", family.SnapshotToken, expectedSnapshotToken)
	}
	group := family.Variants[0]
	if group.State != models.CandidateGroupStateRetained || group.OccurrenceCount != 2 || group.Candidate.ID != candidates[0].ID {
		t.Fatalf("group=%+v", group)
	}
	if len(group.Occurrences) != 2 || len(group.Occurrences[0].Evidences) != 1 || group.Candidate.Comparison == nil || group.Candidate.Comparison.Result != models.CandidateComparisonExact {
		t.Fatalf("occurrences/comparison=%+v", group)
	}

	filtered, err := svc.ListCandidateFamilies(document.ID, tenantID, DocumentCandidateFamilyListOptions{State: models.CandidateGroupStateRejected})
	if err != nil || filtered.Total != 1 || filtered.VariantTotal != 1 || filtered.FamilyComparisonCounts.All != 1 || filtered.FamilyComparisonCounts.Exact != 0 || filtered.FamilyComparisonCounts.ContentConflict != 1 || len(filtered.Data) != 1 || len(filtered.Data[0].Variants) != 1 || filtered.Data[0].Variants[0].State != models.CandidateGroupStateRejected {
		t.Fatalf("filtered=%+v err=%v", filtered, err)
	}
	if filtered.Data[0].TotalVariantCount != 2 {
		t.Fatalf("filtered total_variant_count=%d, want 2", filtered.Data[0].TotalVariantCount)
	}
	if filtered.Data[0].SnapshotToken != family.SnapshotToken {
		t.Fatalf("filtered snapshot_token=%q, want full family token %q", filtered.Data[0].SnapshotToken, family.SnapshotToken)
	}

	exact, err := svc.ListCandidateFamilies(document.ID, tenantID, DocumentCandidateFamilyListOptions{ComparisonResult: models.CandidateComparisonExact, PageSize: 1})
	if err != nil || exact.Total != 1 || exact.VariantTotal != 1 || exact.TotalPages != 1 || exact.VariantStatusCounts.Retained != 1 || exact.VariantStatusCounts.Rejected != 1 || exact.FamilyComparisonCounts.All != 1 || exact.FamilyComparisonCounts.Exact != 1 || exact.FamilyComparisonCounts.ContentConflict != 1 || len(exact.Data) != 1 || len(exact.Data[0].Variants) != 1 || exact.Data[0].Variants[0].Candidate.Comparison == nil || exact.Data[0].Variants[0].Candidate.Comparison.Result != models.CandidateComparisonExact {
		t.Fatalf("exact comparison filter=%+v err=%v", exact, err)
	}
	conflict, err := svc.ListCandidateFamilies(document.ID, tenantID, DocumentCandidateFamilyListOptions{ComparisonResult: models.CandidateComparisonContentConflict, PageSize: 1})
	if err != nil || conflict.Total != 1 || conflict.VariantTotal != 1 || conflict.TotalPages != 1 || len(conflict.Data) != 1 || len(conflict.Data[0].Variants) != 1 || conflict.Data[0].Variants[0].Candidate.Comparison == nil || conflict.Data[0].Variants[0].Candidate.Comparison.Result != models.CandidateComparisonContentConflict {
		t.Fatalf("content conflict filter=%+v err=%v", conflict, err)
	}
	newCandidates, err := svc.ListCandidateFamilies(document.ID, tenantID, DocumentCandidateFamilyListOptions{ComparisonResult: models.CandidateComparisonNew})
	if err != nil || newCandidates.Total != 0 || newCandidates.VariantTotal != 0 || newCandidates.FamilyComparisonCounts.All != 1 || len(newCandidates.Data) != 0 {
		t.Fatalf("new comparison filter=%+v err=%v", newCandidates, err)
	}
	keyword, err := svc.ListCandidateFamilies(document.ID, tenantID, DocumentCandidateFamilyListOptions{Keyword: "  冲突  "})
	if err != nil || keyword.Total != 1 || keyword.VariantTotal != 1 || keyword.VariantStatusCounts.Retained != 1 || keyword.VariantStatusCounts.Rejected != 1 || keyword.FamilyComparisonCounts.All != 1 || keyword.FamilyComparisonCounts.Exact != 0 || keyword.FamilyComparisonCounts.ContentConflict != 1 || len(keyword.Data) != 1 || len(keyword.Data[0].Variants) != 1 || keyword.Data[0].Variants[0].Candidate.ID != candidates[2].ID {
		t.Fatalf("keyword filter=%+v err=%v", keyword, err)
	}
	codeKeyword, err := svc.ListCandidateFamilies(document.ID, tenantID, DocumentCandidateFamilyListOptions{Keyword: strings.ToUpper(code), ComparisonResult: models.CandidateComparisonExact})
	if err != nil || codeKeyword.Total != 1 || codeKeyword.VariantTotal != 1 || codeKeyword.FamilyComparisonCounts.All != 1 || codeKeyword.FamilyComparisonCounts.Exact != 1 || codeKeyword.FamilyComparisonCounts.ContentConflict != 1 || len(codeKeyword.Data) != 1 {
		t.Fatalf("code keyword with comparison filter=%+v err=%v", codeKeyword, err)
	}

	farPage, err := svc.ListCandidateFamilies(document.ID, tenantID, DocumentCandidateFamilyListOptions{Page: int(^uint(0) >> 1), PageSize: 1})
	if err != nil || farPage.Total != 1 || farPage.VariantTotal != 2 || farPage.TotalPages != 1 || len(farPage.Data) != 0 {
		t.Fatalf("far page=%+v err=%v", farPage, err)
	}
}
