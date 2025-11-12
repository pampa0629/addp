package main

import (
	"log"
	"os"

	"github.com/addp/mvt/internal/api"
	"github.com/addp/mvt/internal/config"
	"github.com/addp/mvt/internal/service"
)

func main() {
	log.Println("Starting MVT Tile Server...")

	// 1. 加载环境配置
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// 2. 加载数据源配置
	datasourcesPath := getEnv("DATASOURCES_CONFIG", "config/datasources.yaml")
	dataSources, err := config.LoadDataSources(datasourcesPath)
	if err != nil {
		log.Fatalf("Failed to load datasources: %v", err)
	}
	log.Printf("Loaded %d datasources", len(dataSources))

	// 3. 初始化瓦片服务
	tileService, err := service.NewTileService(cfg, dataSources)
	if err != nil {
		log.Fatalf("Failed to initialize tile service: %v", err)
	}
	defer tileService.Close()
	log.Println("Connected to PostgreSQL")

	// 4. 初始化缓存服务
	cacheService, err := service.NewCacheService(cfg)
	if err != nil {
		log.Fatalf("Failed to initialize cache service: %v", err)
	}
	defer cacheService.Close()
	log.Println("Connected to Redis")

	// 5. 初始化 HTTP 处理器
	handler := api.NewHandler(tileService, cacheService)

	// 6. 配置路由
	router := api.SetupRouter(cfg, handler)

	// 7. 启动服务器
	addr := ":" + cfg.Port
	log.Printf("Server starting on %s", addr)
	log.Printf("Tile endpoint: http://localhost:%s/tiles/{datasource_id}/{z}/{x}/{y}.mvt", cfg.Port)
	log.Printf("API endpoint: http://localhost:%s/api/datasources", cfg.Port)
	log.Printf("Health check: http://localhost:%s/health", cfg.Port)

	if err := router.Run(addr); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
