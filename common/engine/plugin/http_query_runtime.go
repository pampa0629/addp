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

	"github.com/addp/common/datatype"
	commoni18n "github.com/addp/common/middleware/i18n"
)

type federatedQueryHTTPResponse struct {
	Columns []string                 `json:"columns"`
	Fields  []datatype.FieldInfo     `json:"fields"`
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
	httpReq.Header.Set("Accept-Language", commoni18n.Language(ctx))
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
	decoder := json.NewDecoder(bytes.NewReader(respBody))
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode federated query response: %w", err)
	}
	if decoder.Decode(new(interface{})) != io.EOF {
		return nil, commoni18n.NewMessageError("engine.query_runtime.output_invalid")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if payload.Error == "" {
			payload.Error = string(respBody)
		}
		return nil, fmt.Errorf("federated query runtime returned HTTP %d: %s", resp.StatusCode, payload.Error)
	}
	if len(payload.Columns) != len(payload.Fields) {
		return nil, commoni18n.NewMessageError("engine.query_runtime.output_invalid")
	}
	seen := make(map[string]bool, len(payload.Columns))
	for i, name := range payload.Columns {
		if seen[name] || payload.Fields[i].Name != name || !datatype.IsKnownFieldType(payload.Fields[i].Type) {
			return nil, commoni18n.NewMessageError("engine.query_runtime.output_invalid")
		}
		seen[name] = true
	}
	return &QueryResult{Columns: payload.Columns, Fields: payload.Fields, Rows: payload.Rows}, nil
}
