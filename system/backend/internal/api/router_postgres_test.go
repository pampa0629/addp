package api

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/addp/system/internal/migration"
	"github.com/addp/system/internal/models"
	"github.com/addp/system/internal/repository"
	"github.com/addp/system/internal/service"
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
	for _, table := range []string{
		"principals", "tenant_invitations", "oauth_clients", "engines",
		"api_consumers", "api_consumer_service_grants", "api_consumer_credentials",
		"module_definitions", "module_runtime_instances",
	} {
		var exists bool
		if err := db.Raw(`SELECT to_regclass('system.' || ?) IS NOT NULL`, table).Scan(&exists).Error; err != nil || !exists {
			t.Fatalf("target table %s exists=%t err=%v", table, exists, err)
		}
	}
	for _, obsoleteTable := range []string{"applications", "api_keys"} {
		var exists bool
		if err := db.Raw(`SELECT to_regclass('system.' || ?) IS NOT NULL`, obsoleteTable).Scan(&exists).Error; err != nil || exists {
			t.Fatalf("obsolete table %s exists=%t err=%v", obsoleteTable, exists, err)
		}
	}
	assertOfflineTimeQueryAgainstPostgres(t, db)

	cfg := testIAMRuntimeConfig()
	router := SetupRouter(db, cfg)
	routes := router.Routes()
	var runtimeEngineRegistration bool
	for _, route := range routes {
		if route.Method == "POST" && route.Path == "/api/v1/system/runtime/engines" {
			runtimeEngineRegistration = true
		}
		if route.Path == "/api/v1/internal/audit-logs" {
			t.Fatalf("legacy internal audit route is still registered")
		}
	}
	if !runtimeEngineRegistration {
		t.Fatal("Bearer runtime engine registration route is missing")
	}
}

func assertOfflineTimeQueryAgainstPostgres(t *testing.T, db *gorm.DB) {
	t.Helper()
	// Roll back all runtime facts created by this assertion in the standard test DB.
	tx := db.Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	defer tx.Rollback()
	repo := repository.NewModuleRegistryRepository(tx)
	registry := service.NewModuleRegistryService(repo)
	now := time.Now().UTC().Truncate(time.Second)
	offlineAt := now.Add(-5 * time.Minute)
	if err := registry.Register(&models.ModuleRegistrationRequest{
		ModuleName: "manager", InstanceID: "offline-time-postgres-probe", Role: models.ModuleRuntimeRoleBackend,
		ModuleURL: "http://manager.local:8081", RoutePrefix: "/manager", ProcessStartedAt: now.Add(-7 * 24 * time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Model(&models.ModuleRuntimeInstance{}).Where("instance_id = ?", "offline-time-postgres-probe").Updates(map[string]interface{}{
		"registered_at": now.Add(-7 * 24 * time.Hour), "lease_expires_at": offlineAt,
	}).Error; err != nil {
		t.Fatal(err)
	}
	filter := models.ModuleRuntimeInstanceFilter{
		ModuleName: "manager", Status: "down", TimeBasis: "offline", Page: 1, PageSize: 10,
		TimeFrom: now.Add(-time.Hour).In(time.FixedZone("query-offset", 9*3600)), TimeTo: now,
	}
	rows, total, err := registry.ListModuleRuntimeInstances(filter)
	if err != nil || total != 1 || len(rows) != 1 || rows[0].InstanceID != "offline-time-postgres-probe" || rows[0].StoppedAt == nil || !rows[0].StoppedAt.Equal(offlineAt) {
		t.Fatalf("PostgreSQL offline time/offset projection: rows=%#v total=%d err=%v", rows, total, err)
	}
	filter.TimeBasis = "registered"
	_, total, err = registry.ListModuleRuntimeInstances(filter)
	if err != nil || total != 0 {
		t.Fatalf("PostgreSQL registration range included old process: total=%d err=%v", total, err)
	}
	if _, err := repo.MarkStaleModules(now); err != nil {
		t.Fatal(err)
	}
	filter.TimeBasis = "offline"
	_, total, err = registry.ListModuleRuntimeInstances(filter)
	if err != nil || total != 1 {
		t.Fatalf("PostgreSQL scan changed offline membership: total=%d err=%v", total, err)
	}
	if err := registry.SendHeartbeat("manager", "offline-time-postgres-probe"); err != nil {
		t.Fatal(err)
	}
	_, total, err = registry.ListModuleRuntimeInstances(filter)
	if err != nil || total != 0 {
		t.Fatalf("PostgreSQL recovered process remained offline: total=%d err=%v", total, err)
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
