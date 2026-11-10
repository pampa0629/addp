package iam

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/client"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/execution"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/common/schema"
	"github.com/addp/system/internal/migration"
	"github.com/addp/system/internal/testsupport"
	"github.com/google/uuid"
	"github.com/lib/pq"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type developFileOwnerFixture map[string]*client.DevelopFileExecutionFacts

func (f developFileOwnerFixture) ReadFileExecution(_ context.Context, tenant uint, id string) (*client.DevelopFileExecutionFacts, error) {
	v := f[id]
	if v == nil || v.TenantID != strconv.FormatUint(uint64(tenant), 10) {
		return nil, commonapi.ErrNotFound
	}
	return v.Clone(), nil
}

// Real EA storage, credentials, roles, common claim, audits and migration.
// Owner and Meta/source-rule preparation are explicit test doubles. This is
// not GeoPackage admission, actual source-Grant acceptance, OAuth flow or T4.
func TestDevelopFileAuthorizationAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("ADDP_SYSTEM_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("standard System IAM PostgreSQL gate only")
	}
	testsupport.RequireDisposablePostgresDSN(t, dsn)
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	t.Cleanup(func() { _ = pool.Close() })
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	if err := db.Exec("DROP SCHEMA IF EXISTS system CASCADE; DROP SCHEMA IF EXISTS common CASCADE").Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Exec("DROP SCHEMA IF EXISTS common CASCADE; DROP SCHEMA IF EXISTS system CASCADE").Error; err != nil {
			t.Error(err)
		}
	})
	if err := migration.NewRunner(dsn).Run(ctx); err != nil {
		t.Fatal(err)
	}
	if err := schema.InitializeCommon(db); err != nil {
		t.Fatal(err)
	}
	repo := NewRepository(db)
	audit := AuditMetadata{RequestID: stringPointer("develop-file-test")}
	user := createContextSelectionUser(t, ctx, NewIdentityService(repo, nil), "develop-file-user", audit)
	members := NewTenantMembershipService(repo, testFixtureDatabaseClock(t, db))
	tenant := createContextSelectionTenant(t, ctx, members, "develop-file", audit)
	member := establishContextSelectionMembership(t, ctx, members, tenant.ID, user.PrincipalID, audit)
	now, err := repo.internalTaskDatabaseTime(ctx)
	if err != nil {
		t.Fatal(err)
	}
	insertRoleAssignment(t, db, user.PrincipalID, "tenant.data_engineer", "tenant", &tenant.ID, nil, nil, now.Add(-time.Minute), nil, "manual")
	approvalRole, err := NewTenantRoleService(repo, time.Now).CreateRole(ctx, CreateTenantRoleInput{
		TenantID: tenant.ID, RoleKey: "tenant.file_create_approver_fixture", Name: "File create approver fixture",
		PermissionKeys: []string{"system.develop_file_create_approval.create"}, ActorPrincipalID: user.PrincipalID, Audit: audit,
	})
	if err != nil {
		t.Fatal(err)
	}
	insertRoleAssignment(t, db, user.PrincipalID, approvalRole.RoleKey, "tenant", &tenant.ID, nil, nil, now.Add(-time.Minute), nil, "manual")
	var principal Principal
	if err := db.First(&principal, user.PrincipalID).Error; err != nil {
		t.Fatal(err)
	}
	tokens, err := NewTokenFamilyService(repo, BrowserSessionConfig{ResourceTicketOwners: []string{"manager"}}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	selection, err := NewContextSelectionService(repo, tokens)
	if err != nil {
		t.Fatal(err)
	}
	session, err := selection.BeginContextSelection(ctx, BeginContextSelectionInput{PrincipalID: user.PrincipalID, Authentication: SessionAuthentication{Methods: []string{"password"}, AssuranceLevel: AssuranceLevelAAL1, AuthenticatedAt: now}, Audit: audit})
	if err != nil || session.Session == nil {
		t.Fatal(session, err)
	}
	userToken := session.Session.AccessToken
	var runtimeID int64
	if err := db.Raw("SELECT id FROM system.service_principals WHERE name='addp-develop'").Scan(&runtimeID).Error; err != nil || runtimeID == 0 {
		t.Fatal(err)
	}
	runtimeMember := establishContextSelectionMembership(t, ctx, members, tenant.ID, runtimeID, audit)
	insertRoleAssignment(t, db, runtimeID, "tenant.develop_runtime", "tenant", &tenant.ID, nil, nil, now.Add(-time.Minute), nil, "bootstrap")
	hash, _ := bcrypt.GenerateFromPassword([]byte("develop-file-fixture-secret-32-bytes"), bcrypt.MinCost)
	if err := db.Exec("UPDATE system.oauth_clients SET client_secret_hash=?,status='active' WHERE client_id='addp-develop'", string(hash)).Error; err != nil {
		t.Fatal(err)
	}
	var runtimePrincipal Principal
	if err := db.First(&runtimePrincipal, runtimeID).Error; err != nil {
		t.Fatal(err)
	}
	protocol := uuid.New()
	family := &RefreshTokenFamily{ProtocolRequestID: &protocol, PrincipalID: runtimeID, ContextType: ContextTypeTenant, TenantMembershipID: &runtimeMember.Membership.ID, IssuedAuthorizationVersion: runtimePrincipal.AuthorizationVersion, ClientID: "addp-develop", AuthType: "oauth", Audiences: pq.StringArray{"addp.api"}, Scopes: pq.StringArray{"addp.api"}, AuthenticationMethods: pq.StringArray{"service_secret"}, AssuranceLevel: AssuranceLevelNotApplicable, AuthenticatedAt: now, ExpiresAt: now.Add(time.Hour), CreatedAt: now, UpdatedAt: now}
	if err := repo.CreateRefreshTokenFamily(ctx, family); err != nil {
		t.Fatal(err)
	}
	serviceToken := "addp_at_develop_file_fixture"
	serviceCredential := &AccessToken{TokenHash: hashOpaqueToken(serviceToken), FamilyID: family.ID, ExpiresAt: now.Add(30 * time.Minute), CreatedAt: now}
	if err := repo.CreateAccessToken(ctx, serviceCredential); err != nil {
		t.Fatal(err)
	}
	serviceType, tenantType := PrincipalTypeServicePrincipal, ContextTypeTenant
	serviceAudit := AuditMetadata{PrincipalID: &runtimeID, PrincipalType: &serviceType, ContextType: &tenantType, TenantID: &tenant.ID, RequestID: stringPointer("develop-file-service-test")}
	engineID := insertEngineFixture(t, db, tenant.ID, "Develop file NFS", "nfs", plugin.ConnectionInfo{"server": "fixture-nfs", "export_path": "/fixture"}, false)
	path := plugin.FileItemPath(uint(engineID), "sources/A.gpkg")
	owner := developFileOwnerFixture{}
	allowed := true
	targetSnapshots := 0
	var sourceExpiresAt *time.Time // Explicit source-rule double; never a real GPKG Grant.
	prepare := func(ctx context.Context, auth *ExecutionAuthorization, intent execution.DevelopFileIntent) (*DevelopFileReadPreparation, error) {
		version := ""
		if intent.Target != nil {
			version = "1"
		}
		scope, err := execution.NewDevelopBoundFileScope(intent, []execution.DevelopFileSourceReference{{Target: path, ItemFingerprint: commonmodels.GenerateItemFingerprint(uint(engineID), path.StringPath()), BoundaryHash: strings.Repeat("a", 64), EngineVersion: "1"}}, version)
		if err != nil {
			return nil, err
		}
		return &DevelopFileReadPreparation{Scope: *scope, Verify: func(ctx context.Context, tx *gorm.DB, current *ExecutionAuthorization, source *plugin.EngineCatalogPath) (*DevelopFileReadObservation, error) {
			if !allowed || !reflect.DeepEqual(current.DevelopFileScope, scope) {
				return nil, commonapi.ErrForbidden
			}
			var stamp time.Time
			if err := tx.Raw("SELECT clock_timestamp()").Scan(&stamp).Error; err != nil {
				return nil, err
			}
			out := &DevelopFileReadObservation{ObservedAt: stamp, ExpiresAt: sourceExpiresAt}
			if source != nil {
				if !reflect.DeepEqual(*source, path) {
					return nil, commonapi.ErrForbidden
				}
				tenantID := uint(tenant.ID)
				out.Engine = &commonmodels.Engine{ID: uint(engineID), TenantID: &tenantID, EngineType: "nfs", Version: 1, LifecycleState: "active", ConnectionInfo: commonmodels.ConnectionInfo{"server": "fixture-only"}}
			}
			return out, nil
		}, TargetSnapshot: func(context.Context, *gorm.DB) (*commonmodels.Engine, error) {
			targetSnapshots++
			tenantID := uint(tenant.ID)
			return &commonmodels.Engine{ID: uint(engineID), TenantID: &tenantID, EngineType: "nfs", Version: 1,
				LifecycleState: "active", ConnectionInfo: commonmodels.ConnectionInfo{"server": "fixture-target-only"}}, nil
		}}, nil
	}
	svc := NewDevelopFileAuthorizationService(repo, owner, prepare)
	newPending := func(t *testing.T, save bool) (*client.DevelopFileExecutionFacts, uuid.UUID) {
		t.Helper()
		id := uuid.New()
		by := int(user.PrincipalID)
		record := execution.TaskExecution{ExecutionID: id.String(), TenantID: int(tenant.ID), Module: "develop", Source: "develop", TaskType: "workflow", ExecutionBoundary: "bounded", Status: "pending", TriggerType: "manual", TriggeredBy: &by, ActorPrincipalID: &user.PrincipalID, ActorTenantMembershipID: &member.Membership.ID, IssuedAuthorizationVersion: &principal.AuthorizationVersion, MaxAttempts: 1, ExecutionConfig: commonmodels.JSONMap{"content": map[string]any{"workflow_definition": map[string]any{"name": "fixture-only"}}, "value": json.Number("9007199254740993"), "display_language": "en"}}
		if err := db.Create(&record).Error; err != nil {
			t.Fatal(err)
		}
		var stored struct{ ExecutionConfig json.RawMessage }
		if err := db.Raw("SELECT execution_config FROM common.task_executions WHERE execution_id=?", id.String()).Scan(&stored).Error; err != nil {
			t.Fatal(err)
		}
		raw := stored.ExecutionConfig
		var target *execution.DevelopFileTargetIntent
		if save {
			target = &execution.DevelopFileTargetIntent{NodeID: "save", Target: plugin.FileItemPath(uint(engineID), "outputs/result.gpkg"), Mode: "fail"}
		}
		intent, err := execution.NewDevelopFileIntent(raw, 1, 45, []execution.DevelopFileSourceIntent{{NodeID: "load", Target: path}}, target)
		if err != nil {
			t.Fatal(err)
		}
		facts := &client.DevelopFileExecutionFacts{SchemaVersion: client.DevelopFileExecutionSchema, ExecutionID: id.String(), TenantID: strconv.FormatInt(tenant.ID, 10), Actor: client.DevelopFileExecutionActor{PrincipalID: strconv.FormatInt(user.PrincipalID, 10), TenantMembershipID: strconv.FormatInt(member.Membership.ID, 10), AuthorizationVersion: strconv.FormatInt(principal.AuthorizationVersion, 10)}, ConfigJSON: raw, Intent: *intent, AdmissionState: "pending"}
		owner[id.String()] = facts
		return facts, id
	}
	issue := func(t *testing.T, save bool) (*client.DevelopFileExecutionFacts, *client.IssuedDevelopFileAuthorization, IssueDevelopFileAuthorizationInput) {
		t.Helper()
		facts, id := newPending(t, save)
		digest, _ := facts.Intent.Digest()
		input := IssueDevelopFileAuthorizationInput{SourceAccessToken: userToken, ExecutionID: id, IntentDigest: digest, Audit: audit}
		issued, err := svc.Issue(ctx, input)
		if err != nil {
			t.Fatal("issue", err)
		}
		facts.AuthorizationID = issued.Authorization.ID
		facts.BoundScope = issued.Scope.Clone()
		facts.ScopeDigest = issued.ScopeDigest
		authorizationID, _ := strconv.ParseInt(issued.Authorization.ID, 10, 64)
		if err := db.Table("common.task_executions").Where("execution_id=?", id.String()).Updates(map[string]any{"execution_authorization_id": authorizationID, "authorization_expires_at": issued.Authorization.ExpiresAt}).Error; err != nil {
			t.Fatal(err)
		}
		return facts, issued, input
	}
	t.Run("readonly original issuance admission claim and deadline", func(t *testing.T) {
		facts, issued, input := issue(t, false)
		recovered, err := svc.Issue(ctx, input)
		if err != nil || recovered.Authorization.ID != issued.Authorization.ID || !recovered.Authorization.ExpiresAt.Equal(issued.Authorization.ExpiresAt) {
			t.Fatal("recovery changed original authorization", err)
		}
		id, _ := strconv.ParseInt(issued.Authorization.ID, 10, 64)
		check := CheckDevelopFileInput{ServiceAccessToken: serviceToken, AuthorizationID: id, ExecutionID: input.ExecutionID, TenantID: tenant.ID, ExpectedAttempt: 1, ScopeDigest: issued.ScopeDigest, Audit: serviceAudit}
		admission, access, err := svc.Check(ctx, check)
		if err != nil || access != nil || admission == nil || admission.State != "ready" || admission.ApprovalID != "" {
			t.Fatal("admission", admission, err)
		}
		var pending ExecutionAuthorization
		if err := db.First(&pending, id).Error; err != nil || pending.DevelopFileBusinessDeadline != nil {
			t.Fatal("pending manufactured a business deadline", err)
		}
		if err := db.Model(&ExecutionAuthorization{}).Where("id=?", id).UpdateColumn("develop_file_business_deadline", time.Now().Add(time.Second)).Error; err == nil {
			t.Fatal("pending deadline write accepted")
		}
		_, lease, err := execution.ClaimPending(ctx, db, execution.PendingIdentity{ExecutionID: facts.ExecutionID, TenantID: int(tenant.ID), Module: "develop", TaskType: "workflow", Source: "develop"}, execution.ClaimOptions{Module: "develop", TaskType: "workflow", WorkerID: "develop-file-test", LeaseDuration: 10 * time.Second, RequireAuthorization: true}, nil)
		if err != nil || lease == nil {
			t.Fatal("claim", err)
		}
		if err := db.Exec("UPDATE common.task_executions SET started_at=started_at-interval '12 seconds' WHERE execution_id=?", facts.ExecutionID).Error; err != nil {
			t.Fatal(err)
		}
		facts.AdmissionState = "ready"
		check.LeaseToken = uuid.MustParse(lease.Token)
		check.Source = &path
		_, access, err = svc.Check(ctx, check)
		if err != nil || access == nil || access.Engine == nil {
			t.Fatal("source access", err)
		}
		var start time.Time
		if err := db.Raw("SELECT started_at FROM common.task_executions WHERE execution_id=?", facts.ExecutionID).Scan(&start).Error; err != nil {
			t.Fatal(err)
		}
		fixed := start.Add(45 * time.Second)
		if !access.BusinessDeadline.Equal(fixed) || !access.ExpiresAt.Before(fixed) {
			t.Fatal("business deadline conflated with short lease or renewed from now", access)
		}
		firstWindow := access.ExpiresAt
		if err := execution.RenewLease(ctx, db, *lease, time.Minute); err != nil {
			t.Fatal(err)
		}
		_, renewed, err := svc.Check(ctx, check)
		if err != nil || renewed == nil || !renewed.BusinessDeadline.Equal(fixed) || !renewed.ExpiresAt.After(firstWindow) {
			t.Fatal("renewal failed to preserve fixed business deadline", renewed, err)
		}
		var sealed ExecutionAuthorization
		if err := db.First(&sealed, id).Error; err != nil || sealed.DevelopFileBusinessDeadline == nil || !sealed.DevelopFileBusinessDeadline.Equal(fixed) {
			t.Fatal("business deadline was not sealed in original EA", err)
		}
		for _, value := range []any{nil, fixed.Add(time.Second), fixed.Add(-time.Second)} {
			if err := db.Model(&ExecutionAuthorization{}).Where("id=?", id).UpdateColumn("develop_file_business_deadline", value).Error; err == nil {
				t.Fatal("sealed business deadline changed", value)
			}
		}
		for _, mutate := range []func(*CheckDevelopFileInput){func(i *CheckDevelopFileInput) { i.ExpectedAttempt = 2 }, func(i *CheckDevelopFileInput) { i.LeaseToken = uuid.New() }, func(i *CheckDevelopFileInput) { i.TenantID++ }, func(i *CheckDevelopFileInput) { i.ScopeDigest = strings.Repeat("b", 64) }, func(i *CheckDevelopFileInput) {
			p := plugin.FileItemPath(uint(engineID), "sources/neighbor.gpkg")
			i.Source = &p
		}} {
			bad := check
			mutate(&bad)
			if a, b, err := svc.Check(ctx, bad); err == nil || a != nil || b != nil {
				t.Fatal("foreign binding or attempt accepted")
			}
		}
		allowed = false
		if _, access, err := svc.Check(ctx, check); err == nil || access != nil {
			t.Fatal("stale source observation accepted")
		}
		allowed = true
		if err := db.Table("common.task_executions").Where("execution_id=?", facts.ExecutionID).Update("execution_config", gorm.Expr("execution_config || '{\"changed\":true}'::jsonb")).Error; err != nil {
			t.Fatal(err)
		}
		if _, access, err := svc.Check(ctx, check); err == nil || access != nil {
			t.Fatal("changed common configuration accepted")
		}
		if err := db.Exec("UPDATE common.task_executions SET execution_config=CAST(? AS jsonb), started_at=clock_timestamp()-interval '46 seconds' WHERE execution_id=?", string(facts.ConfigJSON), facts.ExecutionID).Error; err != nil {
			t.Fatal(err)
		}
		if _, access, err := svc.Check(ctx, check); err == nil || access != nil {
			t.Fatal("expired original claim budget accepted")
		}
		if err := db.Exec("UPDATE system.execution_authorizations SET develop_file_scope=jsonb_set(develop_file_scope,'{intent_digest}',to_jsonb(?::text)) WHERE id=?", strings.Repeat("b", 64), id).Error; err == nil {
			t.Fatal("sealed scope mutation accepted")
		}
		generic, _ := NewExecutionAuthorizationService(repo)
		if value, err := generic.AuthorizeEngineAccess(ctx, AuthorizeExecutionEngineAccessInput{AuthorizationID: id, ExecutionID: input.ExecutionID, EngineID: engineID, RequiredEffects: []string{"read"}, ServicePrincipalID: runtimeID, ServiceClientID: "addp-develop", TenantID: tenant.ID, Audit: serviceAudit}); err == nil || value != nil {
			t.Fatal("scope downgraded to generic engine access")
		}
	})
	t.Run("save cannot fake publication approval", func(t *testing.T) {
		_, issued, input := issue(t, true)
		id, _ := strconv.ParseInt(issued.Authorization.ID, 10, 64)
		a, b, err := svc.Check(ctx, CheckDevelopFileInput{ServiceAccessToken: serviceToken, AuthorizationID: id, ExecutionID: input.ExecutionID, TenantID: tenant.ID, ExpectedAttempt: 1, ScopeDigest: issued.ScopeDigest, Audit: serviceAudit})
		if err != nil || a == nil || a.State != "awaiting_approval" || a.ApprovalID != "" || b != nil {
			t.Fatal("invented publication readiness", err)
		}
	})
	t.Run("human approval binds one original pending without activating it", func(t *testing.T) {
		approverCredential := func(principalID int64) string {
			t.Helper()
			current, err := repo.internalTaskDatabaseTime(ctx)
			if err != nil {
				t.Fatal(err)
			}
			selected, err := selection.BeginContextSelection(ctx, BeginContextSelectionInput{PrincipalID: principalID,
				Authentication: SessionAuthentication{Methods: []string{"password"}, AssuranceLevel: AssuranceLevelAAL1, AuthenticatedAt: current}, Audit: audit})
			if err != nil || selected.Session == nil {
				t.Fatal(selected, err)
			}
			return selected.Session.AccessToken
		}
		newApprover := func(name string, permitted bool) (string, int64) {
			t.Helper()
			person := createContextSelectionUser(t, ctx, NewIdentityService(repo, nil), name, audit)
			m := establishContextSelectionMembership(t, ctx, members, tenant.ID, person.PrincipalID, audit)
			if permitted {
				insertRoleAssignment(t, db, person.PrincipalID, approvalRole.RoleKey, "tenant", &tenant.ID, nil, nil, now.Add(-time.Minute), nil, "manual")
			}
			return approverCredential(person.PrincipalID), m.Membership.ID
		}
		first, firstMember := newApprover("file-create-approver-first", true)
		second, secondMember := newApprover("file-create-approver-second", true)
		noScope, _ := newApprover("file-create-approver-no-scope", true)
		noPermission, _ := newApprover("file-create-approver-no-permission", false)
		otherTenant := createContextSelectionTenant(t, ctx, members, "develop-file-foreign", audit)
		foreignPerson := createContextSelectionUser(t, ctx, NewIdentityService(repo, nil), "file-create-approver-foreign", audit)
		establishContextSelectionMembership(t, ctx, members, otherTenant.ID, foreignPerson.PrincipalID, audit)
		foreignRole, err := NewTenantRoleService(repo, time.Now).CreateRole(ctx, CreateTenantRoleInput{
			TenantID: otherTenant.ID, RoleKey: "tenant.foreign_file_approver", Name: "Foreign file create approver fixture",
			PermissionKeys: []string{"system.develop_file_create_approval.create"}, ActorPrincipalID: foreignPerson.PrincipalID, Audit: audit,
		})
		if err != nil {
			t.Fatal(err)
		}
		insertRoleAssignment(t, db, foreignPerson.PrincipalID, foreignRole.RoleKey, "tenant", &otherTenant.ID, nil, nil, now.Add(-time.Minute), nil, "manual")
		foreign := approverCredential(foreignPerson.PrincipalID)
		facts, issued, input := issue(t, true)
		id, _ := strconv.ParseInt(issued.Authorization.ID, 10, 64)
		allowManagement := true
		var managementExpiry *time.Time
		svc.SetCreateApproverPreparer(func(_ context.Context, _ *gorm.DB, gotTenant, gotMembership int64, target execution.DevelopBoundFileTarget) (func(time.Time) (*time.Time, error), error) {
			if gotTenant != tenant.ID || (gotMembership != firstMember && gotMembership != secondMember) ||
				!reflect.DeepEqual(target, *issued.Scope.Target) || !allowManagement {
				return nil, commonapi.ErrForbidden
			}
			return func(time.Time) (*time.Time, error) { return managementExpiry, nil }, nil
		})
		req := ApproveDevelopFileCreateInput{AuthorizationID: id, ScopeDigest: issued.ScopeDigest,
			ExpiresAt: issued.Authorization.ExpiresAt.Add(-time.Second), Audit: audit}
		for _, token := range []string{userToken, serviceToken, noPermission, noScope, foreign} {
			req.UserAccessToken = token
			if value, err := svc.ApproveCreate(ctx, req); err == nil || value != nil {
				t.Fatal("unauthorized approval was recorded")
			}
		}
		req.UserAccessToken = first
		for _, expiry := range []time.Time{now.Add(-time.Minute), issued.Authorization.ExpiresAt.Add(time.Second)} {
			bad := req
			bad.ExpiresAt = expiry
			if value, err := svc.ApproveCreate(ctx, bad); err == nil || value != nil {
				t.Fatal("invalid expiry was approved")
			}
		}
		bad := req
		bad.ScopeDigest = strings.Repeat("f", 64)
		if value, err := svc.ApproveCreate(ctx, bad); err == nil || value != nil {
			t.Fatal("different scope was approved")
		}
		type result struct {
			value *DevelopFileCreateApprovalReceipt
			err   error
		}
		results := make(chan result, 2)
		for _, token := range []string{first, second} {
			proposed := req
			proposed.UserAccessToken = token
			go func() {
				value, err := svc.ApproveCreate(ctx, proposed)
				results <- result{value, err}
			}()
		}
		var winner *DevelopFileCreateApprovalReceipt
		for i := 0; i < 2; i++ {
			got := <-results
			if got.err == nil && got.value != nil && winner == nil {
				winner = got.value
			} else if !errors.Is(got.err, ErrExecutionAuthorizationConflict) || got.value != nil {
				t.Fatal("approval uniqueness failed", got.err)
			}
		}
		var persisted developFileCreateApproval
		if err := db.Where("authorization_id=?", id).Take(&persisted).Error; err != nil || winner == nil ||
			persisted.ID.String() != winner.ApprovalID || persisted.ScopeDigest != issued.ScopeDigest || !persisted.ExpiresAt.Equal(req.ExpiresAt) {
			t.Fatal("original approval missing", err)
		}
		var count int64
		if err := db.Model(&AuditLog{}).Where("event_name=? AND entity_id=?", "iam.develop_file_create_approval.created", persisted.ID.String()).Count(&count).Error; err != nil || count != 1 {
			t.Fatal("approval audit missing", err, count)
		}
		check := CheckDevelopFileInput{ServiceAccessToken: serviceToken, AuthorizationID: id,
			ExecutionID: input.ExecutionID, TenantID: tenant.ID, ExpectedAttempt: 1, ScopeDigest: issued.ScopeDigest, Audit: serviceAudit}
		allowManagement = false
		a, b, err := svc.Check(ctx, check)
		if err != nil || a == nil || a.State != "rejected" || a.ApprovalID != "" || b != nil {
			t.Fatal("lost management scope reported ready", a, err)
		}
		allowManagement = true
		current, err := repo.internalTaskDatabaseTime(ctx)
		if err != nil {
			t.Fatal(err)
		}
		managementDeadline := current.Add(20 * time.Second)
		managementExpiry = &managementDeadline
		a, b, err = svc.Check(ctx, check)
		var stillPending execution.TaskExecution
		if dbErr := db.Where("execution_id=?", facts.ExecutionID).Take(&stillPending).Error; dbErr != nil {
			t.Fatal(dbErr)
		}
		if err != nil || a == nil || a.State != "ready" || a.ApprovalID != winner.ApprovalID || a.ExpiresAt.After(winner.ExpiresAt) ||
			!a.ExpiresAt.Equal(managementDeadline) || b != nil || stillPending.Status != "pending" || stillPending.StartedAt != nil {
			t.Fatal("approval observation changed the original execution or omitted the approval", a, err)
		}
		_, lease, err := execution.ClaimPending(ctx, db, execution.PendingIdentity{ExecutionID: facts.ExecutionID, TenantID: int(tenant.ID), Module: "develop", TaskType: "workflow", Source: "develop"},
			execution.ClaimOptions{Module: "develop", TaskType: "workflow", WorkerID: "approved-file-test", LeaseDuration: time.Minute, RequireAuthorization: true}, nil)
		if err != nil || lease == nil {
			t.Fatal("fixture claim failed", err)
		}
		facts.AdmissionState = "ready"
		check.Source, check.LeaseToken = &path, uuid.New()
		if a, b, err := svc.Check(ctx, check); err == nil || a != nil || b != nil {
			t.Fatal("invalid lease used the approval")
		}
		check.LeaseToken = uuid.MustParse(lease.Token)
		operation := uuid.NewString()
		publicationCheck := check
		publicationCheck.Source = nil
		request := client.DevelopFilePublicationRequest{ExecutionID: facts.ExecutionID, Attempt: 1, LeaseToken: lease.Token,
			ScopeDigest: issued.ScopeDigest, ApprovalID: winner.ApprovalID, OperationID: operation, Stage: client.DevelopFilePublicationPrepare}
		facts.Publication = &client.DevelopFilePublicationFacts{ApprovalID: winner.ApprovalID,
			Request: plugin.NewFilePublicationRequest{OperationID: operation, Target: issued.Scope.Target.Target,
				SizeBytes: 8192, SHA256: strings.Repeat("a", 64)}}
		if value, err := svc.AuthorizePublication(ctx, publicationCheck, request); err == nil || value != nil || targetSnapshots != 0 {
			t.Fatal("target preparation created the source-read deadline")
		}
		if a, b, err := svc.Check(ctx, check); err != nil || a != nil || b == nil || b.Engine == nil || b.Engine.ID != uint(engineID) ||
			!b.BusinessDeadline.Equal(managementDeadline) || b.ExpiresAt.After(managementDeadline) {
			t.Fatal("approved running read did not return the exact source", err)
		}
		release := request
		release.Stage, release.PreparationDigest = client.DevelopFilePublicationRelease, strings.Repeat("c", 64)
		facts.Publication.PreparationDigest = release.PreparationDigest
		if value, err := svc.AuthorizePublication(ctx, publicationCheck, release); err == nil || value != nil || targetSnapshots != 0 {
			t.Fatal("release worked before preparation admission")
		}
		facts.Publication.PreparationDigest = ""
		badRequest := request
		badRequest.OperationID = uuid.NewString()
		if value, err := svc.AuthorizePublication(ctx, publicationCheck, badRequest); err == nil || value != nil {
			t.Fatal("unregistered operation obtained target connection")
		}
		staleCheck, staleRequest := publicationCheck, request
		staleCheck.LeaseToken = uuid.New()
		staleRequest.LeaseToken = staleCheck.LeaseToken.String()
		if value, err := svc.AuthorizePublication(ctx, staleCheck, staleRequest); err == nil || value != nil || targetSnapshots != 0 {
			t.Fatal("wrong lease obtained the target connection")
		}
		prepareReceipt, err := svc.AuthorizePublication(ctx, publicationCheck, request)
		if err != nil || prepareReceipt == nil || prepareReceipt.Engine == nil || prepareReceipt.Engine.ConnectionInfo["server"] != "fixture-target-only" ||
			targetSnapshots != 1 || !prepareReceipt.BusinessDeadline.Equal(managementDeadline) {
			t.Fatal("exact preparation failed", err)
		}
		if value, err := svc.AuthorizePublication(ctx, publicationCheck, request); !errors.Is(err, ErrExecutionAuthorizationConflict) || value != nil || targetSnapshots != 1 {
			t.Fatal("lost preparation response could be replayed", err)
		}
		if value, err := svc.AuthorizePublication(ctx, publicationCheck, release); err == nil || value != nil {
			t.Fatal("release accepted without a trusted prepared receipt")
		}
		facts.Publication.PreparationDigest = release.PreparationDigest
		facts.Publication.Request.SizeBytes++
		if value, err := svc.AuthorizePublication(ctx, publicationCheck, release); !errors.Is(err, ErrExecutionAuthorizationConflict) || value != nil {
			t.Fatal("changed output bytes released the original operation", err)
		}
		facts.Publication.Request.SizeBytes--
		allowManagement = false
		if value, err := svc.AuthorizePublication(ctx, publicationCheck, release); err == nil || value != nil {
			t.Fatal("revoked management scope released the operation")
		}
		allowManagement = true
		type publicationResult struct {
			value *client.DevelopFilePublicationObservation
			err   error
		}
		releases := make(chan publicationResult, 2)
		for i := 0; i < 2; i++ {
			go func() {
				value, err := svc.AuthorizePublication(ctx, publicationCheck, release)
				releases <- publicationResult{value, err}
			}()
		}
		successes := 0
		for i := 0; i < 2; i++ {
			result := <-releases
			if result.err == nil && result.value != nil && result.value.Engine == nil && result.value.PreparationDigest == release.PreparationDigest {
				successes++
			} else if !errors.Is(result.err, ErrExecutionAuthorizationConflict) || result.value != nil {
				t.Fatal("invalid concurrent release result", result.err)
			}
		}
		if successes != 1 || targetSnapshots != 1 {
			t.Fatal("release was not unique or returned another connection", successes)
		}
		allowManagement = false
		if a, b, err := svc.Check(ctx, check); err == nil || a != nil || b != nil {
			t.Fatal("running read reused a stale approval scope")
		}
		allowManagement = true
		if err := db.Model(&developFileCreateApproval{}).Where("id=?", persisted.ID).
			Updates(map[string]any{"approved_at": now.Add(-time.Minute), "expires_at": now.Add(-time.Second)}).Error; err != nil {
			t.Fatal(err)
		}
		if a, b, err := svc.Check(ctx, check); err == nil || a != nil || b != nil {
			t.Fatal("expired original approval returned a source connection")
		}
		if err := db.Model(&developFileCreateApproval{}).Where("id=?", persisted.ID).
			Updates(map[string]any{"approved_at": persisted.ApprovedAt, "expires_at": persisted.ExpiresAt}).Error; err != nil {
			t.Fatal(err)
		}
		if _, err := repo.IncrementPrincipalAuthorizationVersion(ctx, persisted.ApproverPrincipalID); err != nil {
			t.Fatal(err)
		}
		if a, b, err := svc.Check(ctx, check); err == nil || a != nil || b != nil {
			t.Fatal("invalidated approver identity returned a source connection")
		}
	})
	t.Run("wrong owner actor and rejected before issuance", func(t *testing.T) {
		facts, id := newPending(t, false)
		digest, _ := facts.Intent.Digest()
		input := IssueDevelopFileAuthorizationInput{SourceAccessToken: userToken, ExecutionID: id, IntentDigest: digest, Audit: audit}
		original := facts.Actor.PrincipalID
		facts.Actor.PrincipalID = "999999"
		if value, err := svc.Issue(ctx, input); err == nil || value != nil {
			t.Fatal("wrong Owner actor accepted")
		}
		facts.Actor.PrincipalID = original
		facts.AdmissionState = "rejected"
		if value, err := svc.Issue(ctx, input); err == nil || value != nil {
			t.Fatal("rejected unissued execution accepted")
		}
	})
	for _, bound := range []string{"ea", "role", "membership"} {
		t.Run("earlier "+bound+" bounds fixed business deadline", func(t *testing.T) {
			originalUser, originalMember, originalPrincipal, originalToken := user, member, principal, userToken
			defer func() {
				user, member, principal, userToken = originalUser, originalMember, originalPrincipal, originalToken
			}()
			stamp, err := repo.internalTaskDatabaseTime(ctx)
			if err != nil {
				t.Fatal(err)
			}
			cap := stamp.Add(5 * time.Minute)
			var membershipExpiry, roleExpiry *time.Time
			if bound == "role" {
				roleExpiry = &cap
			}
			if bound == "membership" {
				membershipExpiry = &cap
			}
			user = createContextSelectionUser(t, ctx, NewIdentityService(repo, nil), "deadline-"+bound, audit)
			member, err = members.EstablishMembership(ctx, EstablishTenantMembershipInput{TenantID: tenant.ID, PrincipalID: user.PrincipalID, SourceType: TenantMembershipSourceManual, CreatedByPrincipalID: &user.PrincipalID, ExpiresAt: membershipExpiry, Audit: audit})
			if err != nil {
				t.Fatal(err)
			}
			insertRoleAssignment(t, db, user.PrincipalID, "tenant.data_engineer", "tenant", &tenant.ID, nil, nil, stamp.Add(-time.Minute), roleExpiry, "manual")
			principal = Principal{}
			if err := db.First(&principal, user.PrincipalID).Error; err != nil {
				t.Fatal(err)
			}
			selected, err := selection.BeginContextSelection(ctx, BeginContextSelectionInput{PrincipalID: user.PrincipalID, Authentication: SessionAuthentication{Methods: []string{"password"}, AssuranceLevel: AssuranceLevelAAL1, AuthenticatedAt: stamp}, Audit: audit})
			if err != nil || selected.Session == nil {
				t.Fatal("select bounded user", err)
			}
			userToken = selected.Session.AccessToken
			facts, executionID := newPending(t, false)
			facts.Intent.TimeoutSeconds = 3600
			digest, _ := facts.Intent.Digest()
			issued, err := svc.Issue(ctx, IssueDevelopFileAuthorizationInput{SourceAccessToken: userToken, ExecutionID: executionID, IntentDigest: digest, Audit: audit})
			if err != nil {
				t.Fatal("issue bounded user", err)
			}
			id, _ := strconv.ParseInt(issued.Authorization.ID, 10, 64)
			facts.AuthorizationID = issued.Authorization.ID
			facts.BoundScope = issued.Scope.Clone()
			facts.ScopeDigest = issued.ScopeDigest
			facts.AdmissionState = "ready"
			if err := db.Table("common.task_executions").Where("execution_id=?", facts.ExecutionID).Updates(map[string]any{"execution_authorization_id": id, "authorization_expires_at": issued.Authorization.ExpiresAt}).Error; err != nil {
				t.Fatal(err)
			}
			_, lease, err := execution.ClaimPending(ctx, db, execution.PendingIdentity{ExecutionID: facts.ExecutionID, TenantID: int(tenant.ID), Module: "develop", TaskType: "workflow", Source: "develop"}, execution.ClaimOptions{Module: "develop", TaskType: "workflow", WorkerID: "deadline-person-test", LeaseDuration: time.Minute, RequireAuthorization: true}, nil)
			if err != nil || lease == nil {
				t.Fatal(err)
			}
			_, access, err := svc.Check(ctx, CheckDevelopFileInput{ServiceAccessToken: serviceToken, AuthorizationID: id, ExecutionID: executionID, TenantID: tenant.ID, ExpectedAttempt: 1, ScopeDigest: issued.ScopeDigest, LeaseToken: uuid.MustParse(lease.Token), Source: &path, Audit: serviceAudit})
			if bound == "ea" {
				cap = issued.Authorization.ExpiresAt
			}
			if err != nil || access == nil || !access.BusinessDeadline.Equal(cap) || !access.ExpiresAt.Before(cap) {
				t.Fatal("earlier human/EA cap ignored", bound, access, err)
			}
		})
	}
	t.Run("earlier source eligibility is sealed and later renewal cannot extend it", func(t *testing.T) {
		stamp, err := repo.internalTaskDatabaseTime(ctx)
		if err != nil {
			t.Fatal(err)
		}
		cap := stamp.Add(30 * time.Second)
		sourceExpiresAt = &cap
		defer func() { sourceExpiresAt = nil }()
		facts, issued, input := issue(t, false)
		id, _ := strconv.ParseInt(issued.Authorization.ID, 10, 64)
		_, lease, err := execution.ClaimPending(ctx, db, execution.PendingIdentity{ExecutionID: facts.ExecutionID, TenantID: int(tenant.ID), Module: "develop", TaskType: "workflow", Source: "develop"}, execution.ClaimOptions{Module: "develop", TaskType: "workflow", WorkerID: "deadline-cap-test", LeaseDuration: time.Minute, RequireAuthorization: true}, nil)
		if err != nil || lease == nil {
			t.Fatal(err)
		}
		facts.AdmissionState = "ready"
		check := CheckDevelopFileInput{ServiceAccessToken: serviceToken, AuthorizationID: id, ExecutionID: input.ExecutionID, TenantID: tenant.ID, ExpectedAttempt: 1, ScopeDigest: issued.ScopeDigest, LeaseToken: uuid.MustParse(lease.Token), Source: &path, Audit: serviceAudit}
		shortBearer := "addp_at_short_develop_file_deadline_fixture"
		shortFamily := *family
		shortFamily.ID = 0
		shortProtocol := uuid.New()
		shortFamily.ProtocolRequestID = &shortProtocol
		if err := repo.CreateRefreshTokenFamily(ctx, &shortFamily); err != nil {
			t.Fatal(err)
		}
		shortToken := &AccessToken{TokenHash: hashOpaqueToken(shortBearer), FamilyID: shortFamily.ID, ExpiresAt: stamp.Add(5 * time.Second), CreatedAt: stamp}
		if err := repo.CreateAccessToken(ctx, shortToken); err != nil {
			t.Fatal(err)
		}
		check.ServiceAccessToken = shortBearer
		_, first, err := svc.Check(ctx, check)
		if err != nil || first == nil || !first.BusinessDeadline.Equal(cap) || !first.ExpiresAt.Equal(shortToken.ExpiresAt) {
			t.Fatal("source cap not sealed", first, err)
		}
		sourceExpiresAt = nil
		check.ServiceAccessToken = serviceToken
		_, renewed, err := svc.Check(ctx, check)
		if err != nil || renewed == nil || !renewed.BusinessDeadline.Equal(cap) {
			t.Fatal("source renewal extended original deadline", renewed, err)
		}
		shorter := cap.Add(-time.Second)
		sourceExpiresAt = &shorter
		_, narrowed, err := svc.Check(ctx, check)
		if err != nil || narrowed == nil || !narrowed.BusinessDeadline.Equal(cap) || narrowed.ExpiresAt.After(shorter) {
			t.Fatal("current shorter source eligibility ignored", narrowed, err)
		}
		if err := db.Model(&ExecutionAuthorization{}).Where("id=?", id).Updates(map[string]any{"revoked_at": gorm.Expr("clock_timestamp()"), "revoked_reason": "test revocation"}).Error; err != nil {
			t.Fatal("fixed deadline prevented revocation", err)
		}
		if a, b, err := svc.Check(ctx, check); err == nil || a != nil || b != nil {
			t.Fatal("revoked EA accepted")
		}
	})
	t.Run("deadline crossed during audit returns no connection", func(t *testing.T) {
		facts, id := newPending(t, false)
		facts.Intent.TimeoutSeconds = 1
		digest, _ := facts.Intent.Digest()
		issued, err := svc.Issue(ctx, IssueDevelopFileAuthorizationInput{SourceAccessToken: userToken, ExecutionID: id, IntentDigest: digest, Audit: audit})
		if err != nil {
			t.Fatal(err)
		}
		authID, _ := strconv.ParseInt(issued.Authorization.ID, 10, 64)
		facts.AuthorizationID = issued.Authorization.ID
		facts.BoundScope = issued.Scope.Clone()
		facts.ScopeDigest = issued.ScopeDigest
		facts.AdmissionState = "ready"
		if err := db.Table("common.task_executions").Where("execution_id=?", id.String()).Updates(map[string]any{"execution_authorization_id": authID, "authorization_expires_at": issued.Authorization.ExpiresAt}).Error; err != nil {
			t.Fatal(err)
		}
		_, lease, err := execution.ClaimPending(ctx, db, execution.PendingIdentity{ExecutionID: id.String(), TenantID: int(tenant.ID), Module: "develop", TaskType: "workflow", Source: "develop"}, execution.ClaimOptions{Module: "develop", TaskType: "workflow", WorkerID: "develop-file-audit-test", LeaseDuration: time.Minute, RequireAuthorization: true}, nil)
		if err != nil || lease == nil {
			t.Fatal(err)
		}
		const callback = "fixture:develop-file-deadline-after-audit"
		reached := false
		if err := db.Callback().Create().After("gorm:create").Register(callback, func(tx *gorm.DB) {
			if log, ok := tx.Statement.Dest.(*AuditLog); ok && log.EventName == "iam.execution_authorization.consumed" && log.EntityID != nil && *log.EntityID == issued.Authorization.ID {
				reached = tx.Error == nil
				tx.AddError(tx.Session(&gorm.Session{NewDB: true}).Exec("SELECT pg_sleep(GREATEST(EXTRACT(EPOCH FROM (started_at+interval '1 second'-clock_timestamp()))+0.03,0)) FROM common.task_executions WHERE execution_id=?", id.String()).Error)
			}
		}); err != nil {
			t.Fatal(err)
		}
		defer db.Callback().Create().Remove(callback)
		a, b, err := svc.Check(ctx, CheckDevelopFileInput{ServiceAccessToken: serviceToken, AuthorizationID: authID, ExecutionID: id, TenantID: tenant.ID, ExpectedAttempt: 1, ScopeDigest: issued.ScopeDigest, LeaseToken: uuid.MustParse(lease.Token), Source: &path, Audit: serviceAudit})
		if !reached || !errors.Is(err, ErrExecutionAuthorizationUnavailable) || a != nil || b != nil {
			t.Fatal("audit wait admitted expired connection", reached, err)
		}
	})
	t.Run("current service credential revocation", func(t *testing.T) {
		_, issued, input := issue(t, false)
		id, _ := strconv.ParseInt(issued.Authorization.ID, 10, 64)
		stamp, err := repo.internalTaskDatabaseTime(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if err := db.Model(&AccessToken{}).Where("id=?", serviceCredential.ID).Updates(map[string]any{"revoked_at": stamp}).Error; err != nil {
			t.Fatal(err)
		}
		a, b, err := svc.Check(ctx, CheckDevelopFileInput{ServiceAccessToken: serviceToken, AuthorizationID: id, ExecutionID: input.ExecutionID, TenantID: tenant.ID, ExpectedAttempt: 1, ScopeDigest: issued.ScopeDigest, Audit: serviceAudit})
		if err == nil || a != nil || b != nil {
			t.Fatal("revoked machine credential accepted")
		}
	})
}
