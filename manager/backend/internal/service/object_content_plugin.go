package service

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/addp/common/format"
	"github.com/addp/common/format/excel"
	"github.com/addp/common/format/geojson"
	"github.com/addp/common/format/sqlite"
	"github.com/addp/common/logger"
	"github.com/addp/manager/internal/models"
	_ "github.com/mattn/go-sqlite3"
	"github.com/xuri/excelize/v2"
)

// ObjectContentRequest 描述对象内容的上下文信息。
type ObjectContentRequest struct {
	Bucket      string
	Path        string
	Extension   string
	ContentType string
	Size        int64
}

// ObjectContentProvider 用于按需读取对象数据，limit <= 0 表示使用默认限制。
type ObjectContentProvider func(limit int64) ([]byte, bool, error)

// ObjectStreamProvider 用于获取对象的流式读取器（用于大文件场景，如 SQLite）
type ObjectStreamProvider func() (io.ReadCloser, error)

// ObjectContentHandler 定义对象内容插件需要实现的接口。
type ObjectContentHandler interface {
	Name() string
	Priority() int
	Matches(req *ObjectContentRequest) bool
	Handle(ctx context.Context, req *ObjectContentRequest, fetcher ObjectContentProvider) (*models.ObjectPreviewContent, bool, error)
}

// StreamableContentHandler 扩展接口，支持流式处理大文件（如 SQLite）
type StreamableContentHandler interface {
	ObjectContentHandler
	HandleStream(ctx context.Context, req *ObjectContentRequest, streamer ObjectStreamProvider) (*models.ObjectPreviewContent, bool, error)
}

// ObjectSiblingStreamProvider 用于获取同前缀下其他对象的流式读取器（针对多文件场景，如 Shapefile）
type ObjectSiblingStreamProvider func(path string) (io.ReadCloser, error)

// CompositeStreamableContentHandler 扩展接口，支持一次处理同一资源下的多个对象文件
type CompositeStreamableContentHandler interface {
	ObjectContentHandler
	HandleCompositeStream(ctx context.Context, req *ObjectContentRequest, baseStreamer ObjectStreamProvider, siblingProvider ObjectSiblingStreamProvider) (*models.ObjectPreviewContent, bool, error)
}

// ObjectContentRegistry 负责根据优先级注册和解析对象内容插件。
type ObjectContentRegistry struct {
	handlers []ObjectContentHandler
}

func NewObjectContentRegistry() *ObjectContentRegistry {
	return &ObjectContentRegistry{
		handlers: make([]ObjectContentHandler, 0),
	}
}

func (r *ObjectContentRegistry) Register(handler ObjectContentHandler) {
	if handler == nil {
		return
	}
	r.handlers = append(r.handlers, handler)
	// 按优先级排序（大优先）
	for i := len(r.handlers) - 1; i > 0; i-- {
		if r.handlers[i].Priority() > r.handlers[i-1].Priority() {
			r.handlers[i], r.handlers[i-1] = r.handlers[i-1], r.handlers[i]
			continue
		}
		break
	}
}

func (r *ObjectContentRegistry) Resolve(req *ObjectContentRequest) ObjectContentHandler {
	if r == nil || req == nil {
		return nil
	}
	for _, handler := range r.handlers {
		if handler.Matches(req) {
			return handler
		}
	}
	return nil
}

// ------------ 辅助函数 ------------

func buildPreviewMetadata(req *ObjectContentRequest, limit int64) map[string]interface{} {
	if req == nil {
		return map[string]interface{}{}
	}
	metadata := map[string]interface{}{
		"path":       req.Path,
		"size_bytes": req.Size,
		"limit":      limit,
	}
	if req.Extension != "" {
		metadata["extension"] = req.Extension
	}
	if req.ContentType != "" {
		metadata["content_type"] = req.ContentType
	}
	return metadata
}

