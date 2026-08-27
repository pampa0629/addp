package service

import (
	"fmt"
	"strconv"

	"github.com/addp/model/i18n"
	"github.com/addp/model/internal/apperrors"
	"github.com/addp/model/internal/models"
)

func modelProfessionalKey(resourceType string, id int64) models.ProfessionalResourceKey {
	return models.ProfessionalResourceKey{OwnerModule: "model", ResourceType: resourceType, ResourceID: strconv.FormatInt(id, 10)}
}

func standardProfessionalKey(resourceType string, id int64) models.ProfessionalResourceKey {
	return models.ProfessionalResourceKey{OwnerModule: "standard", ResourceType: resourceType, ResourceID: strconv.FormatInt(id, 10)}
}

func entityProfessionalNode(entity *models.Entity) models.ProfessionalRelationNode {
	return models.ProfessionalRelationNode{
		ProfessionalResourceKey: modelProfessionalKey("entity", entity.ID),
		Name:                    entity.Name, Code: entity.Code, Status: entity.Status,
	}
}

func logicalTableProfessionalNode(table *models.LogicalTable) models.ProfessionalRelationNode {
	return models.ProfessionalRelationNode{
		ProfessionalResourceKey: modelProfessionalKey("logical_table", table.ID),
		Name:                    table.Name, Code: table.Code, Status: table.Status, Kind: table.TableType,
	}
}

func appendProfessionalNode(nodes *[]models.ProfessionalRelationNode, seen map[models.ProfessionalResourceKey]struct{}, node models.ProfessionalRelationNode) {
	if _, ok := seen[node.ProfessionalResourceKey]; ok {
		return
	}
	seen[node.ProfessionalResourceKey] = struct{}{}
	*nodes = append(*nodes, node)
}

func (s *EntityRelationService) GetProfessionalRelations(tenantID, entityID int64, limit int) (*models.ProfessionalRelationsResponse, error) {
	subject, err := s.entityRepo.GetByID(entityID, tenantID)
	if err != nil {
		return nil, apperrors.NotFound("entity_not_found", i18n.MsgEntityNotFound)
	}
	relations, err := s.relationRepo.GetByEntityID(tenantID, entityID)
	if err != nil {
		return nil, err
	}
	truncated := len(relations) > limit
	if truncated {
		relations = relations[:limit]
	}

	relatedIDs := make([]int64, 0, len(relations)*2)
	for _, relation := range relations {
		relatedIDs = append(relatedIDs, relation.SourceEntity, relation.TargetEntity)
	}
	entities, err := s.entityRepo.GetByIDs(relatedIDs, tenantID)
	if err != nil {
		return nil, err
	}
	entityByID := make(map[int64]*models.Entity, len(entities)+1)
	entityByID[subject.ID] = subject
	for index := range entities {
		entity := &entities[index]
		entityByID[entity.ID] = entity
	}

	response := &models.ProfessionalRelationsResponse{
		SchemaVersion: models.ProfessionalRelationsSchemaVersion,
		Subject:       modelProfessionalKey("entity", subject.ID),
		Nodes:         []models.ProfessionalRelationNode{},
		Edges:         []models.ProfessionalRelationEdge{},
		Truncated:     truncated,
	}
	seen := map[models.ProfessionalResourceKey]struct{}{}
	appendProfessionalNode(&response.Nodes, seen, entityProfessionalNode(subject))
	for _, relation := range relations {
		source, sourceFound := entityByID[relation.SourceEntity]
		target, targetFound := entityByID[relation.TargetEntity]
		if !sourceFound || !targetFound {
			continue
		}
		appendProfessionalNode(&response.Nodes, seen, entityProfessionalNode(source))
		appendProfessionalNode(&response.Nodes, seen, entityProfessionalNode(target))
		response.Edges = append(response.Edges, models.ProfessionalRelationEdge{
			ID:           fmt.Sprintf("model:entity_relation:%d", relation.ID),
			RelationKind: "model.entity." + relation.RelationType,
			Source:       modelProfessionalKey("entity", relation.SourceEntity),
			Target:       modelProfessionalKey("entity", relation.TargetEntity),
			Name:         relation.Name, Description: relation.Description,
		})
	}
	return response, nil
}

