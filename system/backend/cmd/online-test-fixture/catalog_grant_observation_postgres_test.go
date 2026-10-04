package main

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/addp/system/internal/migration"
	"github.com/addp/system/internal/testsupport"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestCatalogGrantObservationAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("ADDP_SYSTEM_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("requires disposable System PostgreSQL gate")
	}
	testsupport.RequireDisposablePostgresDSN(t, dsn)
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	connection, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Exec("DROP SCHEMA IF EXISTS system CASCADE").Error; err != nil {
			t.Errorf("cleanup System observation fixture: %v", err)
		}
		connection.Close()
	})
	if err := db.Exec("DROP SCHEMA IF EXISTS system CASCADE").Error; err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	if err := migration.NewRunner(dsn).Run(ctx); err != nil {
		t.Fatal(err)
	}
	tx := db.WithContext(ctx).Begin(&sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	defer tx.Rollback()
	id := uuid.New()
	result, err := readCatalogGrantObservation(tx, 42, 7, id)
	if err != nil || result.RequestID != id.String() || result.ReceiptCount != 0 || result.GrantCount != 0 || result.IssuanceAuditCount != 0 {
		t.Fatalf("PostgreSQL absent history: %#v %v", result, err)
	}
	// Even an accidental future write with no affected rows must be rejected.
	err = tx.Exec("UPDATE system.engine_access_grants SET granted_at=granted_at WHERE false").Error
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "25006" {
		t.Fatalf("read-only snapshot allowed a write: %v", err)
	}
}
