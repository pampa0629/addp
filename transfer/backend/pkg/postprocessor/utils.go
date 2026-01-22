package postprocessor

import (
	"fmt"
	"strings"
)

// quoteIdentifier SQL 标识符引用
// 用双引号包裹标识符，防止 SQL 注入和保留字冲突
// 如果标识符中包含双引号，会转义为两个双引号
//
// identifier: SQL 标识符（表名、列名、约束名等）
// 返回：引用后的标识符
//
// 示例：
//   quoteIdentifier("users") → "users"
//   quoteIdentifier("my table") → "my table"
//   quoteIdentifier("col\"name") → "col""name"
func quoteIdentifier(identifier string) string {
	return `"` + strings.ReplaceAll(identifier, `"`, `""`) + `"`
}

// quoteIdentifiers 批量引用 SQL 标识符
// ids: SQL 标识符列表
// 返回：引用后的标识符列表
func quoteIdentifiers(ids []string) []string {
	result := make([]string, len(ids))
	for i, id := range ids {
		result[i] = quoteIdentifier(id)
	}
	return result
}

// qualifiedTableName 生成限定表名（schema.table）
// 自动引用标识符，处理包含特殊字符的表名
//
// tableName: 表名（可能包含 schema，如 "public.users" 或 "users"）
// 返回：引用后的限定表名
//
// 示例：
//   qualifiedTableName("users") → "users"
//   qualifiedTableName("public.users") → "public"."users"
//   qualifiedTableName("my schema.my table") → "my schema"."my table"
func qualifiedTableName(tableName string) string {
	schemaName, table := splitTableName(tableName)
	if schemaName == "" {
		return quoteIdentifier(table)
	}
	return fmt.Sprintf("%s.%s", quoteIdentifier(schemaName), quoteIdentifier(table))
}

// splitTableName 解析表名，提取 schema 和 table
// 支持以下格式：
//   - "users" → ("", "users")
//   - "public.users" → ("public", "users")
//   - "\"my schema\".\"my table\"" → ("my schema", "my table")
//
// name: 表名（可能包含 schema）
// 返回：(schema, table) 如果没有 schema，返回 ("", table)
func splitTableName(name string) (string, string) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return "", ""
	}

	// 按 . 分割（注意：这种简单分割不处理引用中的 .）
	parts := strings.Split(trimmed, ".")
	switch len(parts) {
	case 1:
		// 只有表名
		return "", strings.Trim(parts[0], `"`)
	case 2:
		// schema.table
		return strings.Trim(parts[0], `"`), strings.Trim(parts[1], `"`)
	default:
		// 超过两个部分，取最后两个（catalog.schema.table → schema.table）
		schema := strings.Trim(parts[len(parts)-2], `"`)
		table := strings.Trim(parts[len(parts)-1], `"`)
		return schema, table
	}
}

// buildPostgresConnectionString 构建 PostgreSQL 连接字符串
// 从配置映射中提取连接参数并构建 DSN
//
// config: 连接配置映射（包含 host, port, database, username, password, ssl_mode）
// 返回：连接字符串或错误
func buildPostgresConnectionString(config map[string]interface{}) (string, error) {
	// 提取连接参数
	host, ok := config["host"].(string)
	if !ok || host == "" {
		return "", fmt.Errorf("missing or invalid host")
	}

	port, ok := config["port"].(float64) // JSON 数字解析为 float64
	if !ok {
		port = 5432 // 默认端口
	}

	database, ok := config["database"].(string)
	if !ok || database == "" {
		return "", fmt.Errorf("missing or invalid database")
	}

	username, ok := config["username"].(string)
	if !ok || username == "" {
		return "", fmt.Errorf("missing or invalid username")
	}

	password, _ := config["password"].(string) // 密码可以为空

	// SSL 模式（默认 disable）
	sslMode := "disable"
	if ssl, ok := config["ssl_mode"].(string); ok && ssl != "" {
		sslMode = ssl
	}

	return fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		host, int(port), username, password, database, sslMode), nil
}

// isErrorAlreadyExists 检查错误是否为"已存在"类型
// 用于判断主键约束或索引是否已存在
//
// err: 错误对象
// 返回：true 表示资源已存在
func isErrorAlreadyExists(err error) bool {
	if err == nil {
		return false
	}
	errMsg := strings.ToLower(err.Error())
	return strings.Contains(errMsg, "already exists") ||
		strings.Contains(errMsg, "duplicate key") ||
		strings.Contains(errMsg, "multiple primary keys")
}

// isErrorDuplicateValue 检查错误是否为"重复值"类型
// 用于判断主键创建失败是否由于数据重复
//
// err: 错误对象
// 返回：true 表示存在重复值
func isErrorDuplicateValue(err error) bool {
	if err == nil {
		return false
	}
	errMsg := strings.ToLower(err.Error())
	return strings.Contains(errMsg, "duplicate") && !strings.Contains(errMsg, "already exists")
}
