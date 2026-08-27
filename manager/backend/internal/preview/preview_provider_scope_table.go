package preview

import (
	"context"
	"fmt"
	"strings"

	"github.com/addp/common/contentio"
	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	"github.com/addp/manager/internal/models"
)

// ScopeTablePreviewProvider 目录型表格预览 Provider。
// 当前服务 layout=whole 且实现 scope table provider 的表格资源。
type ScopeTablePreviewProvider struct{}

func NewScopeTablePreviewProvider() PreviewProvider {
	return &ScopeTablePreviewProvider{}
}

func (p *ScopeTablePreviewProvider) Name() string {
	return "builtin:scope-table"
}

func (p *ScopeTablePreviewProvider) Preview(ctx context.Context, req *PreviewRequest) (*models.TablePreview, error) {
	if req == nil || req.Engine == nil {
		return nil, fmt.Errorf("engine is required")
	}
	page, pageSize := scopeTablePreviewPagination(req)
	formatType := resolveScopeTableFormat(req)
	if req.ScopeTableReaderProvider != nil {
		return previewRuntimeScopeTable(ctx, req, formatType, page, pageSize)
	}

	plug, err := plugin.Get(req.Engine.EngineType)
	if err != nil {
		return nil, fmt.Errorf("unsupported engine type: %s", req.Engine.EngineType)
	}

	contentReader, _ := plug.(plugin.ContentReadableProvider)
	catalogProvider, _ := plug.(plugin.EngineCatalogProvider)

	connInfo := plugin.ConnectionInfo(req.Engine.ConnectionInfo)
	reader, err := scopeTableContentReader(req, contentReader, catalogProvider, connInfo)
	if err != nil {
		return nil, err
	}

	offset := int64((page - 1) * pageSize)
	limit := int64(pageSize)

	scopeSampleReader, err := format.GetScopeTableSampleReader(formatType)
	if err != nil {
		return nil, fmt.Errorf("no scope table sample reader for %s: %w", formatType, err)
	}

	var tableInfo *datatype.TableInfo
	var rows []map[string]interface{}

	dirPath := req.ScopePath
	if dirPath == "" {
		dirPath = nfsPhysicalPath(req.Schema, req.Table)
	}
	if catalogProvider == nil || contentReader == nil {
		err = fmt.Errorf("engine %s does not implement EngineCatalogProvider and ContentReadableProvider", req.Engine.EngineType)
	} else {
		scope := contentio.NewRef(dirPath, contentio.RoleScope)
		tableInfo = tableInfoFromMetaAttributes(req.Attributes, "table")
		sampleOptions := scopeTableSampleOptionsFromMetaAttributes(req.Attributes)
		if tableInfo == nil {
			scopeInfoProvider, infoErr := format.GetScopeTableInfoProvider(formatType)
			if infoErr != nil {
				err = fmt.Errorf("no scope table info provider for %s: %w", formatType, infoErr)
			} else {
				result, describeErr := scopeInfoProvider.DescribeTableScope(ctx, reader, scope, nil)
				if describeErr == nil {
					tableInfo = format.TableInfoFromDescribeResult(result)
				}
				err = describeErr
			}
		}
		if err == nil {
			rows, err = scopeSampleReader.SampleTableScope(ctx, reader, scope, offset, limit, sampleOptions)
		}
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read scope table preview: %w", err)
	}

	// 构建列名和列元数据
	fields := tableInfo.Fields
	total := int64(len(rows))
	if tableInfo.RowCount != nil {
		total = *tableInfo.RowCount
	}
	columns := make([]string, 0, len(fields))
	columnMeta := make([]models.ColumnMetadata, 0, len(fields))
	for _, f := range fields {
		columns = append(columns, f.Name)
		columnMeta = append(columnMeta, models.ColumnMetadata{
			ColumnName: f.Name,
			Type:       string(f.Type),
			IsNullable: f.Nullable,
		})
	}

	return &models.TablePreview{
		Mode:           "table",
		Columns:        columns,
		Fields:         append([]datatype.FieldInfo(nil), fields...),
		ColumnMetadata: columnMeta,
		Rows:           rows,
		Page:           page,
		PageSize:       pageSize,
		Total:          int(total),
	}, nil
}

func scopeTablePreviewPagination(req *PreviewRequest) (int, int) {
	page := req.Page
	if page < 1 {
		page = 1
	}
	pageSize := req.PageSize
	if pageSize <= 0 {
		pageSize = 20
	}
	return page, pageSize
}

