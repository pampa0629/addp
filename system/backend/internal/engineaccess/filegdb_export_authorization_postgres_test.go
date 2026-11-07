package engineaccess

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/execution"
	"github.com/addp/common/models"
	"github.com/addp/common/resourcetree"
	"github.com/addp/system/internal/iam"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

func exerciseFileGDBExportAuthorization(t *testing.T, db *gorm.DB, path plugin.EngineCatalogPath,
	newUser func(*testing.T, time.Duration) (userProvenance, time.Time), grant func(*testing.T, string, int64, *time.Time) uuid.UUID,
	roles *iam.TenantRoleService, tenantID, adminID int64, revoke func(*testing.T, uuid.UUID),
) {
	t.Run("FileGDB export exact source authorization", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(t.Context(), 35*time.Second)
		defer cancel()
		repo := iam.NewRepository(db)
		source := NewService(NewRepository(db), nil).
			WithAuthorizationFactsReader(testMetaFacts{paths: []plugin.EngineCatalogPath{path}}).
			WithExecutionEngineSnapshot(func(engine *models.Engine) (*models.Engine, error) {
				copy := *engine
				copy.ConnectionInfo = models.ConnectionInfo{"host": "guarded-fixture.invalid"}
				return &copy, nil
			})
		service := iam.NewFileGDBExportAuthorizationService(repo, source.PrepareExecutionSourceRead)
		user, _ := newUser(t, time.Hour)
		role, err := roles.CreateRole(ctx, iam.CreateTenantRoleInput{TenantID: tenantID, RoleKey: "custom.filegdb_source",
			Name: "FileGDB export fixture", PermissionKeys: []string{"manager.data_item.read", "manager.derived_artifact.create"}, ActorPrincipalID: adminID})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := roles.CreateAssignments(ctx, iam.CreateTenantRoleAssignmentsInput{TenantID: tenantID, MembershipID: user.MembershipID,
			RoleIDs: []int64{role.ID}, ActorPrincipalID: adminID, Reason: "Exact archive export fixture"}); err != nil {
			t.Fatal(err)
		}
		principal, err := repo.GetPrincipal(ctx, user.PrincipalID)
		if err != nil {
			t.Fatal(err)
		}
		user.AuthorizationVersion = principal.AuthorizationVersion
		tokens, err := iam.NewTokenFamilyService(repo, iam.BrowserSessionConfig{ResourceTicketOwners: []string{"manager"}}, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		selection, err := iam.NewContextSelectionService(repo, tokens)
		if err != nil {
			t.Fatal(err)
		}
		var now time.Time
		if err := db.Raw("SELECT clock_timestamp()").Scan(&now).Error; err != nil {
			t.Fatal(err)
		}
		choice, err := selection.BeginContextSelection(ctx, iam.BeginContextSelectionInput{PrincipalID: user.PrincipalID,
			Authentication: iam.SessionAuthentication{Methods: []string{"password"}, AssuranceLevel: iam.AssuranceLevelAAL1, AuthenticatedAt: now}})
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
			t.Fatal("no ordinary User session")
		}
		var runtimeID, engineVersion int64
		if err := db.Raw("SELECT id FROM system.service_principals WHERE name='addp-transfer'").Scan(&runtimeID).Error; err != nil || runtimeID == 0 {
			t.Fatalf("Transfer Runtime unavailable: %v", err)
		}
		if err := db.Raw("SELECT version FROM system.engines WHERE id=?", path.EngineID).Scan(&engineVersion).Error; err != nil || engineVersion <= 0 {
			t.Fatalf("engine version unavailable: %v", err)
		}
		hash, err := bcrypt.GenerateFromPassword([]byte("filegdb-disposable-runtime"), bcrypt.MinCost)
		if err != nil {
			t.Fatal(err)
		}
		if err := db.Exec("UPDATE system.oauth_clients SET client_secret_hash=?,status='active' WHERE client_id='addp-transfer'", string(hash)).Error; err != nil {
			t.Fatal(err)
		}
		binding := testReadBindings(path)[0]
		locator := (&resourcetree.ResourceLocator{EngineID: path.EngineID, Type: resourcetree.TypeTable,
			Path: []string{path.Segments[1].Name, path.Segments[2].Name}}).ToURI()
		actor := int(user.PrincipalID)
		job := &execution.TaskExecution{
			ExecutionID: uuid.NewString(), TenantID: int(tenantID), Module: "transfer", Source: "manager", TaskType: execution.TaskTypeSync,
			TriggeredBy: &actor, Status: execution.ExecutionStatusPending, TriggerType: execution.TriggerTypeManual, ExecutionBoundary: execution.ExecutionBoundaryBounded, MaxAttempts: 1,
			ActorPrincipalID: &user.PrincipalID, ActorTenantMembershipID: &user.MembershipID, IssuedAuthorizationVersion: &user.AuthorizationVersion,
			Metadata: models.JSONMap{"export_artifact_protection": map[string]any{}},
			ExecutionConfig: models.JSONMap{
				"runtime": map[string]any{"boundary": "bounded"}, "load": map[string]any{"mode": "snapshot"},
				"source":         map[string]any{"locator": locator, "data_type": "table", "representation": "native"},
				"target":         map[string]any{"parent_locator": "addp-infra://minio/manager/private?type=prefix", "name": "roads.gdb.zip", "data_type": "table", "representation": "encoded", "format": "filegdb"},
				"filegdb_source": execution.FileGDBExportSource{EngineVersion: strconv.FormatInt(engineVersion, 10), Binding: binding},
			},
		}
		if err := execution.CreatePending(ctx, db, job); err != nil {
			t.Fatal(err)
		}
		issue := iam.IssueFileGDBExportAuthorizationInput{SourceAccessToken: session.AccessToken, ExecutionID: uuid.MustParse(job.ExecutionID)}
		if issued, err := service.Issue(ctx, issue); issued != nil || err == nil {
			t.Fatal("export function permissions sufficed without a source Grant")
		}
		requestID := grant(t, "user", user.PrincipalID, nil)
		issued, err := service.Issue(ctx, issue)
		if err != nil || issued == nil {
			t.Fatalf("valid exact Grant failed to issue: %v", err)
		}
		if err := db.Model(job).Updates(map[string]any{"execution_authorization_id": issued.ID, "authorization_expires_at": issued.ExpiresAt}).Error; err != nil {
			t.Fatal(err)
		}
		_, lease, err := execution.ClaimPending(ctx, db, execution.PendingIdentity{ExecutionID: job.ExecutionID, TenantID: int(tenantID), Module: "transfer",
			TaskType: execution.TaskTypeSync, Source: "manager"}, execution.ClaimOptions{Module: "transfer", TaskType: execution.TaskTypeSync, WorkerID: "filegdb-fixture", LeaseDuration: time.Minute, RequireAuthorization: true}, nil)
		if err != nil || lease == nil {
			t.Fatalf("authorized Transfer did not claim: %v", err)
		}
		principalType, contextType := iam.PrincipalTypeServicePrincipal, iam.ContextTypeTenant
		consume := iam.AuthorizeFileGDBExportInput{
			AuthorizationID: issued.ID, ExecutionID: issued.ExecutionID, Attempt: lease.Attempt, LeaseToken: uuid.MustParse(lease.Token),
			SourceReadScope: issued.SourceReadScope, ServicePrincipalID: runtimeID, ServiceClientID: "addp-transfer", TenantID: tenantID,
			Audit: iam.AuditMetadata{PrincipalID: &runtimeID, PrincipalType: &principalType, ContextType: &contextType, TenantID: &tenantID},
		}
		observation, err := service.Authorize(ctx, consume)
		if err != nil || observation == nil || observation.Engine == nil || observation.Engine.ID != path.EngineID ||
			observation.Engine.Version != engineVersion || observation.Engine.ConnectionInfo["host"] != "guarded-fixture.invalid" {
			t.Fatalf("valid current lease failed to observe source: %v", err)
		}
		wrong := consume
		wrong.LeaseToken = uuid.New()
		if result, err := service.Authorize(ctx, wrong); result != nil || err == nil {
			t.Fatal("different lease consumed the scoped authorization")
		}
		generic, err := iam.NewExecutionAuthorizationService(repo)
		if err != nil {
			t.Fatal(err)
		}
		if access, err := generic.AuthorizeEngineAccess(ctx, iam.AuthorizeExecutionEngineAccessInput{
			AuthorizationID: issued.ID, ExecutionID: issued.ExecutionID, EngineID: int64(path.EngineID), RequiredEffects: []string{"read"},
			ServicePrincipalID: runtimeID, ServiceClientID: "addp-transfer", TenantID: tenantID, Audit: consume.Audit,
		}); access != nil || err == nil {
			t.Fatal("generic engine access bypassed the scoped FileGDB authorization")
		}
		revoke(t, requestID)
		if result, err := service.Authorize(ctx, consume); result != nil || err == nil {
			t.Fatal("revoked Grant exposed an engine snapshot")
		}
	})
}
