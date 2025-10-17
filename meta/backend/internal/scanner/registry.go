package scanner

import (
	"sort"
	"strings"
	"sync"
)

// ExtractorRegistry 元数据提取器注册表
type ExtractorRegistry struct {
	extractors map[string][]MetadataExtractor // key: MIME type
	mu         sync.RWMutex
}

// 全局默认注册表
var defaultRegistry = &ExtractorRegistry{
	extractors: make(map[string][]MetadataExtractor),
}

// Register 注册一个元数据提取器到全局注册表
// 通常在各提取器包的 init() 函数中调用
func Register(extractor MetadataExtractor) {
	defaultRegistry.Register(extractor)
}

// Register 注册一个元数据提取器到当前注册表
func (r *ExtractorRegistry) Register(extractor MetadataExtractor) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, mimeType := range extractor.SupportedTypes() {
		// 规范化MIME类型（转小写）
		mimeType = strings.ToLower(strings.TrimSpace(mimeType))
		r.extractors[mimeType] = append(r.extractors[mimeType], extractor)
	}

	// 为每个MIME类型的提取器按优先级排序
	for _, extractors := range r.extractors {
		sort.Slice(extractors, func(i, j int) bool {
			return extractors[i].Priority() > extractors[j].Priority()
		})
	}
}

// GetExtractor 根据内容类型获取最佳提取器（优先级最高的）
// 如果没有找到匹配的提取器，返回 nil
func GetExtractor(contentType string) MetadataExtractor {
	return defaultRegistry.GetExtractor(contentType)
}

// GetExtractor 从当前注册表获取最佳提取器
func (r *ExtractorRegistry) GetExtractor(contentType string) MetadataExtractor {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// 规范化MIME类型
	contentType = strings.ToLower(strings.TrimSpace(contentType))

	// 1. 完全匹配
	if extractors := r.extractors[contentType]; len(extractors) > 0 {
		return extractors[0] // 返回优先级最高的
	}

	// 2. 尝试主类型匹配（例如 "image/jpeg" -> "image/*"）
	if parts := strings.Split(contentType, "/"); len(parts) == 2 {
		wildcardType := parts[0] + "/*"
		if extractors := r.extractors[wildcardType]; len(extractors) > 0 {
			return extractors[0]
		}
	}

	// 3. 尝试通配符匹配 "*/*"
	if extractors := r.extractors["*/*"]; len(extractors) > 0 {
		return extractors[0]
	}

	return nil
}

// GetAllExtractors 获取所有支持指定内容类型的提取器（按优先级排序）
func GetAllExtractors(contentType string) []MetadataExtractor {
	return defaultRegistry.GetAllExtractors(contentType)
}

// GetAllExtractors 从当前注册表获取所有匹配的提取器
func (r *ExtractorRegistry) GetAllExtractors(contentType string) []MetadataExtractor {
	r.mu.RLock()
	defer r.mu.RUnlock()

	contentType = strings.ToLower(strings.TrimSpace(contentType))

	// 收集所有匹配的提取器
	var result []MetadataExtractor

	// 1. 完全匹配
	if extractors := r.extractors[contentType]; len(extractors) > 0 {
		result = append(result, extractors...)
	}

	// 2. 主类型匹配
	if parts := strings.Split(contentType, "/"); len(parts) == 2 {
		wildcardType := parts[0] + "/*"
		if extractors := r.extractors[wildcardType]; len(extractors) > 0 {
			result = append(result, extractors...)
		}
	}

	// 3. 通配符匹配
	if extractors := r.extractors["*/*"]; len(extractors) > 0 {
		result = append(result, extractors...)
	}

	// 去重并按优先级排序
	if len(result) > 0 {
		// 使用map去重
		seen := make(map[MetadataExtractor]bool)
		unique := []MetadataExtractor{}
		for _, ext := range result {
			if !seen[ext] {
				seen[ext] = true
				unique = append(unique, ext)
			}
		}

		// 按优先级排序
		sort.Slice(unique, func(i, j int) bool {
			return unique[i].Priority() > unique[j].Priority()
		})

		return unique
	}

	return nil
}

// ListRegisteredTypes 列出所有已注册的MIME类型
func ListRegisteredTypes() []string {
	return defaultRegistry.ListRegisteredTypes()
}

// ListRegisteredTypes 列出当前注册表中的所有MIME类型
func (r *ExtractorRegistry) ListRegisteredTypes() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	types := make([]string, 0, len(r.extractors))
	for mimeType := range r.extractors {
		types = append(types, mimeType)
	}

	sort.Strings(types)
	return types
}

// Count 返回已注册的提取器总数
func Count() int {
	return defaultRegistry.Count()
}

// Count 返回当前注册表中的提取器总数
func (r *ExtractorRegistry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()

	count := 0
	for _, extractors := range r.extractors {
		count += len(extractors)
	}
	return count
}

// Clear 清空注册表（主要用于测试）
func Clear() {
	defaultRegistry.Clear()
}

// Clear 清空当前注册表
func (r *ExtractorRegistry) Clear() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.extractors = make(map[string][]MetadataExtractor)
}
