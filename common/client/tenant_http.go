package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// tenantHTTPClient is the shared Bearer-only transport for tenant-owned APIs.
// Tenant selection is immutable on the returned client value.
type tenantHTTPClient struct {
	baseURL     string
	httpClient  *http.Client
	tokenSource ServiceTokenProvider
	tenantID    *uint
}

// TenantAPIError preserves the stable HTTP contract returned by a tenant-owned API.
// Response messages are intentionally not retained because callers must not branch on
// localized text or expose an upstream response body to end users.
type TenantAPIError struct {
	Method     string
	Path       string
	StatusCode int
	ErrorCode  string
}

type TenantTransportError struct {
	Method string
	Path   string
	Cause  error
}

func (e *TenantTransportError) Error() string {
	return fmt.Sprintf("tenant API %s %s request failed", e.Method, e.Path)
}

func (e *TenantTransportError) Unwrap() error { return e.Cause }

func (e *TenantAPIError) Error() string {
	return fmt.Sprintf("tenant API %s %s returned HTTP %d", e.Method, e.Path, e.StatusCode)
}

func TenantAPIStatusCode(err error) (int, bool) {
	var apiError *TenantAPIError
	if errors.As(err, &apiError) {
		return apiError.StatusCode, true
	}
	return 0, false
}

func TenantAPIErrorCode(err error) (string, bool) {
	var apiError *TenantAPIError
	if errors.As(err, &apiError) && apiError.ErrorCode != "" {
		return apiError.ErrorCode, true
	}
	return "", false
}

func newTenantHTTPClient(baseURL string, tokenSource ServiceTokenProvider, httpClient *http.Client) tenantHTTPClient {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return tenantHTTPClient{
		baseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"), httpClient: httpClient,
		tokenSource: tokenSource,
	}
}

func (c tenantHTTPClient) withTenantID(tenantID uint) tenantHTTPClient {
	c.tenantID = &tenantID
	return c
}

func (c tenantHTTPClient) doJSON(ctx context.Context, method, path string, payload, result any) error {
	if c.httpClient == nil || c.tokenSource == nil || c.baseURL == "" || c.tenantID == nil || *c.tenantID == 0 {
		return errors.New("tenant service request is not configured")
	}
	token, err := c.tokenSource.Token(ctx, *c.tenantID)
	if err != nil {
		return err
	}
	status, err := c.doJSONWithToken(ctx, method, path, token, payload, result)
	if status != http.StatusUnauthorized {
		return err
	}
	invalidator, ok := c.tokenSource.(ServiceTokenInvalidator)
	if !ok {
		return err
	}
	invalidator.InvalidateToken(*c.tenantID, token)
	token, err = c.tokenSource.Token(ctx, *c.tenantID)
	if err != nil {
		return err
	}
	_, err = c.doJSONWithToken(ctx, method, path, token, payload, result)
	return err
}

func (c tenantHTTPClient) doJSONWithToken(ctx context.Context, method, path, token string, payload, result any) (int, error) {
	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return 0, fmt.Errorf("encode tenant request: %w", err)
		}
		body = strings.NewReader(string(encoded))
	}
	request, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return 0, fmt.Errorf("create tenant request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	response, err := c.httpClient.Do(request)
	if err != nil {
		return 0, &TenantTransportError{Method: method, Path: pathWithoutQuery(path), Cause: err}
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		var errorResponse struct {
			ErrorCode string `json:"error_code"`
		}
		_ = json.NewDecoder(io.LimitReader(response.Body, 64<<10)).Decode(&errorResponse)
		return response.StatusCode, &TenantAPIError{
			Method: method, Path: pathWithoutQuery(path), StatusCode: response.StatusCode,
			ErrorCode: errorResponse.ErrorCode,
		}
	}
	if result == nil || response.StatusCode == http.StatusNoContent {
		return response.StatusCode, nil
	}
	if err := json.NewDecoder(response.Body).Decode(result); err != nil {
		return response.StatusCode, fmt.Errorf("decode tenant response: %w", err)
	}
	return response.StatusCode, nil
}