func buildLimitExceededMessage(kind string, req *ObjectContentRequest, limit int64) string {
	sizeInMB := float64(req.Size) / (1024 * 1024)
	limitInMB := float64(limit) / (1024 * 1024)
	label := contentKindLabel(kind)
	return fmt.Sprintf("%s 文件超过预览限制 (%.2f MB / %.2f MB)，建议下载查看", label, sizeInMB, limitInMB)
}

func contentKindLabel(kind string) string {
	labels := map[string]string{
		"pdf":      "PDF",
		"docx":     "DOCX",
		"wps":      "WPS",
		"pptx":     "PPTX",
		"image":    "图片",
		"json":     "JSON",
		"geojson":  "GeoJSON",
		"excel":    "Excel",
		"sqlite":   "SQLite",
		"text":     "文本",
		"markdown": "Markdown",
	}
	if label, ok := labels[kind]; ok {
		return label
	}
	return strings.ToUpper(kind)
}

// ------------ 匹配器 ------------

type objectContentMatcher struct {
	extensions   []string
	contentTypes []string
}

func newObjectContentMatcher(exts, contentTypes []string) objectContentMatcher {
	normalizedExts := make([]string, 0, len(exts))
	for _, ext := range exts {
		trimmed := strings.ToLower(strings.TrimSpace(ext))
		if trimmed == "" {
			continue
		}
		if !strings.HasPrefix(trimmed, ".") {
			trimmed = "." + trimmed
		}
		normalizedExts = append(normalizedExts, trimmed)
	}

	normalizedTypes := make([]string, 0, len(contentTypes))
	for _, ct := range contentTypes {
		trimmed := strings.ToLower(strings.TrimSpace(ct))
		if trimmed != "" {
			normalizedTypes = append(normalizedTypes, trimmed)
		}
	}

	return objectContentMatcher{
		extensions:   normalizedExts,
		contentTypes: normalizedTypes,
	}
}

func (m objectContentMatcher) matches(req *ObjectContentRequest) bool {
	if req == nil {
		return false
	}
	extMatched := len(m.extensions) == 0
	extLower := strings.ToLower(strings.TrimSpace(req.Extension))
	if len(m.extensions) > 0 {
		for _, ext := range m.extensions {
			if extLower == ext {
				extMatched = true
				break
			}
		}
	}

	ctLower := strings.ToLower(strings.TrimSpace(req.ContentType))
	ctMatched := len(m.contentTypes) == 0 || ctLower == ""
	if !ctMatched {
		if isGenericContentType(ctLower) {
			ctLower = ""
			ctMatched = true
		}
	}
	if ctMatched && ctLower == "" {
		return extMatched
	}
	if !ctMatched {
		for _, ct := range m.contentTypes {
			target := strings.ToLower(strings.TrimSpace(ct))
			if target == "" {
				continue
			}
			if ctLower == target || strings.Contains(ctLower, target) || strings.Contains(target, ctLower) {
				ctMatched = true
				break
			}
		}
	}

	return extMatched && ctMatched
}

// ------------ 内置处理器基类 ------------

type baseContentHandler struct {
	name     string
	priority int
	matcher  objectContentMatcher
}

func (h *baseContentHandler) Name() string {
	return h.name
}

func (h *baseContentHandler) Priority() int {
	return h.priority
}

func (h *baseContentHandler) Matches(req *ObjectContentRequest) bool {
	return h.matcher.matches(req)
}

// ------------ 内置处理器实现 ------------

type binaryBase64Handler struct {
	baseContentHandler
	maxBytes    int64
	contentKind string
	emptyTip    string
}

