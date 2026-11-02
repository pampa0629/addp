package cleanup

import (
	"context"
	"errors"
	"testing"

	"github.com/addp/common/execution"
)

func TestCleanupFailureStepUsesCurrentLeaseWithoutAuthorityOrReceipt(t *testing.T) {
	f := newRuntimeFixture(t)
	var lease execution.Lease
	f.runner.Handle = func(ctx context.Context, run *Run) error {
		lease = run.Lease
		var before execution.TaskExecution
		if err := f.runner.DB.Where("execution_id=?", lease.ExecutionID).First(&before).Error; err != nil {
			t.Fatal(err)
		}
		for _, stage := range []string{"manager_cleanup_plan", "meta_cleanup_plan raw-secret", ""} {
			if err := RecordFailureStep(ctx, f.runner.DB, lease, stage); err == nil {
				t.Fatal("foreign or unsafe stage accepted")
			}
		}
		f.denyEffect = true // Evidence recording must not request new effect authority.
		if err := RecordFailureStep(ctx, f.runner.DB, lease, "meta_cleanup_plan"); err != nil {
			t.Fatal(err)
		}
		var after execution.TaskExecution
		if err := f.runner.DB.Where("execution_id=?", lease.ExecutionID).First(&after).Error; err != nil || !after.LeaseExpiresAt.Equal(*before.LeaseExpiresAt) {
			t.Fatal("diagnostic changed the lease")
		}
		return errors.New("fixture private failure")
	}
	if err := f.runner.step(t.Context(), "failure-step"); err == nil {
		t.Fatal("Owner failure hidden")
	}
	child := f.readChild(t)
	observation := execution.Observe(&child)
	if child.Status != "failed" || observation.CurrentStep == nil || *observation.CurrentStep != "meta_cleanup_plan" || len(observation.Steps) != 1 || observation.Steps[0].Status != "failed" || observation.Steps[0].Phase != "terminal" {
		t.Fatal("closed failure step missing")
	}
	if receipt, err := execution.ReadCleanupReceipt(child); err != nil || receipt != nil {
		t.Fatal("diagnostic invented a receipt")
	}
	if err := RecordFailureStep(t.Context(), f.runner.DB, lease, "meta_cleanup_plan"); err == nil {
		t.Fatal("terminal attempt wrote evidence")
	}
}
