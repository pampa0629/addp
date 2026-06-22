package service

import (
	"fmt"
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
		return fmt.Errorf("invalid orchestration schedule: %w", err)
	}
	next, err := builder.NextRunTime(orch.Schedule, now)
	if err != nil {
		return fmt.Errorf("calculate orchestration next_run_at: %w", err)
	}
	orch.NextRunAt = &next
	return nil
}
