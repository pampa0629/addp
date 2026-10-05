package main

import (
	"context"
	"github.com/addp/system/internal/iam"
	"github.com/addp/system/internal/migration"
	"github.com/addp/system/internal/testsupport"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"os"
	"testing"
	"time"
)

func TestRasterPolicyIdentitiesAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("ADDP_SYSTEM_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("requires standard System PostgreSQL gate")
	}
	testsupport.RequireDisposablePostgresDSN(t, dsn)
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
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
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	if err := migration.NewRunner(dsn).Run(ctx); err != nil {
		t.Fatal(err)
	}
	repo := iam.NewRepository(db)
	identity := iam.NewIdentityService(repo, nil)
	admin, err := createUser(ctx, identity, "raster-tenant-admin")
	if err != nil {
		t.Fatal(err)
	}
	tenant, err := iam.NewPlatformTenantService(repo, nil).Create(ctx, iam.CreateTenantInput{Code: "raster-test", Name: "Raster", InitialAdministratorPrincipalID: admin.PrincipalID, ActorPrincipalID: admin.PrincipalID, Audit: audit("raster-tenant")})
	if err != nil {
		t.Fatal(err)
	}
	tokens, err := iam.NewTokenFamilyService(repo, iam.BrowserSessionConfig{ResourceTicketOwners: []string{"manager"}}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	selection, err := iam.NewContextSelectionService(repo, tokens)
	if err != nil {
		t.Fatal(err)
	}
	values, err := rasterPolicyIdentities(ctx, repo, identity, selection, admin.PrincipalID)
	if err != nil {
		t.Fatal(err)
	}
	contexts, err := iam.NewAuthContextService(repo)
	if err != nil {
		t.Fatal(err)
	}
	for _, scope := range []string{"platform", "tenant"} {
		key := "ADDP_ONLINE_RASTER_POLICY_PLATFORM_TOKEN"
		if scope == "tenant" {
			key = "ADDP_ONLINE_RASTER_POLICY_TENANT_TOKEN"
		}
		projection, err := contexts.ResolveAccessToken(ctx, values[key])
		if err != nil {
			t.Fatal(scope, err)
		}
		if projection.Context.Type != scope {
			t.Fatal("wrong context", projection.Context)
		}
		if scope == "platform" && projection.Authentication.AssuranceLevel != "aal2" {
			t.Fatal("platform assurance", projection.Authentication)
		}
		read, update := false, false
		for _, assignment := range projection.Authorization.RoleAssignments {
			for _, permission := range assignment.Permissions {
				read = read || permission == "system.engine_raster_policy.read"
				update = update || permission == "system.engine_raster_policy.update"
			}
		}
		if !read || !update {
			t.Fatal("missing management permission", scope)
		}
		if scope == "tenant" && projection.Context.TenantID == nil {
			t.Fatal("tenant identity missing", tenant.ID)
		}
	}
}
