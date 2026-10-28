package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/jsonmap"
)

// QueryDataItemIdentities is one bounded read-only owner query. Never retries
// POST or follows redirects; an unavailable owner is not an empty inventory.
func (c *MetaClient) QueryDataItemIdentities(ctx context.Context, tenantID uint, query authorizationfacts.IdentityQuery) (*authorizationfacts.IdentityPage, error) {
	if c == nil || c.httpClient == nil || c.baseURL == "" || tenantID == 0 || query.Validate() != nil {
		return nil, errors.New("invalid Meta identity query")
	}
	copy := *c.WithTenantID(tenantID)
	transport := *copy.httpClient
	transport.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	copy.httpClient = &transport
	body, err := json.Marshal(query)
	if err != nil || len(body) > authorizationfacts.MaxIdentityQueryBytes {
		return nil, errors.New("invalid Meta identity query size")
	}
	const path = "/api/v1/meta/runtime/data-item-identities/query"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, copy.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	// This POST has one attempt, including when credentials have expired.
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
	const maxBytes = authorizationfacts.MaxIdentityQueryBytes
	encoded, err := io.ReadAll(io.LimitReader(response.Body, maxBytes+1))
	if err != nil {
		return nil, errors.New("invalid Meta identity response")
	}
	if _, err := jsonmap.DecodeObjectStrict(encoded, maxBytes, 8); err != nil {
		return nil, errors.New("invalid Meta identity response")
	}
	var result authorizationfacts.IdentityPage
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&result) != nil || result.Validate(query) != nil {
		return nil, errors.New("invalid Meta identity response")
	}
	return &result, nil
}
