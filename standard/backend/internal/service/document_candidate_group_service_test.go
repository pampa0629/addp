package service

import (
	"errors"
	"testing"
	"time"

	"github.com/addp/standard/internal/models"
)

func TestBuildDocumentCandidateGroupsAggregatesNormalizedSemanticOccurrences(t *testing.T) {
	firstSeen := time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)
	lastSeen := firstSeen.Add(24 * time.Hour)
	reviewedAt := firstSeen.Add(time.Hour)
	reviewer := int64(7)
	dataType := " string "
	domainKind := " unrestricted "

	extractions := []models.DocumentExtraction{
		{ID: 2, DocumentRevisionID: 12, RequestedBy: 9, CreatedAt: lastSeen, Candidates: []models.DocumentExtractionCandidate{{
			ID: 22, CandidateType: "element", Code: "outdoor_person_id", Name: "人员 标识", Definition: "户外参与者\n稳定标识",
			Payload: models.DocumentExtractionCandidatePayload{DataType: &dataType, ValueDomainKind: &domainKind, Dimensions: []string{"activity", " person ", "activity"}},
			Status:  "pending", Version: 1, Evidences: []models.DocumentExtractionEvidence{{ID: 102, ExcerptHash: "new"}},
		}}},
		{ID: 1, DocumentRevisionID: 11, RequestedBy: 8, CreatedAt: firstSeen, Candidates: []models.DocumentExtractionCandidate{{
			ID: 11, CandidateType: "element", Code: "outdoor_person_id", Name: "人员  标识", Definition: "户外参与者 稳定标识",
			Payload: models.DocumentExtractionCandidatePayload{DataType: stringPointer("string"), ValueDomainKind: stringPointer("unrestricted"), Dimensions: []string{"person", "activity"}},
			Status:  "retained", Version: 2, ReviewedBy: &reviewer, ReviewedAt: &reviewedAt, Evidences: []models.DocumentExtractionEvidence{{ID: 101, ExcerptHash: "old"}},
		}}},
	}

	groups := buildDocumentCandidateGroups(extractions)
	if len(groups) != 1 {
		t.Fatalf("groups=%d want=1", len(groups))
	}
	group := groups[0]
	if len(group.SemanticFingerprint) != 64 || group.State != models.CandidateGroupStateRetained || group.OccurrenceCount != 2 {
		t.Fatalf("group=%+v", group)
	}
	if group.Candidate.ID != 11 || group.FirstSeenAt != firstSeen || group.LastSeenAt != lastSeen {
		t.Fatalf("representative=%d first=%v last=%v", group.Candidate.ID, group.FirstSeenAt, group.LastSeenAt)
	}
	if len(group.Occurrences) != 2 || group.Occurrences[0].CandidateID != 22 || group.Occurrences[1].CandidateID != 11 {
		t.Fatalf("occurrences=%+v", group.Occurrences)
	}
}

func TestBuildDocumentCandidateGroupsSeparatesSemanticVariantsAndPrioritizesFormalization(t *testing.T) {
	seen := time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)
	formalizedAt := seen.Add(time.Hour)
	reviewedAt := seen.Add(2 * time.Hour)
	formalization := &models.DocumentCandidateFormalization{CandidateID: 1, Action: models.CandidateFormalizationLinkedExisting, CreatedAt: formalizedAt}
	extractions := []models.DocumentExtraction{{ID: 1, CreatedAt: seen, Candidates: []models.DocumentExtractionCandidate{
		{ID: 1, CandidateType: "glossary", Code: "outdoor_activity", Name: "户外活动", Definition: "户外活动定义", Status: "retained", Version: 2, Formalization: formalization},
		{ID: 2, CandidateType: "glossary", Code: "outdoor_activity", Name: "户外活动", Definition: "另一业务定义", Status: "rejected", Version: 2, ReviewedAt: &reviewedAt},
	}}}

	groups := buildDocumentCandidateGroups(extractions)
	if len(groups) != 2 {
		t.Fatalf("groups=%d want=2", len(groups))
	}
	if groups[0].State != models.CandidateGroupStateFormalized || groups[0].Candidate.ID != 1 {
		t.Fatalf("first group=%+v", groups[0])
	}
	if groups[1].State != models.CandidateGroupStateRejected || groups[1].Candidate.ID != 2 {
		t.Fatalf("second group=%+v", groups[1])
	}
}

