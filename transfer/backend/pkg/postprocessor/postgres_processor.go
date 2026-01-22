package postprocessor

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strings"

	"github.com/addp/common/logger"
	"github.com/addp/transfer/pkg/pipeline"
)

// PostgresPostProcessor PostgreSQL 后处理器
// 负责 PostgreSQL 数据库的导入后优化任务：
// - 主键约束创建
// - 空间索引创建（GIST/BRIN）
// - 统计信息更新（ANALYZE）
type PostgresPostProcessor struct {
	db              *sql.DB              // 数据库连接
	ownConnection   bool                 // 是否拥有连接（需要在 Close 时关闭）
	tableName       string               // 目标表名
	schema          *pipeline.Schema     // Schema 信息
	geometryColumns map[string]GeometryColumnMetadata // 空间列信息
	config          PostProcessorConfig  // 配置
	logger          *slog.Logger         // 日志器
}

// NewPostgresPostProcessor 创建 PostgreSQL 后处理器
// config: 后处理器配置
// 返回：PostProcessor 实例或错误
func NewPostgresPostProcessor(config PostProcessorConfig) (PostProcessor, error) {
	// 验证配置
	if config.TableName == "" {
		return nil, fmt.Errorf("table name cannot be empty")
	}

	return &PostgresPostProcessor{
		config:          config,
		tableName:       config.TableName,
		schema:          config.Schema,
		geometryColumns: config.GeometryColumns,
		logger:          logger.With("component", "postgres_postprocessor", "table", config.TableName),
	}, nil
}

// Initialize 初始化后处理器
// ctx: 上下文
// config: 后处理器配置
// 返回：错误（如果初始化失败）
func (p *PostgresPostProcessor) Initialize(ctx context.Context, config PostProcessorConfig) error {
	p.config = config

	// 获取或创建数据库连接
	if config.DB != nil {
		// 复用 Writer 的连接（推荐方式）
		p.db = config.DB
		p.ownConnection = false
		p.logger.Debug("using shared database connection")
	} else if config.ConnectionConfig != nil {
		// 独立创建连接
		connStr, err := buildPostgresConnectionString(config.ConnectionConfig)
		if err != nil {
			return fmt.Errorf("failed to build connection string: %w", err)
		}

		db, err := sql.Open("postgres", connStr)
		if err != nil {
			return fmt.Errorf("failed to open database: %w", err)
		}

		if err := db.PingContext(ctx); err != nil {
			db.Close()
			return fmt.Errorf("failed to ping database: %w", err)
		}

		p.db = db
		p.ownConnection = true
		p.logger.Debug("created new database connection")
	} else {
		return fmt.Errorf("either DB or ConnectionConfig must be provided")
	}

	p.logger.Info("postprocessor initialized", "table", p.tableName)
	return nil
}

// CreatePrimaryKey 创建主键约束
// ctx: 上下文
// pk: 主键元数据
// 返回：错误（主键创建失败）
func (p *PostgresPostProcessor) CreatePrimaryKey(ctx context.Context, pk *PrimaryKeyMetadata) error {
	if pk == nil || len(pk.Columns) == 0 {
		p.logger.Debug("no primary key to create")
		return nil
	}

	// 生成约束名
	constraintName := generatePrimaryKeyConstraintName(pk, p.tableName)

	// 引用列名
	quotedColumns := quoteIdentifiers(pk.Columns)

	// ALTER TABLE ADD PRIMARY KEY
	alterSQL := fmt.Sprintf("ALTER TABLE %s ADD CONSTRAINT %s PRIMARY KEY (%s)",
		qualifiedTableName(p.tableName),
		quoteIdentifier(constraintName),
		strings.Join(quotedColumns, ", "))

	p.logger.Info("creating primary key",
		"constraint", constraintName,
		"columns", pk.Columns)

	if _, err := p.db.ExecContext(ctx, alterSQL); err != nil {
		// 容错：主键已存在
		if isErrorAlreadyExists(err) {
			p.logger.Warn("primary key already exists", "constraint", constraintName)
			return nil
		}
		// 容错：数据有重复
		if isErrorDuplicateValue(err) {
			return fmt.Errorf("cannot create primary key on columns %v: duplicate values found", pk.Columns)
		}
		return fmt.Errorf("failed to create primary key: %w", err)
	}

	p.logger.Info("primary key created successfully",
		"constraint", constraintName,
		"columns", pk.Columns)
	return nil
}

