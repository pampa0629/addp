package main

import (
	"os"
	"path/filepath"
	"strings"

	commonClient "github.com/addp/common/client"
	commonConfig "github.com/addp/common/config"
	"github.com/addp/common/logger"
	"github.com/addp/manager/internal/api"
	"github.com/addp/manager/internal/config"
	"github.com/addp/manager/internal/repository"
	"github.com/addp/manager/internal/service"
)

func main() {
	// 加载 .env 文件
	// Note: levelsUp depends on where we run from:
	// - If running from cmd/server: levelsUp=4
	// - If running from backend: levelsUp=2
	// - If running from root: levelsUp=0
	// We detect current directory and calculate appropriately
	cwd, _ := os.Getwd()
	levelsUp := 4 // default for cmd/server
	if filepath.Base(cwd) == "backend" {
		levelsUp = 2 // manager/backend → addp
	} else if filepath.Base(filepath.Dir(cwd)) == "manager" {
		levelsUp = 4 // manager/backend/cmd/server → addp
	}
	commonConfig.LoadEnv(levelsUp)
	initLogger()

	// 加载配置
	cfg := config.Load()

	// 初始化数据库
	db, err := repository.InitDatabase(cfg)
	if err != nil {
		logger.L().Error("数据库初始化失败", "error", err)
		os.Exit(1)
	}

	// 初始化 repositories
	resourceRepo := repository.NewResourceRepository(db)
	metadataRepo := repository.NewMetadataRepository(db, cfg.EncryptionKey)

	logger.L().Info("Manager 配置加载完成",
		"enable_integration", cfg.EnableIntegration,
		"internal_api_key_set", cfg.InternalAPIKey != "",
	)

	// 初始化 System 客户端（用于拉取解密的资源连接信息）
	var systemClient *commonClient.SystemClient
	if cfg.EnableIntegration && cfg.InternalAPIKey != "" {
		systemClient = commonClient.NewSystemClientWithInternalKey(cfg.SystemServiceURL, cfg.InternalAPIKey)
	}

	contentRegistry := service.NewObjectContentRegistry()
	pluginDirs := service.ParsePluginDirSpec(cfg.PreviewPluginDir)
	contentDirSpec := buildContentDirSpec(pluginDirs)
	if contentDirSpec != "" {
		service.LoadObjectContentPlugins(contentRegistry, contentDirSpec)
	}
	logger.L().Info("数据预览: 已激活内容插件")

	previewRegistry := service.NewPreviewRegistry()
	service.LoadPreviewPlugins(previewRegistry, metadataRepo, contentRegistry, cfg.PreviewPluginDir)
	logger.L().Info("数据预览: 已激活预览插件", "providers", previewRegistry.Providers())

	// 初始化 services
	resourceService := service.NewResourceService(resourceRepo)
	metadataService := service.NewMetadataService(metadataRepo, resourceRepo, systemClient, previewRegistry, contentRegistry)

	// 设置路由
	router := api.SetupRouter(cfg, resourceService, metadataService)

	// 启动服务
	addr := ":" + cfg.Port
	logger.L().Info("Manager 服务启动", "addr", addr)
	if err := router.Run(addr); err != nil {
		logger.L().Error("Manager 服务启动失败", "error", err)
		os.Exit(1)
	}
}

func initLogger() {
	logLevel := commonConfig.GetEnv("LOG_LEVEL", "info")
	logFormat := commonConfig.GetEnv("LOG_FORMAT", "json")
	addSource := commonConfig.GetEnvBool("LOG_ADD_SOURCE", false)

	logFile := commonConfig.GetEnv("LOG_FILE", "")
	if logFile == "" {
		logFile = commonConfig.ResolveFromRoot("logs", "manager-backend.log")
	} else if !filepath.IsAbs(logFile) {
		logFile = commonConfig.ResolveFromRoot(logFile)
	}
	_ = os.Setenv("LOG_FILE", logFile)

	logger.Init(logger.Options{
		Level:          logLevel,
		Format:         logFormat,
		AddSource:      addSource,
		FilePath:       logFile,
		RedirectStdLog: true,
	})
}

func buildContentDirSpec(dirs []string) string {
	if len(dirs) == 0 {
		return ""
	}
	contentDirs := make([]string, 0, len(dirs))
	for _, dir := range dirs {
		trimmed := strings.TrimSpace(dir)
		if trimmed == "" {
			continue
		}
		contentDirs = append(contentDirs, filepath.Join(trimmed, "content"))
	}
	return strings.Join(contentDirs, ",")
}
