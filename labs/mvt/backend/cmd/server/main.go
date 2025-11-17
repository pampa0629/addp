package main

import (
    "log"
    "os"
    "time"
    "context"

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

    // 打印关键启动参数（包含预热与缓存策略）
    log.Printf("Config: port=%s, frontend_url=%s, log_level=%s", cfg.Port, cfg.FrontendURL, cfg.LogLevel)
    log.Printf("DB: host=%s port=%s name=%s user=%s", cfg.Database.Host, cfg.Database.Port, cfg.Database.Name, cfg.Database.User)
    log.Printf("Redis: addr=%s ttl=%s", cfg.GetRedisAddr(), cfg.Redis.CacheTTL.String())
    log.Printf("CachePolicy: persist_min_duration=%s persist_min_raw_kb=%d", cfg.CachePolicy.PersistMinDuration, cfg.CachePolicy.PersistMinRawKB)
    log.Printf("Prewarm: enabled=%t max_zoom=%d concurrency=%d", cfg.Prewarm.Enabled, cfg.Prewarm.MaxZoom, cfg.Prewarm.Concurrency)

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

    // 5. 初始化 HTTP 处理器（带配置）
    handler := api.NewHandler(tileService, cacheService, cfg)

	// 6. 配置路由
	router := api.SetupRouter(cfg, handler)

    // 7. 启动配置热加载监听（datasources.yaml），并在更新后清空缓存以反映 pixel_rules 等变更
    startDataSourcesWatcher(datasourcesPath, tileService, cacheService)

    // 7.5 启动瓦片预热（异步）
    if cfg.Prewarm.Enabled {
        go service.StartPrewarm(cfg, tileService, cacheService)
    }

    // 8. 启动服务器
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

// 监听数据源配置文件变更，并热更新 TileService 的数据源
func startDataSourcesWatcher(path string, tileService *service.TileService, cacheService *service.CacheService) {
    log.Printf("[INFO] Watching datasource config (polling): %s", path)
    // 轮询检测文件修改时间，避免额外依赖
    const interval = 1 * time.Second
    var lastModTime time.Time
    var lastSize int64

    // 初始化状态
    if info, err := os.Stat(path); err == nil {
        lastModTime = info.ModTime()
        lastSize = info.Size()
    }

    go func() {
        ticker := time.NewTicker(interval)
        defer ticker.Stop()
        for range ticker.C {
            info, err := os.Stat(path)
            if err != nil {
                // 文件可能暂时不存在
                continue
            }
            // 通过修改时间或大小变化判断
            if info.ModTime() != lastModTime || info.Size() != lastSize {
                lastModTime = info.ModTime()
                lastSize = info.Size()
                // 防抖：等待一点点时间，避免半写入状态
                time.Sleep(150 * time.Millisecond)
                if ds, err := config.LoadDataSources(path); err != nil {
                    log.Printf("[ERROR] Reload datasources failed: %v", err)
                } else {
                    tileService.UpdateDataSources(ds)
                    log.Printf("[INFO] Datasources reloaded from %s", path)
                    // 简化处理：配置变化后清空所有缓存，避免 pixel_rules 等参数变化导致脏缓存
                    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
                    cacheErr := cacheService.ClearAll(ctx)
                    cancel()
                    if cacheErr != nil {
                        log.Printf("[WARN] Clear cache after reload failed: %v", cacheErr)
                    } else {
                        log.Printf("[INFO] Cache cleared after datasource reload")
                    }
                }
            }
        }
    }()
}
