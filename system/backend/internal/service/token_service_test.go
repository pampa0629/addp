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
	client := models.OAuthClient{ClientID: "addp-cli", Name: "ADDP CLI", ClientType: models.OAuthClientTypePublic, RedirectURIs: []string{"http://127.0.0.1:8765/callback"}, AllowedScopes: []string{"addp.api"}, DeviceFlowEnabled: true, IsActive: true}
	if err := db.Create(&client).Error; err != nil {
		t.Fatalf("create oauth client: %v", err)
	}
	authContext := NewAuthContextService(repositoryUserAdapter{db: db}, repositoryTenantAdapter{db: db})
	service := NewTokenService(db, authContext, nil, &config.Config{
		AccessTokenExpireMinutes: 15,
		RefreshTokenExpireDays:   30,
		AuthorizationCodeMinutes: 5,
		DeviceCodeExpireMinutes:  10,
		DevicePollIntervalSecs:   5,
		ConsoleURL:               "http://localhost:5170",
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
	_, service, user := newTokenServiceTestDB(t)
	pair, err := service.IssueFirstParty(user)
	if err != nil {
		t.Fatalf("issue first-party pair: %v", err)
	}
	context, err := service.ResolveAccessToken(pair.AccessToken)
	if err != nil || context.UserID != user.ID {
		t.Fatalf("resolve access token: context=%#v err=%v", context, err)
	}
	rotated, err := service.RotateWebRefreshToken(pair.RefreshToken)
	if err != nil {
		t.Fatalf("rotate refresh token: %v", err)
	}
	if rotated.RefreshToken == pair.RefreshToken || rotated.AccessToken == pair.AccessToken {
		t.Fatal("token rotation reused plaintext token")
	}
	if _, err := service.RotateWebRefreshToken(pair.RefreshToken); !errors.Is(err, ErrRefreshTokenReuse) {
		t.Fatalf("reuse error = %v, want ErrRefreshTokenReuse", err)
	}
	if _, err := service.ResolveAccessToken(rotated.AccessToken); !errors.Is(err, ErrInvalidAccessToken) {
		t.Fatalf("rotated access token after family revoke = %v", err)
	}
}

func TestAuthorizationCodeRequiresS256AndIsSingleUse(t *testing.T) {
	_, service, user := newTokenServiceTestDB(t)
	verifier := "a-valid-pkce-verifier-with-more-than-forty-three-characters-123"
	redirectURL, err := service.CreateAuthorizationCode(user.ID, &models.OAuthAuthorizationRequest{
		ClientID: "addp-cli", RedirectURI: "http://127.0.0.1:8765/callback", Scope: "addp.api", State: "state", CodeChallenge: pkceChallenge(verifier), CodeChallengeMethod: "S256",
	})
	if err != nil {
		t.Fatalf("create authorization code: %v", err)
	}
	code := redirectQueryValue(t, redirectURL, "code")
	if _, err := service.ExchangeAuthorizationCode("addp-cli", code, "http://127.0.0.1:8765/callback", verifier); err != nil {
		t.Fatalf("exchange authorization code: %v", err)
	}
	if _, err := service.ExchangeAuthorizationCode("addp-cli", code, "http://127.0.0.1:8765/callback", verifier); !errors.Is(err, ErrInvalidAuthorizationCode) {
		t.Fatalf("second exchange error = %v", err)
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
}

func redirectQueryValue(t *testing.T, rawURL, key string) string {
	t.Helper()
	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse redirect url: %v", err)
	}
	return parsed.Query().Get(key)
}
