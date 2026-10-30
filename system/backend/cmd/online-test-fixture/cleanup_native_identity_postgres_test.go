package main

import (
	"context"
	"errors"
	"os"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/addp/system/internal/iam"
	"github.com/addp/system/internal/migration"
	"github.com/addp/system/internal/testsupport"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestCleanupNativeEngineIdentityAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("ADDP_SYSTEM_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("requires standard System PostgreSQL gate")
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
	reset := func() {
		if err := db.Exec("DROP SCHEMA IF EXISTS system CASCADE; DROP SCHEMA IF EXISTS common CASCADE").Error; err != nil {
			t.Error(err)
		}
	}
	t.Cleanup(func() { reset(); _ = connection.Close() })
	reset()
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	if err := migration.NewRunner(dsn).Run(ctx); err != nil {
		t.Fatal(err)
	}
	repository := iam.NewRepository(db)
	role, err := repository.GetActiveBuiltinRoleByKey(ctx, cleanupNativeEngineRoleKey)
	if err != nil {
		t.Fatal(err)
	}
	assertNoAssignments := func() {
		t.Helper()
		var count int64
		if err := db.Raw("SELECT count(*) FROM system.role_assignments WHERE role_id=?", role.ID).Scan(&count).Error; err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("lifecycle role was implicitly assigned: %d", count)
		}
	}
	assertNoAssignments()
	clock, err := testsupport.FixtureDatabaseClock(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	identity := iam.NewIdentityService(repository, nil)
	admin, err := createUser(ctx, identity, "native-role-admin")
	if err != nil {
		t.Fatal(err)
	}
	tenant, err := iam.NewPlatformTenantService(repository, clock).Create(ctx, iam.CreateTenantInput{
		Code: "native_roles", Name: "Native role fixture", InitialAdministratorPrincipalID: admin.PrincipalID,
		ActorPrincipalID: admin.PrincipalID, Audit: audit("native-role-tenant"),
	})
	if err != nil {
		t.Fatal(err)
	}
	assertNoAssignments()
	tokens, err := iam.NewTokenFamilyService(repository, fixtureSessionConfig("cleanup-native-effects"), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	selection, err := iam.NewContextSelectionService(repository, tokens)
	if err != nil {
		t.Fatal(err)
	}
	memberships, roles := iam.NewTenantMembershipService(repository, clock), iam.NewTenantRoleService(repository, clock)
	// The high-risk delete permission must retain the ordinary self-assignment MFA rule.
	var administratorMembershipID int64
	if err := db.Raw("SELECT id FROM system.tenant_memberships WHERE tenant_id=? AND principal_id=?", tenant.ID, admin.PrincipalID).Scan(&administratorMembershipID).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := roles.CreateAssignments(ctx, iam.CreateTenantRoleAssignmentsInput{
		TenantID: tenant.ID, MembershipID: administratorMembershipID, RoleIDs: []int64{role.ID},
		ActorPrincipalID: admin.PrincipalID, Reason: "Negative native self-assignment", Audit: audit("native-role-self-assignment"),
	}); !errors.Is(err, iam.ErrStepUpRequired) {
		t.Fatalf("lifecycle self-assignment bypassed MFA: %v", err)
	}
	assertNoAssignments()
	// The original construction must remain forbidden; the fix does not loosen IAM.
	if _, err := roles.CreateRole(ctx, iam.CreateTenantRoleInput{
		TenantID: tenant.ID, RoleKey: "online.native_invalid", Name: "Invalid native role", PermissionKeys: cleanupNativeEnginePermissions,
		ActorPrincipalID: admin.PrincipalID, Audit: audit("native-role-invalid"),
	}); err == nil {
		t.Fatal("non-customizable permissions became assignable through a custom role")
	}
	for _, subject := range cleanupNativeFixtureSubjects {
		if subject.name != "external-online-cleanup-native-engine" {
			continue
		}
		issued, _, _, err := createPermissionFixture(ctx, repository, identity, memberships, roles, selection,
			tenant.ID, admin.PrincipalID, subject.name, subject.permissions, subject.builtinRoles...)
		if err != nil {
			t.Fatal(err)
		}
		contexts, err := iam.NewAuthContextService(repository)
		if err != nil {
			t.Fatal(err)
		}
		auth, err := contexts.ResolveAccessToken(ctx, issued.AccessToken)
		if err != nil {
			t.Fatal(err)
		}
		if auth.Context.Type != "tenant" || auth.Context.TenantID == nil || *auth.Context.TenantID != strconv.FormatInt(tenant.ID, 10) || auth.Principal.Type != "user" {
			t.Fatal("fixture session escaped its Tenant User identity")
		}
		if len(auth.Authorization.RoleAssignments) != 2 {
			t.Fatal("Engine User must have exactly builtin and professional-result assignments")
		}
		var effective []string
		foundBuiltin := false
		for _, assignment := range auth.Authorization.RoleAssignments {
			if assignment.Scope.Type != "tenant" || assignment.Scope.TenantID == nil || *assignment.Scope.TenantID != *auth.Context.TenantID {
				t.Fatal("fixture authorization escaped its Tenant scope")
			}
			foundBuiltin = foundBuiltin || assignment.RoleKey == cleanupNativeEngineRoleKey
			effective = append(effective, assignment.Permissions...)
		}
		slices.Sort(effective)
		expected := slices.Clone(cleanupNativeEnginePermissions)
		slices.Sort(expected)
		if !foundBuiltin || !slices.Equal(effective, expected) {
			t.Fatalf("actual Engine User permissions differ: %v", effective)
		}
		return
	}
	t.Fatal("native Engine subject is missing")
}
