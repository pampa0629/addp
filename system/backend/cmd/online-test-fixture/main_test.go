package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	commonauthorization "github.com/addp/common/authorization"
	"github.com/addp/system/internal/iam"
	"github.com/addp/system/internal/migration"
	"github.com/addp/system/internal/testsupport"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestGraphProtectionAndOrchestratorForeignTenantIsCreatedSeparatelyAndFailsClosed(t *testing.T) {
	for _, id := range []int64{1, 2, 3} {
		t.Run(fmt.Sprintf("tenant_%d", id), func(t *testing.T) {
			calls := 0
			tenant, err := createOnlineForeignTenant(t.Context(), func(ctx context.Context, input iam.CreateTenantInput) (*iam.ManagedTenant, error) {
				calls++
				if input.Code != "external-online-foreign" || input.InitialAdministratorPrincipalID != 7 || input.ActorPrincipalID != 7 {
					t.Fatalf("foreign Tenant creation lost its dedicated identity: %#v", input)
				}
				return &iam.ManagedTenant{Tenant: iam.Tenant{ID: id}}, nil
			}, 7, 2)
			if calls != 1 {
				t.Fatalf("creation calls = %d", calls)
			}
			if id == 3 {
				if err != nil || tenant == nil || tenant.ID != id {
					t.Fatalf("distinct Tenant rejected: %v", err)
				}
			} else if err == nil || tenant != nil {
				t.Fatal("default or consumer Tenant accepted as the foreign Tenant")
			}
		})
	}
	failure := errors.New("creation failed")
	tenant, err := createOnlineForeignTenant(t.Context(), func(context.Context, iam.CreateTenantInput) (*iam.ManagedTenant, error) {
		return nil, failure
	}, 7, 2)
	if tenant != nil || !errors.Is(err, failure) {
		t.Fatal("creation failure was replaced with a reserve Tenant")
	}
}

func TestTenantAdministratorAssignmentIsResolvedByIdentityRatherThanListOrder(t *testing.T) {
	tenantID := int64(8)
	administrator := iam.ManagedTenantRoleAssignment{
		RoleAssignment: iam.RoleAssignment{ID: 11, PrincipalID: 7, TenantID: &tenantID, ScopeType: "tenant"},
		PrincipalType:  iam.PrincipalTypeUser, RoleKey: "tenant.administrator", EffectiveState: "effective",
	}
	runtime := administrator
	runtime.ID, runtime.PrincipalID, runtime.PrincipalType = 12, 9, iam.PrincipalTypeServicePrincipal
	runtime.RoleKey = "system.service_runtime"
	for _, assignments := range [][]iam.ManagedTenantRoleAssignment{{runtime, administrator}, {administrator, runtime}} {
		id, err := tenantAdministratorAssignmentID(assignments, tenantID, 7)
		if err != nil || id != administrator.ID {
			t.Fatalf("administrator selection depends on list order: id=%d, error=%v", id, err)
		}
	}
	for _, mutate := range []func(*iam.ManagedTenantRoleAssignment){
		func(a *iam.ManagedTenantRoleAssignment) { a.PrincipalID = 9 },
		func(a *iam.ManagedTenantRoleAssignment) { a.PrincipalType = iam.PrincipalTypeServicePrincipal },
		func(a *iam.ManagedTenantRoleAssignment) { a.RoleKey = "tenant.infrastructure_administrator" },
		func(a *iam.ManagedTenantRoleAssignment) { a.TenantID = nil },
		func(a *iam.ManagedTenantRoleAssignment) { other := int64(9); a.TenantID = &other },
		func(a *iam.ManagedTenantRoleAssignment) { a.ScopeType = "department" },
		func(a *iam.ManagedTenantRoleAssignment) { a.EffectiveState = "expired" },
	} {
		invalid := administrator
		mutate(&invalid)
		if id, err := tenantAdministratorAssignmentID([]iam.ManagedTenantRoleAssignment{invalid}, tenantID, 7); id != 0 || err == nil {
			t.Fatal("unrelated or ineffective assignment accepted as the Tenant administrator")
		}
	}
	for _, assignments := range [][]iam.ManagedTenantRoleAssignment{nil, {administrator, administrator}} {
		if id, err := tenantAdministratorAssignmentID(assignments, tenantID, 7); id != 0 || err == nil {
			t.Fatal("missing or ambiguous Tenant administrator accepted")
		}
	}
}

func TestFixtureRolesKeepSystemEngineControlPlaneOutOfConsumerIdentity(t *testing.T) {
	if engineProvisionerRoleKey != "tenant.infrastructure_administrator" {
		t.Fatalf("engine provisioner role = %q", engineProvisionerRoleKey)
	}
	if !contains(consumerPermissions, "system.execution_authorization.create") {
		t.Fatal("consumer role must be able to derive a scoped execution authorization")
	}
	for _, permission := range consumerPermissions {
		if strings.HasPrefix(permission, "system.engine.") {
			t.Fatalf("consumer role must not access the System Engine control plane: %s", permission)
		}
	}
}

func TestPGAuthorizationFixtureSeparatesOperatorAndReader(t *testing.T) {
	permissions, err := suitePermissions("pg-source-authorization")
	if err != nil || !slices.Equal(permissions, pgAuthorizationConsumerPermissions) || !needsEngineProvisioner("pg-source-authorization") {
		t.Fatalf("PG consumer fixture is not registered: %v %v", permissions, err)
	}
	for _, key := range permissions {
		if (strings.HasPrefix(key, "system.") && key != "system.execution_authorization.create") || strings.HasPrefix(key, "iam.") {
			t.Fatalf("consumer gained control-plane permission: %s", key)
		}
	}
	if len(permissions) != 9 || len(pgAuthorizationOperatorPermissions) != 11 || len(sourceInitializerPermissions) != 4 {
		t.Fatal("PG fixture widened shared initializer or reader permissions")
	}
	for _, key := range []string{"system.engine_access_delegation.create", "system.engine_access_deny.create", "system.engine_access_deny.release", "catalog.inventory.read", "system.engine_access_approval_requirement.update",
		"meta.scan_task.create", "meta.scan_task.update", "meta.scan_task.delete",
		"system.meta_collection_qualification.read", "system.meta_collection_qualification.approve", "system.meta_collection_qualification.revoke"} {
		if !slices.Contains(pgAuthorizationOperatorPermissions, key) {
			t.Fatalf("operator lacks explicit command permission: %s", key)
		}
	}
	if slices.Contains(pgAuthorizationOperatorPermissions, "manager.data_item.read") {
		t.Fatal("operator must not be the source reader")
	}
	if !slices.Equal(pgAuthorizationConfirmerPermissions, []string{
		"catalog.entry.read", "catalog.entry.update", "catalog.inventory.read", "catalog.sharing_decision.create",
	}) || slices.Contains(pgAuthorizationOperatorPermissions, "catalog.sharing_decision.create") {
		t.Fatal("business confirmation must remain separate from handling and source reading")
	}
}

