package service

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	"github.com/addp/manager/internal/models"
)

// FileTablePreviewProvider 通用文件表预览 Provider
// 自动支持所有实现了 FileTableParser 的文件格式（CSV、Excel、Shapefile、GeoJSON、Parquet 等）
type FileTablePreviewProvider struct{}

func NewFileTablePreviewProvider() PreviewProvider {
	return &FileTablePreviewProvider{}
}

func (p *FileTablePreviewProvider) Name() string {
	return "builtin:file-table"
}

func objectStorageContentReaderForPreview(req *PreviewRequest) (plugin.ConnectionInfo, string, plugin.ContentReadableProvider, error) {
	if req == nil || req.Engine == nil {
		return nil, "", nil, fmt.Errorf("engine is required")
	}
	pl, err := plugin.Get(req.Engine.EngineType)
	if err != nil {
		return nil, "", nil, fmt.Errorf("unsupported engine type: %s", req.Engine.EngineType)
	}
	contentReader, ok := pl.(plugin.ContentReadableProvider)
	if !ok {
		return nil, "", nil, fmt.Errorf("engine %s does not implement ContentReadableProvider", req.Engine.EngineType)
	}
	connInfo := plugin.ConnectionInfo(req.Engine.ConnectionInfo)
	bucket, err := resolveBucket(plugin.GetString(connInfo, "bucket"), req.Schema)
	if err != nil {
		return nil, "", nil, err
	}
	return connInfo, bucket, contentReader, nil
}

func (p *FileTablePreviewProvider) Preview(ctx context.Context, req *PreviewRequest) (*models.TablePreview, error) {
	connInfo, bucket, contentReader, err := objectStorageContentReaderForPreview(req)
	if err != nil {
		return nil, err
	}

	fullPath := objectKeyFromPreviewRequest(req, bucket)

	// 使用共享 dataitem 口径识别格式，避免在 provider 内重复维护扩展名别名。
	formatType := p.resolveFormat(req)

	// 获取对应的 parser
	parser, err := format.GetFileTableParser(formatType)
	if err != nil {
		return nil, fmt.Errorf("no parser for format %s: %w", formatType, err)
	}

	// 构建解析选项
	opts := p.buildParseOptions(formatType)

	// Shapefile 需要特殊处理：下载所有组件文件
	if formatType == format.FormatShapefile {
		return p.previewShapefile(ctx, contentReader, connInfo, bucket, fullPath, parser, opts, req)
	}

	// 其他格式：流式处理
	return p.previewStreamable(ctx, contentReader, connInfo, bucket, fullPath, formatType, parser, opts, req)
}

