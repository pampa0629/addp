package service

import (
	"fmt"
	"testing"
	"time"

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
		{ExtractionID: second.ID, CandidateType: "glossary", Code: code, Name: "户外活动", Definition: "不同定义", Status: "rejected", Version: 2, ReviewedBy: &reviewer, ReviewedAt: &reviewedAt},
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

	svc := &DocumentService{repo: repo}
	response, err := svc.ListCandidateGroups(document.ID, tenantID, DocumentCandidateGroupListOptions{Page: 1, PageSize: 1})
	if err != nil {
		t.Fatal(err)
	}
	if response.Total != 2 || response.TotalPages != 2 || response.ComparisonCounts.Exact != 1 || response.ComparisonCounts.ContentConflict != 1 || len(response.Data) != 1 {
		t.Fatalf("response=%+v", response)
	}
	if response.StatusCounts.Retained != 1 || response.StatusCounts.Rejected != 1 || response.StatusCounts.Pending != 0 {
		t.Fatalf("status counts=%+v", response.StatusCounts)
	}
	group := response.Data[0]
	if group.State != models.CandidateGroupStateRetained || group.OccurrenceCount != 2 || group.Candidate.ID != candidates[0].ID {
		t.Fatalf("group=%+v", group)
	}
	if len(group.Occurrences) != 2 || len(group.Occurrences[0].Evidences) != 1 || group.Candidate.Comparison == nil || group.Candidate.Comparison.Result != models.CandidateComparisonExact {
		t.Fatalf("occurrences/comparison=%+v", group)
	}

	filtered, err := svc.ListCandidateGroups(document.ID, tenantID, DocumentCandidateGroupListOptions{State: models.CandidateGroupStateRejected})
	if err != nil || filtered.Total != 1 || filtered.ComparisonCounts.Exact != 0 || filtered.ComparisonCounts.ContentConflict != 1 || len(filtered.Data) != 1 || filtered.Data[0].State != models.CandidateGroupStateRejected {
		t.Fatalf("filtered=%+v err=%v", filtered, err)
	}

	exact, err := svc.ListCandidateGroups(document.ID, tenantID, DocumentCandidateGroupListOptions{ComparisonResult: models.CandidateComparisonExact, PageSize: 1})
	if err != nil || exact.Total != 1 || exact.TotalPages != 1 || exact.StatusCounts.Retained != 1 || exact.StatusCounts.Rejected != 1 || exact.ComparisonCounts.Exact != 1 || exact.ComparisonCounts.ContentConflict != 1 || len(exact.Data) != 1 || exact.Data[0].Candidate.Comparison == nil || exact.Data[0].Candidate.Comparison.Result != models.CandidateComparisonExact {
		t.Fatalf("exact comparison filter=%+v err=%v", exact, err)
	}
	conflict, err := svc.ListCandidateGroups(document.ID, tenantID, DocumentCandidateGroupListOptions{ComparisonResult: models.CandidateComparisonContentConflict, PageSize: 1})
	if err != nil || conflict.Total != 1 || conflict.TotalPages != 1 || len(conflict.Data) != 1 || conflict.Data[0].Candidate.Comparison == nil || conflict.Data[0].Candidate.Comparison.Result != models.CandidateComparisonContentConflict {
		t.Fatalf("content conflict filter=%+v err=%v", conflict, err)
	}
	newCandidates, err := svc.ListCandidateGroups(document.ID, tenantID, DocumentCandidateGroupListOptions{ComparisonResult: models.CandidateComparisonNew})
	if err != nil || newCandidates.Total != 0 || len(newCandidates.Data) != 0 {
		t.Fatalf("new comparison filter=%+v err=%v", newCandidates, err)
	}

	farPage, err := svc.ListCandidateGroups(document.ID, tenantID, DocumentCandidateGroupListOptions{Page: int(^uint(0) >> 1), PageSize: 1})
	if err != nil || farPage.Total != 2 || farPage.TotalPages != 2 || len(farPage.Data) != 0 {
		t.Fatalf("far page=%+v err=%v", farPage, err)
	}
}
