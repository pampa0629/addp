package service

import (
	"context"
	"errors"
	"io"
	"reflect"

	"github.com/addp/common/engine/plugin"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/manager/internal/engineaccess"
)

// Source I/O for the already frozen raster composition. Every actual source
// operation rechecks current execution authority and engine availability.
type rasterSourceReader struct {
	auth             *rasterCOGSourceAuthorization
	tenant, engineID uint
	engineType       string
	facts            plugin.EngineCatalogFactsProvider
	readable         plugin.ContentReadableProvider
}

func newRasterSourceReader(ctx context.Context, auth *rasterCOGSourceAuthorization, tenant, engineID uint) (*rasterSourceReader, error) {
	composition := auth.Composition()
	primary, err := composition.PrimaryTarget()
	if err != nil {
		return nil, err
	}
	engine, err := auth.BeforeSourceRead(ctx, primary)
	if err != nil {
		return nil, err
	}
	if engine.ID != engineID || !commonExecution.ManagerFileSourceComposition(engine.EngineType, composition) {
		return nil, ErrFileSourceAuthorizationRequired
	}
	registered, err := plugin.Get(engine.EngineType)
	if err != nil {
		return nil, err
	}
	facts, hasFacts := registered.(plugin.EngineCatalogFactsProvider)
	readable, hasContent := registered.(plugin.ContentReadableProvider)
	if !hasFacts || !hasContent {
		return nil, ErrFileSourceAuthorizationUnavailable
	}
	return &rasterSourceReader{auth: auth, tenant: tenant, engineID: engineID, engineType: engine.EngineType, facts: facts, readable: readable}, nil
}

func (r *rasterSourceReader) current(ctx context.Context, target plugin.EngineCatalogPath) (plugin.ConnectionInfo, error) {
	engine, err := r.auth.BeforeSourceRead(ctx, target)
	if err != nil {
		return nil, err
	}
	if engine == nil || engine.ID != r.engineID || engine.EngineType != r.engineType || engine.TenantID == nil || *engine.TenantID != r.tenant || engineaccess.EnsureAvailable(engine) != nil {
		return nil, ErrFileSourceAuthorizationRequired
	}
	return plugin.ConnectionInfo(engine.ConnectionInfo), nil
}

func (r *rasterSourceReader) stat(ctx context.Context, target plugin.EngineCatalogPath) (int64, error) {
	info, err := r.current(ctx, target)
	if err != nil {
		return 0, err
	}
	observed, err := r.facts.DescribeEngineCatalogFacts(ctx, info, target, plugin.EngineCatalogFactsOptions{})
	if err != nil {
		return 0, err
	}
	if observed == nil || observed.Kind != target.Segments[len(target.Segments)-1].Kind || !reflect.DeepEqual(observed.Path, target) || observed.Storage == nil || observed.Storage.SizeBytes == nil || observed.Storage.Path != target.StringPath() || *observed.Storage.SizeBytes < 0 {
		return 0, ErrFileSourceAuthorizationRequired
	}
	return *observed.Storage.SizeBytes, nil
}

func (r *rasterSourceReader) open(ctx context.Context, target plugin.EngineCatalogPath) (io.ReadCloser, error) {
	info, err := r.current(ctx, target)
	if err != nil {
		return nil, err
	}
	stream, err := r.readable.OpenContent(ctx, info, target, plugin.ReadOptions{})
	if err != nil {
		if stream != nil {
			err = errors.Join(err, stream.Close())
		}
		return nil, err
	}
	if stream == nil {
		return nil, ErrFileSourceAuthorizationRequired
	}
	if err := r.auth.Checkpoint(ctx); err != nil {
		return nil, errors.Join(err, stream.Close())
	}
	return stream, nil
}
