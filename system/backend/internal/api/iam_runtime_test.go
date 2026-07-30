package api

import (
	"testing"
	"time"

	"github.com/addp/system/internal/config"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestNewIAMRuntimeComposesTargetServicesAndFosite(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:iam-runtime?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	cfg := testIAMRuntimeConfig()
	runtime, err := NewIAMRuntime(db, cfg)
	if err != nil {
		t.Fatalf("NewIAMRuntime() error = %v", err)
	}
	if runtime.Repository == nil || runtime.TokenFamilyService == nil || runtime.IdentityService == nil ||
		runtime.TenantMembershipService == nil || runtime.ContextSelectionService == nil ||
		runtime.TenantInvitationService == nil ||
		runtime.BrowserLoginService == nil || runtime.AuthContextService == nil ||
		runtime.ContextOptionsService == nil || runtime.ContextSwitchService == nil ||
		runtime.LogoutService == nil || runtime.UserSelfService == nil || runtime.DelegationService == nil ||
		runtime.OAuthProvider == nil || runtime.ConsentBridge == nil || runtime.AuthHandler == nil ||
		runtime.OAuthHandler == nil || runtime.DelegationHandler == nil || runtime.UserSelfHandler == nil ||
		runtime.TenantInvitationHandler == nil ||
		runtime.InternalAuditHandler == nil ||
		runtime.Authentication == nil || runtime.FirstPartyCredential == nil ||
		runtime.UserAccessCredential == nil || runtime.BusinessCredential == nil ||
		runtime.ServiceCredential == nil ||
		runtime.OAuthFailureAudit == nil {
		t.Fatalf("IAM Runtime is incomplete: %#v", runtime)
	}
	if runtime.OAuthProvider.Config.TokenURL != "http://localhost:8000/api/v1/system/oauth/token" {
		t.Fatalf("TokenURL = %q", runtime.OAuthProvider.Config.TokenURL)
	}
	if runtime.OAuthProvider.Config.DeviceVerificationURL != "http://localhost:5170/oauth/device" {
		t.Fatalf("DeviceVerificationURL = %q", runtime.OAuthProvider.Config.DeviceVerificationURL)
	}
	if runtime.OAuthProvider.Config.AccessTokenLifespan != 15*time.Minute ||
		runtime.OAuthProvider.Config.RefreshTokenLifespan != 30*24*time.Hour ||
		runtime.OAuthProvider.Config.AuthorizeCodeLifespan != 5*time.Minute ||
		runtime.OAuthProvider.Config.DeviceAndUserCodeLifespan != 10*time.Minute ||
		runtime.OAuthProvider.Config.DeviceAuthTokenPollingInterval != 5*time.Second {
		t.Fatalf("unexpected Fosite lifespans: %#v", runtime.OAuthProvider.Config)
	}
}

func TestNewIAMRuntimeRejectsInvalidProductionComposition(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:iam-runtime-invalid?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	for _, mutate := range []func(*config.Config){
		func(cfg *config.Config) { cfg.AuthorizationCodeMinutes = 6 },
		func(cfg *config.Config) { cfg.PublicAPIURL = "/api" },
		func(cfg *config.Config) { cfg.ConsoleURL = "/console" },
		func(cfg *config.Config) { cfg.OAuthUserCodePepper = nil },
		func(cfg *config.Config) { cfg.DelegatedAccessTokenExpireMinutes = 3 },
	} {
		cfg := testIAMRuntimeConfig()
		mutate(cfg)
		if _, err := NewIAMRuntime(db, cfg); err == nil {
			t.Fatalf("NewIAMRuntime() accepted invalid config: %#v", cfg)
		}
	}
}

func testIAMRuntimeConfig() *config.Config {
	return &config.Config{
		Env:                               "development",
		AccessTokenExpireMinutes:          15,
		DelegatedAccessTokenExpireMinutes: 2,
		ResourceAccessTicketExpireMinutes: 15,
		TenantInvitationExpireHours:       168,
		EnrollmentTicketExpireMinutes:     5,
		RefreshTokenExpireDays:            30,
		AuthorizationCodeMinutes:          5,
		DeviceCodeExpireMinutes:           10,
		DevicePollIntervalSecs:            5,
		OAuthUserCodePepper:               []byte("0123456789abcdef0123456789abcdef"),
		IAMMFAEncryptionKey:               []byte("abcdef0123456789abcdef0123456789"),
		PublicAPIURL:                      "http://localhost:8000",
		ConsoleURL:                        "http://localhost:5170",
	}
}
