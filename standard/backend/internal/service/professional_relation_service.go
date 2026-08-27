package service

import (
	"fmt"
	"strconv"

	"github.com/addp/standard/internal/models"
)

func standardMetricProfessionalKey(id int64) models.ProfessionalResourceKey {
	return models.ProfessionalResourceKey{OwnerModule: "standard", ResourceType: "metric", ResourceID: strconv.FormatInt(id, 10)}
}

func standardMetricProfessionalNode(metric *models.Metric) models.ProfessionalRelationNode {
	return models.ProfessionalRelationNode{
		ProfessionalResourceKey: standardMetricProfessionalKey(metric.ID),
		Name:                    metric.Name, Code: metric.Code, Status: metric.Status, Kind: metric.Type,
	}
}

func (s *MetricService) GetProfessionalRelations(metricID, tenantID int64, limit int) (*models.ProfessionalRelationsResponse, error) {
	subject, err := s.metricRepo.GetByID(metricID, tenantID)
	if err != nil {
		return nil, err
	}
	dependencies, err := s.metricRepo.GetDependenciesByMetric(metricID, tenantID)
	if err != nil {
		return nil, err
	}

	relatedIDs := make([]int64, 0, len(dependencies)*2+1)
	if subject.BaseMetricID != nil {
		relatedIDs = append(relatedIDs, *subject.BaseMetricID)
	}
	for _, dependency := range dependencies {
		relatedIDs = append(relatedIDs, dependency.FromMetricID, dependency.ToMetricID)
	}
	relatedMetrics, err := s.metricRepo.GetByIDs(relatedIDs, tenantID)
	if err != nil {
		return nil, err
	}
	metricByID := map[int64]*models.Metric{subject.ID: subject}
	for index := range relatedMetrics {
		metric := &relatedMetrics[index]
		metricByID[metric.ID] = metric
	}

	response := &models.ProfessionalRelationsResponse{
		SchemaVersion: models.ProfessionalRelationsSchemaVersion,
		Subject:       standardMetricProfessionalKey(subject.ID),
		Nodes:         []models.ProfessionalRelationNode{standardMetricProfessionalNode(subject)},
		Edges:         []models.ProfessionalRelationEdge{},
	}
	seen := map[int64]struct{}{subject.ID: {}}
	appendNode := func(id int64) bool {
		if _, ok := seen[id]; ok {
			return true
		}
		metric, ok := metricByID[id]
		if !ok {
			return false
		}
		seen[id] = struct{}{}
		response.Nodes = append(response.Nodes, standardMetricProfessionalNode(metric))
		return true
	}
	appendEdge := func(edge models.ProfessionalRelationEdge) bool {
		if len(response.Edges) >= limit {
			response.Truncated = true
			return false
		}
		response.Edges = append(response.Edges, edge)
		return true
	}

	if subject.BaseMetricID != nil && appendNode(*subject.BaseMetricID) {
		appendEdge(models.ProfessionalRelationEdge{
			ID:           fmt.Sprintf("standard:metric_base:%d", subject.ID),
			RelationKind: "standard.metric.base_metric",
			Source:       response.Subject, Target: standardMetricProfessionalKey(*subject.BaseMetricID),
		})
	}
	for _, dependency := range dependencies {
		if len(response.Edges) >= limit {
			response.Truncated = true
			break
		}
		if !appendNode(dependency.FromMetricID) || !appendNode(dependency.ToMetricID) {
			continue
		}
		appendEdge(models.ProfessionalRelationEdge{
			ID:           fmt.Sprintf("standard:metric_dependency:%d", dependency.ID),
			RelationKind: "standard.metric.dependency",
			Source:       standardMetricProfessionalKey(dependency.FromMetricID),
			Target:       standardMetricProfessionalKey(dependency.ToMetricID),
			Coefficient:  dependency.Coefficient, Note: dependency.Note,
		})
	}
	return response, nil
}
