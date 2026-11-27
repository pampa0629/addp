package migration

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	commonauth "github.com/addp/common/authorization"
	"github.com/addp/common/infra/database"
	platformmysql "github.com/addp/common/infra/database/mysql"
	"github.com/addp/system/internal/iam"
	"github.com/addp/system/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func testMySQLScanIdentity(t *testing.T, db *gorm.DB, tenantID, adminID int64) {
	t.Helper()
	ctx := context.Background()
	repo := iam.NewRepository(db)
	identityService := iam.NewIdentityService(repo, nil)
	password := rand.Text()
	newUser, err := identityService.CreateLocalUser(ctx, iam.CreateLocalUserInput{Username: "scan-member", Password: password, DisplayName: "Scan member"})
	if err != nil {
		t.Fatal(err)
	}
	assignMySQLFixtureRole(t, repo, tenantID, adminID, newUser.PrincipalID, "tenant.data_steward")
	tokens, err := iam.NewTokenFamilyService(repo, iam.BrowserSessionConfig{ResourceTicketOwners: models.BrowserResourceAccessOwners}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	selection, err := iam.NewContextSelectionService(repo, tokens)
	if err != nil {
		t.Fatal(err)
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	cipher, err := iam.NewMFACredentialCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	mfa, err := iam.NewMFAService(repo, cipher, iam.MFAServiceConfig{}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	login, err := iam.NewBrowserLoginService(identityService, mfa, selection)
	if err != nil {
		t.Fatal(err)
	}
	session, err := login.LoginLocalBrowser(ctx, iam.LoginLocalBrowserInput{Username: "scan-member", Password: password})
	if err != nil {
		t.Fatal(err)
	}
	if session.NextAction != iam.ContextSelectionNextActionSessionIssued || session.Session == nil {
		t.Fatal("scan member did not receive a browser session")
	}
	resolver, err := iam.NewAuthContextService(repo)
	if err != nil {
		t.Fatal(err)
	}
	auth, err := resolver.ResolveFirstPartyAccessToken(ctx, session.Session.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	if auth.Context.TenantID == nil || *auth.Context.TenantID != strconv.FormatInt(tenantID, 10) || auth.Principal.ID != fmt.Sprint(newUser.PrincipalID) ||
		len(auth.Authorization.RoleAssignments) != 1 || !commonauth.HasContextPermissions(*auth, "meta.scan_task.execute", "meta.scan_task.read") ||
		commonauth.HasContextPermissions(*auth, "iam.tenant_role_assignment.create") {
		t.Fatal("scan member has unexpected identity or permissions")
	}
}

func assignMySQLFixtureRole(t *testing.T, repo *iam.Repository, tenantID, adminID, principalID int64, roleKeys ...string) {
	t.Helper()
	ctx := t.Context()
	member, err := iam.NewTenantMembershipService(repo, nil).EstablishMembership(ctx, iam.EstablishTenantMembershipInput{
		TenantID: tenantID, PrincipalID: principalID, SourceType: iam.TenantMembershipSourceManual, CreatedByPrincipalID: &adminID,
	})
	if err != nil {
		t.Fatal(err)
	}
	roleIDs := make([]int64, 0, len(roleKeys))
	expected := make(map[string]bool, len(roleKeys))
	for _, roleKey := range roleKeys {
		role, err := repo.GetActiveBuiltinRoleByKey(ctx, roleKey)
		if err != nil {
			t.Fatal(err)
		}
		roleIDs = append(roleIDs, role.ID)
		expected[role.RoleKey] = true
	}
	assignments, err := iam.NewTenantRoleService(repo, nil).CreateAssignments(ctx, iam.CreateTenantRoleAssignmentsInput{
		TenantID: tenantID, MembershipID: member.Membership.ID, RoleIDs: roleIDs, ActorPrincipalID: adminID, Reason: "MySQL platform acceptance",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(assignments) != len(roleKeys) {
		t.Fatalf("expected %d assignments, got %d", len(roleKeys), len(assignments))
	}
	for _, got := range assignments {
		if !expected[got.RoleKey] || got.EffectiveState != "effective" {
			dbNow, err := repo.CurrentDatabaseTime(ctx)
			t.Fatalf("created role=%q state=%q valid_from=%s db_now=%s: %v", got.RoleKey, got.EffectiveState, got.ValidFrom, dbNow, err)
		}
		delete(expected, got.RoleKey)
	}
}

// The disposable gate creates tenant identities through the same IAM services as tenant management.
// The observer receives only public IDs. Passwords and deployment credentials never reach Gateway.
func TestPrepareMySQLGatewayTenant(t *testing.T) {
	path := os.Getenv("ADDP_TEST_MYSQL_TENANT_FACTS")
	if path == "" {
		t.Skip("requires the disposable System installation gate")
	}
	driver, err := platformmysql.Dialector(database.Config{
		Host: "127.0.0.1", Port: os.Getenv("ADDP_TEST_MYSQL_PORT"), DBName: "system",
		User: os.Getenv("ADDP_TEST_MYSQL_USER"), Password: os.Getenv("ADDP_TEST_MYSQL_PASSWORD"), ConnectTimeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	db, err := gorm.Open(driver, &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	ctx := t.Context()
	repo := iam.NewRepository(db)
	identities := iam.NewIdentityService(repo, nil)
	admin, err := identities.CreateLocalUser(ctx, iam.CreateLocalUserInput{Username: "scan-tenant-admin", DisplayName: "Scan tenant administrator", Password: rand.Text()})
	if err != nil {
		t.Fatal(err)
	}
	var creator int64
	if err := db.Raw(`SELECT a.principal_id FROM role_assignments a JOIN roles r ON r.id=a.role_id
		WHERE a.scope_type='platform' AND a.status='active' AND r.role_key='platform.system_administrator'`).Row().Scan(&creator); err != nil {
		t.Fatal(err)
	}
	tenant, err := iam.NewPlatformTenantService(repo, nil).Create(ctx, iam.CreateTenantInput{Code: "mysql-scan", Name: "MySQL scan", ActorPrincipalID: creator, InitialAdministratorPrincipalID: admin.PrincipalID})
	if err != nil {
		t.Fatal(err)
	}
	member, err := identities.CreateLocalUser(ctx, iam.CreateLocalUserInput{Username: "scan-member", DisplayName: "Scan member", Password: os.Getenv("ADDP_TEST_MYSQL_MEMBER_PASSWORD")})
	if err != nil {
		t.Fatal(err)
	}
	assignMySQLFixtureRole(t, repo, tenant.ID, admin.PrincipalID, member.PrincipalID, "tenant.data_steward")
	if password := os.Getenv("ADDP_TEST_LINEAGE_PUBLISHER_PASSWORD"); password != "" {
		publisher, err := identities.CreateLocalUser(ctx, iam.CreateLocalUserInput{Username: "lineage-publisher", DisplayName: "Lineage publisher", Password: password})
		if err != nil {
			t.Fatal(err)
		}
		assignMySQLFixtureRole(t, repo, tenant.ID, admin.PrincipalID, publisher.PrincipalID, "tenant.service_publisher")
	}
	if password := os.Getenv("ADDP_TEST_MYSQL_SEARCH_OPERATOR_PASSWORD"); password != "" {
		operator, err := identities.CreateLocalUser(ctx, iam.CreateLocalUserInput{Username: "search-operator", DisplayName: "Search fixture operator", Password: password})
		if err != nil {
			t.Fatal(err)
		}
		assignMySQLFixtureRole(t, repo, tenant.ID, admin.PrincipalID, operator.PrincipalID,
			"tenant.data_steward", "tenant.engine_lifecycle_operator", "tenant.engine_access_delegation_administrator")
	}
	if fileFacts := os.Getenv("ADDP_TEST_MYSQL_DEVELOP_FILE_FACTS"); fileFacts != "" {
		t.Run("DevelopFilePublicationTenant", func(t *testing.T) {
			prepareMySQLHostedTenant(t, repo, creator, fileFacts, "develop-file", []mysqlHostedUser{
				{"develop-file-actor", "ADDP_TEST_MYSQL_DEVELOP_ACTOR_PASSWORD", "", []string{
					"develop.task.read", "develop.task.execute", "develop.data_read.execute", "develop.data_write.execute",
					"system.execution_authorization.create", "meta.catalog.read", "manager.data_item.read",
				}},
				{"develop-file-approver", "ADDP_TEST_MYSQL_DEVELOP_APPROVER_PASSWORD", "tenant.engine_access_delegation_administrator", []string{
					"meta.scan_task.execute", "meta.scan_task.read", "system.develop_file_create_approval.create",
				}},
				{"develop-file-registrar", "ADDP_TEST_MYSQL_DEVELOP_REGISTRAR_PASSWORD", "tenant.infrastructure_administrator", nil},
			})
		})
	}
	if qualityFacts := os.Getenv("ADDP_TEST_MYSQL_QUALITY_FACTS"); qualityFacts != "" {
		t.Run("QualityWorkerTenant", func(t *testing.T) {
			prepareMySQLHostedTenant(t, repo, creator, qualityFacts, "quality-worker", []mysqlHostedUser{
				{"quality-worker-actor", "ADDP_TEST_MYSQL_QUALITY_ACTOR_PASSWORD", "", []string{
					"quality.plan.execute", "quality.plan.read", "monitor.execution.read",
					"system.execution_authorization.create", "develop.data_read.execute",
				}},
				{"quality-worker-author", "ADDP_TEST_MYSQL_QUALITY_AUTHOR_PASSWORD", "", []string{
					"quality.rule.create", "quality.rule.read", "quality.plan.create",
				}},
				{"quality-worker-registrar", "ADDP_TEST_MYSQL_QUALITY_REGISTRAR_PASSWORD", "tenant.infrastructure_administrator", nil},
			})
		})
	}
	t.Run("ExecutionAuthorization", func(t *testing.T) {
		testMySQLExecutionAuthorization(t, db, tenant.ID, admin.PrincipalID)
	})
	facts, err := json.Marshal(map[string]int64{"tenant_id": tenant.ID, "principal_id": member.PrincipalID})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, facts, 0600); err != nil {
		t.Fatal(err)
	}
}

type mysqlHostedUser struct {
	name, passwordEnv, builtinRole string
	permissions                    []string
}

func prepareMySQLHostedTenant(t *testing.T, repo *iam.Repository, creator int64, path, scenario string, users []mysqlHostedUser) {
	t.Helper()
	ctx := t.Context()
	identities := iam.NewIdentityService(repo, nil)
	admin, err := identities.CreateLocalUser(ctx, iam.CreateLocalUserInput{
		Username: scenario + "-admin", DisplayName: scenario + " fixture administrator", Password: rand.Text(),
	})
	if err != nil {
		t.Fatal(err)
	}
	tenant, err := iam.NewPlatformTenantService(repo, nil).Create(ctx, iam.CreateTenantInput{
		Code: "mysql-" + scenario, Name: "MySQL " + scenario + " fixture", ActorPrincipalID: creator, InitialAdministratorPrincipalID: admin.PrincipalID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if tenant.ID <= 1 {
		t.Fatal("Hosted acceptance requires a nondefault fixture tenant")
	}
	facts := map[string]int64{"tenant_id": tenant.ID}
	members := iam.NewTenantMembershipService(repo, nil)
	roles := iam.NewTenantRoleService(repo, nil)
	for _, fixture := range users {
		user, err := identities.CreateLocalUser(ctx, iam.CreateLocalUserInput{
			Username: fixture.name, DisplayName: fixture.name, Password: os.Getenv(fixture.passwordEnv),
		})
		if err != nil {
			t.Fatal(err)
		}
		facts[fixture.name] = user.PrincipalID
		membership, err := members.EstablishMembership(ctx, iam.EstablishTenantMembershipInput{
			TenantID: tenant.ID, PrincipalID: user.PrincipalID, SourceType: iam.TenantMembershipSourceManual, CreatedByPrincipalID: &admin.PrincipalID,
		})
		if err != nil {
			t.Fatal(err)
		}
		var roleIDs []int64
		if fixture.builtinRole != "" {
			role, err := repo.GetActiveBuiltinRoleByKey(ctx, fixture.builtinRole)
			if err != nil {
				t.Fatal(err)
			}
			roleIDs = append(roleIDs, role.ID)
		}
		if len(fixture.permissions) != 0 {
			role, err := roles.CreateRole(ctx, iam.CreateTenantRoleInput{
				TenantID: tenant.ID, RoleKey: "online." + strings.ReplaceAll(fixture.name, "-", "_"), Name: fixture.name,
				PermissionKeys: fixture.permissions, ActorPrincipalID: admin.PrincipalID,
			})
			if err != nil {
				t.Fatal(err)
			}
			roleIDs = append(roleIDs, role.ID)
		}
		if _, err := roles.CreateAssignments(ctx, iam.CreateTenantRoleAssignmentsInput{
			TenantID: tenant.ID, MembershipID: membership.Membership.ID, RoleIDs: roleIDs, ActorPrincipalID: admin.PrincipalID,
			Reason: "Disposable MySQL " + scenario + " acceptance",
		}); err != nil {
			t.Fatal(err)
		}
	}
	encoded, err := json.Marshal(facts)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, encoded, 0600); err != nil {
		t.Fatal(err)
	}
}

func testMySQLExecutionAuthorization(t *testing.T, db *gorm.DB, tenantID, adminID int64) {
	t.Helper()
	ctx := t.Context()
	repo := iam.NewRepository(db)
	user, err := iam.NewIdentityService(repo, nil).CreateLocalUser(ctx, iam.CreateLocalUserInput{
		Username: "quality-worker-fixture", DisplayName: "Quality operator", Password: rand.Text(),
	})
	if err != nil {
		t.Fatal(err)
	}
	member, err := iam.NewTenantMembershipService(repo, nil).EstablishMembership(ctx, iam.EstablishTenantMembershipInput{
		TenantID: tenantID, PrincipalID: user.PrincipalID, SourceType: iam.TenantMembershipSourceManual, CreatedByPrincipalID: &adminID,
	})
	if err != nil {
		t.Fatal(err)
	}
	role, err := repo.GetActiveBuiltinRoleByKey(ctx, "tenant.governance_manager")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := iam.NewTenantRoleService(repo, nil).CreateAssignments(ctx, iam.CreateTenantRoleAssignmentsInput{
		TenantID: tenantID, MembershipID: member.Membership.ID, RoleIDs: []int64{role.ID}, ActorPrincipalID: adminID, Reason: "execution authorization gate",
	}); err != nil {
		t.Fatal(err)
	}
	tokens, err := iam.NewTokenFamilyService(repo, iam.BrowserSessionConfig{ResourceTicketOwners: models.BrowserResourceAccessOwners}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	selection, err := iam.NewContextSelectionService(repo, tokens)
	if err != nil {
		t.Fatal(err)
	}
	now, err := repo.CurrentDatabaseTime(ctx)
	if err != nil {
		t.Fatal(err)
	}
	session, err := selection.BeginContextSelection(ctx, iam.BeginContextSelectionInput{
		PrincipalID: user.PrincipalID,
		Authentication: iam.SessionAuthentication{
			Methods: []string{"password"}, AssuranceLevel: iam.AssuranceLevelAAL1, AuthenticatedAt: now,
		},
	})
	if err != nil || session.Session == nil {
		t.Fatalf("prepare tenant session: %v", err)
	}

	var engineIDs []int64
	for _, name := range []string{"execution-auth-source-a", "execution-auth-source-b"} {
		const insert = "INSERT INTO engines (tenant_id,name,engine_type,connection_info,identity_key,connection_status) VALUES (?,?,'postgresql','{}',JSON_OBJECT('host',?), 'online')"
		if err := db.Exec(insert, tenantID, name, name).Error; err != nil {
			t.Fatal(err)
		}
		var engineID int64
		if err := db.Raw("SELECT id FROM engines WHERE tenant_id=? AND name=?", tenantID, name).Row().Scan(&engineID); err != nil {
			t.Fatal(err)
		}
		engineIDs = append(engineIDs, engineID)
	}
	var qualityPrincipalID int64
	if err := db.Raw("SELECT id FROM service_principals WHERE name='addp-quality'").Row().Scan(&qualityPrincipalID); err != nil {
		t.Fatal(err)
	}
	service, err := iam.NewExecutionAuthorizationService(repo)
	if err != nil {
		t.Fatal(err)
	}
	checkFacts := func(executionID uuid.UUID, wantAuthorizations, wantAccesses, wantAudits int) {
		t.Helper()
		var authorizations, accesses, audits int
		if err := db.Raw(`SELECT
			(SELECT COUNT(*) FROM execution_authorizations WHERE execution_id=? AND sealed_at IS NOT NULL),
			(SELECT COUNT(*) FROM execution_authorization_engine_accesses a JOIN execution_authorizations p ON p.id=a.authorization_id WHERE p.execution_id=?),
			(SELECT COUNT(*) FROM audit_logs WHERE event_name='iam.execution_authorization.issued' AND JSON_UNQUOTE(JSON_EXTRACT(details,'$.execution_id'))=?)`,
			executionID, executionID, executionID.String()).Row().Scan(&authorizations, &accesses, &audits); err != nil {
			t.Fatal(err)
		}
		if authorizations != wantAuthorizations || accesses != wantAccesses || audits != wantAudits {
			t.Fatalf("execution %s: sealed=%d accesses=%d issue audits=%d, want %d/%d/%d", executionID, authorizations, accesses, audits, wantAuthorizations, wantAccesses, wantAudits)
		}
	}
	input := iam.IssueExecutionAuthorizationInput{
		SourceAccessToken: session.Session.AccessToken, Audience: "quality", ExecutionID: uuid.New(),
		Accesses: []iam.ExecutionEngineAccessScope{
			{EngineID: engineIDs[0], Effects: []string{"read"}}, {EngineID: engineIDs[1], Effects: []string{"read"}},
		},
	}
	issued, err := service.Issue(ctx, input)
	if err != nil {
		t.Fatalf("issue engine-scoped authorization: %v", err)
	}
	checkFacts(input.ExecutionID, 1, 2, 1)
	if _, err := service.Issue(ctx, input); !errors.Is(err, iam.ErrExecutionAuthorizationConflict) {
		t.Fatalf("duplicate issue error = %v, want conflict", err)
	}
	checkFacts(input.ExecutionID, 1, 2, 1)

	invalid := input
	invalid.ExecutionID = uuid.New()
	invalid.Accesses = []iam.ExecutionEngineAccessScope{
		input.Accesses[0], {EngineID: 1 << 60, Effects: []string{"read"}},
	}
	if _, err := service.Issue(ctx, invalid); !errors.Is(err, commonapi.ErrBadRequest) {
		t.Fatalf("issue with missing second engine = %v, want bad request", err)
	}
	checkFacts(invalid.ExecutionID, 0, 0, 0)
	var unsealed int
	if err := db.Raw("SELECT COUNT(*) FROM execution_authorizations WHERE execution_id=?", invalid.ExecutionID).Row().Scan(&unsealed); err != nil || unsealed != 0 {
		t.Fatalf("failed issue left an authorization: count=%d err=%v", unsealed, err)
	}
	t.Log("issue sealed both access rows and wrote one audit; duplicate conflicted; invalid second access rolled the whole transaction back")

	principalType, contextType := iam.PrincipalTypeServicePrincipal, iam.ContextTypeTenant
	consume := iam.AuthorizeExecutionEngineAccessInput{
		AuthorizationID: issued.ID, ExecutionID: issued.ExecutionID, EngineID: engineIDs[0],
		ServicePrincipalID: qualityPrincipalID, ServiceClientID: "addp-quality", TenantID: tenantID,
		RequiredEffects: []string{"read"},
		Audit:           iam.AuditMetadata{PrincipalID: &qualityPrincipalID, PrincipalType: &principalType, ContextType: &contextType, TenantID: &tenantID},
	}
	authorized, err := service.AuthorizeEngineAccess(ctx, consume)
	if err != nil || authorized.EngineID != engineIDs[0] {
		t.Fatalf("authorization could not be consumed: %v", err)
	}
	if err := db.Exec("UPDATE execution_authorizations SET revoked_at=NOW(6), revoked_reason='installation gate' WHERE id=?", issued.ID).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := service.AuthorizeEngineAccess(ctx, consume); !errors.Is(err, iam.ErrExecutionAuthorizationUnavailable) {
		t.Fatalf("revoked authorization error = %v, want unavailable", err)
	}
	if err := db.Exec("UPDATE execution_authorizations SET revoked_at=NULL, revoked_reason=NULL WHERE id=?", issued.ID).Error; err == nil ||
		!strings.Contains(err.Error(), "execution authorization revocation is immutable") {
		t.Fatalf("revocation reset error = %v", err)
	}

	input.ExecutionID, input.ExpiresIn = uuid.New(), time.Second
	expiring, err := service.Issue(ctx, input)
	if err != nil {
		t.Fatalf("issue expiring authorization: %v", err)
	}
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(25 * time.Millisecond)
	defer tick.Stop()
	for {
		now, err := repo.CurrentDatabaseTime(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if !now.Before(expiring.ExpiresAt) {
			break
		}
		select {
		case <-tick.C:
		case <-deadline.C:
			t.Fatal("authorization did not expire against the MySQL clock")
		}
	}
	consume.AuthorizationID, consume.ExecutionID = expiring.ID, expiring.ExecutionID
	if _, err := service.AuthorizeEngineAccess(ctx, consume); !errors.Is(err, iam.ErrExecutionAuthorizationUnavailable) {
		t.Fatalf("expired authorization error = %v, want unavailable", err)
	}
	t.Log("active authorization accepted; revoked and naturally expired authorizations rejected")
	t.Run("LeaseExpiryPrecision", func(t *testing.T) {
		testMySQLExecutionAuthorizationLeaseClock(t, db, issued)
	})
}

func testMySQLExecutionAuthorizationLeaseClock(t *testing.T, db *gorm.DB, issued *iam.IssuedExecutionAuthorization) {
	t.Helper()
	ctx := t.Context()
	if err := db.WithContext(ctx).Connection(func(conn *gorm.DB) error {
		// Pin just this test session so both probes see an expiry in the same second.
		if err := conn.Exec("SET timestamp = 1795659000.750000").Error; err != nil {
			return err
		}
		defer func() {
			if err := conn.Exec("SET timestamp = DEFAULT").Error; err != nil {
				t.Errorf("restore test database session clock: %v", err)
			}
		}()
		parentID, childID, leaseToken := uuid.New(), uuid.New(), uuid.New()
		const attempt = 1
		if err := conn.Exec(`INSERT INTO common.task_executions
			(tenant_id,execution_id,module,task_type,status,trigger_type,actor_principal_id,actor_tenant_membership_id,issued_authorization_version,created_at,updated_at)
			VALUES (?,?,'orchestrator','workflow','running','manual',?,?,?,NOW(6),NOW(6))`,
			issued.TenantID, parentID, issued.ActorPrincipalID, issued.TenantMembershipID, issued.IssuedAuthorizationVersion).Error; err != nil {
			return err
		}
		if err := conn.Exec(`INSERT INTO common.task_executions
			(tenant_id,execution_id,module,task_type,status,trigger_type,actor_principal_id,actor_tenant_membership_id,issued_authorization_version,
			 parent_execution_id,attempt,lease_token,lease_expires_at,created_at,updated_at)
			VALUES (?,?,'quality','quality_plan','running','manual',?,?,?,?,?,?,TIMESTAMPADD(MICROSECOND,100000,NOW(6)),NOW(6),NOW(6))`,
			issued.TenantID, childID, issued.ActorPrincipalID, issued.TenantMembershipID, issued.IssuedAuthorizationVersion,
			parentID, attempt, leaseToken).Error; err != nil {
			return err
		}
		leaseAttempt := attempt
		authorization := &iam.ExecutionAuthorization{
			ExecutionID: childID, TenantID: issued.TenantID, Audience: "quality",
			SourceExecutionAttempt: &leaseAttempt, SourceExecutionLeaseToken: &leaseToken,
			ActorPrincipalID: issued.ActorPrincipalID, TenantMembershipID: issued.TenantMembershipID,
			IssuedAuthorizationVersion: issued.IssuedAuthorizationVersion,
		}
		repo := iam.NewRepository(conn)
		if _, err := repo.GetExecutionAuthorizationProvenance(ctx, parentID, childID, "quality", attempt, leaseToken); err != nil {
			t.Fatalf("unexpired lease did not permit deriving authorization: %v", err)
		}
		if current, err := repo.ExecutionAuthorizationLeaseCurrent(ctx, authorization); err != nil || !current {
			t.Fatalf("unexpired lease current=%t, error=%v", current, err)
		}
		if err := conn.Exec("UPDATE common.task_executions SET lease_expires_at=TIMESTAMPADD(MICROSECOND,-250000,NOW(6)) WHERE execution_id=?", childID).Error; err != nil {
			return err
		}
		if _, err := repo.GetExecutionAuthorizationProvenance(ctx, parentID, childID, "quality", attempt, leaseToken); !errors.Is(err, commonapi.ErrNotFound) {
			t.Errorf("expired lease provenance error=%v, want not found", err)
		}
		if current, err := repo.ExecutionAuthorizationLeaseCurrent(ctx, authorization); err != nil || current {
			t.Errorf("expired lease current=%t, error=%v, want false and no error", current, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
