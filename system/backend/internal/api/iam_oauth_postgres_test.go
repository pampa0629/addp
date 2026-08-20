package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	commonauth "github.com/addp/common/authorization"
	sharedauth "github.com/addp/common/middleware/auth"
	commonsecurity "github.com/addp/common/security"
	"github.com/addp/system/internal/iam"
	iamoauth "github.com/addp/system/internal/iam/oauth"
	"github.com/addp/system/internal/middleware"
	"github.com/addp/system/internal/migration"
	"github.com/addp/system/internal/models"
	"github.com/addp/system/internal/repository"
	"github.com/addp/system/internal/service"
	"github.com/addp/system/internal/testsupport"
	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestIAMOAuthClientCredentialsAuthContextAgainstPostgres(t *testing.T) {
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
		t.Fatalf("reset IAM OAuth client credentials test schema: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := migration.NewRunner(dsn).Run(ctx); err != nil {
		t.Fatalf("apply IAM migrations: %v", err)
	}

	secrets := testBuiltinServiceClientSecrets("initial")
	provisioner, err := iam.NewServiceCredentialProvisioner(iam.NewRepository(db), nil)
	if err != nil {
		t.Fatalf("create service credential provisioner: %v", err)
	}
	if err := provisioner.Apply(ctx, secrets); err != nil {
		t.Fatalf("provision service credentials: %v", err)
	}
	var managerPrincipalID, metaPrincipalID, tenantID, otherTenantID, managerRoleID, metaRoleID int64
	var storedHash string
	if err := db.Raw(`
		SELECT service_principal.id, oauth_client.client_secret_hash
		FROM system.service_principals service_principal
		JOIN system.oauth_clients oauth_client ON oauth_client.service_principal_id = service_principal.id
		WHERE service_principal.name = 'addp-manager'
	`).Row().Scan(&managerPrincipalID, &storedHash); err != nil {
		t.Fatalf("load provisioned manager service client: %v", err)
	}
	if storedHash == secrets["addp-manager"] || bcrypt.CompareHashAndPassword([]byte(storedHash), []byte(secrets["addp-manager"])) != nil {
		t.Fatal("manager service client secret was not stored exclusively as a BCrypt hash")
	}
	if err := db.Raw(`INSERT INTO system.tenants (code, name, status) VALUES ('service-token', 'Service Token', 'active') RETURNING id`).Scan(&tenantID).Error; err != nil {
		t.Fatalf("create service token tenant: %v", err)
	}
	if err := db.Raw(`INSERT INTO system.tenants (code, name, status) VALUES ('service-token-other', 'Service Token Other', 'active') RETURNING id`).Scan(&otherTenantID).Error; err != nil {
		t.Fatalf("create other service token tenant: %v", err)
	}
	if err := db.Exec(`INSERT INTO system.tenant_memberships (tenant_id, principal_id, status, source_type, joined_at, created_by_principal_id) VALUES (?, ?, 'active', 'bootstrap', now(), ?)`, tenantID, managerPrincipalID, managerPrincipalID).Error; err != nil {
		t.Fatalf("create manager runtime membership: %v", err)
	}
	if err := db.Raw(`SELECT id FROM system.roles WHERE tenant_id IS NULL AND role_key = 'tenant.manager_runtime'`).Scan(&managerRoleID).Error; err != nil {
		t.Fatalf("load manager runtime role: %v", err)
	}
	if err := db.Exec(`INSERT INTO system.role_assignments (principal_id, role_id, scope_type, tenant_id, status, valid_from, source_type, reason) VALUES (?, ?, 'tenant', ?, 'active', now(), 'bootstrap', 'test runtime')`, managerPrincipalID, managerRoleID, tenantID).Error; err != nil {
		t.Fatalf("create manager runtime assignment: %v", err)
	}
	if err := db.Raw(`SELECT id FROM system.service_principals WHERE name = 'addp-meta'`).Scan(&metaPrincipalID).Error; err != nil {
		t.Fatalf("load Meta service principal: %v", err)
	}
	if err := db.Raw(`SELECT id FROM system.roles WHERE tenant_id IS NULL AND role_key = 'tenant.meta_runtime'`).Scan(&metaRoleID).Error; err != nil {
		t.Fatalf("load Meta runtime role: %v", err)
	}
	for _, runtimeTenantID := range []int64{tenantID, otherTenantID} {
		if err := db.Exec(`INSERT INTO system.tenant_memberships (tenant_id, principal_id, status, source_type, joined_at, created_by_principal_id) VALUES (?, ?, 'active', 'bootstrap', now(), ?)`, runtimeTenantID, metaPrincipalID, metaPrincipalID).Error; err != nil {
			t.Fatalf("create Meta runtime membership for tenant %d: %v", runtimeTenantID, err)
		}
		if err := db.Exec(`INSERT INTO system.role_assignments (principal_id, role_id, scope_type, tenant_id, status, valid_from, source_type, reason) VALUES (?, ?, 'tenant', ?, 'active', now(), 'bootstrap', 'test runtime')`, metaPrincipalID, metaRoleID, runtimeTenantID).Error; err != nil {
			t.Fatalf("create Meta runtime assignment for tenant %d: %v", runtimeTenantID, err)
		}
	}

	runtime, err := NewIAMRuntime(db, testIAMRuntimeConfig(), testIAMSecurityPolicy())
	if err != nil {
		t.Fatalf("NewIAMRuntime() error = %v", err)
	}
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(runtime.OAuthFailureAudit)
	router.POST("/api/v1/system/oauth/token", runtime.OAuthHandler.Token)

	wrongTenantResponse := performIAMOAuthClientCredentialsRequest(t, router, secrets["addp-manager"], otherTenantID)
	if wrongTenantResponse.Code != http.StatusBadRequest {
		t.Fatalf("wrong tenant token status = %d body=%s", wrongTenantResponse.Code, wrongTenantResponse.Body.String())
	}
	tokenResponse := performIAMOAuthClientCredentialsRequest(t, router, secrets["addp-manager"], tenantID)
	if tokenResponse.Code != http.StatusOK {
		t.Fatalf("client credentials token status = %d body=%s", tokenResponse.Code, tokenResponse.Body.String())
	}
	var tokenPayload struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		TokenType    string `json:"token_type"`
		Scope        string `json:"scope"`
		ExpiresIn    int64  `json:"expires_in"`
	}
	if err := json.Unmarshal(tokenResponse.Body.Bytes(), &tokenPayload); err != nil {
		t.Fatalf("decode client credentials token: %v", err)
	}
	if !strings.HasPrefix(tokenPayload.AccessToken, "addp_at_") || tokenPayload.RefreshToken != "" ||
		!strings.EqualFold(tokenPayload.TokenType, "bearer") || tokenPayload.Scope != "addp.api" || tokenPayload.ExpiresIn <= 0 || tokenPayload.ExpiresIn > 300 {
		t.Fatalf("client credentials token payload = %#v", tokenPayload)
	}
	authContext, err := runtime.AuthContextService.ResolveAccessToken(ctx, tokenPayload.AccessToken)
	if err != nil {
		t.Fatalf("resolve service access token: %v", err)
	}
	if authContext.Principal.Type != "service_principal" || authContext.Context.Type != "tenant" ||
		authContext.Context.TenantID == nil || *authContext.Context.TenantID != strconv.FormatInt(tenantID, 10) ||
		authContext.Authentication.AssuranceLevel != "not_applicable" || len(authContext.Authentication.Methods) != 1 || authContext.Authentication.Methods[0] != "service_secret" ||
		authContext.Token.Type != "service_access_token" || authContext.Client.ClientID == nil || *authContext.Client.ClientID != "addp-manager" {
		t.Fatalf("service AuthContext = %#v", authContext)
	}
	if len(authContext.Authorization.RoleAssignments) != 1 || authContext.Authorization.RoleAssignments[0].RoleKey != "tenant.manager_runtime" ||
		!slices.Equal(authContext.Authorization.RoleAssignments[0].Permissions, []string{
			"inference.runtime.execute",
			"meta.catalog.read",
			"meta.scan_task.execute",
			"system.engine.read",
			"system.engine_descriptor.read",
		}) {
		t.Fatalf("service authorization = %#v", authContext.Authorization)
	}
	var refreshTokenCount int64
	if err := db.Raw(`SELECT count(*) FROM system.refresh_tokens refresh_token JOIN system.refresh_token_families family ON family.id = refresh_token.family_id WHERE family.principal_id = ? AND family.client_id = 'addp-manager'`, managerPrincipalID).Scan(&refreshTokenCount).Error; err != nil {
		t.Fatalf("count service refresh tokens: %v", err)
	}
	if refreshTokenCount != 0 {
		t.Fatalf("service refresh token count = %d, want 0", refreshTokenCount)
	}
	var tokenAuditCount int64
	if err := db.Raw(`
		SELECT count(*) FROM system.audit_logs
		WHERE event_name = 'oauth.token.issued'
		  AND principal_id = ?
		  AND context_type = 'tenant'
		  AND tenant_id = ?
		  AND details->>'client_id' = 'addp-manager'
		  AND details->>'grant_type' = 'client_credentials'
		  AND details->>'scope' = 'addp.api'
	`, managerPrincipalID, tenantID).Scan(&tokenAuditCount).Error; err != nil {
		t.Fatalf("count service token audit: %v", err)
	}
	if tokenAuditCount != 1 {
		t.Fatalf("service token audit count = %d, want 1", tokenAuditCount)
	}
	assertMetaServiceEngineDetailAgainstPostgres(t, db, runtime, router, secrets["addp-meta"], tenantID, otherTenantID)

	platformTokenResponse := performIAMOAuthPlatformClientCredentialsRequest(t, router, "addp-meta", secrets["addp-meta"])
	if platformTokenResponse.Code != http.StatusOK {
		t.Fatalf("platform client credentials token status = %d body=%s", platformTokenResponse.Code, platformTokenResponse.Body.String())
	}
	var platformTokenPayload struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(platformTokenResponse.Body.Bytes(), &platformTokenPayload); err != nil {
		t.Fatalf("decode platform client credentials token: %v", err)
	}
	platformAuthContext, err := runtime.AuthContextService.ResolveAccessToken(ctx, platformTokenPayload.AccessToken)
	if err != nil {
		t.Fatalf("resolve platform service access token: %v", err)
	}
	if platformAuthContext.Principal.Type != "service_principal" || platformAuthContext.Context.Type != "platform" ||
		platformAuthContext.Context.TenantID != nil || platformAuthContext.Context.TenantMembershipID != nil ||
		len(platformAuthContext.Authorization.RoleAssignments) != 1 ||
		platformAuthContext.Authorization.RoleAssignments[0].RoleKey != "platform.meta_runtime" {
		t.Fatalf("platform service AuthContext = %#v", platformAuthContext)
	}
	if response := performIAMOAuthClientCredentialsFormRequest(t, router, "addp-meta", secrets["addp-meta"], url.Values{
		"context_type": {"platform"},
		"tenant_id":    {strconv.FormatInt(tenantID, 10)},
	}); response.Code != http.StatusBadRequest {
		t.Fatalf("ambiguous service context status = %d body=%s", response.Code, response.Body.String())
	}

	rotatedSecrets := testBuiltinServiceClientSecrets("rotated")
	if err := provisioner.Apply(ctx, rotatedSecrets); err != nil {
		t.Fatalf("rotate service credentials: %v", err)
	}
	if _, err := runtime.AuthContextService.ResolveAccessToken(ctx, tokenPayload.AccessToken); err == nil {
		t.Fatal("service token remained valid after credential rotation")
	}
	if response := performIAMOAuthClientCredentialsRequest(t, router, secrets["addp-manager"], tenantID); response.Code != http.StatusUnauthorized {
		t.Fatalf("old service secret status = %d body=%s", response.Code, response.Body.String())
	}
	if err := db.Exec(`UPDATE system.principals SET status = 'suspended' WHERE id = ?`, managerPrincipalID).Error; err != nil {
		t.Fatalf("suspend manager service principal: %v", err)
	}
	if response := performIAMOAuthClientCredentialsRequest(t, router, rotatedSecrets["addp-manager"], tenantID); response.Code != http.StatusBadRequest {
		t.Fatalf("suspended service principal token status = %d body=%s", response.Code, response.Body.String())
	}
}

