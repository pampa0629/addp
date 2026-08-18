package objectcontent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	commondatatype "github.com/addp/common/datatype"
	commonformat "github.com/addp/common/format"
	"github.com/addp/common/logger"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/pluginmanifest"
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

type ObjectContentPluginConfigFile struct {
	ContentPlugins []ObjectContentPluginConfig `json:"content_plugins,omitempty"`
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
	case models.ObjectPreviewKindAudio:
		return buildAudioContentHandler(cfg, "audio"), nil
	case models.ObjectPreviewKindJSON:
		return buildJSONContentHandler(cfg), nil
	case models.ObjectPreviewKindTable:
		return buildTableContentHandler(cfg), nil
	case models.ObjectPreviewKindModel3D:
		return buildModel3DContentHandler(cfg), nil
	case models.ObjectPreviewKindPointCloud:
		return buildPointCloudContentHandler(cfg), nil
	case models.ObjectPreviewKindGaussianSplat:
		return buildGaussianSplatContentHandler(cfg), nil
	case models.ObjectPreviewKindCAD:
		return buildCADContentHandler(cfg), nil
	case models.ObjectPreviewKindVectorTile:
		return buildVectorTileContentHandler(cfg), nil
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
	if descriptor.DataType == commondatatype.Media {
		mediaKind := mediaKindFromDescriptor(descriptor)
		switch mediaKind {
		case "image":
			return buildImageContentHandler(cfg, string(descriptor.Format)), nil
		case "video":
			return buildVideoContentHandler(cfg, string(descriptor.Format)), nil
		case "audio":
			return buildAudioContentHandler(cfg, string(descriptor.Format)), nil
		}
	}
	if descriptor.DataType == commondatatype.Document {
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
	plugins := []ObjectContentPluginConfig{
		{Name: "builtin:content-pdf", Type: "builtin", Builtin: models.ObjectPreviewKindPDF},
		{Name: "builtin:content-docx", Type: "builtin", Builtin: models.ObjectPreviewKindDOCX},
		{Name: "builtin:content-pptx", Type: "builtin", Builtin: models.ObjectPreviewKindPPTX},
		{Name: "builtin:content-wps", Type: "builtin", Builtin: models.ObjectPreviewKindWPS},
	}
	plugins = append(plugins, defaultMediaContentPlugins()...)
	plugins = append(plugins,
		ObjectContentPluginConfig{
			Name:     "builtin:content-table",
			Type:     "builtin",
			Builtin:  models.ObjectPreviewKindTable,
			Metadata: map[string]interface{}{"row_limit": defaultTableRowLimit},
		},
		ObjectContentPluginConfig{Name: "builtin:content-model-3d", Type: "builtin", Builtin: models.ObjectPreviewKindModel3D},
		ObjectContentPluginConfig{Name: "builtin:content-point-cloud", Type: "builtin", Builtin: models.ObjectPreviewKindPointCloud},
		ObjectContentPluginConfig{Name: "builtin:content-gaussian-splat", Type: "builtin", Builtin: models.ObjectPreviewKindGaussianSplat},
		ObjectContentPluginConfig{Name: "builtin:content-cad", Type: "builtin", Builtin: models.ObjectPreviewKindCAD},
		ObjectContentPluginConfig{Name: "builtin:content-vector-tile", Type: "builtin", Builtin: models.ObjectPreviewKindVectorTile},
		ObjectContentPluginConfig{Name: "builtin:content-json", Type: "builtin", Builtin: models.ObjectPreviewKindJSON},
		ObjectContentPluginConfig{Name: "builtin:content-container", Type: "builtin", Builtin: models.ObjectPreviewKindContainer},
		ObjectContentPluginConfig{Name: "builtin:content-markdown", Type: "builtin", Builtin: models.ObjectPreviewKindMarkdown},
		ObjectContentPluginConfig{Name: "builtin:content-text", Type: "builtin", Builtin: models.ObjectPreviewKindText},
		ObjectContentPluginConfig{Name: "builtin:content-unsupported", Type: "builtin", Builtin: models.ObjectPreviewKindUnsupported},
	)
	return plugins
}

func defaultMediaContentPlugins() []ObjectContentPluginConfig {
	kinds := []string{
		models.ObjectPreviewKindImage,
		models.ObjectPreviewKindVideo,
		models.ObjectPreviewKindAudio,
	}
	plugins := make([]ObjectContentPluginConfig, 0, len(kinds))
	for _, kind := range kinds {
		plugins = append(plugins, ObjectContentPluginConfig{
			Name:    "builtin:content-" + kind,
			Type:    "builtin",
			Builtin: kind,
		})
	}
	return plugins
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

func buildAudioContentHandler(cfg ObjectContentPluginConfig, mediaKind string) ObjectContentHandler {
	return &mediaStreamContentHandler{
		baseContentHandler: baseContentHandler{
			name:     cfg.Name,
			priority: cfg.priorityOr(defaultBuiltinContentPriority(models.ObjectPreviewKindAudio)),
			matcher:  mediaObjectContentMatcher(cfg.Match, mediaKind),
		},
		kind: models.ObjectPreviewKindAudio,
	}
}

func buildJSONContentHandler(cfg ObjectContentPluginConfig) ObjectContentHandler {
	return &jsonContentHandler{
		baseContentHandler: baseContentHandler{
			name:     cfg.Name,
			priority: cfg.priorityOr(defaultBuiltinContentPriority(models.ObjectPreviewKindJSON)),
			matcher:  descriptorObjectContentMatcher(cfg.Match, commonformat.FormatJSON, []commonformat.FormatType{commonformat.FormatGeoJSON}, nil),
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
		xmlFormats, xmlExtensions, xmlContentTypes := descriptorMatcherDefaults(commonformat.FormatXML)
		formats = append(formats, xmlFormats...)
		extensions = append(extensions, xmlExtensions...)
		contentTypes = append(contentTypes, xmlContentTypes...)
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

func buildTableContentHandler(cfg ObjectContentPluginConfig) ObjectContentHandler {
	handler := &tableContentHandler{
		baseContentHandler: baseContentHandler{
			name:     cfg.Name,
			priority: cfg.priorityOr(defaultBuiltinContentPriority(models.ObjectPreviewKindTable)),
			matcher:  tableObjectContentMatcher(cfg.Match),
		},
		maxBytes: cfg.maxBytesOr(maxTablePreviewBytes),
		rowLimit: defaultTableRowLimit,
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
	case models.ObjectPreviewKindAudio:
		return 67
	case models.ObjectPreviewKindTable:
		return 63
	case models.ObjectPreviewKindModel3D:
		return 62
	case models.ObjectPreviewKindPointCloud:
		return 61
	case models.ObjectPreviewKindGaussianSplat:
		return 61
	case models.ObjectPreviewKindCAD:
		return 61
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

func buildModel3DContentHandler(cfg ObjectContentPluginConfig) ObjectContentHandler {
	return &model3DContentHandler{
		baseContentHandler: baseContentHandler{
			name:     cfg.Name,
			priority: cfg.priorityOr(defaultBuiltinContentPriority(models.ObjectPreviewKindModel3D)),
			matcher: descriptorObjectContentMatcher(
				cfg.Match,
				commonformat.FormatGLB,
				[]commonformat.FormatType{
					commonformat.FormatFBX,
					commonformat.FormatOBJ,
					commonformat.FormatPLY,
					commonformat.FormatSTL,
					commonformat.Format3DTiles,
					commonformat.FormatS3M,
				},
				nil,
			),
		},
	}
}

func buildPointCloudContentHandler(cfg ObjectContentPluginConfig) ObjectContentHandler {
	return &pointCloudContentHandler{
		baseContentHandler: baseContentHandler{
			name:     cfg.Name,
			priority: cfg.priorityOr(defaultBuiltinContentPriority(models.ObjectPreviewKindPointCloud)),
			matcher: descriptorObjectContentMatcher(
				cfg.Match,
				commonformat.FormatLAS,
				[]commonformat.FormatType{
					commonformat.FormatLAZ,
					commonformat.FormatCOPC,
					commonformat.FormatE57,
					commonformat.FormatPCD,
					commonformat.FormatXYZ,
					commonformat.FormatEPT,
					commonformat.FormatPLY,
				},
				nil,
			),
		},
	}
}

func buildCADContentHandler(cfg ObjectContentPluginConfig) ObjectContentHandler {
	return &cadContentHandler{
		baseContentHandler: baseContentHandler{
			name:     cfg.Name,
			priority: cfg.priorityOr(defaultBuiltinContentPriority(models.ObjectPreviewKindCAD)),
			matcher:  descriptorObjectContentMatcher(cfg.Match, commonformat.FormatDWG, []commonformat.FormatType{commonformat.FormatDXF}, nil),
		},
	}
}

func buildVectorTileContentHandler(cfg ObjectContentPluginConfig) ObjectContentHandler {
	return &vectorTileContentHandler{baseContentHandler: baseContentHandler{
		name: cfg.Name, priority: cfg.priorityOr(64),
		matcher: descriptorObjectContentMatcher(cfg.Match, commonformat.FormatPMTiles, nil, nil),
	}}
}

func buildGaussianSplatContentHandler(cfg ObjectContentPluginConfig) ObjectContentHandler {
	return &gaussianSplatContentHandler{
		baseContentHandler: baseContentHandler{
			name:     cfg.Name,
			priority: cfg.priorityOr(defaultBuiltinContentPriority(models.ObjectPreviewKindGaussianSplat)),
			matcher:  descriptorObjectContentMatcher(cfg.Match, commonformat.FormatPLY, []commonformat.FormatType{commonformat.FormatSplat, commonformat.FormatKSplat}, nil),
		},
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
	if formatDescriptorMatchesMediaKind(descriptor, "audio") {
		return "audio"
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
		formatType := commonformat.NormalizeFormat(formatName)
		if formatType != "" && formatType != commonformat.FormatUnknown {
			normalized = append(normalized, string(formatType))
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

func tableObjectContentMatcher(match ObjectContentMatcherConfig) objectContentMatcher {
	formats := make([]string, 0)
	extensions := make([]string, 0)
	contentTypes := make([]string, 0)
	for _, descriptor := range commonformat.ListFormatDescriptors() {
		if descriptor.Format == commonformat.FormatGeoJSON {
			continue
		}
		if descriptor.DataType != commondatatype.Table || !hasSingleTableContentProviders(descriptor.Format) {
			continue
		}
		formats = append(formats, string(descriptor.Format))
		extensions = append(extensions, descriptor.Identification.Extensions...)
		contentTypes = append(contentTypes, descriptor.Identification.MimeTypes...)
	}
	sort.Strings(formats)
	sort.Strings(extensions)
	sort.Strings(contentTypes)
	return newObjectContentMatcher(
		normalizeFormatsOrDefault(match.Formats, formats),
		normalizeExtensionsOrDefault(match.Extensions, extensions),
		normalizeContentTypesOrDefault(match.ContentTypes, contentTypes),
	)
}

func hasSingleTableContentProviders(formatType commonformat.FormatType) bool {
	if _, err := commonformat.GetTableInfoProvider(formatType); err != nil {
		return false
	}
	if _, err := commonformat.GetTableSampleReader(formatType); err != nil {
		return false
	}
	return true
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
		if descriptor.DataType != commondatatype.Media {
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
		if descriptor.DataType != commondatatype.Container || !hasContainerInfoProvider(descriptor.Format) {
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

func hasContainerInfoProvider(formatType commonformat.FormatType) bool {
	_, err := commonformat.GetContainerInfoProvider(formatType)
	return err == nil
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

// LoadObjectContentPlugins 从插件目录下的 content.json 加载对象内容插件配置。
func LoadObjectContentPlugins(registry *ObjectContentRegistry, pluginDirSpec string) {
	if registry == nil {
		return
	}
	dirs := splitPluginDirSpec(pluginDirSpec)
	if len(dirs) == 0 {
		registerDefaultBuiltinContentHandlers(registry)
		return
	}
	for _, dir := range dirs {
		path := filepath.Join(dir, "content.json")
		loadContentPluginsFromConfig(registry, path)
	}
}

func splitPluginDirSpec(spec string) []string {
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

func loadContentPluginsFromConfig(registry *ObjectContentRegistry, path string) {
	raw, err := os.ReadFile(path)
	if err != nil {
		logger.L().Warn("数据预览: 读取插件配置失败", "path", path, "error", err)
		return
	}
	if err := pluginmanifest.ValidateTopLevelFields(raw, "version", "description", "content_plugins", "notes"); err != nil {
		logger.L().Warn("数据预览: 内容插件配置字段不受支持", "path", path, "error", err)
		return
	}

	var configFile ObjectContentPluginConfigFile
	if err := json.Unmarshal(raw, &configFile); err != nil {
		logger.L().Warn("数据预览: 解析插件配置失败", "path", path, "error", err)
		return
	}

	registerDefaultBuiltinContentHandlers(registry)
	for _, cfg := range configFile.ContentPlugins {
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
