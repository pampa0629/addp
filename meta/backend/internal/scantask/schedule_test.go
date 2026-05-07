package scantask

import (
	"testing"

	commonModels "github.com/addp/common/models"
	commonScheduler "github.com/addp/common/scheduler"
)

func TestBuildCronExpressionFromScanConfigDaily(t *testing.T) {
	t.Parallel()

	got, err := BuildCronExpressionFromScanConfig(nil, &commonModels.ScanConfig{
		ScheduleType: "daily",
		ScheduleTime: "03:15",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "15 3 * * *" {
		t.Fatalf("cron = %q, want 15 3 * * *", got)
	}
}

func TestBuildCronExpressionFromScanConfigWeekly(t *testing.T) {
	t.Parallel()

	got, err := BuildCronExpressionFromScanConfig(nil, &commonModels.ScanConfig{
		ScheduleType:  "weekly",
		ScheduleTime:  "08:30",
		ScheduleValue: []int{1, 3, 5},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "30 8 * * 1,3,5" {
		t.Fatalf("cron = %q, want 30 8 * * 1,3,5", got)
	}
}

func TestBuildCronExpressionFromScanConfigValidatesCron(t *testing.T) {
	t.Parallel()

	builder := commonScheduler.NewExpressionBuilder()
	if _, err := BuildCronExpressionFromScanConfig(builder, &commonModels.ScanConfig{
		ScheduleType:   "cron",
		CronExpression: "invalid",
	}); err == nil {
		t.Fatalf("expected invalid cron error")
	}
}
