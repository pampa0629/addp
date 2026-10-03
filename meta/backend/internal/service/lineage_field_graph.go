package service

import (
	"context"
	"fmt"

	"github.com/addp/common/dataprotection"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/meta/internal/metaquery"
	"github.com/addp/meta/internal/models"
	"gorm.io/gorm"
)

func fieldNodeKey(node models.LineageNode) string {
	return fmt.Sprintf("field:%d:%q:%s", *node.ItemID, node.FieldName, node.SchemaSnapshotHash)
}

func (s *LineageService) buildFieldLineageGraph(ctx context.Context, tenantID uint, request models.LineageGraphRequest) (models.LineageGraphResponse, error) {
	response := models.LineageGraphResponse{Nodes: []models.LineageNode{}, Edges: []models.LineageEdge{}, AsOf: request.AsOf, FieldLineageStatus: "unavailable"}
	var item models.MetaItem
	if err := s.db.WithContext(ctx).Where("tenant_id = ? AND id = ?", tenantID, *request.ItemID).First(&item).Error; err != nil {
		return response, err
	}
	hash := request.SchemaSnapshotHash
	if hash == "" {
		fields, err := metaquery.FieldsFromMetaItem(item)
		if err != nil {
			return response, err
		}
		found := false
		for _, field := range fields {
			if field.Name == request.FieldName {
				found = true
			}
		}
		if !found {
			return response, gorm.ErrRecordNotFound
		}
		hash, err = dataprotection.TableSchemaSnapshotHash(fields)
		if err != nil {
			return response, err
		}
	}
	root := models.LineageNode{Kind: "field_ref", ItemID: &item.ID, FieldName: request.FieldName, SchemaSnapshotHash: hash}
	// A supplied historical snapshot must be grounded in immutable evidence.
	proven := false
	// Prefer the latest target write: a later read cannot make an unavailable
	// target mapping complete. Bound proof lookup in SQL instead of loading history.
	for _, column := range []string{"target", "source"} {
		proofs := s.db.WithContext(ctx).Where("tenant_id = ? AND granularity = 'item' AND relation_kind IN ('derive', 'reference') AND "+column+"_item_id = ?", tenantID, item.ID)
		if request.AsOf != nil {
			proofs = proofs.Where("observed_at <= ?", *request.AsOf)
		}
		if request.SchemaSnapshotHash != "" {
			proofs = proofs.Where(column+"_snapshot -> 'schema_snapshot' ->> 'hash' = ?", hash)
		}
		var observation models.LineageObservation
		if err := proofs.Order("observed_at DESC,id DESC").First(&observation).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				continue
			}
			return response, err
		}
		var snapshot interface{}
		if column == "target" {
			snapshot = observation.TargetSnapshot["schema_snapshot"]
		} else {
			snapshot = observation.SourceSnapshot["schema_snapshot"]
		}
		structure := snapshotFromEvidence(snapshot)
		if structure != nil && structure.Hash == hash && structure.HasField(request.FieldName) {
			proven = true
			mode, _ := observation.Evidence["write_mode"].(string)
			if observation.Evidence["field_lineage_status"] == "complete" && (knownLineageWriteMode(mode) || observation.RelationKind == "reference") {
				if column == "source" {
					response.FieldLineageStatus = "complete"
				} else {
					var mappings []commonExecution.LineageFieldMapping
					if decodeJSONValue(observation.Evidence["field_mappings"], &mappings) == nil {
						for _, mapping := range mappings {
							if mapping.TargetField == request.FieldName {
								response.FieldLineageStatus = "complete"
							}
						}
					}
				}
			}
		}
		break
	}
	if request.SchemaSnapshotHash != "" && !proven {
		return response, gorm.ErrRecordNotFound
	}
	response.Nodes = append(response.Nodes, root)
	nodes := map[string]bool{fieldNodeKey(root): true}
	edges := map[uint]bool{}
	for _, direction := range []string{"upstream", "downstream"} {
		if request.Direction != "both" && request.Direction != direction {
			continue
		}
		frontier := []models.LineageNode{root}
		seen := map[string]bool{fieldNodeKey(root): true}
		for depth := 0; depth < request.Depth && len(frontier) > 0; depth++ {
			query := s.db.WithContext(ctx).Table("meta.lineage_item_relations AS r").Select("r.*").
				Joins("JOIN meta.meta_item AS source ON source.id = r.source_item_id AND source.tenant_id = r.tenant_id AND source.deleted_at IS NULL").
				Joins("JOIN meta.meta_item AS target ON target.id = r.target_item_id AND target.tenant_id = r.tenant_id AND target.deleted_at IS NULL").Where("r.tenant_id = ? AND r.granularity = 'field'", tenantID)
			if request.AsOf == nil {
				query = query.Where("r.status = 'active'")
			} else {
				query = query.Where("r.first_observed_at <= ? AND (r.status IN ('active', 'stale') OR (r.status = 'closed' AND r.closed_at > ?))", *request.AsOf, *request.AsOf)
			}
			column := "target"
			if direction == "downstream" {
				column = "source"
			}
			conditions := s.db.Session(&gorm.Session{NewDB: true}).Where("1 = 0")
			for _, node := range frontier {
				conditions = conditions.Or("r."+column+"_item_id = ? AND r."+column+"_field_name = ? AND r."+column+"_schema_hash = ?", *node.ItemID, node.FieldName, node.SchemaSnapshotHash)
			}
			var relations []models.LineageItemRelation
			if err := query.Where(conditions).Order("r.id").Limit(request.Limit + 1).Find(&relations).Error; err != nil {
				return response, err
			}
			frontier = nil
			for _, relation := range relations {
				source := models.LineageNode{Kind: "field_ref", ItemID: uintPtr(relation.SourceItemID), FieldName: relation.SourceFieldName, SchemaSnapshotHash: relation.SourceSchemaHash}
				target := models.LineageNode{Kind: "field_ref", ItemID: uintPtr(relation.TargetItemID), FieldName: relation.TargetFieldName, SchemaSnapshotHash: relation.TargetSchemaHash}
				if !edges[relation.ID] {
					newNodes := []models.LineageNode{}
					added := map[string]bool{}
					for _, node := range []models.LineageNode{source, target} {
						if !nodes[fieldNodeKey(node)] && !added[fieldNodeKey(node)] {
							added[fieldNodeKey(node)] = true
							newNodes = append(newNodes, node)
						}
					}
					if len(response.Nodes)+len(newNodes) > request.Limit || len(response.Edges) >= request.Limit {
						response.Truncated = true
						continue
					}
					var observation models.LineageObservation
					evidenceQuery := s.db.WithContext(ctx).Where("tenant_id = ? AND granularity = 'field' AND source_item_id = ? AND target_item_id = ? AND source_field_name = ? AND target_field_name = ? AND source_schema_hash = ? AND target_schema_hash = ? AND relation_kind = ?", tenantID, relation.SourceItemID, relation.TargetItemID, relation.SourceFieldName, relation.TargetFieldName, relation.SourceSchemaHash, relation.TargetSchemaHash, relation.RelationKind)
					if request.AsOf != nil {
						evidenceQuery = evidenceQuery.Where("observed_at <= ?", *request.AsOf)
					}
					if err := evidenceQuery.Order("observed_at DESC,id DESC").First(&observation).Error; err != nil {
						return response, err
					}
					for _, node := range newNodes {
						nodes[fieldNodeKey(node)] = true
						response.Nodes = append(response.Nodes, node)
					}
					edges[relation.ID] = true
					transformation, _ := observation.Evidence["transformation"].(string)
					response.Edges = append(response.Edges, models.LineageEdge{Source: source, Target: target, RelationKind: relation.RelationKind, Granularity: "field", Transformation: transformation, Evidence: observation.Evidence, Status: relation.Status, LastObservedAt: observation.ObservedAt})
				}
				next := source
				if direction == "downstream" {
					next = target
				}
				if !seen[fieldNodeKey(next)] {
					seen[fieldNodeKey(next)] = true
					frontier = append(frontier, next)
				}
			}
		}
	}
	ids := []uint{}
	for _, node := range response.Nodes {
		ids = append(ids, *node.ItemID)
	}
	var items []models.MetaItem
	if err := s.db.WithContext(ctx).Where("tenant_id = ? AND id IN ?", tenantID, ids).Find(&items).Error; err != nil {
		return response, err
	}
	names, err := s.lineageEngineNames(tenantID, items)
	if err != nil {
		return response, err
	}
	byID := map[uint]models.MetaItem{}
	for _, item := range items {
		byID[item.ID] = item
	}
	hydrate := func(node models.LineageNode) models.LineageNode {
		item := byID[*node.ItemID]
		node.Name = item.Name
		node.FullName = item.FullName
		node.ItemType = item.ItemType
		node.ItemFingerprint = item.Fingerprint
		node.EngineID = uintPtr(item.EngineID)
		node.EngineName = names[item.EngineID]
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
