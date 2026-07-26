package oauth

import (
	"context"
	"testing"
	"time"

	"github.com/ory/fosite"
)

func TestProviderConfigBuildsStrictFositeConfig(t *testing.T) {
	config, err := (ProviderConfig{
		AccessTokenLifespan:   15 * time.Minute,
		RefreshTokenLifespan:  30 * 24 * time.Hour,
		AuthorizeCodeLifespan: 5 * time.Minute,
		DeviceCodeLifespan:    10 * time.Minute,
		DevicePollingInterval: 5 * time.Second,
		DeviceVerificationURL: "http://localhost:5170/oauth/device",
		TokenEndpointURL:      "http://localhost:8000/api/v1/system/oauth/token",
	}).Fosite()
	if err != nil {
		t.Fatalf("Fosite() error = %v", err)
	}
	if !config.GetEnforcePKCE(context.Background()) ||
		!config.GetEnforcePKCEForPublicClients(context.Background()) ||
		config.GetEnablePKCEPlainChallengeMethod(context.Background()) ||
		config.GetTokenEntropy(context.Background()) != minimumTokenEntropy ||
		config.GetSendDebugMessagesToClients(context.Background()) {
		t.Fatalf("unexpected Fosite security config: %#v", config)
	}
	if !config.GetScopeStrategy(context.Background())([]string{"data.read"}, "data.read") ||
		config.GetScopeStrategy(context.Background())([]string{"data.read"}, "data") {
		t.Fatal("scope strategy is not exact matching")
	}
	if err := config.GetAudienceStrategy(context.Background())([]string{"addp.api"}, []string{"addp.api"}); err != nil {
		t.Fatalf("exact audience rejected: %v", err)
	}
	if err := config.GetAudienceStrategy(context.Background())([]string{"https://api.example.com/path"}, []string{"https://api.example.com"}); err == nil {
		t.Fatal("audience strategy accepted a non-exact audience")
	}
	if scopes := config.GetRefreshTokenScopes(context.Background()); scopes == nil || len(scopes) != 0 {
		t.Fatalf("refresh token scopes = %#v, want explicit empty set", scopes)
	}
	_ = fosite.AccessToken
}

func TestProviderConfigRejectsInvalidEndpointsAndIntervals(t *testing.T) {
	base := ProviderConfig{
		AccessTokenLifespan:   time.Minute,
		RefreshTokenLifespan:  time.Hour,
		AuthorizeCodeLifespan: time.Minute,
		DeviceCodeLifespan:    time.Minute,
		DevicePollingInterval: 5 * time.Second,
		DeviceVerificationURL: "http://localhost/device",
		TokenEndpointURL:      "http://localhost/token",
	}

	invalid := base
	invalid.DevicePollingInterval = 4 * time.Second
	if _, err := invalid.Fosite(); err == nil {
		t.Fatal("Fosite() accepted a polling interval below RFC 8628 baseline")
	}
	invalid = base
	invalid.TokenEndpointURL = "/token"
	if _, err := invalid.Fosite(); err == nil {
		t.Fatal("Fosite() accepted a relative token endpoint")
	}
}
