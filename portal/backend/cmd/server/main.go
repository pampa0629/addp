package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	commonClient "github.com/addp/common/client"
	commonConfig "github.com/addp/common/config"
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
	runtimeContext, stopRuntime := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopRuntime()
	tokenSource, err := commonClient.NewOAuthServiceTokenSource(
		cfg.SystemURL, "addp-portal", cfg.ServiceClientSecret, nil,
	)
	if err != nil {
		log.Fatalf("Portal Service Token Source 初始化失败: %v", err)
	}
	assetClient := commonClient.NewAssetClient(cfg.AssetURL)
	serviceClient := commonClient.NewServiceClient(cfg.ServiceURL, tokenSource, nil)
	systemClient := commonClient.NewSystemServiceClient(cfg.SystemURL, tokenSource, nil)

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

	go func() {
		if err := router.Run(addr); err != nil {
			log.Printf("Failed to start server: %v", err)
			stopRuntime()
		}
	}()

	serviceURL := commonConfig.BuildServiceURL(commonConfig.GetServiceHost(), cfg.Port)
	registrationDone := systemClient.RegisterAndHeartbeatWithMetadata(runtimeContext, "portal", serviceURL, "/portal", nil)
	<-runtimeContext.Done()
	<-registrationDone
}
