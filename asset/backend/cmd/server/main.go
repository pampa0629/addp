package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "github.com/addp/asset/docs"
	_ "github.com/addp/asset/i18n"
	"github.com/addp/asset/internal/api"
	"github.com/addp/asset/internal/config"
	"github.com/addp/asset/internal/repository"
	"github.com/addp/asset/internal/search"
	"github.com/addp/asset/internal/service"
	commonClient "github.com/addp/common/client"
	commonConfig "github.com/addp/common/config"
	"github.com/addp/common/events"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/common/modulelifecycle"
	"github.com/redis/go-redis/v9"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// @title ADDP Asset API
// @version 1.0
// @description ADDP 资产目录、发布、申请、授权和评价 API | ADDP asset catalog, publishing, application, authorization and rating API
// @BasePath /api/v1/asset
// @securityDefinitions.apikey BearerAuth
// @in header
// @name Authorization

func main() {
	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}
	runtimeContext, stopRuntime := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopRuntime()

	db, err := gorm.Open(postgres.Open(cfg.GetDatabaseDSN()), &gorm.Config{})
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}

	if err := commonExecution.EnsureStore(db); err != nil {
		log.Fatalf("Failed to ensure execution store: %v", err)
	}
	if err := repository.Migrate(db); err != nil {
		log.Fatalf("Asset 数据库迁移失败: %v", err)
	}
	log.Printf("✅ 数据库迁移完成")

	// 初始化内置资产类型（幂等，重复执行安全）
	typeSvc := service.NewTypeService(db)
	if err := typeSvc.SeedBuiltinTypes(); err != nil {
		log.Printf("⚠️  内置资产类型初始化失败: %v", err)
	} else {
		log.Printf("✅ 内置资产类型初始化完成")
	}

	// Catalog 只是创建、编辑和发布时的运行时软依赖，不进入 Asset Ready。
	serviceTokenSource, err := commonClient.NewOAuthServiceTokenSource(
		cfg.SystemURL, "addp-asset", cfg.ServiceClientSecret, nil,
	)
	if err != nil {
		log.Fatalf("Asset Service Token Source 初始化失败: %v", err)
	}
	systemClient := commonClient.NewSystemServiceClient(cfg.SystemURL, serviceTokenSource, nil)
	catalogClient := commonClient.NewCatalogClient(cfg.CatalogURL, serviceTokenSource, nil)
	workbenchGrantClient := commonClient.NewWorkbenchResourceGrantClient(cfg.WorkbenchURL, serviceTokenSource, nil)
	indexer, err := search.NewIndexer(cfg.MeilisearchURL, cfg.MeilisearchMasterKey, cfg.MeilisearchPublishedAssetIndex)
	if err != nil {
		log.Printf("⚠️  Meilisearch 初始化失败，资产关键词搜索不可用: %v", err)
		indexer = nil
	}
	assetSvc := service.NewAssetService(db, catalogClient, indexer)
	if err := assetSvc.RebuildPublishedIndex(); err != nil {
		log.Printf("⚠️  已上架资产搜索投影重建失败，资产关键词搜索暂不可用: %v", err)
		indexer = nil
		assetSvc = service.NewAssetService(db, catalogClient, nil)
	} else if indexer != nil && indexer.Enabled() {
		log.Printf("✅ 已上架资产搜索投影已重建")
	}
	taskExecutionRepo := commonExecution.NewTaskExecutionRepository(db)

	var redisClient *redis.Client
	if cfg.RedisHost != "" {
		redisClient = redis.NewClient(&redis.Options{
			Addr:     fmt.Sprintf("%s:%s", cfg.RedisHost, cfg.RedisPort),
			Password: cfg.RedisPassword,
			DB:       cfg.RedisDB,
		})
		defer redisClient.Close()
	}

	authSvc := service.NewAuthorizationService(db)
	grantFulfillmentSvc := service.NewGrantFulfillmentService(db, catalogClient, workbenchGrantClient)
	grantFulfillmentSvc.Start(runtimeContext)
	cleanupSvc := service.NewCleanupService(db, redisClient, taskExecutionRepo)
	if err := cleanupSvc.Start(runtimeContext); err != nil {
		log.Printf("Asset 资源回收执行方启动失败: %v", err)
	}
	defer cleanupSvc.Stop()

	lifecycleController := modulelifecycle.NewBusiness("asset", commonClient.ModuleRuntimeRoleBackend)
	router := api.SetupRouter(db, cfg.SystemURL, redisClient, assetSvc, lifecycleController)

	addr := ":" + cfg.Port
	log.Printf("Asset service starting on %s", addr)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("Failed to bind Asset listener: %v", err)
	}

	go func() {
		if err := router.RunListener(listener); err != nil {
			log.Printf("Failed to start server: %v", err)
			stopRuntime()
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
		for {
			select {
			case <-runtimeContext.Done():
				return
			case <-ticker.C:
				if n, err := authSvc.ExpireOverdue(); err != nil {
					log.Printf("⚠️  授权过期扫描失败: %v", err)
				} else if n > 0 {
					log.Printf("✅ 授权过期扫描：标记 %d 条授权为已过期", n)
				}
			}
		}
	}()

	// 模块注册 + 心跳
	serviceHost := commonConfig.GetServiceHost()
	serviceURL := commonConfig.BuildServiceURL(serviceHost, cfg.Port)
	registration := systemClient.RegisterAndHeartbeatWithMetadata(runtimeContext, "asset", serviceURL, "/asset", map[string]interface{}{
		"module": "asset",
		"capabilities": map[string]interface{}{
			"cleanup_executor": map[string]interface{}{
				"enabled": true,
				"causes":  []string{events.CleanupCauseTenantDeleted},
			},
		},
	})
	lifecycleController.AttachRegistration(registration)
	modulelifecycle.CancelRuntimeOnFatal(registration, stopRuntime)

	<-runtimeContext.Done()
	<-registration.Done()
}
