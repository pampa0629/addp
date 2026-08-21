package selection

import (
	"fmt"

	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/common/models"
)

// CapabilityFilter 能力过滤器
type CapabilityFilter struct {
	StorageTypes []string // 存储能力族：tabular, dynamic_schema, graph, object, file
}

// ParseCapabilities 解析资源的结构化 capabilities JSON
func ParseCapabilities(capabilitiesJSON *models.JSONString) (*engineplugin.EngineCapabilities, error) {
	if capabilitiesJSON == nil || *capabilitiesJSON == "" {
		return nil, nil
	}

	capabilities, err := engineplugin.ParseEngineCapabilities(string(*capabilitiesJSON))
	if err != nil {
		return nil, err
	}
	if capabilities == nil {
		return nil, nil
	}
	if capabilities.SchemaVersion != engineplugin.CapabilitiesSchemaVersion {
		return nil, fmt.Errorf("capabilities schema_version must be %s", engineplugin.CapabilitiesSchemaVersion)
	}
	return capabilities, nil
}

// HasStorageCapability 检查资源是否具有存储能力
func HasStorageCapability(resource *models.Engine) bool {
	cap, err := ParseCapabilities(resource.Capabilities)
	if err != nil || cap == nil {
		return false
	}

	return cap.Storage != nil
}

func matchesStorageTypes(resource *models.Engine, storageTypes []string) bool {
	if len(storageTypes) == 0 {
		return true // 空过滤器匹配所有
	}

	cap, err := ParseCapabilities(resource.Capabilities)
	if err != nil || cap == nil {
		return false
	}

	for _, targetType := range storageTypes {
		if hasStorageFamily(cap, targetType) {
			return true
		}
	}

	return false
}

func matchesFilter(resource *models.Engine, filter CapabilityFilter) bool {
	// 空过滤器匹配所有资源
	if len(filter.StorageTypes) == 0 {
		return true
	}

	return matchesStorageTypes(resource, filter.StorageTypes)
}

// FilterEnginesByCapability 按能力过滤引擎列表
func FilterEnginesByCapability(engines []models.Engine, filter CapabilityFilter) []models.Engine {
	// 空过滤器返回所有引擎
	if len(filter.StorageTypes) == 0 {
		return engines
	}

	var filtered []models.Engine
	for _, engine := range engines {
		if matchesFilter(&engine, filter) {
			filtered = append(filtered, engine)
		}
	}

	return filtered
}

// SupportsComputeEntrypoint 检查资源是否支持指定的计算入口。
// 事实源是 engine.capabilities/v1 的 compute.query/workflow/script/inference。
// entrypoint: "query"/"sql", "workflow", "notebook"/"script", "inference"
func SupportsComputeEntrypoint(resource *models.Engine, entrypoint string) bool {
	cap, err := ParseCapabilities(resource.Capabilities)
	if err != nil || cap == nil {
		return false
	}

	return supportsComputeEntrypoint(cap, entrypoint)
}

// IsAvailable reports whether an Engine Instance may enter a new business
// selection. Management views and existing bindings must not use this helper
// to hide registered instances.
func IsAvailable(resource *models.Engine) bool {
	return resource != nil &&
		resource.LifecycleState == models.EngineLifecycleActive &&
		resource.ConnectionStatus == models.EngineConnectionOnline
}

// IsAvailableForComputeEntrypoint applies the single business-candidate rule
// before matching a compute capability.
func IsAvailableForComputeEntrypoint(resource *models.Engine, entrypoint string) bool {
	return IsAvailable(resource) && SupportsComputeEntrypoint(resource, entrypoint)
}

// IsAvailableStorageEngine applies the single business-candidate rule before
// matching storage capability.
func IsAvailableStorageEngine(resource *models.Engine) bool {
	return IsAvailable(resource) && HasStorageCapability(resource)
}

// GetSupportedComputeEntrypoints 获取资源支持的计算入口名称，名称由 compute 能力派生。
func GetSupportedComputeEntrypoints(resource *models.Engine) []string {
	cap, err := ParseCapabilities(resource.Capabilities)
	if err != nil || cap == nil {
		return []string{}
	}

	return computeEntrypoints(cap)
}

func hasStorageFamily(capabilities *engineplugin.EngineCapabilities, storageType string) bool {
	if capabilities == nil || capabilities.Storage == nil {
		return false
	}

	return capabilities.EngineFamily == storageType
}

func supportsComputeEntrypoint(capabilities *engineplugin.EngineCapabilities, entrypoint string) bool {
	if capabilities == nil || capabilities.Compute == nil {
		return false
	}

	switch entrypoint {
	case "query", "sql":
		return capabilities.Compute.Query != nil && capabilities.Compute.Query.Supported
	case "workflow":
		return capabilities.Compute.Workflow != nil && capabilities.Compute.Workflow.Supported
	case "notebook", "script":
		return capabilities.Compute.Script != nil && capabilities.Compute.Script.Supported
	case "inference":
		return capabilities.Compute.Inference != nil && capabilities.Compute.Inference.Supported
	default:
		return false
	}
}

func computeEntrypoints(capabilities *engineplugin.EngineCapabilities) []string {
	if capabilities == nil || capabilities.Compute == nil {
		return []string{}
	}

	entrypoints := make([]string, 0, 4)
	if capabilities.Compute.Query != nil && capabilities.Compute.Query.Supported {
		entrypoints = append(entrypoints, "query")
	}
	if capabilities.Compute.Workflow != nil && capabilities.Compute.Workflow.Supported {
		entrypoints = append(entrypoints, "workflow")
	}
	if capabilities.Compute.Script != nil && capabilities.Compute.Script.Supported {
		entrypoints = append(entrypoints, "notebook")
	}
	if capabilities.Compute.Inference != nil && capabilities.Compute.Inference.Supported {
		entrypoints = append(entrypoints, "inference")
	}
	return entrypoints
}
