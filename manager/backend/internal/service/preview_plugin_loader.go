package service

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/logger"
	"github.com/addp/manager/internal/repository"
)

var builtinProviderFactoriesWithContent = map[string]func(*repository.MetadataRepository, *commonClient.MetaClient, string, *ObjectContentRegistry) (PreviewProvider, error){
	"database-table": func(repo *repository.MetadataRepository, metaClient *commonClient.MetaClient, _ string, _ *ObjectContentRegistry) (PreviewProvider, error) {
		return NewDatabaseTablePreviewProvider(repo, metaClient), nil
	},
	"doc-collection": func(_ *repository.MetadataRepository, _ *commonClient.MetaClient, _ string, _ *ObjectContentRegistry) (PreviewProvider, error) {
		return NewDocCollectionPreviewProvider(), nil
	},
	"file-table": func(_ *repository.MetadataRepository, _ *commonClient.MetaClient, _ string, _ *ObjectContentRegistry) (PreviewProvider, error) {
		return NewFileTablePreviewProvider(), nil
	},
	"lake-table": func(_ *repository.MetadataRepository, _ *commonClient.MetaClient, _ string, _ *ObjectContentRegistry) (PreviewProvider, error) {
		return NewLakeTablePreviewProvider(), nil
	},
	"object-storage": func(repo *repository.MetadataRepository, metaClient *commonClient.MetaClient, metaServiceURL string, content *ObjectContentRegistry) (PreviewProvider, error) {
		return NewObjectStoragePreviewProvider(repo, metaClient, metaServiceURL, content), nil
	},
	"schema-node": func(repo *repository.MetadataRepository, metaClient *commonClient.MetaClient, _ string, _ *ObjectContentRegistry) (PreviewProvider, error) {
		return NewSchemaPreviewProvider(repo, metaClient), nil
	},
}

var builtinProviderFactories = map[string]builtinProviderFactory{
	"database-table": func(repo *repository.MetadataRepository) (PreviewProvider, error) {
		return NewDatabaseTablePreviewProvider(repo, nil), nil
	},
	"doc-collection": func(_ *repository.MetadataRepository) (PreviewProvider, error) {
		return NewDocCollectionPreviewProvider(), nil
	},
	"schema-node": func(repo *repository.MetadataRepository) (PreviewProvider, error) {
		return NewSchemaPreviewProvider(repo, nil), nil
	},
}

func LoadPreviewPlugins(registry *PreviewRegistry, metadataRepo *repository.MetadataRepository, metaClient *commonClient.MetaClient, contentRegistry *ObjectContentRegistry, metaServiceURL string, dirSpec string) {
	if registry == nil || metadataRepo == nil {
		return
	}

	dirs := splitDirectories(dirSpec)
	for _, dir := range dirs {
		loadPluginsFromDir(registry, metadataRepo, metaClient, metaServiceURL, contentRegistry, dir)
	}
}

func splitDirectories(spec string) []string {
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

func loadPluginsFromDir(registry *PreviewRegistry, metadataRepo *repository.MetadataRepository, metaClient *commonClient.MetaClient, metaServiceURL string, contentRegistry *ObjectContentRegistry, dir string) {
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		logger.L().Warn("数据预览: 插件目录不可用", "dir", dir, "error", err)
		return
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		logger.L().Warn("数据预览: 读取插件目录失败", "dir", dir, "error", err)
		return
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		loadPluginFromFile(registry, metadataRepo, metaClient, metaServiceURL, contentRegistry, path)
	}
}

func loadPluginFromFile(registry *PreviewRegistry, metadataRepo *repository.MetadataRepository, metaClient *commonClient.MetaClient, metaServiceURL string, contentRegistry *ObjectContentRegistry, path string) {
	raw, err := os.ReadFile(path)
	if err != nil {
		logger.L().Warn("数据预览: 读取插件配置失败", "path", path, "error", err)
		return
	}

	var cfg PluginConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		logger.L().Warn("数据预览: 解析插件配置失败", "path", path, "error", err)
		return
	}

	if !cfg.isEnabled() {
		logger.L().Info("数据预览: 跳过已禁用插件", "config", path, "name", displayName(cfg))
		return
	}

	var provider PreviewProvider

	switch cfg.pluginType() {
	case "builtin":
		factoryWithContent, ok := builtinProviderFactoriesWithContent[strings.TrimSpace(cfg.Builtin)]
		if ok {
			p, err := factoryWithContent(metadataRepo, metaClient, metaServiceURL, contentRegistry)
			if err != nil {
				logger.L().Warn("数据预览: 内置插件初始化失败", "config", path, "builtin", cfg.Builtin, "error", err)
				return
			}
			provider = wrapProviderForOverrides(p, cfg)
		} else {
			factory, ok := builtinProviderFactories[strings.TrimSpace(cfg.Builtin)]
			if !ok {
				logger.L().Warn("数据预览: 未知的内置插件", "config", path, "builtin", cfg.Builtin)
				return
			}
			p, err := factory(metadataRepo)
			if err != nil {
				logger.L().Warn("数据预览: 内置插件初始化失败", "config", path, "builtin", cfg.Builtin, "error", err)
				return
			}
			provider = wrapProviderForOverrides(p, cfg)
		}
	case "command":
		p, err := newCommandPreviewProvider(cfg, metadataRepo)
		if err != nil {
			logger.L().Warn("数据预览: 外部命令插件初始化失败", "config", path, "error", err)
			return
		}
		provider = wrapProviderForOverrides(p, cfg)
	default:
		logger.L().Warn("数据预览: 未知插件类型", "config", path, "type", cfg.Type)
		return
	}

	registry.Register(provider)
	logger.L().Info("数据预览: 注册插件成功", "config", path, "plugin", provider.Name(), "priority", provider.Priority())
}

func RegisterBuiltinPluginFactory(name string, factory builtinProviderFactory) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("builtin factory name cannot be empty")
	}
	if factory == nil {
		return fmt.Errorf("builtin factory for %s is nil", name)
	}
	builtinProviderFactories[name] = factory
	return nil
}

func ParsePluginDirSpec(spec string) []string {
	return splitDirectories(spec)
}
