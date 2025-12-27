package client

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	pgvector "github.com/pgvector/pgvector-go"

	"github.com/addp/labs/vector/internal/models"
)

// PgVectorClient PostgreSQL + pgvector 客户端
type PgVectorClient struct {
	pool           *pgxpool.Pool
	schema         string
	table          string
	logger         *slog.Logger
	dimensionSet   bool // 标记是否已设置维度
	currentDim     int  // 当前向量维度
}

// PgVectorConfig 配置
type PgVectorConfig struct {
	DSN    string
	Schema string
	Table  string
}

// NewPgVectorClient 创建客户端
func NewPgVectorClient(ctx context.Context, cfg PgVectorConfig, logger *slog.Logger) (*PgVectorClient, error) {
	logger.Info("[Database] 连接数据库...",
		"dsn", maskDSN(cfg.DSN),
		"schema", cfg.Schema,
		"table", cfg.Table)

	poolConfig, err := pgxpool.ParseConfig(cfg.DSN)
	if err != nil {
		return nil, fmt.Errorf("解析数据库配置失败: %w", err)
	}

	// 连接池配置
	poolConfig.MaxConns = 10
	poolConfig.MinConns = 2
	poolConfig.MaxConnLifetime = 1 * time.Hour
	poolConfig.MaxConnIdleTime = 30 * time.Minute
	poolConfig.HealthCheckPeriod = 1 * time.Minute

	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, fmt.Errorf("创建连接池失败: %w", err)
	}

	// 测试连接
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("数据库连接测试失败: %w", err)
	}

	client := &PgVectorClient{
		pool:   pool,
		schema: cfg.Schema,
		table:  cfg.Table,
		logger: logger,
	}

	logger.Info("[Database] 连接成功", "pool_size", pool.Stat().TotalConns())

	// 检查是否已设置维度
	if err := client.checkDimension(ctx); err != nil {
		logger.Warn("[Database] 维度检查失败，可能需要初始化", "error", err)
	}

	return client, nil
}

// Close 关闭连接池
func (c *PgVectorClient) Close() {
	c.logger.Info("[Database] 关闭连接池")
	c.pool.Close()
}

// checkDimension 检查当前数据库中的向量维度
func (c *PgVectorClient) checkDimension(ctx context.Context) error {
	// 查询表中的任意一条记录获取维度
	tableIdent := pgx.Identifier{c.schema, c.table}.Sanitize()
	query := fmt.Sprintf("SELECT dimension FROM %s LIMIT 1", tableIdent)

	var dimension int
	err := c.pool.QueryRow(ctx, query).Scan(&dimension)
	if err != nil {
		if err == pgx.ErrNoRows {
			return fmt.Errorf("表为空，尚未插入数据")
		}
		return err
	}

	c.currentDim = dimension
	c.dimensionSet = dimension > 0
	return nil
}

// SetDimension 设置向量维度并创建索引
func (c *PgVectorClient) SetDimension(ctx context.Context, dimension int) error {
	if c.dimensionSet && c.currentDim == dimension {
		c.logger.Info("[Database] 维度已设置", "dimension", dimension)
		return nil
	}

	c.logger.Info("[Database] 检测到首次插入，设置维度", "dimension", dimension)

	tableIdent := pgx.Identifier{c.schema, c.table}.Sanitize()

	// 1. 更新表结构 (ALTER TABLE)
	alterSQL := fmt.Sprintf(`
		ALTER TABLE %s
		ALTER COLUMN embedding TYPE vector(%d)
	`, tableIdent, dimension)

	c.logger.Info("[Database] 更新表结构...")
	if _, err := c.pool.Exec(ctx, alterSQL); err != nil {
		return fmt.Errorf("更新表结构失败: %w", err)
	}

	// 2. 创建 HNSW 索引
	indexName := fmt.Sprintf("%s_hnsw_idx", c.table)
	createIndexSQL := fmt.Sprintf(`
		CREATE INDEX IF NOT EXISTS %s
		ON %s
		USING hnsw (embedding vector_cosine_ops)
		WITH (m = 16, ef_construction = 64)
	`, indexName, tableIdent)

	c.logger.Info("[Database] 创建 HNSW 索引...")
	if _, err := c.pool.Exec(ctx, createIndexSQL); err != nil {
		return fmt.Errorf("创建索引失败: %w", err)
	}

	c.dimensionSet = true
	c.currentDim = dimension
	c.logger.Info("[Database] ✅ 维度设置完成，索引创建成功", "dimension", dimension)

	return nil
}

