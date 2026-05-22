package preview

import (
	"context"
	"fmt"
	"github.com/addp/common/datatype"
	"io"
	"path/filepath"
	"sort"
	"strings"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/contentio"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	commonJSON "github.com/addp/common/jsonmap"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/objectcontent"
)

// FileTablePreviewProvider 通用文件表预览 Provider。
// 按 format 层拆分后的能力入口读取 table 信息和样本数据。
type FileTablePreviewProvider struct{}

type tablePreviewContentContext struct {
	reader contentio.Reader
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
	contentCtx, err := p.contentContextForPreview(req)
	if err != nil {
		return nil, err
	}

	// 使用共享 dataitem 口径识别格式，避免在 provider 内重复维护扩展名别名。
	formatType := p.resolveFormat(req)
	fullPath := contentCtx.path

	// 构建解析选项
	opts := p.buildParseOptions(formatType, req)
	contentReader := contentCtx.reader
	if strings.TrimSpace(req.ChildName) != "" {
		resolved, err := p.resolveContainerChild(ctx, contentReader, fullPath, formatType, req)
		if err != nil {
			return nil, err
		}
		contentReader = resolved.Reader
		fullPath = resolved.Ref.Path
		formatType = resolved.Format
		opts = resolved.ParentOptions
		if opts == nil {
			opts = p.buildParseOptions(formatType, req)
		}
	}

	if refInfoProvider, err := format.GetMultiTableInfoProvider(formatType); err == nil {
		refSampleReader, err := format.GetMultiTableSampleReader(formatType)
		if err != nil {
			return nil, fmt.Errorf("no multi table sample reader for format %s: %w", formatType, err)
		}
		refs := refsForPreview(fullPath, formatType, req.Attributes)
		preview, err := p.previewRefs(ctx, contentReader, refs, contentCtx.bucket, formatType, refInfoProvider, refSampleReader, opts, req)
		if err != nil {
			return nil, err
		}
		attachMultiRefPreview(preview, formatType, refs)
		return preview, nil
	}

	infoProvider, _ := format.GetTableInfoProvider(formatType)
	sampleReader, err := format.GetTableSampleReader(formatType)
	if err != nil {
		return nil, fmt.Errorf("no table sample reader for format %s: %w", formatType, err)
	}

	p.ensureContentIndex(ctx, req, contentReader, contentCtx.bucket, fullPath, formatType)

	// 其他格式：流式处理
	return p.previewStreamable(ctx, contentReader, contentCtx.bucket, fullPath, formatType, infoProvider, sampleReader, opts, req)
}

func resolvePreviewContainerChild(ctx context.Context, parent contentio.Reader, parentPath string, parentFormat format.FormatType, req *PreviewRequest) (*format.ContainerChildResource, error) {
	return resolvePreviewContainerChildFromResource(ctx, parent, contentio.NewRef(parentPath, contentio.RoleMain), parentFormat, req)
}

func resolvePreviewContainerChildFromResource(ctx context.Context, parent contentio.Reader, parentRef contentio.Ref, parentFormat format.FormatType, req *PreviewRequest) (*format.ContainerChildResource, error) {
	child := containerChildForRequest(req.Attributes, req.ChildName)
	resolver, err := format.GetContainerChildResolver(parentFormat)
	if err != nil {
		return nil, fmt.Errorf("no container child resolver for format %s: %w", parentFormat, err)
	}
	resolved, err := resolver.ResolveContainerChild(ctx, parent, parentRef, mapToContainerChildInfo(child), format.ChildTableParseOptions(req.ChildName, child))
	if err != nil {
		return nil, err
	}
	if resolved == nil {
		return nil, fmt.Errorf("container child %s resolved to nil", req.ChildName)
	}
	if resolved.ResourceKind == format.ContainerChildResourceNative {
		resolved.Reader = resolved.ParentReader
		resolved.Ref = resolved.ParentRef
		resolved.Format = resolved.ParentFormat
	}
	if resolved.Reader == nil {
		return nil, fmt.Errorf("container child %s has no reader", req.ChildName)
	}
	return resolved, nil
}

