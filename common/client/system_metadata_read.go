package client

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/addp/common/authorization"
	"github.com/addp/common/engine/plugin"
)

var (
	ErrMetadataCredentialRejected = errors.New("metadata query credential rejected")
	ErrMetadataQueryDenied        = errors.New("metadata query denied")
	ErrMetadataQueryUnavailable   = errors.New("metadata query check unavailable")
)

// CheckMetaMetadataRead uses the current User or exact Meta Tool credential, never the
// client's Service identity. Visibility is observed anew for each query.
func (c *SystemServiceClient) CheckMetaMetadataRead(ctx context.Context, credential string, targets []plugin.EngineCatalogPath) (*authorization.SourceMetadataCheck, error) {
	return c.checkMetaVisibility(ctx, credential, targets, "/api/v1/system/engine-access/metadata-checks/meta-query", true)
}

// CheckMetaLineageRead observes PG visibility for ordinary lineage callers only.
// Resource-tree delegated scopes never authorize lineage queries.
func (c *SystemServiceClient) CheckMetaLineageRead(ctx context.Context, credential string, targets []plugin.EngineCatalogPath) (*authorization.SourceMetadataCheck, error) {
	return c.checkMetaVisibility(ctx, credential, targets, "/api/v1/system/engine-access/metadata-checks/meta-lineage", false)
}

func (c *SystemServiceClient) checkMetaVisibility(ctx context.Context, credential string, targets []plugin.EngineCatalogPath, endpoint string, allowDelegate bool) (*authorization.SourceMetadataCheck, error) {
	var result authorization.SourceMetadataCheck
	if err := c.checkSourceVisibility(ctx, credential, targets, endpoint, allowDelegate, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *SystemServiceClient) CheckManagerSearchRead(ctx context.Context, credential string, request authorization.SourceSearchCheckRequest) (*authorization.SourceSearchCheck, error) {
	if !validSourceCredential(credential, true) {
		return nil, ErrMetadataCredentialRejected
	}
	if !request.Valid() {
		return nil, ErrMetadataQueryDenied
	}
	if c == nil || c.baseURL == "" || c.httpClient == nil {
		return nil, ErrMetadataQueryUnavailable
	}
	bound := *c
	transport := *c.httpClient
	transport.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	bound.httpClient = &transport
	var result authorization.SourceSearchCheck
	status, err := bound.doJSON(ctx, http.MethodPost, "/api/v1/system/engine-access/metadata-checks/manager-search", credential, request, &result, 512<<10)
	switch status {
	case http.StatusUnauthorized:
		return nil, ErrMetadataCredentialRejected
	case http.StatusForbidden:
		return nil, ErrMetadataQueryDenied
	case http.StatusConflict:
		return nil, ErrSearchContextChanged
	}
	if err != nil || status != http.StatusOK || !result.Matches(request) {
		return nil, ErrMetadataQueryUnavailable
	}
	return &result, nil
}

var ErrSearchContextChanged = errors.New("search context changed")

func validSourceCredential(credential string, allowDelegate bool) bool {
	ordinary := strings.HasPrefix(credential, "addp_at_") && len(credential) > len("addp_at_")
	delegated := allowDelegate && strings.HasPrefix(credential, "addp_dat_") && len(credential) > len("addp_dat_")
	return (ordinary || delegated) && !strings.ContainsAny(credential, " \t\r\n")
}

func (c *SystemServiceClient) checkSourceVisibility(ctx context.Context, credential string, targets []plugin.EngineCatalogPath, endpoint string, allowDelegate bool, result interface {
	Matches(authorization.SourceMetadataCheckRequest) bool
}) error {
	if !validSourceCredential(credential, allowDelegate) {
		return ErrMetadataCredentialRejected
	}
	request := authorization.SourceMetadataCheckRequest{Targets: targets}
	if !request.Valid() {
		return ErrMetadataQueryDenied
	}
	if c == nil || c.baseURL == "" || c.httpClient == nil {
		return ErrMetadataQueryUnavailable
	}
	bound := *c
	httpClient := *c.httpClient
	httpClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	bound.httpClient = &httpClient
	status, err := bound.doJSON(ctx, http.MethodPost, endpoint, credential, request, result, 512<<10)
	if status == http.StatusUnauthorized {
		return ErrMetadataCredentialRejected
	}
	if status == http.StatusForbidden {
		return ErrMetadataQueryDenied
	}
	if err != nil || status != http.StatusOK || !result.Matches(request) {
		return ErrMetadataQueryUnavailable
	}
	return nil
}

// CheckManagerObjectPreview uses the original User or exact data.preview
// delegate. Service credentials never substitute for user visibility.
func (c *SystemServiceClient) CheckManagerObjectPreview(ctx context.Context, credential string, targets []plugin.EngineCatalogPath) (*authorization.SourceObjectPreviewCheck, error) {
	if !(authorization.SourceMetadataCheckRequest{Targets: targets}).ValidObjectPreview() {
		return nil, ErrMetadataQueryDenied
	}
	var result authorization.SourceObjectPreviewCheck
	if err := c.checkSourceVisibility(ctx, credential, targets, "/api/v1/system/engine-access/metadata-checks/manager-object-preview", true, &result); err != nil {
		return nil, err
	}
	return &result, nil
}
