package main

import (
	"context"
	"fmt"
	commonclient "github.com/addp/common/client"
	commonexecution "github.com/addp/common/execution"
	"github.com/addp/common/modulelifecycle"
	"github.com/addp/security/internal/config"
	"github.com/addp/security/internal/repository"
	"github.com/addp/security/internal/service"
	securityworker "github.com/addp/security/internal/worker"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	cfg := config.Load()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	tokens, err := commonclient.NewOAuthServiceTokenSource(cfg.SystemURL, "addp-security", cfg.ServiceClientSecret, nil)
	if err != nil {
		log.Fatalf("Security worker token source failed: %v", err)
	}
	db, err := gorm.Open(postgres.Open(cfg.DatabaseDSN()), &gorm.Config{TranslateError: true})
	if err != nil {
		log.Fatalf("Security worker database connection failed: %v", err)
	}
	if err := repository.Migrate(db); err != nil {
		log.Fatalf("Security worker schema migration failed: %v", err)
	}
	if err := commonexecution.EnsureStore(db); err != nil {
		log.Fatalf("Security worker execution store migration failed: %v", err)
	}
	metaClient := commonclient.NewMetaClient(cfg.MetaURL, tokens)
	discoveries := service.NewDiscoveryService(db, func(tenantID uint) service.SecurityFactsReader { return metaClient.WithTenantID(tenantID) })
	if _, err := service.ReconcileStructuredOwnerProjections(ctx, db, time.Now().UTC()); err != nil {
		log.Fatalf("Security structured owner projection reconciliation failed: %v", err)
	}
	workerID := fmt.Sprintf("security-%d-%s", os.Getpid(), uuid.NewString())
	runner, err := securityworker.NewDiscoveryRunner(discoveries, workerID)
	if err != nil {
		log.Fatalf("Security discovery runner configuration failed: %v", err)
	}
	client := commonclient.NewSystemServiceClient(cfg.SystemURL, tokens, nil)
	registration := client.RegisterAndHeartbeat(ctx, &commonclient.ModuleRegistrationRequest{ModuleName: "security", InstanceID: workerID, RoutePrefix: "/security", Role: commonclient.ModuleRuntimeRoleWorker, Metadata: map[string]interface{}{"module": "security", "role": "worker", "runtime_name": commonexecution.TaskTypeSensitiveDataDiscovery, "capacity": 1, "capabilities": map[string]interface{}{"detection": map[string]interface{}{"enabled": true}}}})
	modulelifecycle.CancelRuntimeOnFatal(registration, stop)
	runner.Run(ctx, registration.IsRegistered)
	<-registration.Done()
}