func (p *FileTablePreviewProvider) resolveContainerChild(ctx context.Context, parent contentio.Reader, parentPath string, parentFormat format.FormatType, req *PreviewRequest) (*format.ContainerChildResource, error) {
	return resolvePreviewContainerChild(ctx, parent, parentPath, parentFormat, req)
}

func (p *FileTablePreviewProvider) contentContextForPreview(req *PreviewRequest) (*tablePreviewContentContext, error) {
	connInfo, contentReader, catalogProvider, err := contentReaderContextForPreview(req)
	if err != nil {
		return nil, err
	}
	switch previewRequestCatalogItemTerm(req) {
	case "object":
		bucket, err := resolveBucket(plugin.GetString(connInfo, "bucket"), req.Schema)
		if err != nil {
			return nil, err
		}
		return &tablePreviewContentContext{
			reader: newObjectCatalogContentReader(contentReader, catalogProvider, connInfo, req.Engine.ID, bucket),
			bucket: bucket,
			path:   objectKeyFromPreviewRequest(req, bucket),
		}, nil
	case "file":
		path := fileSystemPathFromPreviewRequest(req)
		return &tablePreviewContentContext{
			reader: newFileCatalogContentReader(contentReader, catalogProvider, connInfo, req.Engine.ID),
			bucket: req.Schema,
			path:   path,
		}, nil
	default:
		return nil, fmt.Errorf("item type %s does not provide file content table preview", req.ItemType)
	}
}

// previewStreamable 处理可以流式读取的格式（CSV、Excel、GeoJSON 等）
func (p *FileTablePreviewProvider) previewStreamable(
	ctx context.Context,
	contentReader contentio.Reader,
	bucket, fullPath string,
	formatType format.FormatType,
	infoProvider format.TableInfoProvider,
	sampleReader format.TableSampleReader,
	opts *format.ParseOptions,
	req *PreviewRequest,
) (*models.TablePreview, error) {
	tableInfo, err := p.tableInfoFromAttributes(req)
	if err != nil {
		return nil, err
	}
	if tableInfo == nil {
		if infoProvider == nil {
			return nil, fmt.Errorf("no table info provider for format %s", formatType)
		}
		// 获取对象流
		object, err := contentReader.Open(ctx, contentio.NewRef(fullPath, contentio.RoleMain))
		if err != nil {
			return nil, fmt.Errorf("failed to get object: %w", err)
		}
		defer object.Close()

		// 解析 TableInfo（获取列信息和总行数）
		result, err := infoProvider.DescribeTable(ctx, object, opts)
		if err != nil {
			return nil, fmt.Errorf("failed to parse table info: %w", err)
		}
		tableInfo = format.TableSchemaFromDescribeResult(result)
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

	object, sampleOpts, err := p.openSampleReader(ctx, contentReader, fullPath, tableInfo, opts, req, offset, pageSize)
	if err != nil {
		return nil, fmt.Errorf("failed to reopen object for data: %w", err)
	}
	defer object.Close()

	// 读取分页数据
	rows, err := sampleReader.SampleTable(ctx, object, int64(offset), int64(pageSize), sampleOpts)
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
		srid = format.PrimaryGeometrySRID(spatialInfo)
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
		info.SpatialInfo = spatialInfo
	}
	return info, nil
}

func containerChildNameMatches(child map[string]interface{}, childName string) bool {
	for _, key := range []string{"name", "table", "key", "path"} {
		if strings.EqualFold(strings.TrimSpace(commonJSON.InterfaceString(child[key])), childName) {
			return true
		}
	}
	return false
}

