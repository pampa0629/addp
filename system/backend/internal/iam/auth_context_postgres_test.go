package iam

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	commonauth "github.com/addp/common/authorization"
	"github.com/addp/system/internal/migration"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestAuthContextServiceAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("ADDP_IAM_RUNTIME_TEST_DSN")
	if dsn == "" {
		t.Skip("set ADDP_IAM_RUNTIME_TEST_DSN to a disposable PostgreSQL 15+ database")
	}

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`DROP SCHEMA IF EXISTS system CASCADE`).Error; err != nil {
		t.Fatalf("reset AuthContext test schema: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	if err := migration.NewRunner(dsn).Run(ctx); err != nil {
		t.Fatalf("apply IAM migrations: %v", err)
	}

	repository := NewRepository(db)
	currentTime := time.Now().UTC().Add(-time.Second).Truncate(time.Microsecond)
	now := func() time.Time { return currentTime }
	tokenService, err := NewTokenFamilyService(repository, BrowserSessionConfig{
		ResourceTicketOwners: []string{"manager", "standard"},
	}, nil, now)
	if err != nil {
		t.Fatalf("create TokenFamilyService: %v", err)
	}
	selectionService, err := NewContextSelectionService(repository, tokenService)
	if err != nil {
		t.Fatalf("create ContextSelectionService: %v", err)
	}
	authContextService, err := NewAuthContextService(repository)
	if err != nil {
		t.Fatalf("create AuthContextService: %v", err)
	}
	identityService := NewIdentityService(repository, now)
	membershipService := NewTenantMembershipService(repository, now)

	t.Run("tenant projection isolates context and filters effective facts", func(t *testing.T) {
		audit := AuditMetadata{RequestID: stringPointer("auth-context-tenant")}
		user := createContextSelectionUser(t, ctx, identityService, "auth-context-tenant", audit)
		tenantA := createContextSelectionTenant(t, ctx, membershipService, "context-a", audit)
		tenantB := createContextSelectionTenant(t, ctx, membershipService, "context-b", audit)
		membershipA := establishContextSelectionMembership(
			t, ctx, membershipService, tenantA.ID, user.PrincipalID, audit,
		)
		establishContextSelectionMembership(
			t, ctx, membershipService, tenantB.ID, user.PrincipalID, audit,
		)

		rootDepartmentID := insertDepartment(t, db, tenantA.ID, nil, "root")
		childDepartmentID := insertDepartment(t, db, tenantA.ID, &rootDepartmentID, "child")
		departmentMembershipID := insertDepartmentMembership(
			t, db, tenantA.ID, childDepartmentID, membershipA.Membership.ID,
		)
		projectGroupID := insertProjectGroup(t, db, tenantA.ID, "context-project")
		projectMembershipID := insertProjectGroupMembership(
			t, db, tenantA.ID, projectGroupID, membershipA.Membership.ID,
		)

		validFrom := currentTime.Add(-time.Hour)
		insertRoleAssignment(t, db, user.PrincipalID, "tenant.data_viewer", "tenant", &tenantA.ID, nil, nil, validFrom, nil, "manual")
		insertRoleAssignment(t, db, user.PrincipalID, "tenant.data_viewer", "department", &tenantA.ID, &childDepartmentID, nil, validFrom, nil, "manual")
		insertRoleAssignment(t, db, user.PrincipalID, "tenant.data_viewer", "project_group", &tenantA.ID, nil, &projectGroupID, validFrom, nil, "manual")
		insertRoleAssignment(t, db, user.PrincipalID, "tenant.data_viewer", "tenant", &tenantB.ID, nil, nil, validFrom, nil, "manual")
		expiredAt := currentTime.Add(-time.Minute)
		insertRoleAssignment(t, db, user.PrincipalID, "tenant.ai_user", "tenant", &tenantA.ID, nil, nil, validFrom, &expiredAt, "manual")

		authentication := SessionAuthentication{
			Methods:         []string{"password"},
			AssuranceLevel:  AssuranceLevelAAL1,
			AuthenticatedAt: currentTime.Add(-time.Minute),
		}
		selection, err := selectionService.BeginContextSelection(ctx, BeginContextSelectionInput{
			PrincipalID:    user.PrincipalID,
			Authentication: authentication,
			Audit:          audit,
		})
		if err != nil {
			t.Fatalf("begin tenant AuthContext selection: %v", err)
		}
		if selection.Challenge == nil {
			t.Fatalf("tenant AuthContext selection = %#v", selection)
		}
		session, err := selectionService.ConsumeContextSelection(ctx, ConsumeContextSelectionInput{
			SelectionTicket: selection.Challenge.SelectionTicket,
			Choice: ContextSelectionChoice{
				Type:               ContextTypeTenant,
				TenantMembershipID: &membershipA.Membership.ID,
			},
			Audit: audit,
		})
		if err != nil {
			t.Fatalf("consume tenant AuthContext selection: %v", err)
		}

		resolved, err := authContextService.ResolveFirstPartyAccessToken(ctx, session.AccessToken)
		if err != nil {
			t.Fatalf("resolve tenant AuthContext: %v", err)
		}
		if err := commonauth.ValidateAuthContext(*resolved); err != nil {
			t.Fatalf("validate projected tenant AuthContext: %v", err)
		}
		assertTenantAuthContext(
			t,
			resolved,
			user.PrincipalID,
			tenantA.ID,
			membershipA.Membership.ID,
			rootDepartmentID,
			childDepartmentID,
			departmentMembershipID,
			projectGroupID,
			projectMembershipID,
		)
		encoded, err := json.Marshal(resolved)
		if err != nil {
			t.Fatalf("marshal projected tenant AuthContext: %v", err)
		}
		if _, err := commonauth.DecodeAuthContext(bytes.NewReader(encoded)); err != nil {
			t.Fatalf("decode projected tenant AuthContext: %v", err)
		}

		rotated, err := tokenService.RotateBrowserRefreshToken(ctx, RotateBrowserRefreshTokenInput{
			RefreshToken: session.RefreshToken,
			Audit:        AuditMetadata{RequestID: stringPointer("auth-context-token-rotated")},
		})
		if err != nil {
			t.Fatalf("rotate tenant session after AuthContext projection: %v", err)
		}
		assertAccessTokenReason(
			t,
			authContextService,
			ctx,
			session.AccessToken,
			AccessTokenInvalidTokenRevoked,
		)
		if _, err := authContextService.ResolveFirstPartyAccessToken(ctx, rotated.AccessToken); err != nil {
			t.Fatalf("resolve replacement access token: %v", err)
		}
	})

	t.Run("platform projection is isolated and version mismatch is unauthorized", func(t *testing.T) {
		audit := AuditMetadata{RequestID: stringPointer("auth-context-platform")}
		user := createContextSelectionUser(t, ctx, identityService, "auth-context-platform", audit)
		assignmentID := insertRoleAssignment(
			t,
			db,
			user.PrincipalID,
			"platform.statistics_viewer",
			"platform",
			nil,
			nil,
			nil,
			currentTime.Add(-time.Hour),
			nil,
			"bootstrap",
		)
		authentication := SessionAuthentication{
			Methods:         []string{"password", "totp"},
			AssuranceLevel:  AssuranceLevelAAL2,
			AuthenticatedAt: currentTime.Add(-time.Minute),
		}
		selection, err := selectionService.BeginContextSelection(ctx, BeginContextSelectionInput{
			PrincipalID:    user.PrincipalID,
			Authentication: authentication,
			Audit:          audit,
		})
		if err != nil {
			t.Fatalf("issue platform session: %v", err)
		}
		if selection.Session == nil {
			t.Fatalf("platform selection = %#v", selection)
		}
		resolved, err := authContextService.ResolveFirstPartyAccessToken(ctx, selection.Session.AccessToken)
		if err != nil {
			t.Fatalf("resolve platform AuthContext: %v", err)
		}
		if resolved.Context.Type != "platform" || resolved.Context.TenantID != nil ||
			len(resolved.Organization.Departments) != 0 || len(resolved.Organization.ProjectGroups) != 0 ||
			len(resolved.Authorization.RoleAssignments) != 1 ||
			resolved.Authorization.RoleAssignments[0].AssignmentID != formatIAMID(assignmentID) ||
			resolved.Authorization.RoleAssignments[0].Scope.Type != "platform" {
			t.Fatalf("projected platform AuthContext = %#v", resolved)
		}

		if err := db.Exec(`
			UPDATE system.principals
			SET authorization_version = authorization_version + 1, updated_at = now()
			WHERE id = ?
		`, user.PrincipalID).Error; err != nil {
			t.Fatalf("increment platform principal authorization version: %v", err)
		}
		assertAccessTokenReason(
			t,
			authContextService,
			ctx,
			selection.Session.AccessToken,
			AccessTokenInvalidAuthorizationVersion,
		)
	})

	assertAccessTokenReason(t, authContextService, ctx, "invalid", AccessTokenInvalidFormat)
	assertAccessTokenReason(t, authContextService, ctx, "addp_at_unknown", AccessTokenInvalidNotFound)
}