func previewRuntimeScopeTable(ctx context.Context, req *PreviewRequest, formatType format.FormatType, page, pageSize int) (*models.TablePreview, error) {
	child := containerChildInputForRequest(req.Attributes, req.ChildName)
	options := format.ChildTableParseOptions(req.ChildName, child)
	options.GeometryEncoding = format.GeometryEncodingEWKB
	scopePath := strings.TrimSpace(req.ScopePath)
	if scopePath == "" {
		scopePath = strings.Trim(nfsPhysicalPath(req.Schema, req.Table), "/")
	}
	reader, err := req.ScopeTableReaderProvider.OpenTableScopeReader(ctx, nil, contentio.NewRef(scopePath, contentio.RoleScope), options)
	if err != nil {
		return nil, fmt.Errorf("open runtime scope table preview for %s child %s: %w", formatType, req.ChildName, err)
	}
	fields := reader.Fields()
	spatialInfo := spatialInfoFromMetaAttributes(req.Attributes)
	if provider, ok := reader.(format.TableSpatialInfoProvider); ok && provider.SpatialInfo() != nil {
		spatialInfo = provider.SpatialInfo()
	}
	rows, err := readRuntimeScopeTableWindow(ctx, reader, (page-1)*pageSize, pageSize)
	if err != nil {
		_ = reader.Close(ctx)
		return nil, fmt.Errorf("read runtime scope table preview: %w", err)
	}
	if err := reader.Close(ctx); err != nil {
		return nil, fmt.Errorf("close runtime scope table preview: %w", err)
	}

	tableInfo := &datatype.TableInfo{Name: req.ChildName, Fields: fields}
	geometryColumns := (&FileTablePreviewProvider{}).detectGeometryColumns(tableInfo)
	srid := 0
	sourceCRS := ""
	var sourceCRSDefinition *datatype.CRSDefinition
	if spatialInfo != nil {
		srid = spatialInfo.PrimarySRIDValue()
		sourceCRS = spatialInfo.PrimaryCRSRef()
		sourceCRSDefinition = spatialInfo.CRSDefinitionByID(sourceCRS)
	}
	spatialContract := tablePreviewSpatialCRSContract(geometryColumns, srid, sourceCRS, sourceCRSDefinition)
	columns := make([]string, 0, len(fields))
	columnMeta := make([]models.ColumnMetadata, 0, len(fields))
	for _, field := range fields {
		columns = append(columns, field.Name)
		columnMeta = append(columnMeta, models.ColumnMetadata{ColumnName: field.Name, Type: string(field.Type), IsNullable: field.Nullable})
	}
	total := containerChildRowCount(child)
	if total == 0 {
		total = int64(len(rows))
	}
	return &models.TablePreview{
		Mode: PreviewModeTable, Columns: columns, Fields: append([]datatype.FieldInfo(nil), fields...), ColumnMetadata: columnMeta,
		Rows: rows, Total: int(total), Page: page, PageSize: pageSize,
		GeometryColumns: geometryColumns, GeometryColumn: spatialContract.GeometryColumn,
		SourceSRID: spatialContract.SourceSRID, SourceCRS: spatialContract.SourceCRS,
		SourceCRSDefinition: spatialContract.SourceCRSDefinition, TransformStatus: spatialContract.TransformStatus,
		PreviewHint: spatialContract.PreviewHint, SRID: srid, Extent: tablePreviewSpatialExtent(spatialInfo),
	}, nil
}

func readRuntimeScopeTableWindow(ctx context.Context, reader format.TableReader, offset, limit int) ([]map[string]interface{}, error) {
	for offset > 0 {
		batchSize := offset
		if batchSize > 1000 {
			batchSize = 1000
		}
		rows, err := reader.ReadRows(ctx, batchSize)
		if err != nil {
			return nil, err
		}
		if len(rows) == 0 {
			return []map[string]interface{}{}, nil
		}
		offset -= len(rows)
	}
	return reader.ReadRows(ctx, limit)
}

func containerChildRowCount(child map[string]interface{}) int64 {
	for _, key := range []string{"row_count", "estimated_row_count"} {
		switch value := child[key].(type) {
		case int64:
			return value
		case int:
			return int64(value)
		case float64:
			return int64(value)
		}
	}
	return 0
}

func scopeTableSampleOptionsFromMetaAttributes(attrs map[string]interface{}) *format.ParseOptions {
	formatType := formatTypeFromMetaAttributes(attrs)
	if formatType == format.FormatUnknown {
		return nil
	}
	plugin, err := format.GetFormatPlugin(formatType)
	if err == nil {
		if optionsProvider, ok := plugin.(interface {
			SampleOptionsFromAttributes(map[string]interface{}) *format.ParseOptions
		}); ok {
			return optionsProvider.SampleOptionsFromAttributes(attrs)
		}
	}
	infoProvider, err := format.GetScopeTableInfoProvider(formatType)
	if err != nil {
		return nil
	}
	optionsProvider, ok := infoProvider.(interface {
		SampleOptionsFromAttributes(map[string]interface{}) *format.ParseOptions
	})
	if !ok {
		return nil
	}
	return optionsProvider.SampleOptionsFromAttributes(attrs)
}

func scopeTableContentReader(req *PreviewRequest, contentReader plugin.ContentReadableProvider, catalogProvider plugin.EngineCatalogProvider, connInfo plugin.ConnectionInfo) (contentio.Reader, error) {
	if req == nil || req.Engine == nil {
		return nil, fmt.Errorf("engine is required")
	}
	if previewRequestCatalogLeafTerm(req) == "object" {
		bucket, err := resolveBucket(plugin.GetString(connInfo, "bucket"), req.Schema)
		if err != nil {
			return nil, err
		}
		return newObjectCatalogContentReader(contentReader, catalogProvider, connInfo, req.Engine.ID, bucket), nil
	}
	return newFileCatalogContentReader(contentReader, catalogProvider, connInfo, req.Engine.ID), nil
}

func resolveScopeTableFormat(req *PreviewRequest) format.FormatType {
	if req == nil {
		return format.FormatUnknown
	}
	return formatTypeFromMetaAttributes(req.Attributes)
}
