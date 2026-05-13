package service

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	commonJSON "github.com/addp/common/jsonmap"
	"github.com/addp/common/resource"
	"github.com/addp/manager/internal/models"
)

// FileTablePreviewProvider 通用文件表预览 Provider
// 自动支持所有实现了 format.TableProvider 的文件格式（CSV、Excel、Shapefile、JSON 空间扩展、Parquet 等）
type FileTablePreviewProvider struct{}

type tablePreviewResourceContext struct {
	reader resource.ResourceReader
	bucket string
	path   string
}

func NewFileTablePreviewProvider() PreviewProvider {
	return &FileTablePreviewProvider{}
}

func (p *FileTablePreviewProvider) Name() string {
	return "builtin:file-table"
}

func contentReaderContextForPreview(req *PreviewRequest) (plugin.ConnectionInfo, plugin.ContentReadableProvider, plugin.CatalogProvider, error) {
	if req == nil || req.Engine == nil {
		return nil, nil, nil, fmt.Errorf("engine is required")
	}
	pl, err := plugin.Get(req.Engine.EngineType)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("unsupported engine type: %s", req.Engine.EngineType)
	}
	contentReader, ok := pl.(plugin.ContentReadableProvider)
	if !ok {
		return nil, nil, nil, fmt.Errorf("engine %s does not implement ContentReadableProvider", req.Engine.EngineType)
	}
	catalogProvider, _ := pl.(plugin.CatalogProvider)
	connInfo := plugin.ConnectionInfo(req.Engine.ConnectionInfo)
	return connInfo, contentReader, catalogProvider, nil
}

func (p *FileTablePreviewProvider) Preview(ctx context.Context, req *PreviewRequest) (*models.TablePreview, error) {
	resourceCtx, err := p.resourceContextForPreview(req)
	if err != nil {
		return nil, err
	}

	// 使用共享 dataitem 口径识别格式，避免在 provider 内重复维护扩展名别名。
	formatType := p.resolveFormat(req)

	// 获取对应的格式 provider。provider 只负责从外部提供的内容流中提取 table 语义。
	provider, err := format.GetTableProvider(formatType)
	if err != nil {
		return nil, fmt.Errorf("no table provider for format %s: %w", formatType, err)
	}

	// 构建解析选项
	opts := p.buildParseOptions(formatType, req)

	if componentProvider, ok := provider.(format.ComponentTableProvider); ok {
		return p.previewComponents(ctx, componentReaderForPreview(resourceCtx.reader, resourceCtx.path, formatType, req.Attributes), resourceCtx.bucket, formatType, componentProvider, opts, req)
	}

	p.ensureContentIndex(ctx, req, resourceCtx.reader, resourceCtx.bucket, resourceCtx.path, formatType)

	// 其他格式：流式处理
	return p.previewStreamable(ctx, resourceCtx.reader, resourceCtx.bucket, resourceCtx.path, formatType, provider, opts, req)
}

