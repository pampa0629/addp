package utils

import (
	"encoding/json"
	"fmt"
	"regexp"
)

// ExtractResourceInfo 从 URL 和请求体提取资源信息
func ExtractResourceInfo(method, path, requestBody string) (string, string) {
	// 方案 1: 基于 URL 模式匹配
	entityType, entityID := extractFromURL(path)
	if entityType != "" {
		return entityType, entityID
	}

	// 方案 2: 基于请求体 JSON 解析
	if requestBody != "" {
		entityType, entityID = extractFromRequestBody(requestBody)
		if entityType != "" {
			return entityType, entityID
		}
	}

	return "", ""
}

// extractFromURL 从 URL 中提取资源信息
func extractFromURL(path string) (string, string) {
	// 定义 URL 模式和对应的资源类型
	patterns := []struct {
		regex      *regexp.Regexp
		entityType string
	}{
		// System 模块
		{regexp.MustCompile(`^/api/users/(\d+)`), "user"},
		{regexp.MustCompile(`^/api/engines/(\d+)`), "engine"},
		{regexp.MustCompile(`^/api/tenants/(\d+)`), "tenant"},
		{regexp.MustCompile(`^/api/applications/(\d+)`), "application"},

		// Manager 模块
		{regexp.MustCompile(`^/api/resources/(\d+)`), "resource"},
		{regexp.MustCompile(`^/api/object-storage/(\d+)`), "storage"},

		// Meta 模块
		{regexp.MustCompile(`^/api/meta/schemas/(\d+)`), "schema"},
		{regexp.MustCompile(`^/api/meta/tables/(\d+)`), "table"},

		// Transfer 模块
		{regexp.MustCompile(`^/api/tasks/(\d+)`), "task"},
		{regexp.MustCompile(`^/api/jobs/(\d+)`), "job"},

		// Orchestrator 模块
		{regexp.MustCompile(`^/api/workflows/(\d+)`), "workflow"},
		{regexp.MustCompile(`^/api/dag/(\d+)`), "dag"},

		// Develop 模块
		{regexp.MustCompile(`^/api/queries/(\d+)`), "query"},
		{regexp.MustCompile(`^/api/scripts/(\d+)`), "script"},

		// Service 模块
		{regexp.MustCompile(`^/api/services/(\d+)`), "service"},
		{regexp.MustCompile(`^/api/endpoints/(\d+)`), "endpoint"},
	}

	for _, pattern := range patterns {
		if matches := pattern.regex.FindStringSubmatch(path); len(matches) > 1 {
			return pattern.entityType, matches[1]
		}
	}

	return "", ""
}

// extractFromRequestBody 从请求体提取资源信息
func extractFromRequestBody(requestBody string) (string, string) {
	// 尝试解析 JSON
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(requestBody), &data); err != nil {
		return "", ""
	}

	// 查找常见的 ID 字段
	idFields := []struct {
		key        string
		entityType string
	}{
		{"user_id", "user"},
		{"engine_id", "engine"},
		{"tenant_id", "tenant"},
		{"resource_id", "resource"},
		{"application_id", "application"},
		{"task_id", "task"},
		{"schema_id", "schema"},
		{"table_id", "table"},
		{"workflow_id", "workflow"},
		{"dag_id", "dag"},
		{"query_id", "query"},
		{"service_id", "service"},
	}

	for _, field := range idFields {
		if id, ok := data[field.key]; ok {
			return field.entityType, fmt.Sprintf("%v", id)
		}
	}

	return "", ""
}