func spatialInfoFromAttributes(attrs map[string]interface{}) *datatype.SpatialInfo {
	spatialAttrs := commonJSON.Section(attrs, "capabilities.spatial")
	if len(spatialAttrs) == 0 {
		return nil
	}
	geometryColumn := commonJSON.InterfaceString(spatialAttrs["primary_geometry_column"])
	geometryType := ""
	srid := 0
	dimension := 0
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
			dimension = int(commonJSON.InterfaceInt64(column["dimension"]))
			break
		}
	}
	if geometryColumn == "" {
		return nil
	}
	if dimension == 0 {
		dimension = int(commonJSON.InterfaceInt64(spatialAttrs["dimension"]))
	}
	if dimension == 0 {
		dimension = 2
	}
	spatialInfo := format.NewSingleGeometrySpatialInfo(geometryColumn, geometryType, srid, dimension)
	hasSpatialIndex := commonJSON.InterfaceBool(spatialAttrs["has_spatial_index"])
	spatialInfo.HasSpatialIndex = &hasSpatialIndex
	spatialInfo.IndexName = commonJSON.InterfaceString(spatialAttrs["index_name"])
	if extent := commonJSON.InterfaceFloat64Slice(spatialAttrs["extent"]); len(extent) == 4 {
		boundingBox := datatype.BoundingBox{extent[0], extent[1], extent[2], extent[3]}
		spatialInfo.Extent = &boundingBox
	}
	return spatialInfo
}

func (p *FileTablePreviewProvider) openSampleReader(
	ctx context.Context,
	contentReader contentio.Reader,
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
	reader, err := contentReader.Open(ctx, contentio.NewRef(fullPath, contentio.RoleMain))
	return reader, opts, err
}

