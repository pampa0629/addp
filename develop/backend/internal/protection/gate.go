package protection

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/addp/common/dataprotection"
	"github.com/addp/common/dataprotection/projectionstore"
	"github.com/addp/common/engine/plugin"
)

var (
	ErrRequired       = errors.New("develop data protection gate is required")
	ErrResultRequired = fmt.Errorf("%w: query result protection failed", ErrRequired)
)

type Gate struct {
	store *projectionstore.Store
	reads projectionstore.InflightReads
}

func NewGate(store *projectionstore.Store) *Gate {
	return &Gate{store: store}
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
	subject, _ := ctx.Value(interactiveQuerySubjectKey{}).(dataprotection.SubjectReference)
	protect, err := g.store.PrepareQueryProtection(ctx, int64(tenantID), modelProvider.EngineCatalogModel(), prepared, "query", subject, time.Now().UTC())
	if err != nil {
		end()
		return nil, nil, fmt.Errorf("%w: %w", ErrRequired, err)
	}
	return func(result *plugin.QueryResult) error {
		if capture, ok := ctx.Value(queryCaptureKey{}).(*QueryCapture); ok && protect.CaptureAuthorized != nil {
			snapshot, err := protect.CaptureAuthorized(result, capture.Subject)
			if err != nil {
				return fmt.Errorf("%w: %w", ErrResultRequired, err)
			}
			capture.Result = snapshot
		}
		if err := protect.Apply(result); err != nil {
			if capture, ok := ctx.Value(queryCaptureKey{}).(*QueryCapture); ok && capture.Result != nil {
				// An authorized result may have a deny fallback. Retain no default
				// rows in that case; every real read still rechecks the ephemeral result.
				result.Columns = []string{}
				result.Rows = []map[string]interface{}{}
				return nil
			}
			return fmt.Errorf("%w: %w", ErrResultRequired, err)
		}
		return nil
	}, end, nil
}

func (g *Gate) BeginPreparedGraphQuery(ctx context.Context, tenantID uint, enginePlugin plugin.EnginePlugin, prepared plugin.PreparedGraphQuery) (func(), error) {
	end, err := g.begin(tenantID)
	if err != nil {
		return nil, err
	}
	model, ok := enginePlugin.(plugin.EngineCatalogModelProvider)
	if !ok {
		end()
		return nil, ErrRequired
	}
	if err := g.store.RequireReadPlanUnmanaged(ctx, int64(tenantID), model.EngineCatalogModel(), prepared, time.Now().UTC()); err != nil {
		end()
		return nil, fmt.Errorf("%w: %w", ErrRequired, err)
	}
	return end, nil
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

// BeginUnresolvedRead is the fail-closed boundary for federated reads whose current Provider contract cannot prove a complete
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
	return g.reads.Begin(int64(tenantID))
}

func (g *Gate) HasActiveExecutionsForTenant(tenantID int64) bool {
	if g == nil || tenantID <= 0 {
		return false
	}
	return g.reads.HasActiveExecutionsForTenant(tenantID)
}
