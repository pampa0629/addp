package client

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"strings"

	"github.com/addp/common/authorization"
)

// GetMetadataRefreshScope uses only this request's ordinary User credential.
// The client never substitutes a Service identity or retries a POST implicitly.
func (c *SystemServiceClient) GetMetadataRefreshScope(ctx context.Context, credential string, query authorization.MetadataRefreshQuery) (*authorization.MetadataRefreshScope, error) {
	if !strings.HasPrefix(credential, "addp_at_") || len(credential) == len("addp_at_") || strings.ContainsAny(credential, " \t\r\n") {
		return nil, ErrMetadataCredentialRejected
	}
	if query.Validate() != nil {
		return nil, ErrMetadataQueryDenied
	}
	if c == nil || c.baseURL == "" || c.httpClient == nil {
		return nil, ErrMetadataQueryUnavailable
	}
	bound := *c
	httpClient := *c.httpClient
	httpClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	bound.httpClient = &httpClient
	var result authorization.MetadataRefreshScope
	status, err := bound.doJSON(ctx, http.MethodPost, "/api/v1/system/engine-access/metadata-refresh-scopes/query", credential, query, &result, authorization.MaxMetadataRefreshScopeBytes)
	if status == http.StatusUnauthorized {
		return nil, ErrMetadataCredentialRejected
	}
	if status == http.StatusForbidden {
		return nil, ErrMetadataQueryDenied
	}
	if err != nil {
		return nil, errors.Join(ErrMetadataQueryUnavailable, err)
	}
	if status != http.StatusOK || result.Validate() != nil || !reflect.DeepEqual(result.Query, query) {
		return nil, ErrMetadataQueryUnavailable
	}
	return &result, nil
}
