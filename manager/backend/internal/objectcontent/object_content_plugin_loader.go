package objectcontent

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	commondatatype "github.com/addp/common/datatype"
	commonformat "github.com/addp/common/format"
	"github.com/addp/common/logger"
	"github.com/addp/manager/internal/models"
)

type ObjectContentPluginConfig struct {
	Name     string                     `json:"name"`
	Priority *int                       `json:"priority,omitempty"`
	Enabled  *bool                      `json:"enabled,omitempty"`
	Type     string                     `json:"type,omitempty"` // builtin | command
	Builtin  string                     `json:"builtin,omitempty"`
	Command  string                     `json:"command,omitempty"`
	Args     []string                   `json:"args,omitempty"`
	MaxBytes *int64                     `json:"max_bytes,omitempty"`
	Match    ObjectContentMatcherConfig `json:"match"`
	Metadata map[string]interface{}     `json:"metadata,omitempty"`
}

type ObjectContentMatcherConfig struct {
	Formats      []string `json:"formats,omitempty"`
	Extensions   []string `json:"extensions,omitempty"`
	ContentTypes []string `json:"content_types,omitempty"`
}

type ObjectContentPluginManifest struct {
	DefaultContentPlugins []ObjectContentPluginConfig `json:"default_content_plugins,omitempty"`
	ContentPlugins        []ObjectContentPluginConfig `json:"content_plugins,omitempty"`
}

func (c *ObjectContentPluginConfig) isEnabled() bool {
	if c.Enabled == nil {
		return true
	}
	return *c.Enabled
}

func (c *ObjectContentPluginConfig) priorityOr(defaultValue int) int {
	if c.Priority == nil {
		return defaultValue
	}
	return *c.Priority
}

func (c *ObjectContentPluginConfig) maxBytesOr(defaultValue int64) int64 {
	if c.MaxBytes == nil || *c.MaxBytes <= 0 {
		return defaultValue
	}
	return *c.MaxBytes
}

func buildBuiltinContentHandler(cfg ObjectContentPluginConfig) (ObjectContentHandler, error) {
	builtin := normalizeBuiltinContentName(cfg.Builtin)
	switch builtin {
	case "":
		return nil, fmt.Errorf("缺少 builtin")
	case models.ObjectPreviewKindUnsupported:
		return buildUnsupportedContentHandler(cfg), nil
	case models.ObjectPreviewKindContainer:
		return buildContainerContentHandler(cfg), nil
	case models.ObjectPreviewKindImage:
		return buildImageContentHandler(cfg, "image"), nil
	case models.ObjectPreviewKindVideo:
		return buildVideoContentHandler(cfg, "video"), nil
	case models.ObjectPreviewKindJSON:
		return buildJSONContentHandler(cfg), nil
	case string(commonformat.FormatParquet):
		return buildParquetContentHandler(cfg), nil
	case models.ObjectPreviewKindText:
		return buildTextContentHandler(cfg, commonformat.FormatText, models.ObjectPreviewKindText), nil
	case models.ObjectPreviewKindMarkdown:
		return buildTextContentHandler(cfg, commonformat.FormatMarkdown, models.ObjectPreviewKindMarkdown), nil
	}

	formatType := commonformat.FormatType(builtin)
	descriptor, ok := commonformat.GetFormatDescriptor(formatType)
	if !ok {
		return nil, fmt.Errorf("未知内置内容插件 %q", cfg.Builtin)
	}
	if descriptor.DataType == commondatatype.DataTypeMedia {
		mediaKind := mediaKindFromDescriptor(descriptor)
		switch mediaKind {
		case "image":
			return buildImageContentHandler(cfg, string(descriptor.Format)), nil
		case "video":
			return buildVideoContentHandler(cfg, string(descriptor.Format)), nil
		}
	}
	if descriptor.DataType == commondatatype.DataTypeDocument && commonformat.DescriptorHasContentReader(descriptor, commonformat.ContentReaderRawContent) {
		return buildRawDocumentContentHandler(cfg, descriptor.Format), nil
	}
	return nil, fmt.Errorf("内置内容插件 %q 没有对应的对象内容处理器", cfg.Builtin)
}