func TestDocumentCandidateSemanticFingerprintNormalizesItemAndDimensionOrder(t *testing.T) {
	left := models.DocumentExtractionCandidate{CandidateType: "code_set", Code: "outdoor_status", Name: "户外 状态", Definition: "状态 定义", Payload: models.DocumentExtractionCandidatePayload{
		Dimensions: []string{"activity", "person"},
		Items:      []models.DocumentExtractionCandidatePayloadItem{{Code: "closed", Name: "关闭"}, {Code: "open", Name: "开放"}},
	}}
	right := left
	right.Name = "户外\n状态"
	right.Payload.Dimensions = []string{" person ", "activity", "person"}
	right.Payload.Items = []models.DocumentExtractionCandidatePayloadItem{{Code: "open", Name: "开放"}, {Code: "closed", Name: "关闭"}}

	if documentCandidateSemanticFingerprint(left) != documentCandidateSemanticFingerprint(right) {
		t.Fatal("normalized candidates should have the same fingerprint")
	}
	right.Definition = "不同定义"
	if documentCandidateSemanticFingerprint(left) == documentCandidateSemanticFingerprint(right) {
		t.Fatal("different semantic content must have a different fingerprint")
	}
}

func TestNormalizeDocumentCandidateFamilyOptions(t *testing.T) {
	opts := DocumentCandidateFamilyListOptions{Keyword: "  OUTDOOR   Person  "}
	page, pageSize, err := normalizeDocumentCandidateFamilyOptions(&opts)
	if err != nil || page != 1 || pageSize != 20 {
		t.Fatalf("page=%d pageSize=%d err=%v", page, pageSize, err)
	}
	if opts.Keyword != "outdoor person" {
		t.Fatalf("keyword=%q", opts.Keyword)
	}
	for _, opts := range []DocumentCandidateFamilyListOptions{
		{State: "unknown"}, {CandidateType: "unknown"}, {ComparisonResult: "unknown"}, {Page: -1}, {PageSize: 101},
	} {
		if _, _, err := normalizeDocumentCandidateFamilyOptions(&opts); !errors.Is(err, ErrDocumentCandidateFamilyQueryInvalid) {
			t.Fatalf("opts=%+v err=%v", opts, err)
		}
	}
}

func TestDocumentCandidateMatchesKeywordUsesNormalizedCodeOrName(t *testing.T) {
	candidate := models.DocumentExtractionCandidate{Code: "Outdoor_Person_ID", Name: "户外  人员标识"}
	for _, keyword := range []string{"outdoor_person", "人员标识", "户外 人员"} {
		if !documentCandidateMatchesKeyword(candidate, keyword) {
			t.Fatalf("keyword %q should match", keyword)
		}
	}
	if documentCandidateMatchesKeyword(candidate, "活动日期") {
		t.Fatal("unrelated keyword should not match")
	}
}

func TestBuildDocumentCandidateFamiliesKeepsSemanticVariantsIndependent(t *testing.T) {
	firstSeen := time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)
	lastSeen := firstSeen.Add(24 * time.Hour)
	groups := []models.DocumentExtractionCandidateGroup{
		{SemanticFingerprint: "exact", OccurrenceCount: 2, FirstSeenAt: firstSeen, LastSeenAt: lastSeen, Candidate: models.DocumentExtractionCandidate{CandidateType: "element", Code: "outdoor_person_id", Name: "人员标识", Comparison: &models.DocumentExtractionCandidateComparison{Result: models.CandidateComparisonExact}}},
		{SemanticFingerprint: "conflict", OccurrenceCount: 1, FirstSeenAt: lastSeen, LastSeenAt: lastSeen, Candidate: models.DocumentExtractionCandidate{CandidateType: "element", Code: "outdoor_person_id", Name: "户外人员标识", Comparison: &models.DocumentExtractionCandidateComparison{Result: models.CandidateComparisonContentConflict}}},
		{SemanticFingerprint: "glossary", OccurrenceCount: 1, FirstSeenAt: firstSeen, LastSeenAt: firstSeen, Candidate: models.DocumentExtractionCandidate{CandidateType: "glossary", Code: "outdoor_person_id", Name: "人员标识", Comparison: &models.DocumentExtractionCandidateComparison{Result: models.CandidateComparisonNew}}},
	}

	families := buildDocumentCandidateFamilies(groups)
	if len(families) != 2 {
		t.Fatalf("families=%+v", families)
	}
	family := families[0]
	if family.FamilyKey != "element:outdoor_person_id" || family.VariantCount != 2 || family.OccurrenceCount != 3 || len(family.Variants) != 2 || family.RepresentativeName != "人员标识" || !family.FirstSeenAt.Equal(firstSeen) || !family.LastSeenAt.Equal(lastSeen) {
		t.Fatalf("family=%+v", family)
	}
	if family.Variants[0].SemanticFingerprint != "exact" || family.Variants[1].SemanticFingerprint != "conflict" {
		t.Fatalf("variants=%+v", family.Variants)
	}

	counts := models.DocumentExtractionCandidateFamilyComparisonCounts{}
	for _, item := range families {
		incrementCandidateFamilyComparisonCounts(&counts, item)
	}
	if counts.All != 2 || counts.New != 1 || counts.Exact != 1 || counts.ContentConflict != 1 || counts.ScopeConflict != 0 {
		t.Fatalf("counts=%+v", counts)
	}
}
