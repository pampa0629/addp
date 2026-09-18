package main

import (
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/addp/common/client"
	commonconfig "github.com/addp/common/config"
	"github.com/addp/common/modulelifecycle"
	"github.com/addp/ontology/internal/api"
	"github.com/addp/ontology/internal/config"
	"github.com/addp/ontology/internal/falkor"
	"github.com/addp/ontology/internal/repository"
	"github.com/addp/ontology/internal/service"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// @title ADDP Ontology API
// @version 1.0
// @description 租户本体修订与投影管理 | Tenant ontology revision and projection management
// @host localhost:8195
// @BasePath /api/v1/ontology
// @securityDefinitions.apikey BearerAuth
// @in header
// @name Authorization
func main() {
	if err := run(); err != nil {
		log.Print("Ontology backend stopped: ", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	// Do not log DSNs, bound definitions or execution authorization facts.
	db, err := gorm.Open(postgres.Open(cfg.DatabaseDSN()), &gorm.Config{TranslateError: true, Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		return errors.New("ontology database connection failed")
	}
	sqlDB, err := db.DB()
	if err != nil {
		return errors.New("ontology database pool failed")
	}
	defer sqlDB.Close()
	sqlDB.SetMaxOpenConns(16)
	sqlDB.SetMaxIdleConns(4)
	sqlDB.SetConnMaxLifetime(30 * time.Minute)
	if err := repository.Migrate(db); err != nil {
		return errors.New("ontology schema migration failed")
	}
	graph, err := falkor.New(falkor.Config{Address: cfg.FalkorAddress, Password: cfg.FalkorPassword})
	if err != nil {
		return err
	}
	tokens, err := client.NewOAuthServiceTokenSource(cfg.SystemURL, "addp-ontology", cfg.ServiceClientSecret, nil)
	if err != nil {
		return errors.New("ontology service identity configuration failed")
	}
	system := client.NewSystemServiceClient(cfg.SystemURL, tokens, nil)
	repo := repository.NewRevisionRepository(db)
	authorizer, err := service.NewSystemProjectionAuthorizer(system)
	if err != nil {
		return err
	}
	executor, err := service.NewProjectionExecutor(repo, graph, authorizer)
	if err != nil {
		return err
	}
	supervisor, err := service.NewProjectionSupervisor(repo, executor, "ontology-"+uuid.NewString())
	if err != nil {
		return err
	}
	lifecycle := modulelifecycle.NewBusiness("ontology", client.ModuleRuntimeRoleBackend, readyCheck("postgres", sqlDB.PingContext), readyCheck("falkordb", graph.Health))
	issuer := client.NewSystemExecutionAuthorizationClient(cfg.SystemURL, &http.Client{Timeout: 20 * time.Second})
	router := api.SetupRouter(cfg.SystemURL, lifecycle, service.NewRevisionService(repo), issuer)
	listener, err := net.Listen("tcp", ":"+cfg.Port)
	if err != nil {
		return errors.New("ontology listener failed")
	}
	server := &http.Server{Handler: router, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 40 * time.Second, WriteTimeout: 45 * time.Second, IdleTimeout: 60 * time.Second,
		BaseContext: func(net.Listener) context.Context { return ctx }}
	serverDone := make(chan error, 1)
	go func() { err := server.Serve(listener); serverDone <- err; stop() }()
	serviceURL := commonconfig.BuildServiceURL(commonconfig.GetServiceHost(), cfg.Port)
	registration := system.RegisterAndHeartbeatWithMetadata(ctx, "ontology", serviceURL, "/ontology", map[string]interface{}{"module": "ontology"})
	lifecycle.AttachRegistration(registration)
	modulelifecycle.CancelRuntimeOnFatal(registration, stop)
	workerDone := make(chan error, 1)
	go func() {
		workerDone <- supervisor.Run(ctx, func() bool { _, ready := lifecycle.Readiness(ctx); return ready })
	}()
	<-ctx.Done()
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdown); err != nil {
		_ = server.Close()
	}
	<-workerDone
	<-registration.Done()
	if err := <-serverDone; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return errors.New("ontology HTTP server failed")
	}
	if registration.Snapshot().State == client.ModuleRegistrationFailed {
		return errors.New("ontology module registration failed")
	}
	return nil
}

func readyCheck(name string, check func(context.Context) error) modulelifecycle.CheckFunc {
	return func(ctx context.Context) modulelifecycle.CheckResult {
		bounded, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		if check(bounded) != nil {
			return modulelifecycle.CheckResult{Name: name, Status: modulelifecycle.CheckNotReady, ErrorCode: name + "_unavailable"}
		}
		return modulelifecycle.CheckResult{Name: name, Status: modulelifecycle.CheckReady}
	}
}
