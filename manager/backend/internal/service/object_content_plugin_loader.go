package service

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

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

type objectContentBuiltinFactory func(ObjectContentPluginConfig) (ObjectContentHandler, error)

var builtinContentFactories = map[string]objectContentBuiltinFactory{
	models.ObjectPreviewKindPDF: func(cfg ObjectContentPluginConfig) (ObjectContentHandler, error) {
		handler := &binaryBase64Handler{
			baseContentHandler: baseContentHandler{
				name:     cfg.Name,
				priority: cfg.priorityOr(80),
				matcher:  descriptorObjectContentMatcher(cfg.Match, commonformat.FormatPDF, nil, nil),
			},
			maxBytes:    cfg.maxBytesOr(maxPDFPreviewBytes),
			contentKind: models.ObjectPreviewKindPDF,
			emptyTip:    "PDF 文件内容为空或无法读取",
		}
		return handler, nil
	},
	models.ObjectPreviewKindDOCX: func(cfg ObjectContentPluginConfig) (ObjectContentHandler, error) {
		handler := &binaryBase64Handler{
			baseContentHandler: baseContentHandler{
				name:     cfg.Name,
				priority: cfg.priorityOr(75),
				matcher:  descriptorObjectContentMatcher(cfg.Match, commonformat.FormatDOCX, nil, []string{"wordprocessingml"}),
			},
			maxBytes:    cfg.maxBytesOr(maxDOCXPreviewBytes),
			contentKind: models.ObjectPreviewKindDOCX,
			emptyTip:    "DOCX 文件内容为空或无法读取",
		}
		return handler, nil
	},
	models.ObjectPreviewKindWPS: func(cfg ObjectContentPluginConfig) (ObjectContentHandler, error) {
		handler := &binaryBase64Handler{
			baseContentHandler: baseContentHandler{
				name:     cfg.Name,
				priority: cfg.priorityOr(74),
				matcher:  descriptorObjectContentMatcher(cfg.Match, commonformat.FormatWPS, nil, nil),
			},
			maxBytes:    cfg.maxBytesOr(maxWPSPreviewBytes),
			contentKind: models.ObjectPreviewKindWPS,
			emptyTip:    "WPS 文件内容为空或无法读取",
		}
		return handler, nil
	},
	models.ObjectPreviewKindPPTX: func(cfg ObjectContentPluginConfig) (ObjectContentHandler, error) {
		handler := &binaryBase64Handler{
			baseContentHandler: baseContentHandler{
				name:     cfg.Name,
				priority: cfg.priorityOr(74),
				matcher:  descriptorObjectContentMatcher(cfg.Match, commonformat.FormatPPTX, nil, []string{"presentationml"}),
			},
			maxBytes:    cfg.maxBytesOr(maxPPTXPreviewBytes),
			contentKind: models.ObjectPreviewKindPPTX,
			emptyTip:    "PPTX 文件内容为空或无法读取",
		}
		return handler, nil
	},
	models.ObjectPreviewKindImage: func(cfg ObjectContentPluginConfig) (ObjectContentHandler, error) {
		handler := &imageContentHandler{
			baseContentHandler: baseContentHandler{
				name:     cfg.Name,
				priority: cfg.priorityOr(70),
				matcher:  mediaObjectContentMatcher(cfg.Match, "image"),
			},
		}
		return handler, nil
	},
	models.ObjectPreviewKindJSON: func(cfg ObjectContentPluginConfig) (ObjectContentHandler, error) {
		handler := &jsonContentHandler{
			baseContentHandler: baseContentHandler{
				name:     cfg.Name,
				priority: cfg.priorityOr(60),
				matcher:  descriptorObjectContentMatcher(cfg.Match, commonformat.FormatJSON, nil, nil),
			},
			maxBytes: cfg.maxBytesOr(maxJSONPreviewBytes),
			kind:     models.ObjectPreviewKindJSON,
		}
		return handler, nil
	},
	models.ObjectPreviewKindGeoJSON: func(cfg ObjectContentPluginConfig) (ObjectContentHandler, error) {
		handler := &jsonContentHandler{
			baseContentHandler: baseContentHandler{
				name:     cfg.Name,
				priority: cfg.priorityOr(65),
				matcher: newObjectContentMatcher(
					normalizeFormatsOrDefault(cfg.Match.Formats, []string{models.ObjectPreviewKindGeoJSON, "geo+json"}),
					normalizeExtensionsOrDefault(cfg.Match.Extensions, []string{".geojson"}),
					normalizeContentTypesOrDefault(cfg.Match.ContentTypes, []string{"application/geo+json", "application/vnd.geo+json"}),
				),
			},
			maxBytes: cfg.maxBytesOr(maxGeoJSONPreview),
			kind:     models.ObjectPreviewKindGeoJSON,
		}
		return handler, nil
	},
	models.ObjectPreviewKindExcel: func(cfg ObjectContentPluginConfig) (ObjectContentHandler, error) {
		handler := &excelContentHandler{
			baseContentHandler: baseContentHandler{
				name:     cfg.Name,
				priority: cfg.priorityOr(62),
				matcher:  descriptorObjectContentMatcher(cfg.Match, commonformat.FormatExcel, nil, []string{"xlsx", "xls"}),
			},
			maxBytes:   cfg.maxBytesOr(maxExcelPreviewBytes),
			childLimit: defaultExcelSheetLimit,
		}
		handler.childLimit = metadataInt(cfg.Metadata, "child_limit", metadataInt(cfg.Metadata, "sheet_limit", handler.childLimit))
		return handler, nil
	},
	models.ObjectPreviewKindSQLite: func(cfg ObjectContentPluginConfig) (ObjectContentHandler, error) {
		handler := &containerDatabaseContentHandler{
			baseContentHandler: baseContentHandler{
				name:     cfg.Name,
				priority: cfg.priorityOr(58),
				matcher:  descriptorObjectContentMatcher(cfg.Match, commonformat.FormatSQLite, []commonformat.FormatType{commonformat.FormatGeoPackage}, []string{"application/octet-stream"}),
			},
			maxBytes: cfg.maxBytesOr(maxContainerPreviewBytes),
		}
		return handler, nil
	},
	models.ObjectPreviewKindContainer: func(cfg ObjectContentPluginConfig) (ObjectContentHandler, error) {
		handler := &containerDatabaseContentHandler{
			baseContentHandler: baseContentHandler{
				name:     cfg.Name,
				priority: cfg.priorityOr(58),
				matcher:  containerObjectContentMatcher(cfg.Match),
			},
			maxBytes: cfg.maxBytesOr(maxContainerPreviewBytes),
		}
		return handler, nil
	},
	models.ObjectPreviewKindText: func(cfg ObjectContentPluginConfig) (ObjectContentHandler, error) {
		formats, _, contentTypes := descriptorMatcherDefaults(commonformat.FormatText)
		contentTypes = append(contentTypes, "text/")
		handler := &textContentHandler{
			baseContentHandler: baseContentHandler{
				name:     cfg.Name,
				priority: cfg.priorityOr(0),
				matcher: newObjectContentMatcher(
					normalizeFormatsOrDefault(cfg.Match.Formats, formats),
					normalizeExtensions(cfg.Match.Extensions),
					normalizeContentTypesOrDefault(cfg.Match.ContentTypes, contentTypes),
				),
			},
			maxBytes:   cfg.maxBytesOr(maxTextPreviewBytes),
			kind:       models.ObjectPreviewKindText,
			formatType: commonformat.FormatText,
		}
		return handler, nil
	},
	models.ObjectPreviewKindMarkdown: func(cfg ObjectContentPluginConfig) (ObjectContentHandler, error) {
		handler := &textContentHandler{
			baseContentHandler: baseContentHandler{
				name:     cfg.Name,
				priority: cfg.priorityOr(55),
				matcher:  descriptorObjectContentMatcher(cfg.Match, commonformat.FormatMarkdown, nil, []string{"text/plain"}),
			},
			maxBytes:   cfg.maxBytesOr(maxTextPreviewBytes),
			kind:       models.ObjectPreviewKindMarkdown,
			formatType: commonformat.FormatMarkdown,
		}
		return handler, nil
	},
	"parquet": func(cfg ObjectContentPluginConfig) (ObjectContentHandler, error) {
		handler := &parquetContentHandler{
			baseContentHandler: baseContentHandler{
				name:     cfg.Name,
				priority: cfg.priorityOr(63),
				matcher:  descriptorObjectContentMatcher(cfg.Match, commonformat.FormatParquet, nil, nil),
			},
			maxBytes: cfg.maxBytesOr(maxParquetPreviewBytes),
			rowLimit: defaultParquetRowLimit,
		}
		handler.rowLimit = metadataInt(cfg.Metadata, "row_limit", handler.rowLimit)
		return handler, nil
	},
	models.ObjectPreviewKindUnsupported: func(cfg ObjectContentPluginConfig) (ObjectContentHandler, error) {
		handler := &unsupportedContentHandler{
			baseContentHandler: baseContentHandler{
				name:     cfg.Name,
				priority: cfg.priorityOr(-100),
				matcher:  newObjectContentMatcher(normalizeFormats(cfg.Match.Formats), normalizeExtensions(cfg.Match.Extensions), normalizeContentTypes(cfg.Match.ContentTypes)),
			},
			maxBytes: cfg.maxBytesOr(maxTextPreviewBytes),
		}
		return handler, nil
	},
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
		if descriptor.DataType != commonformat.FormatDataTypeMedia {
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
		if descriptor.DataType != commonformat.FormatDataTypeContainer || !descriptor.Providers.ContainerInfo {
			continue
		}
		formats = append(formats, string(descriptor.Format))
		extensions = append(extensions, descriptor.Identification.Extensions...)
		contentTypes = append(contentTypes, descriptor.Identification.MimeTypes...)
	}
	contentTypes = append(contentTypes, "application/octet-stream")
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

// LoadObjectContentPlugins 从指定目录加载对象内容插件配置。
func LoadObjectContentPlugins(registry *ObjectContentRegistry, dirSpec string) {
	if registry == nil {
		return
	}
	dirs := splitDirectories(dirSpec)
	for _, dir := range dirs {
		loadContentPluginsFromDir(registry, dir)
	}
}

func loadContentPluginsFromDir(registry *ObjectContentRegistry, dir string) {
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		logger.L().Warn("数据预览: 内容插件目录不可用", "dir", dir, "error", err)
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		logger.L().Warn("数据预览: 读取内容插件目录失败", "dir", dir, "error", err)
		return
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		loadContentPluginFromFile(registry, path)
	}
}

