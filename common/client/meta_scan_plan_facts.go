package client

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/addp/common/execution"
	"github.com/addp/common/httptrace"
)

// ReadScanPlanFacts reads one owner projection with exactly one HTTP request.
// It never retries authentication, follows redirects, or interprets facts as Allow.
func (c *MetaClient) ReadScanPlanFacts(ctx context.Context, tenantID uint, executionID string) (*execution.MetaScanPlanFacts, error) {
	if c == nil || c.httpClient == nil || c.serviceTokenSource == nil || c.baseURL == "" || tenantID == 0 || !canonicalExecutionUUID(executionID) || (c.tenantID != nil && *c.tenantID != tenantID) {
		return nil, errors.New("invalid Meta scan plan facts request")
	}
	credential, err := c.serviceTokenSource.Token(ctx, tenantID)
	if err != nil || credential == "" || strings.ContainsAny(credential, " \t\r\n") {
		return nil, errors.New("Meta scan plan facts Service credential unavailable")
	}
	path := "/api/v1/meta/runtime/scan-executions/" + executionID + "/authorization-facts"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return nil, errors.New("invalid Meta scan plan facts endpoint")
	}
	req.Header.Set("Authorization", "Bearer "+credential)
	transport := *c.httpClient
	transport.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := httptrace.Do(&transport, req, c.baseURL)
	if err != nil {
		return nil, errors.New("Meta scan plan facts transport unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, &TenantAPIError{Method: http.MethodGet, Path: path, StatusCode: response.StatusCode}
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, execution.MaxExecutionConfigBytes+1))
	if err != nil || len(raw) > execution.MaxExecutionConfigBytes {
		return nil, errors.New("invalid Meta scan plan facts response size")
	}
	var facts execution.MetaScanPlanFacts
	if json.Unmarshal(raw, &facts) != nil || facts.ExecutionID != executionID {
		return nil, errors.New("invalid Meta scan plan facts response")
	}
	return &facts, nil
}
