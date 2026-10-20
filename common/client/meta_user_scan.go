package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/addp/common/httptrace"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
)

// MetaScanUserClient submits a scan only within an authenticated User request.
// Credentials are call arguments, never client state or background work.
// MetaClient remains the separate Service-only client.
type MetaScanUserClient struct {
	baseURL    string
	httpClient *http.Client
}

type MetaScanRun struct {
	ExecutionID string `json:"execution_id"`
	Status      string `json:"status"`
}

// MetaScanSubmissionError intentionally excludes dependency bodies and URLs.
type MetaScanSubmissionError struct{ StatusCode int }

func (e *MetaScanSubmissionError) Error() string {
	return fmt.Sprintf("Meta scan submission returned HTTP %d", e.StatusCode)
}

func NewMetaScanUserClient(baseURL string) *MetaScanUserClient {
	return &MetaScanUserClient{baseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"), httpClient: &http.Client{
		Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
}

func (c *MetaScanUserClient) CreateManualScanRun(ctx context.Context, userBearer string, opts MetaScanOptions) (*MetaScanRun, error) {
	if c == nil || c.baseURL == "" || c.httpClient == nil || userBearer == "" || strings.ContainsAny(userBearer, " \t\r\n") {
		return nil, errors.New("Meta scan submission requires a configured client and current User credential")
	}
	body, err := manualMetaScanBody(opts)
	if err != nil {
		return nil, errors.New("invalid Meta scan request")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/v1/meta/scan/run/manual", bytes.NewReader(body))
	if err != nil {
		return nil, errors.New("cannot create Meta scan request")
	}
	request.Header.Set("Authorization", "Bearer "+userBearer)
	request.Header.Set("Content-Type", "application/json")
	response, err := httptrace.Do(c.httpClient, request, c.baseURL)
	if err != nil {
		return nil, errors.New("Meta scan submission unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusCreated {
		return nil, &MetaScanSubmissionError{StatusCode: response.StatusCode}
	}
	const maxBody = 1 << 20
	data, err := io.ReadAll(io.LimitReader(response.Body, maxBody+1))
	if err != nil || len(data) > maxBody {
		return nil, errors.New("invalid Meta scan response")
	}
	var run MetaScanRun
	if json.Unmarshal(data, &run) != nil {
		return nil, errors.New("invalid Meta scan response")
	}
	id, err := uuid.Parse(run.ExecutionID)
	if err != nil || id == uuid.Nil || id.String() != run.ExecutionID || (run.Status != "pending" && run.Status != "running" && run.Status != "success" && run.Status != "failed" && run.Status != "cancelled" && run.Status != "timeout") {
		return nil, errors.New("invalid Meta scan execution identity or status")
	}
	return &run, nil
}

// RefreshItem performs a synchronous known-item refresh as the current User only.
func (c *MetaScanUserClient) RefreshItem(ctx context.Context, userBearer string, itemID uint, opts MetaScanOptions) (*MetaScanResponse, error) {
	if c == nil || c.baseURL == "" || c.httpClient == nil || userBearer == "" || strings.ContainsAny(userBearer, " \t\r\n") {
		return nil, errors.New("Meta item refresh requires a configured client and current User credential")
	}
	if itemID == 0 {
		return nil, fmt.Errorf("item_id is required")
	}
	urlStr := fmt.Sprintf("%s/api/v1/meta/items/%d/refresh", c.baseURL, itemID)
	reqPayload := map[string]interface{}{}
	if opts.EngineID > 0 {
		reqPayload["engine_id"] = opts.EngineID
	}
	if opts.Force {
		reqPayload["force"] = true
	}
	if depth := strings.TrimSpace(opts.ScanDepth); depth != "" {
		reqPayload["scan_depth"] = depth
	}
	triggerType, err := normalizeManualMetaTriggerType(opts.TriggerType)
	if err != nil {
		return nil, err
	}
	reqPayload["trigger_type"] = triggerType
	if source := strings.TrimSpace(opts.Source); source != "" {
		reqPayload["source"] = source
	}

	if opts.Encoding != "" {
		reqPayload["encoding"] = opts.Encoding
	}
	if opts.HasHeader != nil {
		reqPayload["has_header"] = *opts.HasHeader
	}

	body, err := json.Marshal(reqPayload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal refresh request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, "POST", urlStr, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+userBearer)

	transport := *c.httpClient
	transport.Timeout = 60 * time.Second
	resp, err := httptrace.Do(&transport, req, c.baseURL)
	if err != nil {
		return nil, &TenantTransportError{Method: req.Method, Path: req.URL.Path, Cause: err}
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}
	if len(bodyBytes) > 1<<20 {
		return nil, errors.New("Meta item refresh response exceeds limit")
	}
	if resp.StatusCode != http.StatusOK {
		var response struct {
			ErrorCode string `json:"error_code"`
		}
		_ = json.Unmarshal(bodyBytes, &response)
		return nil, &TenantAPIError{Method: req.Method, Path: req.URL.Path, StatusCode: resp.StatusCode, ErrorCode: response.ErrorCode}
	}

	var result MetaScanResponse
	if len(bodyBytes) > 0 {
		if err := json.Unmarshal(bodyBytes, &result); err != nil {
			return nil, fmt.Errorf("failed to decode response: %w", err)
		}
	}
	return &result, nil
}