func registerDefaultBuiltinContentHandlers(registry *ObjectContentRegistry) {
	registerBuiltinContentHandlers(registry, fallbackBuiltinContentPlugins())
}

func registerBuiltinContentHandlers(registry *ObjectContentRegistry, configs []ObjectContentPluginConfig) {
	for _, cfg := range configs {
		handler, err := buildBuiltinContentHandler(cfg)
		if err != nil {
			logger.L().Warn("数据预览: 默认内置内容插件初始化失败", "builtin", cfg.Builtin, "error", err)
			continue
		}
		registry.Register(handler)
	}
}

func fallbackBuiltinContentPlugins() []ObjectContentPluginConfig {
	return []ObjectContentPluginConfig{
		{Name: "builtin:content-pdf", Type: "builtin", Builtin: models.ObjectPreviewKindPDF},
		{Name: "builtin:content-docx", Type: "builtin", Builtin: models.ObjectPreviewKindDOCX},
		{Name: "builtin:content-pptx", Type: "builtin", Builtin: models.ObjectPreviewKindPPTX},
		{Name: "builtin:content-wps", Type: "builtin", Builtin: models.ObjectPreviewKindWPS},
		{Name: "builtin:content-image", Type: "builtin", Builtin: models.ObjectPreviewKindImage},
		{Name: "builtin:content-video", Type: "builtin", Builtin: models.ObjectPreviewKindVideo},
		{Name: "builtin:content-parquet", Type: "builtin", Builtin: string(commonformat.FormatParquet), Metadata: map[string]interface{}{"row_limit": defaultParquetRowLimit}},
		{Name: "builtin:content-json", Type: "builtin", Builtin: models.ObjectPreviewKindJSON},
		{Name: "builtin:content-container", Type: "builtin", Builtin: models.ObjectPreviewKindContainer},
		{Name: "builtin:content-markdown", Type: "builtin", Builtin: models.ObjectPreviewKindMarkdown},
		{Name: "builtin:content-text", Type: "builtin", Builtin: models.ObjectPreviewKindText},
		{Name: "builtin:content-unsupported", Type: "builtin", Builtin: models.ObjectPreviewKindUnsupported},
	}
}

func normalizeBuiltinContentName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

func buildRawDocumentContentHandler(cfg ObjectContentPluginConfig, formatType commonformat.FormatType) ObjectContentHandler {
	kind := string(formatType)
	return &rawDocumentContentHandler{
		baseContentHandler: baseContentHandler{
			name:     cfg.Name,
			priority: cfg.priorityOr(defaultBuiltinContentPriority(kind)),
			matcher:  descriptorObjectContentMatcher(cfg.Match, formatType, nil, nil),
		},
		maxBytes:    cfg.maxBytesOr(defaultRawDocumentMaxBytes(formatType)),
		contentKind: kind,
		emptyTip:    fmt.Sprintf("%s 文件内容为空或无法读取", contentKindLabel(kind)),
	}
}

func buildImageContentHandler(cfg ObjectContentPluginConfig, mediaKind string) ObjectContentHandler {
	return &imageContentHandler{
		baseContentHandler: baseContentHandler{
			name:     cfg.Name,
			priority: cfg.priorityOr(defaultBuiltinContentPriority(models.ObjectPreviewKindImage)),
			matcher:  mediaObjectContentMatcher(cfg.Match, mediaKind),
		},
		maxBytes: cfg.maxBytesOr(maxImagePreviewBytes),
	}
}

