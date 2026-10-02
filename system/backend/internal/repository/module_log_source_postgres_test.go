package repository_test

import (
	"context"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/addp/system/internal/migration"
	"github.com/addp/system/internal/testsupport"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestModuleLogSourceCatalogAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("ADDP_SYSTEM_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("requires standard System PostgreSQL gate")
	}
	testsupport.RequireDisposablePostgresDSN(t, dsn)
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", "system")
	u.RawQuery = q.Encode()
	db, err := gorm.Open(postgres.Open(u.String()), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	reset := func() {
		if err := db.Exec("DROP SCHEMA IF EXISTS system CASCADE; DROP SCHEMA IF EXISTS common CASCADE").Error; err != nil {
			t.Fatal(err)
		}
	}
	reset()
	defer reset()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	runner := migration.NewRunner(dsn)
	if err = runner.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if err = runner.Run(ctx); err != nil {
		t.Fatal("migration idempotence", err)
	}
	var count int64
	if err = db.Raw(`SELECT count(*) FROM role_permissions rp JOIN roles r ON r.id=rp.role_id JOIN permissions p ON p.id=rp.permission_id WHERE p.permission_key='system.module_log_source.create' AND r.role_key='platform.log_observer_runtime'`).Scan(&count).Error; err != nil || count != 1 {
		t.Fatal("observer grant", count, err)
	}
	exerciseLogSourceCatalog(t, db)
}