func (h *binaryBase64Handler) Handle(ctx context.Context, req *ObjectContentRequest, fetcher ObjectContentProvider) (*models.ObjectPreviewContent, bool, error) {
	limit := h.maxBytes
	data, truncated, err := fetcher(limit)
	if err != nil {
		return nil, false, err
	}
	metadata := buildPreviewMetadata(req, limit)
	if truncated {
		message := buildLimitExceededMessage(h.contentKind, req, limit)
		return &models.ObjectPreviewContent{
			Kind:      h.contentKind,
			Text:      message,
			Truncated: true,
			Metadata:  metadata,
		}, true, nil
	}
	if len(data) == 0 {
		message := h.emptyTip
		if message == "" {
			message = fmt.Sprintf("%s 文件内容为空或无法读取", contentKindLabel(h.contentKind))
		}
		return &models.ObjectPreviewContent{
			Kind:     h.contentKind,
			Text:     message,
			Metadata: metadata,
		}, false, nil
	}

	encoded := base64.StdEncoding.EncodeToString(data)
	return &models.ObjectPreviewContent{
		Kind:     h.contentKind,
		Data:     encoded,
		Encoding: "base64",
		Metadata: metadata,
	}, false, nil
}

type imageContentHandler struct {
	baseContentHandler
	maxBytes int64
}

func (h *imageContentHandler) Handle(ctx context.Context, req *ObjectContentRequest, fetcher ObjectContentProvider) (*models.ObjectPreviewContent, bool, error) {
	data, truncated, err := fetcher(h.maxBytes)
	if err != nil {
		return nil, false, err
	}

	metadata := buildPreviewMetadata(req, h.maxBytes)
	if truncated {
		message := buildLimitExceededMessage("image", req, h.maxBytes)
		return &models.ObjectPreviewContent{
			Kind:      "image",
			Text:      message,
			Truncated: true,
			Metadata:  metadata,
		}, true, nil
	}

	if len(data) == 0 {
		return &models.ObjectPreviewContent{
			Kind:     "image",
			Text:     "图片内容为空或无法读取",
			Metadata: metadata,
		}, false, nil
	}

	encoded := base64.StdEncoding.EncodeToString(data)
	content := &models.ObjectPreviewContent{
		Kind:      "image",
		ImageData: encoded,
		Encoding:  "base64",
		Metadata:  metadata,
	}

	return content, false, nil
}

type jsonContentHandler struct {
	baseContentHandler
	maxBytes int64
	kind     string
}

func removeUTF8BOM(data []byte) []byte {
	if len(data) >= 3 && data[0] == 0xEF && data[1] == 0xBB && data[2] == 0xBF {
		return data[3:]
	}
	return data
}

func (h *jsonContentHandler) Handle(ctx context.Context, req *ObjectContentRequest, fetcher ObjectContentProvider) (*models.ObjectPreviewContent, bool, error) {
	data, truncated, err := fetcher(h.maxBytes)
	if err != nil {
		return nil, false, err
	}
	clean := removeUTF8BOM(data)
	var parsed interface{}
	if err := json.Unmarshal(clean, &parsed); err != nil {
		return &models.ObjectPreviewContent{
			Kind:      "text",
			Text:      string(data),
			Truncated: truncated,
		}, truncated, nil
	}
	if h.kind == "geojson" {
		if preview, err := buildGeoJSONPreview(ctx, clean, parsed); err == nil {
			preview.Truncated = truncated
			return preview, truncated, nil
		}
		return &models.ObjectPreviewContent{
			Kind:    "geojson",
			Text:    string(clean),
			GeoJSON: parsed,
		}, truncated, nil
	}
	return &models.ObjectPreviewContent{
		Kind: "json",
		Text: string(clean),
		JSON: parsed,
	}, truncated, nil
}

func buildGeoJSONPreview(ctx context.Context, data []byte, parsed interface{}) (*models.ObjectPreviewContent, error) {
	opts := format.DefaultParseOptions()
	parser := geojson.NewParser(opts)

	// GeoJSON parser still uses old interface (backward compatible)
	schema, err := parser.ParseSchema(ctx, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}

	sampleRecords, _ := parser.ReadRecords(ctx, bytes.NewReader(data), 0, 10)
	metaMap, _ := parser.ExtractMetadata(ctx, bytes.NewReader(data))

	metadata := make(map[string]interface{})
	if metaMap != nil {
		for k, v := range metaMap {
			metadata[k] = v
		}
	}
	if len(sampleRecords) > 0 {
		metadata["sample_records"] = sampleRecords
	}
	if schema != nil {
		metadata["columns"] = convertFormatColumns(schema)
		if schema.GeometryField != nil {
			metadata["geometry_field"] = *schema.GeometryField
		}
		if schema.GeometryType != nil {
			metadata["geometry_type"] = *schema.GeometryType
		}
		if schema.SpatialRefSys != nil {
			metadata["spatial_ref_sys"] = *schema.SpatialRefSys
		}
		if schema.RecordCount != nil {
			metadata["record_count"] = *schema.RecordCount
		}
	}

	return &models.ObjectPreviewContent{
		Kind:     "geojson",
		Text:     string(data),
		GeoJSON:  parsed,
		Metadata: metadata,
	}, nil
}

