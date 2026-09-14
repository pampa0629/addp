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

// ModelClient is the Bearer-only client for tenant-owned Model APIs.
type ModelClient struct{ tenantHTTPClient }

func NewModelClient(baseURL string, tokenSource ServiceTokenProvider, httpClient *http.Client) *ModelClient {
	return &ModelClient{tenantHTTPClient: newTenantHTTPClient(baseURL, tokenSource, httpClient)}
}

const ModelCatalogResourceChangesSchemaVersion = "model.catalog_resource_changes/v1"

type ModelCatalogResourceChange struct {
	ChangeID       string         `json:"change_id"`
	SourceType     string         `json:"source_type"`
	SourceIdentity string         `json:"source_identity"`
	Operation      string         `json:"operation"`
	SourceVersion  string         `json:"source_version"`
	ObservedAt     time.Time      `json:"observed_at"`
	Snapshot       map[string]any `json:"snapshot"`
}

type ModelCatalogResourceChangesResponse struct {
	SchemaVersion string                       `json:"schema_version"`
	Changes       []ModelCatalogResourceChange `json:"changes"`
	NextCursor    string                       `json:"next_cursor"`
	HasMore       bool                         `json:"has_more"`
}

type ModelCatalogReference struct {
	SourceType     string `json:"source_type"`
	SourceIdentity string `json:"source_identity"`
}

type ModelCatalogReferenceResolution struct {
	SourceType     string         `json:"source_type"`
	SourceIdentity string         `json:"source_identity"`
	Found          bool           `json:"found"`
	Status         string         `json:"status,omitempty"`
	Version        int64          `json:"version,omitempty"`
	Summary        map[string]any `json:"summary,omitempty"`
	DetailPath     string         `json:"detail_path,omitempty"`
}

type ResolveModelCatalogReferencesResponse struct {
	Results []ModelCatalogReferenceResolution `json:"results"`
}

func (c *ModelClient) ListCatalogResourceChanges(ctx context.Context, afterCursor string, limit int) (*ModelCatalogResourceChangesResponse, error) {
	if c == nil || c.tenantID == nil || *c.tenantID == 0 || limit < 1 || limit > 500 {
		return nil, errors.New("Model catalog resource changes require tenant context and limit between 1 and 500")
	}
	query := url.Values{}
	query.Set("limit", strconv.Itoa(limit))
	if strings.TrimSpace(afterCursor) != "" {
		query.Set("after_cursor", afterCursor)
	}
	var response ModelCatalogResourceChangesResponse
	if err := c.doJSON(ctx, http.MethodGet, "/api/v1/model/catalog-resources/changes?"+query.Encode(), nil, &response); err != nil {
		return nil, fmt.Errorf("Model list catalog resource changes: %w", err)
	}
	if err := validateModelCatalogChanges(&response); err != nil {
		return nil, fmt.Errorf("Model list catalog resource changes: %w", err)
	}
	return &response, nil
}

func (c *ModelClient) ResolveCatalogReferences(ctx context.Context, references []ModelCatalogReference) (*ResolveModelCatalogReferencesResponse, error) {
	if c == nil || c.tenantID == nil || *c.tenantID == 0 || len(references) == 0 || len(references) > 200 {
		return nil, errors.New("Model catalog reference resolution requires tenant context and 1 to 200 references")
	}
	for _, reference := range references {
		if !validModelCatalogReference(reference) {
			return nil, errors.New("Model catalog reference resolution contains an invalid reference")
		}
	}
	var response ResolveModelCatalogReferencesResponse
	payload := map[string]any{"references": references}
	if err := c.doJSON(ctx, http.MethodPost, "/api/v1/model/runtime/catalog-references/resolve", payload, &response); err != nil {
		return nil, fmt.Errorf("Model resolve catalog references: %w", err)
	}
	if len(response.Results) != len(references) {
		return nil, errors.New("Model catalog reference resolution returned a result count mismatch")
	}
	for index, result := range response.Results {
		requested := references[index]
		if result.SourceType != requested.SourceType || result.SourceIdentity != requested.SourceIdentity {
			return nil, errors.New("Model catalog reference resolution returned results out of request order")
		}
		if result.Found && (result.Version <= 0 || (result.Status != "draft" && result.Status != "approved") || len(result.Summary) == 0 || strings.TrimSpace(result.DetailPath) == "") {
			return nil, errors.New("Model catalog reference resolution returned an invalid found result")
		}
	}
	return &response, nil
}

func validateModelCatalogChanges(response *ModelCatalogResourceChangesResponse) error {
	if response == nil || response.SchemaVersion != ModelCatalogResourceChangesSchemaVersion || strings.TrimSpace(response.NextCursor) == "" {
		return errors.New("Model returned an invalid catalog resource change batch")
	}
	for _, change := range response.Changes {
		if strings.TrimSpace(change.ChangeID) == "" || !validModelCatalogReference(ModelCatalogReference{SourceType: change.SourceType, SourceIdentity: change.SourceIdentity}) ||
			(change.Operation != "upsert" && change.Operation != "missing") || len(change.SourceVersion) != 20 || len(change.Snapshot) == 0 || change.ObservedAt.IsZero() {
			return errors.New("Model returned an invalid catalog resource change")
		}
		if _, err := strconv.ParseUint(change.SourceVersion, 10, 64); err != nil {
			return errors.New("Model returned an invalid catalog source version")
		}
	}
	return nil
}

func validModelCatalogReference(reference ModelCatalogReference) bool {
	if reference.SourceType != "entity" && reference.SourceType != "logical_table" {
		return false
	}
	id, err := strconv.ParseInt(reference.SourceIdentity, 10, 64)
	return err == nil && id > 0 && strconv.FormatInt(id, 10) == reference.SourceIdentity
}