func TestPGAuthorizationMembershipsAgainstPostgres(t *testing.T) {
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
	t.Cleanup(func() { reset(); connection.Close() })
	reset()
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	if err := migration.NewRunner(dsn).Run(ctx); err != nil {
		t.Fatal(err)
	}
	repo := iam.NewRepository(db)
	clock, err := testsupport.FixtureDatabaseClock(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	identity := iam.NewIdentityService(repo, nil)
	admin, err := createUser(ctx, identity, "pg-fixture-admin")
	if err != nil {
		t.Fatal(err)
	}
	tenant, err := iam.NewPlatformTenantService(repo, clock).Create(ctx, iam.CreateTenantInput{
		Code: "pg_fixture", Name: "PG fixture", InitialAdministratorPrincipalID: admin.PrincipalID,
		ActorPrincipalID: admin.PrincipalID, Audit: audit("pg-fixture-tenant")})
	if err != nil {
		t.Fatal(err)
	}
	tokens, err := iam.NewTokenFamilyService(repo, fixtureSessionConfig("pg-source-authorization"), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	selection, err := iam.NewContextSelectionService(repo, tokens)
	if err != nil {
		t.Fatal(err)
	}
	memberships, roles := iam.NewTenantMembershipService(repo, clock), iam.NewTenantRoleService(repo, clock)
	reader, assignmentID, _, err := createPermissionFixture(ctx, repo, identity, memberships, roles, selection,
		tenant.ID, admin.PrincipalID, "pg-fixture-reader", pgAuthorizationConsumerPermissions)
	if err != nil {
		t.Fatal(err)
	}
	values, err := preparePGAuthorizationMemberships(ctx, iam.NewOrganizationService(repo, clock),
		tenant.ID, admin.PrincipalID, *reader.Context.TenantMembershipID)
	if err != nil {
		t.Fatal(err)
	}
	contexts, err := iam.NewAuthContextService(repo)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := contexts.ResolveAccessToken(ctx, reader.AccessToken); err == nil {
		t.Fatal("membership changes kept a stale reader credential valid")
	}
	assignment, err := roles.GetAssignment(ctx, tenant.ID, assignmentID)
	if err != nil {
		t.Fatal(err)
	}
	refreshed, err := issueSession(ctx, repo, selection, assignment.PrincipalID, "pg-fixture-reader-current")
	if err != nil {
		t.Fatal(err)
	}
	current, err := contexts.ResolveAccessToken(ctx, refreshed.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	if len(current.Organization.Departments) != 1 || len(current.Organization.ProjectGroups) != 1 ||
		current.Organization.Departments[0].DepartmentID != values["ADDP_ONLINE_FIXTURE_DEPARTMENT_ID"] ||
		current.Organization.ProjectGroups[0].ProjectGroupID != values["ADDP_ONLINE_FIXTURE_PROJECT_GROUP_ID"] {
		t.Fatal("reader organizations differ from the declared fixture")
	}
	for _, value := range values {
		if id, err := strconv.ParseInt(value, 10, 64); err != nil || id <= 0 {
			t.Fatal("invalid organization identity", value)
		}
	}
	outsider, _, _, err := createPermissionFixture(ctx, repo, identity, memberships, roles, selection,
		tenant.ID, admin.PrincipalID, "pg-fixture-outsider", pgAuthorizationConsumerPermissions)
	if err != nil {
		t.Fatal(err)
	}
	other, err := contexts.ResolveAccessToken(ctx, outsider.AccessToken)
	if err != nil || len(other.Organization.Departments) != 0 || len(other.Organization.ProjectGroups) != 0 {
		t.Fatal("outsider gained fixture organization membership", err)
	}
	for _, projection := range []*commonauthorization.AuthContext{current, other} {
		actual := []string{}
		for _, role := range projection.Authorization.RoleAssignments {
			actual = append(actual, role.Permissions...)
		}
		want := slices.Clone(pgAuthorizationConsumerPermissions)
		slices.Sort(actual)
		slices.Sort(want)
		if !slices.Equal(actual, want) {
			t.Fatal("organization preparation changed reader permissions", actual)
		}
	}
	coordinator, err := issueSession(ctx, repo, selection, admin.PrincipalID, "pg-fixture-membership-operator")
	if err != nil {
		t.Fatal(err)
	}
	coordinatorContext, err := contexts.ResolveAccessToken(ctx, coordinator.AccessToken)
	if err != nil || len(coordinatorContext.Organization.Departments) != 0 || len(coordinatorContext.Organization.ProjectGroups) != 0 {
		t.Fatal("membership operator must be independent of reader organizations", err)
	}
	if len(coordinatorContext.Authorization.RoleAssignments) != 1 || coordinatorContext.Authorization.RoleAssignments[0].RoleKey != "tenant.administrator" {
		t.Fatal("membership operator must use the existing fixture Tenant administrator")
	}
	coordinatorID, err := strconv.ParseInt(coordinatorContext.Principal.ID, 10, 64)
	if err != nil || coordinatorID <= 0 {
		t.Fatal("membership operator identity is invalid", err)
	}
	organizations := iam.NewOrganizationService(repo, clock)
	for _, department := range []bool{true, false} {
		var organizationID, membershipID int64
		if department {
			organizationID, _ = strconv.ParseInt(current.Organization.Departments[0].DepartmentID, 10, 64)
			membershipID, _ = strconv.ParseInt(current.Organization.Departments[0].MembershipID, 10, 64)
		} else {
			organizationID, _ = strconv.ParseInt(current.Organization.ProjectGroups[0].ProjectGroupID, 10, 64)
			membershipID, _ = strconv.ParseInt(current.Organization.ProjectGroups[0].MembershipID, 10, 64)
		}
		close := iam.CloseOrganizationMembershipInput{TenantID: tenant.ID, OrganizationID: organizationID,
			MembershipID: membershipID, Version: 1, ActorPrincipalID: coordinatorID,
			Reason: "Disposable PG member exit", Audit: audit("pg-fixture-member-exit")}
		if department {
			_, err = organizations.CloseDepartmentMembership(ctx, close)
		} else {
			_, err = organizations.CloseProjectGroupMembership(ctx, close)
		}
		if err != nil {
			t.Fatal("close PG fixture membership", err)
		}
		if _, err := contexts.ResolveAccessToken(ctx, refreshed.AccessToken); err == nil {
			t.Fatal("member exit kept old reader credential valid")
		}
		fresh, err := issueSession(ctx, repo, selection, assignment.PrincipalID, "pg-fixture-reader-after-exit")
		if err != nil {
			t.Fatal(err)
		}
		projection, err := contexts.ResolveAccessToken(ctx, fresh.AccessToken)
		if err != nil || projection.Principal != current.Principal || !reflect.DeepEqual(projection.Context, current.Context) ||
			!reflect.DeepEqual(projection.Authorization.RoleAssignments, current.Authorization.RoleAssignments) ||
			len(projection.Organization.Departments) != 0 || len(projection.Organization.ProjectGroups) != map[bool]int{true: 1, false: 0}[department] {
			t.Fatal("member exit did not preserve reader identity and remaining memberships", err)
		}
		refreshed, current = fresh, projection
	}
}

func TestFixtureAuthorizationContractMatchesPublishedCatalog(t *testing.T) {
	repositoryRoot := filepath.Clean(filepath.Join("..", "..", "..", ".."))
	catalog, err := commonauthorization.LoadRepositoryAuthorizationCatalog(repositoryRoot)
	if err != nil {
		t.Fatal(err)
	}
	permissions := make(map[string]commonauthorization.PermissionDescriptor, len(catalog.Permissions))
	for _, permission := range catalog.Permissions {
		permissions[permission.Key] = permission
	}
	workspaceRoleFound := false
	for _, role := range catalog.Roles {
		if role.Key != pgAuthorizationWorkspaceRoleKey {
			continue
		}
		workspaceRoleFound = true
		if role.RoleType != "tenant_builtin" || !slices.Equal(role.AllowedScopeTypes, []string{"tenant"}) || !slices.Equal(role.AllowedPrincipalTypes, []string{"user"}) {
			t.Fatal("PG workspace must use the formal Tenant User authorizer role")
		}
		combined := slices.Clone(pgAuthorizationOperatorPermissions)
		for _, key := range []string{"catalog.entry.read", "system.engine_access_fulfillment.create"} {
			if !slices.Contains(role.Permissions, key) || slices.Contains(pgAuthorizationOperatorPermissions, key) {
				t.Fatalf("PG business handling must come from the unified builtin role, not the fixture supplement: %s", key)
			}
		}
		for _, key := range role.Permissions {
			if slices.Contains(combined, key) {
				t.Fatalf("PG fixture supplement duplicates the formal authorizer role: %s", key)
			}
			combined = append(combined, key)
		}
		if len(combined) != 23 || slices.Contains(combined, "manager.data_item.read") {
			t.Fatal("PG workspace widened its exact operator permissions")
		}
		for _, key := range []string{"system.engine.read", "system.engine_catalog.read", "system.engine_access_approval_requirement.read", "iam.tenant_membership.read", "iam.department.read", "iam.project_group.read"} {
			if !slices.Contains(combined, key) {
				t.Fatalf("PG workspace lacks formal read permission: %s", key)
			}
		}
	}
	if !workspaceRoleFound {
		t.Fatal("PG workspace builtin role is missing")
	}
	allPermissions := append(append(append(append(append([]string{}, consumerPermissions...), metricPermissions...), ontologyPermissions...), publicOriginReadPermissions...), publicOriginCreatePermissions...)
	allPermissions = append(allPermissions, orchestratorPermissions...)
	allPermissions = append(allPermissions, transferLineagePermissions...)
	allPermissions = append(allPermissions, catalogConsumerPermissions...)
	allPermissions = append(allPermissions, readWorkflowConsumerPermissions...)
	allPermissions = append(allPermissions, rasterWorkflowPermissions...)
	allPermissions = append(allPermissions, vectorPMTilesPermissions...)
	allPermissions = append(allPermissions, securityPermissions...)
	allPermissions = append(allPermissions, securityInitializerPermissions...)
	allPermissions = append(allPermissions, sourceInitializerPermissions...)
	allPermissions = append(allPermissions, suiteSourceInitializerPermissions("ftp-consumer-flow")...)
	allPermissions = append(allPermissions, pgAuthorizationConsumerPermissions...)
	allPermissions = append(allPermissions, pgAuthorizationOperatorPermissions...)
	allPermissions = append(allPermissions, graphReviewPermissions...)
	allPermissions = append(allPermissions, graphProtectionReaderPermissions...)
	allPermissions = append(allPermissions, graphDevelopReaderPermissions...)
	allPermissions = append(allPermissions, graphServiceReaderPermissions...)
	allPermissions = append(allPermissions, graphProtectionPreparerPermissions...)
	allPermissions = append(allPermissions, graphProtectionOutsiderPermissions...)
	for _, key := range allPermissions {
		permission, exists := permissions[key]
		if !exists {
			t.Fatalf("consumer permission %q is not published", key)
		}
		if permission.Status != "active" || !permission.TenantCustomizable || !contains(permission.AllowedScopeTypes, "tenant") {
			t.Fatalf("consumer permission %q must remain active and tenant-customizable at tenant scope", key)
		}
	}

	for _, role := range catalog.Roles {
		if role.Key != engineProvisionerRoleKey {
			continue
		}
		if role.RoleType != "tenant_builtin" || !contains(role.AllowedScopeTypes, "tenant") || !contains(role.AllowedPrincipalTypes, "user") {
			t.Fatalf("engine provisioner role has incompatible assignment contract: %#v", role)
		}
		for _, required := range []string{"system.engine.create", "system.engine.execute", "system.engine.read"} {
			if !contains(role.Permissions, required) {
				t.Fatalf("engine provisioner role is missing %q", required)
			}
		}
		return
	}
	t.Fatalf("engine provisioner role %q is not published", engineProvisionerRoleKey)
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func TestGraphProtectionReviewerCredentialsReachChildEnvironment(t *testing.T) {
	path := filepath.Join(t.TempDir(), "identity.env")
	const token = "synthetic-review-access-token"
	const username = "external-online-graph-reviewer"
	const password = "synthetic-review'quoted-password"
	if err := writeEnvironmentFile(path, map[string]string{
		"ADDP_ONLINE_GRAPH_REVIEW_ACCESS_TOKEN":          token,
		"ADDP_ONLINE_GRAPH_REVIEW_USERNAME":              username,
		"ADDP_ONLINE_GRAPH_REVIEW_PASSWORD":              password,
		"ADDP_ONLINE_GRAPH_SERVICE_ACCESS_TOKEN":         token,
		"ADDP_ONLINE_GRAPH_SERVICE_USERNAME":             username,
		"ADDP_ONLINE_GRAPH_SERVICE_PASSWORD":             password,
		"ADDP_ONLINE_GRAPH_SERVICE_FOREIGN_ACCESS_TOKEN": "synthetic-foreign-token",
		"ADDP_ONLINE_GRAPH_SERVICE_FOREIGN_TENANT_ID":    "3",
	}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("review credentials mode = %o, want 600", info.Mode().Perm())
	}
	// Start without inherited credentials, then source the real exported file.
	// This catches fields created in memory but omitted from the public helper's
	// owner-only output, as well as quoting mistakes in the fixture password.
	command := exec.Command("bash", "-c", `set -eu
source "$1"
[ "${ADDP_ONLINE_GRAPH_REVIEW_ACCESS_TOKEN:?}" = "$2" ]
[ "${ADDP_ONLINE_GRAPH_REVIEW_USERNAME:?}" = "$3" ]
[ "${ADDP_ONLINE_GRAPH_REVIEW_PASSWORD:?}" = "$4" ]
[ "${ADDP_ONLINE_GRAPH_SERVICE_ACCESS_TOKEN:?}" = "$2" ]
[ "${ADDP_ONLINE_GRAPH_SERVICE_USERNAME:?}" = "$3" ]
[ "${ADDP_ONLINE_GRAPH_SERVICE_PASSWORD:?}" = "$4" ]
[ "${ADDP_ONLINE_GRAPH_SERVICE_FOREIGN_ACCESS_TOKEN:?}" = "synthetic-foreign-token" ]
[ "${ADDP_ONLINE_GRAPH_SERVICE_FOREIGN_TENANT_ID:?}" = "3" ]
`, "graph-review-environment", path, token, username, password)
	command.Env = []string{"PATH=" + os.Getenv("PATH")}
	if err := command.Run(); err != nil {
		t.Fatalf("review credentials did not reach isolated child: %v", err)
	}
}

func TestGraphProtectionFixtureKeepsReaderAndPreparationSeparate(t *testing.T) {
	expectedReview := []string{"graph.graph.read", "graph.review.read", "graph.review.approve", "graph.review.reject", "graph.review.update"}
	if !slices.Equal(graphReviewPermissions, expectedReview) {
		t.Fatalf("reviewer must have exactly the approved five permissions: %v", graphReviewPermissions)
	}
	for _, action := range []string{"read", "create", "update", "execute", "cancel", "delete"} {
		if !slices.Contains(graphProtectionPreparerPermissions, "graph.build_task."+action) {
			t.Fatalf("preparer missing approved build permission: %s", action)
		}
	}
	for _, permission := range graphProtectionPreparerPermissions {
		if strings.HasPrefix(permission, "graph.review.") {
			t.Fatalf("preparer must not gain review authority: %s", permission)
		}
	}
	expectedDevelop := []string{"develop.task.read", "develop.task.execute", "develop.data_read.execute", "system.execution_authorization.create", "meta.catalog.read"}
	if !slices.Equal(graphServiceReaderPermissions, []string{"service.definition.read"}) {
		t.Fatalf("Service graph reader must only read definitions: %v", graphServiceReaderPermissions)
	}
	for _, action := range []string{"create", "read", "update", "delete"} {
		if !slices.Contains(graphProtectionPreparerPermissions, "service.definition."+action) {
			t.Fatalf("Service fixture preparer missing %s", action)
		}
	}
	if !slices.Equal(graphDevelopReaderPermissions, expectedDevelop) {
		t.Fatalf("Develop reader must have exactly the approved five permissions: %v", graphDevelopReaderPermissions)
	}
	permissions, err := suitePermissions("graph-protection")
	if err != nil || !slices.Equal(permissions, []string{"graph.graph.read"}) || !needsEngineProvisioner("graph-protection") {
		t.Fatalf("Graph reader must have only graph.graph.read: %v %v", permissions, err)
	}
	for _, forbidden := range []string{"system.engine.read", "system.engine_access_grant.create", "system.execution_authorization.create", "develop.data_read.execute"} {
		if slices.Contains(graphProtectionPreparerPermissions, forbidden) || slices.Contains(permissions, forbidden) {
			t.Fatalf("Graph fixture must not expand source authorization: %s", forbidden)
		}
	}
	if slices.Contains(graphProtectionOutsiderPermissions, "graph.graph.read") || slices.Contains(permissions, "graph.graph.update") {
		t.Fatal("Graph fixture mixed reader, outsider or public-state authority")
	}
	values := consumerEnvironment("graph-protection", 2, "private-access", "private-password")
	if values["ADDP_ONLINE_TEST_USER_USERNAME"] != "external-online-consumer" || values["ADDP_ONLINE_TEST_USER_PASSWORD"] != "private-password" {
		t.Fatal("Graph reader credentials are not bound to the disposable User")
	}
}

func TestComposePublicOriginFixtureHasNoGrantedPermissions(t *testing.T) {
	permissions, err := suitePermissions("compose-public-origin")
	if err != nil {
		t.Fatal(err)
	}
	if len(permissions) != 0 {
		t.Fatalf("unexpected public origin fixture permissions: %#v", permissions)
	}
	if needsEngineProvisioner("compose-public-origin") {
		t.Fatal("public origin fixture must not grant Engine provisioner access")
	}
	for _, permission := range append(append([]string{}, publicOriginReadPermissions...), publicOriginCreatePermissions...) {
		if permission == "iam.tenant_role_assignment.read" || strings.HasPrefix(permission, "system.engine.") {
			t.Fatalf("public origin business fixture must not receive IAM or Engine control-plane access: %s", permission)
		}
	}
}

func TestComposePublicOriginBrowserReaderCanEnterManagerWithoutWritePrivileges(t *testing.T) {
	// Manager's data-explorer entry requires both navigation and item-read access.
	for _, permission := range []string{"manager.content.read", "manager.data_item.read"} {
		if !contains(publicOriginReadPermissions, permission) {
			t.Fatalf("public origin browser reader cannot enter Manager: missing %s", permission)
		}
	}
	for _, permission := range publicOriginReadPermissions {
		if !strings.HasSuffix(permission, ".read") {
			t.Fatalf("public origin browser reader must remain read-only: %s", permission)
		}
	}
}

func TestValidateExternalEnvironment(t *testing.T) {
	valid := []string{"GITHUB_ACTIONS=true", "RUNNER_OS=Linux", "ADDP_ONLINE_HOSTED=1"}
	if err := validateExternalEnvironment(valid, filepath.Join(t.TempDir(), "fixture.env"), "manager-internal-artifact-lineage"); err != nil {
		t.Fatal(err)
	}
	ownerManaged := []string{"GITHUB_ACTIONS=true", "RUNNER_OS=Linux", "ADDP_ONLINE_OWNER_MANAGED=1"}
	if err := validateExternalEnvironment(ownerManaged, filepath.Join(t.TempDir(), "fixture.env"), "manager-internal-artifact-lineage"); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range [][]string{
		{"GITHUB_ACTIONS=false", "RUNNER_OS=Linux", "ADDP_ONLINE_HOSTED=1"},
		{"GITHUB_ACTIONS=true", "RUNNER_OS=macOS", "ADDP_ONLINE_HOSTED=1"},
		{"GITHUB_ACTIONS=true", "RUNNER_OS=Linux", "ADDP_ONLINE_HOSTED=0"},
	} {
		if err := validateExternalEnvironment(invalid, filepath.Join(t.TempDir(), "fixture.env"), "manager-internal-artifact-lineage"); err == nil {
			t.Fatalf("environment %#v was accepted", invalid)
		}
	}
	both := []string{"GITHUB_ACTIONS=true", "RUNNER_OS=Linux", "ADDP_ONLINE_HOSTED=1", "ADDP_ONLINE_OWNER_MANAGED=1"}
	if err := validateExternalEnvironment(both, filepath.Join(t.TempDir(), "fixture.env"), "manager-internal-artifact-lineage"); err == nil {
		t.Fatal("multiple external profiles were accepted")
	}
	if err := validateExternalEnvironment(valid, "relative.env", "manager-internal-artifact-lineage"); err == nil {
		t.Fatal("relative output path was accepted")
	}
}

func TestLocalSGMFixtureRequiresItsOwnIsolatedProfileAndManagerSuite(t *testing.T) {
	base := []string{"ADDP_ONLINE_LOCAL_SGM=1", "POSTGRES_DB=addp_online", "POSTGRES_HOST=127.0.0.1"}
	output := filepath.Join(t.TempDir(), "identity.env")
	err := validateExternalEnvironment(base, output, "manager-internal-artifact-lineage")
	allowedHost := runtime.GOARCH == "arm64" && (runtime.GOOS == "darwin" || runtime.GOOS == "linux")
	if allowedHost != (err == nil) {
		t.Fatalf("local ARM64 admission mismatch: %v", err)
	}
	for _, extra := range []string{"GITHUB_ACTIONS=true", "GITHUB_ACTIONS=false", "ADDP_ONLINE_HOSTED=1", "ADDP_ONLINE_OWNER_MANAGED=1", "POSTGRES_DB=addp", "POSTGRES_HOST=192.168.1.1"} {
		if err := validateExternalEnvironment(append(slices.Clone(base), extra), output, "manager-internal-artifact-lineage"); err == nil {
			t.Fatalf("local SGM accepted unsafe input %q", extra)
		}
	}
	if err := validateExternalEnvironment(base, output, "kingbase-consumer-flow"); err == nil {
		t.Fatal("local SGM opened another suite")
	}
}

func TestWriteEnvironmentFileUsesOwnerOnlyPermissionsAndShellQuoting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fixture.env")
	values := map[string]string{
		"ADDP_ONLINE_ADMIN_USER_ACCESS_TOKEN":        "addp_at_admin",
		"ADDP_ONLINE_CREATE_USER_ACCESS_TOKEN":       "addp_at_creator",
		"ADDP_ONLINE_CROSS_TENANT_ASSIGNMENT_ID":     "86",
		"ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN":    "addp_at_engine",
		"ADDP_ONLINE_FIXTURE_SECURITY_ACCESS_TOKEN":  "addp_at_security_initializer",
		"ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN":    "addp_at_source_initializer",
		"ADDP_ONLINE_PG_OPERATOR_USERNAME":           "external-online-pg-authorizer",
		"ADDP_ONLINE_PG_OPERATOR_PASSWORD":           "operator-private-password",
		"ADDP_ONLINE_FIXTURE_OUTSIDER_ACCESS_TOKEN":  "addp_at_pg_outsider",
		"ADDP_ONLINE_FIXTURE_CONFIRMER_ACCESS_TOKEN": "addp_at_pg_business_owner",
		"ADDP_ONLINE_FIXTURE_DEPARTMENT_ID":          "41",
		"ADDP_ONLINE_FIXTURE_PROJECT_GROUP_ID":       "51",
		"ADDP_ONLINE_FOREIGN_USER_ACCESS_TOKEN":      "addp_at_foreign",
		"ADDP_ONLINE_GRAPH_PREPARER_ACCESS_TOKEN":    "addp_at_graph_preparer",
		"ADDP_ONLINE_GRAPH_OUTSIDER_ACCESS_TOKEN":    "addp_at_graph_outsider",
		"ADDP_ONLINE_GRAPH_DEVELOP_ACCESS_TOKEN":     "addp_at_graph_develop_reader",
		"ADDP_ONLINE_GRAPH_DEVELOP_USERNAME":         "external-online-graph-develop-reader",
		"ADDP_ONLINE_GRAPH_DEVELOP_PASSWORD":         "develop'quoted-password",
		"ADDP_ONLINE_OWN_ASSIGNMENT_ID":              "84",
		"ADDP_ONLINE_RASTER_POLICY_PLATFORM_TOKEN":   "addp_at_raster_platform",
		"ADDP_ONLINE_RASTER_POLICY_TENANT_TOKEN":     "addp_at_raster_tenant",
		"ADDP_ONLINE_READ_USER_ACCESS_TOKEN":         "addp_at_reader",
		"ADDP_ONLINE_READ_USER_USERNAME":             "external-online-public-reader",
		"ADDP_ONLINE_READ_USER_PASSWORD":             "reader'quoted-password",
		"ADDP_ONLINE_TEST_TENANT_ID":                 "42",
		"ADDP_ONLINE_TEST_APPROVER_ACCESS_TOKEN":     "addp_at_approver'quoted",
		"ADDP_ONLINE_TEST_USER_ACCESS_TOKEN":         "addp_at_user'quoted",
		"UNREGISTERED_PASSWORD":                      "must-not-export-this-password",
	}
	if err := writeEnvironmentFile(path, values); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %o, want 600", info.Mode().Perm())
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"ADDP_ONLINE_GRAPH_DEVELOP_ACCESS_TOKEN", "ADDP_ONLINE_GRAPH_DEVELOP_USERNAME", "ADDP_ONLINE_GRAPH_DEVELOP_PASSWORD"} {
		if !strings.Contains(string(content), "export "+key+"='"+strings.ReplaceAll(values[key], "'", "'\"'\"'")+"'\n") {
			t.Fatalf("Develop identity export omitted or incorrectly quoted %s", key)
		}
	}
	if !strings.Contains(string(content), "addp_at_user'\"'\"'quoted") {
		t.Fatalf("fixture environment is not shell quoted: %s", content)
	}
	if !strings.Contains(string(content), "export ADDP_ONLINE_FIXTURE_SECURITY_ACCESS_TOKEN='addp_at_security_initializer'\n") {
		t.Fatal("fixture environment omitted the exported Security initialization token")
	}
	if !strings.Contains(string(content), "export ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN='addp_at_source_initializer'\n") {
		t.Fatal("fixture environment omitted the separate source initialization token")
	}
	for _, key := range []string{"ADDP_ONLINE_FIXTURE_OUTSIDER_ACCESS_TOKEN", "ADDP_ONLINE_FIXTURE_CONFIRMER_ACCESS_TOKEN", "ADDP_ONLINE_FIXTURE_DEPARTMENT_ID", "ADDP_ONLINE_FIXTURE_PROJECT_GROUP_ID", "ADDP_ONLINE_PG_OPERATOR_USERNAME", "ADDP_ONLINE_PG_OPERATOR_PASSWORD", "ADDP_ONLINE_GRAPH_PREPARER_ACCESS_TOKEN", "ADDP_ONLINE_GRAPH_OUTSIDER_ACCESS_TOKEN"} {
		if !strings.Contains(string(content), "export "+key+"='"+values[key]+"'\n") {
			t.Fatalf("owner-only environment omitted Online fixture field %s", key)
		}
	}
	if !strings.Contains(string(content), "export ADDP_ONLINE_TEST_APPROVER_ACCESS_TOKEN='addp_at_approver'\"'\"'quoted'\n") {
		t.Fatal("fixture environment omitted the shell-quoted plaintext approver token")
	}
	for _, value := range []string{"addp_at_admin", "addp_at_creator", "addp_at_reader", "addp_at_foreign", "84", "86", "external-online-public-reader", "reader'\"'\"'quoted-password"} {
		if !strings.Contains(string(content), value) {
			t.Fatalf("fixture environment omitted %q", value)
		}
	}
	for _, key := range []string{"ADDP_ONLINE_RASTER_POLICY_PLATFORM_TOKEN", "ADDP_ONLINE_RASTER_POLICY_TENANT_TOKEN"} {
		if !strings.Contains(string(content), "export "+key+"='"+values[key]+"'\n") {
			t.Fatalf("owner-only environment omitted raster management identity %s", key)
		}
	}
	if strings.Contains(string(content), values["UNREGISTERED_PASSWORD"]) {
		t.Fatal("fixture environment exported an unregistered credential")
	}
}

func TestSuitePermissionsAreExplicitAndSeparate(t *testing.T) {
	ontology, err := suitePermissions("ontology-revision-lifecycle")
	if err != nil || len(ontology) != 4 || needsEngineProvisioner("ontology-revision-lifecycle") {
		t.Fatalf("ontology requires exactly four permissions and no Engine provisioner: %v, %v", ontology, err)
	}
	for _, key := range []string{"ontology.revision.read", "ontology.revision.update", "ontology.revision.publish", "system.execution_authorization.create"} {
		if !contains(ontology, key) {
			t.Fatalf("missing ontology permission %s", key)
		}
	}
	for _, suite := range []string{"opengauss-consumer-flow", "kingbase-consumer-flow", "metric-service-revision-lifecycle", "orchestrator-execution"} {
		if !needsEngineProvisioner(suite) {
			t.Fatalf("Engine suite %s lost its provisioner", suite)
		}
	}
	if _, err := suitePermissions(""); err == nil {
		t.Fatal("missing suite accepted")
	}
	if _, err := suitePermissions("unknown"); err == nil {
		t.Fatal("unknown suite accepted")
	}
	metric, err := suitePermissions("metric-service-revision-lifecycle")
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"model.metric_implementation.offline", "service.definition.update", "standard.metric.publish"} {
		if !contains(metric, required) {
			t.Fatalf("metric role missing %s", required)
		}
		if contains(consumerPermissions, required) {
			t.Fatalf("relational role gained %s", required)
		}
	}
	for _, key := range metric {
		if strings.HasPrefix(key, "system.engine.") || key == "model.metric_implementation.delete" {
			t.Fatalf("metric role has unnecessary destructive/control-plane permission %s", key)
		}
	}
}

func TestOntologyEnvironmentDoesNotExportAnEngineCredential(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ontology.env")
	if err := writeEnvironmentFile(path, map[string]string{
		"ADDP_ONLINE_TEST_TENANT_ID": "42", "ADDP_ONLINE_TEST_USER_ACCESS_TOKEN": "addp_at_fixture",
	}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "ENGINE") {
		t.Fatal("Ontology exported an unnecessary Engine credential")
	}
}

func TestOrchestratorFixtureHasOnlyRequiredOwnerPermissions(t *testing.T) {
	permissions, err := suitePermissions("orchestrator-execution")
	if err != nil || len(permissions) != 10 || !needsEngineProvisioner("orchestrator-execution") {
		t.Fatalf("Orchestrator fixture contract: %v, %v", permissions, err)
	}
	for _, key := range []string{"monitor.execution.read", "meta.scan_task.read", "meta.scan_task.execute", "orchestrator.workflow.execute", "system.execution_authorization.create"} {
		if !contains(permissions, key) {
			t.Fatalf("missing execution permission %s", key)
		}
	}
	for _, key := range permissions {
		if strings.HasPrefix(key, "system.engine.") || strings.HasPrefix(key, "iam.") || key == "orchestrator.workflow.cancel" {
			t.Fatalf("unnecessary control-plane permission %s", key)
		}
	}
}

func TestManagerArtifactFixtureUsesMinimumPermissionsAndBrowserCredentials(t *testing.T) {
	permissions, err := suitePermissions("manager-internal-artifact-lineage")
	if err != nil || len(permissions) != 10 || !needsEngineProvisioner("manager-internal-artifact-lineage") {
		t.Fatalf("Manager fixture contract: %v, %v", permissions, err)
	}
	if !slices.Contains(permissions, "manager.content.read") {
		t.Fatal("Manager source download requires its existing content read permission")
	}
	if !slices.Contains(permissions, "manager.data_item.update") {
		t.Fatal("Manager encoding and ordinary item refresh require its existing update permission")
	}
	for _, key := range permissions {
		if strings.HasPrefix(key, "system.") || strings.HasPrefix(key, "iam.") {
			t.Fatalf("consumer gained control-plane permission %s", key)
		}
	}
	path := filepath.Join(t.TempDir(), "identity.env")
	if err := writeEnvironmentFile(path, map[string]string{
		"ADDP_ONLINE_TEST_USER_USERNAME": "external-online-consumer",
		"ADDP_ONLINE_TEST_USER_PASSWORD": "Random-secret-123",
	}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(data), "export ADDP_ONLINE_TEST_USER_PASSWORD='Random-secret-123'") {
		t.Fatalf("browser credentials were not written to the owner-only environment: %v", err)
	}
}

func TestManagerKMLExportFixtureReducesConsumerAndInitializerPermissions(t *testing.T) {
	t.Setenv("ADDP_ONLINE_MANAGER_CASE", "kml-export")
	actual, err := suitePermissions("manager-internal-artifact-lineage")
	want := []string{"manager.data_item.read", "manager.derived_artifact.create", "manager.derived_artifact.read", "meta.catalog.read", "meta.scan_task.execute", "meta.scan_task.read", "monitor.execution.read", "transfer.task.read"}
	if err != nil || !slices.Equal(actual, want) {
		t.Fatalf("KML export consumer permissions: %v %v", actual, err)
	}
	if got := suiteSourceInitializerPermissions("manager-internal-artifact-lineage"); !slices.Equal(got, sourceInitializerPermissions) || len(got) != 4 {
		t.Fatalf("KML source initialization expanded: %v", got)
	}
	t.Setenv("ADDP_ONLINE_MANAGER_CASE", "full")
	actual, err = suitePermissions("manager-internal-artifact-lineage")
	if err != nil || len(actual) != 10 || len(suiteSourceInitializerPermissions("manager-internal-artifact-lineage")) != 7 {
		t.Fatal("KML changed the full case identity")
	}
	t.Setenv("ADDP_ONLINE_MANAGER_CASE", "unknown")
	if _, err := suitePermissions("manager-internal-artifact-lineage"); err == nil {
		t.Fatal("unknown Manager case received an identity")
	}
}

func TestTransferLineageFixtureUsesExactConsumerPermissions(t *testing.T) {
	permissions, err := suitePermissions("transfer-relational-sql-etl")
	if err != nil || len(permissions) != 23 || !needsEngineProvisioner("transfer-relational-sql-etl") {
		t.Fatalf("invalid transfer identity contract: %v %v", permissions, err)
	}
	for _, required := range []string{
		"monitor.execution.read",
		"develop.task.create", "develop.task.read", "develop.task.execute", "develop.task.delete",
		"develop.data_read.execute", "develop.data_write.execute", "system.execution_authorization.create",
		"orchestrator.workflow.create", "orchestrator.workflow.read", "orchestrator.workflow.execute", "orchestrator.workflow.delete",
		"manager.content.read", "manager.data_item.read", "manager.data_item.create", "meta.catalog.read", "meta.lineage.read",
		"meta.scan_task.execute", "meta.scan_task.read",
		"transfer.task.create", "transfer.task.delete", "transfer.task.execute", "transfer.task.read",
	} {
		if !contains(permissions, required) {
			t.Fatalf("missing transfer consumer permission %s", required)
		}
	}
}

type fakeTransferConfigCheckRoles struct {
	fault    string
	assigned bool
}

func (r *fakeTransferConfigCheckRoles) GetAssignment(_ context.Context, _ int64, id int64) (*iam.ManagedTenantRoleAssignment, error) {
	assignment := &iam.ManagedTenantRoleAssignment{RoleAssignment: iam.RoleAssignment{ID: id, PrincipalID: 7},
		MembershipID: 8, PrincipalType: iam.PrincipalTypeUser, RoleKey: "online.external_online_transfer_precheck"}
	switch r.fault {
	case "consumer":
		assignment.RoleKey = "online.transfer_relational_sql_etl"
	case "foreign_membership":
		assignment.MembershipID = 99
	case "service":
		assignment.PrincipalType = iam.PrincipalTypeServicePrincipal
	}
	return assignment, nil
}

func (r *fakeTransferConfigCheckRoles) CreateAssignments(_ context.Context, input iam.CreateTenantRoleAssignmentsInput) ([]iam.ManagedTenantRoleAssignment, error) {
	if input.TenantID != 42 || input.MembershipID != 8 || len(input.RoleIDs) != 1 || input.RoleIDs[0] != 12 {
		return nil, errors.New("precheck assignment is not scoped to the dedicated fixture user")
	}
	if r.fault == "assignment_failure" {
		return nil, errors.New("assignment failed")
	}
	r.assigned = true
	id := int64(13)
	if r.fault == "same_assignment" {
		id = 11
	}
	return []iam.ManagedTenantRoleAssignment{{RoleAssignment: iam.RoleAssignment{ID: id, PrincipalID: 7}, MembershipID: 8}}, nil
}

func TestTransferPrecheckFixtureKeepsConsumerSeparateAndReissuesSession(t *testing.T) {
	if len(transferConfigCheckPermissions) != 2 || !contains(transferConfigCheckPermissions, "transfer.task.create") || !contains(transferConfigCheckPermissions, "meta.catalog.read") || contains(transferLineagePermissions, "system.engine.read") {
		t.Fatal("precheck custom permissions and original execution consumer must stay separate")
	}
	for _, fault := range []string{"", "consumer", "foreign_membership", "service", "invalid_tenant", "same_assignment", "assignment_failure"} {
		t.Run(fault, func(t *testing.T) {
			tenant, membership := int64(42), int64(8)
			if fault == "invalid_tenant" {
				tenant = 99
			}
			original := &iam.IssuedBrowserSession{AccessToken: "old-token", Context: iam.ResolvedSessionContext{Type: iam.ContextTypeTenant, TenantID: &tenant, TenantMembershipID: &membership}}
			roles := &fakeTransferConfigCheckRoles{fault: fault}
			issued := 0
			current := &iam.IssuedBrowserSession{AccessToken: "current-token"}
			result, err := grantTransferConfigCheckEngineReader(t.Context(), roles, func(principalID int64) (*iam.IssuedBrowserSession, error) {
				issued++
				if principalID != 7 || !roles.assigned {
					t.Fatal("fresh session issued before role assignment or for another user")
				}
				return current, nil
			}, original, 42, 1, 11, 12)
			if fault == "" {
				if err != nil || result != current || issued != 1 {
					t.Fatalf("fresh dedicated session missing: %v", err)
				}
			} else if err == nil || result != nil || issued != 0 {
				t.Fatalf("invalid role scope issued a session: %v", err)
			}
		})
	}
	path := filepath.Join(t.TempDir(), "precheck.env")
	if err := writeEnvironmentFile(path, map[string]string{"ADDP_ONLINE_TRANSFER_CHECK_USER_USERNAME": "external-online-transfer-precheck", "ADDP_ONLINE_TRANSFER_CHECK_USER_PASSWORD": "fixture-password"}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(data), "ADDP_ONLINE_TRANSFER_CHECK_USER_PASSWORD='fixture-password'") {
		t.Fatal("dedicated browser credentials were not preserved in the owner-only file")
	}
}

type securityMaskingRolesFixture struct {
	fault    string
	created  *iam.CreateTenantRoleInput
	assigned *iam.CreateTenantRoleAssignmentsInput
}

func (f *securityMaskingRolesFixture) GetAssignment(context.Context, int64, int64) (*iam.ManagedTenantRoleAssignment, error) {
	value := &iam.ManagedTenantRoleAssignment{RoleAssignment: iam.RoleAssignment{ID: 10, PrincipalID: 7},
		MembershipID: 8, RoleKey: "online.external_online_security_role_reader", PrincipalType: iam.PrincipalTypeUser}
	if f.fault == "foreign_user" {
		value.MembershipID = 9
	}
	return value, nil
}
func (f *securityMaskingRolesFixture) CreateRole(_ context.Context, input iam.CreateTenantRoleInput) (*iam.TenantRole, error) {
	f.created = &input
	value := &iam.TenantRole{Role: iam.Role{ID: 20, RoleKey: securityMaskingEligibilityRoleKey}}
	if f.fault == "role" {
		value.RoleKey = "online.other"
	}
	return value, nil
}
func (f *securityMaskingRolesFixture) CreateAssignments(_ context.Context, input iam.CreateTenantRoleAssignmentsInput) ([]iam.ManagedTenantRoleAssignment, error) {
	f.assigned = &input
	value := []iam.ManagedTenantRoleAssignment{{RoleAssignment: iam.RoleAssignment{ID: 30, PrincipalID: 7}, MembershipID: 8}}
	if f.fault == "assignment" {
		value[0].ID = 10
	}
	return value, nil
}

func TestSecurityMaskingEligibilityKeepsFunctionalPermissionsAndReissuesSession(t *testing.T) {
	tenant, membership := int64(2), int64(8)
	original := &iam.IssuedBrowserSession{AccessToken: "old", Context: iam.ResolvedSessionContext{
		Type: iam.ContextTypeTenant, TenantID: &tenant, TenantMembershipID: &membership}}
	for _, fault := range []string{"", "foreign_user", "role", "assignment", "issue"} {
		t.Run(fault, func(t *testing.T) {
			fixture := &securityMaskingRolesFixture{fault: fault}
			issued := 0
			fresh, assignment, err := grantSecurityMaskingEligibility(t.Context(), fixture, func(principal int64) (*iam.IssuedBrowserSession, error) {
				issued++
				if principal != 7 || fixture.assigned == nil {
					t.Fatal("session issued before exact second assignment")
				}
				if fault == "issue" {
					return nil, errors.New("session blocked")
				}
				return &iam.IssuedBrowserSession{AccessToken: "fresh", Context: original.Context}, nil
			}, original, tenant, 3, 10)
			if fault != "" {
				if err == nil || fresh != nil {
					t.Fatal("invalid isolated eligibility fixture accepted")
				}
				return
			}
			if err != nil || assignment != 30 || issued != 1 || fresh.AccessToken != "fresh" {
				t.Fatal(assignment, issued, err)
			}
			if fixture.created.RoleKey != securityMaskingEligibilityRoleKey || !slices.Equal(fixture.created.PermissionKeys, []string{"service.definition.read"}) ||
				!slices.Contains(securityRoleReaderPermissions, "service.definition.read") || fixture.assigned.MembershipID != membership ||
				!slices.Equal(fixture.assigned.RoleIDs, []int64{20}) {
				t.Fatal("eligibility role must duplicate an existing read in a separate assignment")
			}
		})
	}
	if len(securityRoleReaderPermissions) != 6 {
		t.Fatal("Security role reader qualifications expanded")
	}
	for _, key := range securityRoleReaderPermissions {
		if strings.HasPrefix(key, "iam.") || strings.HasPrefix(key, "security.") {
			t.Fatal("reader gained governance rights")
		}
	}
	values := map[string]string{}
	for _, key := range []string{"ADDP_ONLINE_SECURITY_TRANSFER_ROLE_USERNAME", "ADDP_ONLINE_SECURITY_TRANSFER_ROLE_PASSWORD", "ADDP_ONLINE_SECURITY_DEVELOP_ROLE_USERNAME", "ADDP_ONLINE_SECURITY_DEVELOP_ROLE_PASSWORD", "ADDP_ONLINE_SECURITY_ROLE_USER_ACCESS_TOKEN", "ADDP_ONLINE_SECURITY_ROLE_USER_USERNAME", "ADDP_ONLINE_SECURITY_ROLE_USER_PASSWORD", "ADDP_ONLINE_SECURITY_ROLE_ASSIGNMENT_ID", "ADDP_ONLINE_SECURITY_ROLE_CONTROLLER_ACCESS_TOKEN"} {
		values[key] = "private-fixture-value"
	}
	path := filepath.Join(t.TempDir(), "security-role.env")
	if err := writeEnvironmentFile(path, values); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for key := range values {
		if !strings.Contains(string(data), key+"='private-fixture-value'") {
			t.Fatal("missing owner-only credential export", key)
		}
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("fixture credentials must remain owner-only")
	}
}

func TestSecurityFixtureSeparatesPreparationFromOwnerPermissions(t *testing.T) {
	permissions, err := suitePermissions("security-mysql-owner-protection")
	if err != nil {
		t.Fatal(err)
	}
	if !needsEngineProvisioner("security-mysql-owner-protection") {
		t.Fatal("missing provisioner")
	}
	for _, key := range []string{"security.protection_baseline.update", "security.policy.create", "security.enrollment.update", "system.execution_authorization.create", "manager.content.read", "manager.search.execute", "manager.derived_artifact.create", "manager.derived_artifact.read", "monitor.execution.read", "service.definition.update"} {
		if !contains(permissions, key) {
			t.Fatalf("missing owner permission %s", key)
		}
	}
	if contains(consumerPermissions, "manager.search.execute") {
		t.Fatal("Security search acceptance must not expand unrelated consumer permissions")
	}
	for _, suite := range []string{"security-plaintext-access", "opengauss-consumer-flow", "kingbase-consumer-flow"} {
		otherPermissions, err := suitePermissions(suite)
		if err != nil {
			t.Fatal(err)
		}
		if contains(otherPermissions, "security.enrollment.update") {
			t.Fatalf("rediscovery qualification leaked into another suite: %s", suite)
		}
		if contains(otherPermissions, "service.definition.update") {
			t.Fatalf("role cursor update qualification leaked into another suite: %s", suite)
		}
	}
	for _, key := range []string{"security.classification.create", "security.grade.create", "security.detector.create", "system.engine.create"} {
		if contains(permissions, key) {
			t.Fatalf("preparation permission leaked into consumer: %s", key)
		}
	}
	if len(sourceInitializerPermissions) != 4 {
		t.Fatal("source initializer must have only its four formal preparation qualifications")
	}
	for _, key := range sourceInitializerPermissions {
		if contains(permissions, key) || contains(securityInitializerPermissions, key) || contains(consumerPermissions, key) {
			t.Fatalf("source preparation permission leaked: %s", key)
		}
	}
}

func TestConsumerEnvironmentPublishesOnlyRequiredBrowserCredentials(t *testing.T) {
	for _, suite := range []string{"pg-source-authorization", "security-plaintext-access", "security-mysql-owner-protection", "hdfs-spark-consumer-flow", "elasticsearch-consumer-flow", "raster-workflow", "raster-workflow-capacity", "redis-consumer-flow", "manager-internal-artifact-lineage", "transfer-relational-sql-etl"} {
		t.Run(suite, func(t *testing.T) {
			values := consumerEnvironment(suite, 2, "consumer-token", "private'password")
			if values["ADDP_ONLINE_TEST_USER_USERNAME"] != "external-online-consumer" || values["ADDP_ONLINE_TEST_USER_PASSWORD"] != "private'password" || values["ADDP_ONLINE_TEST_USER_ACCESS_TOKEN"] != "consumer-token" || values["ADDP_ONLINE_TEST_TENANT_ID"] != "2" || len(values) != 4 {
				t.Fatal("browser suite did not receive exactly its verified consumer identity and login credentials")
			}
			path := filepath.Join(t.TempDir(), "identity.env")
			if err := writeEnvironmentFile(path, values); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(path)
			if err != nil || !strings.Contains(string(data), "export ADDP_ONLINE_TEST_USER_USERNAME='external-online-consumer'") || !strings.Contains(string(data), "export ADDP_ONLINE_TEST_USER_PASSWORD='private'\"'\"'password'") {
				t.Fatal("browser credentials are missing or not shell quoted in the published environment")
			}
			info, err := os.Stat(path)
			if err != nil || info.Mode().Perm() != 0o600 {
				t.Fatal("browser credentials must remain in an owner-only environment file")
			}
		})
	}
	for _, suite := range []string{"opengauss-consumer-flow", "kingbase-consumer-flow", "metric-service-revision-lifecycle", "ontology-revision-lifecycle"} {
		values := consumerEnvironment(suite, 2, "consumer-token", "private-password")
		if len(values) != 2 || values["ADDP_ONLINE_TEST_USER_USERNAME"] != "" || values["ADDP_ONLINE_TEST_USER_PASSWORD"] != "" {
			t.Fatalf("API-only suite %s must not export browser credentials", suite)
		}
	}
}

func TestRedisFixtureUsesCatalogPermissionWithoutInfrastructureAdministration(t *testing.T) {
	permissions, err := suitePermissions("redis-consumer-flow")
	if err != nil {
		t.Fatal(err)
	}
	expected := []string{"system.engine_catalog.read", "meta.catalog.read", "meta.scan_task.execute", "meta.scan_task.read", "manager.data_item.read", "manager.content.read"}
	if len(permissions) != len(expected) {
		t.Fatalf("unexpected Redis permissions: %v", permissions)
	}
	for _, permission := range expected {
		if !contains(permissions, permission) {
			t.Fatalf("missing %s", permission)
		}
	}
	if !needsEngineProvisioner("redis-consumer-flow") {
		t.Fatal("fixture must register through the separate infrastructure identity")
	}
}

type fakeOrchestratorReaderRoles struct {
	fault string
	calls []string
}

func (r *fakeOrchestratorReaderRoles) GetAssignment(ctx context.Context, tenantID, assignmentID int64) (*iam.ManagedTenantRoleAssignment, error) {
	r.calls = append(r.calls, "get")
	membershipID := int64(8)
	if r.fault == "foreign_membership" {
		membershipID = 99
	}
	return &iam.ManagedTenantRoleAssignment{RoleAssignment: iam.RoleAssignment{ID: assignmentID, PrincipalID: 7},
		MembershipID: membershipID, PrincipalType: iam.PrincipalTypeUser, RoleKey: "online.external_online_parent_reader"}, nil
}

func (r *fakeOrchestratorReaderRoles) CreateRole(ctx context.Context, input iam.CreateTenantRoleInput) (*iam.TenantRole, error) {
	r.calls = append(r.calls, "role")
	if input.TenantID != 42 || input.RoleKey != "online.execution_monitor_reader" || len(input.PermissionKeys) != 1 || input.PermissionKeys[0] != "monitor.execution.read" {
		return nil, errors.New("Monitor role is not independent and minimal")
	}
	return &iam.TenantRole{Role: iam.Role{ID: 12}}, nil
}

func (r *fakeOrchestratorReaderRoles) CreateAssignments(ctx context.Context, input iam.CreateTenantRoleAssignmentsInput) ([]iam.ManagedTenantRoleAssignment, error) {
	r.calls = append(r.calls, "assign")
	if input.TenantID != 42 || input.MembershipID != 8 || len(input.RoleIDs) != 1 || input.RoleIDs[0] != 12 {
		return nil, errors.New("Monitor assignment is not bound to the reader")
	}
	if r.fault == "assignment_failure" {
		return nil, errors.New("assignment failed")
	}
	assignmentID := int64(13)
	if r.fault == "same_assignment" {
		assignmentID = 11
	}
	return []iam.ManagedTenantRoleAssignment{{RoleAssignment: iam.RoleAssignment{ID: assignmentID, PrincipalID: 7}, MembershipID: 8}}, nil
}

func TestOrchestratorParentReaderKeepsMonitorAssignmentIndependentAndIssuesCurrentSession(t *testing.T) {
	for _, fault := range []string{"", "invalid_tenant", "foreign_membership", "same_assignment", "assignment_failure"} {
		t.Run(fault, func(t *testing.T) {
			tenantID, membershipID := int64(42), int64(8)
			if fault == "invalid_tenant" {
				tenantID = 99
			}
			original := &iam.IssuedBrowserSession{AccessToken: "stale-token", Context: iam.ResolvedSessionContext{
				Type: iam.ContextTypeTenant, TenantID: &tenantID, TenantMembershipID: &membershipID,
			}}
			roles := &fakeOrchestratorReaderRoles{fault: fault}
			issued := 0
			current := &iam.IssuedBrowserSession{AccessToken: "current-token"}
			session, err := grantExecutionMonitorReader(t.Context(), roles, func(principalID int64) (*iam.IssuedBrowserSession, error) {
				issued++
				if principalID != 7 || strings.Join(roles.calls, ",") != "get,role,assign" {
					t.Fatal("session issued before the independent grant completed for this reader")
				}
				return current, nil
			}, original, 42, 9, 11, "online.external_online_parent_reader")
			if fault == "" {
				if err != nil || session != current || issued != 1 {
					t.Fatalf("current reader session was not issued: %v", err)
				}
			} else if err == nil || session != nil || issued != 0 {
				t.Fatal("invalid or unfinished grant was accepted as a usable reader session")
			}
			if fault == "invalid_tenant" && len(roles.calls) != 0 {
				t.Fatal("invalid session caused IAM mutation")
			}
			if fault == "foreign_membership" && len(roles.calls) != 1 {
				t.Fatal("foreign assignment caused IAM mutation")
			}
		})
	}
}

func TestOrchestratorPeerHasExactlyOwnerAndMonitorReadPermissions(t *testing.T) {
	if len(orchestratorPeerPermissions) != 2 || !contains(orchestratorPeerPermissions, "meta.scan_task.read") || !contains(orchestratorPeerPermissions, "monitor.execution.read") {
		t.Fatalf("peer must only read Owner scan history and Monitor diagnostics: %v", orchestratorPeerPermissions)
	}
}

func TestOrchestratorReloginCredentialsRemainInOwnerOnlyEnvironment(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials", "fixture.env")
	values := map[string]string{
		"ADDP_ONLINE_PARENT_ASSIGNMENT_ID":     "11",
		"ADDP_ONLINE_PARENT_USER_ACCESS_TOKEN": "private-reader-token",
		"ADDP_ONLINE_PARENT_USER_USERNAME":     "external-online-parent-reader",
		"ADDP_ONLINE_PARENT_USER_PASSWORD":     "private'password",
		"ADDP_ONLINE_PEER_USER_ACCESS_TOKEN":   "private-peer-token",
	}
	if err := writeEnvironmentFile(path, values); err != nil {
		t.Fatal(err)
	}
	for target, want := range map[string]os.FileMode{path: 0o600, filepath.Dir(path): 0o700} {
		info, err := os.Stat(target)
		if err != nil || info.Mode().Perm() != want {
			t.Fatal("reader login material is not owner-only")
		}
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for key := range values {
		if !strings.Contains(string(content), "export "+key+"=") {
			t.Fatal("reader login material was not exported")
		}
	}
	if !strings.Contains(string(content), "private'\"'\"'password") {
		t.Fatal("reader password was not shell quoted")
	}
}

func TestRasterFixtureSeparatesConsumerAndProvisioner(t *testing.T) {
	for _, suite := range []string{"raster-workflow", "raster-workflow-capacity", "raster-nibble-capacity"} {
		t.Run(suite, func(t *testing.T) {
			permissions, err := suitePermissions(suite)
			if err != nil || !needsEngineProvisioner(suite) {
				t.Fatal("raster fixture registration is incomplete", err)
			}
			for _, permission := range permissions {
				if strings.HasPrefix(permission, "system.engine.") || permission == "meta.lineage.create" || strings.HasPrefix(permission, "system.engine_raster_policy.") {
					t.Fatalf("consumer has control-plane permission: %s", permission)
				}
			}
			for _, permission := range []string{"develop.data_read.execute", "develop.data_write.execute", "monitor.execution.read"} {
				if !contains(permissions, permission) {
					t.Fatalf("missing %s", permission)
				}
			}
		})
	}
}

func TestCapacityFixtureSessionLastsBeyondTheThreeHourBudget(t *testing.T) {
	ordinary := fixtureSessionConfig("raster-workflow")
	for _, suite := range []string{"raster-workflow-capacity", "raster-nibble-capacity"} {
		capacity := fixtureSessionConfig(suite)
		if capacity.AccessTokenTTL != 4*time.Hour || capacity.RefreshTokenFamilyTTL != 4*time.Hour || ordinary.AccessTokenTTL != 30*time.Minute || ordinary.RefreshTokenFamilyTTL != 45*time.Minute {
			t.Fatalf("%s: fixture session TTL does not match its isolated suite budget", suite)
		}
	}
}

func TestNibbleCapacityRetainsExactlyTheOrdinaryRasterPermissions(t *testing.T) {
	ordinary, err := suitePermissions("raster-workflow")
	if err != nil {
		t.Fatal(err)
	}
	nibble, err := suitePermissions("raster-nibble-capacity")
	if err != nil || len(nibble) != 12 || len(nibble) != len(ordinary) {
		t.Fatal("unexpected nibble capacity permissions", nibble, err)
	}
	for i, permission := range ordinary {
		if nibble[i] != permission {
			t.Fatalf("nibble capacity changed permission %s", permission)
		}
	}
	values := consumerEnvironment("raster-nibble-capacity", 2, "private-token", "private-password")
	if values["ADDP_ONLINE_TEST_USER_USERNAME"] != "external-online-consumer" || values["ADDP_ONLINE_TEST_USER_PASSWORD"] != "private-password" {
		t.Fatal("nibble capacity identity preparation is incomplete")
	}
	nibble[0] = "mutated"
	if ordinary[0] == "mutated" || rasterWorkflowPermissions[0] == "mutated" {
		t.Fatal("nibble capacity aliases the ordinary permissions")
	}
}

func TestRasterCapacityAddsOnlyManagerCreateAndRead(t *testing.T) {
	ordinary, err := suitePermissions("raster-workflow")
	if err != nil {
		t.Fatal(err)
	}
	capacity, err := suitePermissions("raster-workflow-capacity")
	if err != nil || len(capacity) != len(rasterWorkflowPermissions)+2 {
		t.Fatal("unexpected capacity permissions", capacity, err)
	}
	for _, permission := range []string{"manager.derived_artifact.create", "manager.derived_artifact.read"} {
		if !contains(capacity, permission) || contains(ordinary, permission) {
			t.Fatal("Manager permission escaped the capacity fixture", permission)
		}
	}
	for _, permission := range rasterWorkflowPermissions {
		if !contains(capacity, permission) {
			t.Fatal("capacity lost its original workflow permission", permission)
		}
	}
	for _, permission := range []string{"develop.task.create", "develop.task.delete"} {
		if !contains(ordinary, permission) || contains(capacity, permission) {
			t.Fatal("editor permission escaped the ordinary raster fixture", permission)
		}
	}
	if len(ordinary) != len(rasterWorkflowPermissions)+2 || contains(ordinary, "develop.task.update") {
		t.Fatal("raster editor fixture has unnecessary permissions", ordinary)
	}
	capacity[0] = "mutated"
	if ordinary[0] == "mutated" || rasterWorkflowPermissions[0] == "mutated" {
		t.Fatal("capacity aliases the ordinary permission slice")
	}
}

func TestElasticsearchFixtureUsesOnlyReadConsumerAndSeparateProvisioner(t *testing.T) {
	permissions, err := suitePermissions("elasticsearch-consumer-flow")
	if err != nil || len(permissions) != 10 || !needsEngineProvisioner("elasticsearch-consumer-flow") {
		t.Fatalf("unexpected ES fixture: %v %v", permissions, err)
	}
	for _, key := range permissions {
		if strings.HasPrefix(key, "system.engine.") || strings.HasPrefix(key, "iam.") || strings.HasPrefix(key, "transfer.") || key == "develop.task.create" {
			t.Fatalf("unnecessary permission %s", key)
		}
	}
	for _, key := range []string{"system.engine_catalog.read", "meta.scan_task.execute", "manager.content.read", "develop.data_read.execute", "develop.task.execute", "system.execution_authorization.create"} {
		if !contains(permissions, key) {
			t.Fatalf("missing %s", key)
		}
	}
}

func TestHDFSFixtureUsesPersistedWorkflowConsumerAndSeparateProvisioner(t *testing.T) {
	permissions, err := suitePermissions("hdfs-spark-consumer-flow")
	if err != nil || len(permissions) != 13 || !needsEngineProvisioner("hdfs-spark-consumer-flow") {
		t.Fatalf("unexpected HDFS fixture: %v %v", permissions, err)
	}
	for _, key := range permissions {
		if strings.HasPrefix(key, "system.engine.") || strings.HasPrefix(key, "iam.") {
			t.Fatalf("unnecessary permission %s", key)
		}
	}
	for _, key := range []string{"system.engine_catalog.read", "meta.scan_task.execute", "manager.content.read", "develop.data_read.execute", "develop.task.execute", "system.execution_authorization.create", "develop.data_write.execute", "develop.data_ddl.execute", "meta.lineage.read"} {
		if !contains(permissions, key) {
			t.Fatalf("missing %s", key)
		}
	}
}

func TestPlaintextIdentitiesHaveSeparateMinimumPermissions(t *testing.T) {
	permissions, err := suitePermissions("security-plaintext-access")
	if err != nil || !needsEngineProvisioner("security-plaintext-access") {
		t.Fatal("missing Hosted identity registration", err)
	}
	for _, key := range []string{"security.protection_access_request.create", "security.protection_access_request.read", "manager.data_item.read"} {
		if !contains(permissions, key) {
			t.Fatalf("missing applicant permission %s", key)
		}
	}
	for _, key := range []string{"security.protection_access_request.update", "security.protection_exemption.delete", "security.protection_exemption.read"} {
		if !contains(plaintextApproverPermissions, key) || contains(permissions, key) {
			t.Fatalf("approval permission must remain separate: %s", key)
		}
	}
	if contains(plaintextApproverPermissions, "security.protection_access_request.create") {
		t.Fatal("approver must not request")
	}
	for _, preparation := range append(append([]string{}, securityInitializerPermissions...), sourceInitializerPermissions...) {
		if contains(permissions, preparation) || contains(plaintextApproverPermissions, preparation) {
			t.Fatalf("preparation permission leaked: %s", preparation)
		}
	}
}

func TestPlaintextBrowserApproverCredentialsRemainPrivate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "identity.env")
	values := map[string]string{
		"ADDP_ONLINE_TEST_APPROVER_USERNAME": "external-online-plaintext-approver",
		"ADDP_ONLINE_TEST_APPROVER_PASSWORD": "private'password",
	}
	if err := writeEnvironmentFile(path, values); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "export ADDP_ONLINE_TEST_APPROVER_USERNAME=") ||
		!strings.Contains(string(data), "export ADDP_ONLINE_TEST_APPROVER_PASSWORD=") ||
		strings.Contains(string(data), "export ADDP_ONLINE_TEST_USER_PASSWORD=") {
		t.Fatal("approver credentials are missing or mixed with applicant")
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatal("approver credentials must be owner-only")
	}
	for _, key := range []string{"meta.catalog.read", "security.enrollment.read"} {
		if !contains(plaintextApproverPermissions, key) {
			t.Fatalf("missing UI prerequisite %s", key)
		}
	}
}

