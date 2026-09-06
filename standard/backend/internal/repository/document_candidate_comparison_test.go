package repository

import (
	"testing"
	"time"

	"github.com/addp/standard/internal/models"
)

func TestSelectDocumentCandidateComparisonRevisionPriority(t *testing.T) {
	now := time.Date(2026, 9, 6, 0, 0, 0, 0, time.UTC)
	past, future := now.Add(-time.Hour), now.Add(time.Hour)
	revisions := []documentCandidateComparisonRevision{
		{ID: 13, RevisionNo: 3, Status: models.RevisionStatusWithdrawn},
		{ID: 12, RevisionNo: 2, Status: models.RevisionStatusPublished, EffectiveFrom: &past, EffectiveTo: &future},
		{ID: 11, RevisionNo: 1, Status: models.RevisionStatusDraft},
	}
	draftID := int64(11)

	selected, ok := selectDocumentCandidateComparisonRevision(&draftID, revisions, now)
	if !ok || selected.ID != draftID {
		t.Fatalf("draft pointer selection=%+v ok=%v", selected, ok)
	}
	selected, ok = selectDocumentCandidateComparisonRevision(nil, revisions, now)
	if !ok || selected.ID != 12 {
		t.Fatalf("effective published selection=%+v ok=%v", selected, ok)
	}
	selected, ok = selectDocumentCandidateComparisonRevision(nil, revisions[:1], now)
	if !ok || selected.ID != 13 {
		t.Fatalf("latest historical selection=%+v ok=%v", selected, ok)
	}
	if selected, ok = selectDocumentCandidateComparisonRevision(nil, nil, now); ok {
		t.Fatalf("empty revisions selection=%+v ok=%v", selected, ok)
	}
}
