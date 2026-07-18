package service

import (
	"errors"
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

func TestApplyOrchestrationScheduleReturnsStructuredValidationError(t *testing.T) {
	orch := &models.Orchestration{Enabled: true, Schedule: "not-a-cron"}
	err := ApplyOrchestrationSchedule(orch, time.Now())

	var validationErr *ScheduleValidationError
	if !errors.As(err, &validationErr) {
		t.Fatalf("error = %T %v, want *ScheduleValidationError", err, err)
	}
	if validationErr.Code != ScheduleExpressionInvalid || validationErr.Expression != "not-a-cron" {
		t.Fatalf("schedule validation error = %#v", validationErr)
	}
}