func TestContinuousEventFixtureUsesMinimalTenantPermissions(t *testing.T) {
	permissions, err := suitePermissions("transfer-continuous-events")
	if err != nil || !needsEngineProvisioner("transfer-continuous-events") {
		t.Fatalf("permissions=%v err=%v", permissions, err)
	}
	want := []string{"transfer.task.create", "transfer.task.delete", "transfer.task.execute", "transfer.task.read", "transfer.task.update", "monitor.execution.read", "meta.catalog.read", "meta.scan_task.execute", "meta.scan_task.read", "system.execution_authorization.create"}
	if strings.Join(permissions, ",") != strings.Join(want, ",") {
		t.Fatalf("expanded continuous permissions=%v", permissions)
	}
}

func TestContinuousApproverUsesMinimalTenantPermissions(t *testing.T) {
	want := []string{"monitor.execution.read", "transfer.task.read", "transfer.task.update", "meta.scan_task.read"}
	if strings.Join(transferContinuousPeerPermissions, ",") != strings.Join(want, ",") {
		t.Fatalf("expanded continuous approver permissions=%v", transferContinuousPeerPermissions)
	}
}

func TestHighGoFixtureUsesOrdinaryConsumerAndSeparateProvisioner(t *testing.T) {
	permissions, err := suitePermissions("highgo-consumer-flow")
	if err != nil {
		t.Fatal(err)
	}
	expected := append(append([]string{}, catalogConsumerPermissions...), "transfer.task.create", "transfer.task.delete", "transfer.task.execute", "transfer.task.read", "system.execution_authorization.create")
	if len(permissions) != len(expected) {
		t.Fatalf("unexpected HighGo permissions: %v", permissions)
	}
	for _, permission := range expected {
		if !contains(permissions, permission) {
			t.Fatalf("missing %s", permission)
		}
	}
	if !needsEngineProvisioner("highgo-consumer-flow") {
		t.Fatal("separate Engine provisioner required")
	}
	values := consumerEnvironment("highgo-consumer-flow", 2, "consumer-token", "private-password")
	if len(values) != 4 || values["ADDP_ONLINE_TEST_USER_USERNAME"] != "external-online-consumer" {
		t.Fatal("ordinary browser consumer required")
	}
}

