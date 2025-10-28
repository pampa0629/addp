package format

import (
	"sync"
)

// TypeMapper 类型映射器接口
// 不同的数据源（PostgreSQL、MySQL、Shapefile等）实现此接口
// 提供原生类型与通用类型之间的双向转换
type TypeMapper interface {
	// Name 返回类型映射器的名称（如 "postgresql", "mysql", "shapefile"）
	Name() string

	// ToCommon 将原生类型转换为通用类型
	// nativeType: 原生类型字符串（如 PostgreSQL 的 "varchar", MySQL 的 "int"）
	// 返回: 通用的 FieldType
	ToCommon(nativeType string) FieldType

	// FromCommon 将通用类型转换为原生类型
	// commonType: 通用的 FieldType
	// 返回: (原生类型名称, 长度/精度, 小数位数)
	FromCommon(commonType FieldType) (nativeType string, size int, precision int)
}

// TypeMapperRegistry 类型映射器注册表
type TypeMapperRegistry struct {
	mappers map[string]TypeMapper // key: mapper name (e.g., "postgresql", "mysql")
	mu      sync.RWMutex
}

// 全局默认注册表
var defaultMapperRegistry = &TypeMapperRegistry{
	mappers: make(map[string]TypeMapper),
}

// RegisterTypeMapper 注册类型映射器到全局注册表
// 通常在各映射器包的 init() 函数中调用
func RegisterTypeMapper(mapper TypeMapper) {
	defaultMapperRegistry.Register(mapper)
}

// Register 注册类型映射器到当前注册表
func (r *TypeMapperRegistry) Register(mapper TypeMapper) {
	if mapper == nil {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	r.mappers[mapper.Name()] = mapper
}

// GetTypeMapper 根据名称获取类型映射器
// name: 映射器名称（如 "postgresql", "mysql", "shapefile"）
// 返回: TypeMapper 或 nil（未找到）
func GetTypeMapper(name string) TypeMapper {
	return defaultMapperRegistry.GetTypeMapper(name)
}

// GetTypeMapper 从当前注册表获取类型映射器
func (r *TypeMapperRegistry) GetTypeMapper(name string) TypeMapper {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return r.mappers[name]
}

// ListTypeMappers 列出所有已注册的类型映射器名称
func ListTypeMappers() []string {
	return defaultMapperRegistry.ListTypeMappers()
}

// ListTypeMappers 列出当前注册表中的所有映射器名称
func (r *TypeMapperRegistry) ListTypeMappers() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.mappers))
	for name := range r.mappers {
		names = append(names, name)
	}
	return names
}

// HasTypeMapper 检查是否存在指定名称的类型映射器
func HasTypeMapper(name string) bool {
	return defaultMapperRegistry.HasTypeMapper(name)
}

// HasTypeMapper 检查当前注册表中是否存在指定映射器
func (r *TypeMapperRegistry) HasTypeMapper(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	_, exists := r.mappers[name]
	return exists
}

// Clear 清空注册表（主要用于测试）
func ClearTypeMappers() {
	defaultMapperRegistry.Clear()
}

// Clear 清空当前注册表
func (r *TypeMapperRegistry) Clear() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.mappers = make(map[string]TypeMapper)
}
