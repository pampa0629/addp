package service

import (
	"errors"
	"net/url"
	"testing"
	"time"

	"github.com/addp/system/internal/config"
	"github.com/addp/system/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newTokenServiceTestDB(t *testing.T) (*gorm.DB, *TokenService, *models.User) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.Exec("ATTACH DATABASE ':memory:' AS system").Error; err != nil {
		t.Fatalf("attach system schema: %v", err)
	}
	if err := db.AutoMigrate(&models.Tenant{}, &models.User{}); err != nil {
		t.Fatalf("migrate token models: %v", err)
	}
	createTokenTestTables(t, db)
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
	authContext := NewAuthContextService(repositoryUserAdapter{db: db}, repositoryTenantAdapter{db: db})
	service := NewTokenService(db, authContext, nil, &config.Config{
		AccessTokenExpireMinutes:          15,
		DelegatedAccessTokenExpireMinutes: 2,
		ResourceAccessTicketExpireMinutes: 15,
		RefreshTokenExpireDays:            30,
		AuthorizationCodeMinutes:          5,
		DeviceCodeExpireMinutes:           10,
		DevicePollIntervalSecs:            5,
		ConsoleURL:                        "http://localhost:5170",
	})
	return db, service, user
}

func createTokenTestTables(t *testing.T, db *gorm.DB) {
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
			t.Fatalf("create token test table: %v", err)
		}
	}
}

type repositoryUserAdapter struct{ db *gorm.DB }

func (r repositoryUserAdapter) GetByID(id uint) (*models.User, error) {
	var user models.User
	return &user, r.db.First(&user, id).Error
}

type repositoryTenantAdapter struct{ db *gorm.DB }

func (r repositoryTenantAdapter) GetByID(id uint) (*models.Tenant, error) {
	var tenant models.Tenant
	return &tenant, r.db.First(&tenant, id).Error
}

func TestRefreshTokenRotationAndReuseRevokesFamily(t *testing.T) {
	db, service, user := newTokenServiceTestDB(t)
	pair, err := service.IssueFirstParty(user)
	if err != nil {
		t.Fatalf("issue first-party pair: %v", err)
	}
	context, err := service.ResolveAccessToken(pair.AccessToken)
	if err != nil || context.UserID != user.ID {
		t.Fatalf("resolve access token: context=%#v err=%v", context, err)
	}
	managerTicket := pair.ResourceAccessTickets["manager"]
	if managerTicket == "" || pair.ResourceAccessTickets["standard"] == "" {
		t.Fatalf("resource access tickets = %#v", pair.ResourceAccessTickets)
	}
	if pair.ResourceAccessTicketExpiresIn != pair.AccessExpiresIn {
		t.Fatalf("resource ticket expires in = %d, access expires in = %d", pair.ResourceAccessTicketExpiresIn, pair.AccessExpiresIn)
	}
	var storedTicket models.ResourceAccessTicket
	if err := db.Where("owner = ?", "manager").First(&storedTicket).Error; err != nil {
		t.Fatalf("load stored resource ticket: %v", err)
	}
	if storedTicket.TokenHash == managerTicket || storedTicket.TokenHash != hashToken(managerTicket) {
		t.Fatalf("resource ticket was not stored as SHA-256 hash: %q", storedTicket.TokenHash)
	}
	resourceContext, err := service.ResolveAccessToken(managerTicket)
	if err != nil || resourceContext.AuthType != models.AuthTypeResourceAccessTicket ||
		len(resourceContext.Audiences) != 1 || resourceContext.Audiences[0] != "manager" ||
		len(resourceContext.Scopes) != 1 || resourceContext.Scopes[0] != models.BrowserResourceAccessScope {
		t.Fatalf("resolve resource ticket: context=%#v err=%v", resourceContext, err)
	}
	rotated, err := service.RotateWebRefreshToken(pair.RefreshToken)
	if err != nil {
		t.Fatalf("rotate refresh token: %v", err)
	}
	if rotated.RefreshToken == pair.RefreshToken || rotated.AccessToken == pair.AccessToken {
		t.Fatal("token rotation reused plaintext token")
	}
	if rotated.ResourceAccessTickets["manager"] == managerTicket {
		t.Fatal("resource access ticket was not rotated")
	}
	if _, err := service.ResolveAccessToken(managerTicket); !errors.Is(err, ErrInvalidAccessToken) {
		t.Fatalf("old resource access ticket after rotation = %v", err)
	}
	if _, err := service.RotateWebRefreshToken(pair.RefreshToken); !errors.Is(err, ErrRefreshTokenReuse) {
		t.Fatalf("reuse error = %v, want ErrRefreshTokenReuse", err)
	}
	if _, err := service.ResolveAccessToken(rotated.AccessToken); !errors.Is(err, ErrInvalidAccessToken) {
		t.Fatalf("rotated access token after family revoke = %v", err)
	}
	if _, err := service.ResolveAccessToken(rotated.ResourceAccessTickets["manager"]); !errors.Is(err, ErrInvalidAccessToken) {
		t.Fatalf("resource access ticket after family revoke = %v", err)
	}
}

