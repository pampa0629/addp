package postprocessor

import "context"

// PostProcessor 数据导入后处理器接口
// 负责在数据写入完成后执行优化任务（主键、索引、统计信息等）
//
// 设计理念：
// - 与 Writer 解耦：PostProcessor 专注于数据库优化，Writer 专注于数据写入
// - 按引擎类型路由：不同数据库（PostgreSQL/MySQL/Doris）使用不同实现
// - 可插拔架构：通过 Registry 注册和查找实现
type PostProcessor interface {
	// Initialize 初始化后处理器
	// 传递数据库连接信息、表名、Schema 等上下文
	// ctx: 上下文，用于超时和取消控制
	// config: 后处理器配置（包含 DB 连接、Schema、任务开关等）
	Initialize(ctx context.Context, config PostProcessorConfig) error

	// CreatePrimaryKey 创建主键约束
	// ctx: 上下文
	// pk: 主键元数据（列名、约束名）
	// 如果 pk 为 nil 或列为空，则跳过
	// 主键创建失败返回错误（关键约束，应阻断任务）
	CreatePrimaryKey(ctx context.Context, pk *PrimaryKeyMetadata) error

	// CreateSpatialIndexes 创建空间索引
	// ctx: 上下文
	// indexes: 空间索引元数据列表
	// 为所有空间列创建 GIST/SPATIAL 索引（根据引擎类型）
	// 索引创建失败仅记录警告（性能优化，不应阻断任务）
	CreateSpatialIndexes(ctx context.Context, indexes []SpatialIndexMetadata) error

	// UpdateStatistics 更新表统计信息
	// ctx: 上下文
	// 执行数据库特定的统计更新命令：
	// - PostgreSQL: ANALYZE
	// - MySQL: ANALYZE TABLE
	// - Doris: 自动更新（无需手动触发）
	// 统计更新失败仅记录警告
	UpdateStatistics(ctx context.Context) error

	// Execute 执行所有后处理任务（便捷方法）
	// ctx: 上下文
	// 按顺序执行：主键创建 → 空间索引创建 → 统计更新
	// 错误处理策略：
	// - 主键创建失败：返回错误（阻断任务）
	// - 索引/统计失败：记录警告但继续（不阻断任务）
	Execute(ctx context.Context) error

	// Close 关闭资源
	// 如果 PostProcessor 拥有数据库连接（非复用 Writer 连接），则关闭连接
	Close() error
}

// PostProcessorFactory 后处理器工厂函数
// 用于创建 PostProcessor 实例
// config: 后处理器配置
// 返回：PostProcessor 实例或错误
type PostProcessorFactory func(config PostProcessorConfig) (PostProcessor, error)
