package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"net/url"
	"os"
	"time"

	"github.com/addp/system/internal/migration"
	_ "github.com/lib/pq"
)

func main() {
	apply := flag.Bool("apply", false, "apply the narrowly scoped migration 75 repair")
	flag.Parse()
	if !*apply {
		fmt.Fprintln(os.Stderr, "refusing mutation without --apply")
		os.Exit(2)
	}
	dsn := postgresDSN()
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		fmt.Fprintln(os.Stderr, "open migration repair database failed")
		os.Exit(1)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "connect migration repair database failed")
		os.Exit(1)
	}
	updated, err := migration.RepairDirtyExecutionAudienceMigration75(ctx, db)
	if err != nil {
		fmt.Fprintf(os.Stderr, "migration 75 repair failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("migration 75 repair completed; normalized_authorizations=%d; migration_state=74/clean\n", updated)
}

func postgresDSN() string {
	host := envOrDefault("POSTGRES_HOST", "localhost")
	port := envOrDefault("POSTGRES_PORT", "15432")
	user := envOrDefault("POSTGRES_USER", "addp")
	password := envOrDefault("POSTGRES_PASSWORD", "addp_password")
	database := envOrDefault("POSTGRES_DB", "addp")
	value := &url.URL{Scheme: "postgres", User: url.UserPassword(user, password), Host: host + ":" + port, Path: database}
	query := value.Query()
	query.Set("sslmode", "disable")
	value.RawQuery = query.Encode()
	return value.String()
}

func envOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