func TestAuthorizationCodeRequiresS256AndIsSingleUse(t *testing.T) {
	_, service, user := newTokenServiceTestDB(t)
	verifier := "a-valid-pkce-verifier-with-more-than-forty-three-characters-123"
	redirectURI := "http://127.0.0.1:43123/callback"
	redirectURL, err := service.DecideAuthorization(user.ID, &models.OAuthAuthorizationRequest{
		ClientID: "addp-cli", RedirectURI: redirectURI, Scope: "addp.api", State: "state", CodeChallenge: pkceChallenge(verifier), CodeChallengeMethod: "S256", Decision: models.OAuthAuthorizationDecisionApproved,
	})
	if err != nil {
		t.Fatalf("create authorization code: %v", err)
	}
	code := redirectQueryValue(t, redirectURL, "code")
	pair, err := service.ExchangeAuthorizationCode("addp-cli", code, redirectURI, verifier)
	if err != nil {
		t.Fatalf("exchange authorization code: %v", err)
	}
	if len(pair.ResourceAccessTickets) != 0 || pair.ResourceAccessTicketExpiresIn != 0 {
		t.Fatalf("OAuth token exchange issued browser resource tickets: %#v", pair)
	}
	if _, err := service.ExchangeAuthorizationCode("addp-cli", code, redirectURI, verifier); !errors.Is(err, ErrInvalidAuthorizationCode) {
		t.Fatalf("second exchange error = %v", err)
	}
}

func TestAuthorizationRejectionUsesValidatedRedirectWithoutIssuingCode(t *testing.T) {
	db, service, user := newTokenServiceTestDB(t)
	verifier := "a-valid-pkce-verifier-with-more-than-forty-three-characters-123"
	request := &models.OAuthAuthorizationRequest{
		ClientID: "addp-cli", RedirectURI: "http://127.0.0.1:43124/callback", Scope: "addp.api", State: "state", CodeChallenge: pkceChallenge(verifier), CodeChallengeMethod: "S256", Decision: models.OAuthAuthorizationDecisionRejected,
	}

	redirectURL, err := service.DecideAuthorization(user.ID, request)
	if err != nil {
		t.Fatalf("reject authorization: %v", err)
	}
	if got := redirectQueryValue(t, redirectURL, "error"); got != "access_denied" {
		t.Fatalf("redirect error = %q, want access_denied", got)
	}
	if got := redirectQueryValue(t, redirectURL, "state"); got != "state" {
		t.Fatalf("redirect state = %q, want state", got)
	}
	var count int64
	if err := db.Model(&models.OAuthAuthorizationCode{}).Count(&count).Error; err != nil {
		t.Fatalf("count authorization codes: %v", err)
	}
	if count != 0 {
		t.Fatalf("authorization code count = %d, want 0", count)
	}

	request.RedirectURI = "https://attacker.example/callback"
	if _, err := service.DecideAuthorization(user.ID, request); !errors.Is(err, ErrInvalidRedirectURI) {
		t.Fatalf("invalid rejection redirect error = %v, want ErrInvalidRedirectURI", err)
	}
}

