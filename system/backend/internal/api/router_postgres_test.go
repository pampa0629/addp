package api

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/addp/system/internal/migration"
	"github.com/addp/system/internal/repository"
	"github.com/addp/system/internal/testsupport"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestTargetSystemCompositionAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("ADDP_SYSTEM_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("set ADDP_SYSTEM_POSTGRES_TEST_DSN to a disposable PostgreSQL 15+ database")
	}
	testsupport.RequireDisposablePostgresDSN(t, dsn)
	pgxConfig, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse target System PostgreSQL DSN: %v", err)
	}
	pgxConfig.RuntimeParams["search_path"] = "system"
	sqlDB := stdlib.OpenDB(*pgxConfig)
	defer sqlDB.Close()
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB}), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`DROP SCHEMA IF EXISTS system CASCADE`).Error; err != nil {
		t.Fatalf("reset target System schema: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := migration.NewRunner(dsn).Run(ctx); err != nil {
		t.Fatalf("run target IAM migrations: %v", err)
	}
	before := tableColumns(t, db, "principals")
	if err := repository.AutoMigrateNonIAM(db); err != nil {
		t.Fatalf("run non-IAM AutoMigrate: %v", err)
	}
	after := tableColumns(t, db, "principals")
	if before != after {
		t.Fatalf("non-IAM AutoMigrate changed principals columns: before=%q after=%q", before, after)
	}
	for _, table := range []string{"principals", "tenant_invitations", "oauth_clients", "engines", "applications", "module_registry"} {
		var exists bool
		if err := db.Raw(`SELECT to_regclass('system.' || ?) IS NOT NULL`, table).Scan(&exists).Error; err != nil || !exists {
			t.Fatalf("target table %s exists=%t err=%v", table, exists, err)
		}
	}

	cfg := testIAMRuntimeConfig()
	cfg.InternalAPIKey = "target-internal-key"
	router := SetupRouter(db, cfg)
	payload := []byte(`{
		"event_name":"http.request.completed",
		"result":"succeeded",
		"risk_level":"medium",
		"module_name":"manager",
		"http_method":"POST",
		"resource_path":"/api/v1/manager/items",
		"http_status":201,
		"request_id":"target-composition-audit",
		"ip_address":"127.0.0.1",
		"entity_type":"data_item",
		"entity_id":"42",
		"details":{"duration_ms":12}
	}`)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/internal/audit-logs", bytes.NewReader(payload))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Internal-API-Key", cfg.InternalAPIKey)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("internal audit status = %d body=%s", response.Code, response.Body.String())
	}
	var auditCount int64
	if err := db.Raw(`SELECT count(*) FROM system.audit_logs WHERE request_id = 'target-composition-audit'`).Scan(&auditCount).Error; err != nil || auditCount != 1 {
		t.Fatalf("internal audit count = %d err=%v", auditCount, err)
	}
}

func tableColumns(t *testing.T, db *gorm.DB, table string) string {
	t.Helper()
	var columns string
	if err := db.Raw(`
		SELECT string_agg(column_name, ',' ORDER BY ordinal_position)
		FROM information_schema.columns
		WHERE table_schema = 'system' AND table_name = ?
	`, table).Scan(&columns).Error; err != nil {
		t.Fatalf("read %s columns: %v", table, err)
	}
	return columns
}
