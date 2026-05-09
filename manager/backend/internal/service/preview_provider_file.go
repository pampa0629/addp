package service

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	"github.com/addp/common/resource"
	"github.com/addp/manager/internal/models"
)

// FileTablePreviewProvider 通用文件表预览 Provider
// 自动支持所有实现了 format.TableProvider 的文件格式（CSV、Excel、Shapefile、JSON 空间扩展、Parquet 等）
type FileTablePreviewProvider struct{}

func NewFileTablePreviewProvider() PreviewProvider {
	return &FileTablePreviewProvider{}
}

func (p *FileTablePreviewProvider) Name() string {
	return "builtin:file-table"
}

func objectStorageReaderContextForPreview(req *PreviewRequest) (plugin.ConnectionInfo, string, plugin.ContentReadableProvider, plugin.CatalogProvider, error) {
	if req == nil || req.Engine == nil {
		return nil, "", nil, nil, fmt.Errorf("engine is required")
	}
	pl, err := plugin.Get(req.Engine.EngineType)
	if err != nil {
		return nil, "", nil, nil, fmt.Errorf("unsupported engine type: %s", req.Engine.EngineType)
	}
	contentReader, ok := pl.(plugin.ContentReadableProvider)
	if !ok {
		return nil, "", nil, nil, fmt.Errorf("engine %s does not implement ContentReadableProvider", req.Engine.EngineType)
	}
	catalogProvider, _ := pl.(plugin.CatalogProvider)
	connInfo := plugin.ConnectionInfo(req.Engine.ConnectionInfo)
	bucket, err := resolveBucket(plugin.GetString(connInfo, "bucket"), req.Schema)
	if err != nil {
		return nil, "", nil, nil, err
	}
	return connInfo, bucket, contentReader, catalogProvider, nil
}

func (p *FileTablePreviewProvider) Preview(ctx context.Context, req *PreviewRequest) (*models.TablePreview, error) {
	connInfo, bucket, contentReader, catalogProvider, err := objectStorageReaderContextForPreview(req)
	if err != nil {
		return nil, err
	}

	fullPath := objectKeyFromPreviewRequest(req, bucket)

	// 使用共享 dataitem 口径识别格式，避免在 provider 内重复维护扩展名别名。
	formatType := p.resolveFormat(req)

	// 获取对应的格式 provider。provider 只负责从外部提供的内容流中提取 table 语义。
	provider, err := format.GetTableProvider(formatType)
	if err != nil {
		return nil, fmt.Errorf("no table provider for format %s: %w", formatType, err)
	}

	// 构建解析选项
	opts := p.buildParseOptions(formatType)
	resourceReader := newObjectStorageResourceReader(contentReader, catalogProvider, connInfo, req.Engine.ID, bucket)

	// Shapefile 是多组件格式，交给组件型 table provider 消费 ComponentReader。
	if formatType == format.FormatShapefile {
		componentProvider, ok := provider.(format.ComponentTableProvider)
		if !ok {
			return nil, fmt.Errorf("format %s does not implement component table provider", formatType)
		}
		return p.previewShapefile(ctx, shapefileComponentReader(resourceReader, fullPath), bucket, componentProvider, opts, req)
	}

	// 其他格式：流式处理
	return p.previewStreamable(ctx, resourceReader, bucket, fullPath, formatType, provider, opts, req)
}

// previewStreamable 处理可以流式读取的格式（CSV、Excel、GeoJSON 等）
func (p *FileTablePreviewProvider) previewStreamable(
	ctx context.Context,
	resourceReader resource.ResourceReader,
	bucket, fullPath string,
	formatType format.FormatType,
	provider format.TableProvider,
	opts *format.ParseOptions,
	req *PreviewRequest,
) (*models.TablePreview, error) {
	// 获取对象流
	object, err := resourceReader.Open(ctx, resource.NewResourceRef(fullPath, resource.ResourceRoleMain))
	if err != nil {
		return nil, fmt.Errorf("failed to get object: %w", err)
	}
	defer object.Close()

	// 解析 TableInfo（获取列信息和总行数）
	tableInfo, err := provider.DescribeTable(ctx, object, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to parse table info: %w", err)
	}

	// 提取列名
	columns := make([]string, len(tableInfo.Fields))
	for i, field := range tableInfo.Fields {
		columns[i] = field.Name
	}

	// 获取总行数
	totalCount := int64(0)
	if tableInfo.RowCount != nil {
		totalCount = *tableInfo.RowCount
	}

	// 计算分页参数
	pageSize := req.PageSize
	if pageSize <= 0 || pageSize > 1000 {
		pageSize = 100
	}
	page := req.Page
	if page < 1 {
		page = 1
	}
	offset := (page - 1) * pageSize

	// 重新获取对象流（用于读取数据）
	object, err = resourceReader.Open(ctx, resource.NewResourceRef(fullPath, resource.ResourceRoleMain))
	if err != nil {
		return nil, fmt.Errorf("failed to reopen object for data: %w", err)
	}
	defer object.Close()

	// 读取分页数据
	rows, err := provider.SampleTable(ctx, object, int64(offset), int64(pageSize), opts)
	if err != nil && len(rows) == 0 {
		return nil, fmt.Errorf("failed to read data: %w", err)
	}

	// 如果未获取到总行数，使用返回的行数
	if totalCount == 0 {
		totalCount = int64(len(rows))
	}

	// 检测几何列（用于空间数据）
	geometryColumns := p.detectGeometryColumns(tableInfo)
	spatialInfo := tableInfo.GetSpatialInfo()
	srid := 0
	if spatialInfo != nil {
		srid = spatialInfo.SRID
	}

	return &models.TablePreview{
		Mode:            PreviewModeTable,
		Columns:         columns,
		Rows:            rows,
		Total:           int(totalCount),
		Page:            page,
		PageSize:        pageSize,
		GeometryColumns: geometryColumns,
		SRID:            srid,
		Object: &models.ObjectPreview{
			Bucket:      bucket,
			Path:        req.Table,
			ContentType: p.getContentType(formatType),
			Content: &models.ObjectPreviewContent{
				Kind: string(formatType),
			},
		},
	}, nil
}

