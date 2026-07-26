package oauth

import (
	"errors"
	"net/url"
	"time"

	"github.com/ory/fosite"
)

const minimumTokenEntropy = 32

type ProviderConfig struct {
	AccessTokenLifespan        time.Duration
	RefreshTokenLifespan       time.Duration
	AuthorizeCodeLifespan      time.Duration
	DeviceCodeLifespan         time.Duration
	DevicePollingInterval      time.Duration
	DeviceVerificationURL      string
	TokenEndpointURL           string
	SendDebugMessagesToClients bool
}

func (c ProviderConfig) Fosite() (*fosite.Config, error) {
	if c.AccessTokenLifespan <= 0 || c.RefreshTokenLifespan <= 0 ||
		c.AuthorizeCodeLifespan <= 0 || c.DeviceCodeLifespan <= 0 ||
		c.DevicePollingInterval < 5*time.Second {
		return nil, errors.New("OAuth 生命周期或 Device 轮询间隔配置无效")
	}
	if !validAbsoluteHTTPURL(c.DeviceVerificationURL) || !validAbsoluteHTTPURL(c.TokenEndpointURL) {
		return nil, errors.New("OAuth 端点必须是绝对 HTTP(S) URL")
	}

	return &fosite.Config{
		AccessTokenLifespan:            c.AccessTokenLifespan,
		RefreshTokenLifespan:           c.RefreshTokenLifespan,
		AuthorizeCodeLifespan:          c.AuthorizeCodeLifespan,
		DeviceAndUserCodeLifespan:      c.DeviceCodeLifespan,
		DeviceAuthTokenPollingInterval: c.DevicePollingInterval,
		DeviceVerificationURL:          c.DeviceVerificationURL,
		TokenURL:                       c.TokenEndpointURL,
		SendDebugMessagesToClients:     c.SendDebugMessagesToClients,
		ScopeStrategy:                  fosite.ExactScopeStrategy,
		AudienceMatchingStrategy:       fosite.ExactAudienceMatchingStrategy,
		EnforcePKCE:                    true,
		EnforcePKCEForPublicClients:    true,
		EnablePKCEPlainChallengeMethod: false,
		RefreshTokenScopes:             []string{},
		TokenEntropy:                   minimumTokenEntropy,
	}, nil
}

func validAbsoluteHTTPURL(rawURL string) bool {
	parsed, err := url.Parse(rawURL)
	return err == nil && parsed.IsAbs() && parsed.Host != "" &&
		(parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Fragment == ""
}