func TestPrivateImportOutsiderTokenIsExportedToOwnerOnlyEnvironment(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fixture.env")
	token := "private-import-outsider'quoted"
	if err := writeEnvironmentFile(path, map[string]string{"ADDP_ONLINE_IMPORT_OUTSIDER_ACCESS_TOKEN": token}); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "export ADDP_ONLINE_IMPORT_OUTSIDER_ACCESS_TOKEN='private-import-outsider'\"'\"'quoted'\n" {
		t.Fatal("private import outsider token was not exported with shell quoting")
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatal("private import fixture token must remain owner-only")
	}
}

func TestVectorPMTilesIdentityHasExactConsumerPermissions(t *testing.T) {
	actual, err := suitePermissions("vector-pmtiles-workflow")
	expected := []string{"develop.task.read", "develop.task.execute", "develop.data_read.execute", "develop.data_write.execute",
		"system.execution_authorization.create", "meta.catalog.read", "meta.scan_task.execute", "meta.scan_task.read",
		"meta.lineage.read", "monitor.execution.read", "manager.data_item.read", "manager.derived_artifact.read"}
	if err != nil || !slices.Equal(actual, expected) || !needsEngineProvisioner("vector-pmtiles-workflow") {
		t.Fatal("PMTiles fixture must grant exactly the ordinary consumer permissions plus an independent provisioner")
	}
	values := consumerEnvironment("vector-pmtiles-workflow", 42, "token", "password")
	if values["ADDP_ONLINE_TEST_USER_USERNAME"] != "external-online-consumer" || values["ADDP_ONLINE_TEST_USER_PASSWORD"] != "password" {
		t.Fatal("PMTiles Console must use the same ordinary User")
	}
}

