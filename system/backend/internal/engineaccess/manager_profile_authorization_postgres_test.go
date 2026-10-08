package engineaccess

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/execution"
	"github.com/addp/common/models"
	"github.com/addp/system/internal/iam"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// Invoked by the existing formal Grant fixture. All identity, role, Grant,
// restriction and revocation facts use their production owner services.
func exerciseManagerProfileAuthorization(t *testing.T, db *gorm.DB, path plugin.EngineCatalogPath,
	newUser func(*testing.T, time.Duration) (userProvenance, time.Time), grant func(*testing.T, string, int64, *time.Time) uuid.UUID,
	roles *iam.TenantRoleService, tenantID, adminID int64, deny func(*testing.T, string, int64, *time.Time) uuid.UUID, revoke func(*testing.T, uuid.UUID),
) {
	t.Run("Manager complete source execution authorization", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
		defer cancel()
		repo := iam.NewRepository(db)
		service := iam.NewManagerProfileAuthorizationService(repo, VerifyExecutionSourceRead)
		user, _ := newUser(t, time.Hour)
		role, err := roles.CreateRole(ctx, iam.CreateTenantRoleInput{TenantID: tenantID, RoleKey: "custom.profile_source", Name: "Explicit profile functions",
			ScopeTypes: []string{"tenant"}, PermissionKeys: []string{"manager.data_profile.execute", "manager.data_item.read"}, ActorPrincipalID: adminID})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := roles.CreateAssignments(ctx, iam.CreateTenantRoleAssignmentsInput{TenantID: tenantID, MembershipID: user.MembershipID,
			RoleIDs: []int64{role.ID}, ScopeType: "tenant", ActorPrincipalID: adminID, Reason: "Explicit execution fixture"}); err != nil {
			t.Fatal(err)
		}
		p, err := repo.GetPrincipal(ctx, user.PrincipalID)
		if err != nil {
			t.Fatal(err)
		}
		user.AuthorizationVersion = p.AuthorizationVersion
		tokens, err := iam.NewTokenFamilyService(repo, iam.BrowserSessionConfig{ResourceTicketOwners: []string{"manager"}}, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		selection, err := iam.NewContextSelectionService(repo, tokens)
		if err != nil {
			t.Fatal(err)
		}
		// Browser authentication uses the database wall clock. The host and
		// Docker VM clocks need not coincide, even for an immediately used value.
		var authenticatedAt time.Time
		if err := db.WithContext(ctx).Raw("SELECT clock_timestamp()").Scan(&authenticatedAt).Error; err != nil {
			t.Fatal(err)
		}
		choice, err := selection.BeginContextSelection(ctx, iam.BeginContextSelectionInput{PrincipalID: user.PrincipalID,
			Authentication: iam.SessionAuthentication{Methods: []string{"password"}, AssuranceLevel: iam.AssuranceLevelAAL1, AuthenticatedAt: authenticatedAt}})
		if err != nil {
			t.Fatal(err)
		}
		session := choice.Session
		if choice.Challenge != nil {
			session, err = selection.ConsumeContextSelection(ctx, iam.ConsumeContextSelectionInput{SelectionTicket: choice.Challenge.SelectionTicket,
				Choice: iam.ContextSelectionChoice{Type: iam.ContextTypeTenant, TenantMembershipID: &user.MembershipID}})
			if err != nil {
				t.Fatal(err)
			}
		}
		if session == nil {
			t.Fatal("no real User session")
		}
		var runtimeID int64
		if err := db.Raw("SELECT id FROM system.service_principals WHERE name='addp-manager'").Scan(&runtimeID).Error; err != nil || runtimeID == 0 {
			t.Fatalf("runtime: %v", err)
		}
		// The formal Tenant fixture already initializes built-in Runtime
		// membership and assignments. Do not manufacture a second relation.
		var member iam.TenantMembership
		if err := db.Where("tenant_id=? AND principal_id=?", tenantID, runtimeID).Take(&member).Error; err != nil {
			t.Fatal(err)
		}
		hash, err := bcrypt.GenerateFromPassword([]byte("disposable-profile-execution-client"), bcrypt.MinCost)
		if err != nil {
			t.Fatal(err)
		}
		if err := db.Exec("UPDATE system.oauth_clients SET client_secret_hash=?,status='active' WHERE client_id='addp-manager'", string(hash)).Error; err != nil {
			t.Fatal(err)
		}
		principalType, contextType := iam.PrincipalTypeServicePrincipal, iam.ContextTypeTenant
		runtimeAudit := iam.AuditMetadata{PrincipalID: &runtimeID, PrincipalType: &principalType, ContextType: &contextType, TenantID: &tenantID}
		readSet, err := plugin.NewQueryReadSet(path)
		if err != nil {
			t.Fatal(err)
		}
		create := func(readSet *plugin.QueryReadSet) *execution.TaskExecution {
			t.Helper()
			job := &execution.TaskExecution{ExecutionID: uuid.NewString(), TenantID: int(tenantID), Module: "manager", Source: "manager", TaskType: "data_profiling",
				Status: execution.ExecutionStatusPending, TriggerType: execution.TriggerTypeManual, ExecutionBoundary: execution.ExecutionBoundaryBounded, MaxAttempts: 3,
				ActorPrincipalID: &user.PrincipalID, ActorTenantMembershipID: &user.MembershipID, IssuedAuthorizationVersion: &user.AuthorizationVersion,
				ExecutionConfig: models.JSONMap{"config_version": "data-profile-config/v6", "engine_id": path.EngineID, "read_set": readSet,
					"budget": map[string]any{"sample_size": 10, "max_rows_scanned": 10, "page_size": 10, "timeout_ms": 1000}}, Metadata: models.JSONMap{}, ErrorDetails: models.JSONMap{}}
			if err := execution.NewTaskExecutionRepository(db).Create(ctx, job); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := db.Where("execution_id=?", job.ExecutionID).Delete(&execution.TaskExecution{}).Error; err != nil {
					t.Error(err)
				}
			})
			return job
		}
		issue := func(job *execution.TaskExecution) (*iam.IssuedManagerProfileAuthorization, error) {
			return service.Issue(ctx, iam.IssueManagerProfileAuthorizationInput{SourceAccessToken: session.AccessToken, ExecutionID: uuid.MustParse(job.ExecutionID)})
		}
		job := create(readSet)
		if result, err := issue(job); result != nil || !errors.Is(err, commonapi.ErrForbidden) {
			t.Fatalf("function alone allowed: %+v %v", result, err)
		}
		grantID := grant(t, "user", user.PrincipalID, nil)
		t.Run("audit failure rolls back issuance", func(t *testing.T) {
			pending := create(readSet)
			const callback = "fixture:manager_profile_issue_audit_failure"
			reached := false
			if err := db.Callback().Create().After("gorm:create").Register(callback, func(tx *gorm.DB) {
				if log, ok := tx.Statement.Dest.(*iam.AuditLog); ok && log.EventName == "iam.execution_authorization.issued" {
					reached = tx.Error == nil
					tx.AddError(errors.New("injected Manager issuance audit failure"))
				}
			}); err != nil {
				t.Fatal(err)
			}
			defer db.Callback().Create().Remove(callback)
			if result, err := issue(pending); result != nil || err == nil || !reached {
				t.Fatalf("audit failure issuance=%+v reached=%v err=%v", result, reached, err)
			}
			var count int64
			if err := db.Model(&iam.ExecutionAuthorization{}).Where("execution_id=?", pending.ExecutionID).Count(&count).Error; err != nil || count != 0 {
				t.Fatalf("rolled back authorization count=%d err=%v", count, err)
			}
		})
		t.Run("expiry during audit rolls back issuance", func(t *testing.T) {
			pending := create(readSet)
			const callback = "fixture:manager_profile_issue_audit_expiry"
			reached := false
			if err := db.Callback().Create().After("gorm:create").Register(callback, func(tx *gorm.DB) {
				if log, ok := tx.Statement.Dest.(*iam.AuditLog); ok && log.EventName == "iam.execution_authorization.issued" {
					reached = tx.Error == nil
					tx.AddError(tx.WithContext(ctx).Session(&gorm.Session{NewDB: true}).Exec(`SELECT pg_sleep(GREATEST(
						EXTRACT(EPOCH FROM (expires_at-clock_timestamp()))+0.03,0)) FROM system.execution_authorizations WHERE id=?`, log.EntityID).Error)
				}
			}); err != nil {
				t.Fatal(err)
			}
			defer db.Callback().Create().Remove(callback)
			result, err := service.Issue(ctx, iam.IssueManagerProfileAuthorizationInput{SourceAccessToken: session.AccessToken,
				ExecutionID: uuid.MustParse(pending.ExecutionID), ExpiresIn: time.Second})
			if result != nil || !reached || !errors.Is(err, iam.ErrExecutionAuthorizationUnavailable) {
				t.Fatalf("expired issuance=%+v reached=%v err=%v", result, reached, err)
			}
			var count int64
			if err := db.Model(&iam.ExecutionAuthorization{}).Where("execution_id=?", pending.ExecutionID).Count(&count).Error; err != nil || count != 0 {
				t.Fatalf("expired authorization count=%d err=%v", count, err)
			}
		})
		mixed, _ := plugin.NewQueryReadSet(path, plugin.TabularItemPath(path.EngineID, "schema", "public", "ungranted_profile_dependency"))
		if result, err := issue(create(mixed)); result != nil || !errors.Is(err, commonapi.ErrForbidden) {
			t.Fatalf("partial source coverage allowed: %+v %v", result, err)
		}
		issued, err := issue(job)
		if err != nil || issued == nil || issued.SourceReadScope.ConfigDigest == "" {
			t.Fatalf("issue=%+v %v", issued, err)
		}
		if _, err := issue(job); !errors.Is(err, iam.ErrExecutionAuthorizationConflict) {
			t.Fatalf("duplicate issuance: %v", err)
		}
		for _, query := range []string{
			"UPDATE system.execution_authorizations SET source_read_scope=NULL WHERE id=?",
			"UPDATE system.execution_authorizations SET source_read_scope=jsonb_set(source_read_scope,'{config_digest}','\"" + strings.Repeat("f", 64) + "\"') WHERE id=?",
			"INSERT INTO system.execution_authorization_engine_accesses(authorization_id,engine_id,effects) VALUES (?,999,ARRAY['read'])",
		} {
			if err := db.Exec(query, issued.ID).Error; err == nil {
				t.Fatalf("scope mutation accepted: %s", query)
			}
		}
		claim := iam.AuthorizeManagerProfileInput{AuthorizationID: issued.ID, ExecutionID: issued.ExecutionID, Attempt: 1, LeaseToken: uuid.New(),
			SourceReadScope: issued.SourceReadScope, ServicePrincipalID: runtimeID, ServiceClientID: "addp-manager", TenantID: tenantID, Audit: runtimeAudit}
		if observed, err := service.Authorize(ctx, claim); !observed.IsZero() || err == nil {
			t.Fatalf("pending consumption=%v %v", observed, err)
		}
		if err := db.Model(job).Updates(map[string]any{"status": "running", "attempt": claim.Attempt, "lease_token": claim.LeaseToken.String(),
			"lease_owner": "source-profile-fixture", "lease_expires_at": time.Now().Add(time.Minute),
			"execution_authorization_id": issued.ID, "authorization_expires_at": issued.ExpiresAt}).Error; err != nil {
			t.Fatal(err)
		}
		if observed, err := service.Authorize(ctx, claim); observed.IsZero() || err != nil {
			t.Fatalf("consume=%v %v", observed, err)
		}
		existing, _ := iam.NewExecutionAuthorizationService(repo)
		if result, err := existing.AuthorizeEngineAccess(ctx, iam.AuthorizeExecutionEngineAccessInput{AuthorizationID: issued.ID, ExecutionID: issued.ExecutionID,
			EngineID: int64(path.EngineID), RequiredEffects: []string{"read"}, ServicePrincipalID: runtimeID, ServiceClientID: "addp-manager", TenantID: tenantID, Audit: runtimeAudit}); result != nil || !errors.Is(err, iam.ErrExecutionAuthorizationPermissionDenied) {
			t.Fatalf("engine-only bypass: %+v %v", result, err)
		}
		wrong := claim
		wrong.LeaseToken = uuid.New()
		if observed, err := service.Authorize(ctx, wrong); !observed.IsZero() || err == nil {
			t.Fatal("replacement claim accepted")
		}
		wrong = claim
		wrong.SourceReadScope.ConfigDigest = strings.Repeat("a", 64)
		if observed, err := service.Authorize(ctx, wrong); !observed.IsZero() || err == nil {
			t.Fatal("different operation accepted")
		}
		if err := db.Model(job).Update("execution_config", gorm.Expr("jsonb_set(execution_config,'{budget,sample_size}','11')")).Error; err != nil {
			t.Fatal(err)
		}
		if observed, err := service.Authorize(ctx, claim); !observed.IsZero() || err == nil {
			t.Fatal("larger execution budget accepted")
		}
		if err := db.Model(job).Update("execution_config", gorm.Expr("jsonb_set(execution_config,'{budget,sample_size}','10')")).Error; err != nil {
			t.Fatal(err)
		}
		if observed, err := service.Authorize(ctx, claim); observed.IsZero() || err != nil {
			t.Fatalf("restored original: %v", err)
		}
		t.Run("audit failure returns no successful observation", func(t *testing.T) {
			const callback = "fixture:manager_profile_consume_audit_failure"
			reached := false
			if err := db.Callback().Create().After("gorm:create").Register(callback, func(tx *gorm.DB) {
				if log, ok := tx.Statement.Dest.(*iam.AuditLog); ok && log.EventName == "iam.execution_authorization.consumed" {
					reached = tx.Error == nil
					tx.AddError(errors.New("injected Manager consumption audit failure"))
				}
			}); err != nil {
				t.Fatal(err)
			}
			defer db.Callback().Create().Remove(callback)
			var before, after int64
			if err := db.Model(&iam.AuditLog{}).Where("event_name=?", "iam.execution_authorization.consumed").Count(&before).Error; err != nil {
				t.Fatal(err)
			}
			if observed, err := service.Authorize(ctx, claim); !observed.IsZero() || err == nil || !reached {
				t.Fatalf("audit failure observation=%v reached=%v err=%v", observed, reached, err)
			}
			if err := db.Model(&iam.AuditLog{}).Where("event_name=?", "iam.execution_authorization.consumed").Count(&after).Error; err != nil || after != before {
				t.Fatalf("audit rollback before=%d after=%d err=%v", before, after, err)
			}
		})
		t.Run("lease expiry during audit rejects consumption", func(t *testing.T) {
			var expires time.Time
			if err := db.Raw("SELECT clock_timestamp()+interval '0.3 seconds'").Scan(&expires).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Model(job).Update("lease_expires_at", expires).Error; err != nil {
				t.Fatal(err)
			}
			const callback = "fixture:manager_profile_consume_audit_expiry"
			reached := false
			if err := db.Callback().Create().After("gorm:create").Register(callback, func(tx *gorm.DB) {
				if log, ok := tx.Statement.Dest.(*iam.AuditLog); ok && log.EventName == "iam.execution_authorization.consumed" {
					reached = tx.Error == nil
					tx.AddError(tx.WithContext(ctx).Session(&gorm.Session{NewDB: true}).Exec(
						"SELECT pg_sleep(GREATEST(EXTRACT(EPOCH FROM (?::timestamptz-clock_timestamp()))+0.03,0))", expires).Error)
				}
			}); err != nil {
				t.Fatal(err)
			}
			defer db.Callback().Create().Remove(callback)
			if observed, err := service.Authorize(ctx, claim); !observed.IsZero() || !reached || !errors.Is(err, iam.ErrExecutionAuthorizationUnavailable) {
				t.Fatalf("expired lease observation=%v reached=%v err=%v", observed, reached, err)
			}
		})
		if err := db.Model(job).Update("lease_expires_at", gorm.Expr("clock_timestamp()+interval '1 minute'")).Error; err != nil {
			t.Fatal(err)
		}
		revoke(t, grantID)
		if observed, err := service.Authorize(ctx, claim); !observed.IsZero() || !errors.Is(err, commonapi.ErrForbidden) {
			t.Fatalf("revoked source allowed: %v %v", observed, err)
		}
		grant(t, "user", user.PrincipalID, nil)
		deny(t, "user", user.PrincipalID, nil)
		if observed, err := service.Authorize(ctx, claim); !observed.IsZero() || !errors.Is(err, commonapi.ErrForbidden) {
			t.Fatalf("Deny allowed: %v %v", observed, err)
		}
	})
}
