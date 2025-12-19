package plugin

import (
	"context"
)

// ConnectionInfo 连接信息类型（独立定义，避免循环依赖）
type ConnectionInfo map[string]interface{}

// Resource 资源结构（最小化定义，避免循环依赖）
type Resource struct {
	ID             uint
	ResourceType   string
	ConnectionInfo ConnectionInfo
}

// DatabasePlugin 数据库插件基础接口
// 所有数据库类型插件必须实现此接口
type DatabasePlugin interface {
	// Type 返回数据库类型标识（小写）
	// 例如: "postgresql", "mysql", "doris", "spark_sql", "minio", "s3"
	Type() string

	// DisplayName 返回用户友好的显示名称
	// 例如: "PostgreSQL", "MySQL", "Apache Doris"
	DisplayName() string

	// ConnectionCategory 返回连接类别
	// 可选值: "relational_db", "object_storage", "compute_engine"
	ConnectionCategory() string

	// TestConnection 测试数据库连接是否有效
	TestConnection(ctx context.Context, connInfo ConnectionInfo) error

	// BuildConnectionString 根据连接信息构建连接字符串
	// 返回的格式取决于具体数据库类型
	BuildConnectionString(connInfo ConnectionInfo) (string, error)

	// ValidateConnectionInfo 验证连接信息的完整性和有效性
	// 在创建/更新资源前调用，进行字段检查
	ValidateConnectionInfo(connInfo ConnectionInfo) error

	// DefaultPort 返回默认端口号
	// 如果不适用，返回 0
	DefaultPort() int

	// RequiredFields 返回必填字段列表
	// 例如: ["host", "port", "user", "database"]
	RequiredFields() []string

	// SensitiveFields 返回敏感字段列表（需要加密/脱敏）
	// 例如: ["password", "access_key", "secret_key", "token"]
	SensitiveFields() []string

	// GenerateCapabilities 生成资源能力描述（JSON 字符串）
	// 返回 Capability 结构的 JSON 表示
	GenerateCapabilities() string
}

// SQLDatabasePlugin SQL 数据库插件扩展接口（可选）
// 关系型数据库可以实现此接口以提供额外的 SQL 特定功能
type SQLDatabasePlugin interface {
	DatabasePlugin

	// SupportsTransactions 是否支持事务
	SupportsTransactions() bool

	// DefaultDialect 返回 SQL 方言
	// 例如: "postgres", "mysql"
	DefaultDialect() string
}

// ObjectStoragePlugin 对象存储插件扩展接口（可选）
// 对象存储类型可以实现此接口
type ObjectStoragePlugin interface {
	DatabasePlugin

	// DefaultBucket 返回默认存储桶名称（如果适用）
	DefaultBucket() string

	// SupportsSSL 是否支持 SSL 连接
	SupportsSSL() bool
}
