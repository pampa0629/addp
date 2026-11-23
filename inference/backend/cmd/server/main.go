package main

import (
	"context"
	"log"
	"net"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	commonclient "github.com/addp/common/client"
	commonconfig "github.com/addp/common/config"
	"github.com/addp/common/infra/bootstrap"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/common/modulelifecycle"
	"github.com/addp/common/schema"
	_ "github.com/addp/inference/i18n"
	"github.com/addp/inference/internal/api"
	"github.com/addp/inference/internal/config"
	"github.com/addp/inference/internal/migration"
	"github.com/addp/inference/internal/repository"
	"github.com/addp/inference/internal/service"
	"gorm.io/gorm"
)

// @title ADDP Inference API
// @version 1.0
// @description ADDP 统一 AI 推理控制面 API | ADDP unified AI inference control-plane API
// @host localhost:8191
// @BasePath /api/v1/inference
// @securityDefinitions.apikey BearerAuth
// @in header
// @name Authorization
func main() {
	if len(os.Args) != 1 {
		if len(os.Args) != 2 || os.Args[1] != "prepare-database" {
			log.Fatal("expected prepare-database with no arguments")
		}
		if err := migration.Prepare(); err != nil {
			log.Fatal(err)
		}
		return
	}
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}
	runtimeContext, stopRuntime := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopRuntime()
	db, err := bootstrap.OpenPlatformRuntime(config.PlatformDatabase(), "inference", &gorm.Config{})
	if err != nil {
		log.Fatal(err)
	}
	if err := schema.Require(db, "inference", migration.Version); err != nil {
		log.Fatal(err)
	}
	store := repository.NewStore(db)
	control := service.NewControlPlane(store, cfg.EncryptionKey)
	runtime := service.NewRuntime(store, cfg.EncryptionKey)
	lifecycleController := modulelifecycle.NewBusiness("inference", commonclient.ModuleRuntimeRoleBackend)
	router := api.SetupRouter(cfg, api.NewHandler(control, runtime), lifecycleController)
	listener, err := net.Listen("tcp", ":"+cfg.Port)
	if err != nil {
		log.Fatal(err)
	}
	registration := register(runtimeContext, cfg)
	lifecycleController.AttachRegistration(registration)
	modulelifecycle.CancelRuntimeOnFatal(registration, stopRuntime)
	go func() {
		if err := router.RunListener(listener); err != nil {
			log.Printf("inference HTTP service stopped: %v", err)
			stopRuntime()
		}
	}()
	<-runtimeContext.Done()
	if registration != nil {
		<-registration.Done()
	}
	_ = listener.Close()
}

func register(ctx context.Context, cfg *config.Config) *commonclient.ModuleRegistrationLifecycle {
	if len(cfg.ServiceClientSecret) < 32 {
		return nil
	}
	tokenSource, err := commonclient.NewOAuthServiceTokenSource(cfg.SystemURL, "addp-inference", cfg.ServiceClientSecret, nil)
	if err != nil {
		log.Printf("inference module registration disabled: %v", err)
		return nil
	}
	client := commonclient.NewSystemServiceClient(cfg.SystemURL, tokenSource, nil)
	host := commonconfig.GetServiceHost()
	url := commonconfig.BuildServiceURL(host, cfg.Port)
	registration := client.RegisterAndHeartbeat(ctx, &commonclient.ModuleRegistrationRequest{ModuleName: "inference", ModuleURL: url, HealthCheckURL: url + "/health/ready", Metadata: map[string]interface{}{"runtime_api": "addp.inference/v1"}})
	port, err := strconv.Atoi(cfg.Port)
	if err != nil || port <= 0 || port > 65535 {
		log.Printf("inference runtime engine registration disabled: invalid port %q", cfg.Port)
		return registration
	}
	client.RegisterRuntimeEngineWithRetry(ctx, &commonmodels.CapabilityRegistrationRequest{
		Name:        "Inference Runtime",
		EngineType:  "inference_runtime",
		IsBuiltin:   true,
		Description: "ADDP 内置统一 AI 推理 Runtime",
		ConnectionInfo: map[string]interface{}{
			"protocol": "http",
			"host":     host,
			"port":     port,
		},
	}, time.Second, 30*time.Second)
	return registration
}
