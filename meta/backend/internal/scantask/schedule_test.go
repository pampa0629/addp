package scantask

import (
	"testing"

	commonModels "github.com/addp/common/models"
	commonScheduler "github.com/addp/common/scheduler"
)

func TestBuildCronExpressionFromPolicyDaily(t *testing.T) {
	t.Parallel()

	got, err := BuildCronExpressionFromPolicy(nil, &commonModels.ScanPolicy{
		ScheduleMode: "daily",
		ScheduleTime: "03:15",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "15 3 * * *" {
		t.Fatalf("cron = %q, want 15 3 * * *", got)
	}
}

func TestBuildCronExpressionFromPolicyWeekly(t *testing.T) {
	t.Parallel()

	got, err := BuildCronExpressionFromPolicy(nil, &commonModels.ScanPolicy{
		ScheduleMode:  "weekly",
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

func TestBuildCronExpressionFromPolicyValidatesCron(t *testing.T) {
	t.Parallel()

	builder := commonScheduler.NewExpressionBuilder()
	if _, err := BuildCronExpressionFromPolicy(builder, &commonModels.ScanPolicy{
		ScheduleMode:   "cron",
		CronExpression: "invalid",
	}); err == nil {
		t.Fatalf("expected invalid cron error")
	}
}
