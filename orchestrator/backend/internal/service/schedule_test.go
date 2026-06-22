package service

import (
	"testing"
	"time"

	"github.com/addp/orchestrator/internal/models"
)

func TestApplyOrchestrationScheduleCalculatesNextRunAtWhenEnabled(t *testing.T) {
	orch := &models.Orchestration{
		Enabled:  true,
		Schedule: "0 2 * * *",
	}

	if err := ApplyOrchestrationSchedule(orch, time.Date(2026, 6, 19, 1, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("ApplyOrchestrationSchedule() error = %v", err)
	}
	if orch.NextRunAt == nil || orch.NextRunAt.Hour() != 2 {
		t.Fatalf("next_run_at = %#v, want hour 2", orch.NextRunAt)
	}
}

func TestApplyOrchestrationScheduleClearsNextRunAtWhenDisabled(t *testing.T) {
	next := time.Now()
	orch := &models.Orchestration{
		Enabled:   false,
		Schedule:  "0 2 * * *",
		NextRunAt: &next,
	}

	if err := ApplyOrchestrationSchedule(orch, time.Now()); err != nil {
		t.Fatalf("ApplyOrchestrationSchedule() error = %v", err)
	}
	if orch.NextRunAt != nil {
		t.Fatalf("next_run_at = %#v, want nil", orch.NextRunAt)
	}
}