func TestVectorTopologyIdentityHasExactlyElevenPermissions(t *testing.T) {
	actual, err := suitePermissions("vector-topology-persistence")
	expected := append(append([]string{}, rasterWorkflowPermissions...), "manager.data_item.read")
	if err != nil || len(actual) != 11 || !slices.Equal(actual, expected) || !needsEngineProvisioner("vector-topology-persistence") {
		t.Fatal("topology requires exactly eleven consumer permissions and a separate Engine provisioner")
	}
	values := consumerEnvironment("vector-topology-persistence", 42, "token", "password")
	if values["ADDP_ONLINE_TEST_USER_USERNAME"] != "external-online-consumer" || values["ADDP_ONLINE_TEST_USER_PASSWORD"] != "password" {
		t.Fatal("topology Console must use the same ordinary User")
	}
}

func TestGaussDBReadOnlyConsumerFixture(t *testing.T) {
	permissions, err := suitePermissions("gaussdb-consumer-flow")
	expected := append(append([]string{}, catalogConsumerPermissions...), "develop.task.read", "develop.task.execute", "develop.data_read.execute", "system.execution_authorization.create")
	if err != nil || !reflect.DeepEqual(permissions, expected) {
		t.Fatalf("minimal read-only permissions: %v", err)
	}
	if !needsEngineProvisioner("gaussdb-consumer-flow") {
		t.Fatal("separate Engine provisioner required")
	}
	values := consumerEnvironment("gaussdb-consumer-flow", 2, "consumer-token", "private-password")
	if values["ADDP_ONLINE_TEST_USER_USERNAME"] != "external-online-consumer" || values["ADDP_ONLINE_TEST_USER_PASSWORD"] != "private-password" {
		t.Fatal("browser must use the same consumer User")
	}
	if _, ok := values["ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN"]; ok {
		t.Fatal("source authorization is outside GaussDB admission")
	}
}

