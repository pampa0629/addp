package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type federatedQueryHTTPResponse struct {
	Columns []string                 `json:"columns"`
	Rows    []map[string]interface{} `json:"rows"`
	Error   string                   `json:"error,omitempty"`
}

func HTTPExecuteFederatedQuery(
	ctx context.Context,
	connInfo ConnectionInfo,
	req FederatedQueryRequest,
) (*QueryResult, error) {
	baseURL, err := RuntimeBaseURL(connInfo)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.CallerAccessToken) == "" {
		return nil, errors.New("federated query runtime requires a caller access token")
	}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("encode federated query request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/api/v1/queries", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+req.CallerAccessToken)
	httpReq.Header.Set("Content-Type", "application/json")
	timeout := req.Options.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	client := &http.Client{Timeout: timeout + 5*time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("execute federated query: %w", err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var payload federatedQueryHTTPResponse
	if err := json.Unmarshal(respBody, &payload); err != nil {
		return nil, fmt.Errorf("decode federated query response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if payload.Error == "" {
			payload.Error = string(respBody)
		}
		return nil, fmt.Errorf("federated query runtime returned HTTP %d: %s", resp.StatusCode, payload.Error)
	}
	return &QueryResult{Columns: payload.Columns, Rows: payload.Rows}, nil
}