func TestOAuthRevokeRequiresMatchingClient(t *testing.T) {
	_, tokenService, user := newTokenServiceTestDB(t)
	clientID := "addp-cli"
	pair, err := tokenService.issueNewFamily(
		user.ID, user.TenantID, &clientID, models.AuthTypeOAuthAccessToken, []string{"addp-api"}, []string{"addp.api"},
	)
	if err != nil {
		t.Fatalf("issue OAuth family: %v", err)
	}
	if err := tokenService.RevokeOAuthRefreshToken(pair.RefreshToken, "other-client"); !errors.Is(err, ErrInvalidOAuthClient) {
		t.Fatalf("mismatched client error = %v", err)
	}
	if _, err := tokenService.RotateOAuthRefreshToken(pair.RefreshToken, clientID); err != nil {
		t.Fatalf("mismatched revoke changed family: %v", err)
	}

	pair, err = tokenService.issueNewFamily(
		user.ID, user.TenantID, &clientID, models.AuthTypeOAuthAccessToken, []string{"addp-api"}, []string{"addp.api"},
	)
	if err != nil {
		t.Fatalf("issue second OAuth family: %v", err)
	}
	if err := tokenService.RevokeOAuthRefreshToken(pair.RefreshToken, clientID); err != nil {
		t.Fatalf("revoke OAuth family: %v", err)
	}
	if _, err := tokenService.RotateOAuthRefreshToken(pair.RefreshToken, clientID); !errors.Is(err, ErrInvalidRefreshToken) {
		t.Fatalf("revoked refresh error = %v", err)
	}
}

func TestRedirectURIAllowsOnlyRFC8252LoopbackPortVariation(t *testing.T) {
	registered := []string{
		"http://127.0.0.1/callback",
		"http://[::1]/callback",
		"http://127.0.0.1/callback?channel=cli",
		"https://client.example/callback",
	}
	tests := []struct {
		name      string
		requested string
		allowed   bool
	}{
		{name: "dynamic loopback port", requested: "http://127.0.0.1:43123/callback", allowed: true},
		{name: "dynamic IPv6 loopback port", requested: "http://[::1]:43123/callback", allowed: true},
		{name: "matching registered query", requested: "http://127.0.0.1:43123/callback?channel=cli", allowed: true},
		{name: "exact non-loopback", requested: "https://client.example/callback", allowed: true},
		{name: "localhost hostname", requested: "http://localhost:43123/callback"},
		{name: "different loopback IP", requested: "http://127.0.0.2:43123/callback"},
		{name: "https loopback", requested: "https://127.0.0.1:43123/callback"},
		{name: "different path", requested: "http://127.0.0.1:43123/other"},
		{name: "query added", requested: "http://127.0.0.1:43123/callback?next=/admin"},
		{name: "userinfo added", requested: "http://user@127.0.0.1:43123/callback"},
		{name: "fragment added", requested: "http://127.0.0.1:43123/callback#fragment"},
		{name: "invalid port", requested: "http://127.0.0.1:70000/callback"},
		{name: "non-loopback port variation", requested: "https://client.example:43123/callback"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := redirectURIAllowed(registered, test.requested); got != test.allowed {
				t.Fatalf("redirectURIAllowed(%q) = %v, want %v", test.requested, got, test.allowed)
			}
		})
	}
}

func TestDeviceFlowPendingSlowDownAndApproval(t *testing.T) {
	_, service, user := newTokenServiceTestDB(t)
	now := time.Now().UTC()
	service.now = func() time.Time { return now }
	device, err := service.CreateDeviceAuthorization("addp-cli", "addp.api")
	if err != nil {
		t.Fatalf("create device authorization: %v", err)
	}
	if _, err := service.ExchangeDeviceCode("addp-cli", device.DeviceCode); !errors.Is(err, ErrAuthorizationPending) {
		t.Fatalf("first poll error = %v", err)
	}
	if _, err := service.ExchangeDeviceCode("addp-cli", device.DeviceCode); !errors.Is(err, ErrSlowDown) {
		t.Fatalf("fast poll error = %v", err)
	}
	if err := service.ApproveDeviceAuthorization(user.ID, device.UserCode, true); err != nil {
		t.Fatalf("approve device: %v", err)
	}
	now = now.Add(5 * time.Second)
	pair, err := service.ExchangeDeviceCode("addp-cli", device.DeviceCode)
	if err != nil || pair.RefreshToken == "" {
		t.Fatalf("approved device exchange: pair=%#v err=%v", pair, err)
	}
	if _, err := service.ExchangeDeviceCode("addp-cli", device.DeviceCode); !errors.Is(err, ErrExpiredToken) {
		t.Fatalf("second exchange error = %v", err)
	}
}

