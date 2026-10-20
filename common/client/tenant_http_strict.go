package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"unicode/utf8"
)

// Strict machine-history transport: exact HTTP 200, bounded closed JSON, no
// redirect or implicit mutation retry. Unknown responses are never a miss.
func (c tenantHTTPClient) doStrictJSON(ctx context.Context, method, path string, result any, limit int64) error {
	if c.httpClient == nil || c.tokenSource == nil || c.baseURL == "" || c.tenantID == nil || *c.tenantID == 0 || limit <= 0 {
		return errors.New("strict Tenant request is not configured")
	}
	token, err := c.tokenSource.Token(ctx, *c.tenantID)
	if err != nil {
		return err
	}
	if token == "" {
		return errors.New("strict Tenant request requires Service credential")
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, nil)
	if err != nil {
		return errors.New("invalid strict Tenant request")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	transport := *c.httpClient
	transport.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := transport.Do(req)
	if err != nil {
		return &TenantTransportError{Method: method, Path: path, Cause: err}
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		if response.StatusCode == http.StatusUnauthorized {
			if invalidator, ok := c.tokenSource.(ServiceTokenInvalidator); ok {
				invalidator.InvalidateToken(*c.tenantID, token)
			}
		}
		var failure struct {
			ErrorCode string `json:"error_code"`
		}
		_ = json.NewDecoder(io.LimitReader(response.Body, 64<<10)).Decode(&failure)
		return &TenantAPIError{Method: method, Path: path, StatusCode: response.StatusCode, ErrorCode: failure.ErrorCode}
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil || int64(len(body)) > limit || !utf8.Valid(body) {
		return errors.New("invalid strict Tenant response size or encoding")
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if decoder.Decode(result) != nil || decoder.Decode(new(any)) != io.EOF {
		return errors.New("invalid strict Tenant response")
	}
	return nil
}