func TestStarRocksReadOnlyConsumerFixture(t *testing.T) {
	permissions, err := suitePermissions("starrocks-consumer-flow")
	expected := append(append([]string{}, catalogConsumerPermissions...), "develop.task.read", "develop.task.execute", "develop.data_read.execute", "system.execution_authorization.create")
	if err != nil || !reflect.DeepEqual(permissions, expected) || !needsEngineProvisioner("starrocks-consumer-flow") {
		t.Fatal("minimal ordinary consumer plus independent Engine provisioner required")
	}
	values := consumerEnvironment("starrocks-consumer-flow", 2, "consumer-token", "private-password")
	if values["ADDP_ONLINE_TEST_USER_USERNAME"] != "external-online-consumer" {
		t.Fatal("same browser consumer User required")
	}
	if _, ok := values["ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN"]; ok {
		t.Fatal("source authorization is outside read-only fixture scope")
	}
}

func TestFTPFixtureSeparatesModelConsumerAndSourceAuthorization(t *testing.T) {
	permissions, err := suitePermissions("ftp-consumer-flow")
	expected := []string{"system.engine_catalog.read", "meta.catalog.read", "meta.scan_task.execute", "meta.scan_task.read", "manager.data_item.read", "manager.content.read",
		"manager.derived_artifact.create", "manager.derived_artifact.read", "manager.derived_artifact.delete", "manager.data_item.update"}
	if err != nil || !slices.Equal(permissions, expected) || !needsEngineProvisioner("ftp-consumer-flow") {
		t.Fatal("FTP requires a separate provisioner and exactly ten model-consumer permissions", err)
	}
	environment := consumerEnvironment("ftp-consumer-flow", 2, "consumer-token", "private-password")
	if environment["ADDP_ONLINE_TEST_USER_USERNAME"] != "external-online-consumer" || environment["ADDP_ONLINE_TEST_USER_PASSWORD"] != "private-password" {
		t.Fatal("FTP browser must use the same ordinary User")
	}
	if _, exists := environment["ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN"]; exists {
		t.Fatal("consumer environment must not manufacture an issuer credential")
	}
	issuer := suiteSourceInitializerPermissions("ftp-consumer-flow")
	wantIssuer := []string{"system.engine_access_approval_requirement.initialize", "system.engine_access_delegation.create",
		"system.engine_access_grant.create", "system.engine_access_grant.read", "system.engine_access_grant.revoke",
		"system.engine_access_deny.create", "system.engine_access_deny.release"}
	if !slices.Equal(issuer, wantIssuer) {
		t.Fatal("FTP source initializer must have only its seven explicit lifecycle qualifications")
	}
	for _, key := range append(append([]string{}, issuer...), "system.execution_authorization.create", "manager.derived_artifact.update", "monitor.execution.read") {
		if slices.Contains(permissions, key) {
			t.Fatalf("FTP consumer received an unrelated or source-issuing qualification: %s", key)
		}
	}
	managerPermissions, err := suitePermissions("manager-internal-artifact-lineage")
	if err != nil || !slices.Equal(suiteSourceInitializerPermissions("manager-internal-artifact-lineage"), wantIssuer) {
		t.Fatal("Manager MinIO model acceptance requires a separate exact-source issuer", err)
	}
	for _, key := range wantIssuer {
		if slices.Contains(managerPermissions, key) {
			t.Fatalf("Manager browser received issuer permission %s", key)
		}
	}
	issuer[0] = "mutated"
	if !slices.Equal(suiteSourceInitializerPermissions("ftp-consumer-flow"), wantIssuer) || len(sourceInitializerPermissions) != 4 {
		t.Fatal("FTP-specific lifecycle permissions must not mutate the shared initializer")
	}
	for _, suite := range []string{"highgo-consumer-flow", "security-mysql-owner-protection", "security-plaintext-access", "hdfs-spark-consumer-flow"} {
		if !slices.Equal(suiteSourceInitializerPermissions(suite), sourceInitializerPermissions) {
			t.Fatalf("FTP issuer permissions leaked into %s", suite)
		}
	}
	for _, suite := range []string{"redis-consumer-flow", "gaussdb-consumer-flow", "starrocks-consumer-flow"} {
		if len(suiteSourceInitializerPermissions(suite)) != 0 {
			t.Fatalf("FTP unexpectedly created a source initializer for %s", suite)
		}
	}
}

