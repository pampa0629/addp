package plugin_loader

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/addp/transfer/pkg/pipeline"
	"github.com/addp/transfer/plugins/readers"
	"github.com/addp/transfer/plugins/writers"
	"gopkg.in/yaml.v3"
)

// PluginConfig 表示单个插件的配置
type PluginConfig struct {
	Name        string `yaml:"name"`
	Type        string `yaml:"type"`
	Enabled     bool   `yaml:"enabled"`
	Description string `yaml:"description"`
	PluginPath  string `yaml:"plugin_path"` // 外部插件路径（.so 动态库）
}

// PluginsConfig 表示插件配置文件的结构
type PluginsConfig struct {
	Plugins struct {
		Readers []PluginConfig `yaml:"readers"`
		Writers []PluginConfig `yaml:"writers"`
	} `yaml:"plugins"`
}

// PluginLoader 插件加载器
type PluginLoader struct {
	config         *PluginsConfig
	readerRegistry map[string]pipeline.ReaderFactory
	writerRegistry map[string]pipeline.WriterFactory
}

// NewPluginLoader 创建插件加载器
func NewPluginLoader(configPath string) (*PluginLoader, error) {
	loader := &PluginLoader{
		readerRegistry: make(map[string]pipeline.ReaderFactory),
		writerRegistry: make(map[string]pipeline.WriterFactory),
	}

	// 加载配置文件
	if err := loader.loadConfig(configPath); err != nil {
		return nil, fmt.Errorf("failed to load plugin config: %w", err)
	}

	// 初始化内置插件映射
	loader.initBuiltinPlugins()

	return loader, nil
}

// loadConfig 加载 YAML 配置文件
func (l *PluginLoader) loadConfig(configPath string) error {
	// 检查文件是否存在
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		return fmt.Errorf("config file not found: %s", configPath)
	}

	// 读取文件
	data, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("failed to read config file: %w", err)
	}

	// 解析 YAML
	l.config = &PluginsConfig{}
	if err := yaml.Unmarshal(data, l.config); err != nil {
		return fmt.Errorf("failed to parse YAML: %w", err)
	}

	return nil
}

// initBuiltinPlugins 初始化内置插件的工厂函数映射
func (l *PluginLoader) initBuiltinPlugins() {
	// ============================================================
	// JDBC 系列 Readers
	// ============================================================
	l.readerRegistry["jdbc"] = readers.NewJDBCReader
	l.readerRegistry["postgresql"] = readers.NewJDBCReader
	l.readerRegistry["mysql"] = readers.NewJDBCReader

	// ============================================================
	// 空间数据格式 Readers
	// ============================================================
	l.readerRegistry["shapefile"] = readers.NewShapefileReader
	l.readerRegistry["geopackage"] = readers.NewGeoPackageReader
	l.readerRegistry["geojson"] = readers.NewGeoJSONReader
	l.readerRegistry["spatialite"] = readers.NewSpatiaLiteReader
	l.readerRegistry["sqlite"] = readers.NewSpatiaLiteReader
	l.readerRegistry["spatialite_parallel"] = readers.NewSpatiaLiteParallelReader

	// ============================================================
	// 表格数据格式 Readers
	// ============================================================
	l.readerRegistry["csv"] = readers.NewCSVReader

	// ============================================================
	// JDBC 系列 Writers
	// ============================================================
	l.writerRegistry["jdbc"] = writers.NewJDBCWriter
	l.writerRegistry["postgresql"] = writers.NewJDBCWriter
	l.writerRegistry["mysql"] = writers.NewJDBCWriter
	l.writerRegistry["postgres_copy"] = writers.NewPostgresCOPYWriter

	// ============================================================
	// 空间数据格式 Writers
	// ============================================================
	l.writerRegistry["shapefile"] = writers.NewShapefileWriter
	l.writerRegistry["geopackage"] = writers.NewGeoPackageWriter
	l.writerRegistry["geojson"] = writers.NewGeoJSONWriter

	// ============================================================
	// 表格数据格式 Writers
	// ============================================================
	l.writerRegistry["csv"] = writers.NewCSVWriter

	// ============================================================
	// 对象存储 Writers
	// ============================================================
	l.writerRegistry["s3"] = writers.NewS3Writer
	l.writerRegistry["minio"] = writers.NewS3Writer
}

