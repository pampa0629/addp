package audit

import (
	"strconv"
	"strings"
)

// EntityInfo 资源信息
type EntityInfo struct {
	Type string // 资源类型（如 user, engine, tenant...）
	ID   string // 资源ID
}

// PathRule 路径解析规则
type PathRule struct {
	Pattern    string // 路径模式（如 "/api/users/"）
	EntityType string // 资源类型
	IDPosition int    // ID 在路径中的位置（从0开始）
}

// 路径规则映射表
// 根据常见的 RESTful API 路径模式自动提取资源类型和ID
var pathPatterns = []PathRule{
	// Manager 模块
	{Pattern: "/api/v1/manager/embedding_executions", EntityType: "embedding_execution", IDPosition: 4},
	{Pattern: "/api/v1/manager/embeddings", EntityType: "embedding", IDPosition: 4},
	{Pattern: "/api/v1/manager/embedding_tasks", EntityType: "embedding_task", IDPosition: 4},
	{Pattern: "/api/v1/manager/tasks/embedding", EntityType: "embedding_execution", IDPosition: 5},

	// System 模块
	{Pattern: "/api/users/", EntityType: "user", IDPosition: 3},
	{Pattern: "/api/engines/", EntityType: "engine", IDPosition: 3},
	{Pattern: "/api/tenants/", EntityType: "tenant", IDPosition: 3},
	{Pattern: "/api/logs/", EntityType: "audit_log", IDPosition: 3},
	{Pattern: "/api/roles/", EntityType: "role", IDPosition: 3},

	// Meta 模块
	{Pattern: "/api/meta/scan-tasks/", EntityType: "scan_task", IDPosition: 4},
	{Pattern: "/api/meta/schemas/", EntityType: "schema", IDPosition: 4},
	{Pattern: "/api/meta/object-storage/", EntityType: "object_storage", IDPosition: 4},

	// Transfer 模块
	{Pattern: "/api/tasks/", EntityType: "transfer_task", IDPosition: 3},
	{Pattern: "/api/jobs/", EntityType: "transfer_job", IDPosition: 3},

	// Orchestrator 模块
	{Pattern: "/api/workflows/", EntityType: "workflow", IDPosition: 3},
	{Pattern: "/api/workflow-instances/", EntityType: "workflow_instance", IDPosition: 3},

	// Develop 模块
	{Pattern: "/api/develop/queries/", EntityType: "query", IDPosition: 4},
	{Pattern: "/api/develop/operators/", EntityType: "operator", IDPosition: 4},

	// Service 模块
	{Pattern: "/api/services/", EntityType: "service", IDPosition: 3},
}

// ParseEntityFromPath 从 HTTP 请求路径解析资源类型和 ID
//
// 示例：
//   - POST /api/users/123 → {Type: "user", ID: "123"}
//   - PUT /api/engines/456/scan → {Type: "engine", ID: "456"}
//   - DELETE /api/meta/scan-tasks/789 → {Type: "scan_task", ID: "789"}
//   - POST /api/tasks → {Type: "transfer_task", ID: ""}（创建操作，无ID）
func ParseEntityFromPath(method, path string) *EntityInfo {
	// 遍历所有规则
	for _, rule := range pathPatterns {
		if !strings.Contains(path, rule.Pattern) {
			continue
		}

		// 匹配到规则，开始提取 ID
		parts := strings.Split(strings.Trim(path, "/"), "/")

		// 检查路径部分是否足够长
		if len(parts) <= rule.IDPosition {
			// 路径较短，可能是列表查询或创建操作（如 POST /api/users）
			return &EntityInfo{Type: rule.EntityType, ID: ""}
		}

		// 提取 ID 部分
		idPart := parts[rule.IDPosition]

		// 验证 ID 是否有效（避免误匹配子资源）
		if isValidID(idPart) {
			return &EntityInfo{Type: rule.EntityType, ID: idPart}
		}

		// ID 无效，可能是子资源路径（如 /api/users/123/profile）
		// 仍然返回资源类型，但不设置 ID
		return &EntityInfo{Type: rule.EntityType, ID: ""}
	}

	// 未匹配到任何规则
	return nil
}

// isValidID 检查 ID 是否有效
// 有效的 ID 应该是：
// - 数字（如 "123"）
// - UUID 格式（如 "550e8400-e29b-41d4-a716-446655440000"）
// - 短字符串（如 "user-abc"）
// 无效的 ID：
// - 常见的 API 关键词（如 "search", "export", "list"）
func isValidID(id string) bool {
	// 空字符串无效
	if id == "" {
		return false
	}

	// 常见的 API 关键词（不应被识别为 ID）
	keywords := []string{
		"search", "export", "import", "list", "create",
		"update", "delete", "scan", "sync", "test",
		"health", "stats", "trends", "preview",
	}

	idLower := strings.ToLower(id)
	for _, keyword := range keywords {
		if idLower == keyword {
			return false
		}
	}

	// 检查是否为纯数字（最常见的 ID 格式）
	if _, err := strconv.Atoi(id); err == nil {
		return true
	}

	// 检查是否为 UUID 格式（包含连字符）
	if strings.Contains(id, "-") && len(id) >= 32 {
		return true
	}

	// 其他短字符串（长度 < 20）认为是有效 ID
	if len(id) < 20 {
		return true
	}

	return false
}

// GetEntityType 仅提取资源类型（不提取 ID）
// 适用于只需要知道操作的资源类型，不需要具体 ID 的场景
func GetEntityType(path string) string {
	for _, rule := range pathPatterns {
		if strings.Contains(path, rule.Pattern) {
			return rule.EntityType
		}
	}
	return ""
}