func (s *TableRelationService) GetProfessionalRelations(tenantID, tableID int64, limit int) (*models.ProfessionalRelationsResponse, error) {
	subject, err := s.tableRepo.GetByID(tableID, tenantID)
	if err != nil {
		return nil, apperrors.NotFound("logical_table_not_found", i18n.MsgTableNotFound)
	}
	response := &models.ProfessionalRelationsResponse{
		SchemaVersion: models.ProfessionalRelationsSchemaVersion,
		Subject:       modelProfessionalKey("logical_table", subject.ID),
		Nodes:         []models.ProfessionalRelationNode{},
		Edges:         []models.ProfessionalRelationEdge{},
	}
	seen := map[models.ProfessionalResourceKey]struct{}{}
	appendProfessionalNode(&response.Nodes, seen, logicalTableProfessionalNode(subject))
	appendEdge := func(edge models.ProfessionalRelationEdge) bool {
		if len(response.Edges) >= limit {
			response.Truncated = true
			return false
		}
		response.Edges = append(response.Edges, edge)
		return true
	}

	if subject.EntityID != nil && s.entityRepo != nil {
		if entity, entityErr := s.entityRepo.GetByID(*subject.EntityID, tenantID); entityErr == nil {
			appendProfessionalNode(&response.Nodes, seen, entityProfessionalNode(entity))
			appendEdge(models.ProfessionalRelationEdge{
				ID:           fmt.Sprintf("model:logical_table_entity:%d", subject.ID),
				RelationKind: "model.logical_table.entity",
				Source:       response.Subject, Target: modelProfessionalKey("entity", entity.ID),
			})
		}
	}

	relations, err := s.repo.ListByTable(tableID, tenantID)
	if err != nil {
		return nil, err
	}
	for _, relation := range relations {
		if len(response.Edges) >= limit {
			response.Truncated = true
			break
		}
		tables, loadErr := s.tableRepo.GetByIDs([]int64{relation.SourceTable, relation.TargetTable}, tenantID)
		if loadErr != nil {
			return nil, loadErr
		}
		for index := range tables {
			appendProfessionalNode(&response.Nodes, seen, logicalTableProfessionalNode(&tables[index]))
		}
		sourceField, _ := s.tableRepo.GetFieldByID(relation.SourceField, relation.SourceTable)
		targetField, _ := s.tableRepo.GetFieldByID(relation.TargetField, relation.TargetTable)
		edge := models.ProfessionalRelationEdge{
			ID:              fmt.Sprintf("model:table_relation:%d", relation.ID),
			RelationKind:    "model.logical_table." + relation.RelationType,
			Source:          modelProfessionalKey("logical_table", relation.SourceTable),
			Target:          modelProfessionalKey("logical_table", relation.TargetTable),
			SourceComponent: &models.ProfessionalRelationComponent{ResourceID: strconv.FormatInt(relation.SourceField, 10)},
			TargetComponent: &models.ProfessionalRelationComponent{ResourceID: strconv.FormatInt(relation.TargetField, 10)},
		}
		if sourceField != nil {
			edge.SourceComponent.Name = sourceField.Name
		}
		if targetField != nil {
			edge.TargetComponent.Name = targetField.Name
		}
		appendEdge(edge)
	}

	if s.factMetricRepo != nil {
		mappings, mappingErr := s.factMetricRepo.ListByFactTable(tableID, tenantID)
		if mappingErr != nil {
			return nil, mappingErr
		}
		for _, mapping := range mappings {
			if len(response.Edges) >= limit {
				response.Truncated = true
				break
			}
			metricKey := standardProfessionalKey("metric", mapping.MetricID)
			appendProfessionalNode(&response.Nodes, seen, models.ProfessionalRelationNode{ProfessionalResourceKey: metricKey})
			edge := models.ProfessionalRelationEdge{
				ID:           fmt.Sprintf("model:fact_metric_mapping:%d", mapping.ID),
				RelationKind: "model.logical_table.supports_metric",
				Source:       response.Subject, Target: metricKey, Note: mapping.Note,
			}
			if mapping.FieldID != nil {
				edge.SourceComponent = &models.ProfessionalRelationComponent{ResourceID: strconv.FormatInt(*mapping.FieldID, 10)}
				if field, fieldErr := s.tableRepo.GetFieldByID(*mapping.FieldID, tableID); fieldErr == nil {
					edge.SourceComponent.Name = field.Name
				}
			}
			appendEdge(edge)
		}
	}
	return response, nil
}
