package main

import (
	"context"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

	commonclient "github.com/addp/common/client"
	commonconfig "github.com/addp/common/config"
	commonexecution "github.com/addp/common/execution"
	"github.com/addp/common/modulelifecycle"
	_ "github.com/addp/security/i18n"
	"github.com/addp/security/internal/api"
	"github.com/addp/security/internal/config"
	"github.com/addp/security/internal/repository"
	"github.com/addp/security/internal/service"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// @title ADDP Security API
// @version 1.0
// @host localhost:8194
// @BasePath /api/v1/security
// @securityDefinitions.apikey BearerAuth
// @in header
// @name Authorization
func main() {
	cfg := config.Load()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	db, err := gorm.Open(postgres.Open(cfg.DatabaseDSN()), &gorm.Config{TranslateError: true})
	if err != nil {
		log.Fatalf("Security database connection failed: %v", err)
	}
	if err := repository.Migrate(db); err != nil {
		log.Fatalf("Security database migration failed: %v", err)
	}
	if err := commonexecution.EnsureStore(db); err != nil {
		log.Fatalf("Security execution store migration failed: %v", err)
	}
	tokenSource, err := commonclient.NewOAuthServiceTokenSource(cfg.SystemURL, "addp-security", cfg.ServiceClientSecret, nil)
	if err != nil {
		log.Fatalf("Security service token source failed: %v", err)
	}
	metaClient := commonclient.NewMetaClient(cfg.MetaURL, tokenSource)
	definitions := service.NewDefinitionService(db)
	enrollments := service.NewEnrollmentService(db)
	discoveries := service.NewDiscoveryService(db, nil)
	assessments := service.NewAssessmentService(db, func(tenantID uint) service.SecurityFactsReader { return metaClient.WithTenantID(tenantID) })
	policies := service.NewPolicyService(db)
	lifecycle := modulelifecycle.NewBusiness("security", commonclient.ModuleRuntimeRoleBackend)
	router := api.SetupRouter(definitions, enrollments, discoveries, assessments, policies, cfg.SystemURL, lifecycle)
	listener, err := net.Listen("tcp", ":"+cfg.Port)
	if err != nil {
		log.Fatalf("Security listener failed: %v", err)
	}
	go func() {
		if err := router.RunListener(listener); err != nil {
			log.Printf("Security server stopped: %v", err)
			stop()
		}
	}()
	systemClient := commonclient.NewSystemServiceClient(cfg.SystemURL, tokenSource, nil)
	serviceURL := commonconfig.BuildServiceURL(commonconfig.GetServiceHost(), cfg.Port)
	registration := systemClient.RegisterAndHeartbeat(ctx, &commonclient.ModuleRegistrationRequest{ModuleName: "security", ModuleURL: serviceURL, RoutePrefix: "/security", HealthCheckURL: serviceURL + "/health/ready", Metadata: map[string]interface{}{"module": "security"}})
	lifecycle.AttachRegistration(registration)
	modulelifecycle.CancelRuntimeOnFatal(registration, stop)
	<-ctx.Done()
	<-registration.Done()
}
