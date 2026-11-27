package migration

import (
	"crypto/rand"
	"errors"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/execution"
	"github.com/addp/common/infra/database"
	platformmysql "github.com/addp/common/infra/database/mysql"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/system/internal/iam"
	"github.com/addp/system/internal/models"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestMySQLOntologyProjectionAuthorization(t *testing.T) {
	if os.Getenv("ADDP_TEST_MYSQL_ONTOLOGY_AUTHORIZATION") != "1" {
		t.Skip("requires the isolated System installation gate")
	}
	driver, err := platformmysql.Dialector(database.Config{
		Host: os.Getenv("ADDP_MYSQL_HOST"), Port: os.Getenv("ADDP_MYSQL_PORT"), DBName: "system",
		User: os.Getenv("ADDP_DATABASE_RUNTIME_USER"), Password: os.Getenv("ADDP_DATABASE_RUNTIME_PASSWORD"), ConnectTimeout: 5 * time.Second,
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
	var tenantID, adminID int64
	if err := db.Raw("SELECT t.id,a.user_id FROM tenants t JOIN local_accounts a ON a.username='scan-tenant-admin' WHERE t.code='mysql-scan'").Row().Scan(&tenantID, &adminID); err != nil {
		t.Fatal(err)
	}
	user, err := iam.NewIdentityService(repo, nil).CreateLocalUser(ctx, iam.CreateLocalUserInput{
		Username: "ontology-projection-publisher", DisplayName: "Ontology projection publisher", Password: rand.Text(),
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
	roles := iam.NewTenantRoleService(repo, nil)
	role, err := roles.CreateRole(ctx, iam.CreateTenantRoleInput{
		TenantID: tenantID, RoleKey: "tenant.projection_publisher", Name: "Projection publisher",
		PermissionKeys: []string{"ontology.revision.publish", "system.execution_authorization.create"}, ActorPrincipalID: adminID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := roles.CreateAssignments(ctx, iam.CreateTenantRoleAssignmentsInput{
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

	service, err := iam.NewExecutionAuthorizationService(repo)
	if err != nil {
		t.Fatal(err)
	}
	var principal iam.Principal
	if err := db.First(&principal, user.PrincipalID).Error; err != nil {
		t.Fatal(err)
	}
	var runtimeID int64
	if err := db.Raw("SELECT id FROM service_principals WHERE name='addp-ontology'").Row().Scan(&runtimeID); err != nil {
		t.Fatal(err)
	}
	var memberships int64
	if err := db.Table("tenant_memberships").Where("tenant_id=? AND principal_id=?", tenantID, runtimeID).Count(&memberships).Error; err != nil {
		t.Fatal(err)
	}
	if memberships == 0 {
		assignMySQLFixtureRole(t, repo, tenantID, adminID, runtimeID, "tenant.ontology_runtime")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(rand.Text()), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("UPDATE system.oauth_clients SET client_secret_hash=?, status='active' WHERE client_id='addp-ontology'", string(hash)).Error; err != nil {
		t.Fatal(err)
	}
	serviceType, tenantType := iam.PrincipalTypeServicePrincipal, iam.ContextTypeTenant
	runtimeAudit := iam.AuditMetadata{PrincipalID: &runtimeID, PrincipalType: &serviceType, ContextType: &tenantType, TenantID: &tenantID}
	scope := execution.InternalTaskScope{TaskType: "semantic_projection", ResourceID: "beijing_outdoor", Revision: "1", Digest: strings.Repeat("a", 64), Generation: uuid.NewString()}
	newRequest := func() iam.IssueExecutionAuthorizationInput {
		id := uuid.New()
		job := &execution.TaskExecution{ExecutionID: id.String(), TenantID: int(tenantID), Module: "ontology", Source: "ontology", TaskType: scope.TaskType,
			Status: execution.ExecutionStatusPending, TriggerType: execution.TriggerTypeManual, ExecutionBoundary: execution.ExecutionBoundaryBounded, MaxAttempts: 3,
			ActorPrincipalID: &user.PrincipalID, ActorTenantMembershipID: &member.Membership.ID, IssuedAuthorizationVersion: &principal.AuthorizationVersion,
			ExecutionConfig: commonmodels.JSONMap{"ontology_id": scope.ResourceID, "revision": scope.Revision, "digest": scope.Digest, "generation": scope.Generation},
			Metadata:        commonmodels.JSONMap{}, ErrorDetails: commonmodels.JSONMap{}}
		if err := execution.NewTaskExecutionRepository(db).Create(ctx, job); err != nil {
			t.Fatal(err)
		}
		copy := scope
		return iam.IssueExecutionAuthorizationInput{SourceAccessToken: session.Session.AccessToken, Audience: "ontology", ExecutionID: id, InternalTask: &copy}
	}
	for _, field := range []string{"revision", "generation"} {
		t.Run("issue_rejects_wrong_"+field, func(t *testing.T) {
			r := newRequest()
			if field == "revision" {
				r.InternalTask.Revision = "2"
			} else {
				r.InternalTask.Generation = uuid.NewString()
			}
			if _, err := service.Issue(ctx, r); !errors.Is(err, commonapi.ErrBadRequest) {
				t.Fatalf("issue with wrong %s: %v", field, err)
			}
			var grants int64
			if err := db.Model(&iam.ExecutionAuthorization{}).Where("execution_id=?", r.ExecutionID).Count(&grants).Error; err != nil || grants != 0 {
				t.Fatalf("rejected issue left %d grants: %v", grants, err)
			}
		})
	}
	request := newRequest()
	issued, err := service.Issue(ctx, request)
	if err != nil || issued.InternalTask == nil || *issued.InternalTask != scope || len(issued.Accesses) != 0 {
		t.Fatalf("issue: %+v %#v", issued, err)
	}
	leaseToken := uuid.New()
	input := iam.AuthorizeInternalTaskInput{AuthorizationID: issued.ID, ExecutionID: issued.ExecutionID, Attempt: 1, LeaseToken: leaseToken, InternalTask: scope,
		ServicePrincipalID: runtimeID, ServiceClientID: "addp-ontology", TenantID: tenantID, Audit: runtimeAudit}
	if _, err := service.AuthorizeInternalTask(ctx, input); !errors.Is(err, iam.ErrExecutionAuthorizationUnavailable) {
		t.Fatalf("pending execution: %v", err)
	}
	if err := db.Model(&execution.TaskExecution{}).Where("execution_id=?", issued.ExecutionID).Updates(map[string]any{
		"execution_authorization_id": issued.ID, "authorization_expires_at": issued.ExpiresAt,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		_, lease, err := execution.ClaimNext(ctx, tx, execution.ClaimOptions{Module: "ontology", TaskType: scope.TaskType, WorkerID: "ontology-test", LeaseDuration: time.Minute, RequireAuthorization: true})
		if err != nil {
			return err
		}
		if lease == nil || lease.ExecutionID != issued.ExecutionID.String() {
			return errors.New("unexpected claim")
		}
		input.LeaseToken = uuid.MustParse(lease.Token)
		input.Attempt = lease.Attempt
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	access, err := service.AuthorizeInternalTask(ctx, input)
	if err != nil || access.Attempt != 1 || !access.ExpiresAt.Before(issued.ExpiresAt) {
		t.Fatalf("consume: %+v %v", access, err)
	}
	for _, tc := range []struct {
		name   string
		change func(*iam.AuthorizeInternalTaskInput)
		want   error
	}{
		{"revision", func(i *iam.AuthorizeInternalTaskInput) { i.InternalTask.Revision = "2" }, iam.ErrExecutionAuthorizationPermissionDenied},
		{"generation", func(i *iam.AuthorizeInternalTaskInput) { i.InternalTask.Generation = uuid.NewString() }, iam.ErrExecutionAuthorizationPermissionDenied},
		{"digest", func(i *iam.AuthorizeInternalTaskInput) { i.InternalTask.Digest = strings.Repeat("b", 64) }, iam.ErrExecutionAuthorizationPermissionDenied},
		{"client", func(i *iam.AuthorizeInternalTaskInput) { i.ServiceClientID = "addp-develop" }, iam.ErrExecutionAuthorizationPermissionDenied},
		{"lease_token", func(i *iam.AuthorizeInternalTaskInput) { i.LeaseToken = uuid.New() }, iam.ErrExecutionAuthorizationUnavailable},
		{"attempt", func(i *iam.AuthorizeInternalTaskInput) { i.Attempt++ }, iam.ErrExecutionAuthorizationUnavailable},
	} {
		t.Run("consume_rejects_wrong_"+tc.name, func(t *testing.T) {
			candidate := input
			tc.change(&candidate)
			if _, err := service.AuthorizeInternalTask(ctx, candidate); !errors.Is(err, tc.want) {
				t.Fatalf("consume: got %v, want %v", err, tc.want)
			}
		})
	}
	if _, err := service.AuthorizeEngineAccess(ctx, iam.AuthorizeExecutionEngineAccessInput{AuthorizationID: issued.ID, ExecutionID: issued.ExecutionID,
		EngineID: 1, RequiredEffects: []string{"read"}, ServicePrincipalID: runtimeID, ServiceClientID: "addp-ontology", TenantID: tenantID, Audit: runtimeAudit}); !errors.Is(err, commonapi.ErrForbidden) {
		t.Fatalf("engine access: %v", err)
	}

	if err := db.Exec("UPDATE common.task_executions SET lease_expires_at=CURRENT_TIMESTAMP(6) - INTERVAL 1 SECOND WHERE execution_id=?", issued.ExecutionID).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := service.AuthorizeInternalTask(ctx, input); !errors.Is(err, iam.ErrExecutionAuthorizationUnavailable) {
		t.Fatalf("expired lease: %v", err)
	}
	var issuedEvents, consumedEvents int
	if err := db.Raw(`SELECT COUNT(CASE WHEN event_name='iam.execution_authorization.issued' THEN 1 END),
        COUNT(CASE WHEN event_name='iam.execution_authorization.consumed' THEN 1 END) FROM audit_logs
        WHERE entity_type='execution_authorization' AND entity_id=?`, strconv.FormatInt(issued.ID, 10)).Row().Scan(&issuedEvents, &consumedEvents); err != nil || issuedEvents != 1 || consumedEvents != 1 {
		t.Fatalf("audit: issued=%d consumed=%d: %v", issuedEvents, consumedEvents, err)
	}
}