func assertTenantAuthContext(
	t *testing.T,
	authContext *commonauth.AuthContext,
	principalID int64,
	tenantID int64,
	tenantMembershipID int64,
	rootDepartmentID int64,
	childDepartmentID int64,
	departmentMembershipID int64,
	projectGroupID int64,
	projectMembershipID int64,
) {
	t.Helper()
	if authContext.SchemaVersion != commonauth.AuthContextSchemaVersion ||
		authContext.Principal.Type != "user" || authContext.Principal.ID != formatIAMID(principalID) ||
		authContext.Context.Type != "tenant" || authContext.Context.TenantID == nil ||
		*authContext.Context.TenantID != formatIAMID(tenantID) || authContext.Context.TenantMembershipID == nil ||
		*authContext.Context.TenantMembershipID != formatIAMID(tenantMembershipID) {
		t.Fatalf("projected tenant identity/context = %#v", authContext)
	}
	if authContext.Client.ClientID == nil || *authContext.Client.ClientID != "addp-web" ||
		authContext.Client.ScopeMode != "unrestricted" ||
		!reflect.DeepEqual(authContext.Client.Audiences, []string{"addp.api"}) || len(authContext.Client.Scopes) != 0 {
		t.Fatalf("projected tenant client = %#v", authContext.Client)
	}
	if len(authContext.Organization.Departments) != 1 {
		t.Fatalf("projected departments = %#v", authContext.Organization.Departments)
	}
	department := authContext.Organization.Departments[0]
	if department.MembershipID != formatIAMID(departmentMembershipID) ||
		department.DepartmentID != formatIAMID(childDepartmentID) ||
		!reflect.DeepEqual(department.AncestorIDs, []string{formatIAMID(rootDepartmentID)}) {
		t.Fatalf("projected department = %#v", department)
	}
	if len(authContext.Organization.ProjectGroups) != 1 ||
		authContext.Organization.ProjectGroups[0].MembershipID != formatIAMID(projectMembershipID) ||
		authContext.Organization.ProjectGroups[0].ProjectGroupID != formatIAMID(projectGroupID) {
		t.Fatalf("projected project groups = %#v", authContext.Organization.ProjectGroups)
	}
	if len(authContext.Authorization.RoleAssignments) != 3 {
		t.Fatalf("projected role assignments = %#v", authContext.Authorization.RoleAssignments)
	}
	wantScopes := []string{"tenant", "department", "project_group"}
	for index, assignment := range authContext.Authorization.RoleAssignments {
		if assignment.Scope.Type != wantScopes[index] || assignment.RoleKey != "tenant.data_viewer" ||
			!reflect.DeepEqual(assignment.Permissions, []string{
				"manager.content.read",
				"manager.data_item.read",
				"manager.search.execute",
				"meta.catalog.read",
			}) {
			t.Fatalf("projected role assignment %d = %#v", index, assignment)
		}
	}
	if authContext.Token.Type != "first_party_access_token" || authContext.Delegation != nil {
		t.Fatalf("projected token facts = token:%#v delegation:%#v", authContext.Token, authContext.Delegation)
	}
}