func buildVideoContentHandler(cfg ObjectContentPluginConfig, mediaKind string) ObjectContentHandler {
	return &mediaStreamContentHandler{
		baseContentHandler: baseContentHandler{
			name:     cfg.Name,
			priority: cfg.priorityOr(defaultBuiltinContentPriority(models.ObjectPreviewKindVideo)),
			matcher:  mediaObjectContentMatcher(cfg.Match, mediaKind),
		},
		kind: models.ObjectPreviewKindVideo,
	}
}

func buildJSONContentHandler(cfg ObjectContentPluginConfig) ObjectContentHandler {
	return &jsonContentHandler{
		baseContentHandler: baseContentHandler{
			name:     cfg.Name,
			priority: cfg.priorityOr(defaultBuiltinContentPriority(models.ObjectPreviewKindJSON)),
			matcher:  descriptorObjectContentMatcher(cfg.Match, commonformat.FormatJSON, nil, nil),
		},
		maxBytes: cfg.maxBytesOr(maxJSONPreviewBytes),
		kind:     models.ObjectPreviewKindJSON,
	}
}

func buildContainerContentHandler(cfg ObjectContentPluginConfig) ObjectContentHandler {
	return &containerContentHandler{
		baseContentHandler: baseContentHandler{
			name:     cfg.Name,
			priority: cfg.priorityOr(defaultBuiltinContentPriority(models.ObjectPreviewKindContainer)),
			matcher:  containerObjectContentMatcher(cfg.Match),
		},
		maxBytes: cfg.maxBytesOr(maxContainerPreviewBytes),
	}
}

func buildTextContentHandler(cfg ObjectContentPluginConfig, formatType commonformat.FormatType, kind string) ObjectContentHandler {
	formats, extensions, contentTypes := descriptorMatcherDefaults(formatType)
	if formatType == commonformat.FormatText {
		contentTypes = append(contentTypes, "text/")
		extensions = nil
	}
	return &textContentHandler{
		baseContentHandler: baseContentHandler{
			name:     cfg.Name,
			priority: cfg.priorityOr(defaultBuiltinContentPriority(kind)),
			matcher: newObjectContentMatcher(
				normalizeFormatsOrDefault(cfg.Match.Formats, formats),
				normalizeExtensionsOrDefault(cfg.Match.Extensions, extensions),
				normalizeContentTypesOrDefault(cfg.Match.ContentTypes, contentTypes),
			),
		},
		maxBytes:   cfg.maxBytesOr(maxTextPreviewBytes),
		kind:       kind,
		formatType: formatType,
	}
}

func buildParquetContentHandler(cfg ObjectContentPluginConfig) ObjectContentHandler {
	handler := &parquetContentHandler{
		baseContentHandler: baseContentHandler{
			name:     cfg.Name,
			priority: cfg.priorityOr(defaultBuiltinContentPriority(string(commonformat.FormatParquet))),
			matcher:  descriptorObjectContentMatcher(cfg.Match, commonformat.FormatParquet, nil, nil),
		},
		maxBytes: cfg.maxBytesOr(maxParquetPreviewBytes),
		rowLimit: defaultParquetRowLimit,
	}
	handler.rowLimit = metadataInt(cfg.Metadata, "row_limit", handler.rowLimit)
	return handler
}

func buildUnsupportedContentHandler(cfg ObjectContentPluginConfig) ObjectContentHandler {
	return &unsupportedContentHandler{
		baseContentHandler: baseContentHandler{
			name:     cfg.Name,
			priority: cfg.priorityOr(defaultBuiltinContentPriority(models.ObjectPreviewKindUnsupported)),
			matcher:  newObjectContentMatcher(normalizeFormats(cfg.Match.Formats), normalizeExtensions(cfg.Match.Extensions), normalizeContentTypes(cfg.Match.ContentTypes)),
		},
		maxBytes: cfg.maxBytesOr(maxTextPreviewBytes),
	}
}

