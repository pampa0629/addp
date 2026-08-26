package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

	commonClient "github.com/addp/common/client"
	commonConfig "github.com/addp/common/config"
	"github.com/addp/common/modulelifecycle"
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

	lifecycleController := modulelifecycle.NewBusiness("portal", commonClient.ModuleRuntimeRoleBackend)
	router := api.SetupRouter(cfg, redisClient, assetClient, serviceClient, lifecycleController)

	addr := ":" + cfg.Port
	log.Printf("Portal BFF service starting on %s", addr)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("Failed to bind Portal listener: %v", err)
	}

	go func() {
		if err := router.RunListener(listener); err != nil {
			log.Printf("Failed to start server: %v", err)
			stopRuntime()
		}
	}()

	serviceURL := commonConfig.BuildServiceURL(commonConfig.GetServiceHost(), cfg.Port)
	registration := systemClient.RegisterAndHeartbeatWithMetadata(runtimeContext, "portal", serviceURL, "/portal", nil)
	lifecycleController.AttachRegistration(registration)
	modulelifecycle.CancelRuntimeOnFatal(registration, stopRuntime)
	<-runtimeContext.Done()
	<-registration.Done()
}
