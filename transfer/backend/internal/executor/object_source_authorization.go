package executor

import (
	"context"
	"io"
	"reflect"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/resume"
)

type ObjectSourceAuthorizer func(context.Context, plugin.EngineCatalogPath) error

// exactObjectContentReader places the current source decision at every physical Open.
type exactObjectContentReader struct {
	plugin.ContentReadableProvider
	path      plugin.EngineCatalogPath
	authorize ObjectSourceAuthorizer
}

func (r exactObjectContentReader) OpenContent(ctx context.Context, conn plugin.ConnectionInfo, path plugin.EngineCatalogPath, options plugin.ReadOptions) (io.ReadCloser, error) {
	if !reflect.DeepEqual(path, r.path) {
		return nil, ErrProtectionPreparation
	}
	if err := r.authorize(ctx, path); err != nil {
		return nil, err
	}
	return r.ContentReadableProvider.OpenContent(ctx, conn, path, options)
}

type exactObjectBatchSource struct {
	source    TableBatchSource
	path      plugin.EngineCatalogPath
	authorize ObjectSourceAuthorizer
}

func (s exactObjectBatchSource) Open(ctx context.Context) (TableBatchReader, error) {
	if err := s.authorize(ctx, s.path); err != nil {
		return nil, err
	}
	r, err := s.source.Open(ctx)
	if err != nil {
		return nil, err
	}
	return &exactObjectBatchReader{reader: r, path: s.path, authorize: s.authorize}, nil
}

type exactObjectBatchReader struct {
	reader    TableBatchReader
	path      plugin.EngineCatalogPath
	authorize ObjectSourceAuthorizer
}

func (r *exactObjectBatchReader) TableInfo() *datatype.TableInfo     { return r.reader.TableInfo() }
func (r *exactObjectBatchReader) SpatialInfo() *datatype.SpatialInfo { return r.reader.SpatialInfo() }
func (r *exactObjectBatchReader) Close(ctx context.Context) error    { return r.reader.Close(ctx) }
func (r *exactObjectBatchReader) ResumeMarker() *resume.Marker       { return r.reader.ResumeMarker() }
func (r *exactObjectBatchReader) ReadBatch(ctx context.Context, limit int) (*plugin.BatchData, error) {
	if err := r.authorize(ctx, r.path); err != nil {
		return nil, err
	}
	return r.reader.ReadBatch(ctx, limit)
}
func (r *exactObjectBatchReader) ReadSet(context.Context) (*plugin.QueryReadSet, error) {
	return plugin.NewQueryReadSet(r.path)
}