// previewShapefile 处理 Shapefile 格式（需要下载所有组件文件）
func (p *FileTablePreviewProvider) previewShapefile(
	ctx context.Context,
	components resource.ComponentReader,
	bucket string,
	provider format.ComponentTableProvider,
	opts *format.ParseOptions,
	req *PreviewRequest,
) (*models.TablePreview, error) {
	// 解析 TableInfo
	tableInfo, err := provider.DescribeTableComponents(ctx, components, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to parse shapefile info: %w", err)
	}

	// 提取列名
	columns := make([]string, len(tableInfo.Fields))
	for i, field := range tableInfo.Fields {
		columns[i] = field.Name
	}

	// 获取总行数
	totalCount := int64(0)
	if tableInfo.RowCount != nil {
		totalCount = *tableInfo.RowCount
	}

	// 计算分页参数
	pageSize := req.PageSize
	if pageSize <= 0 || pageSize > 1000 {
		pageSize = 100
	}
	page := req.Page
	if page < 1 {
		page = 1
	}
	offset := (page - 1) * pageSize

	// 读取分页数据
	rows, err := provider.SampleTableComponents(ctx, components, int64(offset), int64(pageSize), opts)
	if err != nil && len(rows) == 0 {
		return nil, fmt.Errorf("failed to read shapefile data: %w", err)
	}

	// 检测几何列
	geometryColumns := p.detectGeometryColumns(tableInfo)
	spatialInfo := tableInfo.GetSpatialInfo()
	srid := 0
	if spatialInfo != nil {
		srid = spatialInfo.SRID
	}

	return &models.TablePreview{
		Mode:            PreviewModeTable,
		Columns:         columns,
		Rows:            rows,
		Total:           int(totalCount),
		Page:            page,
		PageSize:        pageSize,
		GeometryColumns: geometryColumns,
		SRID:            srid,
		Object: &models.ObjectPreview{
			Bucket:      bucket,
			Path:        req.Table,
			ContentType: "application/x-esri-shapefile",
			Content: &models.ObjectPreviewContent{
				Kind: "shapefile",
			},
		},
	}, nil
}

// buildParseOptions 根据 Meta 已识别的格式类型构建解析选项。
func (p *FileTablePreviewProvider) buildParseOptions(formatType format.FormatType) *format.ParseOptions {
	opts := &format.ParseOptions{
		HasHeader:  true,
		SampleSize: 100,
	}

	switch formatType {
	case format.FormatTSV:
		opts.Delimiter = '\t'
	case format.FormatCSV:
		opts.Delimiter = ','
	}

	return opts
}

func (p *FileTablePreviewProvider) resolveFormat(req *PreviewRequest) format.FormatType {
	if req == nil {
		return format.FormatUnknown
	}
	if formatName := strings.TrimSpace(stringAttribute(req.Attributes, "format")); formatName != "" {
		return normalizeFileTableFormat(formatName)
	}
	return format.MIMEToFormat(stringAttribute(req.Attributes, "content_type"))
}

func normalizeFileTableFormat(formatName string) format.FormatType {
	switch strings.ToLower(strings.TrimSpace(formatName)) {
	case "xlsx", "xls":
		return format.FormatExcel
	case "shp":
		return format.FormatShapefile
	case "gpkg":
		return format.FormatGeoPackage
	default:
		return format.FormatType(strings.ToLower(strings.TrimSpace(formatName)))
	}
}

func objectKeyFromPreviewRequest(req *PreviewRequest, bucket string) string {
	if req == nil {
		return ""
	}
	for _, path := range []string{
		req.PhysicalPath,
	} {
		path = strings.Trim(path, "/")
		if strings.HasPrefix(path, bucket+"/") {
			return strings.TrimPrefix(path, bucket+"/")
		}
		if path != "" {
			return path
		}
	}
	fullPath := req.Table
	if req.Schema != "" && req.Schema != bucket {
		fullPath = filepath.Join(req.Schema, req.Table)
	}
	return strings.Trim(fullPath, "/")
}

func itemEntryPath(req *PreviewRequest, fallback string) string {
	if req == nil {
		return fallback
	}
	if req.PhysicalPath != "" {
		return req.PhysicalPath
	}
	return fallback
}

// detectGeometryColumns 检测几何列
func (p *FileTablePreviewProvider) detectGeometryColumns(tableInfo *format.TableInfo) []string {
	var geometryColumns []string
	for _, field := range tableInfo.Fields {
		if field.Type == format.FieldTypeGeometry {
			geometryColumns = append(geometryColumns, field.Name)
		}
	}
	return geometryColumns
}

// getContentType 根据格式类型返回 MIME 类型
func (p *FileTablePreviewProvider) getContentType(formatType format.FormatType) string {
	switch formatType {
	case format.FormatCSV:
		return "text/csv"
	case format.FormatTSV:
		return "text/tab-separated-values"
	case format.FormatExcel:
		return "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	case format.FormatJSON:
		return "application/json"
	case format.FormatShapefile:
		return "application/x-esri-shapefile"
	default:
		return "application/octet-stream"
	}
}
