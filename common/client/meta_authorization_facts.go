package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"

	"github.com/addp/common/dataitem/authorizationfacts"
)

// ReadAuthorizationFacts is the exact, bounded and uncached System-to-Meta contract.
func (c *MetaClient) ReadAuthorizationFacts(ctx context.Context, tenantID uint, fingerprint string) (*authorizationfacts.AuthorizationFacts, error) {
	if c == nil || c.httpClient == nil || c.baseURL == "" || tenantID == 0 || !authorizationfacts.ValidItemFingerprint(fingerprint) {
		return nil, errors.New("invalid Meta authorization facts request")
	}
	copy := *c.WithTenantID(tenantID)
	transport := *copy.httpClient
	transport.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	copy.httpClient = &transport
	path := "/api/v1/meta/runtime/data-items/" + url.PathEscape(fingerprint) + "/authorization-facts"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, copy.baseURL+path, nil)
	if err != nil {
		return nil, err
	}
	response, err := copy.do(req)
	if err != nil {
		return nil, &TenantTransportError{Method: http.MethodGet, Path: path, Cause: err}
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		var failure struct {
			ErrorCode string `json:"error_code"`
		}
		_ = json.NewDecoder(io.LimitReader(response.Body, 8192)).Decode(&failure)
		return nil, &TenantAPIError{Method: http.MethodGet, Path: path, StatusCode: response.StatusCode, ErrorCode: failure.ErrorCode}
	}
	const maxBytes = 64 << 10
	encoded, err := io.ReadAll(io.LimitReader(response.Body, maxBytes+1))
	if err != nil || len(encoded) > maxBytes {
		return nil, errors.New("invalid Meta authorization facts response size")
	}
	var result authorizationfacts.AuthorizationFacts
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		return nil, errors.New("invalid Meta authorization facts response")
	}
	if decoder.Decode(new(any)) != io.EOF || result.ItemFingerprint != fingerprint || result.Validate() != nil {
		return nil, errors.New("invalid Meta authorization facts response")
	}
	return &result, nil
}