func convertFormatColumns(schema *format.Schema) []map[string]interface{} {
	if schema == nil {
		return nil
	}
	columns := make([]map[string]interface{}, 0, len(schema.Fields))
	for _, field := range schema.Fields {
		col := map[string]interface{}{
			"name":     field.Name,
			"type":     string(field.Type),
			"nullable": field.Nullable,
		}
		if field.Type == format.FieldTypeGeometry {
			col["type"] = "geometry"
		}
		columns = append(columns, col)
	}
	return columns
}

func buildExcelPreviewFromAnalysis(analysis *excel.WorkbookAnalysis) map[string]interface{} {
	if analysis == nil {
		return map[string]interface{}{
			"default_sheet": "",
			"active_sheet":  "",
			"sheets":        []map[string]interface{}{},
			"summary":       map[string]interface{}{},
		}
	}

	sheets := make([]map[string]interface{}, 0, len(analysis.Sheets))
	for _, sheet := range analysis.Sheets {
		sheetMap := map[string]interface{}{
			"name":           sheet.Name,
			"index":          sheet.Index,
			"row_count":      sheet.RowCount,
			"column_count":   sheet.ColumnCount,
			"has_header":     sheet.HasHeader,
			"headers":        sheet.Headers,
			"column_types":   sheet.ColumnTypes,
			"rows":           sheet.SampleRows,
			"rows_truncated": sheet.RowsTruncated,
		}
		sheets = append(sheets, sheetMap)
	}

	summary := make(map[string]interface{}, len(analysis.Summary))
	for k, v := range analysis.Summary {
		summary[k] = v
	}

	return map[string]interface{}{
		"default_sheet": analysis.DefaultSheet,
		"active_sheet":  analysis.ActiveSheet,
		"sheets":        sheets,
		"summary":       summary,
	}
}

func boolValue(v interface{}) bool {
	switch val := v.(type) {
	case bool:
		return val
	case string:
		return strings.EqualFold(val, "true")
	case int:
		return val != 0
	case int64:
		return val != 0
	default:
		return false
	}
}

type excelContentHandler struct {
	baseContentHandler
	maxBytes    int64
	sheetLimit  int
	rowLimit    int
	columnLimit int
}

const (
	defaultExcelSheetLimit  = 5
	defaultExcelRowLimit    = 50
	defaultExcelColumnLimit = 50
	maxExcelPreviewBytes    = 15 * 1024 * 1024
	excelMaxReadRows        = 200
	excelSampleRowsFallback = 20
	excelTypeDetectLimit    = 100
)

