package protection

import (
	"context"
	"fmt"

	"github.com/addp/common/dataprotection/projectionstore"
)

type ProjectionFreshener interface {
	EnsureCurrent(context.Context, int64) error
}

// ReadBoundary registers work before refreshing the local projection. An end
// function must remain deferred through serialization or result persistence.
// It is not a resource authorization decision.
type ReadBoundary struct {
	store ProjectionFreshener
	reads projectionstore.InflightReads
}

func NewReadBoundary(store ProjectionFreshener) *ReadBoundary {
	return &ReadBoundary{store: store}
}

func (b *ReadBoundary) BeginRead(ctx context.Context, tenantID int64) (func(), error) {
	if b == nil || b.store == nil {
		return nil, ErrRequired
	}
	end, err := b.reads.Begin(tenantID)
	if err != nil {
		return nil, ErrRequired
	}
	if err := b.store.EnsureCurrent(ctx, tenantID); err != nil {
		end()
		return nil, fmt.Errorf("%w: %w", ErrRequired, err)
	}
	return end, nil
}

func (b *ReadBoundary) HasActiveExecutionsForTenant(tenantID int64) bool {
	return b != nil && b.reads.HasActiveExecutionsForTenant(tenantID)
}
