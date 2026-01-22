package postprocessor

import (
	"database/sql"

	"github.com/addp/transfer/pkg/pipeline"
)

// PostProcessorConfig 后处理器配置
// 包含数据库连接、表信息、Schema、任务开关等
type PostProcessorConfig struct {
	// 引擎信息
	EngineType string // 引擎类型：postgresql, mysql, doris, clickhouse 等

	// 数据库连接（两种方式二选一）
	DB               *sql.DB                // 复用 Writer 的连接（推荐方式，避免创建新连接）
	ConnectionConfig map[string]interface{} // 或独立创建连接的配置参数

	// 目标表信息
	TableName string           // 完整表名（可能包含 schema.table，如 public.users）
	Schema    *pipeline.Schema // Schema 信息（包含字段定义、主键元数据等）

	// 空间列信息（从 Writer 传递）
	GeometryColumns map[string]GeometryColumnMetadata

	// 任务开关（默认全部启用）
	CreatePrimaryKey   bool // 是否创建主键约束（默认 true）
	CreateSpatialIndex bool // 是否创建空间索引（默认 true）
	UpdateStatistics   bool // 是否更新统计信息（默认 true）

	// 主键配置（可选，覆盖自动检测）
	ForcePrimaryKey []string // 强制指定主键字段（覆盖 Schema 中的主键检测）
	PrimaryKeyName  string   // 主键约束名（可选，如果为空则自动生成）

	// 空间索引配置
	SpatialIndexPrefix string // 索引名前缀（可选，默认为表名）
	IndexType          string // 索引类型（PostgreSQL: GIST/BRIN, MySQL: SPATIAL）
}

// GeometryColumnMetadata 几何列元数据
// 描述一个空间列的属性
type GeometryColumnMetadata struct {
	ColumnName  string // 列名
	SRID        int    // 空间参考系统 ID（如 4326 表示 WGS84）
	SpatialType string // 几何类型：Point, LineString, Polygon, MultiPoint 等
}

// PrimaryKeyMetadata 主键元数据
// 描述表的主键约束
type PrimaryKeyMetadata struct {
	Columns []string // 主键字段列表（支持复合主键，如 ["id"] 或 ["user_id", "order_id"]）
	Name    string   // 约束名（可选，如 "users_pkey"）
}

// SpatialIndexMetadata 空间索引元数据
// 描述一个空间索引的属性
type SpatialIndexMetadata struct {
	ColumnName string // 列名
	IndexName  string // 索引名（可选，如果为空则自动生成）
	SRID       int    // SRID（可选，用于验证）
}
