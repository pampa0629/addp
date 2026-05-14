package objectcontent

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"unicode/utf8"

	commondataitem "github.com/addp/common/dataitem"
	"github.com/addp/common/format"
	commonJSON "github.com/addp/common/jsonmap"
	"github.com/addp/common/logger"
	"github.com/addp/common/resource"
	"github.com/addp/manager/internal/models"
)

// ObjectContentRequest 描述对象内容的上下文信息。
type ObjectContentRequest struct {
	Bucket      string
	Path        string // 目录路径（以 / 结尾），不含文件名
	Name        string // 文件名
	Format      string
	Extension   string
	ContentType string
	Size        int64
	Attributes  map[string]interface{}
	PreviewURL  string
}

// ObjectContentProvider 用于按需读取对象数据，limit <= 0 表示使用默认限制。
type ObjectContentProvider func(limit int64) ([]byte, bool, error)

// ObjectStreamProvider 用于获取对象的流式读取器（用于大文件容器场景）
type ObjectStreamProvider func() (io.ReadCloser, error)

// ObjectContentHandler 定义对象内容插件需要实现的接口。
type ObjectContentHandler interface {
	Name() string
	Priority() int
	Matches(req *ObjectContentRequest) bool
	Handle(ctx context.Context, req *ObjectContentRequest, fetcher ObjectContentProvider) (*models.ObjectPreviewContent, bool, error)
}

// StreamableContentHandler 扩展接口，支持流式处理大文件容器
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
	tableContent := isTableObjectContentRequest(req)
	for _, handler := range r.handlers {
		if tableContent && isGenericObjectContentHandler(handler) {
			return nil
		}
		if handler.Matches(req) {
			return handler
		}
	}
	return nil
}

func isTableObjectContentRequest(req *ObjectContentRequest) bool {
	if req == nil {
		return false
	}
	formatType := normalizeFileTableFormat(req.Format)
	if formatType == "" || formatType == format.FormatUnknown {
		formatType = format.DetectFormat(req.Name, nil)
	}
	if formatType == format.FormatUnknown && req.Extension != "" {
		formatType = format.DetectFormat("file"+req.Extension, nil)
	}
	if formatType != "" && formatType != format.FormatUnknown {
		if _, err := format.GetTableProvider(formatType); err == nil {
			return true
		}
	}
	contentType := strings.ToLower(strings.TrimSpace(req.ContentType))
	return contentType == "text/csv" ||
		strings.Contains(contentType, "text/csv;") ||
		strings.Contains(contentType, "comma-separated")
}

func isGenericObjectContentHandler(handler ObjectContentHandler) bool {
	if handler == nil {
		return false
	}
	switch handler.Name() {
	case "builtin:content-text", "builtin:content-unsupported":
		return true
	default:
		return false
	}
}

// ------------ 辅助函数 ------------

func buildPreviewMetadata(req *ObjectContentRequest, limit int64) map[string]interface{} {
	if req == nil {
		return map[string]interface{}{}
	}

	// 按照路径统一规范：path 是目录路径（以/结尾），name 是文件名
	metadata := map[string]interface{}{
		"bucket":     req.Bucket,
		"path":       req.Path, // 目录路径（以 / 结尾）
		"name":       req.Name, // 文件名
		"size_bytes": req.Size,
		"limit":      limit,
	}
	if req.Extension != "" {
		metadata["extension"] = req.Extension
	}
	if req.Format != "" {
		metadata["format"] = req.Format
	}
	if req.ContentType != "" {
		metadata["content_type"] = req.ContentType
	}
	return metadata
}

func setPreviewMaterial(content *models.ObjectPreviewContent, material string) {
	if content == nil || material == "" {
		return
	}
	content.PreviewMaterial = material
	if content.Metadata == nil {
		content.Metadata = map[string]interface{}{}
	}
	if _, exists := content.Metadata["preview_material"]; !exists {
		content.Metadata["preview_material"] = material
	}
}

func setFrontendRenderer(content *models.ObjectPreviewContent, renderer string) {
	if content == nil || renderer == "" {
		return
	}
	content.FrontendRenderer = renderer
	if content.Metadata == nil {
		content.Metadata = map[string]interface{}{}
	}
	if _, exists := content.Metadata["frontend_renderer"]; !exists {
		content.Metadata["frontend_renderer"] = renderer
	}
}

func decoratePreviewContent(content *models.ObjectPreviewContent) *models.ObjectPreviewContent {
	if content == nil {
		return nil
	}
	if content.PreviewMaterial == "" {
		content.PreviewMaterial = metadataString(content.Metadata, "preview_material")
	}
	if content.FrontendRenderer == "" {
		content.FrontendRenderer = metadataString(content.Metadata, "frontend_renderer")
	}
	if content.PreviewMaterial == "" {
		content.PreviewMaterial = defaultPreviewMaterial(content)
	}
	if content.FrontendRenderer == "" {
		content.FrontendRenderer = defaultFrontendRenderer(content.Kind)
	}
	if content.Metadata == nil {
		content.Metadata = map[string]interface{}{}
	}
	if content.PreviewMaterial != "" {
		if _, exists := content.Metadata["preview_material"]; !exists {
			content.Metadata["preview_material"] = content.PreviewMaterial
		}
	}
	if content.FrontendRenderer != "" {
		if _, exists := content.Metadata["frontend_renderer"]; !exists {
			content.Metadata["frontend_renderer"] = content.FrontendRenderer
		}
	}
	return content
}

func metadataString(metadata map[string]interface{}, key string) string {
	if metadata == nil {
		return ""
	}
	value, ok := metadata[key]
	if !ok {
		return ""
	}
	if str, ok := value.(string); ok {
		return strings.TrimSpace(str)
	}
	return ""
}

func defaultPreviewMaterial(content *models.ObjectPreviewContent) string {
	if content == nil {
		return ""
	}
	if content.URL != "" {
		return models.PreviewMaterialURL
	}
	if content.Encoding == "base64" && content.Data != "" {
		return models.PreviewMaterialRawBinary
	}
	if content.GeoJSON != nil {
		return models.PreviewMaterialGeoJSON
	}
	if content.JSON != nil {
		return models.PreviewMaterialJSON
	}
	if content.Text != "" {
		switch strings.ToLower(strings.TrimSpace(content.Kind)) {
		case models.ObjectPreviewKindMarkdown:
			return models.PreviewMaterialMarkdown
		default:
			return models.PreviewMaterialText
		}
	}
	return ""
}

