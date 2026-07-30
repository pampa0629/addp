package main

import (
	"fmt"
	"log"

	commonClient "github.com/addp/common/client"
	"github.com/addp/portal/internal/api"
	"github.com/addp/portal/internal/config"
	"github.com/redis/go-redis/v9"
)

// @title           ADDP Portal API
// @version         1.0
// @host      localhost:8184
// @BasePath  /api/v1/portal
// @securityDefinitions.apikey BearerAuth
// @in header
// @name Authorization

func main() {
	cfg := config.LoadConfig()
	tokenSource, err := commonClient.NewOAuthServiceTokenSource(
		cfg.SystemURL, "addp-portal", cfg.ServiceClientSecret, nil,
	)
	if err != nil {
		log.Fatalf("Portal Service Token Source 初始化失败: %v", err)
	}
	assetClient := commonClient.NewAssetClient(cfg.AssetURL)
	serviceClient := commonClient.NewServiceClient(cfg.ServiceURL, tokenSource, nil)

	var redisClient *redis.Client
	if cfg.RedisHost != "" {
		redisClient = redis.NewClient(&redis.Options{
			Addr:     fmt.Sprintf("%s:%s", cfg.RedisHost, cfg.RedisPort),
			Password: cfg.RedisPassword,
			DB:       cfg.RedisDB,
		})
	}

	router := api.SetupRouter(cfg, redisClient, assetClient, serviceClient)

	addr := ":" + cfg.Port
	log.Printf("Portal BFF service starting on %s", addr)

	if err := router.Run(addr); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
