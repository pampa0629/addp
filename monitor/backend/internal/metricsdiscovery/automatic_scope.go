package metricsdiscovery

import (
	"strconv"

	"github.com/addp/common/models"
)

const CollectionScopeLabel = "__meta_addp_collection_scope"
const UnmanagedCollectionScope = "unmanaged"
const AutomaticScopeLimit = 1000

type AutomaticScope struct {
	ID          string `gorm:"primaryKey;size:128" json:"id"`
	Kind        string `json:"kind"`
	ModuleName  string `json:"module_name,omitempty"`
	Role        string `json:"role,omitempty"`
	RuntimeName string `json:"runtime_name,omitempty"`
}

func ProcessCollectionScope(instance models.ObservabilityModuleIdentity) AutomaticScope {
	if !models.ValidServiceGroupName(instance.ModuleName) {
		return AutomaticScope{}
	}
	switch instance.Role {
	case "worker":
		if !models.ValidServiceGroupName(instance.RuntimeName) {
			return AutomaticScope{}
		}
	case "backend", "ingress", "scheduler":
		if instance.RuntimeName != "" {
			return AutomaticScope{}
		}
	default:
		return AutomaticScope{}
	}
	return AutomaticScope{
		ID: "service:" + instance.ModuleName + ":" + instance.Role + ":" + instance.RuntimeName, Kind: "service",
		ModuleName: instance.ModuleName, Role: instance.Role, RuntimeName: instance.RuntimeName,
	}
}

func ComponentCollectionScope(component models.ObservabilityComponentIdentity) AutomaticScope {
	if component.Kind == "meilisearch" {
		return AutomaticScope{ID: "meilisearch:" + component.ID(), Kind: component.Kind}
	}
	return AutomaticScope{}
}

func RuntimeCollectionScope(runtime *models.RuntimeMetricsIdentity) AutomaticScope {
	if runtime == nil || runtime.EngineID == 0 {
		return AutomaticScope{}
	}
	return AutomaticScope{ID: runtime.Kind + ":" + strconv.FormatUint(uint64(runtime.EngineID), 10), Kind: runtime.Kind}
}

func (s AutomaticScope) DiscoveryLabel() string {
	if s.ID == "" {
		return UnmanagedCollectionScope
	}
	return s.ID
}
