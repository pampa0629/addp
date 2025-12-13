package main

import (
	"os"

	commonConfig "github.com/addp/common/config"
	"github.com/addp/common/logger"
	"github.com/addp/system/internal/api"
	"github.com/addp/system/internal/config"
	"github.com/addp/system/internal/repository"
	"github.com/gin-gonic/gin"
)

func main() {
	// 加载根目录统一的环境变量
	commonConfig.LoadEnv()
	commonConfig.InitLogger("system-backend.log", nil)

	// 加载配置
	cfg := config.Load()

	// 初始化数据库
	db, err := repository.InitDB(cfg.DatabaseURL)
	if err != nil {
		logger.L().Error("数据库初始化失败", "error", err)
		os.Exit(1)
	}

	// 自动迁移
	if err := repository.AutoMigrate(db); err != nil {
		logger.L().Error("数据库迁移失败", "error", err)
		os.Exit(1)
	}

	// 迁移现有资源的 display_name
	if err := repository.MigrateExistingResourcesDisplayName(db); err != nil {
		logger.L().Error("现有资源 display_name 迁移失败", "error", err)
		os.Exit(1)
	}

	// 初始化超级管理员用户
	if err := repository.InitSuperAdmin(db); err != nil {
		logger.L().Error("超级管理员用户初始化失败", "error", err)
		os.Exit(1)
	}

	// 初始化默认租户和租户管理员 (仅开发环境且显式启用时)
	if err := repository.InitDefaultTenant(db); err != nil {
		logger.L().Error("默认租户初始化失败", "error", err)
		os.Exit(1)
	}

	// 设置 Gin 模式
	if cfg.Env == "production" {
		gin.SetMode(gin.ReleaseMode)
	}

	// 创建路由
	router := api.SetupRouter(db, cfg)

	// 启动服务器
	logger.L().Info("系统服务启动", "addr", cfg.ServerAddr)
	if err := router.Run(cfg.ServerAddr); err != nil {
		logger.L().Error("服务器启动失败", "error", err)
		os.Exit(1)
	}
}
