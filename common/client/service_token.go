package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

type ServiceTokenProvider interface {
	Token(ctx context.Context, tenantID uint) (string, error)
}

type PlatformServiceTokenProvider interface {
	PlatformToken(ctx context.Context) (string, error)
}

// ServiceTokenInvalidator removes a rejected tenant token from a provider's
// cache. The rejected token is part of the contract so concurrent callers do
// not evict a newer token obtained by another request.
type ServiceTokenInvalidator interface {
	InvalidateToken(tenantID uint, rejectedToken string)
}

// PlatformServiceTokenInvalidator is the platform-context counterpart of
// ServiceTokenInvalidator.
type PlatformServiceTokenInvalidator interface {
	InvalidatePlatformToken(rejectedToken string)
}

type ServiceTokenProviderFunc func(ctx context.Context, tenantID uint) (string, error)

func (f ServiceTokenProviderFunc) Token(ctx context.Context, tenantID uint) (string, error) {
	return f(ctx, tenantID)
}

type OAuthServiceTokenSource struct {
	tokenURL     string
	clientID     string
	clientSecret string
	httpClient   *http.Client
	mu           sync.Mutex
	cache        map[string]cachedServiceToken
	now          func() time.Time
}

type cachedServiceToken struct {
	value     string
	expiresAt time.Time
}

type serviceTokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int64  `json:"expires_in"`
	Scope       string `json:"scope"`
}

func NewOAuthServiceTokenSource(systemURL, clientID, clientSecret string, httpClient *http.Client) (*OAuthServiceTokenSource, error) {
	systemURL = strings.TrimRight(strings.TrimSpace(systemURL), "/")
	clientID = strings.TrimSpace(clientID)
	if systemURL == "" || clientID == "" || len(clientSecret) < 32 {
		return nil, errors.New("service token source requires System URL, client ID and a 32-byte client secret")
	}
	parsed, err := url.Parse(systemURL)
	if err != nil || !parsed.IsAbs() || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, errors.New("service token source System URL must be an absolute HTTP(S) URL")
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	return &OAuthServiceTokenSource{
		tokenURL: systemURL + "/api/v1/system/oauth/token", clientID: clientID,
		clientSecret: clientSecret, httpClient: httpClient,
		cache: make(map[string]cachedServiceToken), now: time.Now,
	}, nil
}

func (s *OAuthServiceTokenSource) Token(ctx context.Context, tenantID uint) (string, error) {
	if s == nil || tenantID == 0 {
		return "", errors.New("service token requires a tenant ID")
	}
	return s.token(ctx, "tenant:"+strconv.FormatUint(uint64(tenantID), 10), url.Values{
		"tenant_id": {strconv.FormatUint(uint64(tenantID), 10)},
	})
}

func (s *OAuthServiceTokenSource) PlatformToken(ctx context.Context) (string, error) {
	if s == nil {
		return "", errors.New("service token source is required")
	}
	return s.token(ctx, "platform", url.Values{"context_type": {"platform"}})
}

func (s *OAuthServiceTokenSource) InvalidateToken(tenantID uint, rejectedToken string) {
	if s == nil || tenantID == 0 || rejectedToken == "" {
		return
	}
	s.invalidate("tenant:"+strconv.FormatUint(uint64(tenantID), 10), rejectedToken)
}

func (s *OAuthServiceTokenSource) InvalidatePlatformToken(rejectedToken string) {
	if s == nil || rejectedToken == "" {
		return
	}
	s.invalidate("platform", rejectedToken)
}

func (s *OAuthServiceTokenSource) invalidate(cacheKey, rejectedToken string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if cached, exists := s.cache[cacheKey]; exists && cached.value == rejectedToken {
		delete(s.cache, cacheKey)
	}
}

func (s *OAuthServiceTokenSource) token(ctx context.Context, cacheKey string, contextValues url.Values) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now().UTC()
	if cached, exists := s.cache[cacheKey]; exists && cached.expiresAt.After(now.Add(30*time.Second)) {
		return cached.value, nil
	}

	form := url.Values{
		"grant_type": {"client_credentials"},
		"scope":      {"addp.api"},
		"audience":   {"addp.api"},
	}
	for key, values := range contextValues {
		form[key] = append([]string(nil), values...)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, s.tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", fmt.Errorf("create service token request: %w", err)
	}
	request.SetBasicAuth(s.clientID, s.clientSecret)
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := s.httpClient.Do(request)
	if err != nil {
		return "", fmt.Errorf("request service token: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("service token endpoint returned HTTP %d", response.StatusCode)
	}
	var payload serviceTokenResponse
	decoder := json.NewDecoder(response.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil {
		return "", fmt.Errorf("decode service token response: %w", err)
	}
	if !strings.HasPrefix(payload.AccessToken, "addp_at_") ||
		!strings.EqualFold(payload.TokenType, "Bearer") || payload.ExpiresIn <= 0 || payload.ExpiresIn > 300 ||
		(payload.Scope != "" && payload.Scope != "addp.api") {
		return "", errors.New("service token endpoint returned an invalid token response")
	}
	s.cache[cacheKey] = cachedServiceToken{value: payload.AccessToken, expiresAt: now.Add(time.Duration(payload.ExpiresIn) * time.Second)}
	return payload.AccessToken, nil
}
