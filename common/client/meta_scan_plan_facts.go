package client

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/addp/common/execution"
	"github.com/addp/common/httptrace"
)

// ReadScanPlanFacts reads one owner projection with exactly one HTTP request.
// It never retries authentication, follows redirects, or interprets facts as Allow.
func (c *MetaClient) ReadScanPlanFacts(ctx context.Context, tenantID uint, executionID string) (*execution.MetaScanPlanFacts, error) {
	if !canonicalExecutionUUID(executionID) {
		return nil, errors.New("invalid Meta scan plan facts request")
	}
	var facts execution.MetaScanPlanFacts
	if err := c.readScanFacts(ctx, tenantID, "/api/v1/meta/runtime/scan-executions/"+executionID+"/authorization-facts", &facts); err != nil {
		return nil, err
	}
	if facts.ExecutionID != executionID {
		return nil, errors.New("invalid Meta scan plan facts response")
	}
	return &facts, nil
}

// ReadScanTaskFacts does not trigger a scan in order to approve a task.
func (c *MetaClient) ReadScanTaskFacts(ctx context.Context, tenantID uint, taskID string) (*execution.MetaScanTaskFacts, error) {
	id, err := strconv.ParseInt(taskID, 10, 64)
	if err != nil || id <= 0 || strconv.FormatInt(id, 10) != taskID {
		return nil, errors.New("invalid Meta scan task facts request")
	}
	var facts execution.MetaScanTaskFacts
	if err := c.readScanFacts(ctx, tenantID, "/api/v1/meta/runtime/scan-tasks/"+taskID+"/authorization-facts", &facts); err != nil {
		return nil, err
	}
	if facts.TaskID != taskID {
		return nil, errors.New("invalid Meta scan task facts response")
	}
	return &facts, nil
}

func (c *MetaClient) readScanFacts(ctx context.Context, tenantID uint, path string, target any) error {
	if c == nil || c.httpClient == nil || c.serviceTokenSource == nil || c.baseURL == "" || tenantID == 0 || (c.tenantID != nil && *c.tenantID != tenantID) {
		return errors.New("invalid Meta scan facts request")
	}
	credential, err := c.serviceTokenSource.Token(ctx, tenantID)
	if err != nil || credential == "" || strings.ContainsAny(credential, " \t\r\n") {
		return errors.New("Meta scan facts Service credential unavailable")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return errors.New("invalid Meta scan facts endpoint")
	}
	req.Header.Set("Authorization", "Bearer "+credential)
	transport := *c.httpClient
	transport.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := httptrace.Do(&transport, req, c.baseURL)
	if err != nil {
		return errors.New("Meta scan facts transport unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return &TenantAPIError{Method: http.MethodGet, Path: path, StatusCode: response.StatusCode}
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, execution.MaxExecutionConfigBytes+1))
	if err != nil || len(raw) > execution.MaxExecutionConfigBytes {
		return errors.New("invalid Meta scan facts response size")
	}
	if json.Unmarshal(raw, target) != nil {
		return errors.New("invalid Meta scan facts response")
	}
	return nil
}
