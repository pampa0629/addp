package api

import (
	"testing"

	commonExecution "github.com/addp/common/execution"
)

func TestPPTXPDFTerminalFailureRequiresExplicitRetry(t *testing.T) {
	for _, status := range []string{
		commonExecution.ExecutionStatusFailed,
		commonExecution.ExecutionStatusTimeout,
		commonExecution.ExecutionStatusCancelled,
	} {
		if !isPPTXPDFTerminalFailure(status) {
			t.Fatalf("status %q should require explicit retry", status)
		}
	}
	for _, status := range []string{
		commonExecution.ExecutionStatusPending,
		commonExecution.ExecutionStatusRunning,
		commonExecution.ExecutionStatusSuccess,
		"",
	} {
		if isPPTXPDFTerminalFailure(status) {
			t.Fatalf("status %q should not be treated as terminal failure", status)
		}
	}
}