func TestDelegatedAccessTokenBindsToolAndNeverForwardsSourceToken(t *testing.T) {
	db, tokenService, user := newTokenServiceTestDB(t)
	pair, err := tokenService.IssueFirstParty(user)
	if err != nil {
		t.Fatalf("issue first-party pair: %v", err)
	}
	issued, err := tokenService.IssueDelegatedAccessToken(pair.AccessToken, &models.DelegatedAccessTokenRequest{
		Audience:   "develop",
		Scopes:     []string{"workflow.validate"},
		AgentRunID: "run-1",
		ToolCallID: "call-1",
	})
	if err != nil {
		t.Fatalf("issue delegated token: %v", err)
	}
	if issued.AccessToken == pair.AccessToken || issued.ExpiresIn != 120 {
		t.Fatalf("delegated token response = %#v", issued)
	}
	var stored models.DelegatedAccessToken
	if err := db.Where("agent_run_id = ? AND tool_call_id = ?", "run-1", "call-1").First(&stored).Error; err != nil {
		t.Fatalf("load delegated token: %v", err)
	}
	if stored.TokenHash == issued.AccessToken || stored.TokenHash != hashToken(issued.AccessToken) {
		t.Fatalf("delegated token was not stored as SHA-256 hash: %q", stored.TokenHash)
	}
	if stored.DelegatedBy != "addp-web" || stored.SourceAccessTokenID == "" {
		t.Fatalf("delegated audit source = %#v", stored)
	}
	context, err := tokenService.ResolveAccessToken(issued.AccessToken)
	if err != nil {
		t.Fatalf("resolve delegated token: %v", err)
	}
	if context.AuthType != models.AuthTypeDelegatedAccessToken ||
		len(context.Audiences) != 1 || context.Audiences[0] != "develop" ||
		len(context.Scopes) != 1 || context.Scopes[0] != "workflow.validate" ||
		context.DelegatedBy == nil || *context.DelegatedBy != "addp-web" ||
		context.AgentRunID == nil || *context.AgentRunID != "run-1" ||
		context.ToolCallID == nil || *context.ToolCallID != "call-1" {
		t.Fatalf("delegated AuthContext = %#v", context)
	}
	if _, err := tokenService.IssueDelegatedAccessToken(pair.AccessToken, &models.DelegatedAccessTokenRequest{
		Audience: "develop", Scopes: []string{"users.delete"}, AgentRunID: "run-2", ToolCallID: "call-2",
	}); !errors.Is(err, ErrInvalidScope) {
		t.Fatalf("unregistered delegated scope error = %v", err)
	}
	if _, err := tokenService.IssueDelegatedAccessToken(issued.AccessToken, &models.DelegatedAccessTokenRequest{
		Audience: "develop", Scopes: []string{"workflow.validate"}, AgentRunID: "run-2", ToolCallID: "call-2",
	}); !errors.Is(err, ErrInvalidAccessToken) {
		t.Fatalf("delegation chaining error = %v", err)
	}
	if err := tokenService.RevokeRefreshToken(pair.RefreshToken); err != nil {
		t.Fatalf("revoke source family: %v", err)
	}
	if err := db.First(&stored, "id = ?", stored.ID).Error; err != nil || stored.RevokedAt == nil {
		t.Fatalf("delegated token was not revoked with source family: token=%#v err=%v", stored, err)
	}
	if _, err := tokenService.ResolveAccessToken(issued.AccessToken); !errors.Is(err, ErrInvalidAccessToken) {
		t.Fatalf("delegated token after source revoke = %v", err)
	}
}

func TestOAuthDelegationDerivesDelegatedByFromClient(t *testing.T) {
	_, tokenService, user := newTokenServiceTestDB(t)
	clientID := "addp-cli"
	pair, err := tokenService.issueNewFamily(
		user.ID,
		user.TenantID,
		&clientID,
		models.AuthTypeOAuthAccessToken,
		[]string{"addp-api"},
		[]string{"addp.api"},
	)
	if err != nil {
		t.Fatalf("issue OAuth pair: %v", err)
	}
	issued, err := tokenService.IssueDelegatedAccessToken(pair.AccessToken, &models.DelegatedAccessTokenRequest{
		Audience: "manager", Scopes: []string{"data.search"}, AgentRunID: "external-run", ToolCallID: "external-call",
	})
	if err != nil {
		t.Fatalf("issue OAuth delegation: %v", err)
	}
	context, err := tokenService.ResolveAccessToken(issued.AccessToken)
	if err != nil || context.ClientID == nil || *context.ClientID != clientID || context.DelegatedBy == nil || *context.DelegatedBy != clientID {
		t.Fatalf("OAuth delegated context = %#v err=%v", context, err)
	}
}

func redirectQueryValue(t *testing.T, rawURL, key string) string {
	t.Helper()
	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse redirect url: %v", err)
	}
	return parsed.Query().Get(key)
}