func TestWorkflowRuntimeHealthFixtureRequiresNoUserPermissionsOrEngineProvisioner(t *testing.T) {
	permissions, err := suitePermissions("workflow-runtime-health-recovery")
	if err != nil || len(permissions) != 0 || needsEngineProvisioner("workflow-runtime-health-recovery") {
		t.Fatalf("health fixture must only create isolated tenant identities: %v, %v", permissions, err)
	}
}

func TestExecutionRetentionFixtureHasOnlyMonitorReadPermission(t *testing.T) {
	for _, suite := range []string{"execution-retention-readonly", "execution-events-retention", "execution-retention-hourly"} {
		permissions, err := suitePermissions(suite)
		if err != nil || len(permissions) != 1 || permissions[0] != "monitor.execution.read" || needsEngineProvisioner(suite) {
			t.Fatalf("%s fixture must not grant Owner, IAM, engine or platform capabilities: %v %v", suite, permissions, err)
		}
	}
}

func TestSecurityMaskingDevelopReaderHasOnlyStableFunctionalPermissions(t *testing.T) {
	want := []string{"develop.task.execute", "develop.task.read", "develop.data_read.execute", "system.execution_authorization.create", "meta.catalog.read", "service.definition.read"}
	if !slices.Equal(securityDevelopRoleReaderPermissions, want) {
		t.Fatal("Develop expiry must preserve the minimal functional role and duplicate eligibility read", securityDevelopRoleReaderPermissions)
	}
}

