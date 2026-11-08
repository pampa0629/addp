package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/addp/common/jsonmap"
)

// metaOwnerQuery is the sole bounded transport for read-only Meta owner queries.
// It neither follows redirects nor replays a command after credential expiry.
func (c *MetaClient) metaOwnerQuery(ctx context.Context, tenantID uint, method, path string, input any, maxBytes, maxDepth int) ([]byte, error) {
	copy := *c.WithTenantID(tenantID)
	transport := *copy.httpClient
	transport.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	copy.httpClient = &transport
	var body []byte
	var err error
	if input != nil {
		body, err = json.Marshal(input)
	}
	if err != nil || len(body) > maxBytes {
		return nil, errors.New("invalid Meta identity query size")
	}
	req, err := http.NewRequestWithContext(ctx, method, copy.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.GetBody = nil
	timing, _ := ctx.Value(metaFactsTimingKey{}).(*metaFactsTiming)
	var started time.Time
	if timing != nil {
		timing.Preparation = time.Since(timing.started)
		started = time.Now()
	}
	response, err := copy.do(req)
	if timing != nil {
		timing.HTTP = time.Since(started)
	}
	if err != nil {
		return nil, &TenantTransportError{Method: method, Path: path, Cause: err}
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		var failure struct {
			ErrorCode string `json:"error_code"`
		}
		_ = json.NewDecoder(io.LimitReader(response.Body, 8192)).Decode(&failure)
		return nil, &TenantAPIError{Method: method, Path: path, StatusCode: response.StatusCode, ErrorCode: failure.ErrorCode}
	}
	if timing != nil {
		started = time.Now()
	}
	encoded, err := io.ReadAll(io.LimitReader(response.Body, int64(maxBytes+1)))
	if timing != nil {
		timing.ResponseRead = time.Since(started)
	}
	if err != nil {
		return nil, errors.New("invalid Meta identity response")
	}
	if timing != nil {
		started = time.Now()
	}
	_, err = jsonmap.DecodeObjectStrict(encoded, maxBytes, maxDepth)
	if timing != nil {
		timing.StrictJSON = time.Since(started)
	}
	if err != nil {
		return nil, errors.New("invalid Meta identity response")
	}
	return encoded, nil
}
