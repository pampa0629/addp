package plugin

import (
	"context"
	"time"

	"gorm.io/gorm"
)

// ConnectionInfo 连接信息类型（独立定义，避免循环依赖）
type ConnectionInfo map[string]interface{}

// Engine 引擎结构（最小化定义，避免循环依赖）
type Engine struct {
	ID             uint
	EngineType     string
	ConnectionInfo ConnectionInfo
}

// ============ 基础接口 ============

// EnginePlugin 所有引擎的基础接口
// 所有引擎（数据库、对象存储、计算引擎）都必须实现此接口
type EnginePlugin interface {
	// Type 返回引擎类型标识（小写）
	// 例如: "postgresql", "mysql", "doris", "spark", "minio", "s3", "acme_geo_workflow"
	Type() string

	// DisplayName 返回用户友好的显示名称
	// 例如: "PostgreSQL", "MySQL", "Apache Doris", "Python Workflow"
	DisplayName() string

	// EngineOrigin 返回引擎来源。
	// 可选值: "general"（用户熟悉的通用现成技术）, "extension"（按 ADDP 扩展规范实现的引擎或运行时）
	EngineOrigin() string

	// TestConnection 测试引擎连接是否真正可用。
	//
	// 实现要求：
	//   - 必须使用 connInfo 中的凭据建立连接并执行一个需要认证的操作（如 SELECT 1、listDatabases、ListBuckets 等）
	//   - 仅检查网络连通性（如 Ping、VerifyConnectivity）是不够的，无法验证账号密码是否正确
	//   - 关系型数据库：至少执行一次 SELECT 查询（如 SELECT 1 或 SELECT version()）
	//   - NoSQL 数据库：至少执行一次需要权限的命令（如 ListDatabases、SHOW DATABASES）
	//   - 对象存储：至少执行一次需要认证的 API 调用（如 ListBuckets）
	TestConnection(ctx context.Context, connInfo ConnectionInfo) error

	// ValidateConnectionInfo 验证连接信息的完整性和有效性
	// 在创建/更新引擎前调用，进行字段检查
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

	// Capabilities 返回结构化引擎能力声明
	Capabilities() EngineCapabilities
}

// DSNProvider 是需要底层 driver DSN 的引擎可选实现。
// connection_info 仍是统一事实源；DSN 不进入 System 持久化，也不作为能力判断依据。
type DSNProvider interface {
	EnginePlugin
	BuildDSN(connInfo ConnectionInfo) (string, error)
}

// ============ 可选 Provider 与专项接口 ============

// ConnectionPoolPlugin SQL数据库连接池管理接口
// 关系型数据库应该实现此接口以提供连接池能力
type ConnectionPoolPlugin interface {
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

// ============ 数据结构定义 ============

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

// DynamicCollectionFacts describes engine-native facts for a schema-flexible collection.
type DynamicCollectionFacts struct {
	DocumentCount int64
	SizeBytes     int64
	IndexCount    int
	AvgRecordSize int64
	Indexes       []IndexFacts
}

// IndexFacts describes engine-native index facts.
type IndexFacts struct {
	Name      string
	Fields    []string
	IsUnique  bool
	IndexType string
}

// QueryResult 通用查询结果（SQL/MQL/Cypher 统一格式）
type QueryResult struct {
	Columns []string                 // 有序列名列表
	Rows    []map[string]interface{} // 每行：列名 → 值
}

// ============ 图数据库插件 ============

// GraphNode is a graph result node returned by graph sample/query providers.
type GraphNode struct {
	ElementId  string                 `json:"element_id"`
	Labels     []string               `json:"labels"`
	Properties map[string]interface{} `json:"properties"`
}

// GraphRelationship is a graph result relationship returned by graph sample/query providers.
type GraphRelationship struct {
	ElementId   string                 `json:"element_id"`
	Type        string                 `json:"type"`
	StartNodeId string                 `json:"start_node_id"`
	EndNodeId   string                 `json:"end_node_id"`
	Properties  map[string]interface{} `json:"properties"`
}

// GraphData is graph-shaped runtime data made of nodes and relationships.
type GraphData struct {
	Nodes         []GraphNode         `json:"nodes"`
	Relationships []GraphRelationship `json:"relationships"`
}

// GraphQueryResult 图查询结果（同时包含表格数据和图数据）
type GraphQueryResult struct {
	QueryResult            // Cypher 的表格结果/统计结果
	GraphData   *GraphData `json:"graph_data,omitempty"`
}
