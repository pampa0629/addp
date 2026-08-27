package client

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// ServiceClient is the Bearer-only client for tenant-owned Service APIs.
type ServiceClient struct{ tenantHTTPClient }

func NewServiceClient(baseURL string, tokenSource ServiceTokenProvider, httpClient *http.Client) *ServiceClient {
	return &ServiceClient{tenantHTTPClient: newTenantHTTPClient(baseURL, tokenSource, httpClient)}
}

func (c *ServiceClient) WithTenantID(tenantID uint) *ServiceClient {
	if c == nil {
		return nil
	}
	return &ServiceClient{tenantHTTPClient: c.tenantHTTPClient.withTenantID(tenantID)}
}

const ServiceCatalogResourceChangesSchemaVersion = "service.catalog_resource_changes/v1"

type ServiceCatalogResourceChange struct {
	ChangeID       string         `json:"change_id"`
	SourceType     string         `json:"source_type"`
	SourceIdentity string         `json:"source_identity"`
	Operation      string         `json:"operation"`
	SourceVersion  string         `json:"source_version"`
	ObservedAt     time.Time      `json:"observed_at"`
	Snapshot       map[string]any `json:"snapshot"`
}

type ServiceCatalogResourceChangesResponse struct {
	SchemaVersion string                         `json:"schema_version"`
	Changes       []ServiceCatalogResourceChange `json:"changes"`
	NextCursor    string                         `json:"next_cursor"`
	HasMore       bool                           `json:"has_more"`
}

type ServiceCatalogReference struct {
	SourceType     string `json:"source_type"`
	SourceIdentity string `json:"source_identity"`
}

type ServiceCatalogReferenceResolution struct {
	SourceType     string         `json:"source_type"`
	SourceIdentity string         `json:"source_identity"`
	Found          bool           `json:"found"`
	Status         string         `json:"status,omitempty"`
	Version        int64          `json:"version,omitempty"`
	Summary        map[string]any `json:"summary,omitempty"`
	DetailPath     string         `json:"detail_path,omitempty"`
}

type ResolveServiceCatalogReferencesResponse struct {
	Results []ServiceCatalogReferenceResolution `json:"results"`
}

func (c *ServiceClient) ListCatalogResourceChanges(ctx context.Context, afterCursor string, limit int) (*ServiceCatalogResourceChangesResponse, error) {
	if c == nil || c.tenantID == nil || *c.tenantID == 0 || limit < 1 || limit > 500 {
		return nil, errors.New("Service catalog resource changes require tenant context and limit between 1 and 500")
	}
	query := url.Values{"limit": []string{strconv.Itoa(limit)}}
	if strings.TrimSpace(afterCursor) != "" {
		query.Set("after_cursor", afterCursor)
	}
	var response ServiceCatalogResourceChangesResponse
	if err := c.doJSON(ctx, http.MethodGet, "/api/v1/service/catalog-resources/changes?"+query.Encode(), nil, &response); err != nil {
		return nil, fmt.Errorf("Service list catalog resource changes: %w", err)
	}
	if err := validateServiceCatalogChanges(&response); err != nil {
		return nil, fmt.Errorf("Service list catalog resource changes: %w", err)
	}
	return &response, nil
}

func (c *ServiceClient) ResolveCatalogReferences(ctx context.Context, references []ServiceCatalogReference) (*ResolveServiceCatalogReferencesResponse, error) {
	if c == nil || c.tenantID == nil || *c.tenantID == 0 || len(references) == 0 || len(references) > 200 {
		return nil, errors.New("Service catalog reference resolution requires tenant context and 1 to 200 references")
	}
	for _, reference := range references {
		if !validServiceCatalogReference(reference) {
			return nil, errors.New("Service catalog reference resolution contains an invalid reference")
		}
	}
	var response ResolveServiceCatalogReferencesResponse
	if err := c.doJSON(ctx, http.MethodPost, "/api/v1/service/runtime/catalog-references/resolve", map[string]any{"references": references}, &response); err != nil {
		return nil, fmt.Errorf("Service resolve catalog references: %w", err)
	}
	if len(response.Results) != len(references) {
		return nil, errors.New("Service catalog reference resolution returned a result count mismatch")
	}
	for index, result := range response.Results {
		requested := references[index]
		if result.SourceType != requested.SourceType || result.SourceIdentity != requested.SourceIdentity {
			return nil, errors.New("Service catalog reference resolution returned results out of request order")
		}
		if result.Found && (result.Version <= 0 || (result.Status != "active" && result.Status != "inactive" && result.Status != "error") || len(result.Summary) == 0 || strings.TrimSpace(result.DetailPath) == "") {
			return nil, errors.New("Service catalog reference resolution returned an invalid found result")
		}
	}
	return &response, nil
}

func validateServiceCatalogChanges(response *ServiceCatalogResourceChangesResponse) error {
	if response == nil || response.SchemaVersion != ServiceCatalogResourceChangesSchemaVersion || strings.TrimSpace(response.NextCursor) == "" {
		return errors.New("Service returned an invalid catalog resource change batch")
	}
	for _, change := range response.Changes {
		if strings.TrimSpace(change.ChangeID) == "" || !validServiceCatalogReference(ServiceCatalogReference{SourceType: change.SourceType, SourceIdentity: change.SourceIdentity}) ||
			(change.Operation != "upsert" && change.Operation != "missing") || len(change.SourceVersion) != 20 || len(change.Snapshot) == 0 || change.ObservedAt.IsZero() {
			return errors.New("Service returned an invalid catalog resource change")
		}
		if _, err := strconv.ParseUint(change.SourceVersion, 10, 64); err != nil {
			return errors.New("Service returned an invalid catalog source version")
		}
	}
	return nil
}

func validServiceCatalogReference(reference ServiceCatalogReference) bool {
	if reference.SourceType != "query_service" {
		return false
	}
	id, err := strconv.ParseInt(reference.SourceIdentity, 10, 64)
	return err == nil && id > 0 && strconv.FormatInt(id, 10) == reference.SourceIdentity
}
