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

// ============ 第一层：EnginePlugin（所有引擎基础接口）============

// EnginePlugin 所有引擎的基础接口
// 所有引擎（数据库、对象存储、计算引擎）都必须实现此接口
type EnginePlugin interface {
	// Type 返回引擎类型标识（小写）
	// 例如: "postgresql", "mysql", "doris", "spark", "minio", "s3", "python_workflow"
	Type() string

	// DisplayName 返回用户友好的显示名称
	// 例如: "PostgreSQL", "MySQL", "Apache Doris", "Python Workflow"
	DisplayName() string

	// EngineCategory 返回引擎分类
	// 可选值: "standard" (标准引擎，如 PostgreSQL/MySQL), "extension" (扩展引擎，如工作流引擎)
	EngineCategory() string

	// TestConnection 测试引擎连接是否真正可用。
	//
	// 实现要求：
	//   - 必须使用 connInfo 中的凭据建立连接并执行一个需要认证的操作（如 SELECT 1、listDatabases、ListBuckets 等）
	//   - 仅检查网络连通性（如 Ping、VerifyConnectivity）是不够的，无法验证账号密码是否正确
	//   - 关系型数据库：至少执行一次 SELECT 查询（如 SELECT 1 或 SELECT version()）
	//   - NoSQL 数据库：至少执行一次需要权限的命令（如 ListDatabases、SHOW DATABASES）
	//   - 对象存储：至少执行一次需要认证的 API 调用（如 ListBuckets）
	TestConnection(ctx context.Context, connInfo ConnectionInfo) error

	// BuildConnectionString 根据连接信息构建连接字符串
	// 返回的格式取决于具体引擎类型
	BuildConnectionString(connInfo ConnectionInfo) (string, error)

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

// StoragePlugin 存储引擎标记接口。
// 具体能力由 Capabilities、CatalogProvider、ItemMetadataProvider、StoreProvider 等表达。
type StoragePlugin interface {
	EnginePlugin
}

// ComputePlugin 计算引擎标记接口。
// 具体能力由 Capabilities、QueryRuntimeProvider、WorkflowRuntimeProvider、ScriptRuntimeProvider 等表达。
type ComputePlugin interface {
	EnginePlugin
}

// ============ 第三层：存储引擎细分 ============

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

// RelationalDBPlugin 关系型数据库插件。
// 真实目录与字段元数据统一由 CatalogProvider / ItemMetadataProvider 暴露；
// 该接口仅保留 SQL/tabular 引擎的连接池和系统 schema 判断能力。
type RelationalDBPlugin interface {
	StoragePlugin
	ConnectionPoolPlugin

	// IsSystemSchema 判断是否为系统 Schema
	// 不同数据库的系统 Schema 不同，由各插件自己实现
	// 例如 PostgreSQL: pg_catalog, information_schema
	//      MySQL: information_schema, mysql, performance_schema
	IsSystemSchema(schemaName string) bool
}

// DocumentDBPlugin 文档型数据库专项元数据增强接口。
// database/collection 列表统一由 CatalogProvider 暴露。
type DocumentDBPlugin interface {
	StoragePlugin

	// GetCollectionStats 获取 Collection 统计信息
	GetCollectionStats(ctx context.Context, connInfo ConnectionInfo, database, collection string) (*CollectionStats, error)
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

// SchemaInfo Schema/Database 信息
type SchemaInfo struct {
	Name       string `gorm:"column:name"`        // Schema 或 Database 名称
	TableCount int    `gorm:"column:table_count"` // 包含的表数量
}

// TableInfo 表信息
type TableInfo struct {
	Schema       string     `gorm:"column:schema"`        // 所属 Schema
	TableName    string     `gorm:"column:table_name"`    // 表名
	Kind         string     `gorm:"column:table_kind"`    // Catalog kind: table/view/materialized_view/external_table
	Comment      string     `gorm:"column:comment"`       // 表注释
	RowCount     int64      `gorm:"column:row_count"`     // 行数（估算值）
	SizeBytes    int64      `gorm:"column:size_bytes"`    // 表大小（字节）
	LastModified *time.Time `gorm:"column:last_modified"` // 表的最后修改时间（用于增量扫描）
}

// ColumnInfo 列信息
type ColumnInfo struct {
	ColumnName   string `gorm:"column:column_name"`    // 列名
	DataType     string `gorm:"column:data_type"`      // 原生数据类型（如 varchar, int4, geometry, geography）
	IsNullable   bool   `gorm:"column:is_nullable"`    // 是否可为空
	IsPrimaryKey bool   `gorm:"column:is_primary_key"` // 是否主键
	Comment      string `gorm:"column:comment"`        // 列注释
}

// DatabaseInfo Database 信息（NoSQL）
type DatabaseInfo struct {
	Name      string // 数据库名称
	SizeBytes int64  // 存储大小（字节）
}

// CollectionInfo Collection 信息（NoSQL）
type CollectionInfo struct {
	Database      string // 所属数据库
	Name          string // 集合名称
	DocumentCount int64  // 文档数量
	SizeBytes     int64  // 存储大小（字节）
}

// CollectionStats Collection 统计信息（NoSQL）
type CollectionStats struct {
	DocumentCount int64       // 文档数量
	SizeBytes     int64       // 存储大小（字节）
	IndexCount    int         // 索引数量
	AvgDocSize    int64       // 平均文档大小（字节）
	Indexes       []IndexInfo // 索引列表
}

// IndexInfo 索引信息（NoSQL）
type IndexInfo struct {
	Name      string   // 索引名称
	Fields    []string // 索引字段
	IsUnique  bool     // 是否唯一索引
	IndexType string   // 索引类型（如 "btree", "hash", "text"）
}

// BucketInfo Bucket 信息
type BucketInfo struct {
	Name         string    // Bucket 名称
	CreationDate time.Time // 创建时间
}

// ObjectStorageEntry 对象存储条目。
type ObjectStorageEntry struct {
	Bucket       string    // 所属 Bucket
	Key          string    // 对象键（完整路径）
	Size         int64     // 对象大小（字节）
	LastModified time.Time // 最后修改时间
	ContentType  string    // MIME 类型
	ETag         string    // ETag（可选）
}

// QueryResult 通用查询结果（SQL/MQL/Cypher 统一格式）
type QueryResult struct {
	Columns []string                 // 有序列名列表
	Rows    []map[string]interface{} // 每行：列名 → 值
}

// ============ 图数据库插件 ============

// NodeLabelInfo 图数据库节点标签信息
type NodeLabelInfo struct {
	Name  string `json:"name"`
	Count int64  `json:"count"`
}

// RelationshipTypeInfo 图数据库关系类型信息
type RelationshipTypeInfo struct {
	Name       string   `json:"name"`
	Count      int64    `json:"count"`
	FromLabels []string `json:"from_labels"`
	ToLabels   []string `json:"to_labels"`
}

// GraphSchema 图数据库 Schema 信息（节点标签 + 关系类型）
type GraphSchema struct {
	NodeLabels    []NodeLabelInfo        `json:"node_labels"`
	Relationships []RelationshipTypeInfo `json:"relationships"`
}

// GraphNode 图节点（用于图可视化）
type GraphNode struct {
	ElementId  string                 `json:"element_id"`
	Labels     []string               `json:"labels"`
	Properties map[string]interface{} `json:"properties"`
}

// GraphRelationship 图关系（用于图可视化）
type GraphRelationship struct {
	ElementId   string                 `json:"element_id"`
	Type        string                 `json:"type"`
	StartNodeId string                 `json:"start_node_id"`
	EndNodeId   string                 `json:"end_node_id"`
	Properties  map[string]interface{} `json:"properties"`
}

// GraphData 图数据（节点 + 关系，用于前端图可视化渲染）
type GraphData struct {
	Nodes         []GraphNode         `json:"nodes"`
	Relationships []GraphRelationship `json:"relationships"`
}

// GraphQueryResult 图查询结果（同时包含表格数据和图数据）
type GraphQueryResult struct {
	QueryResult            // 嵌入表格结果（向后兼容）
	GraphData   *GraphData `json:"graph_data,omitempty"`
}

// GraphDBPlugin 图数据库专项元数据增强接口。
// database/label/relationship 列表统一由 CatalogProvider 暴露。
type GraphDBPlugin interface {
	StoragePlugin

	// GetGraphSchema 获取图数据库完整 Schema（节点标签 + 关系类型 + 连接关系）
	// 供图可视化和元数据展示使用
	GetGraphSchema(ctx context.Context, connInfo ConnectionInfo, database string) (*GraphSchema, error)
}

// ============ 术语 i18n ============

// TermI18nKey 获取术语的 i18n key。
//
// 术语来源于 catalog term / node type / item type，默认规则统一为
// "engine.term." + term。若后续需要引擎级自定义映射，应通过
// EngineCapabilities 的 catalog model 声明序列化表达，而不是新增插件私有接口。
func TermI18nKey(term string) string {
	return "engine.term." + term
}
