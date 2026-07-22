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
	redirectURI := "http://127.0.0.1:43123/callback"
	created := performCreateAuthorizationRequest(router, url.Values{
		"client_id":             {"addp-cli"},
		"redirect_uri":          {redirectURI},
		"scope":                 {"addp.api"},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
	})
	if created.Code != http.StatusCreated {
		t.Fatalf("create authorization request status = %d, body = %s", created.Code, created.Body.String())
	}
	var authorizationRequest models.OAuthAuthorizationRequestCreatedResponse
	if err := json.Unmarshal(created.Body.Bytes(), &authorizationRequest); err != nil {
		t.Fatalf("decode authorization request: %v", err)
	}
	if authorizationRequest.RequestID == "" || !strings.HasPrefix(authorizationRequest.RequestSecret, "addp_ars_") {
		t.Fatalf("unexpected authorization request: %#v", authorizationRequest)
	}

	pending := performGetAuthorizationRequest(router, firstParty.AccessToken, authorizationRequest.RequestID)
	if pending.Code != http.StatusOK || !strings.Contains(pending.Body.String(), `"client_id":"addp-cli"`) {
		t.Fatalf("get authorization request status = %d, body = %s", pending.Code, pending.Body.String())
	}

	approved := performAuthorizationDecision(t, router, firstParty.AccessToken, models.OAuthAuthorizationDecisionRequest{
		RequestID: authorizationRequest.RequestID,
		Decision:  models.OAuthAuthorizationDecisionApproved,
	})
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
	if redirect.Query().Get("state") != authorizationRequest.RequestID || redirect.Query().Get("code") == "" {
		t.Fatalf("approval redirect = %s", approvedResponse.RedirectURL)
	}

	form := url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {"addp-cli"},
		"code":          {redirect.Query().Get("code")},
		"redirect_uri":  {redirectURI},
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

	rejectedRequest := performCreateAuthorizationRequest(router, url.Values{
		"client_id":             {"addp-cli"},
		"redirect_uri":          {"http://127.0.0.1:43124/callback"},
		"scope":                 {"addp.api"},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
	})
	var rejectedAuthorizationRequest models.OAuthAuthorizationRequestCreatedResponse
	if err := json.Unmarshal(rejectedRequest.Body.Bytes(), &rejectedAuthorizationRequest); err != nil {
		t.Fatalf("decode rejected authorization request: %v", err)
	}
	rejected := performAuthorizationDecision(t, router, firstParty.AccessToken, models.OAuthAuthorizationDecisionRequest{
		RequestID: rejectedAuthorizationRequest.RequestID,
		Decision:  models.OAuthAuthorizationDecisionRejected,
	})
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
	if rejectionRedirect.Query().Get("error") != "access_denied" || rejectionRedirect.Query().Get("state") != rejectedAuthorizationRequest.RequestID || rejectionRedirect.Query().Get("code") != "" {
		t.Fatalf("rejection redirect = %s", rejectedResponse.RedirectURL)
	}

	invalidRedirect := performCreateAuthorizationRequest(router, url.Values{
		"client_id":             {"addp-cli"},
		"redirect_uri":          {"https://attacker.example/callback"},
		"scope":                 {"addp.api"},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
	})
	if invalidRedirect.Code != http.StatusBadRequest || !strings.Contains(invalidRedirect.Body.String(), `"error":"invalid_request"`) {
		t.Fatalf("invalid redirect status = %d, body = %s", invalidRedirect.Code, invalidRedirect.Body.String())
	}

	cancelledRequest := performCreateAuthorizationRequest(router, url.Values{
		"client_id":             {"addp-cli"},
		"redirect_uri":          {"http://127.0.0.1:43125/callback"},
		"scope":                 {"addp.api"},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
	})
	var cancelledAuthorizationRequest models.OAuthAuthorizationRequestCreatedResponse
	if err := json.Unmarshal(cancelledRequest.Body.Bytes(), &cancelledAuthorizationRequest); err != nil {
		t.Fatalf("decode cancelled authorization request: %v", err)
	}
	cancelled := performCancelAuthorizationRequest(router, cancelledAuthorizationRequest.RequestID, cancelledAuthorizationRequest.RequestSecret)
	if cancelled.Code != http.StatusNoContent {
		t.Fatalf("cancel authorization request status = %d, body = %s", cancelled.Code, cancelled.Body.String())
	}
	gone := performGetAuthorizationRequest(router, firstParty.AccessToken, cancelledAuthorizationRequest.RequestID)
	if gone.Code != http.StatusGone || !strings.Contains(gone.Body.String(), `"error":"authorization_request_expired"`) {
		t.Fatalf("cancelled authorization request status = %d, body = %s", gone.Code, gone.Body.String())
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
	router.POST("/api/v1/system/oauth/authorization_requests", handler.CreateAuthorizationRequest)
	router.DELETE("/api/v1/system/oauth/authorization_requests/:request_id", handler.CancelAuthorizationRequest)
	router.POST("/api/v1/system/oauth/token", handler.Token)
	protected := router.Group("")
	protected.Use(middleware.AuthMiddleware(tokenService), middleware.TokenTypePolicy("system", nil))
	protected.GET("/api/v1/system/oauth/authorization_requests/:request_id", handler.GetAuthorizationRequest)
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
		`CREATE TABLE system.oauth_authorization_requests (id TEXT PRIMARY KEY, request_secret_hash TEXT NOT NULL, client_id TEXT NOT NULL, redirect_uri TEXT NOT NULL, scopes TEXT NOT NULL, code_challenge TEXT NOT NULL, code_challenge_method TEXT NOT NULL, status TEXT NOT NULL, expires_at DATETIME NOT NULL, completed_at DATETIME, created_at DATETIME, updated_at DATETIME)`,
		`CREATE TABLE system.oauth_authorization_codes (id TEXT PRIMARY KEY, code_hash TEXT NOT NULL UNIQUE, client_id TEXT NOT NULL, user_id INTEGER NOT NULL, tenant_id INTEGER, redirect_uri TEXT NOT NULL, scopes TEXT NOT NULL, code_challenge TEXT NOT NULL, code_challenge_method TEXT NOT NULL, expires_at DATETIME NOT NULL, used_at DATETIME, created_at DATETIME)`,
		`CREATE TABLE system.oauth_device_authorizations (id TEXT PRIMARY KEY, device_code_hash TEXT NOT NULL UNIQUE, user_code_hash TEXT NOT NULL UNIQUE, client_id TEXT NOT NULL, user_id INTEGER, tenant_id INTEGER, scopes TEXT NOT NULL, status TEXT NOT NULL, interval_secs INTEGER NOT NULL, last_polled_at DATETIME, expires_at DATETIME NOT NULL, created_at DATETIME, updated_at DATETIME)`,
	}
	for _, statement := range statements {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("create oauth test table: %v", err)
		}
	}
}

func performAuthorizationDecision(t *testing.T, router http.Handler, accessToken string, request models.OAuthAuthorizationDecisionRequest) *httptest.ResponseRecorder {
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

func performCreateAuthorizationRequest(router http.Handler, form url.Values) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, "/api/v1/system/oauth/authorization_requests", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func performGetAuthorizationRequest(router http.Handler, accessToken, requestID string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodGet, "/api/v1/system/oauth/authorization_requests/"+requestID, nil)
	request.Header.Set("Authorization", "Bearer "+accessToken)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func performCancelAuthorizationRequest(router http.Handler, requestID, requestSecret string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodDelete, "/api/v1/system/oauth/authorization_requests/"+requestID, nil)
	request.Header.Set("Authorization", "Bearer "+requestSecret)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func performTokenRequest(router http.Handler, form url.Values) *httptest.ResponseRecorder {
	httpRequest := httptest.NewRequest(http.MethodPost, "/api/v1/system/oauth/token", strings.NewReader(form.Encode()))
	httpRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httpRequest)
	return response
}