func (c *ModelClient) WithTenantID(tenantID uint) *ModelClient {
	if c == nil {
		return nil
	}
	return &ModelClient{tenantHTTPClient: c.tenantHTTPClient.withTenantID(tenantID)}
}

const (
	StandardReferenceGuardOpen    = "open"
	StandardReferenceGuardFrozen  = "frozen"
	StandardReferenceGuardDeleted = "deleted"
)

type StandardReferenceImpact struct {
	OwnerType string `json:"owner_type"`
	OwnerID   int64  `json:"owner_id"`
	Field     string `json:"field"`
}

type StandardReferenceImpactSummary struct {
	OwnerType string `json:"owner_type"`
	Field     string `json:"field"`
	Count     int64  `json:"count"`
}

type StandardReferenceGuardResponse struct {
	ResourceType    string                           `json:"resource_type"`
	ResourceID      int64                            `json:"resource_id"`
	State           string                           `json:"state"`
	ReferenceCount  int64                            `json:"reference_count"`
	Summary         []StandardReferenceImpactSummary `json:"summary"`
	Sample          []StandardReferenceImpact        `json:"sample"`
	SampleTruncated bool                             `json:"sample_truncated"`
}

func (c *ModelClient) SetStandardReferenceGuard(ctx context.Context, resourceType string, resourceID int64, state string) (*StandardReferenceGuardResponse, error) {
	var response StandardReferenceGuardResponse
	payload := struct {
		State string `json:"state"`
	}{State: state}
	path := fmt.Sprintf("/api/v1/model/standard-reference-guards/%s/%d", resourceType, resourceID)
	if err := c.doJSON(ctx, http.MethodPut, path, payload, &response); err != nil {
		return nil, fmt.Errorf("model set standard reference guard: %w", err)
	}
	if err := validateStandardReferenceGuardResponse(&response, resourceType, resourceID, state); err != nil {
		return nil, fmt.Errorf("model set standard reference guard: %w", err)
	}
	return &response, nil
}

func validateStandardReferenceGuardResponse(response *StandardReferenceGuardResponse, resourceType string, resourceID int64, state string) error {
	if response.ResourceType != resourceType {
		return fmt.Errorf("invalid standard reference guard response: resource_type=%q, want %q", response.ResourceType, resourceType)
	}
	if response.ResourceID != resourceID {
		return fmt.Errorf("invalid standard reference guard response: resource_id=%d, want %d", response.ResourceID, resourceID)
	}
	if response.State != state {
		return fmt.Errorf("invalid standard reference guard response: state=%q, want %q", response.State, state)
	}
	if response.ReferenceCount < 0 {
		return fmt.Errorf("invalid standard reference guard response: reference_count=%d", response.ReferenceCount)
	}
	return nil
}

type ModelEntity struct {
	ID          uint                   `json:"id"`
	Name        string                 `json:"name"`
	Code        string                 `json:"code"`
	Description string                 `json:"description"`
	Attributes  []ModelEntityAttribute `json:"attributes"`
}

type ModelEntityAttribute struct {
	ID         uint   `json:"id"`
	Name       string `json:"name"`
	ColumnName string `json:"column_name"`
	DataType   string `json:"data_type"`
	Nullable   bool   `json:"nullable"`
	IsPK       bool   `json:"is_pk"`
	SortOrder  int    `json:"sort_order"`
}

type ModelEntityRelation struct {
	ID           uint   `json:"id"`
	Name         string `json:"name"`
	SourceEntity uint   `json:"source_entity"`
	TargetEntity uint   `json:"target_entity"`
	RelationType string `json:"relation_type"`
}

func (c *ModelClient) ListEntities(ctx context.Context) ([]ModelEntity, error) {
	var entities []ModelEntity
	for page := 1; ; page++ {
		var response struct {
			Data     []ModelEntity `json:"data"`
			Total    int           `json:"total"`
			PageSize int           `json:"page_size"`
		}
		if err := c.doJSON(ctx, http.MethodGet, fmt.Sprintf("/api/v1/model/entities?status=approved&page=%d&page_size=100", page), nil, &response); err != nil {
			return nil, fmt.Errorf("model list entities: %w", err)
		}
		entities = append(entities, response.Data...)
		if len(entities) >= response.Total || len(response.Data) == 0 || response.PageSize == 0 || len(response.Data) < response.PageSize {
			break
		}
	}
	return entities, nil
}

func (c *ModelClient) GetEntityWithAttributes(ctx context.Context, entityID uint) (*ModelEntity, error) {
	var entity ModelEntity
	if err := c.doJSON(ctx, http.MethodGet, fmt.Sprintf("/api/v1/model/entities/%d", entityID), nil, &entity); err != nil {
		return nil, fmt.Errorf("model get entity: %w", err)
	}
	var attrs []ModelEntityAttribute
	if err := c.doJSON(ctx, http.MethodGet, fmt.Sprintf("/api/v1/model/entities/%d/attributes", entityID), nil, &attrs); err != nil {
		return nil, fmt.Errorf("model get entity attributes: %w", err)
	}
	entity.Attributes = attrs
	return &entity, nil
}

func (c *ModelClient) ListEntityRelations(ctx context.Context) ([]ModelEntityRelation, error) {
	var relations []ModelEntityRelation
	if err := c.doJSON(ctx, http.MethodGet, "/api/v1/model/entity-relations", nil, &relations); err != nil {
		return nil, fmt.Errorf("model list entity relations: %w", err)
	}
	return relations, nil
}
