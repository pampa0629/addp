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

// RelationalDBPlugin 关系型数据库插件
// 包含连接池管理和元数据查询能力
type RelationalDBPlugin interface {
	StoragePlugin
	ConnectionPoolPlugin

	// ===== 元数据查询方法（直接定义，不需要单独的 MetadataPlugin 接口）=====

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

	// IsSystemSchema 判断是否为系统 Schema
	// 不同数据库的系统 Schema 不同，由各插件自己实现
	// 例如 PostgreSQL: pg_catalog, information_schema
	//      MySQL: information_schema, mysql, performance_schema
	IsSystemSchema(schemaName string) bool

	// SchemaNodeType 返回第一层节点的类型名
	// PostgreSQL 返回 "schema"，MySQL/Doris/ClickHouse 返回 "database"
	SchemaNodeType() string
}

// ObjectStoragePlugin 对象存储插件
// 继承 FileSystemPlugin，同时提供对象存储特有的操作能力
type ObjectStoragePlugin interface {
	FileSystemPlugin

	// ===== 对象存储操作方法（直接定义，不需要单独的 ObjectMetadataPlugin 接口）=====

	// ListBuckets 列出所有 Bucket
	// 参数:
	//   - ctx: 上下文
	//   - connInfo: 连接信息
	// 返回: Bucket列表
	ListBuckets(ctx context.Context, connInfo ConnectionInfo) ([]BucketInfo, error)

	// ListObjects 列出对象（支持前缀过滤和递归）
	// 参数:
	//   - ctx: 上下文
	//   - connInfo: 连接信息
	//   - bucket: 存储桶名称
	//   - prefix: 前缀过滤（可选）
	//   - recursive: 是否递归列出子目录
	// 返回: 对象列表
	ListObjects(ctx context.Context, connInfo ConnectionInfo, bucket, prefix string, recursive bool) ([]ObjectInfo, error)

	// GetObjectMetadata 获取单个对象的元数据
	// 参数:
	//   - ctx: 上下文
	//   - connInfo: 连接信息
	//   - bucket: 存储桶名称
	//   - key: 对象键
	// 返回: 对象信息
	GetObjectMetadata(ctx context.Context, connInfo ConnectionInfo, bucket, key string) (*ObjectInfo, error)

	// InferContentType 根据对象键推断 MIME 类型
	// 参数:
	//   - objectKey: 对象键（文件路径）
	// 返回: MIME 类型，例如: "application/geo+json", "image/png"
	InferContentType(objectKey string) string

	// ===== 对象存储特有属性 =====

	// DefaultBucket 返回默认存储桶名称（如果适用）
	DefaultBucket() string

	// SupportsSSL 是否支持 SSL 连接
	SupportsSSL() bool
}

// ============ NoSQL 数据库插件 ============

// NoSQLPlugin NoSQL 数据库插件通用接口（公共基础能力）
// 仅包含所有 NoSQL 引擎共享的能力；文档/图模型专属能力由子接口承载
type NoSQLPlugin interface {
	StoragePlugin

	// ListDatabases 列出所有 Database
	ListDatabases(ctx context.Context, connInfo ConnectionInfo) ([]DatabaseInfo, error)

	// IsSystemDatabase 判断是否为系统 Database
	IsSystemDatabase(databaseName string) bool

	// CreateClient 创建数据库客户端
	CreateClient(ctx context.Context, connInfo ConnectionInfo) (interface{}, error)

	// CloseClient 关闭数据库客户端
	CloseClient(ctx context.Context, client interface{}) error
}

// DocumentDBPlugin 文档型数据库插件接口（MongoDB 等）
// 在 NoSQLPlugin 之上增加文档集合查询能力
type DocumentDBPlugin interface {
	NoSQLPlugin

	// ListCollections 列出指定 Database 下的所有 Collection
	ListCollections(ctx context.Context, connInfo ConnectionInfo, database string) ([]CollectionInfo, error)

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

// ObjectInfo 对象信息
type ObjectInfo struct {
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

// GraphDBPlugin 图数据库插件接口
// 用于支持属性图数据库（Neo4j 及未来的 Amazon Neptune、ArangoDB 等）
// 在 NoSQLPlugin 之上增加图结构查询能力（关系类型、图 Schema）
type GraphDBPlugin interface {
	NoSQLPlugin

	// ListNodeLabels 列出图数据库中所有节点标签及统计信息
	// 等价于 NoSQL 中的 ListCollections，但返回图数据库特有的 NodeLabelInfo 类型
	ListNodeLabels(ctx context.Context, connInfo ConnectionInfo, database string) ([]NodeLabelInfo, error)

	// ListRelationshipTypes 列出图数据库中所有关系类型及连接统计
	// 图数据库特有：返回每种关系类型的起始/终止节点标签及数量
	ListRelationshipTypes(ctx context.Context, connInfo ConnectionInfo, database string) ([]RelationshipTypeInfo, error)

	// GetGraphSchema 获取图数据库完整 Schema（节点标签 + 关系类型 + 连接关系）
	// 供图可视化和元数据展示使用
	GetGraphSchema(ctx context.Context, connInfo ConnectionInfo, database string) (*GraphSchema, error)
}

// ============ 术语 i18n 接口 ============

// TermI18nProvider 术语 i18n 提供者（可选接口）
//
// 引擎插件可选实现此接口，为引擎特有术语提供自定义 i18n key 映射。
// 未实现时，调用方使用默认规则：返回 "engine.term." + term。
//
// 绝大多数插件无需实现此接口，默认规则已足够。
// 仅当引擎需要将某个通用术语映射到不同 i18n key 时才实现。
type TermI18nProvider interface {
	// TermI18nKey 将通用英文术语转换为 i18n key
	// term: 通用英文术语，如 "schema", "database", "label", "relationship", "table", "view"
	// 返回: i18n key，如 "engine.term.schema"
	TermI18nKey(term string) string
}

// GetTermI18nKey 获取术语的 i18n key（统一入口）
//
// 优先使用插件自定义映射（若插件实现了 TermI18nProvider），
// 否则使用默认规则：返回 "engine.term." + term。
func GetTermI18nKey(plug EnginePlugin, term string) string {
	if provider, ok := plug.(TermI18nProvider); ok {
		return provider.TermI18nKey(term)
	}
	return "engine.term." + term
}
