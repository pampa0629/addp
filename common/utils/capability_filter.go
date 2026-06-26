package utils

import (
	"fmt"
	"strings"

	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/common/models"
)

// CapabilityFilter 能力过滤器
type CapabilityFilter struct {
	StorageTypes []string // 存储能力族：tabular, dynamic_schema, graph, object, file
	RequireBoth  bool     // 预留字段，当前仅支持存储能力过滤
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

// HasStorageType 检查资源是否具有指定类型的存储能力
func HasStorageType(resource *models.Engine, storageType string) bool {
	cap, err := ParseCapabilities(resource.Capabilities)
	if err != nil || cap == nil {
		return false
	}

	return hasStorageFamily(cap, storageType)
}

// IsRelationalDatabase 检查资源是否为表格型数据库
func IsRelationalDatabase(resource *models.Engine) bool {
	return HasStorageType(resource, "tabular")
}

// IsObjectStorage 检查资源是否为对象存储
func IsObjectStorage(resource *models.Engine) bool {
	return HasStorageType(resource, "object")
}

// MatchesStorageTypes 检查资源是否匹配任一存储类型
func MatchesStorageTypes(resource *models.Engine, storageTypes []string) bool {
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

// MatchesFilter 检查资源是否匹配过滤器
func MatchesFilter(resource *models.Engine, filter CapabilityFilter) bool {
	// 空过滤器匹配所有资源
	if len(filter.StorageTypes) == 0 {
		return true
	}

	return MatchesStorageTypes(resource, filter.StorageTypes)
}

// FilterEnginesByCapability 按能力过滤引擎列表
func FilterEnginesByCapability(engines []models.Engine, filter CapabilityFilter) []models.Engine {
	// 空过滤器返回所有引擎
	if len(filter.StorageTypes) == 0 {
		return engines
	}

	var filtered []models.Engine
	for _, engine := range engines {
		if MatchesFilter(&engine, filter) {
			filtered = append(filtered, engine)
		}
	}

	return filtered
}

// ParseCommaSeparated 解析逗号分隔的字符串
func ParseCommaSeparated(s string) []string {
	if s == "" {
		return nil
	}

	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}

	return result
}

// SupportsComputeEntrypoint 检查资源是否支持指定的计算入口。
// 事实源是 engine.capabilities/v1 的 compute.query/workflow/script。
// entrypoint: "query"/"sql", "workflow", "notebook"/"script"
func SupportsComputeEntrypoint(resource *models.Engine, entrypoint string) bool {
	cap, err := ParseCapabilities(resource.Capabilities)
	if err != nil || cap == nil {
		return false
	}

	return supportsComputeEntrypoint(cap, entrypoint)
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
	default:
		return false
	}
}

func computeEntrypoints(capabilities *engineplugin.EngineCapabilities) []string {
	if capabilities == nil || capabilities.Compute == nil {
		return []string{}
	}

	entrypoints := make([]string, 0, 3)
	if capabilities.Compute.Query != nil && capabilities.Compute.Query.Supported {
		entrypoints = append(entrypoints, "query")
	}
	if capabilities.Compute.Workflow != nil && capabilities.Compute.Workflow.Supported {
		entrypoints = append(entrypoints, "workflow")
	}
	if capabilities.Compute.Script != nil && capabilities.Compute.Script.Supported {
		entrypoints = append(entrypoints, "notebook")
	}
	return entrypoints
}

// FilterEnginesByComputeEntrypoint 过滤出支持指定计算入口的引擎列表
func FilterEnginesByComputeEntrypoint(engines []models.Engine, entrypoint string) []models.Engine {
	filtered := make([]models.Engine, 0)

	for _, engine := range engines {
		if SupportsComputeEntrypoint(&engine, entrypoint) {
			filtered = append(filtered, engine)
		}
	}

	return filtered
}

// IsAPIEngine 判断资源是否为API引擎(内置模块)
func IsAPIEngine(resource *models.Engine) bool {
	// API引擎的资源类型以 "api." 开头
	if len(resource.EngineType) > 4 && resource.EngineType[:4] == "api." {
		return true
	}
	return false
}

// IsStandardLibraryEngine 判断资源是否为标准库引擎(JDBC/S3协议)
func IsStandardLibraryEngine(resource *models.Engine) bool {
	standardTypes := map[string]bool{
		"postgresql": true,
		"postgres":   true,
		"mysql":      true,
		"doris":      true,
		"spark":      true,
		"minio":      true,
		"s3":         true,
		"oss":        true,
	}

	return standardTypes[resource.EngineType]
}
