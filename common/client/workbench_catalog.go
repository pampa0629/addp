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

	"github.com/google/uuid"
)

type WorkbenchClient struct{ tenantHTTPClient }

func NewWorkbenchClient(baseURL string, tokenSource ServiceTokenProvider, httpClient *http.Client) *WorkbenchClient {
	return &WorkbenchClient{tenantHTTPClient: newTenantHTTPClient(baseURL, tokenSource, httpClient)}
}

func (c *WorkbenchClient) WithTenantID(tenantID uint) *WorkbenchClient {
	if c == nil {
		return nil
	}
	return &WorkbenchClient{tenantHTTPClient: c.tenantHTTPClient.withTenantID(tenantID)}
}

const WorkbenchCatalogResourceChangesSchemaVersion = "workbench.catalog_resource_changes/v1"

type WorkbenchCatalogResourceChange struct {
	ChangeID       string         `json:"change_id"`
	SourceType     string         `json:"source_type"`
	SourceIdentity string         `json:"source_identity"`
	Operation      string         `json:"operation"`
	SourceVersion  string         `json:"source_version"`
	ObservedAt     time.Time      `json:"observed_at"`
	Snapshot       map[string]any `json:"snapshot"`
}

type WorkbenchCatalogResourceChangesResponse struct {
	SchemaVersion string                           `json:"schema_version"`
	Changes       []WorkbenchCatalogResourceChange `json:"changes"`
	NextCursor    string                           `json:"next_cursor"`
	HasMore       bool                             `json:"has_more"`
}

type WorkbenchCatalogReference struct {
	SourceType     string `json:"source_type"`
	SourceIdentity string `json:"source_identity"`
}

type WorkbenchCatalogReferenceResolution struct {
	SourceType     string         `json:"source_type"`
	SourceIdentity string         `json:"source_identity"`
	Found          bool           `json:"found"`
	Status         string         `json:"status,omitempty"`
	Version        int64          `json:"version,omitempty"`
	Summary        map[string]any `json:"summary,omitempty"`
	DetailPath     string         `json:"detail_path,omitempty"`
}

type ResolveWorkbenchCatalogReferencesResponse struct {
	Results []WorkbenchCatalogReferenceResolution `json:"results"`
}

func (c *WorkbenchClient) ListCatalogResourceChanges(ctx context.Context, afterCursor string, limit int) (*WorkbenchCatalogResourceChangesResponse, error) {
	if c == nil || c.tenantID == nil || *c.tenantID == 0 || limit < 1 || limit > 500 {
		return nil, errors.New("Workbench catalog resource changes require tenant context and limit between 1 and 500")
	}
	query := url.Values{"limit": []string{strconv.Itoa(limit)}}
	if strings.TrimSpace(afterCursor) != "" {
		query.Set("after_cursor", afterCursor)
	}
	var response WorkbenchCatalogResourceChangesResponse
	if err := c.doJSON(ctx, http.MethodGet, "/api/v1/workbench/catalog-resources/changes?"+query.Encode(), nil, &response); err != nil {
		return nil, fmt.Errorf("Workbench list catalog resource changes: %w", err)
	}
	if err := validateWorkbenchCatalogChanges(&response); err != nil {
		return nil, fmt.Errorf("Workbench list catalog resource changes: %w", err)
	}
	return &response, nil
}

func (c *WorkbenchClient) ResolveCatalogReferences(ctx context.Context, references []WorkbenchCatalogReference) (*ResolveWorkbenchCatalogReferencesResponse, error) {
	if c == nil || c.tenantID == nil || *c.tenantID == 0 || len(references) == 0 || len(references) > 200 {
		return nil, errors.New("Workbench catalog reference resolution requires tenant context and 1 to 200 references")
	}
	for _, reference := range references {
		if !validWorkbenchCatalogReference(reference) {
			return nil, errors.New("Workbench catalog reference resolution contains an invalid reference")
		}
	}
	var response ResolveWorkbenchCatalogReferencesResponse
	if err := c.doJSON(ctx, http.MethodPost, "/api/v1/workbench/runtime/catalog-references/resolve", map[string]any{"references": references}, &response); err != nil {
		return nil, fmt.Errorf("Workbench resolve catalog references: %w", err)
	}
	if len(response.Results) != len(references) {
		return nil, errors.New("Workbench catalog reference resolution returned a result count mismatch")
	}
	for index, result := range response.Results {
		requested := references[index]
		if result.SourceType != requested.SourceType || result.SourceIdentity != requested.SourceIdentity {
			return nil, errors.New("Workbench catalog reference resolution returned results out of request order")
		}
		if result.Found && (result.Version <= 0 || (result.Status != "published" && result.Status != "offline") || len(result.Summary) == 0 || strings.TrimSpace(result.DetailPath) == "") {
			return nil, errors.New("Workbench catalog reference resolution returned an invalid found result")
		}
	}
	return &response, nil
}

func validateWorkbenchCatalogChanges(response *WorkbenchCatalogResourceChangesResponse) error {
	if response == nil || response.SchemaVersion != WorkbenchCatalogResourceChangesSchemaVersion || strings.TrimSpace(response.NextCursor) == "" {
		return errors.New("Workbench returned an invalid catalog resource change batch")
	}
	for _, change := range response.Changes {
		if strings.TrimSpace(change.ChangeID) == "" || !validWorkbenchCatalogReference(WorkbenchCatalogReference{SourceType: change.SourceType, SourceIdentity: change.SourceIdentity}) ||
			change.Operation != "upsert" || len(change.SourceVersion) != 20 || len(change.Snapshot) == 0 || change.ObservedAt.IsZero() {
			return errors.New("Workbench returned an invalid catalog resource change")
		}
		if _, err := strconv.ParseUint(change.SourceVersion, 10, 64); err != nil {
			return errors.New("Workbench returned an invalid catalog source version")
		}
	}
	return nil
}

func validWorkbenchCatalogReference(reference WorkbenchCatalogReference) bool {
	if reference.SourceType != "data_application" {
		return false
	}
	id, err := uuid.Parse(reference.SourceIdentity)
	return err == nil && id != uuid.Nil && id.String() == reference.SourceIdentity
}