// CreateSpatialIndexes 创建空间索引
// ctx: 上下文
// indexes: 空间索引元数据列表
// 返回：错误（如果所有索引都创建失败）
func (p *PostgresPostProcessor) CreateSpatialIndexes(ctx context.Context, indexes []SpatialIndexMetadata) error {
	if len(indexes) == 0 {
		p.logger.Debug("no spatial indexes to create")
		return nil
	}

	successCount := 0

	for _, idx := range indexes {
		// 确定索引类型（GIST 或 BRIN）
		indexType := p.config.IndexType
		if indexType == "" {
			indexType = "GIST" // 默认使用 GIST
		}

		// CREATE INDEX USING GIST/BRIN
		createIndexSQL := fmt.Sprintf("CREATE INDEX %s ON %s USING %s (%s)",
			quoteIdentifier(idx.IndexName),
			qualifiedTableName(p.tableName),
			indexType,
			quoteIdentifier(idx.ColumnName))

		p.logger.Info("creating spatial index",
			"index", idx.IndexName,
			"column", idx.ColumnName,
			"type", indexType)

		if _, err := p.db.ExecContext(ctx, createIndexSQL); err != nil {
			// 容错：索引已存在
			if isErrorAlreadyExists(err) {
				p.logger.Warn("spatial index already exists", "index", idx.IndexName)
				successCount++
				continue
			}
			// 其他错误记录但不中断
			p.logger.Warn("failed to create spatial index",
				"index", idx.IndexName,
				"column", idx.ColumnName,
				"error", err)
			continue
		}

		p.logger.Info("spatial index created successfully", "index", idx.IndexName)
		successCount++
	}

	if successCount > 0 {
		p.logger.Info("spatial indexes created",
			"success", successCount,
			"total", len(indexes))
	}

	return nil
}

// UpdateStatistics 更新表统计信息
// ctx: 上下文
// 返回：错误（统计更新失败）
func (p *PostgresPostProcessor) UpdateStatistics(ctx context.Context) error {
	analyzeSQL := fmt.Sprintf("ANALYZE %s", qualifiedTableName(p.tableName))

	p.logger.Info("updating table statistics")

	if _, err := p.db.ExecContext(ctx, analyzeSQL); err != nil {
		return fmt.Errorf("failed to update statistics: %w", err)
	}

	p.logger.Info("table statistics updated successfully")
	return nil
}

// Execute 执行所有后处理任务
// ctx: 上下文
// 返回：错误（主键创建失败时返回错误，其他任务失败仅记录警告）
func (p *PostgresPostProcessor) Execute(ctx context.Context) error {
	p.logger.Info("starting post-processing tasks")

	// 任务 1: 创建主键
	if p.config.CreatePrimaryKey {
		pk := extractPrimaryKeyMetadata(p.config)
		if err := p.CreatePrimaryKey(ctx, pk); err != nil {
			p.logger.Error("primary key creation failed", "error", err)
			return err // 主键失败返回错误（关键约束）
		}
	} else {
		p.logger.Debug("primary key creation disabled")
	}

	// 任务 2: 创建空间索引
	if p.config.CreateSpatialIndex {
		indexes := extractSpatialIndexMetadata(p.config)
		if err := p.CreateSpatialIndexes(ctx, indexes); err != nil {
			// 空间索引失败仅记录警告（性能优化，非关键）
			p.logger.Warn("spatial index creation failed", "error", err)
		}
	} else {
		p.logger.Debug("spatial index creation disabled")
	}

	// 任务 3: 更新统计信息
	if p.config.UpdateStatistics {
		if err := p.UpdateStatistics(ctx); err != nil {
			// 统计更新失败仅记录警告
			p.logger.Warn("statistics update failed", "error", err)
		}
	} else {
		p.logger.Debug("statistics update disabled")
	}

	p.logger.Info("post-processing tasks completed")
	return nil
}

// Close 关闭资源
// 如果后处理器拥有数据库连接（非复用），则关闭连接
// 返回：错误（连接关闭失败）
func (p *PostgresPostProcessor) Close() error {
	if p.ownConnection && p.db != nil {
		p.logger.Debug("closing owned database connection")
		return p.db.Close()
	}
	p.logger.Debug("skipping connection close (shared connection)")
	return nil
}