// InsertVector 插入或更新向量
func (c *PgVectorClient) InsertVector(ctx context.Context, record models.VectorRecord) error {
	c.logger.Info("[Database] 插入向量",
		"file_path", record.FilePath,
		"dimension", len(record.Embedding),
		"modality", record.Modality)

	// 首次插入时设置维度
	if !c.dimensionSet {
		if err := c.SetDimension(ctx, len(record.Embedding)); err != nil {
			return fmt.Errorf("设置维度失败: %w", err)
		}
	}

	// 生成 ID
	if record.ID == "" {
		record.ID = uuid.NewString()
	}

	now := time.Now()
	record.CreatedAt = now
	record.UpdatedAt = now
	record.Dimension = len(record.Embedding)

	// 序列化 metadata
	metadataJSON, err := json.Marshal(record.Metadata)
	if err != nil {
		return fmt.Errorf("序列化 metadata 失败: %w", err)
	}

	// 转换为 pgvector 格式
	pgVector := pgvector.NewVector(record.Embedding)

	tableIdent := pgx.Identifier{c.schema, c.table}.Sanitize()
	sql := fmt.Sprintf(`
		INSERT INTO %s (id, file_path, file_name, file_size, modality, model, embedding, dimension, metadata, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		ON CONFLICT (file_path, modality, model) DO UPDATE
		SET embedding = EXCLUDED.embedding,
		    file_size = EXCLUDED.file_size,
		    dimension = EXCLUDED.dimension,
		    metadata = EXCLUDED.metadata,
		    updated_at = EXCLUDED.updated_at
		RETURNING id, created_at, updated_at
	`, tableIdent)

	row := c.pool.QueryRow(ctx, sql,
		record.ID,
		record.FilePath,
		record.FileName,
		record.FileSize,
		record.Modality,
		record.Model,
		pgVector,
		record.Dimension,
		string(metadataJSON),
		record.CreatedAt,
		record.UpdatedAt,
	)

	var returnedID string
	var createdAt, updatedAt time.Time
	if err := row.Scan(&returnedID, &createdAt, &updatedAt); err != nil {
		return fmt.Errorf("插入向量失败: %w", err)
	}

	isNew := createdAt.Equal(updatedAt)
	c.logger.Info("[Database] ✅ 向量插入成功",
		"id", returnedID,
		"created_at", createdAt.Format(time.RFC3339),
		"is_new", isNew)

	return nil
}

// SearchSimilar 相似度检索
func (c *PgVectorClient) SearchSimilar(
	ctx context.Context,
	queryVector []float32,
	topK int,
	modalityFilter string,
) ([]models.SearchResult, error) {
	c.logger.Info("[Database] 执行相似度检索",
		"query_dimension", len(queryVector),
		"top_k", topK,
		"modality_filter", modalityFilter)

	if topK <= 0 {
		topK = 10
	}

	pgVector := pgvector.NewVector(queryVector)
	tableIdent := pgx.Identifier{c.schema, c.table}.Sanitize()

	// 构建查询（支持可选的 modality 过滤）
	var sql string
	var args []interface{}

	if modalityFilter != "" {
		sql = fmt.Sprintf(`
			SELECT
				id, file_path, file_name, file_size, modality, model,
				dimension, metadata, created_at, updated_at,
				embedding <=> $1 AS distance
			FROM %s
			WHERE modality = $2
			ORDER BY embedding <=> $1
			LIMIT $3
		`, tableIdent)
		args = []interface{}{pgVector, modalityFilter, topK}
	} else {
		sql = fmt.Sprintf(`
			SELECT
				id, file_path, file_name, file_size, modality, model,
				dimension, metadata, created_at, updated_at,
				embedding <=> $1 AS distance
			FROM %s
			ORDER BY embedding <=> $1
			LIMIT $2
		`, tableIdent)
		args = []interface{}{pgVector, topK}
	}

	rows, err := c.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("检索查询失败: %w", err)
	}
	defer rows.Close()

	var results []models.SearchResult
	for rows.Next() {
		var (
			id, filePath, fileName, modality, model string
			fileSize, dimension                     int64
			metadataJSON                            []byte
			createdAt, updatedAt                    time.Time
			distance                                float64
		)

		if err := rows.Scan(&id, &filePath, &fileName, &fileSize, &modality, &model,
			&dimension, &metadataJSON, &createdAt, &updatedAt, &distance); err != nil {
			return nil, fmt.Errorf("扫描结果失败: %w", err)
		}

		var metadata map[string]interface{}
		if len(metadataJSON) > 0 {
			_ = json.Unmarshal(metadataJSON, &metadata)
		}

		// 余弦相似度 = 1 - 余弦距离
		similarity := 1.0 - distance

		results = append(results, models.SearchResult{
			VectorRecord: models.VectorRecord{
				ID:        id,
				FilePath:  filePath,
				FileName:  fileName,
				FileSize:  fileSize,
				Modality:  modality,
				Model:     model,
				Dimension: int(dimension),
				Metadata:  metadata,
				CreatedAt: createdAt,
				UpdatedAt: updatedAt,
			},
			Distance:   distance,
			Similarity: similarity,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("迭代结果失败: %w", err)
	}

	c.logger.Info("[Database] 检索完成", "result_count", len(results))

	return results, nil
}

// GetVectorCount 获取向量总数
func (c *PgVectorClient) GetVectorCount(ctx context.Context) (int64, error) {
	tableIdent := pgx.Identifier{c.schema, c.table}.Sanitize()
	sql := fmt.Sprintf("SELECT COUNT(*) FROM %s", tableIdent)

	var count int64
	err := c.pool.QueryRow(ctx, sql).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("查询向量数量失败: %w", err)
	}

	return count, nil
}

// GetVectorsByModality 按模态类型统计
func (c *PgVectorClient) GetVectorsByModality(ctx context.Context) (map[string]int64, error) {
	tableIdent := pgx.Identifier{c.schema, c.table}.Sanitize()
	sql := fmt.Sprintf("SELECT modality, COUNT(*) FROM %s GROUP BY modality", tableIdent)

	rows, err := c.pool.Query(ctx, sql)
	if err != nil {
		return nil, fmt.Errorf("查询失败: %w", err)
	}
	defer rows.Close()

	stats := make(map[string]int64)
	for rows.Next() {
		var modality string
		var count int64
		if err := rows.Scan(&modality, &count); err != nil {
			return nil, err
		}
		stats[modality] = count
	}

	return stats, nil
}

// maskDSN 隐藏 DSN 中的密码
func maskDSN(dsn string) string {
	// 简单实现：隐藏密码部分
	// 实际可能需要更复杂的解析
	return "***masked***"
}
