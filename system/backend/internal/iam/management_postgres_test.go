package iam

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/addp/system/internal/migration"
	"github.com/addp/system/internal/testsupport"
	passwordutils "github.com/addp/system/pkg/utils"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestIAMManagementServicesAgainstPostgres(t *testing.T) {
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
		t.Fatalf("reset IAM management test schema: %v", err)
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
	auditService := NewAuditQueryService(repository)
	privilegedChangeService := NewPrivilegedIdentityChangeService(repository, now)
	platformContext := ContextTypePlatform
	platformAudit := AuditMetadata{ContextType: &platformContext, RequestID: stringPointer("iam-management-platform")}

	createdUser, err := userService.Create(ctx, CreateManagedLocalUserInput{
		Username: "managed-user", Password: "managed-password", DisplayName: "Managed User", Audit: platformAudit,
	})
	if err != nil {
		t.Fatalf("create managed user: %v", err)
	}
	if createdUser.Status != PrincipalStatusActive || createdUser.Username == nil || *createdUser.Username != "managed-user" {
		t.Fatalf("created managed user = %#v", createdUser)
	}
	users, total, err := userService.List(ctx, 1, 20, "Managed", nil)
	if err != nil || total != 1 || len(users) != 1 {
		t.Fatalf("managed user list = %#v total=%d err=%v", users, total, err)
	}
	email := "managed@example.com"
	updatedUser, err := userService.Update(ctx, UpdateManagedUserInput{
		UserID: createdUser.ID, DisplayName: "Managed User Updated", PrimaryEmail: &email, Audit: platformAudit,
	})
	if err != nil || updatedUser.PrimaryEmail == nil || *updatedUser.PrimaryEmail != email {
		t.Fatalf("updated managed user = %#v err=%v", updatedUser, err)
	}

	currentTime = currentTime.Add(time.Second)
	passwordReset, err := userService.ResetLocalAccountPassword(ctx, ResetManagedLocalAccountPasswordInput{
		UserID: createdUser.ID, NewPassword: "managed-password-reset", Reason: "user lost password",
		Audit: platformAudit,
	})
	if err != nil || passwordReset.RevokedFamilyCount != 0 ||
		passwordReset.AuthorizationVersion <= createdUser.AuthorizationVersion {
		t.Fatalf("managed local account password reset = %#v err=%v", passwordReset, err)
	}
	loginAudit := AuditMetadata{RequestID: stringPointer("managed-user-password-login")}
	if _, err := identityService.AuthenticateLocalAccount(ctx, "managed-user", "managed-password", loginAudit); err == nil {
		t.Fatal("old password authenticated after managed reset")
	}
	if _, err := identityService.AuthenticateLocalAccount(ctx, "managed-user", "managed-password-reset", loginAudit); err != nil {
		t.Fatalf("new password did not authenticate after managed reset: %v", err)
	}
	assertIAMServiceAuditCount(t, db, "iam.local_account.password_reset", AuditResultSucceeded, 1)

	rollbackUser, err := userService.Create(ctx, CreateManagedLocalUserInput{
		Username: "password-rollback", Password: "password-before-rollback",
		DisplayName: "Password Rollback", Audit: platformAudit,
	})
	if err != nil {
		t.Fatalf("create password rollback user: %v", err)
	}
	invalidAudit := platformAudit
	invalidTenantID := int64(999999)
	invalidAudit.TenantID = &invalidTenantID
	if _, err := userService.ResetLocalAccountPassword(ctx, ResetManagedLocalAccountPasswordInput{
		UserID: rollbackUser.ID, NewPassword: "password-after-rollback", Reason: "force audit failure",
		Audit: invalidAudit,
	}); err == nil {
		t.Fatal("password reset with invalid audit metadata unexpectedly succeeded")
	}
	rollbackAccount, err := repository.GetLocalAccountByUserID(ctx, rollbackUser.ID)
	if err != nil {
		t.Fatalf("read rollback local account: %v", err)
	}
	if !passwordutils.CheckPassword("password-before-rollback", rollbackAccount.PasswordHash) ||
		passwordutils.CheckPassword("password-after-rollback", rollbackAccount.PasswordHash) {
		t.Fatal("password reset was not rolled back with rejected audit")
	}

	currentTime = currentTime.Add(time.Second)
	suspendedUser, err := userService.Suspend(ctx, ChangeManagedUserStatusInput{
		UserID: createdUser.ID, Reason: "security review", Audit: platformAudit,
	})
	if err != nil || suspendedUser.User.Status != PrincipalStatusSuspended {
		t.Fatalf("suspended managed user = %#v err=%v", suspendedUser, err)
	}
	currentTime = currentTime.Add(time.Second)
	reactivatedUser, err := userService.Reactivate(ctx, ChangeManagedUserStatusInput{
		UserID: createdUser.ID, Reason: "review completed", Audit: platformAudit,
	})
	if err != nil || reactivatedUser.User.Status != PrincipalStatusActive {
		t.Fatalf("reactivated managed user = %#v err=%v", reactivatedUser, err)
	}

	target := createGovernedManagementUser(t, ctx, identityService, "audit-target", "platform.audit_administrator", platformAudit, db)
	requester := createGovernedManagementUser(t, ctx, identityService, "security-requester", "platform.security_administrator", platformAudit, db)
	reviewer := createGovernedManagementUser(t, ctx, identityService, "system-reviewer", "platform.system_administrator", platformAudit, db)
	requesterAudit := platformAudit
	requesterType := PrincipalTypeUser
	requesterAudit.PrincipalID = &requester.PrincipalID
	requesterAudit.PrincipalType = &requesterType
	reviewerAudit := platformAudit
	reviewerAudit.PrincipalID = &reviewer.PrincipalID
	reviewerAudit.PrincipalType = &requesterType
	governedTarget, err := userService.Get(ctx, target.PrincipalID)
	if err != nil || !governedTarget.HasEffectivePlatformRole {
		t.Fatalf("governed target projection = %#v err=%v", governedTarget, err)
	}
	if _, err := userService.ResetLocalAccountPassword(ctx, ResetManagedLocalAccountPasswordInput{
		UserID: target.PrincipalID, NewPassword: "forbidden-platform-reset", Reason: "must be governed",
		Audit: requesterAudit,
	}); err == nil {
		t.Fatal("platform role holder password reset unexpectedly succeeded")
	}
	targetAccount, err := repository.GetLocalAccountByUserID(ctx, target.PrincipalID)
	if err != nil || !passwordutils.CheckPassword("governed-password", targetAccount.PasswordHash) {
		t.Fatalf("platform role holder password changed: account=%#v err=%v", targetAccount, err)
	}
	membershipUser, err := userService.Create(ctx, CreateManagedLocalUserInput{
		Username: "membership-user", Password: "membership-password", DisplayName: "Membership User", Audit: platformAudit,
	})
	if err != nil {
		t.Fatalf("create membership lifecycle user: %v", err)
	}

	currentTime = currentTime.Add(time.Second)
	suspendRequest, err := privilegedChangeService.Create(ctx, CreatePrivilegedIdentityChangeInput{
		ChangeType: PrivilegedChangePlatformIdentitySuspend, TargetPrincipalID: target.PrincipalID,
		Reason: "governed suspension", RequestedByPrincipalID: requester.PrincipalID, Audit: requesterAudit,
	})
	if err != nil {
		t.Fatalf("create governed suspension request: %v", err)
	}
	currentTime = currentTime.Add(time.Second)
	suspendRequest, err = privilegedChangeService.Approve(ctx, ReviewPrivilegedIdentityChangeInput{
		RequestID: suspendRequest.ID, ReviewerPrincipalID: reviewer.PrincipalID,
		Reason: "approved by separate administrator", Audit: reviewerAudit,
	})
	if err != nil || suspendRequest.Status != PrivilegedChangeStatusApproved {
		t.Fatalf("approved suspension request = %#v err=%v", suspendRequest, err)
	}
	currentTime = currentTime.Add(time.Second)
	governedSuspension, err := userService.Suspend(ctx, ChangeManagedUserStatusInput{
		UserID: target.PrincipalID, Reason: "apply approved suspension",
		ChangeRequestID: &suspendRequest.ID, Audit: reviewerAudit,
	})
	if err != nil || governedSuspension.User.Status != PrincipalStatusSuspended {
		t.Fatalf("governed suspension = %#v err=%v", governedSuspension, err)
	}
	appliedSuspend, err := privilegedChangeService.Get(ctx, suspendRequest.ID)
	if err != nil || appliedSuspend.Status != PrivilegedChangeStatusApplied {
		t.Fatalf("applied suspension request = %#v err=%v", appliedSuspend, err)
	}

	currentTime = currentTime.Add(time.Second)
	reactivateRequest, err := privilegedChangeService.Create(ctx, CreatePrivilegedIdentityChangeInput{
		ChangeType: PrivilegedChangePlatformIdentityReactivate, TargetPrincipalID: target.PrincipalID,
		Reason: "governed reactivation", RequestedByPrincipalID: requester.PrincipalID, Audit: requesterAudit,
	})
	if err != nil {
		t.Fatalf("create governed reactivation request: %v", err)
	}
	currentTime = currentTime.Add(time.Second)
	reactivateRequest, err = privilegedChangeService.Approve(ctx, ReviewPrivilegedIdentityChangeInput{
		RequestID: reactivateRequest.ID, ReviewerPrincipalID: reviewer.PrincipalID,
		Reason: "approved reactivation", Audit: reviewerAudit,
	})
	if err != nil || reactivateRequest.Status != PrivilegedChangeStatusApproved {
		t.Fatalf("approved reactivation request = %#v err=%v", reactivateRequest, err)
	}
	currentTime = currentTime.Add(time.Second)
	governedReactivation, err := userService.Reactivate(ctx, ChangeManagedUserStatusInput{
		UserID: target.PrincipalID, Reason: "apply approved reactivation",
		ChangeRequestID: &reactivateRequest.ID, Audit: reviewerAudit,
	})
	if err != nil || governedReactivation.User.Status != PrincipalStatusActive {
		t.Fatalf("governed reactivation = %#v err=%v", governedReactivation, err)
	}
	appliedReactivate, err := privilegedChangeService.Get(ctx, reactivateRequest.ID)
	if err != nil || appliedReactivate.Status != PrivilegedChangeStatusApplied {
		t.Fatalf("applied reactivation request = %#v err=%v", appliedReactivate, err)
	}

	tenant, err := tenantService.Create(ctx, CreateTenantInput{
		Code: "management-test", Name: "Management Test", Description: "IAM management",
		InitialAdministratorPrincipalID: createdUser.ID, ActorPrincipalID: reviewer.PrincipalID, Audit: platformAudit,
	})
	if err != nil {
		t.Fatalf("create managed tenant: %v", err)
	}
	currentTime = currentTime.Add(time.Second)
	tenantContext := ContextTypeTenant
	tenantAudit := AuditMetadata{
		ContextType: &tenantContext, TenantID: &tenant.ID, RequestID: stringPointer("iam-management-tenant"),
	}
	membership, err := membershipService.EstablishMembership(ctx, EstablishTenantMembershipInput{
		TenantID: tenant.ID, PrincipalID: membershipUser.ID, SourceType: TenantMembershipSourceManual,
		CreatedByPrincipalID: &createdUser.ID, Audit: tenantAudit,
	})
	if err != nil {
		t.Fatalf("establish managed membership: %v", err)
	}
	memberships, membershipTotal, err := membershipService.ListManagedMemberships(
		ctx, tenant.ID, 1, 20, "Membership", nil,
	)
	if err != nil || membershipTotal != 1 || len(memberships) != 1 || memberships[0].ID != membership.Membership.ID {
		t.Fatalf("managed membership list = %#v total=%d err=%v", memberships, membershipTotal, err)
	}
	expiresAt := currentTime.Add(24 * time.Hour)
	if _, err := membershipService.UpdateManagedMembership(ctx, UpdateTenantMembershipInput{
		TenantID: tenant.ID, MembershipID: membership.Membership.ID, ExpiresAt: &expiresAt, Audit: tenantAudit,
	}); err != nil {
		t.Fatalf("update managed membership: %v", err)
	}
	currentTime = currentTime.Add(time.Second)
	if _, err := membershipService.SuspendMembership(ctx, ChangeTenantMembershipInput{
		TenantID: tenant.ID, PrincipalID: membershipUser.ID, Reason: "temporary leave", Audit: tenantAudit,
	}); err != nil {
		t.Fatalf("suspend managed membership: %v", err)
	}
	currentTime = currentTime.Add(time.Second)
	if _, err := membershipService.RestoreMembership(ctx, ChangeTenantMembershipInput{
		TenantID: tenant.ID, PrincipalID: membershipUser.ID, ExpiresAt: &expiresAt,
		Reason: "returned", Audit: tenantAudit,
	}); err != nil {
		t.Fatalf("restore managed membership: %v", err)
	}
	currentTime = currentTime.Add(time.Second)
	if _, err := membershipService.EndMembership(ctx, ChangeTenantMembershipInput{
		TenantID: tenant.ID, PrincipalID: membershipUser.ID, Reason: "membership completed", Audit: tenantAudit,
	}); err != nil {
		t.Fatalf("close managed membership: %v", err)
	}

	currentTime = currentTime.Add(time.Second)
	if _, err := tenantService.Suspend(ctx, ChangeTenantStatusInput{
		TenantID: tenant.ID, Reason: "maintenance", Audit: platformAudit,
	}); err != nil {
		t.Fatalf("suspend managed tenant: %v", err)
	}
	currentTime = currentTime.Add(time.Second)
	if _, err := tenantService.Restore(ctx, ChangeTenantStatusInput{
		TenantID: tenant.ID, Reason: "maintenance completed", Audit: platformAudit,
	}); err != nil {
		t.Fatalf("restore managed tenant: %v", err)
	}
	currentTime = currentTime.Add(time.Second)
	closedTenant, err := tenantService.Close(ctx, ChangeTenantStatusInput{
		TenantID: tenant.ID, Reason: "tenant retired", Audit: platformAudit,
	})
	if err != nil || closedTenant.Tenant.Status != TenantStatusClosed {
		t.Fatalf("closed managed tenant = %#v err=%v", closedTenant, err)
	}

	allLogs, allTotal, err := auditService.List(ctx, AuditQuery{}, 1, 100)
	if err != nil || allTotal == 0 || len(allLogs) == 0 {
		t.Fatalf("platform audit list total=%d logs=%d err=%v", allTotal, len(allLogs), err)
	}
	tenantLogs, tenantTotal, err := auditService.List(ctx, AuditQuery{TenantID: &tenant.ID}, 1, 100)
	if err != nil || tenantTotal < 4 || len(tenantLogs) != int(tenantTotal) {
		t.Fatalf("tenant audit list total=%d logs=%d err=%v", tenantTotal, len(tenantLogs), err)
	}
	for _, auditLog := range tenantLogs {
		if auditLog.TenantID == nil || *auditLog.TenantID != tenant.ID {
			t.Fatalf("tenant audit escaped scope: %#v", auditLog)
		}
	}
	var entityLog *AuditLog
	for index := range tenantLogs {
		if tenantLogs[index].EntityType != nil && tenantLogs[index].EntityID != nil {
			entityLog = &tenantLogs[index]
			break
		}
	}
	if entityLog == nil {
		t.Fatal("tenant audit list has no entity-bound event")
	}
	entityLogs, entityTotal, err := auditService.List(ctx, AuditQuery{
		TenantID: &tenant.ID, EntityType: *entityLog.EntityType, EntityID: *entityLog.EntityID,
	}, 1, 100)
	if err != nil || entityTotal == 0 || len(entityLogs) != int(entityTotal) {
		t.Fatalf("entity audit list total=%d logs=%d err=%v", entityTotal, len(entityLogs), err)
	}
	for _, auditLog := range entityLogs {
		if auditLog.EntityType == nil || *auditLog.EntityType != *entityLog.EntityType ||
			auditLog.EntityID == nil || *auditLog.EntityID != *entityLog.EntityID {
			t.Fatalf("entity audit filter escaped scope: %#v", auditLog)
		}
	}
	summary, err := auditService.Summary(ctx, AuditQuery{TenantID: &tenant.ID})
	if err != nil || summary.Total != tenantTotal {
		t.Fatalf("tenant audit summary = %#v err=%v", summary, err)
	}
}

func createGovernedManagementUser(
	t *testing.T,
	ctx context.Context,
	identityService *IdentityService,
	username string,
	roleKey string,
	audit AuditMetadata,
	db *gorm.DB,
) *CreatedLocalUser {
	t.Helper()
	created, err := identityService.CreateLocalUser(ctx, CreateLocalUserInput{
		Username: username, Password: "governed-password", DisplayName: username, Audit: audit,
	})
	if err != nil {
		t.Fatalf("create governed user %s: %v", username, err)
	}
	if err := db.Exec(`
		INSERT INTO system.role_assignments (
			principal_id, role_id, scope_type, status, valid_from, source_type,
			created_by_principal_id, reason
		)
		SELECT ?, role.id, 'platform', 'active', now(), 'bootstrap', NULL, 'management integration test'
		FROM system.roles role
		WHERE role.tenant_id IS NULL AND role.role_key = ?
	`, created.PrincipalID, roleKey).Error; err != nil {
		t.Fatalf("assign governed role %s: %v", roleKey, err)
	}
	return created
}
