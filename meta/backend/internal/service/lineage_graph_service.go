package service

import (
	"context"
	"fmt"

	"github.com/addp/meta/internal/models"
	"gorm.io/gorm"
)

func lineageGraphNodeKey(node models.LineageNode) string {
	if node.ItemID != nil {
		return fmt.Sprintf("item:%d", *node.ItemID)
	}
	return fmt.Sprintf("service:%d:%s", *node.ServiceID, node.PublishedRevision)
}

// Each breadth-first frontier keeps its direction. A shared input or output
// never permits traversal to switch direction into a sibling branch.
func (s *LineageService) buildLineageGraph(ctx context.Context, tenantID uint, request models.LineageGraphRequest) (models.LineageGraphResponse, error) {
	response := models.LineageGraphResponse{AsOf: request.AsOf, Nodes: []models.LineageNode{}, Edges: []models.LineageEdge{}}
	root := models.LineageNode{Kind: request.SubjectKind}
	if request.SubjectKind == "data_item" {
		root.ItemID = request.ItemID
	} else {
		root.ServiceID = request.ServiceID
		root.PublishedRevision = request.Revision
		var publication models.LineageServiceDependency
		if err := s.db.WithContext(ctx).Where("tenant_id = ? AND service_id = ? AND published_revision = ?", tenantID, *request.ServiceID, request.Revision).First(&publication).Error; err != nil {
			return response, err
		}
		root.Name = publication.ServiceName
	}
	if request.SubjectKind == "data_item" {
		var item models.MetaItem
		if err := s.db.WithContext(ctx).Where("tenant_id = ? AND id = ?", tenantID, *request.ItemID).First(&item).Error; err != nil {
			return response, err
		}
	}
	response.Nodes = append(response.Nodes, root)
	nodes := map[string]bool{lineageGraphNodeKey(root): true}
	edges := map[string]bool{}
	addEdge := func(key string, edge models.LineageEdge) bool {
		if edges[key] {
			return true
		}
		newNodes := []models.LineageNode{}
		for _, node := range []models.LineageNode{edge.Source, edge.Target} {
			if !nodes[lineageGraphNodeKey(node)] {
				newNodes = append(newNodes, node)
			}
		}
		if len(response.Nodes)+len(newNodes) > request.Limit || len(response.Edges) >= request.Limit {
			response.Truncated = true
			return false
		}
		for _, node := range newNodes {
			nodes[lineageGraphNodeKey(node)] = true
			response.Nodes = append(response.Nodes, node)
		}
		edges[key] = true
		response.Edges = append(response.Edges, edge)
		return true
	}
	active := func(query *gorm.DB) *gorm.DB {
		if request.AsOf == nil {
			return query.Where("r.status = 'active'")
		}
		return query.Where("(r.status = 'active' OR (r.status = 'closed' AND r.closed_at > ?)) AND r.last_observed_at <= ?", *request.AsOf, *request.AsOf)
	}
	// A service dependency counts as one hop, just like a data-item relation.
	serviceEdges := func(frontier []uint, serviceRoot bool) ([]uint, error) {
		query := active(s.db.WithContext(ctx).Table("meta.lineage_service_dependencies AS r").Select("r.*").
			Joins("JOIN meta.meta_item AS source ON source.id = r.source_item_id AND source.tenant_id = r.tenant_id AND source.deleted_at IS NULL").
			Where("r.tenant_id = ?", tenantID))
		if serviceRoot {
			query = query.Where("r.service_id = ? AND r.published_revision = ?", *request.ServiceID, request.Revision)
		} else {
			query = query.Where("r.source_item_id IN ?", frontier)
		}
		var deps []models.LineageServiceDependency
		if err := query.Order("r.id").Limit(request.Limit + 1).Find(&deps).Error; err != nil {
			return nil, err
		}
		var next []uint
		for _, dep := range deps {
			evidence, err := s.latestServiceEvidence(ctx, tenantID, dep)
			if err != nil {
				return nil, err
			}
			edge := models.LineageEdge{
				Source:       models.LineageNode{Kind: "data_item", ItemID: uintPtr(dep.SourceItemID)},
				Target:       models.LineageNode{Kind: "published_service", ServiceID: uintPtr(dep.ServiceID), PublishedRevision: dep.PublishedRevision, Name: dep.ServiceName},
				RelationKind: "serve", Granularity: dep.Granularity, Status: dep.Status, LastObservedAt: dep.LastObservedAt, Evidence: evidence,
			}
			if addEdge(fmt.Sprintf("service:%d", dep.ID), edge) {
				next = append(next, dep.SourceItemID)
			}
		}
		return next, nil
	}
	up, down := []uint{}, []uint{}
	startDepth := 0
	if request.SubjectKind == "data_item" {
		if request.Direction != "downstream" {
			up = append(up, *request.ItemID)
		}
		if request.Direction != "upstream" {
			down = append(down, *request.ItemID)
		}
	} else if request.Depth > 0 && request.Direction != "downstream" {
		var err error
		up, err = serviceEdges(nil, true)
		if err != nil {
			return response, err
		}
		startDepth = 1
	}
	seen := []map[uint]bool{{}, {}}
	distances := []map[uint]int{{}, {}}
	for i, frontier := range [][]uint{up, down} {
		for _, id := range frontier {
			seen[i][id] = true
			distances[i][id] = startDepth
		}
	}
	expansions := []map[uint]bool{{}, {}}
	for direction, ids := range [][]uint{request.ExpandUpstream, request.ExpandDownstream} {
		for _, id := range ids {
			expansions[direction][id] = true
		}
	}
	for depth := startDepth; depth < 20 && len(up)+len(down) > 0; depth++ {
		frontiers := [][]uint{up, down}
		up, down = nil, nil
		for direction, frontier := range frontiers {
			if depth >= request.Depth {
				filtered := []uint{}
				for _, id := range frontier {
					if expansions[direction][id] {
						filtered = append(filtered, id)
					}
				}
				frontier = filtered
			}
			if len(frontier) == 0 {
				continue
			}
			column := "r.target_item_id"
			if direction == 1 {
				column = "r.source_item_id"
			}
			query := active(s.db.WithContext(ctx).Table("meta.lineage_item_relations AS r").Select("r.*").
				Joins("JOIN meta.meta_item AS source ON source.id = r.source_item_id AND source.tenant_id = r.tenant_id AND source.deleted_at IS NULL").
				Joins("JOIN meta.meta_item AS target ON target.id = r.target_item_id AND target.tenant_id = r.tenant_id AND target.deleted_at IS NULL").
				Where("r.tenant_id = ? AND "+column+" IN ?", tenantID, frontier))
			var relations []models.LineageItemRelation
			if err := query.Order("r.id").Limit(request.Limit + 1).Find(&relations).Error; err != nil {
				return response, err
			}
			for _, relation := range relations {
				evidence, err := s.latestEvidence(ctx, tenantID, relation)
				if err != nil {
					return response, err
				}
				edge := models.LineageEdge{
					Source:       models.LineageNode{Kind: "data_item", ItemID: uintPtr(relation.SourceItemID)},
					Target:       models.LineageNode{Kind: "data_item", ItemID: uintPtr(relation.TargetItemID)},
					RelationKind: relation.RelationKind, Granularity: relation.Granularity, Status: relation.Status, LastObservedAt: relation.LastObservedAt, Evidence: evidence,
				}
				if !addEdge(fmt.Sprintf("item:%d", relation.ID), edge) {
					continue
				}
				id := relation.SourceItemID
				if direction == 1 {
					id = relation.TargetItemID
				}
				if !seen[direction][id] {
					seen[direction][id] = true
					distances[direction][id] = depth + 1
					if direction == 0 {
						up = append(up, id)
					} else {
						down = append(down, id)
					}
				}
			}
			if direction == 1 {
				if _, err := serviceEdges(frontier, false); err != nil {
					return response, err
				}
			}
		}
	}
	var ids []uint
	for _, node := range response.Nodes {
		if node.ItemID != nil {
			ids = append(ids, *node.ItemID)
		}
	}
	// Count only outward neighbours of nodes reached in the same root direction.
	// Excluding visible endpoints keeps counts distinct from already drawn links.
	hidden := []map[uint]int{{}, {}}
	for direction, reached := range seen {
		eligible := []uint{}
		for id := range reached {
			eligible = append(eligible, id)
		}
		if len(eligible) == 0 {
			continue
		}
		current, neighbour := "r.target_item_id", "r.source_item_id"
		if direction == 1 {
			current, neighbour = neighbour, current
		}
		var counts []struct {
			ItemID uint
			Count  int
		}
		query := active(s.db.WithContext(ctx).Table("meta.lineage_item_relations AS r").
			Joins("JOIN meta.meta_item AS source ON source.id = r.source_item_id AND source.tenant_id = r.tenant_id AND source.deleted_at IS NULL").
			Joins("JOIN meta.meta_item AS target ON target.id = r.target_item_id AND target.tenant_id = r.tenant_id AND target.deleted_at IS NULL").
			Where("r.tenant_id = ? AND "+current+" IN ? AND "+neighbour+" NOT IN ?", tenantID, eligible, ids))
		if err := query.Select(current + " AS item_id, COUNT(DISTINCT " + neighbour + ") AS count").Group(current).Scan(&counts).Error; err != nil {
			return response, err
		}
		for _, count := range counts {
			hidden[direction][count.ItemID] = count.Count
		}
		if direction == 1 {
			services := []string{}
			for _, node := range response.Nodes {
				if node.ServiceID != nil {
					services = append(services, fmt.Sprintf("%d:%s", *node.ServiceID, node.PublishedRevision))
				}
			}
			key := "CAST(r.service_id AS TEXT) || ':' || r.published_revision"
			query := active(s.db.WithContext(ctx).Table("meta.lineage_service_dependencies AS r").Where("r.tenant_id = ? AND r.source_item_id IN ?", tenantID, eligible))
			if len(services) > 0 {
				query = query.Where("("+key+") NOT IN ?", services)
			}
			counts = nil
			if err := query.Select("r.source_item_id AS item_id, COUNT(DISTINCT " + key + ") AS count").Group("r.source_item_id").Scan(&counts).Error; err != nil {
				return response, err
			}
			for _, count := range counts {
				hidden[direction][count.ItemID] += count.Count
			}
		}
	}
	for direction, counts := range hidden {
		for id, count := range counts {
			if count > 0 && distances[direction][id] >= 20 {
				response.Truncated = true
			}
		}
	}
	var items []models.MetaItem
	if len(ids) > 0 {
		if err := s.db.WithContext(ctx).Where("tenant_id = ? AND id IN ?", tenantID, ids).Find(&items).Error; err != nil {
			return response, err
		}
	}
	names, err := s.lineageEngineNames(tenantID, items)
	if err != nil {
		return response, err
	}
	byID := map[uint]models.LineageNode{}
	for _, item := range items {
		byID[item.ID] = models.LineageNode{Kind: "data_item", ItemID: uintPtr(item.ID), ItemFingerprint: item.Fingerprint, EngineID: uintPtr(item.EngineID), EngineName: names[item.EngineID], ItemType: item.ItemType, Name: item.Name, FullName: item.FullName}
	}
	if len(byID) != len(ids) {
		return response, gorm.ErrRecordNotFound
	}
	hydrate := func(node models.LineageNode) models.LineageNode {
		if node.ItemID != nil {
			hydrated := byID[*node.ItemID]
			hydrated.HiddenUpstreamCount = hidden[0][*node.ItemID]
			hydrated.HiddenDownstreamCount = hidden[1][*node.ItemID]
			return hydrated
		}
		return node
	}
	for i := range response.Nodes {
		response.Nodes[i] = hydrate(response.Nodes[i])
	}
	for i := range response.Edges {
		response.Edges[i].Source = hydrate(response.Edges[i].Source)
		response.Edges[i].Target = hydrate(response.Edges[i].Target)
	}
	response.Subject = hydrate(root)
	return response, nil
}
