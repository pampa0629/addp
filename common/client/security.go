package client

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"github.com/addp/common/dataprotection"
)

type SecurityClient struct {
	http tenantHTTPClient
}

func NewSecurityClient(baseURL string, tokenSource ServiceTokenProvider, httpClient *http.Client) *SecurityClient {
	return &SecurityClient{http: newTenantHTTPClient(baseURL, tokenSource, httpClient)}
}

func (c *SecurityClient) WithTenantID(tenantID uint) *SecurityClient {
	if c == nil {
		return nil
	}
	return &SecurityClient{http: c.http.withTenantID(tenantID)}
}

func (c *SecurityClient) ListProtectionProjectionChanges(
	ctx context.Context,
	afterCursor string,
	limit int,
) (*dataprotection.ProjectionChangesResponse, error) {
	if c == nil || c.http.tenantID == nil || limit <= 0 || limit > 500 {
		return nil, errors.New("Security protection projection changes require tenant context and limit between 1 and 500")
	}
	query := url.Values{"limit": {strconv.Itoa(limit)}}
	if afterCursor != "" {
		query.Set("after_cursor", afterCursor)
	}
	var response dataprotection.ProjectionChangesResponse
	path := "/api/v1/security/runtime/protection-projections/changes?" + query.Encode()
	if err := c.http.doJSON(ctx, http.MethodGet, path, nil, &response); err != nil {
		return nil, fmt.Errorf("Security list protection projection changes: %w", err)
	}
	return &response, nil
}

func (c *SecurityClient) AcknowledgeProtectionProjectionCursor(ctx context.Context, appliedCursor string) error {
	if c == nil || c.http.tenantID == nil || appliedCursor == "" {
		return errors.New("Security protection projection acknowledgement requires tenant context and cursor")
	}
	return c.http.doJSON(
		ctx,
		http.MethodPost,
		"/api/v1/security/runtime/protection-projection-acknowledgements",
		dataprotection.ProjectionAcknowledgementRequest{AppliedCursor: appliedCursor},
		nil,
	)
}

func (c *SecurityClient) ListProtectionProjectionChangesForTenant(ctx context.Context, tenantID uint, afterCursor string, limit int) (*dataprotection.ProjectionChangesResponse, error) {
	return c.WithTenantID(tenantID).ListProtectionProjectionChanges(ctx, afterCursor, limit)
}

func (c *SecurityClient) AcknowledgeProtectionProjectionCursorForTenant(ctx context.Context, tenantID uint, appliedCursor string) error {
	return c.WithTenantID(tenantID).AcknowledgeProtectionProjectionCursor(ctx, appliedCursor)
}
