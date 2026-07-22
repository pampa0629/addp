package config

import "testing"

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
	if len(cfg.TrustedProxies) != 2 || cfg.TrustedProxies[0] != "127.0.0.1" || cfg.TrustedProxies[1] != "::1" {
		t.Fatalf("trusted proxies = %#v", cfg.TrustedProxies)
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
