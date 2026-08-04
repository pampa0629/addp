package preview

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/logger"
	"github.com/addp/manager/internal/objectcontent"
	"github.com/addp/manager/internal/pluginmanifest"
	"github.com/addp/manager/internal/repository"
)

var builtinProviderFactoriesWithContent = map[string]func(*repository.MetadataRepository, *commonClient.MetaClient, *objectcontent.ObjectContentRegistry) (PreviewProvider, error){
	"database-table": func(repo *repository.MetadataRepository, metaClient *commonClient.MetaClient, _ *objectcontent.ObjectContentRegistry) (PreviewProvider, error) {
		return NewDatabaseTablePreviewProvider(repo, metaClient), nil
	},
	"dynamic-schema-collection": func(_ *repository.MetadataRepository, _ *commonClient.MetaClient, _ *objectcontent.ObjectContentRegistry) (PreviewProvider, error) {
		return NewDynamicSchemaCollectionPreviewProvider(), nil
	},
	"graph": func(_ *repository.MetadataRepository, _ *commonClient.MetaClient, _ *objectcontent.ObjectContentRegistry) (PreviewProvider, error) {
		return NewGraphPreviewProvider(), nil
	},
	"event-stream-topic": func(_ *repository.MetadataRepository, _ *commonClient.MetaClient, _ *objectcontent.ObjectContentRegistry) (PreviewProvider, error) {
		return NewEventStreamTopicPreviewProvider(), nil
	},
	"file-table": func(_ *repository.MetadataRepository, _ *commonClient.MetaClient, _ *objectcontent.ObjectContentRegistry) (PreviewProvider, error) {
		return NewFileTablePreviewProvider(), nil
	},
	"scope-table": func(_ *repository.MetadataRepository, _ *commonClient.MetaClient, _ *objectcontent.ObjectContentRegistry) (PreviewProvider, error) {
		return NewScopeTablePreviewProvider(), nil
	},
	"container-child": func(_ *repository.MetadataRepository, _ *commonClient.MetaClient, content *objectcontent.ObjectContentRegistry) (PreviewProvider, error) {
		return NewContainerChildPreviewProvider(content), nil
	},
	"ref-file": func(_ *repository.MetadataRepository, _ *commonClient.MetaClient, content *objectcontent.ObjectContentRegistry) (PreviewProvider, error) {
		return NewRefFilePreviewProvider(content), nil
	},
	"object-catalog": func(repo *repository.MetadataRepository, metaClient *commonClient.MetaClient, content *objectcontent.ObjectContentRegistry) (PreviewProvider, error) {
		return NewObjectCatalogPreviewProvider(repo, metaClient, content), nil
	},
	"file-catalog": func(repo *repository.MetadataRepository, _ *commonClient.MetaClient, content *objectcontent.ObjectContentRegistry) (PreviewProvider, error) {
		return NewFileCatalogPreviewProvider(repo, content), nil
	},
	"schema-node": func(repo *repository.MetadataRepository, metaClient *commonClient.MetaClient, _ *objectcontent.ObjectContentRegistry) (PreviewProvider, error) {
		return NewSchemaPreviewProvider(repo, metaClient), nil
	},
}

type PreviewPluginConfigFile struct {
	Providers []PluginConfig `json:"providers,omitempty"`
}

func LoadPreviewPlugins(registry *PreviewRegistry, metadataRepo *repository.MetadataRepository, metaClient *commonClient.MetaClient, contentRegistry *objectcontent.ObjectContentRegistry, pluginDirSpec string) {
	if registry == nil || metadataRepo == nil {
		return
	}

	dirs := splitPluginDirSpec(pluginDirSpec)
	if len(dirs) == 0 {
		registerDefaultBuiltinPreviewProviders(registry, metadataRepo, metaClient, contentRegistry)
		return
	}
	for _, dir := range dirs {
		path := filepath.Join(dir, "preview.json")
		loadPluginsFromPreviewConfig(registry, metadataRepo, metaClient, contentRegistry, path)
	}
}

func registerDefaultBuiltinPreviewProviders(registry *PreviewRegistry, metadataRepo *repository.MetadataRepository, metaClient *commonClient.MetaClient, contentRegistry *objectcontent.ObjectContentRegistry) {
	registerBuiltinPreviewProviders(registry, metadataRepo, metaClient, contentRegistry, fallbackBuiltinPreviewPlugins())
}

func registerBuiltinPreviewProviders(registry *PreviewRegistry, metadataRepo *repository.MetadataRepository, metaClient *commonClient.MetaClient, contentRegistry *objectcontent.ObjectContentRegistry, configs []PluginConfig) {
	for _, cfg := range configs {
		provider, err := buildBuiltinPreviewProvider(cfg, metadataRepo, metaClient, contentRegistry)
		if err != nil {
			logger.L().Warn("数据预览: 默认内置插件初始化失败", "builtin", cfg.Builtin, "error", err)
			continue
		}
		registry.Register(provider)
	}
}

