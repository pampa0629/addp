package service

import (
	"context"
	"encoding/json"
	"time"

	commonmodels "github.com/addp/common/models"
	"github.com/addp/system/internal/models"
)

func parseCDCMetrics(raw json.RawMessage, services []models.DiscoveredDeploymentService, at time.Time) map[string]*commonmodels.EngineMetricsSource {
	result := map[string]*commonmodels.EngineMetricsSource{}
	if len(raw) == 0 {
		return nil
	}
	var declared map[string]*commonmodels.EngineMetricsSource
	if DecodeDeploymentJSON(raw, &declared) != nil || declared == nil || len(declared) > 2 {
		return result
	}
	for kind := range declared {
		if kind != "redpanda" && kind != "kafka-connect" {
			return result
		}
	}
	for kind, metrics := range declared {
		result[kind] = matchComponentMetrics(metrics, services, kind, at)
	}
	return result
}

func parseComponentMetrics(raw json.RawMessage, services []models.DiscoveredDeploymentService, kind string, at time.Time) *commonmodels.EngineMetricsSource {
	if len(raw) == 0 {
		return &commonmodels.EngineMetricsSource{State: "not_deployed"}
	}
	var claim commonmodels.EngineMetricsSource
	if DecodeDeploymentJSON(raw, &claim) != nil {
		return &commonmodels.EngineMetricsSource{State: "unavailable"}
	}
	return matchComponentMetrics(&claim, services, kind, at)
}

func matchComponentMetrics(metrics *commonmodels.EngineMetricsSource, services []models.DiscoveredDeploymentService, kind string, at time.Time) *commonmodels.EngineMetricsSource {
	if metrics != nil && metrics.Validate() == nil && (metrics.StartedAt == nil || !metrics.StartedAt.After(at)) {
		if metrics.State != "available" {
			return metrics
		}
		for _, service := range services {
			if service.Service == kind && len(service.Containers) == 1 {
				c := service.Containers[0]
				started, err := time.Parse(time.RFC3339Nano, c.StartedAt)
				if err == nil && c.Status == "running" && c.ContainerID == metrics.ContainerID && started.Equal(*metrics.StartedAt) {
					return metrics
				}
			}
		}
	}
	return &commonmodels.EngineMetricsSource{State: "unavailable"}
}

func (s *ModuleRegistryService) enrichInfrastructureComponents(ctx context.Context, snapshot *commonmodels.ObservabilityIdentitySnapshot) {
	projection := &commonmodels.ObservabilityComponentSnapshot{State: "unavailable", Items: []commonmodels.ObservabilityComponentIdentity{}}
	snapshot.InfrastructureComponents = projection
	value := s.deploymentDiscovery.readScope(ctx, "infra")
	if value.State != "available" || value.Source == nil {
		return
	}
	projection.State = "available"
	for _, service := range value.Services {
		if service.Service != "redpanda" && service.Service != "kafka-connect" && service.Service != "meilisearch" {
			continue
		}
		if len(service.Containers) != 1 || service.Containers[0].Status != "running" {
			continue
		}
		container := service.Containers[0]
		started, err := time.Parse(time.RFC3339Nano, container.StartedAt)
		if err != nil || started.After(snapshot.ObservedAt) {
			continue
		}
		metrics := &commonmodels.EngineMetricsSource{State: "not_deployed"}
		if service.Service == "meilisearch" {
			metrics = value.MeilisearchMetrics
		} else if value.CDCMetrics != nil {
			metrics = value.CDCMetrics[service.Service]
			if metrics == nil {
				metrics = &commonmodels.EngineMetricsSource{State: "unavailable"}
			}
		}
		item := commonmodels.ObservabilityComponentIdentity{DockerEngineID: value.Source.DockerEngineID, Project: value.Source.Project,
			Kind: service.Service, ContainerID: container.ContainerID, StartedAt: started, Metrics: metrics}
		if item.Validate() == nil {
			projection.Items = append(projection.Items, item)
		}
	}
}
