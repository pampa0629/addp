package main

import (
	"context"
	"encoding/json"
	"github.com/addp/system/internal/iam"
	"github.com/addp/system/internal/migration"
	"github.com/addp/system/internal/testsupport"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCatalogBrowserIdentitiesAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("ADDP_SYSTEM_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("requires standard System PostgreSQL gate")
	}
	testsupport.RequireDisposablePostgresDSN(t, dsn)
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := db.DB()
	defer raw.Close()
	reset := func() {
		if err := db.Exec("DROP SCHEMA IF EXISTS system CASCADE; DROP SCHEMA IF EXISTS common CASCADE").Error; err != nil {
			t.Fatal(err)
		}
	}
	reset()
	defer reset()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	if err := migration.NewRunner(dsn).Run(ctx); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "identities.json")
	id := "11111111-1111-4111-8111-111111111111"
	if err := prepareCatalogBrowserUsers(db, path, id, time.Hour); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0600 {
		t.Fatal("public credentials")
	}
	data, _ := os.ReadFile(path)
	var values catalogBrowserCredentials
	if err := json.Unmarshal(data, &values); err != nil {
		t.Fatal(err)
	}
	if len(values.Users) != 3 || values.RunID != id {
		t.Fatal("invalid credentials")
	}
	var tokens int64
	if err := db.Table("system.access_tokens").Count(&tokens).Error; err != nil || tokens != 0 {
		t.Fatal("fixture must not issue tokens", err)
	}
	repo := iam.NewRepository(db)
	memberships := iam.NewTenantMembershipService(repo, nil)
	for _, user := range values.Users {
		member, err := memberships.GetManagedMembership(ctx, values.TenantID, user.MembershipID)
		if err != nil || member.Status != iam.TenantMembershipStatusActive {
			t.Fatal("membership missing", err)
		}
		var count int64
		if err := db.Table("system.role_assignments").Where("principal_id = ? AND valid_until IS NOT NULL", user.PrincipalID).Count(&count).Error; err != nil || count != 1 {
			t.Fatal("assignment must expire", err)
		}
	}
	if err := releaseCatalogBrowserUsers(db, path, id, "preparer"); err != nil {
		t.Fatal(err)
	}
	for name, user := range values.Users {
		member, err := memberships.GetManagedMembership(ctx, values.TenantID, user.MembershipID)
		if err != nil {
			t.Fatal(err)
		}
		if (member.Status == iam.TenantMembershipStatusEnded) != (name == "preparer") {
			t.Fatal("release scope leaked", name)
		}
	}
	if err := releaseCatalogBrowserUsers(db, path, id, "all"); err != nil {
		t.Fatal(err)
	}
	if err := releaseCatalogBrowserUsers(db, path, id, "all"); err != nil {
		t.Fatal("release must be idempotent", err)
	}
	for _, user := range values.Users {
		member, _ := memberships.GetManagedMembership(ctx, values.TenantID, user.MembershipID)
		if member.Status != iam.TenantMembershipStatusEnded {
			t.Fatal("live membership residue")
		}
	}
}
