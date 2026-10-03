package client

import (
	"context"
	"errors"
	"github.com/addp/common/authorization"
	"github.com/google/uuid"
	"net/http"
)

// CatalogFulfillmentClient only reads exact owner-persisted pending requests.
type CatalogFulfillmentClient struct{ tenantHTTPClient }

func NewCatalogFulfillmentClient(url string, tokens ServiceTokenProvider, httpClient *http.Client) *CatalogFulfillmentClient {
	return &CatalogFulfillmentClient{newTenantHTTPClient(url, tokens, httpClient)}
}

func (c *CatalogFulfillmentClient) ReadFulfillmentBasis(ctx context.Context, tenantID uint, id uuid.UUID, binding authorization.SharingFulfillmentBinding) (*authorization.SharingFulfillmentBasis, error) {
	if id == uuid.Nil || binding.Validate() != nil {
		return nil, errors.New("invalid fulfillment basis request")
	}
	var result authorization.SharingFulfillmentBasis
	err := c.tenantHTTPClient.withTenantID(tenantID).doJSON(ctx, http.MethodPost, "/api/v1/catalog/runtime/sharing-fulfillments/"+id.String()+"/basis", binding, &result)
	if err != nil {
		return nil, err
	}
	return &result, nil
}