func (p *FileTablePreviewProvider) ensureContentIndex(
	ctx context.Context,
	req *PreviewRequest,
	contentReader contentio.Reader,
	bucket string,
	fullPath string,
	formatType format.FormatType,
) {
	if req == nil || req.Engine == nil || contentReader == nil || tableContentIndexFromAttributes(req.Attributes) != nil {
		return
	}
	if !format.SupportsContentIndex(formatType) {
		return
	}
	token := getTokenFromContext(ctx)
	if token == "" {
		return
	}
	object, err := contentReader.Open(ctx, contentio.NewRef(fullPath, contentio.RoleMain))
	if err != nil {
		return
	}
	defer object.Close()

	metaURL := getEnvOrDefault("META_URL", "http://localhost:8082")
	metaClient := commonClient.NewMetaClient(metaURL, token)
	attrs, err := metaClient.BuildObjectContentIndex(&commonClient.ObjectMetadataRequest{
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
	if previewRequestCatalogItemTerm(req) == "object" {
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
	if previewRequestCatalogItemTerm(req) == "file" {
		reader, err := rangeReader.OpenRange(ctx, connInfo, plugin.FileItemPath(req.Engine.ID, fileSystemPathFromPreviewRequest(req)), plugin.ReadOptions{
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
	reader, err := rangeReader.OpenRange(ctx, connInfo, plugin.ObjectItemPath(req.Engine.ID, bucket, objectPath), plugin.ReadOptions{
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

func usableTableContentIndex(index *datatype.ContentIndex) bool {
	return index != nil &&
		index.Kind == datatype.ContentIndexKindSparseRow &&
		index.Unit == datatype.ContentIndexUnitRow &&
		index.OffsetUnit == datatype.ContentIndexOffsetByte &&
		len(index.Anchors) > 0
}

func rangeForTableWindow(index *datatype.ContentIndex, offset, limit, totalSize int64) (datatype.ContentIndexAnchor, int64) {
	anchors := append([]datatype.ContentIndexAnchor(nil), index.Anchors...)
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

func tableContentIndexFromAttributes(attrs map[string]interface{}) *datatype.ContentIndex {
	indexAttrs := commonJSON.Section(attrs, "content_index.table")
	if len(indexAttrs) == 0 {
		return nil
	}
	index := &datatype.ContentIndex{
		Kind:        commonJSON.InterfaceString(indexAttrs["kind"]),
		DataType:    datatype.DataType(commonJSON.InterfaceString(indexAttrs["data_type"])),
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

func contentIndexAnchorsFromAttribute(value interface{}) []datatype.ContentIndexAnchor {
	items := interfaceSlice(value)
	anchors := make([]datatype.ContentIndexAnchor, 0, len(items))
	for _, item := range items {
		attrs := rawMapAttribute(item)
		if len(attrs) == 0 {
			continue
		}
		anchors = append(anchors, datatype.ContentIndexAnchor{
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
			Name:     name,
			Type:     datatype.FieldType(commonJSON.InterfaceString(attrs["type"])),
			Nullable: commonJSON.InterfaceBool(attrs["nullable"]),
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

// previewRefs 处理多 ref表格格式。
func (p *FileTablePreviewProvider) previewRefs(
	ctx context.Context,
	reader contentio.Reader,
	refs []format.RelatedRef,
	bucket string,
	formatType format.FormatType,
	infoProvider format.MultiTableInfoProvider,
	sampleReader format.MultiTableSampleReader,
	opts *format.ParseOptions,
	req *PreviewRequest,
) (*models.TablePreview, error) {
	// 解析 TableInfo
	tableInfo, err := p.tableInfoFromAttributes(req)
	if err != nil {
		return nil, err
	}
	if tableInfo == nil {
		result, err := infoProvider.DescribeMultiTable(ctx, reader, refs, opts)
		if err != nil {
			return nil, fmt.Errorf("failed to parse %s ref table info: %w", formatType, err)
		}
		tableInfo = format.TableSchemaFromDescribeResult(result)
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
	rows, err := sampleReader.SampleMultiTable(ctx, reader, refs, int64(offset), int64(pageSize), opts)
	if err != nil && len(rows) == 0 {
		return nil, fmt.Errorf("failed to read %s ref table data: %w", formatType, err)
	}

	// 检测几何列
	geometryColumns := p.detectGeometryColumns(tableInfo)
	spatialInfo := tableInfo.GetSpatialInfo()
	srid := 0
	if spatialInfo != nil {
		srid = format.PrimaryGeometrySRID(spatialInfo)
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

func attachMultiRefPreview(preview *models.TablePreview, formatType format.FormatType, refs []format.RelatedRef) {
	if preview == nil || preview.Object == nil || len(refs) == 0 {
		return
	}
	if preview.Object.Attributes == nil {
		preview.Object.Attributes = models.JSONMap{}
	}
	preview.Object.Attributes["refs"] = refPreviewDescriptors(formatType, refs)
	if preview.Object.Content == nil {
		preview.Object.Content = &models.ObjectPreviewContent{}
	}
	if preview.Object.Content.Metadata == nil {
		preview.Object.Content.Metadata = map[string]interface{}{}
	}
	preview.Object.Content.Metadata["refs"] = preview.Object.Attributes["refs"]
	preview.Object.Content.Metadata["layout"] = "multi"
}

func refPreviewDescriptors(formatType format.FormatType, refs []format.RelatedRef) []map[string]interface{} {
	descriptors := format.DescribeRefs(formatType, refs)
	result := make([]map[string]interface{}, 0, len(descriptors))
	for index, descriptor := range descriptors {
		key := strings.TrimSpace(descriptor.Key)
		if key == "" {
			key = strings.TrimSpace(descriptor.Role)
		}
		if key == "" {
			key = fmt.Sprintf("%d", index)
		}
		item := map[string]interface{}{
			"key":      key,
			"path":     descriptor.Path,
			"role":     descriptor.Role,
			"label":    descriptor.Label,
			"required": descriptor.Required,
			"primary":  descriptor.Primary,
		}
		if descriptor.DataType != "" {
			item["data_type"] = descriptor.DataType
		}
		if descriptor.Format != "" {
			item["format"] = string(descriptor.Format)
		}
		if descriptor.Extension != "" {
			item["extension"] = descriptor.Extension
		}
		hint := previewHintForRefDescriptor(&descriptor, descriptor.Path)
		item["preview_data_type"] = hint.DataType
		item["preview_format"] = string(hint.Format)
		item["preview_material"] = hint.Material
		item["preview_renderer"] = hint.Renderer
		item["previewable"] = hint.Previewable
		result = append(result, item)
	}
	return result
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
	if resolved := objectcontent.ResolveContainerAttributeChildrenForPreview(stringAttribute(attrs, "format"), interfaceSlice(containerAttrs["children"])); resolved != nil && len(resolved.Children) > 0 {
		for _, child := range resolved.Children {
			if len(child) > 0 && containerChildNameMatches(child, childName) {
				return child
			}
		}
	}
	for _, item := range interfaceSlice(containerAttrs["children"]) {
		child := rawMapAttribute(item)
		if len(child) > 0 && containerChildNameMatches(child, childName) {
			return child
		}
	}
	return map[string]interface{}{"name": childName}
}

func mapToContainerChildInfo(child map[string]interface{}) format.ContainerChildInfo {
	if child == nil {
		child = map[string]interface{}{}
	}
	name := strings.TrimSpace(commonJSON.InterfaceString(child["name"]))
	childKind := containerChildKindFromMap(child)
	dataType := strings.TrimSpace(commonJSON.InterfaceString(child["data_type"]))
	properties := make(map[string]interface{}, len(child))
	for key, value := range child {
		switch key {
		case "name", "child_kind", "data_type", "format", "layout", "refs", "row_count", "column_count", "has_header":
			continue
		default:
			properties[key] = value
		}
	}
	rowCountValue := commonJSON.InterfaceInt64(child["row_count"])
	var rowCount *int64
	if rowCountValue > 0 {
		rowCount = &rowCountValue
	}
	columnCountValue := int(commonJSON.InterfaceInt64(child["column_count"]))
	var columnCount *int
	if columnCountValue > 0 {
		columnCount = &columnCountValue
	}
	var hasHeader *bool
	if _, ok := child["has_header"]; ok {
		value := commonJSON.InterfaceBool(child["has_header"])
		hasHeader = &value
	}
	return format.ContainerChildInfo{
		Name:        name,
		ChildKind:   childKind,
		DataType:    dataType,
		Format:      format.FormatType(strings.TrimSpace(commonJSON.InterfaceString(child["format"]))),
		Layout:      strings.TrimSpace(commonJSON.InterfaceString(child["layout"])),
		Refs:        containerChildRefsFromMap(child),
		RowCount:    rowCount,
		ColumnCount: columnCount,
		HasHeader:   hasHeader,
		Properties:  properties,
	}
}

func containerChildKindFromMap(child map[string]interface{}) string {
	return strings.TrimSpace(commonJSON.InterfaceString(child["child_kind"]))
}

func containerChildRefsFromMap(child map[string]interface{}) []format.ContainerChildRef {
	values := interfaceSlice(child["refs"])
	if len(values) == 0 {
		return nil
	}
	refs := make([]format.ContainerChildRef, 0, len(values))
	for _, value := range values {
		ref := rawMapAttribute(value)
		if len(ref) == 0 {
			continue
		}
		path := strings.TrimSpace(commonJSON.InterfaceString(ref["path"]))
		if path == "" {
			continue
		}
		refs = append(refs, format.ContainerChildRef{
			Role:      strings.TrimSpace(commonJSON.InterfaceString(ref["role"])),
			Path:      path,
			Required:  commonJSON.InterfaceBool(ref["required"]),
			Primary:   commonJSON.InterfaceBool(ref["primary"]),
			Extension: strings.TrimSpace(commonJSON.InterfaceString(ref["extension"])),
		})
	}
	return refs
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
		if field.Type == datatype.FieldTypeGeometry {
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
