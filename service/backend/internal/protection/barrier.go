package protection

import (
	"context"
	"fmt"
	"sync"
)

type DerivedDataPurger interface {
	PurgeProtectionDerivedData(context.Context, int64) error
}

type AcknowledgementBarrier struct {
	gate   *Gate
	purger DerivedDataPurger
	mu     sync.Mutex
	purged map[int64]string
}

func NewAcknowledgementBarrier(gate *Gate, purger DerivedDataPurger) *AcknowledgementBarrier {
	return &AcknowledgementBarrier{gate: gate, purger: purger, purged: make(map[int64]string)}
}

func (b *AcknowledgementBarrier) ReadyToAcknowledge(ctx context.Context, tenantID int64, cursor string) error {
	if b == nil || b.gate == nil || b.purger == nil || tenantID <= 0 || cursor == "" {
		return fmt.Errorf("service protection acknowledgement barrier is not configured")
	}
	if b.gate.HasActiveExecutionsForTenant(tenantID) {
		return fmt.Errorf("service still has requests running under the previous protection cursor")
	}
	b.mu.Lock()
	alreadyPurged := b.purged[tenantID] == cursor
	b.mu.Unlock()
	if alreadyPurged {
		return nil
	}
	if err := b.purger.PurgeProtectionDerivedData(ctx, tenantID); err != nil {
		return err
	}
	b.mu.Lock()
	b.purged[tenantID] = cursor
	b.mu.Unlock()
	return nil
}
