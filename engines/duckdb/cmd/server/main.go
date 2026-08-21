package main

import (
	"context"
	"log"
	"net"
	"time"

	commonclient "github.com/addp/common/client"
	commonconfig "github.com/addp/common/config"
	commonmodels "github.com/addp/common/models"
	duckapi "github.com/addp/engines/duckdb/internal/api"
	"github.com/addp/engines/duckdb/internal/config"
	"github.com/addp/engines/duckdb/internal/duckdb"
	duckruntime "github.com/addp/engines/duckdb/internal/runtime"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}
	verifyCtx, cancelVerify := context.WithTimeout(context.Background(), 30*time.Second)
	if err := duckdb.VerifyRequiredExtensions(verifyCtx); err != nil {
		cancelVerify()
		log.Fatalf("DuckDB extension verification failed: %v", err)
	}
	cancelVerify()
	tokens, err := commonclient.NewOAuthServiceTokenSource(cfg.SystemURL, "addp-duckdb", cfg.ClientSecret, nil)
	if err != nil {
		log.Fatal(err)
	}
	systemClient := commonclient.NewSystemServiceClient(cfg.SystemURL, tokens, nil)
	metaClient := commonclient.NewMetaClient(cfg.MetaURL, tokens)
	executor := duckruntime.NewExecutor(
		systemClient, metaClient, cfg.MaxRows, cfg.MaxMemory, cfg.Threads, cfg.DefaultTimeout, cfg.SourceLoopbackHost,
	)
	router, err := duckapi.NewRouter(duckapi.RouterConfig{SystemURL: cfg.SystemURL, AllowedCallerIDs: cfg.AllowedCallerIDs}, executor)
	if err != nil {
		log.Fatal(err)
	}
	listener, err := net.Listen("tcp", cfg.Addr)
	if err != nil {
		log.Fatal(err)
	}
	systemClient.RegisterRuntimeEngineWithRetry(context.Background(), &commonmodels.CapabilityRegistrationRequest{
		Name:        "DuckDB",
		EngineType:  "duckdb",
		IsBuiltin:   true,
		Description: "ADDP 内置联邦只读查询 Runtime",
		ConnectionInfo: map[string]interface{}{
			"protocol": "http",
			"host":     commonconfig.GetServiceHost(),
			"port":     cfg.Port,
		},
	}, time.Second, 30*time.Second)
	if err := router.RunListener(listener); err != nil {
		log.Fatal(err)
	}
}