func TestSecurityMaskingTransferReaderKeepsFunctionalAuthoritySeparate(t *testing.T) {
	want := []string{"transfer.task.create", "transfer.task.read", "transfer.task.update", "transfer.task.delete", "transfer.task.execute", "develop.task.execute", "develop.task.read", "develop.data_read.execute", "develop.data_write.execute", "system.execution_authorization.create", "meta.catalog.read", "service.definition.read"}
	if !slices.Equal(securityTransferRoleReaderPermissions, want) {
		t.Fatal("Transfer eligibility expiry must retain only its task/query authority and duplicate read", securityTransferRoleReaderPermissions)
	}
}

func TestExecutionEventsPaginationFixtureIsReadOnlyAndHasNoEngineProvisioner(t *testing.T) {
	permissions, err := suitePermissions("execution-events-pagination")
	if err != nil || !slices.Equal(permissions, []string{"monitor.execution.read", "meta.scan_task.read"}) || needsEngineProvisioner("execution-events-pagination") {
		t.Fatal("event pagination fixture changed authority", err)
	}
	environment := consumerEnvironment("execution-events-pagination", 2, "private-token", "private-password")
	if environment["ADDP_ONLINE_TEST_USER_USERNAME"] != "external-online-consumer" || environment["ADDP_ONLINE_TEST_USER_PASSWORD"] != "private-password" {
		t.Fatal("browser identity unavailable")
	}
}

func TestExecutionEventsPaginationPrivateEnvironmentReachesScenarioIdentity(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "identity.env")
	if err := writeEnvironmentFile(path, map[string]string{
		"ADDP_ONLINE_TEST_TENANT_ID":              "2",
		"ADDP_ONLINE_TEST_USER_ACCESS_TOKEN":      "synthetic-consumer-token",
		"ADDP_ONLINE_EVENTS_PEER_ACCESS_TOKEN":    "synthetic-peer'quoted-token",
		"ADDP_ONLINE_EVENTS_FOREIGN_ACCESS_TOKEN": "synthetic-foreign-token",
		"ADDP_ONLINE_EVENTS_FOREIGN_TENANT_ID":    "3",
		"ADDP_ONLINE_UNDECLARED_TOKEN":            "must-not-be-exported",
	}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatal("event pagination identity file must remain owner-only")
	}
	// Exercise the real writer, shell export and scenario identity preparation.
	// Only HTTP identities are simulated; stop before any database/fixture work.
	command := exec.Command("bash", "-c", `set -eu
source "$1"
python3 - "$2" <<'PY'
import importlib, json, os, sys
from pathlib import Path
from unittest.mock import patch

M = importlib.import_module('scripts.test.execution-events-pagination-online')
directory = Path(sys.argv[1])
artifact = directory / 'artifact'
artifact.mkdir()
assert 'ADDP_ONLINE_UNDECLARED_TOKEN' not in os.environ
calls = []
class IdentityPrepared(Exception): pass
def client(url, token, timeout):
    assert url == 'http://127.0.0.1:8000' and timeout == 10
    return token
def validate(token, tenant, permissions):
    assert permissions == {'monitor.execution.read', 'meta.scan_task.read'}
    calls.append((token, tenant))
    return {'tenant_id': str(tenant)}
with patch.object(M.BASE, 'boundary', return_value=(directory, artifact)), \
     patch.object(M.API, 'GatewayClient', side_effect=client), \
     patch.object(M.API, 'validate_user_identity', side_effect=validate), \
     patch.object(M, 'run_private', side_effect=IdentityPrepared):
    try:
        M.run()
    except IdentityPrepared:
        pass
    else:
        raise AssertionError('identity preparation did not reach fixture boundary')
assert calls == [('synthetic-consumer-token', 2), ("synthetic-peer'quoted-token", 2), ('synthetic-foreign-token', 3)]
report = json.loads((artifact / 'execution-events-pagination.json').read_text())
assert report['failed_stage'] == 'fixture' and report['checks'] == []
PY
`, "event-pagination-environment", path, directory)
	command.Dir = filepath.Clean(filepath.Join("..", "..", "..", ".."))
	command.Env = []string{"PATH=" + os.Getenv("PATH"), "GATEWAY_URL=http://127.0.0.1:8000", "ADDP_ONLINE_TEST_RUN_ID=unit-event-environment"}
	if err := command.Run(); err != nil {
		t.Fatalf("private identity environment did not reach scenario: %v", err)
	}
}

func TestManagerORCFixtureUsesApprovedElevenPermissions(t *testing.T) {
	t.Setenv("ADDP_ONLINE_MANAGER_CASE", "native-table-orc")
	got, err := suitePermissions("manager-internal-artifact-lineage")
	want := []string{"manager.content.read", "manager.data_item.read", "meta.catalog.read", "meta.scan_task.execute", "meta.scan_task.read", "monitor.execution.read", "transfer.task.create", "transfer.task.read", "transfer.task.execute", "transfer.task.delete", "system.execution_authorization.create"}
	if err != nil || !slices.Equal(got, want) {
		t.Fatalf("ORC fixture permissions differ: %v %v", got, err)
	}
}
