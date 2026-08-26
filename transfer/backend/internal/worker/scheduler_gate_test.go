package worker

import (
	"context"
	"testing"
)

func TestSchedulerDoesNotClaimWhenRegistrationGateIsClosed(t *testing.T) {
	gateChecked := false
	scheduler := &Scheduler{claimGate: func() bool {
		gateChecked = true
		return false
	}}
	scheduler.runDueScheduledTasks(context.Background())
	if !gateChecked {
		t.Fatal("registration gate was not checked")
	}
}
