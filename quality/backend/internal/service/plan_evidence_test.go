package service

import (
	"context"
	"testing"

	"github.com/addp/quality/internal/models"
)

func TestFailureEvidenceLimitNeverCollectsPartialBaseline(t *testing.T) {
	e, err := collectFailureEvidence(context.Background(), nil, planCompiledRule{}, PlanTableBinding{RecordKey: []string{"id"}}, nil, planCounts{FailedCount: models.MaxFailureKeys + 1}, nil)
	if err != nil || e.Reason != "too_many_failures" || len(e.Keys) != 0 || e.Scope != "" {
		t.Fatalf("partial evidence allowed: %+v %v", e, err)
	}
}
