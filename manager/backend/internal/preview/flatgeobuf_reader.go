package preview

import (
	"context"
	"errors"
	"fmt"
	"io"
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
	providerReq, err := r.buildProviderRequest(req)
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
	plug, err := plugin.Get(req.Engine.EngineType)
	if err != nil {
		return nil, fmt.Errorf("unsupported engine type: %s", req.Engine.EngineType)
	}
	sessionProvider, ok := plug.(plugin.TableReadSessionProvider)
	if !ok {
		return nil, fmt.Errorf("engine %s does not implement TableReadSessionProvider", req.Engine.EngineType)
	}

	metaSpatial := spatialInfoFromMetaAttributes(req.Attributes)
	geometryColumn := resolveFlatGeobufGeometryColumn(requestedGeometryColumn, metaSpatial, fieldsFromMetaAttributes(req.Attributes))
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
		geometryColumn = resolveFlatGeobufGeometryColumn(requestedGeometryColumn, spatialInfo, fields)
	}
	if geometryColumn == "" {
		_ = session.Close(ctx)
		return nil, ErrFlatGeobufGeometryColumnRequired
	}

	reader := &flatGeobufSessionFeatureReader{
		session:        session,
		bufferedRows:   bufferedRows,
		geometryColumn: geometryColumn,
		columns:        flatGeobufColumnsFromFields(fields, geometryColumn),
		srid:           flatGeobufSourceSRID(spatialInfo, geometryColumn),
		rowLimit:       rowLimit,
		readRows:       len(bufferedRows),
	}
	return &FlatGeobufFeatureReaderResult{
		Reader:  reader,
		Options: flatGeobufOptions("quick_view", reader.columns, spatialInfo, geometryColumn),
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
	geometryColumn := resolveFlatGeobufGeometryColumn(requestedGeometryColumn, spatialInfo, fields)
	if geometryColumn == "" {
		_ = closeFn(context.Background())
		return nil, ErrFlatGeobufGeometryColumnRequired
	}
	columns := flatGeobufColumnsFromFields(fields, geometryColumn)
	featureReader := &flatGeobufTableFeatureReader{
		reader:         reader,
		geometryColumn: geometryColumn,
		columns:        columns,
		srid:           flatGeobufSourceSRID(spatialInfo, geometryColumn),
		rowLimit:       rowLimit,
	}
	return &FlatGeobufFeatureReaderResult{
		Reader:  featureReader,
		Options: flatGeobufOptions("quick_view", columns, spatialInfo, geometryColumn),
		Close:   closeFn,
	}, nil
}

type flatGeobufTableFeatureReader struct {
	reader         format.TableReader
	bufferedRows   []map[string]interface{}
	geometryColumn string
	columns        []commonSpatial.FlatGeobufColumn
	srid           int
	rowLimit       int
	readRows       int
}

func (r *flatGeobufTableFeatureReader) NextFlatGeobufFeature(ctx context.Context) (*commonSpatial.FlatGeobufFeature, error) {
	for {
		if len(r.bufferedRows) == 0 {
			if r.rowLimit > 0 && r.readRows >= r.rowLimit {
				return nil, io.EOF
			}
			limit := flatGeobufReadBatchSize
			if r.rowLimit > 0 && r.readRows+limit > r.rowLimit {
				limit = r.rowLimit - r.readRows
			}
			rows, err := r.reader.ReadRows(ctx, limit)
			if err != nil {
				return nil, err
			}
			if len(rows) == 0 {
				return nil, io.EOF
			}
			r.readRows += len(rows)
			r.bufferedRows = rows
		}
		row := r.bufferedRows[0]
		r.bufferedRows = r.bufferedRows[1:]
		if feature := flatGeobufFeatureFromRow(row, r.geometryColumn, r.columns, r.srid); feature != nil {
			return feature, nil
		}
	}
}

type flatGeobufSessionFeatureReader struct {
	session        plugin.TableReadSession
	bufferedRows   []map[string]interface{}
	geometryColumn string
	columns        []commonSpatial.FlatGeobufColumn
	srid           int
	rowLimit       int
	readRows       int
}

func (r *flatGeobufSessionFeatureReader) NextFlatGeobufFeature(ctx context.Context) (*commonSpatial.FlatGeobufFeature, error) {
	for {
		if len(r.bufferedRows) == 0 {
			if r.rowLimit > 0 && r.readRows >= r.rowLimit {
				return nil, io.EOF
			}
			limit := flatGeobufReadBatchSize
			if r.rowLimit > 0 && r.readRows+limit > r.rowLimit {
				limit = r.rowLimit - r.readRows
			}
			batch, err := r.session.ReadBatch(ctx, limit)
			if err != nil {
				return nil, err
			}
			if batch == nil || len(batch.Rows) == 0 {
				return nil, io.EOF
			}
			r.readRows += len(batch.Rows)
			r.bufferedRows = batch.Rows
		}
		row := r.bufferedRows[0]
		r.bufferedRows = r.bufferedRows[1:]
		if feature := flatGeobufFeatureFromRow(row, r.geometryColumn, r.columns, r.srid); feature != nil {
			return feature, nil
		}
	}
}

func flatGeobufFeatureFromRow(row map[string]interface{}, geometryColumn string, columns []commonSpatial.FlatGeobufColumn, srid int) *commonSpatial.FlatGeobufFeature {
	if len(row) == 0 {
		return nil
	}
	geometry, ok := row[geometryColumn]
	if !ok || geometry == nil {
		for key, value := range row {
			if strings.EqualFold(strings.TrimSpace(key), geometryColumn) {
				geometry = value
				ok = true
				break
			}
		}
	}
	if !ok || geometry == nil {
		return nil
	}
	properties := make(map[string]interface{}, len(columns))
	for _, column := range columns {
		if value, ok := row[column.Name]; ok {
			properties[column.Name] = value
		}
	}
	return &commonSpatial.FlatGeobufFeature{
		Geometry:         geometry,
		GeometryEncoding: string(commonSpatial.GeometryEncodingEWKB),
		GeometrySRID:     srid,
		Properties:       properties,
	}
}