func assertMetaServiceEngineDetailAgainstPostgres(
	t *testing.T,
	db *gorm.DB,
	runtime *IAMRuntime,
	oauthRouter *gin.Engine,
	metaSecret string,
	tenantID, otherTenantID int64,
) {
	t.Helper()
	if err := db.Exec(`SET search_path TO system`).Error; err != nil {
		t.Fatalf("set System search_path: %v", err)
	}
	if err := db.AutoMigrate(&models.Engine{}); err != nil {
		t.Fatalf("migrate engine table for contract test: %v", err)
	}
	encryptionKey := []byte("0123456789abcdef0123456789abcdef")
	encryptedPassword, err := commonsecurity.Encrypt("meta-plain-password", encryptionKey)
	if err != nil {
		t.Fatalf("encrypt engine password: %v", err)
	}
	engineTenantID := uint(tenantID)
	engine := &models.Engine{
		TenantID: &engineTenantID, Name: "Meta PostgreSQL", EngineType: "postgresql",
		EngineOrigin: "general", LifecycleState: models.EngineLifecycleActive,
		ConnectionInfo: models.ConnectionInfo{
			"host": "postgres.internal", "port": float64(5432), "password": encryptedPassword,
		},
	}
	engineRepository := repository.NewEngineRepository(db)
	if err := engineRepository.Create(engine); err != nil {
		t.Fatalf("create encrypted engine: %v", err)
	}
	engineHandler := NewEngineHandler(service.NewEngineService(engineRepository, encryptionKey, nil))
	serviceCredential, err := middleware.NewIAMCredentialGuard(middleware.IAMTokenTypeServiceAccess)
	if err != nil {
		t.Fatal(err)
	}
	engineRead, err := middleware.NewIAMPermissionGuard("system.engine.read")
	if err != nil {
		t.Fatal(err)
	}
	oauthRouter.GET(
		"/api/v1/system/engines/:id",
		runtime.Authentication,
		serviceCredential,
		engineRead,
		engineHandler.GetByID,
	)

	tenantToken := performIAMOAuthClientCredentialsFormRequest(t, oauthRouter, "addp-meta", metaSecret, url.Values{
		"tenant_id": {strconv.FormatInt(tenantID, 10)},
	})
	if tenantToken.Code != http.StatusOK {
		t.Fatalf("Meta tenant token status = %d body=%s", tenantToken.Code, tenantToken.Body.String())
	}
	accessToken := decodeIAMAccessToken(t, tenantToken)
	response := performIAMJSONRequest(
		t,
		oauthRouter,
		http.MethodGet,
		fmt.Sprintf("/api/v1/system/engines/%d", engine.ID),
		nil,
		map[string]string{"Authorization": "Bearer " + accessToken},
	)
	if response.Code != http.StatusOK {
		t.Fatalf("Meta engine detail status = %d body=%s", response.Code, response.Body.String())
	}
	var detail struct {
		ConnectionInfo map[string]interface{} `json:"connection_info"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &detail); err != nil {
		t.Fatalf("decode Meta engine detail: %v", err)
	}
	if detail.ConnectionInfo["password"] != "meta-plain-password" {
		t.Fatalf("Meta engine password = %#v, want decrypted connection detail", detail.ConnectionInfo["password"])
	}

	otherTenantToken := performIAMOAuthClientCredentialsFormRequest(t, oauthRouter, "addp-meta", metaSecret, url.Values{
		"tenant_id": {strconv.FormatInt(otherTenantID, 10)},
	})
	if otherTenantToken.Code != http.StatusOK {
		t.Fatalf("other Tenant Meta token status = %d body=%s", otherTenantToken.Code, otherTenantToken.Body.String())
	}
	crossTenant := performIAMJSONRequest(
		t,
		oauthRouter,
		http.MethodGet,
		fmt.Sprintf("/api/v1/system/engines/%d", engine.ID),
		nil,
		map[string]string{"Authorization": "Bearer " + decodeIAMAccessToken(t, otherTenantToken)},
	)
	if crossTenant.Code != http.StatusForbidden {
		t.Fatalf("cross-tenant engine detail status = %d, want 403, body=%s", crossTenant.Code, crossTenant.Body.String())
	}

	userContext := testIAMActorContext("tenant")
	formattedTenantID := strconv.FormatInt(tenantID, 10)
	userContext.Context.TenantID = &formattedTenantID
	userRouter := gin.New()
	userRouter.Use(func(c *gin.Context) {
		if err := sharedauth.SetAuthContextForGin(c, userContext); err != nil {
			t.Fatal(err)
		}
		c.Next()
	})
	userRouter.GET("/engines/:id", engineHandler.GetByID)
	userResponse := performIAMJSONRequest(t, userRouter, http.MethodGet, fmt.Sprintf("/engines/%d", engine.ID), nil, nil)
	if userResponse.Code != http.StatusOK {
		t.Fatalf("user engine detail status = %d body=%s", userResponse.Code, userResponse.Body.String())
	}
	if err := json.Unmarshal(userResponse.Body.Bytes(), &detail); err != nil {
		t.Fatalf("decode user engine detail: %v", err)
	}
	if detail.ConnectionInfo["password"] == "meta-plain-password" || detail.ConnectionInfo["password"] == encryptedPassword {
		t.Fatalf("user engine detail leaked password: %#v", detail.ConnectionInfo["password"])
	}
}

func decodeIAMAccessToken(t *testing.T, response *httptest.ResponseRecorder) string {
	t.Helper()
	var payload struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode access token: %v", err)
	}
	return payload.AccessToken
}

func performIAMOAuthClientCredentialsRequest(t *testing.T, router http.Handler, secret string, tenantID int64) *httptest.ResponseRecorder {
	t.Helper()
	return performIAMOAuthClientCredentialsFormRequest(t, router, "addp-manager", secret, url.Values{
		"tenant_id": {strconv.FormatInt(tenantID, 10)},
	})
}

func performIAMOAuthPlatformClientCredentialsRequest(t *testing.T, router http.Handler, clientID, secret string) *httptest.ResponseRecorder {
	t.Helper()
	return performIAMOAuthClientCredentialsFormRequest(t, router, clientID, secret, url.Values{
		"context_type": {"platform"},
	})
}

func performIAMOAuthClientCredentialsFormRequest(t *testing.T, router http.Handler, clientID, secret string, contextForm url.Values) *httptest.ResponseRecorder {
	t.Helper()
	form := url.Values{
		"grant_type": {"client_credentials"},
		"scope":      {"addp.api"},
		"audience":   {"addp.api"},
	}
	for key, values := range contextForm {
		form[key] = append([]string(nil), values...)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/system/oauth/token", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.SetBasicAuth(clientID, secret)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func testBuiltinServiceClientSecrets(prefix string) map[string]string {
	return map[string]string{
		"addp-agent":        prefix + "-agent-0123456789abcdef0123456789abcdef",
		"addp-asset":        prefix + "-asset-0123456789abcdef0123456789abcdef",
		"addp-copilot":      prefix + "-copilot-0123456789abcdef0123456789abcdef",
		"addp-develop":      prefix + "-develop-0123456789abcdef0123456789abcdef",
		"addp-duckdb":       prefix + "-duckdb-0123456789abcdef0123456789abcdef",
		"addp-gateway":      prefix + "-gateway-0123456789abcdef0123456789abcdef",
		"addp-graph":        prefix + "-graph-0123456789abcdef0123456789abcdef",
		"addp-geopython":    prefix + "-geopython-0123456789abcdef0123456789abcdef",
		"addp-manager":      prefix + "-manager-0123456789abcdef0123456789abcdef",
		"addp-inference":    prefix + "-inference-0123456789abcdef0123456789abcdef",
		"addp-meta":         prefix + "-meta-0123456789abcdef0123456789abcdef",
		"addp-model":        prefix + "-model-0123456789abcdef0123456789abcdef",
		"addp-model3d":      prefix + "-model3d-0123456789abcdef0123456789abcdef",
		"addp-monitor":      prefix + "-monitor-0123456789abcdef0123456789abcdef",
		"addp-orchestrator": prefix + "-orchestrator-0123456789abcdef0123456789abcdef",
		"addp-portal":       prefix + "-portal-0123456789abcdef0123456789abcdef",
		"addp-quality":      prefix + "-quality-0123456789abcdef0123456789abcdef",
		"addp-pointcloud":   prefix + "-pointcloud-0123456789abcdef0123456789abcdef",
		"addp-service":      prefix + "-service-0123456789abcdef0123456789abcdef",
		"addp-standard":     prefix + "-standard-0123456789abcdef0123456789abcdef",
		"addp-spark":        prefix + "-spark-0123456789abcdef0123456789abcdef",
		"addp-transfer":     prefix + "-transfer-0123456789abcdef0123456789abcdef",
	}
}

func TestIAMOAuthHandlerDeviceTokenRevocationAgainstPostgres(t *testing.T) {
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
		t.Fatalf("reset IAM OAuth handler test schema: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := migration.NewRunner(dsn).Run(ctx); err != nil {
		t.Fatalf("apply IAM migrations: %v", err)
	}

	runtime, err := NewIAMRuntime(db, testIAMRuntimeConfig(), testIAMSecurityPolicy())
	if err != nil {
		t.Fatalf("NewIAMRuntime() error = %v", err)
	}
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(runtime.OAuthFailureAudit)
	router.POST("/api/v1/system/oauth/device/code", runtime.OAuthHandler.DeviceCode)
	router.POST("/api/v1/system/oauth/token", runtime.OAuthHandler.Token)
	router.POST("/api/v1/system/oauth/revoke", runtime.OAuthHandler.Revoke)

	invalidDeviceResponse := performIAMOAuthFormRequest(t, router, "/api/v1/system/oauth/device/code", url.Values{
		"client_id": []string{"addp-cli"},
		"scope":     []string{"addp.api"},
	})
	if invalidDeviceResponse.Code != http.StatusBadRequest {
		t.Fatalf("invalid device code status = %d body=%s", invalidDeviceResponse.Code, invalidDeviceResponse.Body.String())
	}

	deviceResponse := performIAMOAuthFormRequest(t, router, "/api/v1/system/oauth/device/code", url.Values{
		"client_id": []string{"addp-cli"},
		"scope":     []string{"addp.api"},
		"audience":  []string{"addp.api"},
	})
	if deviceResponse.Code != http.StatusOK {
		t.Fatalf("device code status = %d body=%s", deviceResponse.Code, deviceResponse.Body.String())
	}
	var devicePayload struct {
		DeviceCode string `json:"device_code"`
		UserCode   string `json:"user_code"`
	}
	decodeIAMResponse(t, deviceResponse, &devicePayload)
	if !strings.HasPrefix(devicePayload.DeviceCode, "addp_dc_") || devicePayload.UserCode == "" {
		t.Fatalf("device response = %#v", devicePayload)
	}

	principalID, tenantID, membershipID, authorizationVersion := seedIAMOAuthHandlerIdentity(t, db)
	authContext := iamOAuthHandlerAuthContext(
		principalID,
		tenantID,
		membershipID,
		authorizationVersion,
	)
	if err := runtime.ConsentBridge.DecideDeviceAuthorization(ctx, iamoauth.DeviceAuthorizationDecisionInput{
		UserCode:    devicePayload.UserCode,
		Approve:     true,
		AuthContext: authContext,
		Audit:       iam.AuditMetadata{},
	}); err != nil {
		t.Fatalf("DecideDeviceAuthorization() error = %v", err)
	}
	time.Sleep(6 * time.Second)

	tokenResponse := performIAMOAuthFormRequest(t, router, "/api/v1/system/oauth/token", url.Values{
		"grant_type":  []string{"urn:ietf:params:oauth:grant-type:device_code"},
		"client_id":   []string{"addp-cli"},
		"device_code": []string{devicePayload.DeviceCode},
	})
	if tokenResponse.Code != http.StatusOK {
		t.Fatalf("token status = %d body=%s", tokenResponse.Code, tokenResponse.Body.String())
	}
	var tokenPayload struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	}
	decodeIAMResponse(t, tokenResponse, &tokenPayload)
	if !strings.HasPrefix(tokenPayload.AccessToken, "addp_at_") ||
		!strings.HasPrefix(tokenPayload.RefreshToken, "addp_rt_") {
		t.Fatalf("token response = %#v", tokenPayload)
	}

	revokeResponse := performIAMOAuthFormRequest(t, router, "/api/v1/system/oauth/revoke", url.Values{
		"client_id":       []string{"addp-cli"},
		"token":           []string{tokenPayload.AccessToken},
		"token_type_hint": []string{"access_token"},
	})
	if revokeResponse.Code != http.StatusOK {
		t.Fatalf("revoke status = %d body=%s", revokeResponse.Code, revokeResponse.Body.String())
	}

	for _, eventName := range []string{
		"oauth.device.code.failed",
		"oauth.device.code.issued",
		"oauth.device.authorization.approved",
		"oauth.token.issued",
		"oauth.token.revoked",
	} {
		var count int64
		if err := db.Raw(`SELECT count(*) FROM system.audit_logs WHERE event_name = ?`, eventName).
			Scan(&count).Error; err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Fatalf("audit event %q count = %d, want 1", eventName, count)
		}
	}
	var revokedFamilies int64
	if err := db.Raw(`
		SELECT count(*) FROM system.refresh_token_families
		WHERE principal_id = ? AND client_id = 'addp-cli'
		  AND auth_type = 'oauth' AND revoked_at IS NOT NULL
	`, principalID).Scan(&revokedFamilies).Error; err != nil {
		t.Fatal(err)
	}
	if revokedFamilies != 1 {
		t.Fatalf("revoked OAuth family count = %d, want 1", revokedFamilies)
	}
}

func performIAMOAuthFormRequest(
	t *testing.T,
	router http.Handler,
	path string,
	form url.Values,
) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func seedIAMOAuthHandlerIdentity(t *testing.T, db *gorm.DB) (int64, int64, int64, int64) {
	t.Helper()
	var principalID, tenantID, membershipID, authorizationVersion int64
	if err := db.Raw(`
		INSERT INTO system.principals (principal_type, status)
		VALUES ('user', 'active') RETURNING id
	`).Scan(&principalID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO system.users (id, display_name) VALUES (?, 'OAuth Handler User')`, principalID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Raw(`
		INSERT INTO system.tenants (code, name, description, status)
		VALUES ('oauth-handler', 'OAuth Handler', '', 'active') RETURNING id
	`).Scan(&tenantID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Raw(`
		INSERT INTO system.tenant_memberships
		    (tenant_id, principal_id, status, source_type, joined_at, created_by_principal_id)
		VALUES (?, ?, 'active', 'manual', now(), ?) RETURNING id
	`, tenantID, principalID, principalID).Scan(&membershipID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Raw(`SELECT authorization_version FROM system.principals WHERE id = ?`, principalID).
		Scan(&authorizationVersion).Error; err != nil {
		t.Fatal(err)
	}
	return principalID, tenantID, membershipID, authorizationVersion
}

func iamOAuthHandlerAuthContext(
	principalID int64,
	tenantID int64,
	membershipID int64,
	authorizationVersion int64,
) commonauth.AuthContext {
	now := time.Now().UTC()
	principalIDText := strconv.FormatInt(principalID, 10)
	tenantIDText := strconv.FormatInt(tenantID, 10)
	membershipIDText := strconv.FormatInt(membershipID, 10)
	clientID := "addp-web"
	return commonauth.AuthContext{
		SchemaVersion: commonauth.AuthContextSchemaVersion,
		Principal:     commonauth.AuthPrincipal{Type: "user", ID: principalIDText},
		Context: commonauth.AuthSessionContext{
			Type:               "tenant",
			TenantID:           &tenantIDText,
			TenantMembershipID: &membershipIDText,
		},
		Authentication: commonauth.AuthenticationFacts{
			Methods:         []string{"password"},
			AssuranceLevel:  "aal1",
			AuthenticatedAt: now.Add(-time.Minute),
		},
		Client: commonauth.ClientConstraints{
			ClientID:  &clientID,
			Audiences: []string{"addp.api"},
			ScopeMode: "unrestricted",
			Scopes:    []string{},
		},
		Organization: commonauth.OrganizationContext{
			Departments:   []commonauth.DepartmentMembership{},
			ProjectGroups: []commonauth.ProjectGroupMembership{},
		},
		Authorization: commonauth.AuthorizationFacts{
			AuthorizationVersion: strconv.FormatInt(authorizationVersion, 10),
			RoleAssignments:      []commonauth.RoleAssignment{},
		},
		Token: commonauth.TokenFacts{
			Type:      "first_party_access_token",
			IssuedAt:  now.Add(-time.Minute),
			ExpiresAt: now.Add(14 * time.Minute),
		},
	}
}
