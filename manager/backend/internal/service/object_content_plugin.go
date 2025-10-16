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

	"github.com/addp/common/logger"
	"github.com/addp/manager/internal/models"
	_ "github.com/mattn/go-sqlite3"
)

// ObjectContentRequest 描述对象内容的上下文信息。
type ObjectContentRequest struct {
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

type textContentHandler struct {
	baseContentHandler
	maxBytes int64
}

func (h *textContentHandler) Handle(ctx context.Context, req *ObjectContentRequest, fetcher ObjectContentProvider) (*models.ObjectPreviewContent, bool, error) {
	data, truncated, err := fetcher(h.maxBytes)
	if err != nil {
		return nil, false, err
	}
	return &models.ObjectPreviewContent{
		Kind:      "text",
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

	dsn := fmt.Sprintf("file:%s?mode=ro&_query_only=1&_busy_timeout=5000", strings.ReplaceAll(tmpPath, "\\", "/"))
	logger.L().Info("SQLite 预览: 打开数据库", "dsn", dsn)

	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		logger.L().Error("SQLite 预览: 打开数据库失败", "error", err, "dsn", dsn)
		return nil, false, fmt.Errorf("打开 SQLite 数据库失败: %w", err)
	}
	defer db.Close()

	tableLimit := h.tableLimit
	if tableLimit <= 0 {
		tableLimit = defaultSQLiteTableLimit
	}
	rowLimit := h.rowLimit
	if rowLimit <= 0 {
		rowLimit = defaultSQLiteRowLimit
	}

	var totalTables int
	countQuery := `
		SELECT COUNT(*)
		FROM sqlite_master
		WHERE lower(type) IN ('table', 'view')
		  AND name NOT LIKE 'sqlite_%'
	`
	logger.L().Info("SQLite 预览: 查询表数量", "query", countQuery)
	if err := db.QueryRowContext(ctx, countQuery).Scan(&totalTables); err != nil {
		logger.L().Error("SQLite 预览: 读取表数量失败", "error", err)
		return nil, false, fmt.Errorf("读取 SQLite 表数量失败: %w", err)
	}
	logger.L().Info("SQLite 预览: 找到表", "total_tables", totalTables, "table_limit", tableLimit, "row_limit", rowLimit)

	type tableMeta struct {
		Name string
		Type string
	}

	tableNames := make([]tableMeta, 0)
	queryTables := fmt.Sprintf(`
		SELECT name, type
		FROM sqlite_master
		WHERE lower(type) IN ('table', 'view')
		  AND name NOT LIKE 'sqlite_%%'
		ORDER BY name
		LIMIT %d
	`, tableLimit)
	rows, err := db.QueryContext(ctx, queryTables)
	if err != nil {
		return nil, false, fmt.Errorf("读取 SQLite 表信息失败: %w", err)
	}
	for rows.Next() {
		var name string
		var objectType string
		if err := rows.Scan(&name, &objectType); err != nil {
			logger.L().Warn("SQLite 插件: 读取表名失败", "error", err)
			continue
		}
		tableNames = append(tableNames, tableMeta{Name: name, Type: strings.ToLower(objectType)})
	}
	if err := rows.Err(); err != nil {
		logger.L().Warn("SQLite 插件: 遍历表名异常", "error", err)
	}
	rows.Close()

	type tableInfo struct {
		Name          string                   `json:"name"`
		Type          string                   `json:"type,omitempty"`
		Columns       []string                 `json:"columns"`
		RowCount      *int64                   `json:"row_count,omitempty"`
		Rows          []map[string]interface{} `json:"rows"`
		RowsTruncated bool                     `json:"rows_truncated,omitempty"`
	}

	tables := make([]tableInfo, 0, len(tableNames))
	anyRowsTruncated := false

	for _, entry := range tableNames {
		name := entry.Name
		escaped := escapeSqliteIdentifier(name)

		columns := make([]string, 0)
		pragmaQuery := fmt.Sprintf(`PRAGMA table_info(%s)`, escaped)
		colRows, err := db.QueryContext(ctx, pragmaQuery)
		if err != nil {
			logger.L().Warn("SQLite 插件: 读取列信息失败", "table", name, "error", err)
			continue
		}
		for colRows.Next() {
			var (
				cid       int
				colName   string
				colType   string
				notnull   int
				dfltValue interface{}
				pk        int
			)
			if err := colRows.Scan(&cid, &colName, &colType, &notnull, &dfltValue, &pk); err != nil {
				logger.L().Warn("SQLite 插件: 解析列信息失败", "table", name, "error", err)
				continue
			}
			columns = append(columns, colName)
		}
		colRows.Close()

		countQuery := fmt.Sprintf(`SELECT COUNT(*) FROM %s`, escaped)
		var rowCount int64
		var hasRowCount bool
		if err := db.QueryRowContext(ctx, countQuery).Scan(&rowCount); err != nil {
			logger.L().Warn("SQLite 插件: 统计表行数失败", "table", name, "error", err)
		} else {
			hasRowCount = true
		}

		sampleRows := make([]map[string]interface{}, 0)
		dataQuery := fmt.Sprintf(`SELECT * FROM %s LIMIT %d`, escaped, rowLimit)
		dataRows, err := db.QueryContext(ctx, dataQuery)
		if err != nil {
			logger.L().Warn("SQLite 插件: 读取示例数据失败", "table", name, "error", err)
		} else {
			columnsFromQuery, err := dataRows.Columns()
			if err != nil {
				logger.L().Warn("SQLite 插件: 获取列名失败", "table", name, "error", err)
			} else if len(columns) == 0 {
				columns = columnsFromQuery
			}

			for dataRows.Next() {
				values := make([]interface{}, len(columnsFromQuery))
				valuePtrs := make([]interface{}, len(columnsFromQuery))
				for i := range values {
					valuePtrs[i] = &values[i]
				}
				if err := dataRows.Scan(valuePtrs...); err != nil {
					logger.L().Warn("SQLite 插件: 解析行数据失败", "table", name, "error", err)
					continue
				}
				row := make(map[string]interface{}, len(columnsFromQuery))
				for i, col := range columnsFromQuery {
					row[col] = normalizeSQLiteValue(values[i])
				}
				sampleRows = append(sampleRows, row)
			}
			dataRows.Close()
		}

		rowsTruncated := false
		if hasRowCount && int64(len(sampleRows)) < rowCount {
			rowsTruncated = true
			anyRowsTruncated = true
		}

		var countPtr *int64
		if hasRowCount {
			countPtr = &rowCount
		}

		tables = append(tables, tableInfo{
			Name:          name,
			Type:          entry.Type,
			Columns:       columns,
			RowCount:      countPtr,
			Rows:          sampleRows,
			RowsTruncated: rowsTruncated,
		})
	}

	result := map[string]interface{}{
		"summary": map[string]interface{}{
			"table_count":      totalTables,
			"sampled_tables":   len(tables),
			"table_limit":      tableLimit,
			"row_limit":        rowLimit,
			"size_bytes":       req.Size,
			"tables_truncated": totalTables > len(tables),
			"rows_truncated":   anyRowsTruncated,
		},
		"tables": tables,
	}

	isTruncated := totalTables > len(tables) || anyRowsTruncated

	return &models.ObjectPreviewContent{
		Kind:      "sqlite",
		JSON:      result,
		Truncated: isTruncated,
	}, isTruncated, nil
}

func escapeSqliteIdentifier(name string) string {
	escaped := strings.ReplaceAll(name, `"`, `""`)
	return `"` + escaped + `"`
}

func normalizeSQLiteValue(value interface{}) interface{} {
	switch v := value.(type) {
	case nil:
		return nil
	case []byte:
		return string(v)
	default:
		return v
	}
}

func buildPreviewMetadata(req *ObjectContentRequest, limit int64) map[string]interface{} {
	if req == nil {
		return map[string]interface{}{
			"limit_bytes": limit,
		}
	}
	meta := map[string]interface{}{
		"limit_bytes": limit,
	}
	if req.Size > 0 {
		meta["size_bytes"] = req.Size
	}
	if req.ContentType != "" {
		meta["content_type"] = req.ContentType
	}
	if req.Path != "" {
		meta["path"] = req.Path
	}
	if req.Extension != "" {
		meta["extension"] = req.Extension
	}
	return meta
}

func buildLimitExceededMessage(kind string, req *ObjectContentRequest, limit int64) string {
	label := contentKindLabel(kind)
	limitLabel := formatByteSize(limit)
	if req != nil && req.Size > 0 {
		return fmt.Sprintf("%s大小 %s 超出预览限制（%s），建议下载查看。", label, formatByteSize(req.Size), limitLabel)
	}
	return fmt.Sprintf("%s超过预览限制（%s），建议下载查看。", label, limitLabel)
}

func contentKindLabel(kind string) string {
	switch strings.ToLower(kind) {
	case "pdf":
		return "PDF 文件"
	case "docx":
		return "DOCX 文件"
	case "pptx":
		return "PPTX 文件"
	case "image":
		return "图片"
	default:
		upper := strings.ToUpper(kind)
		if upper == "" {
			return "文件"
		}
		return upper + " 文件"
	}
}

func formatByteSize(size int64) string {
	if size <= 0 {
		return "未知大小"
	}
	const (
		kb = 1024
		mb = kb * 1024
		gb = mb * 1024
		tb = gb * 1024
	)

	var value float64
	var unit string
	switch {
	case size >= tb:
		value = float64(size) / float64(tb)
		unit = "TB"
	case size >= gb:
		value = float64(size) / float64(gb)
		unit = "GB"
	case size >= mb:
		value = float64(size) / float64(mb)
		unit = "MB"
	case size >= kb:
		value = float64(size) / float64(kb)
		unit = "KB"
	default:
		return fmt.Sprintf("%d B", size)
	}
	if value >= 100 {
		return fmt.Sprintf("%.0f %s", value, unit)
	}
	if value >= 10 {
		return fmt.Sprintf("%.1f %s", value, unit)
	}
	return fmt.Sprintf("%.2f %s", value, unit)
}

// ------------ 工具方法 ------------

func defaultExtension(path string) string {
	if idx := strings.LastIndex(path, "."); idx != -1 && idx < len(path)-1 {
		return strings.ToLower(path[idx:])
	}
	return ""
}

func normalizeExtensions(exts []string) []string {
	if len(exts) == 0 {
		return nil
	}
	result := make([]string, 0, len(exts))
	for _, ext := range exts {
		e := strings.TrimSpace(strings.ToLower(ext))
		if e == "" {
			continue
		}
		if !strings.HasPrefix(e, ".") {
			e = "." + e
		}
		result = append(result, e)
	}
	return result
}

func normalizeContentTypes(types []string) []string {
	if len(types) == 0 {
		return nil
	}
	result := make([]string, 0, len(types))
	for _, ct := range types {
		t := strings.TrimSpace(strings.ToLower(ct))
		if t != "" {
			result = append(result, t)
		}
	}
	return result
}

// ------------ 命令处理器 ------------

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
