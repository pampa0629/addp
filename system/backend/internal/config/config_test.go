package config

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestPostgreSQLDSNEscapesCredentials(t *testing.T) {
	cfg := &Config{
		PostgresHost: "localhost", PostgresPort: "15432", PostgresUser: "addp@example",
		PostgresPassword: "p@ss/word", PostgresDB: "addp",
	}
	dsn := cfg.PostgreSQLDSN()
	if !strings.Contains(dsn, "addp%40example:p%40ss%2Fword@localhost:15432/addp") ||
		!strings.Contains(dsn, "sslmode=disable") {
		t.Fatalf("PostgreSQLDSN() = %q", dsn)
	}
}

func TestLoadDefaultsDevelopServiceURLToStandardPort(t *testing.T) {
	t.Setenv("DEVELOP_URL", "")

	cfg := Load()
	if cfg.DevelopServiceURL != "http://localhost:8185" {
		t.Fatalf("expected develop service URL http://localhost:8185, got %s", cfg.DevelopServiceURL)
	}
}

func TestLoadOAuthSecurityDefaults(t *testing.T) {
	t.Setenv("OAUTH_PUBLIC_RATE_LIMIT_PER_MINUTE", "")
	t.Setenv("OAUTH_USER_RATE_LIMIT_PER_MINUTE", "")
	t.Setenv("TRUSTED_PROXIES", "")

	cfg := Load()
	if cfg.OAuthPublicRateLimitPerMinute != 60 || cfg.OAuthUserRateLimitPerMinute != 30 {
		t.Fatalf("OAuth limits = %d/%d", cfg.OAuthPublicRateLimitPerMinute, cfg.OAuthUserRateLimitPerMinute)
	}
	if cfg.TenantInvitationExpireHours != 168 || cfg.EnrollmentTicketExpireMinutes != 5 {
		t.Fatalf("invitation expiration defaults = %d/%d", cfg.TenantInvitationExpireHours, cfg.EnrollmentTicketExpireMinutes)
	}
	if len(cfg.TrustedProxies) != 2 || cfg.TrustedProxies[0] != "127.0.0.1" || cfg.TrustedProxies[1] != "::1" {
		t.Fatalf("trusted proxies = %#v", cfg.TrustedProxies)
	}
	if len(cfg.OAuthUserCodePepper) != 32 || cfg.OAuthPreviousUserCodePepper != nil {
		t.Fatalf("OAuth User Code peppers = %d/%d", len(cfg.OAuthUserCodePepper), len(cfg.OAuthPreviousUserCodePepper))
	}
	if len(cfg.IAMMFAEncryptionKey) != 32 || string(cfg.IAMMFAEncryptionKey) == string(cfg.OAuthUserCodePepper) {
		t.Fatalf("MFA encryption key is missing or reused")
	}
}

func TestLoadOAuthUserCodePepperRotationWindow(t *testing.T) {
	current := []byte("0123456789abcdef0123456789abcdef")
	previous := []byte("abcdef0123456789abcdef0123456789")
	t.Setenv("OAUTH_USER_CODE_PEPPER", base64.StdEncoding.EncodeToString(current))
	t.Setenv("OAUTH_PREVIOUS_USER_CODE_PEPPER", base64.StdEncoding.EncodeToString(previous))

	cfg := Load()
	if string(cfg.OAuthUserCodePepper) != string(current) ||
		string(cfg.OAuthPreviousUserCodePepper) != string(previous) {
		t.Fatalf("OAuth User Code pepper rotation was not loaded")
	}
}

func TestValidateTrustedProxiesRejectsUniversalNetworks(t *testing.T) {
	for _, proxy := range []string{"*", "0.0.0.0/0", "::/0"} {
		cfg := &Config{TrustedProxies: []string{proxy}}
		if err := cfg.ValidateTrustedProxies(); err == nil {
			t.Fatalf("proxy %q was accepted", proxy)
		}
	}
	if err := (&Config{TrustedProxies: []string{"127.0.0.1", "10.0.0.0/8"}}).ValidateTrustedProxies(); err != nil {
		t.Fatalf("valid proxies rejected: %v", err)
	}
}