func (h *excelContentHandler) Handle(ctx context.Context, req *ObjectContentRequest, fetcher ObjectContentProvider) (*models.ObjectPreviewContent, bool, error) {
	data, truncated, err := fetcher(h.maxBytes)
	if err != nil {
		return nil, false, err
	}

	if len(data) == 0 {
		return &models.ObjectPreviewContent{
			Kind: "excel",
			Text: "Excel 文件为空或无法读取",
		}, truncated, nil
	}

	if truncated {
		return &models.ObjectPreviewContent{
			Kind:      "excel",
			Text:      buildLimitExceededMessage("excel", req, h.maxBytes),
			Truncated: true,
		}, true, nil
	}

	workbook, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		return &models.ObjectPreviewContent{
			Kind: "excel",
			Text: fmt.Sprintf("解析 Excel 失败: %v", err),
		}, false, nil
	}
	defer workbook.Close()

	sheetNames := workbook.GetSheetList()
	options := excel.Options{
		SheetLimit:      h.effectiveSheetLimit(len(sheetNames)),
		RowLimit:        h.effectiveRowLimit(),
		ColumnLimit:     h.effectiveColumnLimit(),
		TypeDetectLimit: excelTypeDetectLimit,
	}
	analysis, err := excel.Analyze(ctx, workbook, &options)
	if err != nil {
		return &models.ObjectPreviewContent{
			Kind: "excel",
			Text: fmt.Sprintf("解析 Excel 失败: %v", err),
		}, false, nil
	}

	preview := buildExcelPreviewFromAnalysis(analysis)
	previewTruncated := boolValue(analysis.Summary["sheets_truncated"]) || boolValue(analysis.Summary["rows_truncated"])

	metadata := map[string]interface{}{
		"sheet_limit":  h.sheetLimit,
		"row_limit":    h.rowLimit,
		"column_limit": h.columnLimit,
	}
	if req != nil {
		metadata["size_bytes"] = req.Size
		metadata["path"] = req.Path
	}

	return &models.ObjectPreviewContent{
		Kind:      "excel",
		JSON:      preview,
		Metadata:  metadata,
		Truncated: previewTruncated,
	}, truncated || previewTruncated, nil
}

func (h *excelContentHandler) effectiveSheetLimit(total int) int {
	limit := h.sheetLimit
	if limit <= 0 || limit > total {
		return total
	}
	return limit
}

func (h *excelContentHandler) effectiveRowLimit() int {
	if h.rowLimit <= 0 {
		return defaultExcelRowLimit
	}
	return h.rowLimit
}

func (h *excelContentHandler) effectiveColumnLimit() int {
	if h.columnLimit <= 0 {
		return defaultExcelColumnLimit
	}
	return h.columnLimit
}

type textContentHandler struct {
	baseContentHandler
	maxBytes int64
	kind     string
}

func (h *textContentHandler) Handle(ctx context.Context, req *ObjectContentRequest, fetcher ObjectContentProvider) (*models.ObjectPreviewContent, bool, error) {
	data, truncated, err := fetcher(h.maxBytes)
	if err != nil {
		return nil, false, err
	}
	kind := h.kind
	if kind == "" {
		kind = "text"
	}
	return &models.ObjectPreviewContent{
		Kind:      kind,
		Text:      string(data),
		Truncated: truncated,
	}, truncated, nil
}

type sqliteContentHandler struct {
	baseContentHandler
	maxBytes   int64
	tableLimit int
	rowLimit   int
}

const (
	defaultSQLiteTableLimit = 5
	defaultSQLiteRowLimit   = 20
)

