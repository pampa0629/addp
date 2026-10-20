package engineaccess

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	shared "github.com/addp/common/authorization"
	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/execution"
	"github.com/addp/common/models"
	"github.com/addp/system/internal/iam"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// Reuses the formal file Grant fixture and current User session. Meta and the
// connection decoder are controlled projections, not physical FTP content I/O.
func exerciseManagerModel3DAuthorization(t *testing.T, db *gorm.DB, source *Service, tenantID int64, user userProvenance, credential string, operator Actor, engineID uint, facts authorizationfacts.AuthorizationFacts) {
	t.Run("Manager FTP model source execution authorization", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(t.Context(), 25*time.Second)
		defer cancel()
		repo := iam.NewRepository(db)
		principal, err := repo.GetPrincipal(ctx, user.PrincipalID)
		if err != nil {
			t.Fatal(err)
		}
		user.AuthorizationVersion = principal.AuthorizationVersion
		decoded := 0
		var recoveryProbeID int64
		source.WithExecutionEngineSnapshot(func(stored *models.Engine) (*models.Engine, error) {
			decoded++
			if recoveryProbeID > 0 {
				probeErr := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
					return tx.Exec("SELECT id FROM system.execution_authorizations WHERE id=? FOR UPDATE NOWAIT", recoveryProbeID).Error
				})
				var pgErr *pgconn.PgError
				if !errors.As(probeErr, &pgErr) || pgErr.Code != "55P03" {
					t.Fatalf("reconciliation did not hold current EA against revocation: %v", probeErr)
				}
			}
			copy := *stored
			copy.ConnectionInfo = models.ConnectionInfo{"host": stored.ConnectionInfo["host"]}
			return &copy, nil
		})
		service := iam.NewManagerModel3DAuthorizationService(repo, source.PrepareExecutionSourceRead)
		var runtimeID int64
		if err := db.Raw("SELECT id FROM system.service_principals WHERE name='addp-manager'").Scan(&runtimeID).Error; err != nil || runtimeID == 0 {
			t.Fatalf("runtime unavailable: %v", err)
		}
		hash, err := bcrypt.GenerateFromPassword([]byte("disposable-model3d-source-client"), bcrypt.MinCost)
		if err != nil {
			t.Fatal(err)
		}
		if err := db.Exec("UPDATE system.oauth_clients SET client_secret_hash=?,status='active' WHERE client_id='addp-manager'", string(hash)).Error; err != nil {
			t.Fatal(err)
		}
		kind, contextType := iam.PrincipalTypeServicePrincipal, iam.ContextTypeTenant
		audit := iam.AuditMetadata{PrincipalID: &runtimeID, PrincipalType: &kind, ContextType: &contextType, TenantID: &tenantID}
		create := func() *execution.TaskExecution {
			t.Helper()
			var createdAt time.Time
			if err := db.WithContext(ctx).Raw("SELECT clock_timestamp()").Scan(&createdAt).Error; err != nil {
				t.Fatal(err)
			}
			task := "123"
			actor := int(user.PrincipalID)
			job := &execution.TaskExecution{ExecutionID: uuid.NewString(), TenantID: int(tenantID), Module: "manager", Source: "manager", TaskType: execution.TaskTypeModel3DGLBGeneration,
				SourceTaskID: &task, TriggeredBy: &actor, Status: "pending", TriggerType: "manual", ExecutionBoundary: "bounded", MaxAttempts: 3,
				ActorPrincipalID: &user.PrincipalID, ActorTenantMembershipID: &user.MembershipID, IssuedAuthorizationVersion: &user.AuthorizationVersion, CreatedAt: createdAt, UpdatedAt: createdAt,
				ExecutionConfig: models.JSONMap{"config_version": execution.ManagerModel3DConfigVersion, "owner_task": map[string]any{"id": task, "version": "7"}, "source_engine_version": "1", "source_composition": facts.Boundary.FileComposition.Clone(), "source": map[string]any{"engine_id": engineID}, "result": map[string]any{"key": uuid.NewString() + "/model.glb"}, "options": map[string]any{"max_bytes": 100}}, Metadata: models.JSONMap{}, ErrorDetails: models.JSONMap{}}
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
		issue := func(job *execution.TaskExecution, ttl time.Duration) (*iam.IssuedManagerModel3DAuthorization, error) {
			return service.Issue(ctx, iam.IssueManagerModel3DAuthorizationInput{SourceAccessToken: credential, ExecutionID: uuid.MustParse(job.ExecutionID), ExpiresIn: ttl})
		}
		t.Run("audit failure rolls back issuance", func(t *testing.T) {
			job := create()
			const callback = "fixture:model3d_issue_audit_failure"
			reached := false
			if err := db.Callback().Create().After("gorm:create").Register(callback, func(tx *gorm.DB) {
				if event, ok := tx.Statement.Dest.(*iam.AuditLog); ok && event.EventName == "iam.execution_authorization.issued" {
					reached = true
					tx.AddError(errors.New("injected issue audit failure"))
				}
			}); err != nil {
				t.Fatal(err)
			}
			defer db.Callback().Create().Remove(callback)
			if result, err := issue(job, time.Minute); result != nil || err == nil || !reached {
				t.Fatalf("issue succeeded on failed audit: %v", err)
			}
			var count int64
			if err := db.Model(&iam.ExecutionAuthorization{}).Where("execution_id=?", job.ExecutionID).Count(&count).Error; err != nil || count != 0 {
				t.Fatalf("issue residual=%d err=%v", count, err)
			}
		})
		job := create()
		issued, err := issue(job, time.Minute)
		if err != nil || issued == nil {
			t.Fatalf("issue failed: %v", err)
		}
		var before int64
		if err := db.Model(&iam.AuditLog{}).Where("event_name='iam.execution_authorization.issued' AND entity_id=?", strconv.FormatInt(issued.ID, 10)).Count(&before).Error; err != nil || before != 1 {
			t.Fatalf("initial audit count=%d err=%v", before, err)
		}
		recoveryProbeID = issued.ID
		recovered, err := issue(job, time.Minute)
		recoveryProbeID = 0
		if err != nil || recovered == nil || recovered.ID != issued.ID || !recovered.ExpiresAt.Equal(issued.ExpiresAt) {
			t.Fatalf("same UUID did not recover original: %v", err)
		}
		var after int64
		if err := db.Model(&iam.AuditLog{}).Where("event_name='iam.execution_authorization.issued' AND entity_id=?", strconv.FormatInt(issued.ID, 10)).Count(&after).Error; err != nil || after != before {
			t.Fatal("recovery issued another audit")
		}
		if result, err := issue(job, 2*time.Minute); result != nil || !errors.Is(err, iam.ErrExecutionAuthorizationConflict) {
			t.Fatal("recovery silently renewed TTL", err)
		}
		profile := iam.NewManagerProfileAuthorizationService(repo, source.PrepareExecutionSourceRead)
		if result, err := profile.Issue(ctx, iam.IssueManagerProfileAuthorizationInput{SourceAccessToken: credential, ExecutionID: issued.ExecutionID}); result != nil || err == nil {
			t.Fatal("model execution issued via profile purpose")
		}
		claim := iam.AuthorizeManagerModel3DInput{AuthorizationID: issued.ID, ExecutionID: issued.ExecutionID, Attempt: 1, LeaseToken: uuid.New(), SourceReadScope: issued.SourceReadScope, ServicePrincipalID: runtimeID, ServiceClientID: "addp-manager", TenantID: tenantID, Audit: audit}
		deny := func(input iam.AuthorizeManagerModel3DInput) {
			t.Helper()
			if got, err := service.Authorize(ctx, input); got != nil || err == nil {
				t.Fatal("invalid source/claim accepted")
			}
		}
		deny(claim)
		if err := db.Model(job).Updates(map[string]any{"status": "running", "attempt": 1, "lease_token": claim.LeaseToken.String(), "lease_owner": "model3d-source-fixture", "lease_expires_at": gorm.Expr("clock_timestamp()+interval '1 minute'"), "execution_authorization_id": issued.ID, "authorization_expires_at": issued.ExpiresAt}).Error; err != nil {
			t.Fatal(err)
		}
		allow := func() {
			t.Helper()
			got, err := service.Authorize(ctx, claim)
			if err != nil || got == nil || got.Engine == nil || got.Engine.ID != engineID || got.Engine.Version != 1 || got.Engine.ConnectionInfo["host"] != "file-composition.invalid" {
				t.Fatalf("current source snapshot rejected: %v", err)
			}
		}
		allow()
		if decoded == 0 {
			t.Fatal("same-decision snapshot decoder never called")
		}
		wrong := claim
		wrong.Attempt++
		deny(wrong)
		wrong = claim
		wrong.LeaseToken = uuid.New()
		deny(wrong)
		wrong = claim
		wrong.ServiceClientID = "addp-meta"
		deny(wrong)
		wrong = claim
		wrong.SourceReadScope.ConfigDigest = strings.Repeat("e", 64)
		deny(wrong)
		for _, owner := range []any{nil, "", " "} {
			if err := db.Model(job).Update("lease_owner", owner).Error; err != nil {
				t.Fatal(err)
			}
			deny(claim)
		}
		if err := db.Model(job).Update("lease_owner", "model3d-source-fixture").Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Model(job).Update("execution_config", gorm.Expr("jsonb_set(execution_config,'{owner_task,version}','\"8\"')")).Error; err != nil {
			t.Fatal(err)
		}
		deny(claim)
		if err := db.Model(job).Update("execution_config", gorm.Expr("jsonb_set(execution_config,'{owner_task,version}','\"7\"')")).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Table("system.engines").Where("id=?", engineID).Update("version", 2).Error; err != nil {
			t.Fatal(err)
		}
		deny(claim)
		if err := db.Table("system.engines").Where("id=?", engineID).Update("version", 1).Error; err != nil {
			t.Fatal(err)
		}
		allow()
		primary, _ := facts.Boundary.FileComposition.PrimaryTarget()
		denyID := uuid.New()
		if _, err := source.CreateDeny(ctx, CreateDenyInput{Actor: operator, EngineID: int64(engineID), DenyID: denyID, CatalogPath: primary, ItemFingerprint: facts.ItemFingerprint, RecipientType: "user", RecipientID: user.PrincipalID, Action: "read", ExpiryMode: shared.SharingExpiryUntilRevoked, Reason: "Deny current model consumption"}); err != nil {
			t.Fatal(err)
		}
		deny(claim)
		if _, err := source.ReleaseDeny(ctx, ReleaseDenyInput{Actor: operator, EngineID: int64(engineID), DenyID: denyID, Reason: "Release model fixture denial"}); err != nil {
			t.Fatal(err)
		}
		allow()
		t.Run("audit failure returns no connection", func(t *testing.T) {
			const callback = "fixture:model3d_consume_audit_failure"
			reached := false
			if err := db.Callback().Create().After("gorm:create").Register(callback, func(tx *gorm.DB) {
				if event, ok := tx.Statement.Dest.(*iam.AuditLog); ok && event.EventName == "iam.execution_authorization.consumed" {
					reached = true
					tx.AddError(errors.New("injected consume audit failure"))
				}
			}); err != nil {
				t.Fatal(err)
			}
			defer db.Callback().Create().Remove(callback)
			deny(claim)
			if !reached {
				t.Fatal("consume audit not reached")
			}
		})
		generic, _ := iam.NewExecutionAuthorizationService(repo)
		if got, err := generic.AuthorizeEngineAccess(ctx, iam.AuthorizeExecutionEngineAccessInput{AuthorizationID: issued.ID, ExecutionID: issued.ExecutionID, EngineID: int64(engineID), RequiredEffects: []string{"read"}, ServicePrincipalID: runtimeID, ServiceClientID: "addp-manager", TenantID: tenantID, Audit: audit}); got != nil || !errors.Is(err, iam.ErrExecutionAuthorizationPermissionDenied) {
			t.Fatal("generic engine-only bypass", err)
		}
		for _, query := range []string{"UPDATE system.execution_authorizations SET source_read_scope=NULL WHERE id=?", "INSERT INTO system.execution_authorization_engine_accesses(authorization_id,engine_id,effects) VALUES (?,999,ARRAY['read'])"} {
			if err := db.Exec(query, issued.ID).Error; err == nil {
				t.Fatal("immutable model scope mutation accepted")
			}
		}
		raw, _ := json.Marshal(issued.SourceReadScope)
		var frozen iam.ManagerSourceReadScope
		if json.Unmarshal(raw, &frozen) != nil || frozen.Model3D() == nil {
			t.Fatal("persisted model scope unreadable")
		}
		if err := db.Exec("UPDATE system.execution_authorizations SET revoked_at=clock_timestamp(),revoked_reason='test' WHERE id=?", issued.ID).Error; err != nil {
			t.Fatal(err)
		}
		deny(claim)
		// Restore only the pending fixture, not the immutable EA: reconciliation
		// must refuse the original revoked record instead of creating another.
		if err := db.Model(job).Updates(map[string]any{"status": "pending", "attempt": 0, "lease_token": nil, "lease_owner": nil, "lease_expires_at": nil, "execution_authorization_id": nil, "authorization_expires_at": nil}).Error; err != nil {
			t.Fatal(err)
		}
		if got, err := issue(job, time.Minute); got != nil || !errors.Is(err, iam.ErrExecutionAuthorizationConflict) {
			t.Fatal("revoked authorization revived", err)
		}
	})
}
