package service

import (
	"errors"
	commonAPI "github.com/addp/common/api"
	"github.com/addp/quality/internal/repository"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"testing"
)

func TestStandardReferenceGuardRejectsTerminalTransitionAsConflict(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	for _, query := range []string{
		`ATTACH DATABASE ':memory:' AS quality`,
		`CREATE TABLE quality.standard_reference_guards (id INTEGER PRIMARY KEY, tenant_id INTEGER, resource_type TEXT, resource_id INTEGER, state TEXT, created_at DATETIME, updated_at DATETIME, UNIQUE(tenant_id,resource_type,resource_id))`,
		`INSERT INTO quality.standard_reference_guards (tenant_id,resource_type,resource_id,state) VALUES (7,'domain',42,'deleted')`,
	} {
		if err := db.Exec(query).Error; err != nil {
			t.Fatal(err)
		}
	}
	svc := NewStandardReferenceGuardService(repository.NewStandardReferenceGuardRepository(db))
	for _, state := range []string{"open", "frozen"} {
		if _, err := svc.SetState(7, 42, state); !errors.Is(err, commonAPI.ErrConflict) {
			t.Fatalf("state %s: %v", state, err)
		}
	}
	if _, err := svc.SetState(7, 42, "deleted"); err != nil {
		t.Fatalf("idempotent finalize: %v", err)
	}
	if _, err := svc.SetState(7, 42, "invalid"); !errors.Is(err, commonAPI.ErrBadRequest) {
		t.Fatalf("invalid state: %v", err)
	}
}
