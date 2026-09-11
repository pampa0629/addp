package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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

type ServiceTokenError struct {
	Code                string
	StatusCode          int
	Retryable           bool
	ResponseReason      string
	ResponseContentType string
	ResponseBodyBytes   int
}

func (e *ServiceTokenError) Error() string {
	detail := e.Code
	if e.ResponseReason != "" {
		detail += ": " + e.ResponseReason
	}
	if e.StatusCode > 0 {
		return fmt.Sprintf("service token endpoint returned HTTP %d: %s", e.StatusCode, detail)
	}
	return detail
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

type serviceTokenErrorResponse struct {
	Error            string `json:"error"`
	ErrorCode        string `json:"error_code"`
	ErrorDescription string `json:"error_description"`
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
		return "", &ServiceTokenError{Code: "service_token_unavailable", Retryable: true}
	}
	defer response.Body.Close()
	responseBody, readErr := io.ReadAll(io.LimitReader(response.Body, 8192))
	if readErr != nil {
		return "", &ServiceTokenError{Code: "service_token_unavailable", Retryable: true}
	}
	if response.StatusCode != http.StatusOK {
		code := "service_token_rejected"
		var failure serviceTokenErrorResponse
		if err := json.Unmarshal(responseBody, &failure); err == nil {
			code = strings.TrimSpace(failure.ErrorCode)
			if code == "" {
				code = strings.TrimSpace(failure.Error)
			}
			if code == "" {
				code = "service_token_rejected"
			}
		}
		return "", &ServiceTokenError{
			Code: code, StatusCode: response.StatusCode,
			Retryable: response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= http.StatusInternalServerError,
		}
	}
	responseContentType := strings.TrimSpace(response.Header.Get("Content-Type"))
	if len(responseContentType) > 256 {
		responseContentType = responseContentType[:256]
	}
	responseDiagnostics := ServiceTokenError{
		ResponseContentType: responseContentType,
		ResponseBodyBytes:   len(responseBody),
	}
	var envelope any
	if err := json.Unmarshal(responseBody, &envelope); err != nil {
		responseDiagnostics.Code = "service_token_response_malformed"
		responseDiagnostics.Retryable = true
		responseDiagnostics.ResponseReason = "json_decode_failed"
		return "", &responseDiagnostics
	}
	fields, ok := envelope.(map[string]any)
	if !ok {
		responseDiagnostics.Code = "service_token_response_invalid"
		responseDiagnostics.ResponseReason = "top_level_type"
		return "", &responseDiagnostics
	}
	allowedFields := map[string]struct{}{
		"access_token": {},
		"token_type":   {},
		"expires_in":   {},
		"scope":        {},
	}
	for field := range fields {
		if _, allowed := allowedFields[field]; !allowed {
			responseDiagnostics.Code = "service_token_response_invalid"
			responseDiagnostics.ResponseReason = "unexpected_fields"
			return "", &responseDiagnostics
		}
	}
	var payload serviceTokenResponse
	if err := json.Unmarshal(responseBody, &payload); err != nil {
		responseDiagnostics.Code = "service_token_response_invalid"
		responseDiagnostics.ResponseReason = "field_types"
		return "", &responseDiagnostics
	}
	responseDiagnostics.Code = "service_token_response_invalid"
	if !strings.HasPrefix(payload.AccessToken, "addp_at_") || len(payload.AccessToken) == len("addp_at_") {
		responseDiagnostics.ResponseReason = "access_token"
		return "", &responseDiagnostics
	}
	if !strings.EqualFold(payload.TokenType, "Bearer") {
		responseDiagnostics.ResponseReason = "token_type"
		return "", &responseDiagnostics
	}
	if payload.ExpiresIn <= 0 || payload.ExpiresIn > 300 {
		responseDiagnostics.ResponseReason = "expires_in"
		return "", &responseDiagnostics
	}
	if payload.Scope != "" && payload.Scope != "addp.api" {
		responseDiagnostics.ResponseReason = "scope"
		return "", &responseDiagnostics
	}
	s.cache[cacheKey] = cachedServiceToken{value: payload.AccessToken, expiresAt: now.Add(time.Duration(payload.ExpiresIn) * time.Second)}
	return payload.AccessToken, nil
}