func defaultBuiltinContentPriority(kind string) int {
	switch kind {
	case models.ObjectPreviewKindPDF:
		return 80
	case models.ObjectPreviewKindDOCX:
		return 75
	case models.ObjectPreviewKindWPS, models.ObjectPreviewKindPPTX:
		return 74
	case models.ObjectPreviewKindImage:
		return 70
	case models.ObjectPreviewKindVideo:
		return 68
	case string(commonformat.FormatParquet):
		return 63
	case models.ObjectPreviewKindJSON:
		return 60
	case models.ObjectPreviewKindContainer:
		return 58
	case models.ObjectPreviewKindMarkdown:
		return 55
	case models.ObjectPreviewKindUnsupported:
		return -100
	default:
		return 0
	}
}

func defaultRawDocumentMaxBytes(formatType commonformat.FormatType) int64 {
	switch formatType {
	case commonformat.FormatPDF:
		return maxPDFPreviewBytes
	case commonformat.FormatDOCX:
		return maxDOCXPreviewBytes
	case commonformat.FormatWPS:
		return maxWPSPreviewBytes
	case commonformat.FormatPPTX:
		return maxPPTXPreviewBytes
	default:
		return maxTextPreviewBytes
	}
}

func mediaKindFromDescriptor(descriptor commonformat.FormatDescriptor) string {
	if formatDescriptorMatchesMediaKind(descriptor, "image") {
		return "image"
	}
	if formatDescriptorMatchesMediaKind(descriptor, "video") {
		return "video"
	}
	return ""
}

func normalizeExtensions(values []string) []string {
	normalized := make([]string, 0, len(values))
	for _, ext := range values {
		trimmed := strings.ToLower(strings.TrimSpace(ext))
		if trimmed == "" {
			continue
		}
		if !strings.HasPrefix(trimmed, ".") {
			trimmed = "." + trimmed
		}
		normalized = append(normalized, trimmed)
	}
	return normalized
}

func normalizeFormats(values []string) []string {
	normalized := make([]string, 0, len(values))
	for _, formatName := range values {
		trimmed := strings.ToLower(strings.TrimSpace(formatName))
		if trimmed != "" {
			normalized = append(normalized, trimmed)
		}
	}
	return normalized
}

func normalizeContentTypes(values []string) []string {
	normalized := make([]string, 0, len(values))
	for _, ct := range values {
		trimmed := strings.ToLower(strings.TrimSpace(ct))
		if trimmed != "" {
			normalized = append(normalized, trimmed)
		}
	}
	return normalized
}

func descriptorObjectContentMatcher(match ObjectContentMatcherConfig, formatType commonformat.FormatType, extraFormats []commonformat.FormatType, extraContentTypes []string) objectContentMatcher {
	formats, extensions, contentTypes := descriptorMatcherDefaults(formatType)
	for _, extraFormat := range extraFormats {
		extraFormatName, extraExtensions, extraMIMETypes := descriptorMatcherDefaults(extraFormat)
		formats = append(formats, extraFormatName...)
		extensions = append(extensions, extraExtensions...)
		contentTypes = append(contentTypes, extraMIMETypes...)
	}
	contentTypes = append(contentTypes, extraContentTypes...)
	return newObjectContentMatcher(
		normalizeFormatsOrDefault(match.Formats, formats),
		normalizeExtensionsOrDefault(match.Extensions, extensions),
		normalizeContentTypesOrDefault(match.ContentTypes, contentTypes),
	)
}

func mediaObjectContentMatcher(match ObjectContentMatcherConfig, mediaKind string) objectContentMatcher {
	formats, extensions, contentTypes := mediaMatcherDefaults(mediaKind)
	return newObjectContentMatcher(
		normalizeFormatsOrDefault(match.Formats, formats),
		normalizeExtensionsOrDefault(match.Extensions, extensions),
		normalizeContentTypesOrDefault(match.ContentTypes, contentTypes),
	)
}

