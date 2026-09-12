package candidate

import (
	"testing"
	"time"

	"github.com/addp/standard/internal/models"
)

func TestIsPreferredRepresentativeUsesGovernanceThenTimeThenID(t *testing.T) {
	seen := time.Date(2026, 9, 10, 8, 0, 0, 0, time.UTC)
	reviewedAt := seen.Add(time.Hour)
	formalizedAt := seen.Add(2 * time.Hour)
	pending := models.DocumentExtractionCandidate{ID: 3, Status: models.CandidateGroupStatePending}
	reviewed := models.DocumentExtractionCandidate{ID: 2, Status: models.CandidateGroupStateRetained, ReviewedAt: &reviewedAt}
	formalized := models.DocumentExtractionCandidate{ID: 1, Status: models.CandidateGroupStateRetained, ReviewedAt: &reviewedAt, Formalization: &models.DocumentCandidateFormalization{CreatedAt: formalizedAt}}

	if !IsPreferredRepresentative(reviewed, seen, pending, seen.Add(time.Hour)) {
		t.Fatal("reviewed candidate must outrank a newer pending occurrence")
	}
	if !IsPreferredRepresentative(formalized, seen, reviewed, seen) {
		t.Fatal("formalized candidate must outrank a reviewed occurrence")
	}
	if GovernanceState(formalized) != models.CandidateGroupStateFormalized || GovernanceState(reviewed) != models.CandidateGroupStateRetained || GovernanceState(pending) != models.CandidateGroupStatePending {
		t.Fatal("unexpected governance state")
	}
	newerPending := pending
	newerPending.ID = 4
	if !IsPreferredRepresentative(newerPending, seen.Add(time.Hour), pending, seen) {
		t.Fatal("newer pending occurrence must be preferred")
	}
	tiedPending := pending
	tiedPending.ID = 4
	if !IsPreferredRepresentative(tiedPending, seen, pending, seen) {
		t.Fatal("higher candidate ID must break an equal-time tie")
	}
}

func TestFamilySnapshotTokenIsOrderIndependentAndVersionSensitive(t *testing.T) {
	members := []models.DocumentExtractionCandidate{
		{ID: 12, CandidateType: "glossary", Code: "outdoor_activity", Name: "户外活动", Definition: "定义二", Version: 4},
		{ID: 11, CandidateType: "glossary", Code: "outdoor_activity", Name: "户外活动", Definition: "定义一", Version: 3},
	}
	token := FamilySnapshotToken("glossary", "outdoor_activity", members)
	if len(token) != 64 {
		t.Fatalf("token length = %d, want 64", len(token))
	}
	if reordered := FamilySnapshotToken("glossary", "outdoor_activity", []models.DocumentExtractionCandidate{members[1], members[0]}); reordered != token {
		t.Fatalf("reordered token = %q, want %q", reordered, token)
	}
	changed := append([]models.DocumentExtractionCandidate(nil), members...)
	changed[0].Version++
	if FamilySnapshotToken("glossary", "outdoor_activity", changed) == token {
		t.Fatal("candidate version change must invalidate the family snapshot token")
	}
	if FamilySnapshotToken("glossary", "outdoor_route", members) == token {
		t.Fatal("candidate family identity must be part of the snapshot token")
	}
}
