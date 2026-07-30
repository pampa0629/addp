package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ServiceClient 使用 Tenant Service Access Token 读取 Service 端点投影。
type ServiceClient struct {
	baseURL     string
	httpClient  *http.Client
	tokenSource ServiceTokenProvider
}

type ServiceEndpointInfo struct {
	ServiceType string            `json:"service_type"`
	Title       string            `json:"title"`
	Endpoints   map[string]string `json:"endpoints"`
}

func NewServiceClient(baseURL string, tokenSource ServiceTokenProvider, httpClient *http.Client) *ServiceClient {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &ServiceClient{
		baseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"), httpClient: httpClient, tokenSource: tokenSource,
	}
}

func (c *ServiceClient) GetEndpointsByRef(ctx context.Context, tenantID uint, sourceRef string) (*ServiceEndpointInfo, error) {
	if c == nil || c.httpClient == nil || c.tokenSource == nil || c.baseURL == "" {
		return nil, errors.New("Service client is not configured")
	}
	if tenantID == 0 || strings.TrimSpace(sourceRef) == "" {
		return nil, errors.New("Service endpoint request requires tenant and source reference")
	}

	token, err := c.tokenSource.Token(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("get Service access token: %w", err)
	}
	response, err := c.getEndpoints(ctx, sourceRef, token)
	if err != nil {
		return nil, err
	}
	if response.StatusCode == http.StatusUnauthorized {
		invalidator, ok := c.tokenSource.(ServiceTokenInvalidator)
		if ok {
			_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
			_ = response.Body.Close()
			invalidator.InvalidateToken(tenantID, token)
			token, err = c.tokenSource.Token(ctx, tenantID)
			if err != nil {
				return nil, fmt.Errorf("refresh Service access token: %w", err)
			}
			response, err = c.getEndpoints(ctx, sourceRef, token)
			if err != nil {
				return nil, err
			}
		}
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 8192))
		return nil, fmt.Errorf("Service API returned status %d: %s", response.StatusCode, strings.TrimSpace(string(body)))
	}

	var result ServiceEndpointInfo
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode Service endpoint response: %w", err)
	}
	return &result, nil
}

func (c *ServiceClient) getEndpoints(ctx context.Context, sourceRef, token string) (*http.Response, error) {
	endpoint := c.baseURL + "/api/v1/service/endpoints?ref=" + url.QueryEscape(sourceRef)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("create Service endpoint request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+token)
	response, err := c.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("send Service endpoint request: %w", err)
	}
	return response, nil
}
