package preview

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/addp/common/contentio"
	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	commonSpatial "github.com/addp/common/spatial"
)

const flatGeobufReadBatchSize = 512

var ErrFlatGeobufGeometryColumnRequired = errors.New("geometry column is required for direct FlatGeobuf quick view")

type FlatGeobufFeatureReaderResult struct {
	Reader  commonSpatial.FlatGeobufFeatureReader
	Options commonSpatial.FlatGeobufOptions
	Close   func(context.Context) error
}

func (r *PreviewResolver) OpenFlatGeobufFeatureReaderFromURI(
	ctx context.Context,
	locatorURI string,
	geometryColumn string,
	rowLimit int,
	tenantID *uint,
) (*FlatGeobufFeatureReaderResult, error) {
	req, err := r.ResolveRequestFromURIWithSelection(ctx, locatorURI, 1, rowLimit, "", "", "", plugin.GraphSampleFilter{}, tenantID)
	if err != nil {
		return nil, err
	}
	if req.Metadata == nil {
		return nil, ErrPreviewRequiresScannedMeta
	}
	providerReq, err := r.buildProviderRequest(ctx, req)
	if err != nil {
		return nil, err
	}
	if rowLimit <= 0 {
		rowLimit = 1000
	}

	itemType := strings.ToLower(strings.TrimSpace(providerReq.ItemType))
	if isContentFileItemType(itemType) {
		return openFileFlatGeobufFeatureReader(ctx, providerReq, geometryColumn, rowLimit)
	}
	switch itemType {
	case "table", "view", "materialized_view":
		return openDatabaseFlatGeobufFeatureReader(ctx, providerReq, geometryColumn, rowLimit)
	default:
		return nil, fmt.Errorf("item type %s does not support direct FlatGeobuf quick view", providerReq.ItemType)
	}
}

func openFileFlatGeobufFeatureReader(ctx context.Context, req *PreviewRequest, requestedGeometryColumn string, rowLimit int) (*FlatGeobufFeatureReaderResult, error) {
	provider := &FileTablePreviewProvider{}
	contentCtx, err := provider.contentContextForPreview(req)
	if err != nil {
		return nil, err
	}
	formatType := provider.resolveFormat(req)
	fullPath := contentCtx.path
	contentReader := contentCtx.reader
	opts := provider.buildParseOptions(formatType, req)
	opts.GeometryEncoding = format.GeometryEncodingEWKB

	if strings.TrimSpace(req.ChildName) != "" {
		resolved, err := provider.resolveContainerChild(ctx, contentReader, fullPath, formatType, req)
		if err != nil {
			return nil, err
		}
		contentReader = resolved.Reader
		fullPath = resolved.Ref.Path
		formatType = resolved.Format
		if resolved.ParentOptions != nil {
			opts = resolved.ParentOptions
		} else {
			opts = provider.buildParseOptions(formatType, req)
		}
		opts.GeometryEncoding = format.GeometryEncodingEWKB
	}

	if multiReaderProvider, err := format.GetMultiTableReaderProvider(formatType); err == nil {
		refs := refsForPreview(fullPath, formatType, req.Attributes)
		reader, err := multiReaderProvider.OpenMultiTableReader(ctx, contentReader, refs, opts)
		if err != nil {
			return nil, err
		}
		return flatGeobufResultFromTableReader(reader, requestedGeometryColumn, rowLimit, req.Attributes, func(closeCtx context.Context) error {
			return reader.Close(closeCtx)
		})
	}

	readerProvider, err := format.GetTableReaderProvider(formatType)
	if err != nil {
		return nil, fmt.Errorf("format %s does not support direct table reading: %w", formatType, err)
	}
	object, err := contentReader.Open(ctx, contentio.NewRef(fullPath, contentio.RoleMain))
	if err != nil {
		return nil, fmt.Errorf("failed to open content for FlatGeobuf quick view: %w", err)
	}
	reader, err := readerProvider.OpenTableReader(ctx, object, opts)
	if err != nil {
		_ = object.Close()
		return nil, err
	}
	return flatGeobufResultFromTableReader(reader, requestedGeometryColumn, rowLimit, req.Attributes, func(closeCtx context.Context) error {
		err := reader.Close(closeCtx)
		if closeErr := object.Close(); err == nil {
			err = closeErr
		}
		return err
	})
}

