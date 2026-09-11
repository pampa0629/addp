package tidb

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

var tidbDisposableDatabasePattern = regexp.MustCompile(`^[A-Za-z0-9_]+$`)

func TestMain(m *testing.M) {
	if os.Getenv("ADDP_TIDB_INTEGRATION") != "1" {
		os.Exit(m.Run())
	}

	database := tidbIntegrationEnv("ADDP_TEST_TIDB_DATABASE", "addp_tidb_disposable")
	if !tidbDisposableDatabasePattern.MatchString(database) || !strings.Contains(database, "disposable") {
		fmt.Fprintf(os.Stderr, "ADDP_TEST_TIDB_DATABASE must be an identifier containing disposable, got %q\n", database)
		os.Exit(1)
	}

	p := &Plugin{}
	adminInfo := tidbIntegrationConnInfo()
	adminInfo["database"] = "mysql"
	dsn, err := p.BuildDSN(adminInfo)
	if err != nil {
		fmt.Fprintf(os.Stderr, "build TiDB admin DSN: %v\n", err)
		os.Exit(1)
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		fmt.Fprintf(os.Stderr, "open TiDB admin connection: %v\n", err)
		os.Exit(1)
	}
	deadline := time.Now().Add(3 * time.Minute)
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		err = resetTiDBIntegrationDatabase(ctx, db, database)
		cancel()
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			_ = db.Close()
			fmt.Fprintf(os.Stderr, "prepare disposable TiDB database: %v\n", err)
			os.Exit(1)
		}
		time.Sleep(2 * time.Second)
	}

	code := m.Run()
	plugin.CloseAllPools()
	cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 45*time.Second)
	if _, err := db.ExecContext(cleanupCtx, "DROP DATABASE IF EXISTS `"+database+"`"); err != nil {
		fmt.Fprintf(os.Stderr, "drop disposable TiDB database: %v\n", err)
		if code == 0 {
			code = 1
		}
	}
	cleanupCancel()
	if err := db.Close(); err != nil {
		fmt.Fprintf(os.Stderr, "close TiDB admin connection: %v\n", err)
		if code == 0 {
			code = 1
		}
	}
	os.Exit(code)
}

func resetTiDBIntegrationDatabase(ctx context.Context, db *sql.DB, database string) error {
	if _, err := db.ExecContext(ctx, "DROP DATABASE IF EXISTS `"+database+"`"); err != nil {
		return err
	}
	_, err := db.ExecContext(ctx, "CREATE DATABASE `"+database+"` DEFAULT CHARACTER SET utf8mb4")
	return err
}
