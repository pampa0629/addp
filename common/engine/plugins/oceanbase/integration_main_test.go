package oceanbase

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/engine/plugin"
)

var oceanBaseDisposableDatabasePattern = regexp.MustCompile(`^[A-Za-z0-9_]+$`)

func TestMain(m *testing.M) {
	if os.Getenv("ADDP_OCEANBASE_INTEGRATION") != "1" {
		os.Exit(m.Run())
	}

	database := oceanBaseIntegrationEnv("ADDP_TEST_OCEANBASE_DATABASE", "addp_oceanbase_disposable")
	if !oceanBaseDisposableDatabasePattern.MatchString(database) || !strings.Contains(database, "disposable") {
		fmt.Fprintf(os.Stderr, "ADDP_TEST_OCEANBASE_DATABASE must be an identifier containing disposable, got %q\n", database)
		os.Exit(1)
	}
	if strings.TrimSpace(os.Getenv("ADDP_TEST_OCEANBASE_PASSWORD")) == "" {
		fmt.Fprintln(os.Stderr, "ADDP_TEST_OCEANBASE_PASSWORD is required")
		os.Exit(1)
	}

	p := &Plugin{}
	adminInfo := oceanBaseIntegrationConnInfo()
	adminInfo["database"] = "oceanbase"
	dsn, err := p.BuildDSN(adminInfo)
	if err != nil {
		fmt.Fprintf(os.Stderr, "build OceanBase admin DSN: %v\n", err)
		os.Exit(1)
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		fmt.Fprintf(os.Stderr, "open OceanBase admin connection: %v\n", err)
		os.Exit(1)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	if err := resetOceanBaseIntegrationDatabase(ctx, db, database); err != nil {
		cancel()
		_ = db.Close()
		fmt.Fprintf(os.Stderr, "prepare disposable OceanBase database: %v\n", err)
		os.Exit(1)
	}
	cancel()

	code := m.Run()
	plugin.CloseAllPools()
	cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
	if _, err := db.ExecContext(cleanupCtx, "DROP DATABASE IF EXISTS `"+database+"`"); err != nil {
		fmt.Fprintf(os.Stderr, "drop disposable OceanBase database: %v\n", err)
		if code == 0 {
			code = 1
		}
	}
	cleanupCancel()
	if err := db.Close(); err != nil {
		fmt.Fprintf(os.Stderr, "close OceanBase admin connection: %v\n", err)
		if code == 0 {
			code = 1
		}
	}
	os.Exit(code)
}

func resetOceanBaseIntegrationDatabase(ctx context.Context, db *sql.DB, database string) error {
	if _, err := db.ExecContext(ctx, "DROP DATABASE IF EXISTS `"+database+"`"); err != nil {
		return err
	}
	_, err := db.ExecContext(ctx, "CREATE DATABASE `"+database+"` DEFAULT CHARACTER SET utf8mb4")
	return err
}
