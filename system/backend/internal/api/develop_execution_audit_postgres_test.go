package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"
	"time"

	commonclient "github.com/addp/common/client"
	commonaudit "github.com/addp/common/middleware/audit"
	sharedauth "github.com/addp/common/middleware/auth"
	requestidmiddleware "github.com/addp/common/middleware/requestid"
	"github.com/addp/system/internal/iam"
	"github.com/addp/system/internal/middleware"
	"github.com/addp/system/internal/migration"
	"github.com/addp/system/internal/testsupport"
	"github.com/gin-gonic/gin"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestDevelopExecutionServiceAuditPersistsAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("ADDP_SYSTEM_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("set ADDP_SYSTEM_POSTGRES_TEST_DSN to a disposable PostgreSQL 15+ database")
	}
	testsupport.RequireDisposablePostgresDSN(t, dsn)
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`DROP SCHEMA IF EXISTS system CASCADE`).Error; err != nil {
		t.Fatalf("reset Develop execution audit test schema: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := migration.NewRunner(dsn).Run(ctx); err != nil {
		t.Fatalf("apply IAM migrations: %v", err)
	}

	secrets := testBuiltinServiceClientSecrets("develop-audit")
	provisioner, err := iam.NewServiceCredentialProvisioner(iam.NewRepository(db), nil)
	if err != nil {
		t.Fatalf("create service credential provisioner: %v", err)
	}
	if err := provisioner.Apply(ctx, secrets); err != nil {
		t.Fatalf("provision service credentials: %v", err)
	}

	var developPrincipalID, developRoleID, tenantID int64
	if err := db.Raw(`SELECT id FROM system.service_principals WHERE name = 'addp-develop'`).Scan(&developPrincipalID).Error; err != nil {
		t.Fatalf("load Develop service principal: %v", err)
	}
	if err := db.Raw(`SELECT id FROM system.roles WHERE tenant_id IS NULL AND role_key = 'tenant.develop_runtime'`).Scan(&developRoleID).Error; err != nil {
		t.Fatalf("load Develop runtime role: %v", err)
	}
	if err := db.Raw(`
		INSERT INTO system.tenants (code, name, status)
		VALUES ('develop-execution-audit', 'Develop Execution Audit', 'active')
		RETURNING id
	`).Scan(&tenantID).Error; err != nil {
		t.Fatalf("create Develop execution audit tenant: %v", err)
	}
	if err := db.Exec(`
		INSERT INTO system.tenant_memberships
		    (tenant_id, principal_id, status, source_type, joined_at, created_by_principal_id)
		VALUES (?, ?, 'active', 'bootstrap', now(), ?)
	`, tenantID, developPrincipalID, developPrincipalID).Error; err != nil {
		t.Fatalf("create Develop runtime membership: %v", err)
	}
	if err := db.Exec(`
		INSERT INTO system.role_assignments
		    (principal_id, role_id, scope_type, tenant_id, status, valid_from, source_type, grant_reason)
		VALUES (?, ?, 'tenant', ?, 'active', now(), 'bootstrap', 'Develop execution audit integration test')
	`, developPrincipalID, developRoleID, tenantID).Error; err != nil {
		t.Fatalf("assign Develop runtime role: %v", err)
	}

	runtime, err := NewIAMRuntime(db, testIAMRuntimeConfig(), testIAMSecurityPolicy())
	if err != nil {
		t.Fatalf("create IAM runtime: %v", err)
	}
	tenantContext, err := middleware.NewIAMServiceContextGuard("tenant")
	if err != nil {
		t.Fatal(err)
	}
	auditCreate, err := middleware.NewIAMPermissionGuard("audit.tenant_event.create")
	if err != nil {
		t.Fatal(err)
	}

	gin.SetMode(gin.TestMode)
	systemRouter := gin.New()
	systemRouter.POST("/api/v1/system/oauth/token", runtime.OAuthHandler.Token)
	systemRouter.POST(
		"/api/v1/system/tenant/audit/events",
		runtime.Authentication,
		runtime.ServiceCredential,
		tenantContext,
		auditCreate,
		runtime.InternalAuditHandler.CreateService,
	)
	systemServer := httptest.NewServer(systemRouter)
	defer systemServer.Close()

	tokenSource, err := commonclient.NewOAuthServiceTokenSource(
		systemServer.URL,
		"addp-develop",
		secrets["addp-develop"],
		systemServer.Client(),
	)
	if err != nil {
		t.Fatalf("create Develop service token source: %v", err)
	}
	systemClient := commonclient.NewSystemServiceClient(systemServer.URL, tokenSource, systemServer.Client())

	tenantIDText := strconv.FormatInt(tenantID, 10)
	requestID := "develop-execution-audit-request"
	developRouter := gin.New()
	developRouter.Use(requestidmiddleware.RequestIDMiddleware())
	developRouter.Use(func(c *gin.Context) {
		authContext := testIAMActorContext("tenant")
		authContext.Context.TenantID = &tenantIDText
		if err := sharedauth.SetAuthContextForGin(c, authContext); err != nil {
			t.Fatalf("set Develop request AuthContext: %v", err)
		}
		c.Next()
	})
	developRouter.Use(commonaudit.ServiceAuditMiddleware("develop", systemClient))
	developRouter.POST("/api/v1/develop/executions", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"execution_id": "develop-query-execution", "status": "pending"})
	})

	request := httptest.NewRequest(http.MethodPost, "/api/v1/develop/executions", nil)
	request.Header.Set(requestidmiddleware.RequestIDHeader, requestID)
	response := httptest.NewRecorder()
	developRouter.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("Develop execution status = %d body=%s", response.Code, response.Body.String())
	}

	deadline := time.Now().Add(10 * time.Second)
	for {
		var count int64
		if err := db.Table("system.audit_logs").Where("request_id = ?", requestID).Count(&count).Error; err != nil {
			t.Fatalf("count Develop execution audit events: %v", err)
		}
		if count == 1 {
			break
		}
		if count > 1 {
			t.Fatalf("Develop execution audit event count = %d, want 1", count)
		}
		if time.Now().After(deadline) {
			t.Fatal("Develop execution audit event was not persisted within 10 seconds")
		}
		time.Sleep(20 * time.Millisecond)
	}

	var auditEvent struct {
		PrincipalID   int64
		PrincipalType string
		ContextType   string
		TenantID      int64
		EventName     string
		Result        string
		ModuleName    string
		HTTPMethod    string
		ResourcePath  string
		HTTPStatus    int
		RequestID     string
	}
	if err := db.Table("system.audit_logs").Where("request_id = ?", requestID).Take(&auditEvent).Error; err != nil {
		t.Fatalf("load Develop execution audit event: %v", err)
	}
	if auditEvent.PrincipalID != developPrincipalID ||
		auditEvent.PrincipalType != "service_principal" ||
		auditEvent.ContextType != "tenant" ||
		auditEvent.TenantID != tenantID ||
		auditEvent.EventName != "http.request.completed" ||
		auditEvent.Result != "succeeded" ||
		auditEvent.ModuleName != "develop" ||
		auditEvent.HTTPMethod != http.MethodPost ||
		auditEvent.ResourcePath != "/api/v1/develop/executions" ||
		auditEvent.HTTPStatus != http.StatusOK ||
		auditEvent.RequestID != requestID {
		t.Fatalf("Develop execution audit event = %#v", auditEvent)
	}
}
