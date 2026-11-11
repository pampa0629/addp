package service

import (
	"context"
	"encoding/json"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/system/internal/models"
	"time"
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
		result[kind] = &commonmodels.EngineMetricsSource{State: "unavailable"}
		if metrics == nil || metrics.Validate() != nil || (metrics.StartedAt != nil && metrics.StartedAt.After(at)) {
			continue
		}
		if metrics.State == "available" {
			matched := false
			for _, service := range services {
				if service.Service == kind && len(service.Containers) == 1 {
					c := service.Containers[0]
					started, err := time.Parse(time.RFC3339Nano, c.StartedAt)
					matched = err == nil && c.Status == "running" && c.ContainerID == metrics.ContainerID && started.Equal(*metrics.StartedAt)
				}
			}
			if !matched {
				continue
			}
		}
		result[kind] = metrics
	}
	return result
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
		if service.Service != "redpanda" && service.Service != "kafka-connect" {
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
		if value.CDCMetrics != nil {
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
