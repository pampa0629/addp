package main

import (
	"context"
	"log"
	"time"

	commonclient "github.com/addp/common/client"
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
	if err := router.Run(cfg.Addr); err != nil {
		log.Fatal(err)
	}
}
