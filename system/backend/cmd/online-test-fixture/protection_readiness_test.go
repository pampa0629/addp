package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type fixtureProtectionReader struct {
	read func(context.Context, int64) (bool, error)
}

func (r fixtureProtectionReader) Initialized(ctx context.Context, tenantID int64) (bool, error) {
	return r.read(ctx, tenantID)
}

func TestProtectionReadinessWaitsForEveryOwnerAndExactTenant(t *testing.T) {
	counts := map[string]int{}
	readers := map[string]protectionInitializationReader{}
	for owner, readyAfter := range map[string]int{"develop": 2, "manager": 3} {
		readers[owner] = fixtureProtectionReader{func(_ context.Context, tenantID int64) (bool, error) {
			if tenantID != 42 {
				t.Fatalf("observed another Tenant: %d", tenantID)
			}
			counts[owner]++
			return counts[owner] >= readyAfter, nil
		}}
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if err := waitProtectionInitialization(ctx, readers, 42, time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if counts["develop"] != 3 || counts["manager"] != 3 {
		t.Fatalf("returned before all Owners initialized: %v", counts)
	}
}

func TestProtectionReadinessFailsOnReadErrorWithoutRetry(t *testing.T) {
	failure := errors.New("database unavailable")
	calls := 0
	reader := fixtureProtectionReader{func(context.Context, int64) (bool, error) {
		calls++
		return false, failure
	}}
	err := waitProtectionInitialization(t.Context(), map[string]protectionInitializationReader{"develop": reader}, 42, time.Millisecond)
	if !errors.Is(err, failure) || !strings.Contains(err.Error(), "develop") || !strings.Contains(err.Error(), "42") || calls != 1 {
		t.Fatalf("lost diagnostic or retried a read failure: calls=%d, err=%v", calls, err)
	}
}

func TestProtectionReadinessDeadlinePreservesPendingOwner(t *testing.T) {
	reader := fixtureProtectionReader{func(context.Context, int64) (bool, error) { return false, nil }}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
	defer cancel()
	err := waitProtectionInitialization(ctx, map[string]protectionInitializationReader{"manager": reader}, 42, time.Hour)
	if !errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), "manager") || !strings.Contains(err.Error(), "42") {
		t.Fatalf("uninitialized Owner treated as ready or lost timeout evidence: %v", err)
	}
}