func fallbackBuiltinPreviewPlugins() []PluginConfig {
	return []PluginConfig{
		{Name: "builtin:database-table", Type: "builtin", Builtin: "database-table"},
		{Name: "builtin:dynamic-schema-collection", Type: "builtin", Builtin: "dynamic-schema-collection"},
		{Name: "builtin:graph", Type: "builtin", Builtin: "graph"},
		{Name: "builtin:event-stream-topic", Type: "builtin", Builtin: "event-stream-topic"},
		{Name: "builtin:scope-table", Type: "builtin", Builtin: "scope-table"},
		{Name: "builtin:container-child", Type: "builtin", Builtin: "container-child"},
		{Name: "builtin:ref-file", Type: "builtin", Builtin: "ref-file"},
		{Name: "builtin:file-table", Type: "builtin", Builtin: "file-table"},
		{Name: "builtin:object-catalog", Type: "builtin", Builtin: "object-catalog"},
		{Name: "builtin:file-catalog", Type: "builtin", Builtin: "file-catalog"},
		{Name: "builtin:schema-node", Type: "builtin", Builtin: "schema-node"},
	}
}

func splitPluginDirSpec(spec string) []string {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return nil
	}
	normalized := strings.ReplaceAll(spec, ";", ",")
	parts := strings.Split(normalized, ",")
	var paths []string
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			paths = append(paths, trimmed)
		}
	}
	return paths
}

func loadPluginsFromPreviewConfig(registry *PreviewRegistry, metadataRepo *repository.MetadataRepository, metaClient *commonClient.MetaClient, contentRegistry *objectcontent.ObjectContentRegistry, path string) {
	raw, err := os.ReadFile(path)
	if err != nil {
		logger.L().Warn("数据预览: 读取插件配置失败", "path", path, "error", err)
		return
	}
	if err := pluginmanifest.ValidateTopLevelFields(raw, "version", "description", "providers", "notes"); err != nil {
		logger.L().Warn("数据预览: 插件配置字段不受支持", "path", path, "error", err)
		return
	}

	var configFile PreviewPluginConfigFile
	if err := json.Unmarshal(raw, &configFile); err != nil {
		logger.L().Warn("数据预览: 解析插件配置失败", "path", path, "error", err)
		return
	}

	registerDefaultBuiltinPreviewProviders(registry, metadataRepo, metaClient, contentRegistry)
	for _, cfg := range configFile.Providers {
		loadPluginConfig(registry, metadataRepo, metaClient, contentRegistry, cfg, path)
	}
}

func loadPluginConfig(registry *PreviewRegistry, metadataRepo *repository.MetadataRepository, metaClient *commonClient.MetaClient, contentRegistry *objectcontent.ObjectContentRegistry, cfg PluginConfig, source string) {
	if !cfg.isEnabled() {
		if name := builtinPreviewProviderName(cfg); name != "" {
			registry.Unregister(name)
		}
		logger.L().Info("数据预览: 跳过已禁用插件", "config", source, "name", displayName(cfg))
		return
	}

	var provider PreviewProvider

	switch cfg.pluginType() {
	case "builtin":
		p, err := buildBuiltinPreviewProvider(cfg, metadataRepo, metaClient, contentRegistry)
		if err != nil {
			logger.L().Warn("数据预览: 内置插件初始化失败", "config", source, "builtin", cfg.Builtin, "error", err)
			return
		}
		provider = p
	case "command":
		p, err := newCommandPreviewProvider(cfg, metadataRepo)
		if err != nil {
			logger.L().Warn("数据预览: 外部命令插件初始化失败", "config", source, "error", err)
			return
		}
		provider = wrapProviderForOverrides(p, cfg)
	default:
		logger.L().Warn("数据预览: 未知插件类型", "config", source, "type", cfg.Type)
		return
	}

	registry.Register(provider)
	logger.L().Info("数据预览: 注册插件成功", "config", source, "plugin", provider.Name())
}

func buildBuiltinPreviewProvider(cfg PluginConfig, metadataRepo *repository.MetadataRepository, metaClient *commonClient.MetaClient, contentRegistry *objectcontent.ObjectContentRegistry) (PreviewProvider, error) {
	builtin := strings.TrimSpace(cfg.Builtin)
	factory, ok := builtinProviderFactoriesWithContent[builtin]
	if !ok {
		return nil, fmt.Errorf("未知的内置插件 %q", cfg.Builtin)
	}
	provider, err := factory(metadataRepo, metaClient, contentRegistry)
	if err != nil {
		return nil, err
	}
	return wrapProviderForOverrides(provider, cfg), nil
}

func builtinPreviewProviderName(cfg PluginConfig) string {
	name := strings.TrimSpace(cfg.Name)
	if name != "" {
		return name
	}
	builtin := strings.TrimSpace(cfg.Builtin)
	if builtin == "" {
		return ""
	}
	return "builtin:" + builtin
}

func RegisterBuiltinPluginFactory(name string, factory builtinProviderFactory) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("builtin factory name cannot be empty")
	}
	if factory == nil {
		return fmt.Errorf("builtin factory for %s is nil", name)
	}
	builtinProviderFactoriesWithContent[name] = func(repo *repository.MetadataRepository, _ *commonClient.MetaClient, _ *objectcontent.ObjectContentRegistry) (PreviewProvider, error) {
		return factory(repo)
	}
	return nil
}

func ParsePluginDirSpec(spec string) []string {
	return splitPluginDirSpec(spec)
}
