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
	byScope := map[string]commonmodels.ObservabilityEngineSourceKey{}
	metrics := map[string]*commonmodels.PostgreSQLMetricsSource{}
	for _, scope := range []string{"infra", "business"} {
		value := s.deploymentDiscovery.readScope(ctx, scope)
		if value.State == "available" && value.Source != nil {
			byScope[scope] = commonmodels.ObservabilityEngineSourceKey{DockerEngineID: value.Source.DockerEngineID, Project: value.Source.Project, Service: "postgres"}
			metrics[scope] = value.PostgreSQLMetrics
		}
	}
	for index := range snapshot.EngineDeployments {
		engine := &snapshot.EngineDeployments[index]
		engine.Metrics = &commonmodels.PostgreSQLMetricsSource{State: "unavailable"}
		if engine.SourceKey == byScope[engine.Scope] && metrics[engine.Scope] != nil {
			engine.Metrics = metrics[engine.Scope]
		}
	}
}
