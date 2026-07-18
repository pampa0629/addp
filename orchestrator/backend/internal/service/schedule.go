package service

import (
	"strings"
	"time"

	commonScheduler "github.com/addp/common/scheduler"
	"github.com/addp/orchestrator/internal/models"
)

func ApplyOrchestrationSchedule(orch *models.Orchestration, now time.Time) error {
	if orch == nil {
		return nil
	}
	orch.Schedule = strings.TrimSpace(orch.Schedule)
	if orch.Schedule == "" || !orch.Enabled {
		orch.NextRunAt = nil
		return nil
	}

	builder := commonScheduler.NewExpressionBuilder()
	if err := builder.Validate(orch.Schedule); err != nil {
		return &ScheduleValidationError{Code: ScheduleExpressionInvalid, Expression: orch.Schedule, Cause: err}
	}
	next, err := builder.NextRunTime(orch.Schedule, now)
	if err != nil {
		return &ScheduleValidationError{Code: ScheduleNextRunFailed, Expression: orch.Schedule, Cause: err}
	}
	orch.NextRunAt = &next
	return nil
}
