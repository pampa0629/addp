package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/addp/common/execution"
	"github.com/addp/common/httptrace"
)

// CheckMetaCollection makes one current authorization observation. It never
// retries POST or follows redirects and never interprets an error as permission.
func (c *SystemServiceClient) CheckMetaCollection(ctx context.Context, tenantID uint, input execution.MetaCollectionAccessRequest) (*execution.MetaCollectionAccessObservation, error) {
	if c == nil || c.baseURL == "" || c.httpClient == nil || c.tenantTokens == nil || tenantID == 0 || (c.tenantID != nil && *c.tenantID != tenantID) || input.Validate() != nil {
		return nil, errors.New("invalid Meta collection authorization request")
	}
	token, err := c.tenantTokens.Token(ctx, tenantID)
	if err != nil || token == "" || strings.ContainsAny(token, " \t\r\n") {
		return nil, errors.New("Meta collection Service credential unavailable")
	}
	raw, err := json.Marshal(input)
	if err != nil {
		return nil, errors.New("invalid Meta collection authorization payload")
	}
	const path = "/api/v1/system/runtime/meta_collection_checks"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(raw))
	if err != nil {
		return nil, errors.New("invalid Meta collection endpoint")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	transport := *c.httpClient
	transport.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := httptrace.Do(&transport, req, c.baseURL)
	if err != nil {
		return nil, errors.New("Meta collection authorization transport unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, &TenantAPIError{Method: http.MethodPost, Path: path, StatusCode: response.StatusCode}
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 8193))
	if err != nil || len(body) > 8192 {
		return nil, errors.New("invalid Meta collection authorization response size")
	}
	var result execution.MetaCollectionAccessObservation
	if json.Unmarshal(body, &result) != nil || result.ExecutionID != input.ExecutionID || result.ExecutionPlanDigest != input.ExecutionPlanDigest {
		return nil, errors.New("invalid Meta collection authorization response")
	}
	return &result, nil
}
