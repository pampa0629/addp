package iam

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/system/internal/migration"
	"github.com/addp/system/internal/testsupport"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestRepositoryAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("ADDP_IAM_RUNTIME_TEST_DSN")
	if dsn == "" {
		t.Skip("set ADDP_IAM_RUNTIME_TEST_DSN to a disposable PostgreSQL 15+ database")
	}
	testsupport.RequireDisposablePostgresDSN(t, dsn)

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`DROP SCHEMA IF EXISTS system CASCADE`).Error; err != nil {
		t.Fatalf("reset IAM runtime test schema: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := migration.NewRunner(dsn).Run(ctx); err != nil {
		t.Fatalf("apply IAM migrations: %v", err)
	}

	repository := NewRepository(db)
	now := time.Now().UTC().Truncate(time.Microsecond)
	var principalID, tenantID int64
	if err := repository.Transaction(ctx, func(tx *Repository) error {
		principal := &Principal{
			PrincipalType: PrincipalTypeUser,
			Status:        PrincipalStatusActive,
		}
		if err := tx.CreatePrincipal(ctx, principal); err != nil {
			return err
		}
		principalID = principal.ID

		email := "alice@example.com"
		locale := "zh-cn"
		if err := tx.CreateUser(ctx, &User{
			ID:           principal.ID,
			DisplayName:  "Alice",
			PrimaryEmail: &email,
			Locale:       &locale,
		}); err != nil {
			return err
		}
		if err := tx.CreateLocalAccount(ctx, &LocalAccount{
			UserID:            principal.ID,
			Username:          "Alice",
			PasswordHash:      "$2a$10$runtime-test-password-hash",
			Status:            LocalAccountStatusActive,
			PasswordChangedAt: now,
		}); err != nil {
			return err
		}

		tenant := &Tenant{
			Code:        " Research-Lab ",
			Name:        "Research Lab",
			Description: "Repository integration test tenant",
			Status:      TenantStatusActive,
		}
		if err := tx.CreateTenant(ctx, tenant); err != nil {
			return err
		}
		tenantID = tenant.ID
		return tx.CreateTenantMembership(ctx, &TenantMembership{
			TenantID:             tenant.ID,
			PrincipalID:          principal.ID,
			Status:               TenantMembershipStatusActive,
			SourceType:           TenantMembershipSourceManual,
			JoinedAt:             now.Add(-time.Minute),
			CreatedByPrincipalID: &principal.ID,
		})
	}); err != nil {
		t.Fatalf("create local user identity and membership: %v", err)
	}

	identity, err := repository.GetLocalUserIdentityByUsername(ctx, "  ＡＬＩＣＥ ")
	if err != nil {
		t.Fatalf("find normalized local identity: %v", err)
	}
	if identity.PrincipalID != principalID || identity.NormalizedUsername != "alice" {
		t.Fatalf("local identity = %#v", identity)
	}
	if identity.AuthorizationVersion != 2 {
		t.Fatalf("authorization version = %d, want 2 after Membership insert", identity.AuthorizationVersion)
	}

	memberships, err := repository.ListEffectiveTenantMemberships(ctx, principalID, now)
	if err != nil {
		t.Fatalf("list effective Tenant Memberships: %v", err)
	}
	if len(memberships) != 1 || memberships[0].TenantID != tenantID || memberships[0].TenantCode != "research-lab" {
		t.Fatalf("effective Tenant Memberships = %#v", memberships)
	}

	err = repository.Transaction(ctx, func(tx *Repository) error {
		duplicatePrincipal := &Principal{PrincipalType: PrincipalTypeUser, Status: PrincipalStatusActive}
		if err := tx.CreatePrincipal(ctx, duplicatePrincipal); err != nil {
			return err
		}
		if err := tx.CreateUser(ctx, &User{ID: duplicatePrincipal.ID, DisplayName: "Duplicate Alice"}); err != nil {
			return err
		}
		return tx.CreateLocalAccount(ctx, &LocalAccount{
			UserID:            duplicatePrincipal.ID,
			Username:          "Ａlice",
			PasswordHash:      "$2a$10$duplicate-runtime-test-hash",
			Status:            LocalAccountStatusActive,
			PasswordChangedAt: now,
		})
	})
	if !errors.Is(err, commonapi.ErrConflict) {
		t.Fatalf("duplicate normalized username error = %v, want conflict", err)
	}
	assertRuntimeTableCount(t, db, "system.principals", 1)
	assertRuntimeTableCount(t, db, "system.users", 1)
	assertRuntimeTableCount(t, db, "system.local_accounts", 1)

	if _, err := repository.GetPrincipal(ctx, 999999); !errors.Is(err, commonapi.ErrNotFound) {
		t.Fatalf("missing Principal error = %v, want not found", err)
	}
	if err := db.Model(&Tenant{}).Where("id = ?", tenantID).Update("status", TenantStatusSuspended).Error; err != nil {
		t.Fatalf("suspend Tenant: %v", err)
	}
	memberships, err = repository.ListEffectiveTenantMemberships(ctx, principalID, now)
	if err != nil {
		t.Fatalf("list Memberships after Tenant suspension: %v", err)
	}
	if len(memberships) != 0 {
		t.Fatalf("suspended Tenant remained effective: %#v", memberships)
	}
}

func assertRuntimeTableCount(t *testing.T, db *gorm.DB, tableName string, want int64) {
	t.Helper()
	var got int64
	if err := db.Table(tableName).Count(&got).Error; err != nil {
		t.Fatalf("count %s: %v", tableName, err)
	}
	if got != want {
		t.Fatalf("%s count = %d, want %d", tableName, got, want)
	}
}