func flatGeobufOptions(name string, columns []commonSpatial.FlatGeobufColumn, spatialInfo *datatype.SpatialInfo, geometryColumn string) commonSpatial.FlatGeobufOptions {
	srid := flatGeobufSourceSRID(spatialInfo, geometryColumn)
	crsRef := strings.TrimSpace(flatGeobufSourceCRS(spatialInfo, geometryColumn))
	crsDefinition := flatGeobufCRSDefinition(spatialInfo, crsRef)
	opts := commonSpatial.FlatGeobufOptions{
		Name:            name,
		SRID:            srid,
		CRSName:         crsRef,
		Columns:         columns,
		GeometryType:    flatGeobufGeometryType(spatialInfo, geometryColumn),
		DefaultEncoding: string(commonSpatial.GeometryEncodingEWKB),
	}
	if crsDefinition != nil {
		opts.CRSWKT = crsDefinition.Definition
	}
	return opts
}

func resolveFlatGeobufGeometryColumn(requested string, spatialInfo *datatype.SpatialInfo, fields []datatype.FieldInfo) string {
	if value := strings.TrimSpace(requested); value != "" {
		return value
	}
	if spatialInfo != nil {
		if value := strings.TrimSpace(spatialInfo.PrimaryGeometryName()); value != "" {
			return value
		}
		for _, value := range spatialInfo.GeometryColumnNames() {
			if value = strings.TrimSpace(value); value != "" {
				return value
			}
		}
	}
	for _, field := range fields {
		if datatype.IsSpatialFieldType(field.Type) && strings.TrimSpace(field.Name) != "" {
			return strings.TrimSpace(field.Name)
		}
	}
	return ""
}

func fieldsFromMetaAttributes(attrs map[string]interface{}) []datatype.FieldInfo {
	tableInfo := tableInfoFromMetaAttributes(attrs, "table")
	if tableInfo == nil {
		return nil
	}
	return tableInfo.Fields
}

func flatGeobufColumnsFromFields(fields []datatype.FieldInfo, geometryColumn string) []commonSpatial.FlatGeobufColumn {
	columns := make([]commonSpatial.FlatGeobufColumn, 0, len(fields))
	for _, field := range fields {
		name := strings.TrimSpace(field.Name)
		if name == "" || strings.EqualFold(name, geometryColumn) || datatype.IsSpatialFieldType(field.Type) {
			continue
		}
		columns = append(columns, commonSpatial.FlatGeobufColumn{
			Name: name,
			Type: flatGeobufPropertyTypeFromField(field),
		})
	}
	return columns
}

func flatGeobufPropertyTypeFromField(field datatype.FieldInfo) commonSpatial.FlatGeobufPropertyType {
	switch field.Type {
	case datatype.FieldTypeBool:
		return commonSpatial.FlatGeobufPropertyBool
	case datatype.FieldTypeInt, datatype.FieldTypeBigInt:
		return commonSpatial.FlatGeobufPropertyInt64
	case datatype.FieldTypeFloat, datatype.FieldTypeDouble, datatype.FieldTypeDecimal:
		return commonSpatial.FlatGeobufPropertyFloat64
	case datatype.FieldTypeBytes:
		return commonSpatial.FlatGeobufPropertyBinary
	case datatype.FieldTypeJSON, datatype.FieldTypeArray, datatype.FieldTypeMixed:
		return commonSpatial.FlatGeobufPropertyJSON
	default:
		return commonSpatial.FlatGeobufPropertyString
	}
}

func flatGeobufSourceSRID(spatialInfo *datatype.SpatialInfo, geometryColumn string) int {
	if spatialInfo == nil {
		return 0
	}
	for _, column := range spatialInfo.GeometryColumns {
		if strings.EqualFold(strings.TrimSpace(column.Name), geometryColumn) && column.SRID != nil {
			return *column.SRID
		}
	}
	return spatialInfo.PrimarySRIDValue()
}

func flatGeobufSourceCRS(spatialInfo *datatype.SpatialInfo, geometryColumn string) string {
	if spatialInfo == nil {
		return ""
	}
	for _, column := range spatialInfo.GeometryColumns {
		if strings.EqualFold(strings.TrimSpace(column.Name), geometryColumn) {
			if value := strings.TrimSpace(column.CRSRef); value != "" {
				return value
			}
			if column.SRID != nil && *column.SRID > 0 {
				return datatype.EPSGCRSRef(*column.SRID)
			}
		}
	}
	return spatialInfo.PrimaryCRSRef()
}

func flatGeobufGeometryType(spatialInfo *datatype.SpatialInfo, geometryColumn string) string {
	if spatialInfo == nil {
		return ""
	}
	for _, column := range spatialInfo.GeometryColumns {
		if strings.EqualFold(strings.TrimSpace(column.Name), geometryColumn) {
			return column.GeometryType
		}
	}
	return spatialInfo.PrimaryGeometryType()
}

func flatGeobufCRSDefinition(spatialInfo *datatype.SpatialInfo, crsRef string) *datatype.CRSDefinition {
	if spatialInfo == nil {
		return nil
	}
	if value := strings.TrimSpace(crsRef); value != "" {
		if definition := spatialInfo.CRSDefinitionByID(value); definition != nil {
			return definition
		}
	}
	if primary := spatialInfo.PrimaryCRSRef(); primary != "" {
		return spatialInfo.CRSDefinitionByID(primary)
	}
	return nil
}
