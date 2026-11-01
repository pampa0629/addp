package service

import (
	"context"

	commonmodels "github.com/addp/common/models"
)

// SetDeploymentDiscovery uses the same deployment-owned files as the public
// admission flow, without returning their private metrics fields to a browser.
func (s *ModuleRegistryService) SetDeploymentDiscovery(discovery *DeploymentDiscoveryService) {
	s.deploymentDiscovery = discovery
}

func (s *ModuleRegistryService) enrichEngineMetrics(ctx context.Context, snapshot *commonmodels.ObservabilityIdentitySnapshot) {
	if snapshot == nil {
		return
	}
	type source struct {
		key     commonmodels.ObservabilityEngineSourceKey
		metrics *commonmodels.EngineMetricsSource
	}
	byScope := map[string]map[string]source{}
	for _, scope := range []string{"infra", "business"} {
		value := s.deploymentDiscovery.readScope(ctx, scope)
		if value.State != "available" || value.Source == nil {
			continue
		}
		byScope[scope] = map[string]source{}
		for engineType, metrics := range map[string]*commonmodels.EngineMetricsSource{"postgresql": value.PostgreSQLMetrics, "redis": value.RedisMetrics} {
			service, _ := commonmodels.PlatformEngineService(engineType)
			byScope[scope][engineType] = source{commonmodels.ObservabilityEngineSourceKey{DockerEngineID: value.Source.DockerEngineID, Project: value.Source.Project, Service: service}, metrics}
		}
	}
	for index := range snapshot.EngineDeployments {
		engine := &snapshot.EngineDeployments[index]
		engine.Metrics = &commonmodels.EngineMetricsSource{State: "unavailable"}
		current := byScope[engine.Scope][engine.EngineType]
		if engine.SourceKey == current.key && current.metrics != nil && (current.metrics.StartedAt == nil || !current.metrics.StartedAt.After(snapshot.ObservedAt)) {
			engine.Metrics = current.metrics
		}
	}
}
