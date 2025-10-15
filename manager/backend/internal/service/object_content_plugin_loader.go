package service

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/addp/common/logger"
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
	"pdf": func(cfg ObjectContentPluginConfig) (ObjectContentHandler, error) {
		handler := &binaryBase64Handler{
			baseContentHandler: baseContentHandler{
				name:     cfg.Name,
				priority: cfg.priorityOr(80),
				matcher:  newObjectContentMatcher(normalizeExtensionsOrDefault(cfg.Match.Extensions, []string{".pdf"}), normalizeContentTypesOrDefault(cfg.Match.ContentTypes, []string{"application/pdf"})),
			},
			maxBytes:    cfg.maxBytesOr(maxPDFPreviewBytes),
			contentKind: "pdf",
			emptyTip:    "PDF 文件内容为空或无法读取",
		}
		return handler, nil
	},
	"docx": func(cfg ObjectContentPluginConfig) (ObjectContentHandler, error) {
		handler := &binaryBase64Handler{
			baseContentHandler: baseContentHandler{
				name:     cfg.Name,
				priority: cfg.priorityOr(75),
				matcher:  newObjectContentMatcher(normalizeExtensionsOrDefault(cfg.Match.Extensions, []string{".docx"}), normalizeContentTypesOrDefault(cfg.Match.ContentTypes, []string{"application/vnd.openxmlformats-officedocument.wordprocessingml.document", "wordprocessingml"})),
			},
			maxBytes:    cfg.maxBytesOr(maxDOCXPreviewBytes),
			contentKind: "docx",
			emptyTip:    "DOCX 文件内容为空或无法读取",
		}
		return handler, nil
	},
	"pptx": func(cfg ObjectContentPluginConfig) (ObjectContentHandler, error) {
		handler := &binaryBase64Handler{
			baseContentHandler: baseContentHandler{
				name:     cfg.Name,
				priority: cfg.priorityOr(74),
				matcher:  newObjectContentMatcher(normalizeExtensionsOrDefault(cfg.Match.Extensions, []string{".pptx"}), normalizeContentTypesOrDefault(cfg.Match.ContentTypes, []string{"application/vnd.openxmlformats-officedocument.presentationml.presentation", "presentationml"})),
			},
			maxBytes:    cfg.maxBytesOr(maxPPTXPreviewBytes),
			contentKind: "pptx",
			emptyTip:    "PPTX 文件内容为空或无法读取",
		}
		return handler, nil
	},
	"image": func(cfg ObjectContentPluginConfig) (ObjectContentHandler, error) {
		handler := &imageContentHandler{
			baseContentHandler: baseContentHandler{
				name:     cfg.Name,
				priority: cfg.priorityOr(70),
				matcher:  newObjectContentMatcher(normalizeExtensionsOrDefault(cfg.Match.Extensions, []string{".png", ".jpg", ".jpeg", ".gif", ".bmp", ".webp", ".tiff", ".svg", ".heic"}), normalizeContentTypesOrDefault(cfg.Match.ContentTypes, []string{"image/", "image/png", "image/jpeg", "image/jpg", "image/gif", "image/bmp", "image/webp", "image/tiff", "image/svg+xml", "image/heic"})),
			},
			maxBytes: cfg.maxBytesOr(maxImagePreviewBytes),
		}
		return handler, nil
	},
	"json": func(cfg ObjectContentPluginConfig) (ObjectContentHandler, error) {
		handler := &jsonContentHandler{
			baseContentHandler: baseContentHandler{
				name:     cfg.Name,
				priority: cfg.priorityOr(60),
				matcher:  newObjectContentMatcher(normalizeExtensionsOrDefault(cfg.Match.Extensions, []string{".json"}), normalizeContentTypesOrDefault(cfg.Match.ContentTypes, []string{"application/json"})),
			},
			maxBytes: cfg.maxBytesOr(maxJSONPreviewBytes),
			kind:     "json",
		}
		return handler, nil
	},
	"geojson": func(cfg ObjectContentPluginConfig) (ObjectContentHandler, error) {
		handler := &jsonContentHandler{
			baseContentHandler: baseContentHandler{
				name:     cfg.Name,
				priority: cfg.priorityOr(65),
				matcher:  newObjectContentMatcher(normalizeExtensionsOrDefault(cfg.Match.Extensions, []string{".geojson"}), normalizeContentTypesOrDefault(cfg.Match.ContentTypes, []string{"application/geo+json", "geojson", "geo+json"})),
			},
			maxBytes: cfg.maxBytesOr(maxGeoJSONPreview),
			kind:     "geojson",
		}
		return handler, nil
	},
	"sqlite": func(cfg ObjectContentPluginConfig) (ObjectContentHandler, error) {
		handler := &sqliteContentHandler{
			baseContentHandler: baseContentHandler{
				name:     cfg.Name,
				priority: cfg.priorityOr(58),
				matcher:  newObjectContentMatcher(normalizeExtensionsOrDefault(cfg.Match.Extensions, []string{".sqlite", ".db", ".sqlite3"}), normalizeContentTypesOrDefault(cfg.Match.ContentTypes, []string{"application/x-sqlite3", "application/sqlite", "application/octet-stream"})),
			},
			maxBytes:   cfg.maxBytesOr(maxSQLitePreviewBytes),
			tableLimit: defaultSQLiteTableLimit,
			rowLimit:   defaultSQLiteRowLimit,
		}
		handler.tableLimit = metadataInt(cfg.Metadata, "table_limit", handler.tableLimit)
		handler.rowLimit = metadataInt(cfg.Metadata, "row_limit", handler.rowLimit)
		return handler, nil
	},
	"text": func(cfg ObjectContentPluginConfig) (ObjectContentHandler, error) {
		handler := &textContentHandler{
			baseContentHandler: baseContentHandler{
				name:     cfg.Name,
				priority: cfg.priorityOr(0),
				matcher:  newObjectContentMatcher(normalizeExtensions(cfg.Match.Extensions), normalizeContentTypes(cfg.Match.ContentTypes)),
			},
			maxBytes: cfg.maxBytesOr(maxTextPreviewBytes),
		}
		return handler, nil
	},
}

func normalizeExtensionsOrDefault(values, fallback []string) []string {
	if len(values) > 0 {
		return normalizeExtensions(values)
	}
	return normalizeExtensions(fallback)
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
				matcher:  newObjectContentMatcher(normalizeExtensions(cfg.Match.Extensions), normalizeContentTypes(cfg.Match.ContentTypes)),
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
