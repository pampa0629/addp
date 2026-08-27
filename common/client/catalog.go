package client

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/google/uuid"
)

// CatalogClient is the Bearer-only client for tenant-owned Catalog runtime APIs.
type CatalogClient struct{ tenantHTTPClient }

func NewCatalogClient(baseURL string, tokenSource ServiceTokenProvider, httpClient *http.Client) *CatalogClient {
	return &CatalogClient{tenantHTTPClient: newTenantHTTPClient(baseURL, tokenSource, httpClient)}
}

func (c *CatalogClient) WithTenantID(tenantID uint) *CatalogClient {
	if c == nil {
		return nil
	}
	return &CatalogClient{tenantHTTPClient: c.tenantHTTPClient.withTenantID(tenantID)}
}

type CatalogReferenceResolution struct {
	ID               uuid.UUID `json:"id"`
	Found            bool      `json:"found"`
	Selectable       bool      `json:"selectable"`
	Publishable      bool      `json:"publishable"`
	DisplayName      string    `json:"display_name,omitempty"`
	EntryStatus      string    `json:"entry_status,omitempty"`
	GovernanceStatus string    `json:"governance_status,omitempty"`
	SourceStatus     string    `json:"source_status,omitempty"`
	Version          int64     `json:"version,string"`
}

func (c *CatalogClient) ResolveReferences(ctx context.Context, ids []uuid.UUID) ([]CatalogReferenceResolution, error) {
	if len(ids) == 0 || len(ids) > 200 {
		return nil, errors.New("Catalog resolve references requires 1 to 200 references")
	}
	wireIDs := make([]string, 0, len(ids))
	seen := make(map[uuid.UUID]struct{}, len(ids))
	for _, id := range ids {
		if id == uuid.Nil {
			return nil, errors.New("Catalog resolve references contains an invalid reference")
		}
		if _, exists := seen[id]; exists {
			return nil, errors.New("Catalog resolve references contains a duplicate reference")
		}
		seen[id] = struct{}{}
		wireIDs = append(wireIDs, id.String())
	}
	var response struct {
		Results []CatalogReferenceResolution `json:"results"`
	}
	if err := c.doJSON(ctx, http.MethodPost, "/api/v1/catalog/runtime/references/resolve", map[string]any{"ids": wireIDs}, &response); err != nil {
		return nil, fmt.Errorf("Catalog resolve references: %w", err)
	}
	if len(response.Results) != len(ids) {
		return nil, errors.New("Catalog resolve references returned a result count mismatch")
	}
	for index, result := range response.Results {
		if result.ID != ids[index] {
			return nil, errors.New("Catalog resolve references returned results out of request order")
		}
	}
	return response.Results, nil
}
