package service

import (
	"context"

	commonapi "github.com/addp/common/api"
	"github.com/addp/meta/internal/metaquery"
	"github.com/addp/meta/internal/models"
	"gorm.io/gorm"
)

type lineageVisibilityKey struct{}
type lineageVisibleItemsKey struct{}

// WithLineageVisibility binds one current-user observation to a graph request.
// Machine fact queries omit it; no visibility decision is cached between requests.
func WithLineageVisibility(ctx context.Context, observe func(context.Context) ([]uint, error)) context.Context {
	return context.WithValue(ctx, lineageVisibilityKey{}, observe)
}

// VisibleLineageItems reuses the exact PG identity construction and System
// metadata decision used by browsing. Non-PG items cannot become graph bridges.
func (s *ResourceTreeService) VisibleLineageItems(ctx context.Context, tenant uint) ([]uint, error) {
	if s == nil || s.engineService == nil || s.metadataQueryService == nil {
		return nil, commonapi.ErrForbidden
	}
	if _, ok := metadataVisibility(ctx); !ok {
		return nil, commonapi.ErrForbidden
	}
	engines, err := s.engineService.GetEnginesByTenant(tenant)
	if err != nil {
		return nil, err
	}
	engineIDs := []uint{}
	for _, engine := range engines {
		if engine != nil && engine.EngineType == "postgresql" {
			engineIDs = append(engineIDs, engine.ID)
		}
	}
	var items []models.MetaItem
	if len(engineIDs) > 0 {
		err = s.metadataQueryService.db.WithContext(ctx).Select("id", "tenant_id", "engine_id", "node_id", "item_type", "name", "full_name", "fingerprint").Where("tenant_id = ? AND engine_id IN ? AND item_type = 'table' AND deleted_at IS NULL", tenant, engineIDs).Order("id").Find(&items).Error
		if err != nil {
			return nil, err
		}
	}
	candidates := make([]models.MetaItemLite, len(items))
	for i, item := range items {
		candidates[i] = metaquery.ToMetaItemLite(item)
	}
	visible, err := s.FilterMetadataItems(ctx, tenant, candidates)
	if err != nil {
		return nil, err
	}
	ids := make([]uint, 0, len(visible))
	for _, item := range visible {
		ids = append(ids, item.ID)
	}
	return ids, nil
}

func prepareLineageVisibility(ctx context.Context, request models.LineageGraphRequest) (context.Context, error) {
	observe, user := ctx.Value(lineageVisibilityKey{}).(func(context.Context) ([]uint, error))
	if !user {
		return ctx, nil
	}
	if observe == nil || request.SubjectKind == "published_service" {
		return ctx, commonapi.ErrForbidden
	}
	ids, err := observe(ctx)
	if err != nil {
		return ctx, err
	}
	found := false
	for _, id := range ids {
		if request.ItemID != nil && id == *request.ItemID {
			found = true
		}
	}
	if !found {
		return ctx, gorm.ErrRecordNotFound
	}
	return context.WithValue(ctx, lineageVisibleItemsKey{}, ids), nil
}

func userLineageQuery(ctx context.Context) bool {
	_, user := ctx.Value(lineageVisibilityKey{}).(func(context.Context) ([]uint, error))
	return user
}

func visibleLineageRelations(ctx context.Context, query *gorm.DB) *gorm.DB {
	if userLineageQuery(ctx) {
		ids, _ := ctx.Value(lineageVisibleItemsKey{}).([]uint)
		return query.Where("r.source_item_id IN ? AND r.target_item_id IN ?", ids, ids)
	}
	return query
}

func projectUserLineage(ctx context.Context, graph models.LineageGraphResponse) models.LineageGraphResponse {
	if !userLineageQuery(ctx) {
		return graph
	}
	for i := range graph.Edges {
		safe := map[string]interface{}{}
		for _, key := range []string{"execution_id", "write_mode", "field_lineage_status"} {
			if value, ok := graph.Edges[i].Evidence[key].(string); ok {
				safe[key] = value
			}
		}
		graph.Edges[i].Evidence = safe
		graph.Edges[i].Transformation = ""
	}
	return graph
}
