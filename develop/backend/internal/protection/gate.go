package protection

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/addp/common/dataprotection/projectionstore"
	"github.com/addp/common/engine/plugin"
)

var ErrRequired = errors.New("develop data protection gate is required")

type Gate struct {
	store  *projectionstore.Store
	mu     sync.Mutex
	active map[int64]int
}

func NewGate(store *projectionstore.Store) *Gate {
	return &Gate{store: store, active: make(map[int64]int)}
}

func (g *Gate) BeginPreparedQuery(ctx context.Context, tenantID uint, enginePlugin plugin.EnginePlugin, prepared plugin.PreparedQuery) (func(*plugin.QueryResult) error, func(), error) {
	end, err := g.begin(tenantID)
	if err != nil {
		return nil, nil, err
	}
	modelProvider, ok := enginePlugin.(plugin.EngineCatalogModelProvider)
	if !ok || prepared == nil {
		end()
		return nil, nil, fmt.Errorf("%w: query provider has no catalog model", ErrRequired)
	}
	protect, err := g.store.PrepareQueryProtection(ctx, int64(tenantID), modelProvider.EngineCatalogModel(), prepared, "query", time.Now().UTC())
	if err != nil {
		end()
		return nil, nil, fmt.Errorf("%w: %w", ErrRequired, err)
	}
	return protect, end, nil
}

func (g *Gate) BeginCatalogPath(ctx context.Context, tenantID uint, enginePlugin plugin.EnginePlugin, path plugin.EngineCatalogPath) (func(), error) {
	end, err := g.begin(tenantID)
	if err != nil {
		return nil, err
	}
	modelProvider, ok := enginePlugin.(plugin.EngineCatalogModelProvider)
	if !ok {
		end()
		return nil, fmt.Errorf("%w: native reader has no catalog model", ErrRequired)
	}
	if err := g.store.RequireCatalogPathUnmanaged(ctx, int64(tenantID), modelProvider.EngineCatalogModel(), path, time.Now().UTC()); err != nil {
		end()
		return nil, fmt.Errorf("%w: %w", ErrRequired, err)
	}
	return end, nil
}

// BeginUnresolvedRead is the fail-closed boundary for legacy graph and
// federated reads whose current Provider contract cannot prove a complete
// DataItem read set. It is intentionally not used by ordinary SQL/MQL paths.
func (g *Gate) BeginUnresolvedRead(ctx context.Context, tenantID uint) (func(), error) {
	end, err := g.begin(tenantID)
	if err != nil {
		return nil, err
	}
	if err := g.store.EnsureCurrent(ctx, int64(tenantID)); err != nil {
		end()
		return nil, fmt.Errorf("%w: %w", ErrRequired, err)
	}
	if g.store.HasManagedTargets(int64(tenantID)) {
		end()
		return nil, fmt.Errorf("%w: protected resource read set is unresolved", ErrRequired)
	}
	return end, nil
}

func (g *Gate) begin(tenantID uint) (func(), error) {
	if g == nil || g.store == nil || tenantID == 0 {
		return nil, ErrRequired
	}
	tenant := int64(tenantID)
	g.mu.Lock()
	g.active[tenant]++
	g.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			g.mu.Lock()
			g.active[tenant]--
			if g.active[tenant] <= 0 {
				delete(g.active, tenant)
			}
			g.mu.Unlock()
		})
	}, nil
}

func (g *Gate) HasActiveExecutionsForTenant(tenantID int64) bool {
	if g == nil || tenantID <= 0 {
		return false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.active[tenantID] > 0
}