func assertAccessTokenReason(
	t *testing.T,
	service *AuthContextService,
	ctx context.Context,
	accessToken string,
	want AccessTokenInvalidReason,
) {
	t.Helper()
	_, err := service.ResolveFirstPartyAccessToken(ctx, accessToken)
	if !errors.Is(err, commonapi.ErrUnauthorized) {
		t.Fatalf("resolve access token error = %v, want unauthorized", err)
	}
	var validationError *AccessTokenValidationError
	if !errors.As(err, &validationError) || validationError.Reason != want {
		t.Fatalf("resolve access token reason = %#v, want %s", validationError, want)
	}
	if err.Error() != commonapi.ErrUnauthorized.Error() {
		t.Fatalf("access token error leaked internal reason: %q", err.Error())
	}
}

func insertDepartment(t *testing.T, db *gorm.DB, tenantID int64, parentID *int64, code string) int64 {
	t.Helper()
	var id int64
	if err := db.Raw(`
		INSERT INTO system.departments (tenant_id, parent_id, code, name)
		VALUES (?, ?, ?, ?)
		RETURNING id
	`, tenantID, parentID, code, code).Scan(&id).Error; err != nil {
		t.Fatalf("insert department %s: %v", code, err)
	}
	return id
}

func insertDepartmentMembership(
	t *testing.T,
	db *gorm.DB,
	tenantID int64,
	departmentID int64,
	tenantMembershipID int64,
) int64 {
	t.Helper()
	var id int64
	if err := db.Raw(`
		INSERT INTO system.department_memberships (
			tenant_id, department_id, tenant_membership_id, membership_type, relation_role
		)
		VALUES (?, ?, ?, 'primary', 'member')
		RETURNING id
	`, tenantID, departmentID, tenantMembershipID).Scan(&id).Error; err != nil {
		t.Fatalf("insert department membership: %v", err)
	}
	return id
}

