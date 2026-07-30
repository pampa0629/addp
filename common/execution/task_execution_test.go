package execution

import (
	"testing"

	"github.com/addp/common/events"
)

func TestStatusFromCleanupResult(t *testing.T) {
	t.Parallel()

	tests := []struct {
		cleanupStatus string
		want          string
	}{
		{cleanupStatus: events.CleanupResultSuccess, want: ExecutionStatusSuccess},
		{cleanupStatus: events.CleanupResultSkipped, want: ExecutionStatusSuccess},
		{cleanupStatus: events.CleanupResultFailed, want: ExecutionStatusFailed},
		{cleanupStatus: events.CleanupResultPartialSuccess, want: ExecutionStatusFailed},
		{cleanupStatus: events.CleanupResultTimeout, want: ExecutionStatusTimeout},
	}

	for _, test := range tests {
		if got := StatusFromCleanupResult(test.cleanupStatus); got != test.want {
			t.Errorf("StatusFromCleanupResult(%q) = %q, want %q", test.cleanupStatus, got, test.want)
		}
	}
}
