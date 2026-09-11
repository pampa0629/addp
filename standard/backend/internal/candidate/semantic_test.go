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
