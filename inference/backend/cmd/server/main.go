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
	commonconfiguration "github.com/addp/common/configuration"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/common/modulelifecycle"
	_ "github.com/addp/inference/i18n"
	"github.com/addp/inference/internal/api"
	inferenceauthorization "github.com/addp/inference/internal/authorization"
	"github.com/addp/inference/internal/config"
	"github.com/addp/inference/internal/models"
	"github.com/addp/inference/internal/repository"
	"github.com/addp/inference/internal/service"
	"gorm.io/driver/postgres"
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
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}
	runtimeContext, stopRuntime := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopRuntime()
	db, err := gorm.Open(postgres.Open(cfg.DatabaseDSN()), &gorm.Config{})
	if err != nil {
		log.Fatal(err)
	}
	if err := db.Exec("CREATE SCHEMA IF NOT EXISTS inference").Error; err != nil {
		log.Fatal(err)
	}
	if err := db.AutoMigrate(&models.ProviderConnection{}, &models.ProviderTenantGrant{}, &models.ModelDeployment{}, &models.ModelProfile{}, &models.CredentialAudit{}); err != nil {
		log.Fatal(err)
	}
	if err := ensureSchemaConstraints(db); err != nil {
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
	registration := client.RegisterAndHeartbeat(ctx, &commonclient.ModuleRegistrationRequest{ModuleName: "inference", ModuleURL: url, RoutePrefix: "/inference", HealthCheckURL: url + "/health/ready", Metadata: map[string]interface{}{"runtime_api": "addp.inference/v1"}, ConfigurationManagement: &commonconfiguration.ManagementDeclaration{SchemaVersion: commonconfiguration.ManagementSchemaVersion, Entries: []commonconfiguration.ManagementEntry{{ID: "inference.configuration", OwnerModule: "inference", ScopeTypes: []string{commonconfiguration.ScopePlatformDefaultWithTenantOverride}, FrontendRoute: "/inference/settings/models", ReadPermission: inferenceauthorization.PermissionInferenceProviderRead, UpdatePermission: inferenceauthorization.PermissionInferenceProviderUpdate}}}})
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

func ensureSchemaConstraints(db *gorm.DB) error {
	statements := []string{
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_inference_platform_profile_code ON inference.model_profiles (code) WHERE scope_type = 'platform' AND tenant_id IS NULL`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_inference_tenant_profile_code ON inference.model_profiles (tenant_id, code) WHERE scope_type = 'tenant' AND tenant_id IS NOT NULL`,
		`CREATE INDEX IF NOT EXISTS idx_inference_profiles_resolution ON inference.model_profiles (scope_type, tenant_id, status)`,
		`DO $$ BEGIN
			IF NOT EXISTS (
				SELECT 1 FROM pg_constraint WHERE conname = 'ck_inference_deployment_chat_max_output_tokens_parameter'
			) THEN
				ALTER TABLE inference.model_deployments
				ADD CONSTRAINT ck_inference_deployment_chat_max_output_tokens_parameter
				CHECK (chat_max_output_tokens_parameter IN ('max_tokens', 'max_completion_tokens'));
			END IF;
		END $$`,
		`DO $$ BEGIN
			IF NOT EXISTS (
				SELECT 1 FROM pg_constraint WHERE conname = 'ck_inference_deployment_chat_temperature_mode'
			) THEN
				ALTER TABLE inference.model_deployments
				ADD CONSTRAINT ck_inference_deployment_chat_temperature_mode
				CHECK (chat_temperature_mode IN ('configurable', 'default_only'));
			END IF;
		END $$`,
	}
	for _, statement := range statements {
		if err := db.Exec(statement).Error; err != nil {
			return err
		}
	}
	return nil
}
