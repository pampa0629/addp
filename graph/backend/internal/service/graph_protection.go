package service

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/addp/common/dataprotection/projectionstore"
	"github.com/addp/common/engine/plugin"
)

var ErrGraphProtectionRequired = errors.New("graph data protection is required")

// GraphProtectionGate protects provider-owned domain targets, never arbitrary Cypher.
// Its lifetime covers the whole service operation, including native search and traversal.
type GraphProtectionGate struct {
	store *projectionstore.Store
	reads projectionstore.InflightReads
}

func NewGraphProtectionGate(store *projectionstore.Store) *GraphProtectionGate {
	return &GraphProtectionGate{store: store}
}

func (g *GraphProtectionGate) BeginTarget(ctx context.Context, tenantID uint, provider plugin.PropertyGraphProvider, path plugin.EngineCatalogPath) (func(), error) {
	if g == nil || g.store == nil || tenantID == 0 {
		return nil, ErrGraphProtectionRequired
	}
	model, ok := provider.(plugin.EngineCatalogModelProvider)
	if !ok {
		return nil, ErrGraphProtectionRequired
	}
	end, err := g.reads.Begin(int64(tenantID))
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrGraphProtectionRequired, err)
	}
	if err = g.store.RequireCatalogPathUnmanaged(ctx, int64(tenantID), model.EngineCatalogModel(), path, time.Now().UTC()); err != nil {
		end()
		return nil, fmt.Errorf("%w: %w", ErrGraphProtectionRequired, err)
	}
	if response, ok := ctx.Value(graphProtectionResponseKey{}).(*graphProtectionResponse); ok {
		response.mu.Lock()
		if response.closed {
			response.mu.Unlock()
			end()
			return nil, ErrGraphProtectionRequired
		}
		response.ends = append(response.ends, end)
		response.mu.Unlock()
		return func() {}, nil
	}
	return end, nil
}

func (g *GraphProtectionGate) ReadyToAcknowledge(_ context.Context, tenantID int64, _ string) error {
	if g == nil || g.store == nil || tenantID <= 0 {
		return ErrGraphProtectionRequired
	}
	if g.reads.HasActiveExecutionsForTenant(tenantID) {
		return fmt.Errorf("%w: reads under the previous projection are still active", ErrGraphProtectionRequired)
	}
	return nil
}

// Retain reads through HTTP serialization; domain calls without a response
// context release on service return (for background build/review operations).
type graphProtectionResponseKey struct{}
type graphProtectionResponse struct {
	mu     sync.Mutex
	ends   []func()
	closed bool
}

func WithGraphProtectionResponse(ctx context.Context) (context.Context, func()) {
	response := &graphProtectionResponse{}
	return context.WithValue(ctx, graphProtectionResponseKey{}, response), func() {
		response.mu.Lock()
		defer response.mu.Unlock()
		for _, end := range response.ends {
			end()
		}
		response.ends = nil
		response.closed = true
	}
}