// LoadPlugins 加载所有启用的插件到运行时注册表
func (l *PluginLoader) LoadPlugins(registry *pipeline.ConnectorRegistry) error {
	readersLoaded := 0
	writersLoaded := 0

	// 加载 Readers
	for _, pluginCfg := range l.config.Plugins.Readers {
		if !pluginCfg.Enabled {
			log.Printf("⏸️  跳过禁用的 Reader 插件: %s", pluginCfg.Name)
			continue
		}

		// 检查是否有外部插件路径
		if pluginCfg.PluginPath != "" {
			if err := l.loadExternalReader(registry, pluginCfg); err != nil {
				log.Printf("⚠️  加载外部 Reader 插件失败 (%s): %v", pluginCfg.Name, err)
				continue
			}
		} else {
			// 加载内置插件
			if err := l.loadBuiltinReader(registry, pluginCfg); err != nil {
				log.Printf("⚠️  加载内置 Reader 插件失败 (%s): %v", pluginCfg.Name, err)
				continue
			}
		}
		readersLoaded++
	}

	// 加载 Writers
	for _, pluginCfg := range l.config.Plugins.Writers {
		if !pluginCfg.Enabled {
			log.Printf("⏸️  跳过禁用的 Writer 插件: %s", pluginCfg.Name)
			continue
		}

		// 检查是否有外部插件路径
		if pluginCfg.PluginPath != "" {
			if err := l.loadExternalWriter(registry, pluginCfg); err != nil {
				log.Printf("⚠️  加载外部 Writer 插件失败 (%s): %v", pluginCfg.Name, err)
				continue
			}
		} else {
			// 加载内置插件
			if err := l.loadBuiltinWriter(registry, pluginCfg); err != nil {
				log.Printf("⚠️  加载内置 Writer 插件失败 (%s): %v", pluginCfg.Name, err)
				continue
			}
		}
		writersLoaded++
	}

	log.Printf("✅ 插件加载完成: Readers: %d/%d, Writers: %d/%d",
		readersLoaded, len(l.config.Plugins.Readers),
		writersLoaded, len(l.config.Plugins.Writers))

	return nil
}

// loadBuiltinReader 加载内置 Reader 插件
func (l *PluginLoader) loadBuiltinReader(registry *pipeline.ConnectorRegistry, cfg PluginConfig) error {
	factory, exists := l.readerRegistry[cfg.Type]
	if !exists {
		return fmt.Errorf("未找到内置 Reader 工厂: %s", cfg.Type)
	}

	if err := registry.RegisterReader(cfg.Type, factory); err != nil {
		return fmt.Errorf("注册 Reader 失败: %w", err)
	}

	return nil
}

// loadBuiltinWriter 加载内置 Writer 插件
func (l *PluginLoader) loadBuiltinWriter(registry *pipeline.ConnectorRegistry, cfg PluginConfig) error {
	factory, exists := l.writerRegistry[cfg.Type]
	if !exists {
		return fmt.Errorf("未找到内置 Writer 工厂: %s", cfg.Type)
	}

	if err := registry.RegisterWriter(cfg.Type, factory); err != nil {
		return fmt.Errorf("注册 Writer 失败: %w", err)
	}

	return nil
}

// loadExternalReader 加载外部 Reader 插件（动态库）
func (l *PluginLoader) loadExternalReader(registry *pipeline.ConnectorRegistry, cfg PluginConfig) error {
	// 检查插件文件是否存在
	absPath, err := filepath.Abs(cfg.PluginPath)
	if err != nil {
		return fmt.Errorf("无法解析插件路径: %w", err)
	}

	if _, err := os.Stat(absPath); os.IsNotExist(err) {
		return fmt.Errorf("插件文件不存在: %s", absPath)
	}

	// TODO: 实现动态库加载（使用 plugin.Open）
	// 这需要插件遵循特定的接口规范
	// 示例代码：
	//
	// p, err := plugin.Open(absPath)
	// if err != nil {
	//     return fmt.Errorf("failed to open plugin: %w", err)
	// }
	//
	// symbol, err := p.Lookup("NewReader")
	// if err != nil {
	//     return fmt.Errorf("plugin missing NewReader symbol: %w", err)
	// }
	//
	// factory, ok := symbol.(pipeline.ReaderFactory)
	// if !ok {
	//     return fmt.Errorf("invalid reader factory type")
	// }
	//
	// return registry.RegisterReader(cfg.Type, factory)

	return fmt.Errorf("外部插件加载暂未实现（需要 Go 1.8+ plugin 支持）")
}

// loadExternalWriter 加载外部 Writer 插件（动态库）
func (l *PluginLoader) loadExternalWriter(registry *pipeline.ConnectorRegistry, cfg PluginConfig) error {
	// 检查插件文件是否存在
	absPath, err := filepath.Abs(cfg.PluginPath)
	if err != nil {
		return fmt.Errorf("无法解析插件路径: %w", err)
	}

	if _, err := os.Stat(absPath); os.IsNotExist(err) {
		return fmt.Errorf("插件文件不存在: %s", absPath)
	}

	// TODO: 实现动态库加载（使用 plugin.Open）
	return fmt.Errorf("外部插件加载暂未实现（需要 Go 1.8+ plugin 支持）")
}

// GetConfig 返回插件配置（用于调试）
func (l *PluginLoader) GetConfig() *PluginsConfig {
	return l.config
}

// ListEnabledPlugins 列出所有启用的插件
func (l *PluginLoader) ListEnabledPlugins() (readers []string, writers []string) {
	for _, cfg := range l.config.Plugins.Readers {
		if cfg.Enabled {
			readers = append(readers, cfg.Type)
		}
	}

	for _, cfg := range l.config.Plugins.Writers {
		if cfg.Enabled {
			writers = append(writers, cfg.Type)
		}
	}

	return readers, writers
}