func defaultFrontendRenderer(kind string) string {
	kind = strings.ToLower(strings.TrimSpace(kind))
	switch kind {
	case models.ObjectPreviewKindPDF:
		return models.ObjectPreviewKindPDF
	case models.ObjectPreviewKindDOCX:
		return models.ObjectPreviewKindDOCX
	case models.ObjectPreviewKindWPS:
		return models.ObjectPreviewKindWPS
	case models.ObjectPreviewKindPPTX:
		return models.ObjectPreviewKindPPTX
	case models.ObjectPreviewKindImage:
		return models.ObjectPreviewKindImage
	case models.ObjectPreviewKindJSON:
		return models.ObjectPreviewKindJSON
	case models.ObjectPreviewKindGeoJSON:
		return "map"
	case models.ObjectPreviewKindContainer:
		return models.ObjectPreviewKindContainer
	case models.ObjectPreviewKindMarkdown:
		return models.ObjectPreviewKindMarkdown
	case models.ObjectPreviewKindTable:
		return models.ObjectPreviewKindTable
	case models.ObjectPreviewKindText, models.ObjectPreviewKindUnsupported:
		return models.ObjectPreviewKindText
	default:
		return ""
	}
}

func buildLimitExceededMessage(kind string, req *ObjectContentRequest, limit int64) string {
	sizeInMB := float64(req.Size) / (1024 * 1024)
	limitInMB := float64(limit) / (1024 * 1024)
	label := contentKindLabel(kind)
	return fmt.Sprintf("%s 文件超过预览限制 (%.2f MB / %.2f MB)，建议下载查看", label, sizeInMB, limitInMB)
}

func contentKindLabel(kind string) string {
	labels := map[string]string{
		models.ObjectPreviewKindPDF:         "PDF",
		models.ObjectPreviewKindDOCX:        "DOCX",
		models.ObjectPreviewKindWPS:         "WPS",
		models.ObjectPreviewKindPPTX:        "PPTX",
		models.ObjectPreviewKindImage:       "图片",
		models.ObjectPreviewKindJSON:        "JSON",
		models.ObjectPreviewKindGeoJSON:     "GeoJSON",
		models.ObjectPreviewKindContainer:   "容器",
		models.ObjectPreviewKindText:        "文本",
		models.ObjectPreviewKindMarkdown:    "Markdown",
		models.ObjectPreviewKindUnsupported: "暂不支持的格式",
	}
	if label, ok := labels[kind]; ok {
		return label
	}
	return strings.ToUpper(kind)
}

// ------------ 匹配器 ------------

type objectContentMatcher struct {
	formats      []string
	extensions   []string
	contentTypes []string
}

func newObjectContentMatcher(formats, exts, contentTypes []string) objectContentMatcher {
	normalizedFormats := make([]string, 0, len(formats))
	for _, formatName := range formats {
		trimmed := strings.ToLower(strings.TrimSpace(formatName))
		if trimmed != "" {
			normalizedFormats = append(normalizedFormats, trimmed)
		}
	}

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
		formats:      normalizedFormats,
		extensions:   normalizedExts,
		contentTypes: normalizedTypes,
	}
}

