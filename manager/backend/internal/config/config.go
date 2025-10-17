package config

import (
	"log"
	"strings"

	commonConfig "github.com/addp/common/config"
)

type Config struct {
	commonConfig.BaseConfig

	// Manager 模块特有配置
	Port             string
	DBSchema         string
	PreviewPluginDir string
	MetaServiceURL   string
}

func Load() *Config {
	systemURL := commonConfig.GetEnv("SYSTEM_SERVICE_URL", "http://localhost:8080")
	metaURL := commonConfig.GetEnv("META_SERVICE_URL", "http://localhost:8082")

	rawPluginDir := commonConfig.GetEnv("PREVIEW_PLUGIN_DIR", "")
	builtinPluginDir := commonConfig.ResolveFromRoot("manager", "backend", "plugins")
	var pluginDirs []string
	if trimmed := strings.TrimSpace(rawPluginDir); trimmed != "" {
		pluginDirs = append(pluginDirs, trimmed)
	}
	pluginDirs = append(pluginDirs, builtinPluginDir)
	defaultPluginDir := strings.Join(pluginDirs, ",")

	cfg := &Config{
		Port:             commonConfig.GetEnv("PORT", "8081"),
		DBSchema:         commonConfig.GetEnv("DB_SCHEMA", "manager"),
		PreviewPluginDir: defaultPluginDir,
		MetaServiceURL:   metaURL,
	}

	// 设置 BaseConfig 字段
	cfg.SystemServiceURL = systemURL
	cfg.EnableIntegration = commonConfig.GetEnvBool("ENABLE_SERVICE_INTEGRATION", true)

	// 从 System 获取共享配置
	if cfg.EnableIntegration {
		log.Println("🔄 Attempting to load shared config from System service...")
		if err := commonConfig.LoadSharedConfig(systemURL, &cfg.BaseConfig); err != nil {
			log.Printf("⚠️  Warning: Failed to load shared config from System: %v", err)
			log.Printf("⚠️  Falling back to local environment variables...")
			commonConfig.LoadLocalConfig(&cfg.BaseConfig)
		} else {
			log.Println("✅ Successfully loaded shared config from System service")
		}
	} else {
		log.Println("ℹ️  Service integration disabled, using local config")
		commonConfig.LoadLocalConfig(&cfg.BaseConfig)
	}

	return cfg
}
