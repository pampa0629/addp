package utils

import (
	"encoding/json"
	"strings"

	"github.com/addp/common/models"
)

// CapabilityFilter 能力过滤器
type CapabilityFilter struct {
	StorageTypes []string // 存储能力类型：relational_db, object_storage, generic
	ComputeTypes []string // 计算能力类型：sql_query, scan, transform 等
	RequireBoth  bool     // true: 同时满足 storage 和 compute, false: 满足任一即可
}

// ParseCapabilities 解析资源的 capabilities JSON
func ParseCapabilities(capabilitiesJSON *string) (*models.Capability, error) {
	if capabilitiesJSON == nil || *capabilitiesJSON == "" {
		return nil, nil
	}

	var cap models.Capability
	if err := json.Unmarshal([]byte(*capabilitiesJSON), &cap); err != nil {
		return nil, err
	}

	return &cap, nil
}

// HasStorageCapability 检查资源是否具有存储能力
func HasStorageCapability(resource *models.Resource) bool {
	cap, err := ParseCapabilities(resource.Capabilities)
	if err != nil || cap == nil {
		return false
	}

	return len(cap.Storage) > 0
}

// HasComputeCapability 检查资源是否具有指定的计算能力
func HasComputeCapability(resource *models.Resource, computeType string) bool {
	cap, err := ParseCapabilities(resource.Capabilities)
	if err != nil || cap == nil {
		return false
	}

	for _, compute := range cap.Compute {
		if compute.Type == computeType {
			return true
		}
	}

	return false
}

// HasStorageType 检查资源是否具有指定类型的存储能力
func HasStorageType(resource *models.Resource, storageType string) bool {
	cap, err := ParseCapabilities(resource.Capabilities)
	if err != nil || cap == nil {
		return false
	}

	for _, storage := range cap.Storage {
		if storage.Type == storageType {
			return true
		}
	}

	return false
}

// IsRelationalDatabase 检查资源是否为关系型数据库
func IsRelationalDatabase(resource *models.Resource) bool {
	return HasStorageType(resource, "relational_db")
}

// IsObjectStorage 检查资源是否为对象存储
func IsObjectStorage(resource *models.Resource) bool {
	return HasStorageType(resource, "object_storage")
}

// SupportsSQLQuery 检查资源是否支持 SQL 查询
func SupportsSQLQuery(resource *models.Resource) bool {
	return HasComputeCapability(resource, "sql_query")
}

// MatchesStorageTypes 检查资源是否匹配任一存储类型
func MatchesStorageTypes(resource *models.Resource, storageTypes []string) bool {
	if len(storageTypes) == 0 {
		return true // 空过滤器匹配所有
	}

	cap, err := ParseCapabilities(resource.Capabilities)
	if err != nil || cap == nil {
		return false
	}

	for _, storage := range cap.Storage {
		for _, targetType := range storageTypes {
			if storage.Type == targetType {
				return true
			}
		}
	}

	return false
}

// MatchesComputeTypes 检查资源是否匹配任一计算类型
func MatchesComputeTypes(resource *models.Resource, computeTypes []string) bool {
	if len(computeTypes) == 0 {
		return true // 空过滤器匹配所有
	}

	cap, err := ParseCapabilities(resource.Capabilities)
	if err != nil || cap == nil {
		return false
	}

	for _, compute := range cap.Compute {
		for _, targetType := range computeTypes {
			if compute.Type == targetType {
				return true
			}
		}
	}

	return false
}

// MatchesFilter 检查资源是否匹配过滤器
func MatchesFilter(resource *models.Resource, filter CapabilityFilter) bool {
	// 空过滤器匹配所有资源
	if len(filter.StorageTypes) == 0 && len(filter.ComputeTypes) == 0 {
		return true
	}

	matchesStorage := MatchesStorageTypes(resource, filter.StorageTypes)
	matchesCompute := MatchesComputeTypes(resource, filter.ComputeTypes)

	if filter.RequireBoth {
		// AND 逻辑：必须同时满足
		return matchesStorage && matchesCompute
	}

	// OR 逻辑：满足任一即可
	if len(filter.StorageTypes) > 0 && len(filter.ComputeTypes) > 0 {
		return matchesStorage || matchesCompute
	}

	// 只有一种过滤条件
	if len(filter.StorageTypes) > 0 {
		return matchesStorage
	}

	return matchesCompute
}

// FilterResourcesByCapability 按能力过滤资源列表
func FilterResourcesByCapability(resources []models.Resource, filter CapabilityFilter) []models.Resource {
	// 空过滤器返回所有资源
	if len(filter.StorageTypes) == 0 && len(filter.ComputeTypes) == 0 {
		return resources
	}

	var filtered []models.Resource
	for _, resource := range resources {
		if MatchesFilter(&resource, filter) {
			filtered = append(filtered, resource)
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

// SupportsDevMode 检查资源是否支持指定的开发模式
// devMode: "sql", "workflow", "form", "script"
func SupportsDevMode(resource *models.Resource, devMode string) bool {
	cap, err := ParseCapabilities(resource.Capabilities)
	if err != nil || cap == nil {
		return false
	}

	// 检查所有计算能力
	for _, compute := range cap.Compute {
		for _, mode := range compute.DevModes {
			if mode == devMode {
				return true
			}
		}
	}

	return false
}

// GetSupportedDevModes 获取资源支持的所有开发模式
func GetSupportedDevModes(resource *models.Resource) []string {
	cap, err := ParseCapabilities(resource.Capabilities)
	if err != nil || cap == nil {
		return []string{}
	}

	// 使用map去重
	modeSet := make(map[string]bool)
	for _, compute := range cap.Compute {
		for _, mode := range compute.DevModes {
			modeSet[mode] = true
		}
	}

	// 转换为数组
	modes := make([]string, 0, len(modeSet))
	for mode := range modeSet {
		modes = append(modes, mode)
	}

	return modes
}

// FilterResourcesByDevMode 过滤出支持指定开发模式的资源列表
func FilterResourcesByDevMode(resources []models.Resource, devMode string) []models.Resource {
	filtered := make([]models.Resource, 0)

	for _, resource := range resources {
		if SupportsDevMode(&resource, devMode) {
			filtered = append(filtered, resource)
		}
	}

	return filtered
}

// IsAPIEngine 判断资源是否为API引擎(内置模块)
func IsAPIEngine(resource *models.Resource) bool {
	// API引擎的资源类型以 "api." 开头
	if len(resource.ResourceType) > 4 && resource.ResourceType[:4] == "api." {
		return true
	}
	return false
}

// IsStandardLibraryEngine 判断资源是否为标准库引擎(JDBC/S3协议)
func IsStandardLibraryEngine(resource *models.Resource) bool {
	standardTypes := map[string]bool{
		"postgresql": true,
		"postgres":   true,
		"mysql":      true,
		"doris":      true,
		"spark_sql":  true,
		"minio":      true,
		"s3":         true,
		"oss":        true,
	}

	return standardTypes[resource.ResourceType]
}
