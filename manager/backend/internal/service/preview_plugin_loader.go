package service

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/addp/common/logger"
	"github.com/addp/manager/internal/repository"
)

// 已弃用：内置插件现在通过 init() 自动注册
// 保留此 map 仅用于向后兼容外部插件加载器
var builtinProviderFactoriesWithContent = map[string]func(*repository.MetadataRepository, *ObjectContentRegistry) (PreviewProvider, error){
	"postgresql-table": func(repo *repository.MetadataRepository, _ *ObjectContentRegistry) (PreviewProvider, error) {
		return NewPostgresPreviewProvider(repo), nil
	},
	"shapefile": func(_ *repository.MetadataRepository, _ *ObjectContentRegistry) (PreviewProvider, error) {
		return NewShapefilePreviewProvider(), nil
	},
	"csv": func(_ *repository.MetadataRepository, _ *ObjectContentRegistry) (PreviewProvider, error) {
		return NewCSVPreviewProvider(), nil
	},
	"object-storage": func(repo *repository.MetadataRepository, content *ObjectContentRegistry) (PreviewProvider, error) {
		return NewObjectStoragePreviewProvider(repo, content), nil
	},
	"schema-node": func(repo *repository.MetadataRepository, _ *ObjectContentRegistry) (PreviewProvider, error) {
		return NewSchemaPreviewProvider(repo), nil
	},
}

// 已弃用：为兼容性保留旧的工厂接口
var builtinProviderFactories = map[string]builtinProviderFactory{
	"postgresql-table": func(repo *repository.MetadataRepository) (PreviewProvider, error) {
		return NewPostgresPreviewProvider(repo), nil
	},
	"schema-node": func(repo *repository.MetadataRepository) (PreviewProvider, error) {
		return NewSchemaPreviewProvider(repo), nil
	},
}

// LoadPreviewPlugins 从指定目录加载插件配置。支持多个目录，使用逗号或冒号分隔。
func LoadPreviewPlugins(registry *PreviewRegistry, metadataRepo *repository.MetadataRepository, contentRegistry *ObjectContentRegistry, dirSpec string) {
	if registry == nil || metadataRepo == nil {
		return
	}

	dirs := splitDirectories(dirSpec)
	for _, dir := range dirs {
		loadPluginsFromDir(registry, metadataRepo, contentRegistry, dir)
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

func loadPluginsFromDir(registry *PreviewRegistry, metadataRepo *repository.MetadataRepository, contentRegistry *ObjectContentRegistry, dir string) {
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
		loadPluginFromFile(registry, metadataRepo, contentRegistry, path)
	}
}

func loadPluginFromFile(registry *PreviewRegistry, metadataRepo *repository.MetadataRepository, contentRegistry *ObjectContentRegistry, path string) {
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
		// 优先使用新的工厂接口（支持 content registry）
		factoryWithContent, ok := builtinProviderFactoriesWithContent[strings.TrimSpace(cfg.Builtin)]
		if ok {
			p, err := factoryWithContent(metadataRepo, contentRegistry)
			if err != nil {
				logger.L().Warn("数据预览: 内置插件初始化失败", "config", path, "builtin", cfg.Builtin, "error", err)
				return
			}
			provider = wrapProviderForOverrides(p, cfg)
		} else {
			// 回退到旧接口（不支持 content registry）
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

// RegisterBuiltinPluginFactory 允许扩展内置插件工厂。
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

// ParsePluginDirSpec 将插件目录配置解析为切片，用于对外调用。
func ParsePluginDirSpec(spec string) []string {
	return splitDirectories(spec)
}
