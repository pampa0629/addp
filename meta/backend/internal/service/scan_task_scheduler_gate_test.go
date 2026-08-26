package service

import (
	"context"
	"testing"
)

func TestScanTaskSchedulerDoesNotClaimWhenRegistrationGateIsClosed(t *testing.T) {
	gateChecked := false
	scheduler := &ScanTaskScheduler{claimGate: func() bool {
		gateChecked = true
		return false
	}}
	scheduler.runDueScheduledTasks(context.Background())
	if !gateChecked {
		t.Fatal("registration gate was not checked")
	}
}
