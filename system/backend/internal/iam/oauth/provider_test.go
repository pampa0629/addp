package oauth

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestProviderComposesOnlyApprovedFactories(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:oauth-provider?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	providerConfig := ProviderConfig{
		AccessTokenLifespan:   15 * time.Minute,
		RefreshTokenLifespan:  30 * 24 * time.Hour,
		AuthorizeCodeLifespan: 5 * time.Minute,
		DeviceCodeLifespan:    10 * time.Minute,
		DevicePollingInterval: 5 * time.Second,
		DeviceVerificationURL: "http://localhost:5170/oauth/device",
		TokenEndpointURL:      "http://localhost:8000/api/v1/system/oauth/token",
	}
	strategyConfig := StrategyConfig{
		AccessTokenLifespan:   providerConfig.AccessTokenLifespan,
		RefreshTokenLifespan:  providerConfig.RefreshTokenLifespan,
		AuthorizeCodeLifespan: providerConfig.AuthorizeCodeLifespan,
		DeviceCodeLifespan:    providerConfig.DeviceCodeLifespan,
		UserCodePepper:        []byte("0123456789abcdef0123456789abcdef"),
	}
	provider, err := NewProvider(db, providerConfig, strategyConfig)
	if err != nil {
		t.Fatalf("NewProvider() error = %v", err)
	}
	ctx := context.Background()
	if len(provider.Config.GetAuthorizeEndpointHandlers(ctx)) != 2 ||
		len(provider.Config.GetTokenEndpointHandlers(ctx)) != 4 ||
		len(provider.Config.GetRevocationHandlers(ctx)) != 1 ||
		len(provider.Config.GetDeviceEndpointHandlers(ctx)) != 1 ||
		len(provider.Config.GetTokenIntrospectionHandlers(ctx)) != 0 ||
		len(provider.Config.GetPushedAuthorizeEndpointHandlers(ctx)) != 0 {
		t.Fatalf(
			"unexpected handler counts: authorize=%d token=%d revoke=%d device=%d introspect=%d pushed=%d",
			len(provider.Config.GetAuthorizeEndpointHandlers(ctx)),
			len(provider.Config.GetTokenEndpointHandlers(ctx)),
			len(provider.Config.GetRevocationHandlers(ctx)),
			len(provider.Config.GetDeviceEndpointHandlers(ctx)),
			len(provider.Config.GetTokenIntrospectionHandlers(ctx)),
			len(provider.Config.GetPushedAuthorizeEndpointHandlers(ctx)),
		)
	}

	var handlerTypes []string
	for _, handler := range provider.Config.GetAuthorizeEndpointHandlers(ctx) {
		handlerTypes = append(handlerTypes, fmt.Sprintf("%T", handler))
	}
	for _, handler := range provider.Config.GetTokenEndpointHandlers(ctx) {
		handlerTypes = append(handlerTypes, fmt.Sprintf("%T", handler))
	}
	joinedTypes := strings.Join(handlerTypes, "\n")
	for _, forbidden := range []string{"Implicit", "ResourceOwner", "ClientCredentials", "RFC7523", "Pushed", "OpenID"} {
		if strings.Contains(joinedTypes, forbidden) {
			t.Fatalf("forbidden handler %q was composed:\n%s", forbidden, joinedTypes)
		}
	}
}

func TestProviderRejectsDivergentLifespans(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:oauth-provider-invalid?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	providerConfig := ProviderConfig{
		AccessTokenLifespan:   time.Minute,
		RefreshTokenLifespan:  time.Hour,
		AuthorizeCodeLifespan: time.Minute,
		DeviceCodeLifespan:    time.Minute,
		DevicePollingInterval: 5 * time.Second,
		DeviceVerificationURL: "http://localhost/device",
		TokenEndpointURL:      "http://localhost/token",
	}
	strategyConfig := StrategyConfig{
		AccessTokenLifespan:   2 * time.Minute,
		RefreshTokenLifespan:  time.Hour,
		AuthorizeCodeLifespan: time.Minute,
		DeviceCodeLifespan:    time.Minute,
		UserCodePepper:        make([]byte, 32),
	}
	if _, err := NewProvider(db, providerConfig, strategyConfig); err == nil {
		t.Fatal("NewProvider() accepted divergent lifespans")
	}
}
