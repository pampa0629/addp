package client

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

type SecurityAccessActor struct {
	ID          int64
	DisplayName string
}

func (c *SystemServiceClient) ResolveSecurityAccessActors(ctx context.Context, ids []int64) ([]SecurityAccessActor, error) {
	if len(ids) == 0 || len(ids) > 200 {
		return nil, errors.New("System resolve Security access actors requires 1 to 200 IDs")
	}
	references := make([]systemCatalogReferenceWire, 0, len(ids))
	for _, id := range ids {
		if id <= 0 {
			return nil, errors.New("System resolve Security access actors contains an invalid ID")
		}
		references = append(references, systemCatalogReferenceWire{SubjectType: "user", ID: strconv.FormatInt(id, 10)})
	}
	var response struct {
		Results []systemCatalogReferenceResolutionWire `json:"results"`
	}
	if err := c.doTenantJSON(ctx, http.MethodPost, "/api/v1/system/runtime/security-access-actors/resolve", map[string]any{"references": references}, &response); err != nil {
		return nil, fmt.Errorf("System resolve Security access actors: %w", err)
	}
	if len(response.Results) != len(ids) {
		return nil, errors.New("System resolve Security access actors returned a result count mismatch")
	}
	result := make([]SecurityAccessActor, 0, len(ids))
	for index, item := range response.Results {
		id, err := strconv.ParseInt(item.ID, 10, 64)
		if err != nil || id != ids[index] || item.SubjectType != "user" || !item.Found || strings.TrimSpace(item.Name) == "" {
			return nil, errors.New("System resolve Security access actors returned an invalid result")
		}
		result = append(result, SecurityAccessActor{ID: id, DisplayName: strings.TrimSpace(item.Name)})
	}
	return result, nil
}
