package api

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/addp/system/internal/config"
	"github.com/addp/system/internal/middleware"
	"github.com/addp/system/internal/models"
	"github.com/addp/system/internal/repository"
	"github.com/addp/system/internal/service"
	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestOAuthAuthorizationHTTPFlow(t *testing.T) {
	router, tokenService, user := newOAuthHTTPTestRouter(t)
	firstParty, err := tokenService.IssueFirstParty(user)
	if err != nil {
		t.Fatalf("issue first-party token: %v", err)
	}

	verifier := "a-valid-pkce-verifier-with-more-than-forty-three-characters-123"
	challengeHash := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(challengeHash[:])
	request := models.OAuthAuthorizationRequest{
		ClientID:            "addp-cli",
		RedirectURI:         "http://127.0.0.1:43123/callback",
		Scope:               "addp.api",
		State:               "state-approved",
		CodeChallenge:       challenge,
		CodeChallengeMethod: "S256",
		Decision:            models.OAuthAuthorizationDecisionApproved,
	}

	approved := performAuthorizationRequest(t, router, firstParty.AccessToken, request)
	if approved.Code != http.StatusOK {
		t.Fatalf("approve status = %d, body = %s", approved.Code, approved.Body.String())
	}
	var approvedResponse models.OAuthAuthorizationResponse
	if err := json.Unmarshal(approved.Body.Bytes(), &approvedResponse); err != nil {
		t.Fatalf("decode approval response: %v", err)
	}
	redirect, err := url.Parse(approvedResponse.RedirectURL)
	if err != nil {
		t.Fatalf("parse approval redirect: %v", err)
	}
	if redirect.Query().Get("state") != request.State || redirect.Query().Get("code") == "" {
		t.Fatalf("approval redirect = %s", approvedResponse.RedirectURL)
	}

	form := url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {request.ClientID},
		"code":          {redirect.Query().Get("code")},
		"redirect_uri":  {request.RedirectURI},
		"code_verifier": {verifier},
	}
	exchanged := performTokenRequest(router, form)
	if exchanged.Code != http.StatusOK {
		t.Fatalf("exchange status = %d, body = %s", exchanged.Code, exchanged.Body.String())
	}
	var tokenResponse models.TokenResponse
	if err := json.Unmarshal(exchanged.Body.Bytes(), &tokenResponse); err != nil {
		t.Fatalf("decode token response: %v", err)
	}
	if !strings.HasPrefix(tokenResponse.AccessToken, "addp_at_") || !strings.HasPrefix(tokenResponse.RefreshToken, "addp_rt_") {
		t.Fatalf("unexpected token response: %#v", tokenResponse)
	}

	reused := performTokenRequest(router, form)
	if reused.Code != http.StatusBadRequest || !strings.Contains(reused.Body.String(), `"error":"invalid_grant"`) {
		t.Fatalf("authorization code reuse status = %d, body = %s", reused.Code, reused.Body.String())
	}

	request.State = "state-rejected"
	request.Decision = models.OAuthAuthorizationDecisionRejected
	rejected := performAuthorizationRequest(t, router, firstParty.AccessToken, request)
	if rejected.Code != http.StatusOK {
		t.Fatalf("reject status = %d, body = %s", rejected.Code, rejected.Body.String())
	}
	var rejectedResponse models.OAuthAuthorizationResponse
	if err := json.Unmarshal(rejected.Body.Bytes(), &rejectedResponse); err != nil {
		t.Fatalf("decode rejection response: %v", err)
	}
	rejectionRedirect, err := url.Parse(rejectedResponse.RedirectURL)
	if err != nil {
		t.Fatalf("parse rejection redirect: %v", err)
	}
	if rejectionRedirect.Query().Get("error") != "access_denied" || rejectionRedirect.Query().Get("state") != request.State || rejectionRedirect.Query().Get("code") != "" {
		t.Fatalf("rejection redirect = %s", rejectedResponse.RedirectURL)
	}

	request.RedirectURI = "https://attacker.example/callback"
	invalidRedirect := performAuthorizationRequest(t, router, firstParty.AccessToken, request)
	if invalidRedirect.Code != http.StatusBadRequest || !strings.Contains(invalidRedirect.Body.String(), `"error":"invalid_request"`) {
		t.Fatalf("invalid redirect status = %d, body = %s", invalidRedirect.Code, invalidRedirect.Body.String())
	}
}

func newOAuthHTTPTestRouter(t *testing.T) (*gin.Engine, *service.TokenService, *models.User) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.Exec("ATTACH DATABASE ':memory:' AS system").Error; err != nil {
		t.Fatalf("attach system schema: %v", err)
	}
	if err := db.AutoMigrate(&models.Tenant{}, &models.User{}); err != nil {
		t.Fatalf("migrate identity models: %v", err)
	}
	createOAuthHTTPTestTables(t, db)

	tenant := models.Tenant{Name: "default", IsActive: true}
	if err := db.Create(&tenant).Error; err != nil {
		t.Fatalf("create tenant: %v", err)
	}
	user := &models.User{Username: "alice", Email: "alice@example.com", PasswordHash: "hash", IsActive: true, UserType: models.UserTypeTenantAdmin, TenantID: &tenant.ID}
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	client := models.OAuthClient{ClientID: "addp-cli", Name: "ADDP CLI", ClientType: models.OAuthClientTypePublic, RedirectURIs: []string{"http://127.0.0.1/callback"}, AllowedScopes: []string{"addp.api"}, DeviceFlowEnabled: true, IsActive: true}
	if err := db.Create(&client).Error; err != nil {
		t.Fatalf("create oauth client: %v", err)
	}

	authContext := service.NewAuthContextService(repository.NewUserRepository(db), repository.NewTenantRepository(db))
	tokenService := service.NewTokenService(db, authContext, nil, &config.Config{
		AccessTokenExpireMinutes:          15,
		DelegatedAccessTokenExpireMinutes: 2,
		ResourceAccessTicketExpireMinutes: 15,
		RefreshTokenExpireDays:            30,
		AuthorizationCodeMinutes:          5,
		DeviceCodeExpireMinutes:           10,
		DevicePollIntervalSecs:            5,
		ConsoleURL:                        "http://localhost:5170",
	})
	handler := NewOAuthHandler(tokenService)
	router := gin.New()
	router.POST("/api/v1/system/oauth/token", handler.Token)
	protected := router.Group("")
	protected.Use(middleware.AuthMiddleware(tokenService), middleware.TokenTypePolicy("system", nil))
	protected.POST("/api/v1/system/oauth/authorizations", handler.Authorize)
	return router, tokenService, user
}

