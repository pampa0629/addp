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

func TestOrganizationServiceAgainstPostgres(t *testing.T) {
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
		t.Fatalf("reset organization test schema: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := migration.NewRunner(dsn).Run(ctx); err != nil {
		t.Fatalf("apply IAM migrations: %v", err)
	}

	repository := NewRepository(db)
	now := time.Now().UTC().Truncate(time.Microsecond)
	identityService := NewIdentityService(repository, func() time.Time { return now })
	membershipService := NewTenantMembershipService(repository, func() time.Time { return now })
	organizationService := NewOrganizationService(repository, func() time.Time { return now })
	bootstrapAudit := AuditMetadata{RequestID: stringPointer("organization-bootstrap")}
	user := createContextSelectionUser(t, ctx, identityService, "organization-user", bootstrapAudit)
	tenant := createContextSelectionTenant(t, ctx, membershipService, "organization", bootstrapAudit)
	contextType := ContextTypeTenant
	principalType := PrincipalTypeUser
	tenantAudit := AuditMetadata{
		PrincipalID: &user.PrincipalID, PrincipalType: &principalType,
		ContextType: &contextType, TenantID: &tenant.ID,
		RequestID: stringPointer("organization-management"),
	}
	tenantMembership := establishContextSelectionMembership(t, ctx, membershipService, tenant.ID, user.PrincipalID, tenantAudit)

	root, err := organizationService.CreateDepartment(ctx, CreateDepartmentInput{
		TenantID: tenant.ID, ActorPrincipalID: user.PrincipalID,
		Code: "root", Name: "Root", Audit: tenantAudit,
	})
	if err != nil {
		t.Fatalf("create root department: %v", err)
	}
	child, err := organizationService.CreateDepartment(ctx, CreateDepartmentInput{
		TenantID: tenant.ID, ActorPrincipalID: user.PrincipalID, ParentID: &root.ID,
		Code: "child", Name: "Child", Audit: tenantAudit,
	})
	if err != nil {
		t.Fatalf("create child department: %v", err)
	}
	if _, err := organizationService.UpdateDepartment(ctx, UpdateDepartmentInput{
		TenantID: tenant.ID, DepartmentID: root.ID, Version: root.Version,
		ActorPrincipalID: user.PrincipalID, ParentID: &child.ID, Name: root.Name, Audit: tenantAudit,
	}); !errors.Is(err, commonapi.ErrConflict) {
		t.Fatalf("department cycle error = %v, want conflict", err)
	}
	if _, err := organizationService.DisableDepartment(ctx, ChangeDepartmentStatusInput{
		TenantID: tenant.ID, DepartmentID: root.ID, Version: root.Version,
		ActorPrincipalID: user.PrincipalID, Reason: "reorganization", Audit: tenantAudit,
	}); !errors.Is(err, commonapi.ErrConflict) {
		t.Fatalf("disable department with active child error = %v, want conflict", err)
	}
	if _, err := organizationService.UpdateDepartment(ctx, UpdateDepartmentInput{
		TenantID: tenant.ID, DepartmentID: child.ID, Version: child.Version + 1,
		ActorPrincipalID: user.PrincipalID, ParentID: &root.ID, Name: "Stale Child", Audit: tenantAudit,
	}); !errors.Is(err, ErrOrganizationVersionConflict) {
		t.Fatalf("stale department update error = %v, want version conflict", err)
	}

	departmentMembership, err := organizationService.CreateDepartmentMembership(ctx, CreateDepartmentMembershipInput{
		TenantID: tenant.ID, DepartmentID: child.ID,
		TenantMembershipID: tenantMembership.Membership.ID, ActorPrincipalID: user.PrincipalID,
		MembershipType: DepartmentMembershipTypePrimary, RelationRole: DepartmentRelationRoleLeader,
		Audit: tenantAudit,
	})
	if err != nil {
		t.Fatalf("create department membership: %v", err)
	}
	closedDepartmentMembership, err := organizationService.CloseDepartmentMembership(ctx, CloseOrganizationMembershipInput{
		TenantID: tenant.ID, OrganizationID: child.ID, MembershipID: departmentMembership.ID,
		Version: departmentMembership.Version, ActorPrincipalID: user.PrincipalID,
		Reason: "team change", Audit: tenantAudit,
	})
	if err != nil || closedDepartmentMembership.Status != OrganizationMembershipStatusEnded || closedDepartmentMembership.EndedAt == nil {
		t.Fatalf("close department membership = %#v error=%v", closedDepartmentMembership, err)
	}
	recreatedDepartmentMembership, err := organizationService.CreateDepartmentMembership(ctx, CreateDepartmentMembershipInput{
		TenantID: tenant.ID, DepartmentID: child.ID,
		TenantMembershipID: tenantMembership.Membership.ID, ActorPrincipalID: user.PrincipalID,
		MembershipType: DepartmentMembershipTypePrimary, RelationRole: DepartmentRelationRoleMember,
		Audit: tenantAudit,
	})
	if err != nil || recreatedDepartmentMembership.ID == departmentMembership.ID {
		t.Fatalf("recreate ended department membership = %#v error=%v", recreatedDepartmentMembership, err)
	}

	startsAt := now.Add(24 * time.Hour)
	endsAt := startsAt.Add(7 * 24 * time.Hour)
	projectGroup, err := organizationService.CreateProjectGroup(ctx, CreateProjectGroupInput{
		TenantID: tenant.ID, ActorPrincipalID: user.PrincipalID,
		Code: "future_project", Name: "Future Project", Status: ProjectGroupStatusPlanned,
		StartsAt: &startsAt, EndsAt: &endsAt, Audit: tenantAudit,
	})
	if err != nil {
		t.Fatalf("create future project group: %v", err)
	}
	projectMembership, err := organizationService.CreateProjectGroupMembership(ctx, CreateProjectGroupMembershipInput{
		TenantID: tenant.ID, ProjectGroupID: projectGroup.ID,
		TenantMembershipID: tenantMembership.Membership.ID, ActorPrincipalID: user.PrincipalID,
		RelationRole: ProjectGroupRelationRoleCoordinator, Audit: tenantAudit,
	})
	if err != nil {
		t.Fatalf("create project group membership: %v", err)
	}
	if _, err := organizationService.CloseProjectGroupMembership(ctx, CloseOrganizationMembershipInput{
		TenantID: tenant.ID, OrganizationID: projectGroup.ID, MembershipID: projectMembership.ID,
		Version: projectMembership.Version, ActorPrincipalID: user.PrincipalID,
		Reason: "rotation", Audit: tenantAudit,
	}); err != nil {
		t.Fatalf("close project group membership: %v", err)
	}
	if _, err := organizationService.CreateProjectGroupMembership(ctx, CreateProjectGroupMembershipInput{
		TenantID: tenant.ID, ProjectGroupID: projectGroup.ID,
		TenantMembershipID: tenantMembership.Membership.ID, ActorPrincipalID: user.PrincipalID,
		RelationRole: ProjectGroupRelationRoleMember, Audit: tenantAudit,
	}); err != nil {
		t.Fatalf("recreate ended project group membership: %v", err)
	}
	closedProjectGroup, err := organizationService.CloseProjectGroup(ctx, CloseProjectGroupInput{
		TenantID: tenant.ID, ProjectGroupID: projectGroup.ID, Version: projectGroup.Version,
		ActorPrincipalID: user.PrincipalID, Reason: "cancelled", Audit: tenantAudit,
	})
	if err != nil {
		t.Fatalf("close future project group: %v", err)
	}
	if closedProjectGroup.Status != ProjectGroupStatusClosed || closedProjectGroup.EndsAt == nil || !closedProjectGroup.EndsAt.Equal(endsAt) {
		t.Fatalf("closed project group = %#v, want planned ends_at preserved", closedProjectGroup)
	}

	var auditCount int64
	if err := db.Table("system.audit_logs").
		Where("event_name IN ?", []string{"iam.department.created", "iam.department_membership.closed", "iam.project_group.closed"}).
		Count(&auditCount).Error; err != nil || auditCount != 4 {
		t.Fatalf("organization audit count = %d error=%v, want 4", auditCount, err)
	}
}