func openDatabaseFlatGeobufFeatureReader(ctx context.Context, req *PreviewRequest, requestedGeometryColumn string, rowLimit int) (*FlatGeobufFeatureReaderResult, error) {
	plug := req.EnginePlugin
	if plug == nil {
		return nil, fmt.Errorf("engine provider is not resolved for %s", req.Engine.EngineType)
	}
	sessionProvider, ok := plug.(plugin.TableReadSessionProvider)
	if !ok {
		return nil, fmt.Errorf("engine %s does not implement TableReadSessionProvider", req.Engine.EngineType)
	}

	metaSpatial := spatialInfoFromMetaAttributes(req.Attributes)
	geometryColumn := commonSpatial.ResolveFlatGeobufGeometryColumn(requestedGeometryColumn, metaSpatial, fieldsFromMetaAttributes(req.Attributes))
	if geometryColumn == "" {
		return nil, ErrFlatGeobufGeometryColumnRequired
	}

	session, err := sessionProvider.OpenTableReadSession(ctx, plugin.ConnectionInfo(req.Engine.ConnectionInfo), req.ProviderPath, plugin.TableReadSessionOptions{
		Hints: map[string]interface{}{
			plugin.TableReadHintGeometryEncoding: string(format.GeometryEncodingEWKB),
			plugin.TableReadHintGeometryField:    geometryColumn,
		},
	})
	if err != nil {
		return nil, err
	}

	prefetchLimit := rowLimit
	if prefetchLimit > flatGeobufReadBatchSize {
		prefetchLimit = flatGeobufReadBatchSize
	}
	firstBatch, err := session.ReadBatch(ctx, prefetchLimit)
	if err != nil {
		_ = session.Close(ctx)
		return nil, err
	}

	spatialInfo := metaSpatial
	fields := fieldsFromMetaAttributes(req.Attributes)
	bufferedRows := []map[string]interface{}{}
	if firstBatch != nil {
		bufferedRows = firstBatch.Rows
		if len(firstBatch.Fields) > 0 {
			fields = firstBatch.Fields
		}
		if firstBatch.Spatial != nil {
			spatialInfo = firstBatch.Spatial
		}
	}
	if geometryColumn == "" {
		geometryColumn = commonSpatial.ResolveFlatGeobufGeometryColumn(requestedGeometryColumn, spatialInfo, fields)
	}
	if geometryColumn == "" {
		_ = session.Close(ctx)
		return nil, ErrFlatGeobufGeometryColumnRequired
	}

	reader, options := commonSpatial.NewFlatGeobufBatchFeatureReader(
		func(readCtx context.Context, limit int) ([]map[string]interface{}, error) {
			batch, err := session.ReadBatch(readCtx, limit)
			if err != nil || batch == nil {
				return nil, err
			}
			return batch.Rows, nil
		},
		bufferedRows, geometryColumn, fields, spatialInfo, rowLimit,
	)
	return &FlatGeobufFeatureReaderResult{
		Reader:  reader,
		Options: options,
		Close: func(closeCtx context.Context) error {
			return session.Close(closeCtx)
		},
	}, nil
}

func flatGeobufResultFromTableReader(reader format.TableReader, requestedGeometryColumn string, rowLimit int, attrs map[string]interface{}, closeFn func(context.Context) error) (*FlatGeobufFeatureReaderResult, error) {
	spatialInfo := spatialInfoFromMetaAttributes(attrs)
	if spatialProvider, ok := reader.(format.TableSpatialInfoProvider); ok && spatialProvider.SpatialInfo() != nil {
		spatialInfo = spatialProvider.SpatialInfo()
	}
	fields := reader.Fields()
	if len(fields) == 0 {
		fields = fieldsFromMetaAttributes(attrs)
	}
	geometryColumn := commonSpatial.ResolveFlatGeobufGeometryColumn(requestedGeometryColumn, spatialInfo, fields)
	if geometryColumn == "" {
		_ = closeFn(context.Background())
		return nil, ErrFlatGeobufGeometryColumnRequired
	}
	featureReader, options := commonSpatial.NewFlatGeobufBatchFeatureReader(
		reader.ReadRows, nil, geometryColumn, fields, spatialInfo, rowLimit,
	)
	return &FlatGeobufFeatureReaderResult{
		Reader:  featureReader,
		Options: options,
		Close:   closeFn,
	}, nil
}

func fieldsFromMetaAttributes(attrs map[string]interface{}) []datatype.FieldInfo {
	tableInfo := tableInfoFromMetaAttributes(attrs, "table")
	if tableInfo == nil {
		return nil
	}
	return tableInfo.Fields
}