func mediaMatcherDefaults(mediaKind string) ([]string, []string, []string) {
	mediaKind = strings.ToLower(strings.TrimSpace(mediaKind))
	formats := make([]string, 0)
	extensions := make([]string, 0)
	contentTypes := make([]string, 0)
	for _, descriptor := range commonformat.ListFormatDescriptors() {
		if descriptor.DataType != commondatatype.DataTypeMedia {
			continue
		}
		if mediaKind != "" && !formatDescriptorMatchesMediaKind(descriptor, mediaKind) {
			continue
		}
		formats = append(formats, string(descriptor.Format))
		extensions = append(extensions, descriptor.Identification.Extensions...)
		contentTypes = append(contentTypes, descriptor.Identification.MimeTypes...)
	}
	return formats, extensions, contentTypes
}

func formatDescriptorMatchesMediaKind(descriptor commonformat.FormatDescriptor, mediaKind string) bool {
	if mediaKind == "" {
		return true
	}
	if strings.EqualFold(string(descriptor.Format), mediaKind) {
		return true
	}
	prefix := mediaKind + "/"
	for _, mimeType := range descriptor.Identification.MimeTypes {
		normalized := strings.ToLower(strings.TrimSpace(mimeType))
		if strings.HasPrefix(normalized, prefix) {
			return true
		}
	}
	return false
}

func descriptorMatcherDefaults(formatType commonformat.FormatType) ([]string, []string, []string) {
	descriptor, ok := commonformat.GetFormatDescriptor(formatType)
	if !ok {
		return []string{string(formatType)}, nil, nil
	}
	formats := []string{string(descriptor.Format)}
	return formats, descriptor.Identification.Extensions, descriptor.Identification.MimeTypes
}

func containerObjectContentMatcher(match ObjectContentMatcherConfig) objectContentMatcher {
	formats := make([]string, 0)
	extensions := make([]string, 0)
	contentTypes := make([]string, 0)
	for _, descriptor := range commonformat.ListFormatDescriptors() {
		if descriptor.DataType != commondatatype.DataTypeContainer || !descriptor.Providers.ContainerInfo {
			continue
		}
		formats = append(formats, string(descriptor.Format))
		extensions = append(extensions, descriptor.Identification.Extensions...)
		contentTypes = append(contentTypes, descriptor.Identification.MimeTypes...)
	}
	contentTypes = append(contentTypes, "application/octet-stream")
	sort.Strings(formats)
	sort.Strings(extensions)
	sort.Strings(contentTypes)
	return newObjectContentMatcher(
		normalizeFormatsOrDefault(match.Formats, formats),
		normalizeExtensionsOrDefault(match.Extensions, extensions),
		normalizeContentTypesOrDefault(match.ContentTypes, contentTypes),
	)
}

func normalizeExtensionsOrDefault(values, fallback []string) []string {
	if len(values) > 0 {
		return normalizeExtensions(values)
	}
	return normalizeExtensions(fallback)
}

func normalizeFormatsOrDefault(values, fallback []string) []string {
	if len(values) > 0 {
		return normalizeFormats(values)
	}
	return normalizeFormats(fallback)
}

func normalizeContentTypesOrDefault(values, fallback []string) []string {
	if len(values) > 0 {
		return normalizeContentTypes(values)
	}
	return normalizeContentTypes(fallback)
}

func metadataInt(meta map[string]interface{}, key string, fallback int) int {
	if meta == nil {
		return fallback
	}
	value, ok := meta[key]
	if !ok {
		return fallback
	}
	switch v := value.(type) {
	case float64:
		if v > 0 {
			return int(v)
		}
	case int:
		if v > 0 {
			return v
		}
	case int64:
		if v > 0 {
			return int(v)
		}
	case json.Number:
		if parsed, err := v.Int64(); err == nil && parsed > 0 {
			return int(parsed)
		}
	}
	return fallback
}