// HandleStream 实现流式处理 SQLite 文件（推荐，避免大文件内存占用）
func (h *sqliteContentHandler) HandleStream(ctx context.Context, req *ObjectContentRequest, streamer ObjectStreamProvider) (*models.ObjectPreviewContent, bool, error) {
	// 创建临时文件
	tmpFile, err := os.CreateTemp("", "sqlite-preview-*.db")
	if err != nil {
		return nil, false, fmt.Errorf("创建临时 SQLite 文件失败: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer func() {
		tmpFile.Close()
		os.Remove(tmpPath)
	}()

	// 流式下载到临时文件（无需加载到内存）
	reader, err := streamer()
	if err != nil {
		return nil, false, fmt.Errorf("获取对象流失败: %w", err)
	}
	defer reader.Close()

	written, err := io.Copy(tmpFile, reader)
	if err != nil {
		return nil, false, fmt.Errorf("写入 SQLite 临时文件失败: %w", err)
	}

	if written == 0 {
		return &models.ObjectPreviewContent{
			Kind: "sqlite",
			Text: "SQLite 文件为空或无法读取",
		}, false, nil
	}

	if err := tmpFile.Close(); err != nil {
		return nil, false, fmt.Errorf("关闭 SQLite 临时文件失败: %w", err)
	}

	logger.L().Info("SQLite 预览: 流式下载完成", "path", req.Path, "size_bytes", written, "tmp_path", tmpPath)

	// 解析 SQLite 数据库
	return h.parseSQLiteDatabase(ctx, tmpPath, req)
}

// Handle 实现传统的字节数组处理（保持兼容性，但不推荐用于大文件）
func (h *sqliteContentHandler) Handle(ctx context.Context, req *ObjectContentRequest, fetcher ObjectContentProvider) (*models.ObjectPreviewContent, bool, error) {
	// 对于大文件，如果调用方支持流式处理，应该优先调用 HandleStream
	// 这里保留向后兼容，但会受 maxBytes 限制
	data, truncated, err := fetcher(h.maxBytes)
	if err != nil {
		return nil, false, err
	}

	if truncated {
		return &models.ObjectPreviewContent{
			Kind:      "sqlite",
			Text:      fmt.Sprintf("SQLite 文件超过 %d MB 预览限制，建议下载查看。如需预览大文件，请联系管理员。", h.maxBytes/(1024*1024)),
			Truncated: true,
		}, true, nil
	}

	if len(data) == 0 {
		return &models.ObjectPreviewContent{
			Kind: "sqlite",
			Text: "SQLite 文件为空或无法读取",
		}, false, nil
	}

	// 写入临时文件
	tmpFile, err := os.CreateTemp("", "sqlite-preview-*.db")
	if err != nil {
		return nil, false, fmt.Errorf("创建临时 SQLite 文件失败: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer func() {
		tmpFile.Close()
		os.Remove(tmpPath)
	}()

	if _, err := tmpFile.Write(data); err != nil {
		return nil, false, fmt.Errorf("写入 SQLite 临时文件失败: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return nil, false, fmt.Errorf("关闭 SQLite 临时文件失败: %w", err)
	}

	// 解析 SQLite 数据库
	return h.parseSQLiteDatabase(ctx, tmpPath, req)
}

// parseSQLiteDatabase 解析 SQLite 数据库文件并提取元数据和示例数据
func (h *sqliteContentHandler) parseSQLiteDatabase(ctx context.Context, tmpPath string, req *ObjectContentRequest) (*models.ObjectPreviewContent, bool, error) {
	dsn := fmt.Sprintf("file:%s?mode=ro&_query_only=1&_busy_timeout=5000", strings.ReplaceAll(tmpPath, "\\\\", "/"))
	logger.L().Info("SQLite 预览: 打开数据库", "dsn", dsn)

	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		logger.L().Error("SQLite 预览: 打开数据库失败", "error", err, "dsn", dsn)
		return nil, false, fmt.Errorf("打开 SQLite 数据库失败: %w", err)
	}
	defer db.Close()

	options := sqlite.DefaultOptions()
	if h.tableLimit > 0 {
		options.TableLimit = h.tableLimit
	} else {
		options.TableLimit = defaultSQLiteTableLimit
	}
	if h.rowLimit > 0 {
		options.SampleRowLimit = h.rowLimit
	} else {
		options.SampleRowLimit = defaultSQLiteRowLimit
	}

	analysis, err := sqlite.Analyze(ctx, db, &options)
	if err != nil {
		logger.L().Error("SQLite 预览: 分析数据库失败", "error", err)
		return nil, false, fmt.Errorf("提取 SQLite 元数据失败: %w", err)
	}

	metadata := buildSQLiteMetadataMap(analysis.Metadata, req)
	preview := buildSQLitePreviewFromAnalysis(analysis.Metadata)

	truncated := len(analysis.Metadata.Tables) < analysis.Metadata.TableCount || hasAnyTruncatedTable(analysis.Metadata.Tables)

	content := &models.ObjectPreviewContent{
		Kind:      "sqlite",
		JSON:      preview,
		Metadata:  metadata,
		Truncated: truncated,
	}

	return content, truncated, nil
}

func buildSQLiteMetadataMap(meta sqlite.Metadata, req *ObjectContentRequest) map[string]interface{} {
	result := map[string]interface{}{
		"version":     meta.Version,
		"page_size":   meta.PageSize,
		"page_count":  meta.PageCount,
		"table_count": meta.TableCount,
		"view_count":  meta.ViewCount,
		"index_count": meta.IndexCount,
	}
	if req != nil {
		result["size_bytes"] = req.Size
	}
	return result
}

func buildSQLitePreviewFromAnalysis(meta sqlite.Metadata) map[string]interface{} {
	tables := make([]map[string]interface{}, 0, len(meta.Tables))
	for _, tbl := range meta.Tables {
		row := map[string]interface{}{
			"name":           tbl.Name,
			"type":           tbl.Type,
			"columns":        tbl.Columns,
			"rows":           tbl.SampleRows,
			"rows_truncated": tbl.RowsTruncated,
		}
		if tbl.RowCount != nil {
			row["row_count"] = *tbl.RowCount
		}
		tables = append(tables, row)
	}

	summary := map[string]interface{}{
		"table_count":      meta.TableCount,
		"sampled_tables":   len(meta.Tables),
		"tables_truncated": meta.TableCount > len(meta.Tables),
		"rows_truncated":   hasAnyTruncatedTable(meta.Tables),
	}

	return map[string]interface{}{
		"tables":  tables,
		"summary": summary,
	}
}

func hasAnyTruncatedTable(tables []sqlite.TableInfo) bool {
	for _, tbl := range tables {
		if tbl.RowsTruncated {
			return true
		}
	}
	return false
}

type commandContentHandler struct {
	baseContentHandler
	command  string
	args     []string
	maxBytes int64
}

type commandContentPayload struct {
	Path        string `json:"path"`
	Extension   string `json:"extension"`
	ContentType string `json:"content_type"`
	Size        int64  `json:"size"`
	MaxBytes    int64  `json:"max_bytes"`
	DataBase64  string `json:"data_base64"`
	Truncated   bool   `json:"truncated"`
}

func (h *commandContentHandler) Handle(ctx context.Context, req *ObjectContentRequest, fetcher ObjectContentProvider) (*models.ObjectPreviewContent, bool, error) {
	data, truncated, err := fetcher(h.maxBytes)
	if err != nil {
		return nil, false, err
	}

	payload := commandContentPayload{
		Path:        req.Path,
		Extension:   req.Extension,
		ContentType: req.ContentType,
		Size:        req.Size,
		MaxBytes:    h.maxBytes,
		DataBase64:  base64.StdEncoding.EncodeToString(data),
		Truncated:   truncated,
	}

	input, err := json.Marshal(payload)
	if err != nil {
		return nil, false, fmt.Errorf("marshal command payload: %w", err)
	}

	cmd := execCommandContext(ctx, h.command, h.args...)
	cmd.Stdin = bytes.NewReader(input)

	output, stderr, err := runCommandCollectingOutput(cmd)
	if err != nil {
		return nil, false, fmt.Errorf("content plugin %s failed: %v (%s)", h.Name(), err, stderr)
	}
	if len(output) == 0 {
		return nil, false, fmt.Errorf("content plugin %s returned empty response", h.Name())
	}

	var preview models.ObjectPreviewContent
	if err := json.Unmarshal(output, &preview); err != nil {
		return nil, false, fmt.Errorf("content plugin %s returned invalid JSON: %w", h.Name(), err)
	}

	return &preview, preview.Truncated || truncated, nil
}

func execCommandContext(ctx context.Context, command string, args ...string) *exec.Cmd {
	if ctx != nil {
		return exec.CommandContext(ctx, command, args...)
	}
	return exec.Command(command, args...)
}

func runCommandCollectingOutput(cmd *exec.Cmd) ([]byte, string, error) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.Bytes(), stderr.String(), err
}
