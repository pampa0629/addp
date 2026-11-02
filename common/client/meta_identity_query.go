package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/addp/common/jsonmap"
)

// postIdentityQuery is the sole bounded transport for read-only Meta identity POSTs.
// It neither follows redirects nor replays a command after credential expiry.
func (c *MetaClient) postIdentityQuery(ctx context.Context, tenantID uint, path string, input any, maxBytes int) ([]byte, error) {
	copy := *c.WithTenantID(tenantID)
	transport := *copy.httpClient
	transport.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	copy.httpClient = &transport
	body, err := json.Marshal(input)
	if err != nil || len(body) > maxBytes {
		return nil, errors.New("invalid Meta identity query size")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, copy.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.GetBody = nil
	response, err := copy.do(req)
	if err != nil {
		return nil, &TenantTransportError{Method: http.MethodPost, Path: path, Cause: err}
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		var failure struct {
			ErrorCode string `json:"error_code"`
		}
		_ = json.NewDecoder(io.LimitReader(response.Body, 8192)).Decode(&failure)
		return nil, &TenantAPIError{Method: http.MethodPost, Path: path, StatusCode: response.StatusCode, ErrorCode: failure.ErrorCode}
	}
	encoded, err := io.ReadAll(io.LimitReader(response.Body, int64(maxBytes+1)))
	if err != nil {
		return nil, errors.New("invalid Meta identity response")
	}
	if _, err := jsonmap.DecodeObjectStrict(encoded, maxBytes, 8); err != nil {
		return nil, errors.New("invalid Meta identity response")
	}
	return encoded, nil
}
