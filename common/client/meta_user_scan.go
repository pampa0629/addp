package client

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
	response, err := c.httpClient.Do(request)
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