// previewStreamable 处理可以流式读取的格式（CSV、Excel、GeoJSON 等）
func (p *FileTablePreviewProvider) previewStreamable(
	ctx context.Context,
	contentReader plugin.ContentReadableProvider,
	connInfo plugin.ConnectionInfo,
	bucket, fullPath string,
	formatType format.FormatType,
	parser format.FileTableParser,
	opts *format.ParseOptions,
	req *PreviewRequest,
) (*models.TablePreview, error) {
	// 获取对象流
	object, err := openObjectStorageContent(ctx, contentReader, connInfo, req.Engine.ID, bucket, fullPath)
	if err != nil {
		return nil, fmt.Errorf("failed to get object: %w", err)
	}
	defer object.Close()

	// 解析 TableInfo（获取列信息和总行数）
	tableInfo, err := parser.ParseTableInfo(ctx, object, opts)
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
	offset := req.Page * pageSize

	// 重新获取对象流（用于读取数据）
	object, err = openObjectStorageContent(ctx, contentReader, connInfo, req.Engine.ID, bucket, fullPath)
	if err != nil {
		return nil, fmt.Errorf("failed to reopen object for data: %w", err)
	}
	defer object.Close()

	// 读取分页数据
	rows, err := parser.ReadPreview(ctx, object, int64(offset), int64(pageSize), opts)
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
		Mode:            PreviewModeObject,
		Columns:         columns,
		Rows:            rows,
		Total:           int(totalCount),
		Page:            req.Page,
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
	contentReader plugin.ContentReadableProvider,
	connInfo plugin.ConnectionInfo,
	bucket, fullPath string,
	parser format.FileTableParser,
	opts *format.ParseOptions,
	req *PreviewRequest,
) (*models.TablePreview, error) {
	// 下载所有 shapefile 组件文件到临时目录
	tempDir, basePath, err := p.downloadShapefileComponents(ctx, contentReader, connInfo, req.Engine.ID, bucket, fullPath)
	if err != nil {
		return nil, fmt.Errorf("failed to download shapefile: %w", err)
	}
	defer os.RemoveAll(tempDir)

	if prjBytes, readErr := os.ReadFile(basePath + ".prj"); readErr == nil {
		opts.SpatialRefSys = strings.TrimSpace(string(prjBytes))
	}

	// 打开主文件
	shpFile, err := os.Open(basePath + ".shp")
	if err != nil {
		return nil, fmt.Errorf("failed to open shapefile: %w", err)
	}
	defer shpFile.Close()

	// 解析 TableInfo
	tableInfo, err := parser.ParseTableInfo(ctx, shpFile, opts)
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
	offset := req.Page * pageSize

	// 重新打开文件读取数据
	shpFile, err = os.Open(basePath + ".shp")
	if err != nil {
		return nil, fmt.Errorf("failed to reopen shapefile: %w", err)
	}
	defer shpFile.Close()

	// 读取分页数据
	rows, err := parser.ReadPreview(ctx, shpFile, int64(offset), int64(pageSize), opts)
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
		Mode:            PreviewModeObject,
		Columns:         columns,
		Rows:            rows,
		Total:           int(totalCount),
		Page:            req.Page,
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

// downloadShapefileComponents 下载 shapefile 的所有组件文件
func (p *FileTablePreviewProvider) downloadShapefileComponents(
	ctx context.Context,
	contentReader plugin.ContentReadableProvider,
	connInfo plugin.ConnectionInfo,
	engineID uint,
	bucket, fullPath string,
) (tempDir string, basePath string, err error) {
	// 提取基础路径（去掉扩展名）
	ext := filepath.Ext(fullPath)
	basePathRemote := fullPath
	if ext != "" {
		basePathRemote = fullPath[:len(fullPath)-len(ext)]
	}

	// 创建临时目录
	tempDir, err = os.MkdirTemp("", "shapefile-preview-*")
	if err != nil {
		return "", "", err
	}

	// 下载主文件 .shp
	shpPath := basePathRemote + ".shp"
	localShpPath := filepath.Join(tempDir, filepath.Base(shpPath))
	if err := p.downloadFile(ctx, contentReader, connInfo, engineID, bucket, shpPath, localShpPath); err != nil {
		os.RemoveAll(tempDir)
		return "", "", fmt.Errorf("failed to download .shp file: %w", err)
	}

	// 下载其他组件文件（.shx, .dbf, .prj, .cpg）
	basePathLocal := localShpPath[:len(localShpPath)-4]
	extensions := []string{".shx", ".dbf", ".prj", ".cpg"}
	for _, extension := range extensions {
		remotePath := basePathRemote + extension
		localPath := basePathLocal + extension
		// 忽略可选文件的下载错误
		_ = p.downloadFile(ctx, contentReader, connInfo, engineID, bucket, remotePath, localPath)
	}

	return tempDir, basePathLocal, nil
}

// downloadFile 从对象存储下载文件
func (p *FileTablePreviewProvider) downloadFile(ctx context.Context, contentReader plugin.ContentReadableProvider, connInfo plugin.ConnectionInfo, engineID uint, bucket, objectKey, destPath string) error {
	object, err := openObjectStorageContent(ctx, contentReader, connInfo, engineID, bucket, objectKey)
	if err != nil {
		return err
	}
	defer object.Close()

	destFile, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer destFile.Close()

	_, err = io.Copy(destFile, object)
	return err
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
	case format.FormatGeoJSON:
		return "application/geo+json"
	case format.FormatShapefile:
		return "application/x-esri-shapefile"
	default:
		return "application/octet-stream"
	}
}
