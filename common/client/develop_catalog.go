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

type DevelopClient struct{ tenantHTTPClient }

func NewDevelopClient(baseURL string, tokenSource ServiceTokenProvider, httpClient *http.Client) *DevelopClient {
	return &DevelopClient{tenantHTTPClient: newTenantHTTPClient(baseURL, tokenSource, httpClient)}
}

func (c *DevelopClient) WithTenantID(tenantID uint) *DevelopClient {
	if c == nil {
		return nil
	}
	return &DevelopClient{tenantHTTPClient: c.tenantHTTPClient.withTenantID(tenantID)}
}

const DevelopCatalogResourceChangesSchemaVersion = "develop.catalog_resource_changes/v1"

type DevelopCatalogResourceChange struct {
	ChangeID       string         `json:"change_id"`
	SourceType     string         `json:"source_type"`
	SourceIdentity string         `json:"source_identity"`
	Operation      string         `json:"operation"`
	SourceVersion  string         `json:"source_version"`
	ObservedAt     time.Time      `json:"observed_at"`
	Snapshot       map[string]any `json:"snapshot"`
}

type DevelopCatalogResourceChangesResponse struct {
	SchemaVersion string                         `json:"schema_version"`
	Changes       []DevelopCatalogResourceChange `json:"changes"`
	NextCursor    string                         `json:"next_cursor"`
	HasMore       bool                           `json:"has_more"`
}

type DevelopCatalogReference struct {
	SourceType     string `json:"source_type"`
	SourceIdentity string `json:"source_identity"`
}

type DevelopCatalogReferenceResolution struct {
	SourceType     string         `json:"source_type"`
	SourceIdentity string         `json:"source_identity"`
	Found          bool           `json:"found"`
	Status         string         `json:"status,omitempty"`
	Version        int64          `json:"version,omitempty"`
	Summary        map[string]any `json:"summary,omitempty"`
	DetailPath     string         `json:"detail_path,omitempty"`
}

type ResolveDevelopCatalogReferencesResponse struct {
	Results []DevelopCatalogReferenceResolution `json:"results"`
}

func (c *DevelopClient) ListCatalogResourceChanges(ctx context.Context, afterCursor string, limit int) (*DevelopCatalogResourceChangesResponse, error) {
	if c == nil || c.tenantID == nil || *c.tenantID == 0 || limit < 1 || limit > 500 {
		return nil, errors.New("Develop catalog resource changes require tenant context and limit between 1 and 500")
	}
	query := url.Values{"limit": []string{strconv.Itoa(limit)}}
	if strings.TrimSpace(afterCursor) != "" {
		query.Set("after_cursor", afterCursor)
	}
	var response DevelopCatalogResourceChangesResponse
	if err := c.doJSON(ctx, http.MethodGet, "/api/v1/develop/catalog-resources/changes?"+query.Encode(), nil, &response); err != nil {
		return nil, fmt.Errorf("Develop list catalog resource changes: %w", err)
	}
	if err := validateDevelopCatalogChanges(&response); err != nil {
		return nil, fmt.Errorf("Develop list catalog resource changes: %w", err)
	}
	return &response, nil
}

func (c *DevelopClient) ResolveCatalogReferences(ctx context.Context, references []DevelopCatalogReference) (*ResolveDevelopCatalogReferencesResponse, error) {
	if c == nil || c.tenantID == nil || *c.tenantID == 0 || len(references) == 0 || len(references) > 200 {
		return nil, errors.New("Develop catalog reference resolution requires tenant context and 1 to 200 references")
	}
	for _, reference := range references {
		if !validDevelopCatalogReference(reference) {
			return nil, errors.New("Develop catalog reference resolution contains an invalid reference")
		}
	}
	var response ResolveDevelopCatalogReferencesResponse
	if err := c.doJSON(ctx, http.MethodPost, "/api/v1/develop/runtime/catalog-references/resolve", map[string]any{"references": references}, &response); err != nil {
		return nil, fmt.Errorf("Develop resolve catalog references: %w", err)
	}
	if len(response.Results) != len(references) {
		return nil, errors.New("Develop catalog reference resolution returned a result count mismatch")
	}
	for index, result := range response.Results {
		requested := references[index]
		if result.SourceType != requested.SourceType || result.SourceIdentity != requested.SourceIdentity {
			return nil, errors.New("Develop catalog reference resolution returned results out of request order")
		}
		if result.Found && (result.Version <= 0 || (result.Status != "active" && result.Status != "inactive" && result.Status != "archived") || len(result.Summary) == 0 || strings.TrimSpace(result.DetailPath) == "") {
			return nil, errors.New("Develop catalog reference resolution returned an invalid found result")
		}
	}
	return &response, nil
}

func validateDevelopCatalogChanges(response *DevelopCatalogResourceChangesResponse) error {
	if response == nil || response.SchemaVersion != DevelopCatalogResourceChangesSchemaVersion || strings.TrimSpace(response.NextCursor) == "" {
		return errors.New("Develop returned an invalid catalog resource change batch")
	}
	for _, change := range response.Changes {
		if strings.TrimSpace(change.ChangeID) == "" || !validDevelopCatalogReference(DevelopCatalogReference{SourceType: change.SourceType, SourceIdentity: change.SourceIdentity}) ||
			(change.Operation != "upsert" && change.Operation != "missing") || len(change.SourceVersion) != 20 || len(change.Snapshot) == 0 || change.ObservedAt.IsZero() {
			return errors.New("Develop returned an invalid catalog resource change")
		}
		if _, err := strconv.ParseUint(change.SourceVersion, 10, 64); err != nil {
			return errors.New("Develop returned an invalid catalog source version")
		}
	}
	return nil
}

func validDevelopCatalogReference(reference DevelopCatalogReference) bool {
	if reference.SourceType != "dev_task" {
		return false
	}
	id, err := strconv.ParseInt(reference.SourceIdentity, 10, 64)
	return err == nil && id > 0 && strconv.FormatInt(id, 10) == reference.SourceIdentity
}
