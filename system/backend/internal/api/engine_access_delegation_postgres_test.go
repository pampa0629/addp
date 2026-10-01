package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	commonauth "github.com/addp/common/authorization"
	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/system/internal/engineaccess"
	"github.com/addp/system/internal/iam"
	"github.com/addp/system/internal/middleware"
	"github.com/addp/system/internal/migration"
	"github.com/addp/system/internal/models"
	"github.com/addp/system/internal/testsupport"
	"github.com/gin-gonic/gin"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// This test is automatically included by the existing API PostgreSQL gate's
// AgainstPostgres suffix; it never touches a business engine connection.
func TestEngineAccessDelegationAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("ADDP_SYSTEM_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("requires disposable System IAM PostgreSQL gate")
	}
	testsupport.RequireDisposablePostgresDSN(t, dsn)
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := sqlDB.Close(); err != nil {
			t.Error(err)
		}
	})
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		if err := db.WithContext(cleanupCtx).Exec("DROP SCHEMA IF EXISTS system CASCADE; DROP SCHEMA IF EXISTS common CASCADE").Error; err != nil {
			t.Errorf("cleanup owned test schemas: %v", err)
			return
		}
		var remaining int64
		if err := db.WithContext(cleanupCtx).Raw("SELECT count(*) FROM pg_namespace WHERE nspname IN ('system', 'common')").Scan(&remaining).Error; err != nil || remaining != 0 {
			t.Errorf("test schema residual=%d err=%v", remaining, err)
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := db.Exec("DROP SCHEMA IF EXISTS system CASCADE; DROP SCHEMA IF EXISTS common CASCADE").Error; err != nil {
		t.Fatal(err)
	}
	if err := migration.NewRunner(dsn).Run(ctx); err != nil {
		t.Fatal(err)
	}
	// A repeated migration run preserves all published checksums.
	if err := migration.NewRunner(dsn).Run(ctx); err != nil {
		t.Fatal(err)
	}
	identity := iam.NewRepository(db)
	newUser := func(name string) int64 {
		t.Helper()
		principal := &iam.Principal{PrincipalType: iam.PrincipalTypeUser, Status: iam.PrincipalStatusActive, AuthorizationVersion: 1}
		if err := identity.Transaction(ctx, func(tx *iam.Repository) error {
			if err := tx.CreatePrincipal(ctx, principal); err != nil {
				return err
			}
			return tx.CreateUser(ctx, &iam.User{ID: principal.ID, DisplayName: name})
		}); err != nil {
			t.Fatal(err)
		}
		return principal.ID
	}
	actorID, recipientID, otherID := newUser("Delegator"), newUser("Recipient"), newUser("Other tenant")
	tenantService := iam.NewPlatformTenantService(identity, time.Now)
	tenant, err := tenantService.Create(ctx, iam.CreateTenantInput{Code: "delegation_test", Name: "Delegation Test", InitialAdministratorPrincipalID: actorID, ActorPrincipalID: actorID})
	if err != nil {
		t.Fatal(err)
	}
	otherTenant, err := tenantService.Create(ctx, iam.CreateTenantInput{Code: "delegation_other", Name: "Other", InitialAdministratorPrincipalID: otherID, ActorPrincipalID: otherID})
	if err != nil {
		t.Fatal(err)
	}
	membershipService := iam.NewTenantMembershipService(identity, time.Now)
	recipient, err := membershipService.EstablishMembership(ctx, iam.EstablishTenantMembershipInput{TenantID: tenant.ID, PrincipalID: recipientID, SourceType: iam.TenantMembershipSourceManual})
	if err != nil {
		t.Fatal(err)
	}
	actorMember, err := identity.LockTenantMembership(ctx, tenant.ID, actorID)
	if err != nil {
		t.Fatal(err)
	}
	otherMember, err := identity.LockTenantMembership(ctx, otherTenant.ID, otherID)
	if err != nil {
		t.Fatal(err)
	}
	caps, err := json.Marshal(engineplugin.NewTabularCapabilities("postgresql", "schema", engineplugin.TabularCapabilityOptions{}))
	if err != nil {
		t.Fatal(err)
	}
	capabilities := models.JSONString(caps)
	engineTenantID := uint(tenant.ID)
	engine := models.Engine{Name: "Delegation fixture", EngineType: "postgresql", TenantID: &engineTenantID, LifecycleState: "active", Capabilities: &capabilities, ConnectionInfo: models.ConnectionInfo{}, IdentityKey: models.JSONString(`{"engine_type":"postgresql","host":"delegation-fixture.invalid","port":5432,"database":"fixture"}`)}
	if err := db.Table("system.engines").Create(&engine).Error; err != nil {
		t.Fatal(err)
	}
	var builtinBindings int64
	if err := db.Raw(`SELECT count(*) FROM system.role_permissions rp JOIN system.permissions p ON p.id = rp.permission_id WHERE p.permission_key LIKE 'system.engine_access_delegation.%'`).Scan(&builtinBindings).Error; err != nil || builtinBindings != 0 {
		t.Fatalf("migration implicitly granted built-in roles: count=%d err=%v", builtinBindings, err)
	}
	roleService := iam.NewTenantRoleService(identity, time.Now)
	permissions := []string{"system.engine_access_delegation.create", "system.engine_access_delegation.read", "system.engine_access_delegation.revoke"}
	role, err := roleService.CreateRole(ctx, iam.CreateTenantRoleInput{TenantID: tenant.ID, RoleKey: "custom.engine_delegator", Name: "Engine Delegator", ScopeTypes: []string{"tenant"}, PermissionKeys: permissions, ActorPrincipalID: actorID})
	if err != nil {
		t.Fatal(err)
	}
	stepUp := time.Now().Add(5 * time.Minute)
	assignments, err := roleService.CreateAssignments(ctx, iam.CreateTenantRoleAssignmentsInput{TenantID: tenant.ID, MembershipID: actorMember.ID, RoleIDs: []int64{role.ID}, ScopeType: "tenant", Reason: "Explicit test qualification", ActorPrincipalID: actorID, AssuranceLevel: iam.AssuranceLevelAAL2, StepUpExpiresAt: &stepUp})
	if err != nil {
		t.Fatal(err)
	}
	actorPrincipal, err := identity.LockPrincipal(ctx, actorID)
	if err != nil {
		t.Fatal(err)
	}
	projection := testIAMActorContext("tenant")
	tenantText, memberText := strconv.FormatInt(tenant.ID, 10), strconv.FormatInt(actorMember.ID, 10)
	projection.Principal.ID = strconv.FormatInt(actorID, 10)
	projection.Context.TenantID, projection.Context.TenantMembershipID = &tenantText, &memberText
	projection.Authorization.AuthorizationVersion = strconv.FormatInt(actorPrincipal.AuthorizationVersion, 10)
	projection.Authorization.RoleAssignments = []commonauth.RoleAssignment{{AssignmentID: strconv.FormatInt(assignments[0].ID, 10), RoleKey: role.RoleKey,
		Scope: commonauth.AssignmentScope{Type: "tenant", TenantID: &tenantText}, Permissions: permissions, SourceType: "manual", ValidFrom: time.Now().Add(-time.Minute)}}
	actor := engineaccess.Actor{TenantID: tenant.ID, PrincipalID: actorID, MembershipID: actorMember.ID, AuthorizationVersion: actorPrincipal.AuthorizationVersion, TokenExpiresAt: time.Now().Add(time.Hour)}
	service := engineaccess.NewService(engineaccess.NewRepository(db), hasLiveEngineCatalogCapability)
	baseInput := engineaccess.CreateInput{Actor: actor, EngineID: int64(engine.ID), TenantMembershipID: recipient.Membership.ID, ExpiresAt: time.Now().Add(time.Hour), Reason: "Explicit onboarding administration"}
	limitedExpiry := time.Now().Add(30 * time.Minute)
	limited, err := membershipService.EstablishMembership(ctx, iam.EstablishTenantMembershipInput{TenantID: tenant.ID, PrincipalID: newUser("Limited member"), SourceType: iam.TenantMembershipSourceManual, ExpiresAt: &limitedExpiry})
	if err != nil {
		t.Fatal(err)
	}
	servicePrincipal := &iam.Principal{PrincipalType: iam.PrincipalTypeServicePrincipal, Status: iam.PrincipalStatusActive, AuthorizationVersion: 1}
	if err := identity.CreatePrincipal(ctx, servicePrincipal); err != nil {
		t.Fatal(err)
	}
	serviceMember, err := membershipService.EstablishMembership(ctx, iam.EstablishTenantMembershipInput{TenantID: tenant.ID, PrincipalID: servicePrincipal.ID, SourceType: iam.TenantMembershipSourceManual})
	if err != nil {
		t.Fatal(err)
	}
	beforeRecipient, err := identity.LockPrincipal(ctx, recipientID)
	if err != nil {
		t.Fatal(err)
	}
	// Test-only session fixture, not a new authentication or runtime route.
	if err := db.Exec(`INSERT INTO system.refresh_token_families (principal_id, context_type, tenant_membership_id, issued_authorization_version, client_id, auth_type, audiences, scopes, authentication_methods, assurance_level, authenticated_at, expires_at)
        VALUES (?, 'tenant', ?, ?, 'addp-web', 'first_party', ARRAY['addp.api'], ARRAY[]::text[], ARRAY['password'], 'aal1', now(), now() + interval '1 hour')`, recipientID, recipient.Membership.ID, beforeRecipient.AuthorizationVersion).Error; err != nil {
		t.Fatal(err)
	}
	var initialAudits int64
	db.Table("system.audit_logs").Where("event_name LIKE 'system.engine_access_delegation.%'").Count(&initialAudits)
	for _, invalid := range []struct {
		name   string
		change func(*engineaccess.CreateInput)
		want   error
	}{
		{"missing expiry", func(i *engineaccess.CreateInput) { i.ExpiresAt = time.Time{} }, engineaccess.ErrExpiry},
		{"past expiry", func(i *engineaccess.CreateInput) { i.ExpiresAt = time.Now().Add(-time.Minute) }, engineaccess.ErrExpiry},
		{"unrepresentable expiry", func(i *engineaccess.CreateInput) { i.ExpiresAt = time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC) }, engineaccess.ErrExpiry},
		{"blank reason", func(i *engineaccess.CreateInput) { i.Reason = " " }, commonapi.ErrBadRequest},
		{"cross tenant member", func(i *engineaccess.CreateInput) { i.TenantMembershipID = otherMember.ID }, commonapi.ErrNotFound},
		{"cross tenant engine", func(i *engineaccess.CreateInput) { i.Actor.TenantID = otherTenant.ID }, commonapi.ErrNotFound},
		{"stale principal version", func(i *engineaccess.CreateInput) { i.Actor.AuthorizationVersion++ }, commonapi.ErrForbidden},
		{"expired request", func(i *engineaccess.CreateInput) { i.Actor.TokenExpiresAt = time.Now().Add(-time.Second) }, commonapi.ErrForbidden},
		{"beyond membership expiry", func(i *engineaccess.CreateInput) { i.TenantMembershipID = limited.Membership.ID }, engineaccess.ErrExpiry},
		{"service principal is not a management user", func(i *engineaccess.CreateInput) { i.TenantMembershipID = serviceMember.Membership.ID }, engineaccess.ErrUnavailable},
		{"no live catalog capability", func(i *engineaccess.CreateInput) {
			if err := db.Exec("UPDATE system.engines SET capabilities = '{}'::jsonb WHERE id = ?", engine.ID).Error; err != nil {
				t.Fatal(err)
			}
		}, engineaccess.ErrUnavailable},
	} {
		t.Run(invalid.name, func(t *testing.T) {
			input := baseInput
			invalid.change(&input)
			if _, err := service.Create(ctx, input); !errors.Is(err, invalid.want) {
				t.Fatalf("error=%v want=%v", err, invalid.want)
			}
		})
	}
	if err := db.Exec("UPDATE system.engines SET capabilities = ?::jsonb WHERE id = ?", string(caps), engine.ID).Error; err != nil {
		t.Fatal(err)
	}
	var auditsAfterRejected int64
	db.Table("system.audit_logs").Where("event_name LIKE 'system.engine_access_delegation.%'").Count(&auditsAfterRejected)
	if auditsAfterRejected != initialAudits {
		t.Fatal("rejected requests wrote success audits")
	}
	router := engineDelegationTestRouter(t, service, &projection)
	path := fmt.Sprintf("/api/v1/system/engines/%d/access_delegations", engine.ID)
	requestBody := map[string]any{"tenant_membership_id": strconv.FormatInt(recipient.Membership.ID, 10), "expires_at": baseInput.ExpiresAt, "reason": baseInput.Reason}
	createdResponse := engineDelegationTestRequest(t, router, "POST", path, requestBody, 201)
	var created EngineAccessDelegationResponse
	if err := json.Unmarshal(createdResponse.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.EffectiveState != "effective" || created.Version != 1 || created.PrincipalID != strconv.FormatInt(recipientID, 10) {
		t.Fatalf("created=%+v", created)
	}
	if _, err := service.Create(ctx, baseInput); !errors.Is(err, engineaccess.ErrOverlap) {
		t.Fatalf("overlap=%v", err)
	}
	afterRecipient, _ := identity.LockPrincipal(ctx, recipientID)
	if afterRecipient.AuthorizationVersion != beforeRecipient.AuthorizationVersion+1 {
		t.Fatal("recipient authorization version not advanced")
	}
	var activeFamilies int64
	db.Table("system.refresh_token_families").Where("principal_id = ? AND revoked_at IS NULL", recipientID).Count(&activeFamilies)
	if activeFamilies != 0 {
		t.Fatal("recipient old sessions remain usable")
	}
	listResponse := engineDelegationTestRequest(t, router, "GET", path, nil, 200)
	var page struct {
		Data     []EngineAccessDelegationResponse `json:"data"`
		Total    int64                            `json:"total"`
		Page     int                              `json:"page"`
		PageSize int                              `json:"page_size"`
	}
	if err := json.Unmarshal(listResponse.Body.Bytes(), &page); err != nil || page.Total != 1 || page.Page != 1 || page.PageSize != 10 || len(page.Data) != 1 || page.Data[0].ID != created.ID || page.Data[0].EngineName != engine.Name {
		t.Fatalf("delegation list=%+v err=%v", page, err)
	}
	detailPath := path + "/" + created.ID
	engineDelegationTestRequest(t, router, "GET", detailPath, nil, 200)
	engineDelegationTestRequest(t, router, "POST", detailPath+"/revoke", map[string]any{"version": 2, "reason": "conflict"}, 409)
	engineDelegationTestRequest(t, router, "POST", path, map[string]any{"tenant_id": tenant.ID}, 400)
	engineDelegationTestRequest(t, router, "GET", path+"?tenant_id="+tenantText, nil, 400)
	saved := projection.Authorization.RoleAssignments
	projection.Authorization.RoleAssignments = []commonauth.RoleAssignment{}
	engineDelegationTestRequest(t, router, "GET", path, nil, 403)
	projection.Authorization.RoleAssignments = saved
	projection.Authorization.RoleAssignments[0].Permissions = []string{"system.engine_access_delegation.read"}
	engineDelegationTestRequest(t, router, "POST", path, requestBody, 403)
	projection.Authorization.RoleAssignments[0].Permissions = permissions
	id, _ := strconv.ParseInt(created.ID, 10, 64)
	if _, err := service.Get(ctx, otherTenant.ID, int64(engine.ID), id); !errors.Is(err, commonapi.ErrNotFound) {
		t.Fatalf("cross tenant detail=%v", err)
	}
	// Immutable definition and no deletion/restore route, even through SQL.
	for _, query := range []string{"UPDATE system.engine_access_delegations SET grant_reason = 'rewritten' WHERE id = ?", "DELETE FROM system.engine_access_delegations WHERE id = ?"} {
		if err := db.Exec(query, id).Error; err == nil {
			t.Fatal("delegation history mutation allowed")
		}
	}
	var wg sync.WaitGroup
	outcomes := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := service.Revoke(ctx, engineaccess.RevokeInput{Actor: actor, EngineID: int64(engine.ID), ID: id, Version: 1, Reason: "End onboarding"})
			outcomes <- err
		}()
	}
	wg.Wait()
	close(outcomes)
	successful, conflicting := 0, 0
	for err := range outcomes {
		if err == nil {
			successful++
		} else if errors.Is(err, engineaccess.ErrVersionConflict) {
			conflicting++
		} else {
			t.Fatal(err)
		}
	}
	if successful != 1 || conflicting != 1 {
		t.Fatalf("concurrent revoke=%d successful/%d conflicting", successful, conflicting)
	}
	current, err := service.Get(ctx, tenant.ID, int64(engine.ID), id)
	if err != nil || current.EffectiveState != "revoked" || current.Version != 2 {
		t.Fatalf("revoked=%+v err=%v", current, err)
	}
	if err := db.Exec("UPDATE system.engine_access_delegations SET status = 'active', version = version+1 WHERE id = ?", id).Error; err == nil {
		t.Fatal("history restored")
	}
	// Revocation frees future management qualification; no old row is reused.
	renewed, err := service.Create(ctx, baseInput)
	if err != nil || renewed.ID == id {
		t.Fatalf("new delegation=%+v err=%v", renewed, err)
	}
	if err := db.Exec("UPDATE system.engines SET lifecycle_state = 'disabled' WHERE id = ?", engine.ID).Error; err != nil {
		t.Fatal(err)
	}
	unavailable, err := service.Get(ctx, tenant.ID, int64(engine.ID), renewed.ID)
	if err != nil || unavailable.EffectiveState != "unavailable" {
		t.Fatalf("disabled engine=%+v err=%v", unavailable, err)
	}
	if _, err := service.Create(ctx, baseInput); !errors.Is(err, engineaccess.ErrUnavailable) {
		t.Fatalf("disabled engine creation=%v", err)
	}
	if err := db.Exec("UPDATE system.engines SET lifecycle_state = 'active' WHERE id = ?", engine.ID).Error; err != nil {
		t.Fatal(err)
	}
	// Both creates use the same current qualification; only one may commit.
	limitedInput := baseInput
	limitedInput.TenantMembershipID, limitedInput.ExpiresAt = limited.Membership.ID, limitedExpiry
	outcomes = make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := service.Create(ctx, limitedInput)
			outcomes <- err
		}()
	}
	wg.Wait()
	close(outcomes)
	successful, conflicting = 0, 0
	for err := range outcomes {
		if err == nil {
			successful++
		} else if errors.Is(err, engineaccess.ErrOverlap) {
			conflicting++
		} else {
			t.Fatal(err)
		}
	}
	if successful != 1 || conflicting != 1 {
		t.Fatalf("concurrent create=%d successful/%d conflicting", successful, conflicting)
	}
	// Failure to persist a success audit rolls back delegation and version.
	rollbackRecipient, err := membershipService.EstablishMembership(ctx, iam.EstablishTenantMembershipInput{TenantID: tenant.ID, PrincipalID: newUser("Rollback member"), SourceType: iam.TenantMembershipSourceManual})
	if err != nil {
		t.Fatal(err)
	}
	rollbackPrincipal, err := identity.LockPrincipal(ctx, rollbackRecipient.Membership.PrincipalID)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE FUNCTION system.reject_delegation_test_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
        IF NEW.event_name LIKE 'system.engine_access_delegation.%' THEN RAISE EXCEPTION 'test audit failure'; END IF;
        RETURN NEW; END; $$;
        CREATE TRIGGER reject_delegation_test_audit BEFORE INSERT ON system.audit_logs FOR EACH ROW EXECUTE FUNCTION system.reject_delegation_test_audit()`).Error; err != nil {
		t.Fatal(err)
	}
	rollbackInput := baseInput
	rollbackInput.TenantMembershipID = rollbackRecipient.Membership.ID
	if _, err := service.Create(ctx, rollbackInput); err == nil {
		t.Fatal("audit failure did not roll back")
	}
	if err := db.Exec("DROP TRIGGER reject_delegation_test_audit ON system.audit_logs; DROP FUNCTION system.reject_delegation_test_audit()").Error; err != nil {
		t.Fatal(err)
	}
	rolledBack, err := identity.LockPrincipal(ctx, rollbackPrincipal.ID)
	if err != nil || rolledBack.AuthorizationVersion != rollbackPrincipal.AuthorizationVersion {
		t.Fatalf("rollback principal=%+v err=%v", rolledBack, err)
	}
	var rollbackRows int64
	if err := db.Table("system.engine_access_delegations").Where("tenant_membership_id = ?", rollbackRecipient.Membership.ID).Count(&rollbackRows).Error; err != nil || rollbackRows != 0 {
		t.Fatalf("rollback rows=%d err=%v", rollbackRows, err)
	}
	// Natural expiry is read-only history, not a background mutation.
	shortInput := rollbackInput
	shortInput.ExpiresAt = time.Now().Add(time.Second)
	short, err := service.Create(ctx, shortInput)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(time.Until(short.ExpiresAt) + 50*time.Millisecond)
	expired, err := service.Get(ctx, tenant.ID, int64(engine.ID), short.ID)
	if err != nil || expired.EffectiveState != "expired" || expired.Version != 1 {
		t.Fatalf("expired=%+v err=%v", expired, err)
	}
	if _, err := service.Revoke(ctx, engineaccess.RevokeInput{Actor: actor, EngineID: int64(engine.ID), ID: short.ID, Version: 1, Reason: "Too late"}); !errors.Is(err, engineaccess.ErrExpired) {
		t.Fatalf("expired revocation=%v", err)
	}
	// Qualification is checked against current IAM, not caller-supplied roles.
	noPermissionActor := actor
	noPermissionActor.PrincipalID, noPermissionActor.MembershipID, noPermissionActor.AuthorizationVersion = rolledBack.ID, rollbackRecipient.Membership.ID, rolledBack.AuthorizationVersion+1
	noPermissionInput := baseInput
	noPermissionInput.Actor = noPermissionActor
	if _, err := service.Create(ctx, noPermissionInput); !errors.Is(err, commonapi.ErrForbidden) {
		t.Fatalf("unqualified actor=%v", err)
	}
	if _, err := membershipService.SuspendMembership(ctx, iam.ChangeTenantMembershipInput{TenantID: tenant.ID, PrincipalID: recipientID, Reason: "Suspend test member"}); err != nil {
		t.Fatal(err)
	}
	unavailable, err = service.Get(ctx, tenant.ID, int64(engine.ID), renewed.ID)
	if err != nil || unavailable.EffectiveState != "unavailable" {
		t.Fatalf("suspended membership=%+v err=%v", unavailable, err)
	}
	if _, err := service.Create(ctx, baseInput); !errors.Is(err, engineaccess.ErrUnavailable) {
		t.Fatalf("suspended member creation=%v", err)
	}
	if _, err := service.Revoke(ctx, engineaccess.RevokeInput{Actor: actor, EngineID: int64(engine.ID), ID: renewed.ID, Version: 1, Reason: "End unavailable qualification"}); err != nil {
		t.Fatalf("unavailable recipient must still allow revocation: %v", err)
	}
	var finalAudits int64
	db.Table("system.audit_logs").Where("event_name LIKE 'system.engine_access_delegation.%'").Count(&finalAudits)
	if finalAudits != initialAudits+6 {
		t.Fatalf("audit count=%d want=%d", finalAudits, initialAudits+6)
	}
}

func engineDelegationTestRouter(t *testing.T, service engineAccessDelegationService, projection *commonauth.AuthContext) *gin.Engine {
	t.Helper()
	authentication, err := middleware.NewIAMAuthenticationMiddleware(iamActorResolver{authContext: projection})
	if err != nil {
		t.Fatal(err)
	}
	credential, err := middleware.NewIAMCredentialGuard(middleware.IAMTokenTypeFirstPartyAccess, middleware.IAMTokenTypeOAuthAccess)
	if err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	if err := RegisterEngineAccessDelegationRoutes(router.Group("/api/v1/system"), &IAMRuntime{Authentication: authentication, UserAccessCredential: credential}, NewEngineAccessDelegationHandler(service)); err != nil {
		t.Fatal(err)
	}
	return router
}
func engineDelegationTestRequest(t *testing.T, router http.Handler, method, path string, body any, status int) *httptest.ResponseRecorder {
	t.Helper()
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(method, path, bytes.NewReader(data))
	request.Header.Set("Authorization", "Bearer addp_at_fixture")
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != status {
		t.Fatalf("%s %s = %d want %d body=%s", method, path, response.Code, status, response.Body.String())
	}
	return response
}