// LoadObjectContentPlugins 从 manifest 文件加载对象内容插件配置。
func LoadObjectContentPlugins(registry *ObjectContentRegistry, manifestSpec string) {
	if registry == nil {
		return
	}
	paths := splitManifestPaths(manifestSpec)
	if len(paths) == 0 {
		registerDefaultBuiltinContentHandlers(registry)
		return
	}
	for _, path := range paths {
		loadContentPluginsFromManifest(registry, path)
	}
}

func splitManifestPaths(spec string) []string {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return nil
	}
	normalized := strings.ReplaceAll(spec, ";", ",")
	parts := strings.Split(normalized, ",")
	paths := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			paths = append(paths, trimmed)
		}
	}
	return paths
}

func loadContentPluginsFromManifest(registry *ObjectContentRegistry, path string) {
	raw, err := os.ReadFile(path)
	if err != nil {
		logger.L().Warn("数据预览: 读取插件清单失败", "path", path, "error", err)
		return
	}

	var manifest ObjectContentPluginManifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		logger.L().Warn("数据预览: 解析插件清单失败", "path", path, "error", err)
		return
	}

	defaultConfigs := manifest.DefaultContentPlugins
	if len(defaultConfigs) == 0 {
		defaultConfigs = fallbackBuiltinContentPlugins()
	}
	registerBuiltinContentHandlers(registry, defaultConfigs)
	for _, cfg := range manifest.ContentPlugins {
		loadContentPluginConfig(registry, cfg, path)
	}
}

func loadContentPluginConfig(registry *ObjectContentRegistry, cfg ObjectContentPluginConfig, source string) {
	if !cfg.isEnabled() {
		if name := builtinContentHandlerName(cfg); name != "" {
			registry.Unregister(name)
		}
		logger.L().Info("数据预览: 跳过已禁用内容插件", "config", source, "name", cfg.Name)
		return
	}

	var handler ObjectContentHandler
	switch strings.TrimSpace(strings.ToLower(cfg.Type)) {
	case "", "builtin":
		name := strings.TrimSpace(strings.ToLower(cfg.Builtin))
		if cfg.Name == "" {
			cfg.Name = fmt.Sprintf("builtin:content:%s", name)
		}
		h, err := buildBuiltinContentHandler(cfg)
		if err != nil {
			logger.L().Warn("数据预览: 内置内容插件初始化失败", "config", source, "error", err)
			return
		}
		handler = h
	case "command":
		if cfg.Name == "" {
			cfg.Name = "command:content"
		}
		command := strings.TrimSpace(cfg.Command)
		if command == "" {
			logger.L().Warn("数据预览: 内容插件缺少 command", "config", source, "name", cfg.Name)
			return
		}
		handler = &commandContentHandler{
			baseContentHandler: baseContentHandler{
				name:     cfg.Name,
				priority: cfg.priorityOr(50),
				matcher:  newObjectContentMatcher(normalizeFormats(cfg.Match.Formats), normalizeExtensions(cfg.Match.Extensions), normalizeContentTypes(cfg.Match.ContentTypes)),
			},
			command:  command,
			args:     cfg.Args,
			maxBytes: cfg.maxBytesOr(maxTextPreviewBytes),
		}
	default:
		logger.L().Warn("数据预览: 不支持的内容插件类型", "config", source, "type", cfg.Type)
		return
	}

	if handler == nil {
		return
	}

	registry.Register(handler)
	logger.L().Info("数据预览: 注册内容插件成功", "config", source, "plugin", handler.Name(), "priority", handler.Priority())
}

func builtinContentHandlerName(cfg ObjectContentPluginConfig) string {
	name := strings.TrimSpace(cfg.Name)
	if name != "" {
		return name
	}
	builtin := normalizeBuiltinContentName(cfg.Builtin)
	if builtin == "" {
		return ""
	}
	return fmt.Sprintf("builtin:content-%s", builtin)
}