func loadContentPluginFromFile(registry *ObjectContentRegistry, path string) {
	raw, err := os.ReadFile(path)
	if err != nil {
		logger.L().Warn("数据预览: 读取内容插件配置失败", "path", path, "error", err)
		return
	}

	var cfg ObjectContentPluginConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		logger.L().Warn("数据预览: 解析内容插件配置失败", "path", path, "error", err)
		return
	}

	if !cfg.isEnabled() {
		logger.L().Info("数据预览: 跳过已禁用内容插件", "config", path, "name", cfg.Name)
		return
	}

	var handler ObjectContentHandler
	switch strings.TrimSpace(strings.ToLower(cfg.Type)) {
	case "", "builtin":
		name := strings.TrimSpace(strings.ToLower(cfg.Builtin))
		factory, ok := builtinContentFactories[name]
		if !ok {
			logger.L().Warn("数据预览: 未知内置内容插件", "config", path, "builtin", cfg.Builtin)
			return
		}
		if cfg.Name == "" {
			cfg.Name = fmt.Sprintf("builtin:content:%s", name)
		}
		h, err := factory(cfg)
		if err != nil {
			logger.L().Warn("数据预览: 内置内容插件初始化失败", "config", path, "error", err)
			return
		}
		handler = h
	case "command":
		if cfg.Name == "" {
			cfg.Name = fmt.Sprintf("command:content:%s", filepath.Base(cfg.Command))
		}
		command := strings.TrimSpace(cfg.Command)
		if command == "" {
			logger.L().Warn("数据预览: 内容插件缺少 command", "config", path, "name", cfg.Name)
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
		logger.L().Warn("数据预览: 不支持的内容插件类型", "config", path, "type", cfg.Type)
		return
	}

	if handler == nil {
		return
	}

	registry.Register(handler)
	logger.L().Info("数据预览: 注册内容插件成功", "config", path, "plugin", handler.Name(), "priority", handler.Priority())
}
