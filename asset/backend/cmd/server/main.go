package main

import (
	"fmt"
	"log"
	"time"

	_ "github.com/addp/asset/i18n"
	"github.com/addp/asset/internal/api"
	"github.com/addp/asset/internal/config"
	"github.com/addp/asset/internal/models"
	"github.com/addp/asset/internal/search"
	"github.com/addp/asset/internal/service"
	commonClient "github.com/addp/common/client"
	"github.com/addp/common/utils"
	"github.com/redis/go-redis/v9"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func main() {
	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	db, err := gorm.Open(postgres.Open(cfg.GetDatabaseDSN()), &gorm.Config{})
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}

	// 确保 asset schema 存在
	if err := db.Exec(fmt.Sprintf("CREATE SCHEMA IF NOT EXISTS %s", cfg.DBSchema)).Error; err != nil {
		log.Fatalf("Failed to create schema: %v", err)
	}

	// AutoMigrate 所有资产相关表
	if err := db.AutoMigrate(
		&models.TypeDefinition{},
		&models.TypeFieldSchema{},
		&models.Catalog{},
		&models.Asset{},
		&models.AssetExtField{},
		&models.Application{},
		&models.Authorization{},
		&models.Rating{},
	); err != nil {
		log.Fatalf("Failed to migrate database: %v", err)
	}

	// 迁移后执行自定义 DDL
	// 1. 删除已废弃的旧表（AutoMigrate 已创建 asset.catalogs）
	// 2. 删除 asset.assets 的旧 category_id 列（GORM AutoMigrate 不自动删列）
	// 3. 添加同级唯一约束（GORM 不支持 partial index tag，手动创建）
	migrations := []string{
		`DROP TABLE IF EXISTS asset.categories`,
		`ALTER TABLE asset.assets DROP COLUMN IF EXISTS category_id`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_catalogs_unique_root_name
		 ON asset.catalogs (tenant_id, name) WHERE parent_id IS NULL`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_catalogs_unique_child_name
		 ON asset.catalogs (tenant_id, parent_id, name) WHERE parent_id IS NOT NULL`,
	}
	for _, sql := range migrations {
		if err := db.Exec(sql).Error; err != nil {
			log.Printf("⚠️  迁移 SQL 执行失败（可忽略）: %v", err)
		}
	}
	log.Printf("✅ 数据库迁移完成")

	// 初始化内置资产类型（幂等，重复执行安全）
	typeSvc := service.NewTypeService(db)
	if err := typeSvc.SeedBuiltinTypes(); err != nil {
		log.Printf("⚠️  内置资产类型初始化失败: %v", err)
	} else {
		log.Printf("✅ 内置资产类型初始化完成")
	}

	// 初始化 AssetService（注入各源模块 URL 和 Meilisearch indexer）
	moduleURLs := map[string]string{
		"meta":     cfg.MetaURL,
		"service":  cfg.ServiceURL,
		"standard": cfg.StandardURL,
		"develop":  cfg.DevelopURL,
	}
	indexer, err := search.NewIndexer(cfg.MeilisearchURL, cfg.MeilisearchMasterKey, cfg.MeilisearchAssetIndex)
	if err != nil {
		log.Printf("⚠️  Meilisearch 初始化失败，搜索功能将 fallback 到数据库: %v", err)
		indexer = nil
	}
	assetSvc := service.NewAssetService(db, moduleURLs, cfg.InternalAPIKey, indexer)

	var redisClient *redis.Client
	if cfg.RedisHost != "" {
		redisClient = redis.NewClient(&redis.Options{
			Addr:     fmt.Sprintf("%s:%s", cfg.RedisHost, cfg.RedisPort),
			Password: cfg.RedisPassword,
			DB:       cfg.RedisDB,
		})
	}

	systemClient := commonClient.NewSystemClientWithInternalKey(cfg.SystemURL, cfg.InternalAPIKey)

	authSvc := service.NewAuthorizationService(db)

	router := api.SetupRouter(db, cfg.SystemURL, redisClient, assetSvc)

	addr := ":" + cfg.Port
	log.Printf("Asset service starting on %s", addr)

	go func() {
		if err := router.Run(addr); err != nil {
			log.Fatalf("Failed to start server: %v", err)
		}
	}()

	// 授权有效期定时扫描：每小时检查并标记过期授权
	go func() {
		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()
		// 启动时立即执行一次
		if n, err := authSvc.ExpireOverdue(); err != nil {
			log.Printf("⚠️  授权过期扫描失败: %v", err)
		} else if n > 0 {
			log.Printf("✅ 授权过期扫描：标记 %d 条授权为已过期", n)
		}
		for range ticker.C {
			if n, err := authSvc.ExpireOverdue(); err != nil {
				log.Printf("⚠️  授权过期扫描失败: %v", err)
			} else if n > 0 {
				log.Printf("✅ 授权过期扫描：标记 %d 条授权为已过期", n)
			}
		}
	}()

	// 模块注册 + 心跳
	serviceHost := utils.GetServiceHost()
	port := utils.GetModulePort("asset")
	serviceURL := utils.BuildServiceURL(serviceHost, port)
	systemClient.RegisterAndHeartbeat("asset", serviceURL, "/asset")

	select {}
}
