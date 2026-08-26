package service

import (
	"context"
	"testing"
)

func TestEmbeddingTaskSchedulerDoesNotClaimWhenRegistrationGateIsClosed(t *testing.T) {
	gateChecked := false
	scheduler := &EmbeddingTaskScheduler{claimGate: func() bool {
		gateChecked = true
		return false
	}}
	scheduler.runDueScheduledTasks(context.Background())
	if !gateChecked {
		t.Fatal("registration gate was not checked")
	}
}
