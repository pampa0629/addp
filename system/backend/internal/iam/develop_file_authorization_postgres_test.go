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
			out := &DevelopFileReadObservation{ObservedAt: stamp}
			if source != nil {
				if !reflect.DeepEqual(*source, path) {
					return nil, commonapi.ErrForbidden
				}
				tenantID := uint(tenant.ID)
				out.Engine = &commonmodels.Engine{ID: uint(engineID), TenantID: &tenantID, EngineType: "nfs", Version: 1, LifecycleState: "active", ConnectionInfo: commonmodels.ConnectionInfo{"server": "fixture-only"}}
			}
			return out, nil
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
		_, lease, err := execution.ClaimPending(ctx, db, execution.PendingIdentity{ExecutionID: facts.ExecutionID, TenantID: int(tenant.ID), Module: "develop", TaskType: "workflow", Source: "develop"}, execution.ClaimOptions{Module: "develop", TaskType: "workflow", WorkerID: "develop-file-test", LeaseDuration: time.Minute, RequireAuthorization: true}, nil)
		if err != nil || lease == nil {
			t.Fatal("claim", err)
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
		if access.ExpiresAt.After(start.Add(45 * time.Second)) {
			t.Fatal("deadline renewed from now")
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
		if !errors.Is(err, ErrDevelopFileDependencyUnavailable) || a != nil || b != nil {
			t.Fatal("invented publication readiness", err)
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
