package client

import (
	"context"
	"fmt"
	"net/http"
)

// ModelClient is the Bearer-only client for tenant-owned Model APIs.
type ModelClient struct{ tenantHTTPClient }

func NewModelClient(baseURL string, tokenSource ServiceTokenProvider, httpClient *http.Client) *ModelClient {
	return &ModelClient{tenantHTTPClient: newTenantHTTPClient(baseURL, tokenSource, httpClient)}
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
