package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strconv"
	"testing"
	"time"

	commonclient "github.com/addp/common/client"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/execution"
	sharedauth "github.com/addp/common/middleware/auth"
	"github.com/addp/system/internal/iam"
	"github.com/addp/system/internal/migration"
	"github.com/addp/system/internal/testsupport"
	"github.com/gin-gonic/gin"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// The Owner response is a facts double; OAuth issuance, tenant initialization,
// AuthContext resolution and all guards are real. Develop's own route/repository
// tests cover the Owner implementation. This is a disposable T2, not a T4 run.
func TestDevelopFileRuntimeAuthContextAgainstPostgres(t *testing.T) {
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
	tokens, err := commonclient.NewOAuthServiceTokenSource(systemServer.URL, "addp-system", secrets["addp-system"], systemServer.Client())
	if err != nil {
		t.Fatal(err)
	}
	token, err := tokens.Token(ctx, uint(tenant.ID))
	if err != nil {
		t.Fatal(err)
	}
	authContext, err := runtime.AuthContextService.ResolveAccessToken(ctx, token)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, assignment := range authContext.Authorization.RoleAssignments {
		if slices.Contains(assignment.Permissions, "develop.file_execution.read") {
			if assignment.RoleKey != "tenant.system_runtime" {
				t.Fatalf("unexpected permission owner: %s", assignment.RoleKey)
			}
			found = true
		}
	}
	if !found || authContext.Principal.Type != "service_principal" || authContext.Client.ClientID == nil || *authContext.Client.ClientID != "addp-system" {
		t.Fatal("real System machine credential lacks frozen execution read permission")
	}
	if _, err := tokens.PlatformToken(ctx); err == nil {
		t.Fatal("tenant runtime role unexpectedly issued a platform credential")
	}
	const executionID = "91000000-0000-4000-8000-000000000291"
	raw := json.RawMessage(`{"value":9007199254740993}`)
	intent, err := execution.NewDevelopFileIntent(raw, 1, 45, []execution.DevelopFileSourceIntent{{NodeID: "load", Target: plugin.FileItemPath(11, "source.gpkg")}}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	var memberID, userVersion int64
	if err := db.Raw("SELECT m.id,p.authorization_version FROM system.tenant_memberships m JOIN system.principals p ON p.id=m.principal_id WHERE m.tenant_id=? AND m.principal_id=?", tenant.ID, user.ID).Row().Scan(&memberID, &userVersion); err != nil {
		t.Fatal(err)
	}
	facts := commonclient.DevelopFileExecutionFacts{SchemaVersion: commonclient.DevelopFileExecutionSchema, ExecutionID: executionID, TenantID: strconv.FormatInt(tenant.ID, 10), Actor: commonclient.DevelopFileExecutionActor{PrincipalID: strconv.FormatInt(user.ID, 10), TenantMembershipID: strconv.FormatInt(memberID, 10), AuthorizationVersion: strconv.FormatInt(userVersion, 10)}, ConfigJSON: raw, Intent: *intent, AdmissionState: "pending"}
	ownerRouter := gin.New()
	routes := ownerRouter.Group("/api/v1/develop", sharedauth.MustNewMiddleware(sharedauth.MiddlewareConfig{SystemURL: systemServer.URL}), sharedauth.MustNewContextGuard("tenant"))
	routes.Use(sharedauth.MustNewServiceClientGuard("addp-system"))
	routes.GET("/runtime/file-executions/:execution_id", sharedauth.MustNewPermissionGuard("develop.file_execution.read"), func(c *gin.Context) {
		actualTenant, _ := sharedauth.TenantIDFromGin(c)
		if actualTenant != tenant.ID || c.Param("execution_id") != executionID {
			c.JSON(http.StatusNotFound, gin.H{"error_code": "execution_not_found"})
			return
		}
		c.Header("Cache-Control", "no-store")
		c.JSON(http.StatusOK, facts)
	})
	ownerServer := httptest.NewServer(ownerRouter)
	defer ownerServer.Close()
	reader := commonclient.NewDevelopClient(ownerServer.URL, tokens, ownerServer.Client())
	assertReadDenied := func(reader *commonclient.DevelopClient, tenantID uint, status int) {
		t.Helper()
		_, err := reader.ReadFileExecution(ctx, tenantID, executionID)
		var failure *commonclient.TenantAPIError
		if !errors.As(err, &failure) || failure.StatusCode != status {
			t.Fatalf("expected frozen facts read status %d, got %v", status, err)
		}
	}
	read, err := reader.ReadFileExecution(ctx, uint(tenant.ID), executionID)
	if err != nil || read == nil || read.Intent.ConfigDigest != intent.ConfigDigest {
		t.Fatalf("read frozen Owner facts through real machine credential: %v", err)
	}
	assertReadDenied(reader, uint(other.ID), http.StatusNotFound)
	metaTokens, err := commonclient.NewOAuthServiceTokenSource(systemServer.URL, "addp-meta", secrets["addp-meta"], systemServer.Client())
	if err != nil {
		t.Fatal(err)
	}
	assertReadDenied(commonclient.NewDevelopClient(ownerServer.URL, metaTokens, ownerServer.Client()), uint(tenant.ID), http.StatusForbidden)
	var humanPermission, grants int64
	if err := db.Raw(`SELECT count(*) FROM system.role_permissions rp JOIN system.permissions p ON p.id=rp.permission_id JOIN system.role_assignments a ON a.role_id=rp.role_id WHERE a.principal_id=? AND p.permission_key='develop.file_execution.read'`, user.ID).Scan(&humanPermission).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Raw("SELECT count(*) FROM system.engine_access_grants").Scan(&grants).Error; err != nil || humanPermission != 0 || grants != 0 {
		t.Fatalf("human permission=%d data grants=%d err=%v", humanPermission, grants, err)
	}
	// Current-state revocation must invalidate an already-issued credential and
	// also reject a freshly issued credential at the permission guard.
	if err := db.Exec(`DELETE FROM system.role_permissions WHERE role_id=(SELECT id FROM system.roles WHERE tenant_id IS NULL AND role_key='tenant.system_runtime') AND permission_id=(SELECT id FROM system.permissions WHERE permission_key='develop.file_execution.read')`).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.AuthContextService.ResolveAccessToken(ctx, token); err == nil {
		t.Fatal("stale machine credential survived permission revocation")
	}
	assertReadDenied(reader, uint(tenant.ID), http.StatusUnauthorized)
	currentTokens, err := commonclient.NewOAuthServiceTokenSource(systemServer.URL, "addp-system", secrets["addp-system"], systemServer.Client())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := currentTokens.Token(ctx, uint(tenant.ID)); err != nil {
		t.Fatalf("issue current machine credential after revocation: %v", err)
	}
	assertReadDenied(commonclient.NewDevelopClient(ownerServer.URL, currentTokens, ownerServer.Client()), uint(tenant.ID), http.StatusForbidden)
}
