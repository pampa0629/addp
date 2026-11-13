package api

import (
	"bytes"
	"context"
	"encoding/json"
	commonclient "github.com/addp/common/client"
	"github.com/addp/system/internal/iam"
	"github.com/addp/system/internal/migration"
	"github.com/addp/system/internal/testsupport"
	"github.com/gin-gonic/gin"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"testing"
	"time"
)

// Real disposable IAM -> OAuth -> Python AuthContext client -> actual Runtime routes.
// No AuthContext or Runtime authorization function is replaced by a test double.
func TestWorkflowPrivateFilesRuntimeAuthAgainstPostgres(t *testing.T) {
	python := os.Getenv("ADDP_WORKFLOW_PRIVATE_FILES_PYTHON")
	if python == "" {
		t.Skip("requires standard System IAM Runtime auth gate")
	}
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
		defer func() {
			if err := sqlDB.Close(); err != nil {
				t.Error(err)
			}
		}()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := db.WithContext(ctx).Exec("DROP SCHEMA IF EXISTS system CASCADE; DROP SCHEMA IF EXISTS common CASCADE").Error; err != nil {
			t.Error(err)
			return
		}
		var remaining int64
		if err := db.WithContext(ctx).Raw("SELECT count(*) FROM pg_namespace WHERE nspname IN ('system','common')").Scan(&remaining).Error; err != nil || remaining != 0 {
			t.Errorf("test schema residual=%d err=%v", remaining, err)
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	if err := db.Exec("DROP SCHEMA IF EXISTS system CASCADE; DROP SCHEMA IF EXISTS common CASCADE").Error; err != nil {
		t.Fatal(err)
	}
	if err := migration.NewRunner(dsn).Run(ctx); err != nil {
		t.Fatal(err)
	}
	identity := iam.NewRepository(db)
	user := &iam.Principal{PrincipalType: iam.PrincipalTypeUser, Status: iam.PrincipalStatusActive, AuthorizationVersion: 1}
	if err := identity.Transaction(ctx, func(tx *iam.Repository) error {
		if err := tx.CreatePrincipal(ctx, user); err != nil {
			return err
		}
		return tx.CreateUser(ctx, &iam.User{ID: user.ID, DisplayName: "Frozen facts administrator"})
	}); err != nil {
		t.Fatal(err)
	}
	// Use the production tenant initializer, with no manual runtime membership
	// or role assignment, to prove future tenants inherit the existing binding.
	tenantService := iam.NewPlatformTenantService(identity, time.Now)
	tenant, err := tenantService.Create(ctx, iam.CreateTenantInput{Code: "file_facts", Name: "Frozen facts", InitialAdministratorPrincipalID: user.ID, ActorPrincipalID: user.ID})
	if err != nil {
		t.Fatal(err)
	}
	other, err := tenantService.Create(ctx, iam.CreateTenantInput{Code: "file_facts_other", Name: "Other tenant", InitialAdministratorPrincipalID: user.ID, ActorPrincipalID: user.ID})
	if err != nil {
		t.Fatal(err)
	}
	secrets := testBuiltinServiceClientSecrets("frozen-facts")
	secrets["addp-system"] = "frozen-facts-system-0123456789abcdef0123456789abcdef"
	provisioner, err := iam.NewServiceCredentialProvisioner(identity, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := provisioner.Apply(ctx, secrets); err != nil {
		t.Fatal(err)
	}
	runtime, err := NewIAMRuntime(db, testIAMRuntimeConfig(), testIAMSecurityPolicy())
	if err != nil {
		t.Fatal(err)
	}
	systemRouter := gin.New()
	systemRouter.POST("/api/v1/system/oauth/token", runtime.OAuthHandler.Token)
	systemRouter.GET("/api/v1/system/auth/context", runtime.AuthHandler.Context)
	systemServer := httptest.NewServer(systemRouter)
	defer systemServer.Close()

	root, err := filepath.Abs("../../../..")
	if err != nil {
		t.Fatal(err)
	}
	probe := func(token string, status int, principalID, tenantID string) {
		t.Helper()
		raw, err := json.Marshal(map[string]any{"system_url": systemServer.URL, "token": token, "expected_status": status, "principal_id": principalID, "tenant_id": tenantID})
		if err != nil {
			t.Fatal(err)
		}
		cmd := exec.CommandContext(ctx, python, filepath.Join(root, "scripts/test/workflow-private-files-auth-probe.py"))
		cmd.Stdin = bytes.NewReader(raw)
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("actual Runtime guard expected=%d: %v %s", status, err, output)
		}
		var report struct {
			Result string `json:"result"`
			Routes int    `json:"routes_checked"`
		}
		if json.Unmarshal(bytes.TrimSpace(output), &report) != nil || report.Result != "passed" || report.Routes != 5 {
			t.Fatalf("missing real Runtime guard result: %s", output)
		}
	}
	source, err := commonclient.NewOAuthServiceTokenSource(systemServer.URL, "addp-develop", secrets["addp-develop"], systemServer.Client())
	if err != nil {
		t.Fatal(err)
	}
	token, err := source.Token(ctx, uint(tenant.ID))
	if err != nil {
		t.Fatal(err)
	}
	ac, err := runtime.AuthContextService.ResolveAccessToken(ctx, token)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, assignment := range ac.Authorization.RoleAssignments {
		if slices.Contains(assignment.Permissions, "system.workflow_private_files.execute") {
			if assignment.RoleKey != "tenant.develop_runtime" {
				t.Fatal("private Runtime permission escaped fixed role")
			}
			found = true
		}
	}
	if !found {
		t.Fatal("real Develop OAuth credential lacks private files Runtime permission")
	}
	probe(token, 404, ac.Principal.ID, strconv.FormatInt(tenant.ID, 10))
	otherToken, err := source.Token(ctx, uint(other.ID))
	if err != nil {
		t.Fatal(err)
	}
	probe(otherToken, 404, ac.Principal.ID, strconv.FormatInt(other.ID, 10))
	wrongSource, err := commonclient.NewOAuthServiceTokenSource(systemServer.URL, "addp-system", secrets["addp-system"], systemServer.Client())
	if err != nil {
		t.Fatal(err)
	}
	wrongToken, err := wrongSource.Token(ctx, uint(tenant.ID))
	if err != nil {
		t.Fatal(err)
	}
	probe(wrongToken, 403, "", "")
	// Develop already owns a separate Platform runtime role for registration.
	// Its valid Platform credential must still be rejected by this Tenant-only guard.
	platformToken, err := source.PlatformToken(ctx)
	if err != nil {
		t.Fatal(err)
	}
	probe(platformToken, 403, "", "")
	var membership, version int64
	if err := db.Raw("SELECT m.id,p.authorization_version FROM system.tenant_memberships m JOIN system.principals p ON p.id=m.principal_id WHERE m.tenant_id=? AND m.principal_id=?", tenant.ID, user.ID).Row().Scan(&membership, &version); err != nil {
		t.Fatal(err)
	}
	var now time.Time
	if err := db.Raw("SELECT clock_timestamp()").Scan(&now).Error; err != nil {
		t.Fatal(err)
	}
	userContext, err := runtime.ContextSelectionService.BeginContextSelection(ctx, iam.BeginContextSelectionInput{PrincipalID: user.ID, Authentication: iam.SessionAuthentication{Methods: []string{"password", "totp"}, AssuranceLevel: iam.AssuranceLevelAAL2, AuthenticatedAt: now}})
	if err != nil || userContext == nil || userContext.Challenge == nil {
		t.Fatalf("issue real User context challenge: %v", err)
	}
	userSession, err := runtime.ContextSelectionService.ConsumeContextSelection(ctx, iam.ConsumeContextSelectionInput{SelectionTicket: userContext.Challenge.SelectionTicket, Choice: iam.ContextSelectionChoice{Type: iam.ContextTypeTenant, TenantMembershipID: &membership}})
	if err != nil {
		t.Fatal(err)
	}
	probe(userSession.AccessToken, 403, "", "")
	if err := db.Exec(`DELETE FROM system.role_permissions WHERE role_id=(SELECT id FROM system.roles WHERE tenant_id IS NULL AND role_key='tenant.develop_runtime') AND permission_id=(SELECT id FROM system.permissions WHERE permission_key='system.workflow_private_files.execute')`).Error; err != nil {
		t.Fatal(err)
	}
	probe(token, 401, "", "")
	fresh, err := commonclient.NewOAuthServiceTokenSource(systemServer.URL, "addp-develop", secrets["addp-develop"], systemServer.Client())
	if err != nil {
		t.Fatal(err)
	}
	freshToken, err := fresh.Token(ctx, uint(tenant.ID))
	if err != nil {
		t.Fatal(err)
	}
	probe(freshToken, 403, "", "")
	var humanPermission, grants, currentVersion int64
	if err := db.Raw(`SELECT count(*) FROM system.role_permissions rp JOIN system.permissions p ON p.id=rp.permission_id JOIN system.role_assignments a ON a.role_id=rp.role_id WHERE a.principal_id=? AND p.permission_key='system.workflow_private_files.execute'`, user.ID).Scan(&humanPermission).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Raw("SELECT count(*) FROM system.engine_access_grants").Scan(&grants).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Raw("SELECT authorization_version FROM system.principals WHERE id=?", user.ID).Scan(&currentVersion).Error; err != nil {
		t.Fatal(err)
	}
	if humanPermission != 0 || grants != 0 || currentVersion != version {
		t.Fatal("machine qualification changed User authority or data Grants")
	}
	if _, err := runtime.AuthContextService.ResolveAccessToken(ctx, userSession.AccessToken); err != nil {
		t.Fatalf("machine permission revocation invalidated the existing User session: %v", err)
	}
}