func (m objectContentMatcher) matches(req *ObjectContentRequest) bool {
	if req == nil {
		return false
	}
	formatLower := normalizeContentFormat(req.Format)
	if formatLower != "" && len(m.formats) > 0 {
		for _, formatName := range m.formats {
			if formatLower == formatName {
				return true
			}
		}
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
		if len(m.contentTypes) > 0 && len(m.extensions) == 0 {
			return false
		}
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

func normalizeContentFormat(formatName string) string {
	normalized := strings.ToLower(strings.TrimSpace(formatName))
	switch normalized {
	case "", "unknown":
		return ""
	default:
		if formatType := format.DetectFormat("value."+normalized, nil); formatType != format.FormatUnknown {
			return string(formatType)
		}
		return normalized
	}
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

type rawDocumentContentHandler struct {
	baseContentHandler
	maxBytes    int64
	contentKind string
	emptyTip    string
}

func (h *rawDocumentContentHandler) Handle(ctx context.Context, req *ObjectContentRequest, fetcher ObjectContentProvider) (*models.ObjectPreviewContent, bool, error) {
	limit := h.maxBytes
	metadata := buildPreviewMetadata(req, limit)
	if req != nil && strings.TrimSpace(req.PreviewURL) != "" {
		content := &models.ObjectPreviewContent{
			Kind:            h.contentKind,
			URL:             strings.TrimSpace(req.PreviewURL),
			PreviewMaterial: models.PreviewMaterialURL,
			Metadata:        metadata,
		}
		setFrontendRenderer(content, h.contentKind)
		return decoratePreviewContent(content), false, nil
	}
	data, truncated, err := fetcher(limit)
	if err != nil {
		return nil, false, err
	}
	if truncated {
		message := buildLimitExceededMessage(h.contentKind, req, limit)
		return decoratePreviewContent(&models.ObjectPreviewContent{
			Kind:      h.contentKind,
			Text:      message,
			Truncated: true,
			Metadata:  metadata,
		}), true, nil
	}
	if len(data) == 0 {
		message := h.emptyTip
		if message == "" {
			message = fmt.Sprintf("%s 文件内容为空或无法读取", contentKindLabel(h.contentKind))
		}
		return decoratePreviewContent(&models.ObjectPreviewContent{
			Kind:     h.contentKind,
			Text:     message,
			Metadata: metadata,
		}), false, nil
	}

	encoded := base64.StdEncoding.EncodeToString(data)
	return decoratePreviewContent(&models.ObjectPreviewContent{
		Kind:     h.contentKind,
		Data:     encoded,
		Encoding: "base64",
		Metadata: metadata,
	}), false, nil
}

type imageContentHandler struct {
	baseContentHandler
	maxBytes int64
}

func (h *imageContentHandler) Handle(_ context.Context, req *ObjectContentRequest, fetcher ObjectContentProvider) (*models.ObjectPreviewContent, bool, error) {
	metadata := buildPreviewMetadata(req, 0)
	if req != nil && strings.TrimSpace(req.PreviewURL) != "" {
		content := &models.ObjectPreviewContent{
			Kind:            models.ObjectPreviewKindImage,
			URL:             strings.TrimSpace(req.PreviewURL),
			PreviewMaterial: models.PreviewMaterialURL,
			Metadata:        metadata,
		}
		setFrontendRenderer(content, models.ObjectPreviewKindImage)
		return decoratePreviewContent(content), false, nil
	}
	if fetcher != nil {
		limit := h.maxBytes
		if limit <= 0 {
			limit = maxImagePreviewBytes
		}
		metadata = buildPreviewMetadata(req, limit)
		data, truncated, err := fetcher(limit)
		if err != nil {
			return nil, false, err
		}
		if truncated {
			return decoratePreviewContent(&models.ObjectPreviewContent{
				Kind:      models.ObjectPreviewKindImage,
				Text:      buildLimitExceededMessage(models.ObjectPreviewKindImage, req, limit),
				Truncated: true,
				Metadata:  metadata,
			}), true, nil
		}
		if len(data) > 0 {
			content := &models.ObjectPreviewContent{
				Kind:            models.ObjectPreviewKindImage,
				Data:            base64.StdEncoding.EncodeToString(data),
				Encoding:        "base64",
				PreviewMaterial: models.PreviewMaterialRawBinary,
				Metadata:        metadata,
			}
			setFrontendRenderer(content, models.ObjectPreviewKindImage)
			return decoratePreviewContent(content), false, nil
		}
	}
	return decoratePreviewContent(&models.ObjectPreviewContent{
		Kind:     models.ObjectPreviewKindImage,
		Text:     "图片文件内容为空或无法读取",
		Metadata: metadata,
	}), false, nil
}

type jsonContentHandler struct {
	baseContentHandler
	maxBytes int64
	kind     string
}

func (h *jsonContentHandler) Matches(req *ObjectContentRequest) bool {
	if h.kind == models.ObjectPreviewKindGeoJSON && !looksLikeGeoJSONContentRequest(req) {
		return false
	}
	return h.baseContentHandler.Matches(req)
}

func removeUTF8BOM(data []byte) []byte {
	if len(data) >= 3 && data[0] == 0xEF && data[1] == 0xBB && data[2] == 0xBF {
		return data[3:]
	}
	return data
}

func (h *jsonContentHandler) Handle(ctx context.Context, req *ObjectContentRequest, fetcher ObjectContentProvider) (*models.ObjectPreviewContent, bool, error) {
	reader, readerErr := format.GetDocumentTextReader(format.FormatJSON)
	var text string
	var truncated bool
	var err error
	if readerErr == nil {
		var data []byte
		var fetchTruncated bool
		data, fetchTruncated, err = fetcher(h.maxBytes)
		if err == nil {
			var readerTruncated bool
			text, readerTruncated, err = reader.ReadDocumentText(ctx, bytes.NewReader(data), h.maxBytes, nil)
			truncated = fetchTruncated || readerTruncated
		}
	} else {
		var data []byte
		data, truncated, err = fetcher(h.maxBytes)
		text = string(removeUTF8BOM(data))
	}
	if err != nil {
		return nil, false, err
	}
	var parsed interface{}
	if err := json.Unmarshal([]byte(text), &parsed); err != nil {
		return decoratePreviewContent(&models.ObjectPreviewContent{
			Kind:      models.ObjectPreviewKindText,
			Text:      text,
			Truncated: truncated,
		}), truncated, nil
	}
	if h.kind == models.ObjectPreviewKindGeoJSON {
		if preview, err := buildGeoJSONPreview(ctx, []byte(text), parsed); err == nil {
			preview.Truncated = truncated
			return decoratePreviewContent(preview), truncated, nil
		}
		return decoratePreviewContent(&models.ObjectPreviewContent{
			Kind: models.ObjectPreviewKindJSON,
			Text: text,
			JSON: parsed,
		}), truncated, nil
	}
	return decoratePreviewContent(&models.ObjectPreviewContent{
		Kind: models.ObjectPreviewKindJSON,
		Text: text,
		JSON: parsed,
	}), truncated, nil
}

func looksLikeGeoJSONContentRequest(req *ObjectContentRequest) bool {
	if req == nil {
		return false
	}
	formatName := strings.ToLower(strings.TrimSpace(req.Format))
	if formatName == models.ObjectPreviewKindGeoJSON || formatName == "geo+json" {
		return true
	}
	extension := strings.ToLower(strings.TrimSpace(req.Extension))
	if extension == ".geojson" {
		return true
	}
	contentType := strings.ToLower(strings.TrimSpace(strings.Split(req.ContentType, ";")[0]))
	return contentType == "application/geo+json" || contentType == "application/vnd.geo+json"
}

func buildGeoJSONPreview(ctx context.Context, data []byte, parsed interface{}) (*models.ObjectPreviewContent, error) {
	opts := format.DefaultParseOptions()
	provider, err := format.GetTableProvider(format.FormatJSON)
	if err != nil {
		return nil, err
	}

	// 使用格式解析器提取 table 语义
	tableInfo, err := provider.DescribeTable(ctx, bytes.NewReader(data), opts)
	if err != nil {
		return nil, err
	}

	// 读取预览数据（前10条记录）
	sampleRecords, _ := provider.SampleTable(ctx, bytes.NewReader(data), 0, 10, opts)

	// 构建元数据
	metadata := make(map[string]interface{})

	// 从 TableInfo 提取元数据
	if tableInfo != nil {
		// 添加字段信息
		columns := make([]map[string]interface{}, 0, len(tableInfo.Fields))
		for _, field := range tableInfo.Fields {
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
		metadata["columns"] = columns

		// 添加行数
		if tableInfo.RowCount != nil {
			metadata["record_count"] = *tableInfo.RowCount
			metadata["feature_count"] = *tableInfo.RowCount
		}

		// 从 Extensions 中提取空间信息
		for _, ext := range tableInfo.Extensions {
			if spatialInfo, ok := ext.(*format.SpatialInfo); ok {
				metadata["geometry_field"] = spatialInfo.GeometryColumn
				metadata["geometry_type"] = spatialInfo.GeometryType
				metadata["geometry_types"] = []string{spatialInfo.GeometryType}
				if spatialInfo.SRID != 0 {
					metadata["spatial_ref_sys"] = fmt.Sprintf("EPSG:%d", spatialInfo.SRID)
				}
				break
			}
		}
	}

	// 添加示例记录
	if len(sampleRecords) > 0 {
		metadata["sample_records"] = sampleRecords
	}

	return decoratePreviewContent(&models.ObjectPreviewContent{
		Kind:     models.ObjectPreviewKindGeoJSON,
		Text:     string(data),
		GeoJSON:  parsed,
		Metadata: metadata,
	}), nil
}

func emptyContainerPreview(formatName string) map[string]interface{} {
	return buildContainerPreview(formatName, "", "", []map[string]interface{}{}, map[string]interface{}{})
}

type containerPreviewChildren struct {
	Children              []map[string]interface{}
	RawCount              int
	VisibleCount          int
	IgnoredCount          int
	GroupedItemCount      int
	GroupedComponentCount int
	FilteredCount         int
	Resolved              bool
}

func buildContainerPreviewFromAttributes(attrs map[string]interface{}, sizeBytes int64) map[string]interface{} {
	containerAttrs := commonJSON.Section(attrs, "type_info.container")
	if len(containerAttrs) == 0 {
		return nil
	}
	formatName := strings.ToLower(strings.TrimSpace(stringAttribute(attrs, "format")))
	formatAttrs := commonJSON.Section(attrs, "format_info."+formatName)

	children := interfaceSlice(containerAttrs["children"])
	if len(children) == 0 {
		return nil
	}

	if resolved := resolveContainerAttributeChildrenForPreview(formatName, children); resolved != nil && len(resolved.Children) > 0 {
		summary := buildContainerSummary(formatName, containerAttrs, formatAttrs, sizeBytes)
		applyContainerChildrenSummary(summary, resolved)
		defaultChild := containerDefaultChild(formatName, formatAttrs, resolved.Children)
		return buildContainerPreview(formatName, defaultChild, defaultChild, resolved.Children, summary)
	}

	previewChildren := make([]map[string]interface{}, 0, len(children))
	for index, item := range children {
		child := rawMapAttribute(item)
		if len(child) == 0 {
			continue
		}
		name := commonJSON.InterfaceString(child["name"])
		tableName := commonJSON.InterfaceString(child["table"])
		kind := commonJSON.InterfaceString(child["kind"])
		columnCount := commonJSON.InterfaceInt64(child["column_count"])
		childMap := map[string]interface{}{
			"key":          containerChildKey(name, tableName, index),
			"name":         name,
			"label":        name,
			"table":        tableName,
			"index":        index,
			"kind":         kind,
			"data_type":    commonJSON.InterfaceString(child["data_type"]),
			"row_count":    commonJSON.InterfaceInt64(child["row_count"]),
			"column_count": columnCount,
		}
		if _, ok := child["has_header"]; ok {
			childMap["has_header"] = commonJSON.InterfaceBool(child["has_header"])
		}
		if childMap["label"] == "" {
			childMap["label"] = tableName
		}
		if childMap["data_type"] == "" {
			childMap["data_type"] = string(format.FormatDataTypeTable)
		}
		previewChildren = append(previewChildren, childMap)
	}
	if len(previewChildren) == 0 {
		return nil
	}

	summary := buildContainerSummary(formatName, containerAttrs, formatAttrs, sizeBytes)
	applyContainerChildrenSummary(summary, &containerPreviewChildren{
		Children:      previewChildren,
		RawCount:      len(children),
		VisibleCount:  len(previewChildren),
		FilteredCount: len(children) - len(previewChildren),
	})
	defaultChild := containerDefaultChild(formatName, formatAttrs, previewChildren)
	return buildContainerPreview(formatName, defaultChild, defaultChild, previewChildren, summary)
}

func resolveContainerAttributeChildrenForPreview(formatName string, children []interface{}) *containerPreviewChildren {
	if len(children) == 0 {
		return nil
	}
	candidates := make([]commondataitem.Candidate, 0, len(children))
	rawCount := len(children)
	skippedCount := 0
	for _, item := range children {
		child := rawMapAttribute(item)
		if len(child) == 0 {
			skippedCount++
			continue
		}
		kind := strings.ToLower(strings.TrimSpace(commonJSON.InterfaceString(child["kind"])))
		if kind == "directory" {
			skippedCount++
			continue
		}
		if kind != "" && kind != "file" && kind != "object" && kind != "entry" && kind != "multi" {
			continue
		}
		pathValue := commonJSON.InterfaceString(child["path"])
		name := commonJSON.InterfaceString(child["name"])
		if pathValue == "" {
			pathValue = name
		}
		var sizePtr *int64
		if size := commonJSON.InterfaceInt64(child["uncompressed_size"]); size > 0 {
			sizePtr = &size
		}
		props := map[string]interface{}{}
		for key, value := range child {
			props[key] = value
		}
		candidates = append(candidates, commondataitem.Candidate{
			Path:       pathValue,
			Name:       name,
			SizeBytes:  sizePtr,
			Properties: props,
		})
	}
	if len(candidates) == 0 {
		return nil
	}
	resolved, err := commondataitem.ResolveItems(commondataitem.ResolveInput{
		ScopeKind:  commondataitem.ScopeKindContainer,
		Candidates: candidates,
		Options: commondataitem.ResolveOptions{
			IncludeIgnored: true,
		},
	})
	if err != nil || resolved == nil {
		return nil
	}
	previewChildren := make([]map[string]interface{}, 0, len(resolved.Items))
	groupedItemCount := 0
	groupedComponentCount := 0
	for index, item := range resolved.Items {
		childInfo := commondataitem.ContainerChildInfoFromResolvedItem(item)
		if item.Organization == commondataitem.OrganizationMulti {
			groupedItemCount++
			if len(item.ComponentList) > 1 {
				groupedComponentCount += len(item.ComponentList) - 1
			}
		}
		previewChildren = append(previewChildren, containerChildPreviewMap(childInfo, index))
	}
	if len(previewChildren) == 0 {
		return nil
	}
	return &containerPreviewChildren{
		Children:              previewChildren,
		RawCount:              rawCount,
		VisibleCount:          len(previewChildren),
		IgnoredCount:          len(resolved.Ignored),
		GroupedItemCount:      groupedItemCount,
		GroupedComponentCount: groupedComponentCount,
		FilteredCount:         skippedCount + len(resolved.Ignored),
		Resolved:              true,
	}
}

func containerChildPreviewMap(childInfo format.ContainerChildInfo, index int) map[string]interface{} {
	key := containerChildKey(childInfo.Name, commonJSON.InterfaceString(childInfo.Properties["table"]), index)
	child := map[string]interface{}{
		"key":          key,
		"name":         childInfo.Name,
		"label":        childInfo.Name,
		"kind":         childInfo.Kind,
		"data_type":    childInfo.DataType,
		"format":       string(childInfo.Format),
		"organization": childInfo.Organization,
	}
	if childInfo.RowCount != nil {
		child["row_count"] = *childInfo.RowCount
	}
	if childInfo.ColumnCount != nil {
		child["column_count"] = *childInfo.ColumnCount
	}
	if childInfo.HasHeader != nil {
		child["has_header"] = *childInfo.HasHeader
	}
	if len(childInfo.Components) > 0 {
		child["components"] = containerChildComponentDescriptors(childInfo)
	}
	for key, value := range childInfo.Properties {
		child[key] = value
	}
	if len(childInfo.Components) > 0 {
		child["components"] = containerChildComponentDescriptors(childInfo)
	}
	return child
}

func containerChildComponentDescriptors(childInfo format.ContainerChildInfo) []map[string]interface{} {
	refs := make([]resource.ComponentRef, 0, len(childInfo.Components))
	for _, component := range childInfo.Components {
		role := resource.ResourceRoleComponent
		if component.Primary {
			role = resource.ResourceRoleMain
		}
		refs = append(refs, resource.ComponentRef{
			ResourceRef:   resource.NewResourceRef(component.Path, role),
			ComponentRole: component.Role,
			Required:      component.Required,
		})
	}
	descriptors := format.DescribeComponents(childInfo.Format, refs)
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
		if descriptor.PreviewDataType != "" {
			item["preview_data_type"] = descriptor.PreviewDataType
		}
		if descriptor.PreviewFormat != "" {
			item["preview_format"] = string(descriptor.PreviewFormat)
		}
		if descriptor.PreviewMaterial != "" {
			item["preview_material"] = descriptor.PreviewMaterial
		}
		if descriptor.PreviewRenderer != "" {
			item["preview_renderer"] = descriptor.PreviewRenderer
		}
		if descriptor.Previewable != nil {
			item["previewable"] = *descriptor.Previewable
		}
		result = append(result, item)
	}
	return result
}

func buildContainerPreview(formatName, defaultChild, activeChild string, children []map[string]interface{}, summary map[string]interface{}) map[string]interface{} {
	return map[string]interface{}{
		"format":        formatName,
		"default_child": defaultChild,
		"active_child":  activeChild,
		"children":      children,
		"summary":       summary,
	}
}

func buildContainerSummary(formatName string, containerAttrs, formatAttrs map[string]interface{}, sizeBytes int64) map[string]interface{} {
	summary := map[string]interface{}{}
	for key, value := range formatAttrs {
		summary[key] = value
	}
	if childCount := commonJSON.InterfaceInt64(containerAttrs["child_count"]); childCount > 0 {
		summary["child_count"] = childCount
	}
	if sampledChildren := commonJSON.InterfaceInt64(formatAttrs["sampled_children"]); sampledChildren > 0 {
		summary["sampled_children"] = sampledChildren
	}
	if sizeBytes > 0 {
		summary["size_bytes"] = sizeBytes
	}
	return summary
}

func applyContainerChildrenSummary(summary map[string]interface{}, resolved *containerPreviewChildren) {
	if summary == nil || resolved == nil {
		return
	}
	if resolved.RawCount > 0 {
		summary["raw_child_count"] = resolved.RawCount
		if _, exists := summary["child_count"]; !exists {
			summary["child_count"] = resolved.RawCount
		}
	}
	summary["visible_child_count"] = resolved.VisibleCount
	summary["sampled_children"] = resolved.VisibleCount
	if resolved.FilteredCount > 0 {
		summary["filtered_child_count"] = resolved.FilteredCount
	}
	if resolved.IgnoredCount > 0 {
		summary["ignored_child_count"] = resolved.IgnoredCount
	}
	if resolved.GroupedItemCount > 0 {
		summary["grouped_item_count"] = resolved.GroupedItemCount
	}
	if resolved.GroupedComponentCount > 0 {
		summary["grouped_component_count"] = resolved.GroupedComponentCount
	}
	if resolved.Resolved {
		summary["organization_resolved"] = true
	}
}

func containerDefaultChild(formatName string, formatAttrs map[string]interface{}, children []map[string]interface{}) string {
	if value := strings.TrimSpace(commonJSON.InterfaceString(formatAttrs["default_child"])); value != "" {
		return value
	}
	if len(children) == 0 {
		return ""
	}
	return commonJSON.InterfaceString(children[0]["key"])
}

func containerChildKey(name, tableName string, index int) string {
	if name != "" {
		return name
	}
	if tableName != "" {
		return tableName
	}
	return fmt.Sprintf("%d", index)
}

func isContainerObjectContentFormat(formatName string) bool {
	formatType := normalizeFileTableFormat(formatName)
	descriptor, ok := format.GetFormatDescriptor(formatType)
	return ok && descriptor.DataType == format.FormatDataTypeContainer && descriptor.Providers.ContainerInfo
}

type textContentHandler struct {
	baseContentHandler
	maxBytes   int64
	kind       string
	formatType format.FormatType
}

func (h *textContentHandler) Handle(ctx context.Context, req *ObjectContentRequest, fetcher ObjectContentProvider) (*models.ObjectPreviewContent, bool, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	data, fetchTruncated, err := fetcher(h.maxBytes)
	if err != nil {
		return nil, false, err
	}
	text := string(removeUTF8BOM(data))
	truncated := fetchTruncated
	if h.formatType != "" {
		if reader, readerErr := format.GetDocumentTextReader(h.formatType); readerErr == nil {
			var readerTruncated bool
			text, readerTruncated, err = reader.ReadDocumentText(ctx, bytes.NewReader(data), h.maxBytes, nil)
			if err != nil {
				return nil, false, err
			}
			truncated = fetchTruncated || readerTruncated
		}
	}
	text = string(removeUTF8BOM([]byte(text)))
	kind := h.kind
	if kind == "" {
		kind = models.ObjectPreviewKindText
	}
	return decoratePreviewContent(&models.ObjectPreviewContent{
		Kind:      kind,
		Text:      text,
		Truncated: truncated,
	}), truncated, nil
}

type unsupportedContentHandler struct {
	baseContentHandler
	maxBytes int64
}

func (h *unsupportedContentHandler) Handle(ctx context.Context, req *ObjectContentRequest, fetcher ObjectContentProvider) (*models.ObjectPreviewContent, bool, error) {
	data, truncated, err := fetcher(h.maxBytes)
	if err != nil {
		return nil, false, err
	}

	metadata := buildPreviewMetadata(req, h.maxBytes)
	metadata["preview_material"] = "raw_binary"
	metadata["probe_truncated"] = truncated

	if len(data) == 0 {
		return decoratePreviewContent(&models.ObjectPreviewContent{
			Kind:     models.ObjectPreviewKindUnsupported,
			Text:     "文件内容为空或无法读取",
			Metadata: metadata,
		}), false, nil
	}

	if isLikelyTextContent(data) {
		metadata["preview_material"] = models.ObjectPreviewKindText
		return decoratePreviewContent(&models.ObjectPreviewContent{
			Kind:      models.ObjectPreviewKindText,
			Text:      string(removeUTF8BOM(data)),
			Truncated: truncated,
			Metadata:  metadata,
		}), truncated, nil
	}

	return decoratePreviewContent(&models.ObjectPreviewContent{
		Kind:     models.ObjectPreviewKindUnsupported,
		Text:     "暂不支持该文件类型的在线预览，请下载后查看。",
		Metadata: metadata,
	}), false, nil
}

func isLikelyTextContent(data []byte) bool {
	data = removeUTF8BOM(data)
	if len(data) == 0 {
		return true
	}
	if bytes.IndexByte(data, 0) >= 0 {
		return false
	}
	if !utf8.Valid(data) {
		return false
	}

	controlCount := 0
	runeCount := 0
	for len(data) > 0 {
		r, size := utf8.DecodeRune(data)
		if r == utf8.RuneError && size == 1 {
			return false
		}
		runeCount++
		if r < 0x20 {
			switch r {
			case '\t', '\n', '\r', '\f':
			default:
				controlCount++
			}
		}
		data = data[size:]
	}
	if runeCount == 0 {
		return true
	}
	return controlCount*100 <= runeCount
}

type containerContentHandler struct {
	baseContentHandler
	maxBytes int64
}

// HandleStream streams large file-backed containers into a temporary file before provider parsing.
func (h *containerContentHandler) HandleStream(ctx context.Context, req *ObjectContentRequest, streamer ObjectStreamProvider) (*models.ObjectPreviewContent, bool, error) {
	tmpFile, err := os.CreateTemp("", "container-preview-*.bin")
	if err != nil {
		return nil, false, fmt.Errorf("创建临时容器文件失败: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer func() {
		tmpFile.Close()
		os.Remove(tmpPath)
	}()

	reader, err := streamer()
	if err != nil {
		return nil, false, fmt.Errorf("获取对象流失败: %w", err)
	}
	defer reader.Close()

	written, err := io.Copy(tmpFile, reader)
	if err != nil {
		return nil, false, fmt.Errorf("写入容器临时文件失败: %w", err)
	}

	if written == 0 {
		return decoratePreviewContent(&models.ObjectPreviewContent{
			Kind: models.ObjectPreviewKindContainer,
			Text: "容器文件为空或无法读取",
		}), false, nil
	}

	if err := tmpFile.Close(); err != nil {
		return nil, false, fmt.Errorf("关闭容器临时文件失败: %w", err)
	}

	logger.L().Info("容器预览: 流式下载完成", "path", req.Path+req.Name, "size_bytes", written, "tmp_path", tmpPath)

	return h.parseContainer(ctx, tmpPath, req)
}

func (h *containerContentHandler) Handle(ctx context.Context, req *ObjectContentRequest, fetcher ObjectContentProvider) (*models.ObjectPreviewContent, bool, error) {
	data, truncated, err := fetcher(h.maxBytes)
	if err != nil {
		return nil, false, err
	}

	if truncated {
		return decoratePreviewContent(&models.ObjectPreviewContent{
			Kind:      models.ObjectPreviewKindContainer,
			Text:      fmt.Sprintf("容器文件超过 %d MB 预览限制，建议下载查看。如需预览大文件，请联系管理员。", h.maxBytes/(1024*1024)),
			Truncated: true,
		}), true, nil
	}

	if len(data) == 0 {
		return decoratePreviewContent(&models.ObjectPreviewContent{
			Kind: models.ObjectPreviewKindContainer,
			Text: "容器文件为空或无法读取",
		}), false, nil
	}

	tmpFile, err := os.CreateTemp("", "container-preview-*.bin")
	if err != nil {
		return nil, false, fmt.Errorf("创建临时容器文件失败: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer func() {
		tmpFile.Close()
		os.Remove(tmpPath)
	}()

	if _, err := tmpFile.Write(data); err != nil {
		return nil, false, fmt.Errorf("写入容器临时文件失败: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return nil, false, fmt.Errorf("关闭容器临时文件失败: %w", err)
	}

	return h.parseContainer(ctx, tmpPath, req)
}

// parseContainer uses common/format container providers to build the container index.
func (h *containerContentHandler) parseContainer(ctx context.Context, tmpPath string, req *ObjectContentRequest) (*models.ObjectPreviewContent, bool, error) {
	formatType := objectContentContainerFormat(req)
	provider, err := format.GetContainerInfoProvider(formatType)
	if err != nil {
		return nil, false, fmt.Errorf("获取 %s 容器 provider 失败: %w", formatType, err)
	}

	file, err := os.Open(tmpPath)
	if err != nil {
		return nil, false, fmt.Errorf("打开临时容器文件失败: %w", err)
	}
	info, err := provider.DescribeContainer(ctx, file, &format.ParseOptions{
		ExtraParams: map[string]interface{}{
			format.ContainerChildLimitParam: 0,
			format.ContainerRowLimitParam:   0,
		},
	})
	_ = file.Close()
	if err != nil {
		logger.L().Error("容器预览: 分析失败", "error", err, "format", formatType)
		return nil, false, fmt.Errorf("提取 %s 容器元数据失败: %w", formatType, err)
	}

	metadata := buildContainerMetadataMap(info, req, formatType)
	info = resolveContainerChildrenForPreview(info)
	preview := buildContainerPreviewFromContainerInfo(info, string(formatType))
	truncated := containerInfoTruncated(info)

	content := &models.ObjectPreviewContent{
		Kind:      models.ObjectPreviewKindContainer,
		JSON:      preview,
		Metadata:  metadata,
		Truncated: truncated,
	}

	return decoratePreviewContent(content), truncated, nil
}

func buildContainerMetadataMap(info *format.ContainerInfo, req *ObjectContentRequest, formatType format.FormatType) map[string]interface{} {
	result := map[string]interface{}{"format": string(formatType)}
	if req != nil {
		result["size_bytes"] = req.Size
	}
	if info == nil {
		return result
	}
	for key, value := range info.FormatInfo {
		result[key] = value
	}
	result["child_count"] = info.ChildCount
	result["sampled_children"] = len(info.Children)
	return result
}

func resolveContainerChildrenForPreview(info *format.ContainerInfo) *format.ContainerInfo {
	if info == nil || len(info.Children) == 0 {
		return info
	}
	candidates := make([]commondataitem.Candidate, 0, len(info.Children))
	rawCount := info.ChildCount
	if rawCount <= 0 {
		rawCount = len(info.Children)
	}
	skippedCount := 0
	for _, child := range info.Children {
		kind := strings.ToLower(strings.TrimSpace(child.Kind))
		if kind == "directory" {
			skippedCount++
			continue
		}
		if kind != "" && kind != "file" && kind != "object" && kind != "entry" && kind != "multi" {
			continue
		}
		pathValue := commonJSON.InterfaceString(child.Properties["path"])
		if pathValue == "" {
			pathValue = child.Name
		}
		var sizePtr *int64
		if size := commonJSON.InterfaceInt64(child.Properties["uncompressed_size"]); size > 0 {
			sizePtr = &size
		}
		props := map[string]interface{}{}
		for key, value := range child.Properties {
			props[key] = value
		}
		if child.Format != "" {
			props["format"] = string(child.Format)
		}
		candidates = append(candidates, commondataitem.Candidate{
			Path:       pathValue,
			Name:       child.Name,
			SizeBytes:  sizePtr,
			Properties: props,
		})
	}
	if len(candidates) == 0 {
		return info
	}
	resolved, err := commondataitem.ResolveItems(commondataitem.ResolveInput{
		ScopeKind:  commondataitem.ScopeKindContainer,
		Candidates: candidates,
		Options: commondataitem.ResolveOptions{
			IncludeIgnored: true,
		},
	})
	if err != nil || resolved == nil {
		return info
	}
	next := *info
	next.Children = make([]format.ContainerChildInfo, 0, len(resolved.Items))
	groupedItemCount := 0
	groupedComponentCount := 0
	for _, item := range resolved.Items {
		if item.Organization == commondataitem.OrganizationMulti {
			groupedItemCount++
			if len(item.ComponentList) > 1 {
				groupedComponentCount += len(item.ComponentList) - 1
			}
		}
		next.Children = append(next.Children, commondataitem.ContainerChildInfoFromResolvedItem(item))
	}
	if len(next.Children) > 0 {
		next.DefaultChild = next.Children[0].Name
		if next.FormatInfo == nil {
			next.FormatInfo = map[string]interface{}{}
		}
		applyContainerChildrenSummary(next.FormatInfo, &containerPreviewChildren{
			RawCount:              rawCount,
			VisibleCount:          len(next.Children),
			IgnoredCount:          len(resolved.Ignored),
			GroupedItemCount:      groupedItemCount,
			GroupedComponentCount: groupedComponentCount,
			FilteredCount:         skippedCount + len(resolved.Ignored),
			Resolved:              true,
		})
	}
	return &next
}

func buildContainerPreviewFromContainerInfo(info *format.ContainerInfo, fallbackFormat string) map[string]interface{} {
	if info == nil {
		return emptyContainerPreview(fallbackFormat)
	}
	formatName := string(info.Format)
	if formatName == "" {
		formatName = fallbackFormat
	}
	children := make([]map[string]interface{}, 0, len(info.Children))
	for _, childInfo := range info.Children {
		children = append(children, containerChildPreviewMap(childInfo, len(children)))
	}

	summary := map[string]interface{}{
		"child_count":      info.ChildCount,
		"sampled_children": len(info.Children),
	}
	for key, value := range info.FormatInfo {
		summary[key] = value
	}
	applyContainerChildrenSummary(summary, &containerPreviewChildren{
		RawCount:     info.ChildCount,
		VisibleCount: len(info.Children),
		FilteredCount: func() int {
			if info.ChildCount > len(info.Children) {
				return info.ChildCount - len(info.Children)
			}
			return 0
		}(),
	})

	defaultChild := info.DefaultChild
	if defaultChild == "" && len(children) > 0 {
		defaultChild = commonJSON.InterfaceString(children[0]["key"])
	}
	return buildContainerPreview(formatName, defaultChild, defaultChild, children, summary)
}

func containerInfoTruncated(info *format.ContainerInfo) bool {
	if info == nil {
		return false
	}
	if info.ChildCount > len(info.Children) {
		return true
	}
	return commonJSON.InterfaceBool(info.FormatInfo["children_truncated"])
}

func objectContentContainerFormat(req *ObjectContentRequest) format.FormatType {
	if req != nil {
		for _, value := range []string{req.Format, req.Extension, req.ContentType, req.Name} {
			for _, formatType := range []format.FormatType{
				normalizeFileTableFormat(value),
				format.MIMEToFormat(value),
				format.DetectFormat(value, nil),
			} {
				if isContainerFormatType(formatType) {
					return formatType
				}
			}
		}
	}
	return firstContainerFormatType()
}

func isContainerFormatType(formatType format.FormatType) bool {
	if formatType == "" || formatType == format.FormatUnknown {
		return false
	}
	descriptor, ok := format.GetFormatDescriptor(formatType)
	return ok && descriptor.DataType == format.FormatDataTypeContainer && descriptor.Providers.ContainerInfo
}

func firstContainerFormatType() format.FormatType {
	for _, descriptor := range format.ListFormatDescriptors() {
		if descriptor.DataType == format.FormatDataTypeContainer && descriptor.Providers.ContainerInfo {
			return descriptor.Format
		}
	}
	return format.FormatUnknown
}

type commandContentHandler struct {
	baseContentHandler
	command  string
	args     []string
	maxBytes int64
}

type commandContentPayload struct {
	Path        string `json:"path"` // 目录路径（以 / 结尾）
	Name        string `json:"name"` // 文件名
	Format      string `json:"format"`
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
		Name:        req.Name,
		Format:      req.Format,
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

	return decoratePreviewContent(&preview), preview.Truncated || truncated, nil
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

// ------------ 表格文件对象内容处理器 ------------

const (
	maxParquetPreviewBytes = 200 * 1024 * 1024 // 200MB
	defaultParquetRowLimit = 50
)

type parquetContentHandler struct {
	baseContentHandler
	maxBytes int64
	rowLimit int
}

// HandleStream 流式处理表格文件（下载到临时文件后解析）
func (h *parquetContentHandler) HandleStream(ctx context.Context, req *ObjectContentRequest, streamer ObjectStreamProvider) (*models.ObjectPreviewContent, bool, error) {
	formatType := objectContentTableFormat(req)
	tmpFile, err := os.CreateTemp("", "table-preview-*"+strings.ToLower(string(formatType)))
	if err != nil {
		return nil, false, fmt.Errorf("创建临时表格文件失败: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer func() {
		tmpFile.Close()
		os.Remove(tmpPath)
	}()

	reader, err := streamer()
	if err != nil {
		return nil, false, fmt.Errorf("获取对象流失败: %w", err)
	}
	defer reader.Close()

	written, err := io.Copy(tmpFile, reader)
	if err != nil {
		return nil, false, fmt.Errorf("写入表格临时文件失败: %w", err)
	}
	if written == 0 {
		return decoratePreviewContent(&models.ObjectPreviewContent{
			Kind: models.ObjectPreviewKindTable,
			Text: "表格文件为空或无法读取",
		}), false, nil
	}
	if err := tmpFile.Close(); err != nil {
		return nil, false, fmt.Errorf("关闭表格临时文件失败: %w", err)
	}

	logger.L().Info("表格文件预览: 流式下载完成", "path", req.Path+req.Name, "format", formatType, "size_bytes", written, "tmp_path", tmpPath)

	f, err := os.Open(tmpPath)
	if err != nil {
		return nil, false, fmt.Errorf("打开表格临时文件失败: %w", err)
	}
	defer f.Close()

	opts := format.DefaultParseOptions()
	provider, err := format.GetTableProvider(formatType)
	if err != nil {
		return nil, false, fmt.Errorf("获取 %s Provider 失败: %w", formatType, err)
	}

	tableInfo, err := provider.DescribeTable(ctx, f, opts)
	if err != nil {
		return nil, false, fmt.Errorf("解析 %s Schema 失败: %w", formatType, err)
	}

	// 重新打开文件读取预览数据（ParseTableInfo 已消耗了 reader）
	f2, err := os.Open(tmpPath)
	if err != nil {
		return nil, false, fmt.Errorf("重新打开表格临时文件失败: %w", err)
	}
	defer f2.Close()

	rowLimit := int64(h.rowLimit)
	if rowLimit <= 0 {
		rowLimit = defaultParquetRowLimit
	}
	rows, err := provider.SampleTable(ctx, f2, 0, rowLimit, opts)
	if err != nil {
		return nil, false, fmt.Errorf("读取 %s 预览数据失败: %w", formatType, err)
	}

	// 构建列信息
	columns := make([]map[string]interface{}, 0, len(tableInfo.Fields))
	for _, field := range tableInfo.Fields {
		col := map[string]interface{}{
			"name":     field.Name,
			"type":     string(field.Type),
			"nullable": field.Nullable,
		}
		if field.OriginalType != "" {
			col["original_type"] = field.OriginalType
		}
		columns = append(columns, col)
	}

	totalRows := int64(0)
	if tableInfo.RowCount != nil {
		totalRows = *tableInfo.RowCount
	}
	truncated := int64(len(rows)) < totalRows

	metadata := buildPreviewMetadata(req, h.maxBytes)
	metadata["row_count"] = totalRows
	metadata["column_count"] = len(columns)

	return decoratePreviewContent(&models.ObjectPreviewContent{
		Kind: models.ObjectPreviewKindTable,
		JSON: map[string]interface{}{
			"columns":    columns,
			"rows":       rows,
			"total_rows": totalRows,
		},
		Metadata:  metadata,
		Truncated: truncated,
	}), truncated, nil
}

// Handle 回退实现（不推荐，部分表格格式需要随机访问）
func (h *parquetContentHandler) Handle(ctx context.Context, req *ObjectContentRequest, fetcher ObjectContentProvider) (*models.ObjectPreviewContent, bool, error) {
	formatType := objectContentTableFormat(req)
	data, _, err := fetcher(h.maxBytes)
	if err != nil {
		return nil, false, err
	}
	if len(data) == 0 {
		return decoratePreviewContent(&models.ObjectPreviewContent{Kind: models.ObjectPreviewKindTable, Text: "表格文件为空或无法读取"}), false, nil
	}

	opts := format.DefaultParseOptions()
	provider, err := format.GetTableProvider(formatType)
	if err != nil {
		return nil, false, fmt.Errorf("获取 %s Provider 失败: %w", formatType, err)
	}

	tableInfo, err := provider.DescribeTable(ctx, bytes.NewReader(data), opts)
	if err != nil {
		return nil, false, fmt.Errorf("解析 %s Schema 失败: %w", formatType, err)
	}

	rowLimit := int64(h.rowLimit)
	if rowLimit <= 0 {
		rowLimit = defaultParquetRowLimit
	}
	rows, err := provider.SampleTable(ctx, bytes.NewReader(data), 0, rowLimit, opts)
	if err != nil {
		return nil, false, fmt.Errorf("读取 %s 预览数据失败: %w", formatType, err)
	}

	columns := make([]map[string]interface{}, 0, len(tableInfo.Fields))
	for _, field := range tableInfo.Fields {
		columns = append(columns, map[string]interface{}{
			"name":     field.Name,
			"type":     string(field.Type),
			"nullable": field.Nullable,
		})
	}

	totalRows := int64(0)
	if tableInfo.RowCount != nil {
		totalRows = *tableInfo.RowCount
	}

	return decoratePreviewContent(&models.ObjectPreviewContent{
		Kind: models.ObjectPreviewKindTable,
		JSON: map[string]interface{}{
			"columns":    columns,
			"rows":       rows,
			"total_rows": totalRows,
		},
		Metadata:  buildPreviewMetadata(req, h.maxBytes),
		Truncated: int64(len(rows)) < totalRows,
	}), int64(len(rows)) < totalRows, nil
}

func objectContentTableFormat(req *ObjectContentRequest) format.FormatType {
	if req == nil {
		return format.FormatParquet
	}
	for _, value := range []string{req.Format, req.Extension, req.ContentType, req.Name} {
		if strings.TrimSpace(value) == "" {
			continue
		}
		if formatType := normalizeFileTableFormat(value); formatType != "" && formatType != format.FormatUnknown {
			return formatType
		}
		if formatType := format.MIMEToFormat(value); formatType != format.FormatUnknown {
			return formatType
		}
		if formatType := format.DetectFormat(value, nil); formatType != format.FormatUnknown {
			return formatType
		}
	}
	return format.FormatParquet
}