func insertProjectGroup(t *testing.T, db *gorm.DB, tenantID int64, code string) int64 {
	t.Helper()
	var id int64
	if err := db.Raw(`
		INSERT INTO system.project_groups (tenant_id, code, name, status)
		VALUES (?, ?, ?, 'planned')
		RETURNING id
	`, tenantID, code, code).Scan(&id).Error; err != nil {
		t.Fatalf("insert project group: %v", err)
	}
	return id
}

func insertProjectGroupMembership(
	t *testing.T,
	db *gorm.DB,
	tenantID int64,
	projectGroupID int64,
	tenantMembershipID int64,
) int64 {
	t.Helper()
	var id int64
	if err := db.Raw(`
		INSERT INTO system.project_group_memberships (
			tenant_id, project_group_id, tenant_membership_id, relation_role
		)
		VALUES (?, ?, ?, 'member')
		RETURNING id
	`, tenantID, projectGroupID, tenantMembershipID).Scan(&id).Error; err != nil {
		t.Fatalf("insert project group membership: %v", err)
	}
	return id
}

func insertRoleAssignment(
	t *testing.T,
	db *gorm.DB,
	principalID int64,
	roleKey string,
	scopeType string,
	tenantID *int64,
	departmentID *int64,
	projectGroupID *int64,
	validFrom time.Time,
	validUntil *time.Time,
	sourceType string,
) int64 {
	t.Helper()
	var roleID int64
	if err := db.Table("system.roles").Select("id").Where("role_key = ?", roleKey).Scan(&roleID).Error; err != nil {
		t.Fatalf("find role %s: %v", roleKey, err)
	}
	if roleID == 0 {
		t.Fatalf("role %s was not seeded", roleKey)
	}
	var createdByPrincipalID *int64
	if sourceType != "bootstrap" {
		createdByPrincipalID = &principalID
	}
	var assignmentID int64
	if err := db.Raw(`
		INSERT INTO system.role_assignments (
			principal_id, role_id, scope_type, tenant_id, department_id, project_group_id,
			valid_from, valid_until, source_type, created_by_principal_id, reason
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'AuthContext integration test')
		RETURNING id
	`,
		principalID,
		roleID,
		scopeType,
		tenantID,
		departmentID,
		projectGroupID,
		validFrom,
		validUntil,
		sourceType,
		createdByPrincipalID,
	).Scan(&assignmentID).Error; err != nil {
		t.Fatalf("insert %s role assignment at %s scope: %v", roleKey, scopeType, err)
	}
	return assignmentID
}
