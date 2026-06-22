package service

import (
	"fmt"
	"strings"
	"time"

	commonScheduler "github.com/addp/common/scheduler"
)

func nextTransferRunAt(schedule string, from time.Time) (*time.Time, error) {
	schedule = strings.TrimSpace(schedule)
	if schedule == "" {
		return nil, nil
	}
	builder := commonScheduler.NewExpressionBuilderWithSeconds()
	if err := builder.Validate(schedule); err != nil {
		return nil, fmt.Errorf("invalid transfer task schedule: %w", err)
	}
	next, err := builder.NextRunTime(schedule, from)
	if err != nil {
		return nil, fmt.Errorf("calculate transfer task next_run_at: %w", err)
	}
	return &next, nil
}

func NextTransferRunAtForScheduler(schedule string, from time.Time) (*time.Time, error) {
	return nextTransferRunAt(schedule, from)
}
