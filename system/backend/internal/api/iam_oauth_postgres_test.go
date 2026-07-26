package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	commonauth "github.com/addp/common/authorization"
	"github.com/addp/system/internal/iam"
	iamoauth "github.com/addp/system/internal/iam/oauth"
	"github.com/addp/system/internal/migration"
	"github.com/addp/system/internal/testsupport"
	"github.com/gin-gonic/gin"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestIAMOAuthHandlerDeviceTokenRevocationAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("ADDP_IAM_RUNTIME_TEST_DSN")
	if dsn == "" {
		t.Skip("set ADDP_IAM_RUNTIME_TEST_DSN to a disposable PostgreSQL 15+ database")
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

	runtime, err := NewIAMRuntime(db, testIAMRuntimeConfig())
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
