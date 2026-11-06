package service

import (
	"context"
	"slices"

	commonapi "github.com/addp/common/api"
	"github.com/addp/meta/internal/metaquery"
	"github.com/addp/meta/internal/models"
	"gorm.io/gorm"
)

type lineageVisibilityKey struct{}

type lineageVisibility struct {
	check     func(context.Context, []uint) ([]uint, error)
	decisions map[uint]bool
}

// WithLineageVisibility binds one current-user observation to a graph request.
// Machine fact queries omit it; no visibility decision is cached between requests.
func WithLineageVisibility(ctx context.Context, check func(context.Context, []uint) ([]uint, error)) context.Context {
	return context.WithValue(ctx, lineageVisibilityKey{}, lineageVisibility{check: check})
}

// LineageItemVisibility resolves only the requested items against the same
// typed metadata targets and System authorization used by metadata browsing.
func (s *ResourceTreeService) LineageItemVisibility(tenant uint, check MetadataVisibilityCheck) func(context.Context, []uint) ([]uint, error) {
	var storageEngines map[uint]bool
	return func(ctx context.Context, ids []uint) ([]uint, error) {
		if s == nil || s.engineService == nil || s.metadataQueryService == nil || check == nil {
			return nil, commonapi.ErrForbidden
		}
		if storageEngines == nil {
			engines, err := s.engineService.GetEnginesByTenant(tenant)
			if err != nil {
				return nil, err
			}
			storageEngines = make(map[uint]bool, len(engines))
			for _, engine := range engines {
				if engine != nil {
					storageEngines[engine.ID] = true
				}
			}
		}
		ctx = WithMetadataVisibility(ctx, check)
		var allowed []uint
		for batch := range slices.Chunk(ids, 200) {
			var items []models.MetaItem
			if err := s.metadataQueryService.db.WithContext(ctx).Select("id", "tenant_id", "engine_id").
				Where("tenant_id = ? AND id IN ? AND deleted_at IS NULL", tenant, batch).Find(&items).Error; err != nil {
				return nil, err
			}
			candidates := make([]models.MetaItemLite, 0, len(items))
			for _, item := range items {
				if storageEngines[item.EngineID] {
					candidates = append(candidates, metaquery.ToMetaItemLite(item))
				}
			}
			visible, err := s.FilterMetadataItems(ctx, tenant, candidates)
			if err != nil {
				return nil, err
			}
			for _, item := range visible {
				allowed = append(allowed, item.ID)
			}
		}
		return allowed, nil
	}
}

func prepareLineageVisibility(ctx context.Context, request models.LineageGraphRequest) (context.Context, error) {
	visibility, user := ctx.Value(lineageVisibilityKey{}).(lineageVisibility)
	if !user {
		return ctx, nil
	}
	if visibility.check == nil || request.SubjectKind == "published_service" {
		return ctx, commonapi.ErrForbidden
	}
	visibility.decisions = make(map[uint]bool)
	root := *request.ItemID
	if err := visibility.observe(ctx, []uint{root}); err != nil {
		return ctx, err
	}
	if !visibility.decisions[root] {
		return ctx, gorm.ErrRecordNotFound
	}
	return context.WithValue(ctx, lineageVisibilityKey{}, visibility), nil
}

func userLineageQuery(ctx context.Context) bool {
	_, user := ctx.Value(lineageVisibilityKey{}).(lineageVisibility)
	return user
}

func (v lineageVisibility) observe(ctx context.Context, ids []uint) error {
	var pending []uint
	for _, id := range ids {
		if _, checked := v.decisions[id]; !checked {
			pending = append(pending, id)
			v.decisions[id] = false
		}
	}
	if len(pending) == 0 {
		return nil
	}
	allowed, err := v.check(ctx, pending)
	if err != nil {
		return err
	}
	visible := make(map[uint]bool, len(allowed))
	for _, id := range allowed {
		visible[id] = true
	}
	for _, id := range pending {
		v.decisions[id] = visible[id]
	}
	return nil
}

// Unexpanded counts include only distinct visible neighbours. A count probe
// can look one hop beyond the displayed depth without setting the notice.
func (v lineageVisibility) countNeighbours(ctx context.Context, query *gorm.DB, current, neighbour string, counts map[uint]int) error {
	var pairs []struct {
		ItemID      uint
		NeighbourID uint
	}
	if err := query.Select("DISTINCT " + current + " AS item_id, " + neighbour + " AS neighbour_id").Scan(&pairs).Error; err != nil {
		return err
	}
	ids := make([]uint, 0, len(pairs))
	for _, pair := range pairs {
		ids = append(ids, pair.NeighbourID)
	}
	if err := v.observe(ctx, ids); err != nil {
		return err
	}
	for _, pair := range pairs {
		if v.decisions[pair.NeighbourID] {
			counts[pair.ItemID]++
		}
	}
	return nil
}

// The caller supplies an authorized frontier and all relation predicates.
// Read only candidate IDs until visibility is known, then apply the edge limit
// to authorized relations. Hidden endpoints cannot consume the graph budget.
func lineageRelations(ctx context.Context, query *gorm.DB, neighbour string, limit int, response *models.LineageGraphResponse) ([]models.LineageItemRelation, error) {
	if visibility, user := ctx.Value(lineageVisibilityKey{}).(lineageVisibility); user {
		var candidates []struct {
			ID     uint
			ItemID uint
		}
		if err := query.Session(&gorm.Session{}).Select("r.id, " + neighbour + " AS item_id").Order("r.id").Scan(&candidates).Error; err != nil {
			return nil, err
		}
		ids := make([]uint, 0, len(candidates))
		for _, candidate := range candidates {
			ids = append(ids, candidate.ItemID)
		}
		if err := visibility.observe(ctx, ids); err != nil {
			return nil, err
		}
		allowed := make([]uint, 0, min(limit, len(candidates)))
		for _, candidate := range candidates {
			if !visibility.decisions[candidate.ItemID] {
				response.PermissionFiltered = true
			} else if len(allowed) < limit {
				allowed = append(allowed, candidate.ID)
			}
		}
		if len(allowed) == 0 {
			return nil, nil
		}
		query = query.Where("r.id IN ?", allowed)
	}
	var relations []models.LineageItemRelation
	err := query.Order("r.id").Limit(limit).Find(&relations).Error
	return relations, err
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