func (p *FileTablePreviewProvider) resourceContextForPreview(req *PreviewRequest) (*tablePreviewResourceContext, error) {
	connInfo, contentReader, catalogProvider, err := contentReaderContextForPreview(req)
	if err != nil {
		return nil, err
	}
	if isObjectStorageType(req.Engine.EngineType) {
		bucket, err := resolveBucket(plugin.GetString(connInfo, "bucket"), req.Schema)
		if err != nil {
			return nil, err
		}
		return &tablePreviewResourceContext{
			reader: newObjectStorageResourceReader(contentReader, catalogProvider, connInfo, req.Engine.ID, bucket),
			bucket: bucket,
			path:   objectKeyFromPreviewRequest(req, bucket),
		}, nil
	}
	if isFileSystemType(req.Engine.EngineType) {
		path := fileSystemPathFromPreviewRequest(req)
		return &tablePreviewResourceContext{
			reader: newFileSystemResourceReader(contentReader, catalogProvider, connInfo, req.Engine.ID),
			bucket: req.Schema,
			path:   path,
		}, nil
	}
	return nil, fmt.Errorf("engine %s does not provide file content table preview", req.Engine.EngineType)
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
	tableInfo, err := p.tableInfoFromAttributes(req)
	if err != nil {
		return nil, err
	}
	if tableInfo == nil {
		// 获取对象流
		object, err := resourceReader.Open(ctx, resource.NewResourceRef(fullPath, resource.ResourceRoleMain))
		if err != nil {
			return nil, fmt.Errorf("failed to get object: %w", err)
		}
		defer object.Close()

		// 解析 TableInfo（获取列信息和总行数）
		tableInfo, err = provider.DescribeTable(ctx, object, opts)
		if err != nil {
			return nil, fmt.Errorf("failed to parse table info: %w", err)
		}
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

	object, sampleOpts, err := p.openSampleReader(ctx, resourceReader, fullPath, tableInfo, opts, req, offset, pageSize)
	if err != nil {
		return nil, fmt.Errorf("failed to reopen object for data: %w", err)
	}
	defer object.Close()

	// 读取分页数据
	rows, err := provider.SampleTable(ctx, object, int64(offset), int64(pageSize), sampleOpts)
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

func (p *FileTablePreviewProvider) tableInfoFromAttributes(req *PreviewRequest) (*format.TableInfo, error) {
	attrs := map[string]interface{}(nil)
	if req != nil {
		attrs = req.Attributes
	}
	tableAttrs := commonJSON.Section(attrs, "type_info.table")
	if len(tableAttrs) == 0 {
		return nil, nil
	}
	fields := fieldsFromAttribute(tableAttrs["fields"])
	rowCount := commonJSON.InterfaceInt64(tableAttrs["row_count"])
	if len(fields) == 0 {
		return nil, nil
	}
	info := &format.TableInfo{
		Name:       "table",
		Fields:     fields,
		PrimaryKey: interfaceToStringSlice(tableAttrs["primary_key"]),
	}
	if rowCount > 0 {
		info.RowCount = &rowCount
	}
	if spatialInfo := spatialInfoFromAttributes(attrs); spatialInfo != nil {
		info.Extensions = append(info.Extensions, spatialInfo)
	}
	return info, nil
}

func containerChildNameMatches(child map[string]interface{}, childName string) bool {
	for _, key := range []string{"name", "table"} {
		if strings.EqualFold(strings.TrimSpace(commonJSON.InterfaceString(child[key])), childName) {
			return true
		}
	}
	return false
}

func spatialInfoFromAttributes(attrs map[string]interface{}) *format.SpatialInfo {
	spatialAttrs := commonJSON.Section(attrs, "capabilities.spatial")
	if len(spatialAttrs) == 0 {
		return nil
	}
	geometryColumn := commonJSON.InterfaceString(spatialAttrs["primary_geometry_column"])
	geometryType := ""
	srid := 0
	for _, item := range interfaceSlice(spatialAttrs["geometry_columns"]) {
		column := rawMapAttribute(item)
		if len(column) == 0 {
			continue
		}
		name := commonJSON.InterfaceString(column["name"])
		if geometryColumn == "" || name == geometryColumn {
			if geometryColumn == "" {
				geometryColumn = name
			}
			geometryType = commonJSON.InterfaceString(column["geometry_type"])
			srid = int(commonJSON.InterfaceInt64(column["srid"]))
			break
		}
	}
	if geometryColumn == "" {
		return nil
	}
	spatialInfo := &format.SpatialInfo{
		GeometryColumn:  geometryColumn,
		GeometryType:    geometryType,
		SRID:            srid,
		HasSpatialIndex: commonJSON.InterfaceBool(spatialAttrs["has_spatial_index"]),
		IndexName:       commonJSON.InterfaceString(spatialAttrs["index_name"]),
		Dimension:       int(commonJSON.InterfaceInt64(spatialAttrs["dimension"])),
	}
	if spatialInfo.Dimension == 0 {
		spatialInfo.Dimension = 2
	}
	if extent := commonJSON.InterfaceFloat64Slice(spatialAttrs["extent"]); len(extent) == 4 {
		spatialInfo.BoundingBox = &[4]float64{extent[0], extent[1], extent[2], extent[3]}
	}
	return spatialInfo
}

func (p *FileTablePreviewProvider) openSampleReader(
	ctx context.Context,
	resourceReader resource.ResourceReader,
	fullPath string,
	tableInfo *format.TableInfo,
	opts *format.ParseOptions,
	req *PreviewRequest,
	offset int,
	pageSize int,
) (io.ReadCloser, *format.ParseOptions, error) {
	if reader, sampleOpts, ok := p.openIndexedRangeReader(ctx, tableInfo, opts, req, offset, pageSize); ok {
		return reader, sampleOpts, nil
	}
	reader, err := resourceReader.Open(ctx, resource.NewResourceRef(fullPath, resource.ResourceRoleMain))
	return reader, opts, err
}

func (p *FileTablePreviewProvider) ensureContentIndex(
	ctx context.Context,
	req *PreviewRequest,
	resourceReader resource.ResourceReader,
	bucket string,
	fullPath string,
	formatType format.FormatType,
) {
	if req == nil || req.Engine == nil || resourceReader == nil || tableContentIndexFromAttributes(req.Attributes) != nil {
		return
	}
	if !format.SupportsContentIndex(formatType) {
		return
	}
	token := getTokenFromContext(ctx)
	if token == "" {
		return
	}
	object, err := resourceReader.Open(ctx, resource.NewResourceRef(fullPath, resource.ResourceRoleMain))
	if err != nil {
		return
	}
	defer object.Close()

	metaURL := getEnvOrDefault("META_URL", "http://localhost:8082")
	metaClient := NewMetaClient(metaURL, token)
	attrs, err := metaClient.BuildObjectContentIndex(&ExtractObjectMetadataRequest{
		EngineID:   req.Engine.ID,
		ObjectKey:  contentIndexObjectKey(req, bucket, fullPath),
		ObjectData: object,
	})
	if err != nil || len(attrs) == 0 {
		return
	}
	req.Attributes = attrs
}

func contentIndexObjectKey(req *PreviewRequest, bucket string, fullPath string) string {
	if req != nil && req.Engine != nil && isObjectStorageType(req.Engine.EngineType) {
		if strings.HasPrefix(fullPath, bucket+"/") {
			return fullPath
		}
		return bucket + "/" + strings.TrimPrefix(fullPath, "/")
	}
	return fullPath
}

func (p *FileTablePreviewProvider) openIndexedRangeReader(
	ctx context.Context,
	tableInfo *format.TableInfo,
	opts *format.ParseOptions,
	req *PreviewRequest,
	offset int,
	pageSize int,
) (io.ReadCloser, *format.ParseOptions, bool) {
	if req == nil || req.Engine == nil || tableInfo == nil {
		return nil, nil, false
	}
	index := tableContentIndexFromAttributes(req.Attributes)
	if !usableTableContentIndex(index) {
		return nil, nil, false
	}
	anchor, length := rangeForTableWindow(index, int64(offset), int64(pageSize), int64Attribute(req.Attributes, "total_size"))
	if length <= 0 {
		return nil, nil, false
	}
	pl, err := plugin.Get(req.Engine.EngineType)
	if err != nil {
		return nil, nil, false
	}
	rangeReader, ok := pl.(plugin.RangeReadableProvider)
	if !ok {
		return nil, nil, false
	}
	connInfo := plugin.ConnectionInfo(req.Engine.ConnectionInfo)
	if isFileSystemType(req.Engine.EngineType) {
		reader, err := rangeReader.OpenRange(ctx, connInfo, fileSystemCatalogPath(req.Engine.ID, fileSystemPathFromPreviewRequest(req)), plugin.ReadOptions{
			Offset: anchor.ByteOffset,
			Length: length,
		})
		if err != nil {
			return nil, nil, false
		}
		return reader, positionedTableSampleOptions(opts, tableInfo, anchor.Row), true
	}
	bucket := commonJSON.String(req.Attributes, "storage", "bucket")
	if bucket == "" {
		resolved, err := resolveBucket(plugin.GetString(connInfo, "bucket"), req.Schema)
		if err != nil {
			return nil, nil, false
		}
		bucket = resolved
	}
	objectPath := objectKeyFromPreviewRequest(req, bucket)
	reader, err := rangeReader.OpenRange(ctx, connInfo, objectStorageObjectCatalogPath(req.Engine.ID, bucket, objectPath), plugin.ReadOptions{
		Offset: anchor.ByteOffset,
		Length: length,
	})
	if err != nil {
		return nil, nil, false
	}
	return reader, positionedTableSampleOptions(opts, tableInfo, anchor.Row), true
}

func positionedTableSampleOptions(opts *format.ParseOptions, tableInfo *format.TableInfo, row int64) *format.ParseOptions {
	baseOpts := format.DefaultParseOptions()
	if opts != nil {
		copied := *opts
		baseOpts = &copied
	}
	sampleOpts := *baseOpts
	sampleOpts.TableSample = &format.TableSampleOptions{
		Fields:            tableInfo.Fields,
		InputStartsAtRow:  row,
		InputIsPositioned: true,
	}
	return &sampleOpts
}

func usableTableContentIndex(index *format.ContentIndex) bool {
	return index != nil &&
		index.Kind == format.ContentIndexKindSparseRow &&
		index.Unit == format.ContentIndexUnitRow &&
		index.OffsetUnit == format.ContentIndexOffsetByte &&
		len(index.Anchors) > 0
}

func rangeForTableWindow(index *format.ContentIndex, offset, limit, totalSize int64) (format.ContentIndexAnchor, int64) {
	anchors := append([]format.ContentIndexAnchor(nil), index.Anchors...)
	sort.Slice(anchors, func(i, j int) bool {
		return anchors[i].Row < anchors[j].Row
	})
	anchor := anchors[0]
	for _, candidate := range anchors {
		if candidate.Row <= offset {
			anchor = candidate
			continue
		}
		break
	}
	endRow := offset + limit
	endByte := totalSize
	for _, candidate := range anchors {
		if candidate.Row >= endRow && candidate.ByteOffset > anchor.ByteOffset {
			endByte = candidate.ByteOffset
			break
		}
	}
	if endByte <= anchor.ByteOffset {
		return anchor, 0
	}
	return anchor, endByte - anchor.ByteOffset
}

func tableContentIndexFromAttributes(attrs map[string]interface{}) *format.ContentIndex {
	indexAttrs := commonJSON.Section(attrs, "content_index.table")
	if len(indexAttrs) == 0 {
		return nil
	}
	index := &format.ContentIndex{
		Kind:        commonJSON.InterfaceString(indexAttrs["kind"]),
		DataType:    commonJSON.InterfaceString(indexAttrs["data_type"]),
		Format:      commonJSON.InterfaceString(indexAttrs["format"]),
		Unit:        commonJSON.InterfaceString(indexAttrs["unit"]),
		OffsetUnit:  commonJSON.InterfaceString(indexAttrs["offset_unit"]),
		Step:        commonJSON.InterfaceInt64(indexAttrs["step"]),
		RowCount:    commonJSON.InterfaceInt64(indexAttrs["row_count"]),
		HeaderBytes: commonJSON.InterfaceInt64(indexAttrs["header_bytes"]),
		Source:      rawMapAttribute(indexAttrs["source"]),
		Anchors:     contentIndexAnchorsFromAttribute(indexAttrs["anchors"]),
	}
	return index
}

func contentIndexAnchorsFromAttribute(value interface{}) []format.ContentIndexAnchor {
	items := interfaceSlice(value)
	anchors := make([]format.ContentIndexAnchor, 0, len(items))
	for _, item := range items {
		attrs := rawMapAttribute(item)
		if len(attrs) == 0 {
			continue
		}
		anchors = append(anchors, format.ContentIndexAnchor{
			Row:        commonJSON.InterfaceInt64(attrs["row"]),
			ByteOffset: commonJSON.InterfaceInt64(attrs["byte_offset"]),
		})
	}
	return anchors
}

func fieldsFromAttribute(value interface{}) []format.FieldInfo {
	items := interfaceSlice(value)
	fields := make([]format.FieldInfo, 0, len(items))
	for _, item := range items {
		attrs := rawMapAttribute(item)
		name := commonJSON.InterfaceString(attrs["name"])
		if name == "" {
			continue
		}
		fields = append(fields, format.FieldInfo{
			Name:         name,
			Type:         format.FieldType(commonJSON.InterfaceString(attrs["type"])),
			OriginalType: commonJSON.InterfaceString(attrs["original_type"]),
			Nullable:     commonJSON.InterfaceBool(attrs["nullable"]),
		})
	}
	return fields
}

func interfaceSlice(value interface{}) []interface{} {
	switch typed := value.(type) {
	case []interface{}:
		return typed
	case []map[string]interface{}:
		result := make([]interface{}, 0, len(typed))
		for _, item := range typed {
			result = append(result, item)
		}
		return result
	default:
		return nil
	}
}

func rawMapAttribute(value interface{}) map[string]interface{} {
	switch typed := value.(type) {
	case map[string]interface{}:
		return typed
	case models.JSONMap:
		return map[string]interface{}(typed)
	default:
		return nil
	}
}

// previewComponents 处理多组件表格格式。
func (p *FileTablePreviewProvider) previewComponents(
	ctx context.Context,
	components resource.ComponentReader,
	bucket string,
	formatType format.FormatType,
	provider format.ComponentTableProvider,
	opts *format.ParseOptions,
	req *PreviewRequest,
) (*models.TablePreview, error) {
	// 解析 TableInfo
	tableInfo, err := p.tableInfoFromAttributes(req)
	if err != nil {
		return nil, err
	}
	if tableInfo == nil {
		tableInfo, err = provider.DescribeTableComponents(ctx, components, opts)
		if err != nil {
			return nil, fmt.Errorf("failed to parse %s component table info: %w", formatType, err)
		}
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
		return nil, fmt.Errorf("failed to read %s component table data: %w", formatType, err)
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
			ContentType: p.getContentType(formatType),
			Content: &models.ObjectPreviewContent{
				Kind: string(formatType),
			},
		},
	}, nil
}

// buildParseOptions 根据 Meta 已识别的格式类型构建解析选项。
func (p *FileTablePreviewProvider) buildParseOptions(formatType format.FormatType, req ...*PreviewRequest) *format.ParseOptions {
	opts := &format.ParseOptions{
		HasHeader:  true,
		SampleSize: 100,
	}
	if len(req) > 0 && req[0] != nil && strings.TrimSpace(req[0].ChildName) != "" {
		return format.ChildTableParseOptions(req[0].ChildName, containerChildForRequest(req[0].Attributes, req[0].ChildName))
	}
	return opts
}

func containerChildTableNameForRequest(attrs map[string]interface{}, childName string) string {
	child := containerChildForRequest(attrs, childName)
	if len(child) > 0 {
		tableName := strings.TrimSpace(commonJSON.InterfaceString(child["table"]))
		if tableName != "" {
			return tableName
		}
	}
	return strings.TrimSpace(childName)
}

func containerChildForRequest(attrs map[string]interface{}, childName string) map[string]interface{} {
	childName = strings.TrimSpace(childName)
	if childName == "" {
		return nil
	}
	containerAttrs := commonJSON.Section(attrs, "type_info.container")
	for _, item := range interfaceSlice(containerAttrs["children"]) {
		child := rawMapAttribute(item)
		if len(child) > 0 && containerChildNameMatches(child, childName) {
			return child
		}
	}
	return map[string]interface{}{"name": childName}
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
	normalized := strings.ToLower(strings.TrimSpace(formatName))
	if normalized == "" {
		return format.FormatUnknown
	}
	if byExt := format.DetectFormat("file."+strings.TrimPrefix(normalized, "."), nil); byExt != format.FormatUnknown {
		return byExt
	}
	if byMime := format.MIMEToFormat(normalized); byMime != format.FormatUnknown {
		return byMime
	}
	return format.FormatType(normalized)
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

func fileSystemPathFromPreviewRequest(req *PreviewRequest) string {
	if req == nil {
		return ""
	}
	if req.PhysicalPath != "" {
		return strings.Trim(req.PhysicalPath, "/")
	}
	return strings.Trim(nfsPhysicalPath(req.Schema, req.Table), "/")
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
	contentType := format.FormatToMIME(formatType)
	if contentType == "" {
		return "application/octet-stream"
	}
	return contentType
}
