package plugin

import (
	"context"
	"time"

	"gorm.io/gorm"
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

// PoolConfig 连接池配置
type PoolConfig struct {
	MaxOpenConns    int           // 最大打开连接数，默认10
	MaxIdleConns    int           // 最大空闲连接数，默认5
	ConnMaxLifetime time.Duration // 连接最大生命周期，默认1小时
}

// DefaultPoolConfig 返回默认连接池配置
func DefaultPoolConfig() *PoolConfig {
	return &PoolConfig{
		MaxOpenConns:    10,
		MaxIdleConns:    5,
		ConnMaxLifetime: time.Hour,
	}
}

// ConnectionPoolPlugin SQL数据库插件，支持连接池管理
// 关系型数据库应该实现此接口以提供连接池能力
type ConnectionPoolPlugin interface {
	DatabasePlugin

	// CreateConnectionPool 创建GORM连接池
	// 参数:
	//   - connInfo: 连接信息
	//   - poolConfig: 连接池配置
	// 返回: GORM数据库实例，已配置连接池参数
	CreateConnectionPool(connInfo ConnectionInfo, poolConfig *PoolConfig) (*gorm.DB, error)

	// GetDialect 获取数据库方言（用于GORM）
	// 返回值: "postgres", "mysql", "sqlite" 等
	GetDialect() string
}

// SchemaInfo Schema/Database 信息
type SchemaInfo struct {
	Name       string // Schema 或 Database 名称
	TableCount int    // 包含的表数量
}

// TableInfo 表信息
type TableInfo struct {
	Schema    string // 所属 Schema
	TableName string // 表名
	RowCount  int64  // 行数（估算值）
	SizeBytes int64  // 表大小（字节）
}

// ColumnInfo 列信息
type ColumnInfo struct {
	ColumnName   string // 列名
	DataType     string // 原生数据类型（如 varchar, int4）
	StdType      string // 标准化类型（如 string, integer）
	IsNullable   bool   // 是否可为空
	IsPrimaryKey bool   // 是否主键
	Comment      string // 列注释
}

// MetadataPlugin 元数据查询插件
// 支持查询数据库的元数据信息（Schema、表、列）
type MetadataPlugin interface {
	ConnectionPoolPlugin

	// ListSchemas 列出所有Schema（PostgreSQL）或Database（MySQL）
	// 参数:
	//   - ctx: 上下文
	//   - db: GORM数据库实例
	// 返回: Schema列表
	ListSchemas(ctx context.Context, db *gorm.DB) ([]SchemaInfo, error)

	// ListTables 列出指定Schema下的所有表
	// 参数:
	//   - ctx: 上下文
	//   - db: GORM数据库实例
	//   - schema: Schema名称
	// 返回: 表列表
	ListTables(ctx context.Context, db *gorm.DB, schema string) ([]TableInfo, error)

	// ListColumns 列出指定表的所有列
	// 参数:
	//   - ctx: 上下文
	//   - db: GORM数据库实例
	//   - schema: Schema名称
	//   - table: 表名
	// 返回: 列列表
	ListColumns(ctx context.Context, db *gorm.DB, schema, table string) ([]ColumnInfo, error)

	// GetTableRowCount 获取表的行数（精确或估算）
	// 参数:
	//   - ctx: 上下文
	//   - db: GORM数据库实例
	//   - schema: Schema名称
	//   - table: 表名
	// 返回: 行数
	GetTableRowCount(ctx context.Context, db *gorm.DB, schema, table string) (int64, error)
}
