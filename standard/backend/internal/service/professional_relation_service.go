package service

import (
	"fmt"
	"strconv"

	"github.com/addp/standard/internal/models"
)

func standardMetricProfessionalKey(id int64) models.ProfessionalResourceKey {
	return models.ProfessionalResourceKey{OwnerModule: "standard", ResourceType: "metric", ResourceID: strconv.FormatInt(id, 10)}
}

func standardMetricProfessionalNode(metric *models.MetricDefinitionAggregate) models.ProfessionalRelationNode {
	name, status, kind := metric.Code, metric.LifecycleState, "metric"
	if revision := displayMetricRevision(*metric); revision != nil {
		name, status, kind = revision.Name, revision.Status, revision.MetricType
	}
	return models.ProfessionalRelationNode{ProfessionalResourceKey: standardMetricProfessionalKey(metric.ID), Name: name, Code: metric.Code, Status: status, Kind: kind}
}

func (s *MetricService) GetProfessionalRelations(metricID, tenantID int64, limit int) (*models.ProfessionalRelationsResponse, error) {
	subject, err := s.metricRepo.GetAggregate(metricID, tenantID)
	if err != nil {
		return nil, err
	}
	response := &models.ProfessionalRelationsResponse{SchemaVersion: models.ProfessionalRelationsSchemaVersion, Subject: standardMetricProfessionalKey(subject.ID), Nodes: []models.ProfessionalRelationNode{standardMetricProfessionalNode(subject)}, Edges: []models.ProfessionalRelationEdge{}}
	revision := displayMetricRevision(*subject)
	if revision == nil {
		return response, nil
	}
	ids := make([]int64, 0, len(revision.Dependencies))
	for _, dependency := range revision.Dependencies {
		ids = append(ids, dependency.DependencyDefinitionID)
	}
	related, err := s.metricRepo.GetByIDs(ids, tenantID)
	if err != nil {
		return nil, err
	}
	byID := map[int64]*models.MetricDefinitionAggregate{}
	for index := range related {
		byID[related[index].ID] = &related[index]
	}
	for _, dependency := range revision.Dependencies {
		if len(response.Edges) >= limit {
			response.Truncated = true
			break
		}
		target, ok := byID[dependency.DependencyDefinitionID]
		if !ok {
			continue
		}
		response.Nodes = append(response.Nodes, standardMetricProfessionalNode(target))
		response.Edges = append(response.Edges, models.ProfessionalRelationEdge{ID: fmt.Sprintf("standard:metric_revision_dependency:%d", dependency.ID), RelationKind: "standard.metric." + dependency.RelationKind, Source: response.Subject, Target: standardMetricProfessionalKey(target.ID), Coefficient: dependency.Coefficient, Note: dependency.Note})
	}
	return response, nil
}