func createOAuthHTTPTestTables(t *testing.T, db *gorm.DB) {
	t.Helper()
	statements := []string{
		`CREATE TABLE system.oauth_clients (client_id TEXT PRIMARY KEY, name TEXT NOT NULL, client_type TEXT NOT NULL, redirect_uris TEXT NOT NULL, allowed_scopes TEXT NOT NULL, device_flow_enabled NUMERIC NOT NULL, is_active NUMERIC NOT NULL, created_at DATETIME, updated_at DATETIME)`,
		`CREATE TABLE system.refresh_token_families (id TEXT PRIMARY KEY, user_id INTEGER NOT NULL, tenant_id INTEGER, client_id TEXT, auth_type TEXT NOT NULL, audiences TEXT NOT NULL, scopes TEXT NOT NULL, expires_at DATETIME NOT NULL, revoked_at DATETIME, revoked_reason TEXT, created_at DATETIME, updated_at DATETIME)`,
		`CREATE TABLE system.refresh_tokens (id TEXT PRIMARY KEY, family_id TEXT NOT NULL, token_hash TEXT NOT NULL UNIQUE, parent_token_id TEXT, replaced_by_token_id TEXT, expires_at DATETIME NOT NULL, used_at DATETIME, revoked_at DATETIME, created_at DATETIME)`,
		`CREATE TABLE system.access_tokens (id TEXT PRIMARY KEY, token_hash TEXT NOT NULL UNIQUE, family_id TEXT NOT NULL, user_id INTEGER NOT NULL, tenant_id INTEGER, client_id TEXT, auth_type TEXT NOT NULL, audiences TEXT NOT NULL, scopes TEXT NOT NULL, expires_at DATETIME NOT NULL, revoked_at DATETIME, created_at DATETIME)`,
		`CREATE TABLE system.delegated_access_tokens (id TEXT PRIMARY KEY, token_hash TEXT NOT NULL UNIQUE, source_access_token_id TEXT NOT NULL, user_id INTEGER NOT NULL, tenant_id INTEGER, client_id TEXT, delegated_by TEXT NOT NULL, audience TEXT NOT NULL, scopes TEXT NOT NULL, agent_run_id TEXT NOT NULL, tool_call_id TEXT NOT NULL, expires_at DATETIME NOT NULL, revoked_at DATETIME, created_at DATETIME)`,
		`CREATE TABLE system.resource_access_tickets (id TEXT PRIMARY KEY, token_hash TEXT NOT NULL UNIQUE, family_id TEXT NOT NULL, owner TEXT NOT NULL, expires_at DATETIME NOT NULL, revoked_at DATETIME, created_at DATETIME)`,
		`CREATE TABLE system.oauth_authorization_codes (id TEXT PRIMARY KEY, code_hash TEXT NOT NULL UNIQUE, client_id TEXT NOT NULL, user_id INTEGER NOT NULL, tenant_id INTEGER, redirect_uri TEXT NOT NULL, scopes TEXT NOT NULL, code_challenge TEXT NOT NULL, code_challenge_method TEXT NOT NULL, expires_at DATETIME NOT NULL, used_at DATETIME, created_at DATETIME)`,
		`CREATE TABLE system.oauth_device_authorizations (id TEXT PRIMARY KEY, device_code_hash TEXT NOT NULL UNIQUE, user_code_hash TEXT NOT NULL UNIQUE, client_id TEXT NOT NULL, user_id INTEGER, tenant_id INTEGER, scopes TEXT NOT NULL, status TEXT NOT NULL, interval_secs INTEGER NOT NULL, last_polled_at DATETIME, expires_at DATETIME NOT NULL, created_at DATETIME, updated_at DATETIME)`,
	}
	for _, statement := range statements {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("create oauth test table: %v", err)
		}
	}
}

func performAuthorizationRequest(t *testing.T, router http.Handler, accessToken string, request models.OAuthAuthorizationRequest) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(request)
	if err != nil {
		t.Fatalf("marshal authorization request: %v", err)
	}
	httpRequest := httptest.NewRequest(http.MethodPost, "/api/v1/system/oauth/authorizations", bytes.NewReader(body))
	httpRequest.Header.Set("Authorization", "Bearer "+accessToken)
	httpRequest.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httpRequest)
	return response
}

func performTokenRequest(router http.Handler, form url.Values) *httptest.ResponseRecorder {
	httpRequest := httptest.NewRequest(http.MethodPost, "/api/v1/system/oauth/token", strings.NewReader(form.Encode()))
	httpRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httpRequest)
	return response
}
