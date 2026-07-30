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

func TestTenantAdministrationClosureAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("ADDP_SYSTEM_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("set ADDP_SYSTEM_POSTGRES_TEST_DSN to a disposable PostgreSQL 15+ database")
	}
	testsupport.RequireDisposablePostgresDSN(t, dsn)
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`DROP SCHEMA IF EXISTS system CASCADE`).Error; err != nil {
		t.Fatalf("reset tenant administration test schema: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := migration.NewRunner(dsn).Run(ctx); err != nil {
		t.Fatalf("apply IAM migrations: %v", err)
	}

	repository := NewRepository(db)
	currentTime := time.Now().UTC().Truncate(time.Microsecond)
	now := func() time.Time { return currentTime }
	identityService := NewIdentityService(repository, now)
	userService := NewPlatformUserService(repository, identityService, now)
	tenantService := NewPlatformTenantService(repository, now)
	membershipService := NewTenantMembershipService(repository, now)
	roleService := NewTenantRoleService(repository, now)
	tokenService, err := NewTokenFamilyService(repository, BrowserSessionConfig{
		ResourceTicketOwners: []string{"system"},
	}, nil, now)
	if err != nil {
		t.Fatalf("create tenant administration TokenFamilyService: %v", err)
	}
	selectionService, err := NewContextSelectionService(repository, tokenService)
	if err != nil {
		t.Fatalf("create tenant administration ContextSelectionService: %v", err)
	}
	mfaCipher, err := NewMFACredentialCipher([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatalf("create tenant administration MFA cipher: %v", err)
	}
	mfaService, err := NewMFAService(repository, mfaCipher, MFAServiceConfig{}, nil, now)
	if err != nil {
		t.Fatalf("create tenant administration MFA service: %v", err)
	}
	loginService, err := NewBrowserLoginService(identityService, mfaService, selectionService)
	if err != nil {
		t.Fatalf("create tenant administration BrowserLoginService: %v", err)
	}
	authContextService, err := NewAuthContextService(repository)
	if err != nil {
		t.Fatalf("create tenant administration AuthContextService: %v", err)
	}
	platformContext := ContextTypePlatform
	platformAudit := AuditMetadata{ContextType: &platformContext, RequestID: stringPointer("tenant-administration-closure")}
	systemAdministrator := createGovernedManagementUser(
		t, ctx, identityService, "tenant-system-administrator", "platform.system_administrator", platformAudit, db,
	)
	principalType := PrincipalTypeUser
	platformAudit.PrincipalID = &systemAdministrator.PrincipalID
	platformAudit.PrincipalType = &principalType

	initialAdministrator := createTenantAdministrationUser(t, ctx, userService, "tenant-initial-administrator", platformAudit)
	infrastructureAdministrator := createTenantAdministrationUser(t, ctx, userService, "tenant-infrastructure-administrator", platformAudit)
	legacyAdministrator := createTenantAdministrationUser(t, ctx, userService, "tenant-legacy-administrator", platformAudit)
	rollbackAdministrator := createTenantAdministrationUser(t, ctx, userService, "tenant-rollback-administrator", platformAudit)

	if _, err := tenantService.Create(ctx, CreateTenantInput{
		Code: "invalid-platform-admin", Name: "Invalid Platform Admin",
		InitialAdministratorPrincipalID: systemAdministrator.PrincipalID,
		ActorPrincipalID:                systemAdministrator.PrincipalID,
		Audit:                           platformAudit,
	}); err == nil {
		t.Fatal("platform administrator became initial tenant administrator")
	}
	assertTenantCodeCount(t, db, "invalid-platform-admin", 0)

	tenant, err := tenantService.Create(ctx, CreateTenantInput{
		Code: "administration-closure", Name: "Administration Closure",
		InitialAdministratorPrincipalID: initialAdministrator.ID,
		ActorPrincipalID:                systemAdministrator.PrincipalID,
		Audit:                           platformAudit,
	})
	if err != nil {
		t.Fatalf("create initialized tenant: %v", err)
	}
	if !tenant.Initialized || tenant.InitializedAt == nil || tenant.InitializedByPrincipalID == nil ||
		*tenant.InitializedByPrincipalID != systemAdministrator.PrincipalID {
		t.Fatalf("initialized tenant = %#v", tenant)
	}
	assertTenantInitializationFacts(t, db, tenant.ID, initialAdministrator.ID)
	currentTime = tenant.InitializedAt.Add(time.Microsecond)
	effectiveMemberships, err := repository.ListEffectiveTenantMemberships(ctx, initialAdministrator.ID, currentTime)
	if err != nil {
		t.Fatalf("list initial tenant administrator effective memberships: %v", err)
	}
	if len(effectiveMemberships) != 1 {
		t.Fatalf("initial tenant administrator effective memberships = %#v", effectiveMemberships)
	}
	login, err := loginService.LoginLocalBrowser(ctx, LoginLocalBrowserInput{
		Username: "tenant-initial-administrator", Password: "tenant-administration-password", Audit: platformAudit,
	})
	if err != nil {
		t.Fatalf("login initial tenant administrator: %v", err)
	}
	if login.Session == nil || login.NextAction != ContextSelectionNextActionSessionIssued {
		t.Fatalf("initial tenant administrator login = %#v", login)
	}
	initialAuthContext, err := authContextService.ResolveFirstPartyAccessToken(ctx, login.Session.AccessToken)
	if err != nil {
		t.Fatalf("resolve initial tenant administrator AuthContext: %v", err)
	}
	if len(initialAuthContext.Authorization.RoleAssignments) != 1 ||
		initialAuthContext.Authorization.RoleAssignments[0].RoleKey != tenantAdministratorRoleKey ||
		len(initialAuthContext.Authorization.RoleAssignments[0].Permissions) != 15 {
		t.Fatalf("initial tenant administrator AuthContext = %#v", initialAuthContext)
	}

	tenantContext := ContextTypeTenant
	tenantAudit := AuditMetadata{
		PrincipalID: &initialAdministrator.ID, PrincipalType: &principalType,
		ContextType: &tenantContext, TenantID: &tenant.ID, RequestID: stringPointer("tenant-administration-tenant"),
	}
	membership, err := membershipService.EstablishMembership(ctx, EstablishTenantMembershipInput{
		TenantID: tenant.ID, PrincipalID: infrastructureAdministrator.ID,
		SourceType: TenantMembershipSourceManual, CreatedByPrincipalID: &initialAdministrator.ID, Audit: tenantAudit,
	})
	if err != nil {
		t.Fatalf("establish infrastructure administrator membership: %v", err)
	}

	roles, err := roleService.ListRoles(ctx, tenant.ID)
	if err != nil {
		t.Fatalf("list tenant roles: %v", err)
	}
	if _, err := roleService.CreateRole(ctx, CreateTenantRoleInput{
		TenantID: tenant.ID, RoleKey: tenantAdministratorRoleKey, Name: "Conflicting Administrator",
		ScopeTypes: []string{"tenant"}, PermissionKeys: []string{"agent.run.create"},
		ActorPrincipalID: initialAdministrator.ID, Audit: tenantAudit,
	}); !errors.Is(err, commonapi.ErrConflict) {
		t.Fatalf("create custom role with built-in role key error = %v, want conflict", err)
	}
	infrastructureRole := findTenantRoleByKey(t, roles, "tenant.infrastructure_administrator")
	assigned, err := roleService.CreateAssignment(ctx, CreateTenantRoleAssignmentInput{
		TenantID: tenant.ID, MembershipID: membership.Membership.ID, RoleID: infrastructureRole.ID,
		ScopeType: "tenant", Reason: "engine administration", ActorPrincipalID: initialAdministrator.ID, Audit: tenantAudit,
	})
	if err != nil {
		t.Fatalf("assign infrastructure administrator: %v", err)
	}
	if assigned.MembershipID != membership.Membership.ID || assigned.RoleKey != "tenant.infrastructure_administrator" ||
		assigned.DisplayName != infrastructureAdministrator.DisplayName {
		t.Fatalf("created assignment projection = %#v", assigned)
	}
	if _, err := roleService.CreateAssignment(ctx, CreateTenantRoleAssignmentInput{
		TenantID: tenant.ID, MembershipID: membership.Membership.ID, RoleID: infrastructureRole.ID,
		ScopeType: "tenant", Reason: "duplicate assignment", ActorPrincipalID: initialAdministrator.ID, Audit: tenantAudit,
	}); !errors.Is(err, ErrTenantRoleAssignmentAlreadyExists) || !errors.Is(err, commonapi.ErrConflict) {
		t.Fatalf("duplicate assignment error = %v, want role assignment already exists", err)
	}
	membershipID := membership.Membership.ID
	activeStatus := "active"
	tenantScope := "tenant"
	filteredAssignments, filteredTotal, err := roleService.ListAssignments(ctx, tenant.ID, TenantRoleAssignmentFilter{
		MembershipID: &membershipID,
		Status:       &activeStatus,
		ScopeType:    &tenantScope,
	}, 1, 100)
	if err != nil || filteredTotal != 1 || len(filteredAssignments) != 1 || filteredAssignments[0].ID != assigned.ID {
		t.Fatalf("filtered active assignments = %#v total=%d err=%v", filteredAssignments, filteredTotal, err)
	}
	revoked, err := roleService.RevokeAssignment(ctx, RevokeTenantRoleAssignmentInput{
		TenantID: tenant.ID, AssignmentID: assigned.ID, Reason: "assignment test completed",
		ActorPrincipalID: initialAdministrator.ID, Audit: tenantAudit,
	})
	if err != nil || revoked.Status != "revoked" || revoked.RoleKey != "tenant.infrastructure_administrator" {
		t.Fatalf("revoked assignment = %#v err=%v", revoked, err)
	}

	assignments, _, err := roleService.ListAssignments(ctx, tenant.ID, TenantRoleAssignmentFilter{}, 1, 100)
	if err != nil {
		t.Fatalf("list tenant role assignments: %v", err)
	}
	initialAssignment := findActiveTenantAssignment(t, assignments, initialAdministrator.ID, tenantAdministratorRoleKey)
	if _, err := roleService.RevokeAssignment(ctx, RevokeTenantRoleAssignmentInput{
		TenantID: tenant.ID, AssignmentID: initialAssignment.ID, Reason: "must fail as last administrator",
		ActorPrincipalID: initialAdministrator.ID, Audit: tenantAudit,
	}); err == nil {
		t.Fatal("last tenant administrator assignment was revoked")
	}
	if _, err := membershipService.SuspendMembership(ctx, ChangeTenantMembershipInput{
		TenantID: tenant.ID, PrincipalID: initialAdministrator.ID,
		Reason: "must fail as last administrator", Audit: tenantAudit,
	}); err == nil {
		t.Fatal("last tenant administrator membership was suspended")
	}
	var initialMembership TenantMembership
	if err := db.Where("tenant_id = ? AND principal_id = ?", tenant.ID, initialAdministrator.ID).Take(&initialMembership).Error; err != nil {
		t.Fatalf("load initial administrator membership: %v", err)
	}
	expiresAt := currentTime.Add(24 * time.Hour)
	if _, err := membershipService.UpdateManagedMembership(ctx, UpdateTenantMembershipInput{
		TenantID: tenant.ID, MembershipID: initialMembership.ID, ExpiresAt: &expiresAt, Audit: tenantAudit,
	}); err == nil {
		t.Fatal("last tenant administrator membership received an expiry")
	}
	if _, err := userService.Suspend(ctx, ChangeManagedUserStatusInput{
		UserID: initialAdministrator.ID, Reason: "must fail as last administrator", Audit: platformAudit,
	}); err == nil {
		t.Fatal("last tenant administrator principal was suspended")
	}

	administratorRole := findTenantRoleByKey(t, roles, tenantAdministratorRoleKey)
	secondAdministrator, err := roleService.CreateAssignment(ctx, CreateTenantRoleAssignmentInput{
		TenantID: tenant.ID, MembershipID: membership.Membership.ID, RoleID: administratorRole.ID,
		ScopeType: "tenant", Reason: "administrator rotation", ActorPrincipalID: initialAdministrator.ID, Audit: tenantAudit,
	})
	if err != nil {
		t.Fatalf("assign replacement tenant administrator: %v", err)
	}
	if _, err := roleService.RevokeAssignment(ctx, RevokeTenantRoleAssignmentInput{
		TenantID: tenant.ID, AssignmentID: initialAssignment.ID, Reason: "administrator rotation completed",
		ActorPrincipalID: initialAdministrator.ID, Audit: tenantAudit,
	}); err != nil {
		t.Fatalf("revoke replaced tenant administrator: %v", err)
	}
	if secondAdministrator.RoleKey != tenantAdministratorRoleKey {
		t.Fatalf("replacement administrator assignment = %#v", secondAdministrator)
	}

	legacyTenant := &Tenant{Code: "legacy-default", Name: "Legacy Default", Status: TenantStatusActive}
	if err := repository.Transaction(ctx, func(tx *Repository) error { return tx.CreateTenant(ctx, legacyTenant) }); err != nil {
		t.Fatalf("create legacy tenant fixture: %v", err)
	}
	initializedLegacy, err := tenantService.Initialize(ctx, InitializeTenantInput{
		TenantID: legacyTenant.ID, InitialAdministratorPrincipalID: legacyAdministrator.ID,
		ActorPrincipalID: systemAdministrator.PrincipalID, Audit: platformAudit,
	})
	if err != nil || !initializedLegacy.Initialized {
		t.Fatalf("initialize legacy tenant = %#v err=%v", initializedLegacy, err)
	}
	if _, err := tenantService.Initialize(ctx, InitializeTenantInput{
		TenantID: legacyTenant.ID, InitialAdministratorPrincipalID: rollbackAdministrator.ID,
		ActorPrincipalID: systemAdministrator.PrincipalID, Audit: platformAudit,
	}); err == nil {
		t.Fatal("legacy tenant was initialized twice")
	}
	var legacyMembership TenantMembership
	if err := db.Where("tenant_id = ? AND principal_id = ?", legacyTenant.ID, legacyAdministrator.ID).Take(&legacyMembership).Error; err != nil {
		t.Fatalf("load legacy tenant membership: %v", err)
	}
	if _, err := roleService.CreateAssignment(ctx, CreateTenantRoleAssignmentInput{
		TenantID: tenant.ID, MembershipID: legacyMembership.ID, RoleID: infrastructureRole.ID,
		ScopeType: "tenant", ActorPrincipalID: infrastructureAdministrator.ID, Audit: tenantAudit,
	}); err == nil {
		t.Fatal("cross-tenant membership received role assignment")
	}

	if err := db.Exec(`
		CREATE FUNCTION system.reject_test_audit() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN RAISE EXCEPTION 'test audit failure'; END;
		$$;
		CREATE TRIGGER trg_reject_test_audit BEFORE INSERT ON system.audit_logs
		FOR EACH ROW EXECUTE FUNCTION system.reject_test_audit()
	`).Error; err != nil {
		t.Fatalf("install audit failure trigger: %v", err)
	}
	_, createErr := tenantService.Create(ctx, CreateTenantInput{
		Code: "audit-rollback", Name: "Audit Rollback",
		InitialAdministratorPrincipalID: rollbackAdministrator.ID,
		ActorPrincipalID:                systemAdministrator.PrincipalID, Audit: platformAudit,
	})
	if dropErr := db.Exec(`DROP TRIGGER trg_reject_test_audit ON system.audit_logs; DROP FUNCTION system.reject_test_audit()`).Error; dropErr != nil {
		t.Fatalf("remove audit failure trigger: %v", dropErr)
	}
	if createErr == nil {
		t.Fatal("tenant creation succeeded while audit write failed")
	}
	assertTenantCodeCount(t, db, "audit-rollback", 0)
}

func createTenantAdministrationUser(
	t *testing.T,
	ctx context.Context,
	service *PlatformUserService,
	username string,
	audit AuditMetadata,
) *ManagedUser {
	t.Helper()
	user, err := service.Create(ctx, CreateManagedLocalUserInput{
		Username: username, Password: "tenant-administration-password", DisplayName: username, Audit: audit,
	})
	if err != nil {
		t.Fatalf("create tenant administration user %s: %v", username, err)
	}
	return user
}

func findTenantRoleByKey(t *testing.T, roles []TenantRole, roleKey string) TenantRole {
	t.Helper()
	for _, role := range roles {
		if role.RoleKey == roleKey {
			return role
		}
	}
	t.Fatalf("tenant role %s not found", roleKey)
	return TenantRole{}
}

func findActiveTenantAssignment(
	t *testing.T,
	assignments []ManagedTenantRoleAssignment,
	principalID int64,
	roleKey string,
) ManagedTenantRoleAssignment {
	t.Helper()
	for _, assignment := range assignments {
		if assignment.PrincipalID == principalID && assignment.RoleKey == roleKey && assignment.Status == "active" {
			return assignment
		}
	}
	t.Fatalf("active assignment principal=%d role=%s not found", principalID, roleKey)
	return ManagedTenantRoleAssignment{}
}

func assertTenantCodeCount(t *testing.T, db *gorm.DB, code string, want int64) {
	t.Helper()
	var count int64
	if err := db.Model(&Tenant{}).Where("code = ?", code).Count(&count).Error; err != nil {
		t.Fatalf("count tenant code %s: %v", code, err)
	}
	if count != want {
		t.Fatalf("tenant code %s count = %d, want %d", code, count, want)
	}
}

func assertTenantInitializationFacts(t *testing.T, db *gorm.DB, tenantID, administratorID int64) {
	t.Helper()
	var membershipCount, assignmentCount, serviceMembershipCount, serviceAssignmentCount, auditCount int64
	if err := db.Model(&TenantMembership{}).Where("tenant_id = ? AND principal_id = ? AND status = 'active'", tenantID, administratorID).Count(&membershipCount).Error; err != nil {
		t.Fatalf("count initial membership: %v", err)
	}
	if err := db.Table("system.role_assignments assignment").Joins("JOIN system.roles role ON role.id = assignment.role_id").
		Where("assignment.tenant_id = ? AND assignment.principal_id = ? AND assignment.status = 'active' AND role.role_key = ?", tenantID, administratorID, tenantAdministratorRoleKey).
		Count(&assignmentCount).Error; err != nil {
		t.Fatalf("count initial administrator assignment: %v", err)
	}
	if err := db.Model(&AuditLog{}).Where("request_id = ? AND event_name IN ?", "tenant-administration-closure", []string{
		"iam.tenant.created", "iam.tenant_membership.established", "iam.tenant_role_assignment.created",
	}).Count(&auditCount).Error; err != nil {
		t.Fatalf("count tenant initialization audit: %v", err)
	}
	if err := db.Table("system.tenant_memberships membership").
		Joins("JOIN system.principals principal ON principal.id = membership.principal_id").
		Where("membership.tenant_id = ? AND membership.status = 'active' AND membership.source_type = 'bootstrap' AND membership.created_by_principal_id IS NOT NULL AND principal.principal_type = 'service_principal'", tenantID).
		Count(&serviceMembershipCount).Error; err != nil {
		t.Fatalf("count service runtime memberships: %v", err)
	}
	if err := db.Table("system.role_assignments assignment").
		Joins("JOIN system.roles role ON role.id = assignment.role_id").
		Joins("JOIN system.principals principal ON principal.id = assignment.principal_id").
		Where("assignment.tenant_id = ? AND assignment.status = 'active' AND assignment.source_type = 'bootstrap' AND assignment.created_by_principal_id IS NULL AND principal.principal_type = 'service_principal' AND role.role_key LIKE 'tenant.%_runtime'", tenantID).
		Count(&serviceAssignmentCount).Error; err != nil {
		t.Fatalf("count service runtime assignments: %v", err)
	}
	var serviceRuntimeCount int
	if err := db.Raw(`SELECT (details->>'service_runtime_count')::int FROM system.audit_logs WHERE request_id = ? AND event_name = 'iam.tenant.created'`, "tenant-administration-closure").Scan(&serviceRuntimeCount).Error; err != nil {
		t.Fatalf("read tenant creation service runtime audit: %v", err)
	}
	if membershipCount != 1 || assignmentCount != 1 || serviceMembershipCount != 10 || serviceAssignmentCount != 10 || auditCount != 3 || serviceRuntimeCount != 10 {
		t.Fatalf("initialization facts membership=%d assignment=%d service_membership=%d service_assignment=%d audit=%d service_runtime_count=%d", membershipCount, assignmentCount, serviceMembershipCount, serviceAssignmentCount, auditCount, serviceRuntimeCount)
	}
}
