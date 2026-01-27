package service

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	"github.com/addp/meta/internal/config"
	"github.com/addp/meta/internal/models"
)

// SpatialMetadataService 负责扫描空间表的几何元数据
// 从 scan_spatial.go 提取，消除循环依赖
type SpatialMetadataService struct {
	config *config.Config
	log    *slog.Logger
}

// NewSpatialMetadataService 创建空间元数据服务
func NewSpatialMetadataService(cfg *config.Config, log *slog.Logger) *SpatialMetadataService {
	return &SpatialMetadataService{
		config: cfg,
		log:    log,
	}
}

// ScanTableSpatialMetadata 扫描空间表的几何元数据
// 在深度扫描表时调用，检测空间列并提取元数据
func (s *SpatialMetadataService) ScanTableSpatialMetadata(
	ctx context.Context,
	db *sql.DB,
	schema, table string,
) (*models.SpatialMetadata, error) {
	// 1. 检测几何列
	geomColumn, err := s.detectGeometryColumn(db, schema, table)
	if err != nil || geomColumn == "" {
		return nil, nil // 不是空间表
	}

	// 2. 查询 SRID
	srid, err := s.querySRID(db, schema, table, geomColumn)
	if err != nil {
		s.log.Warn("Failed to query SRID", "schema", schema, "table", table, "error", err)
		return nil, err
	}

	// 3. 计算 extent (WGS84)
	extent, err := s.calculateExtent(db, schema, table, geomColumn, srid)
	if err != nil {
		s.log.Warn("Failed to calculate extent", "schema", schema, "table", table, "error", err)
		return nil, err
	}

	// 4. 统计几何类型
	geomTypes, err := s.getGeometryTypes(db, schema, table, geomColumn)
	if err != nil {
		s.log.Warn("Failed to get geometry types", "schema", schema, "table", table, "error", err)
		geomTypes = []string{}
	}

	// 5. 检查空间索引
	hasIndex, indexName, err := s.checkSpatialIndex(db, schema, table, geomColumn)
	if err != nil {
		s.log.Warn("Failed to check spatial index", "schema", schema, "table", table, "error", err)
		hasIndex = false
	}

	// 6. 检查是否有 updated_at 字段
	hasUpdatedAt, updatedAtColumn := s.detectUpdatedAtColumn(db, schema, table)

	return &models.SpatialMetadata{
		GeometryColumn:  geomColumn,
		SRID:            srid,
		ExtentSRID:      4326, // extent 总是 WGS84（calculateExtent 内部使用 ST_Transform）
		Extent:          extent,
		GeometryTypes:   geomTypes,
		HasSpatialIndex: hasIndex,
		IndexName:       indexName,
		HasUpdatedAt:    hasUpdatedAt,
		UpdatedAtColumn: updatedAtColumn,
	}, nil
}

// detectGeometryColumn 检测几何列
func (s *SpatialMetadataService) detectGeometryColumn(db *sql.DB, schema, table string) (string, error) {
	// 常见的几何列名
	commonNames := []string{"geom", "geometry", "shape", "the_geom", "geog", "geography", "smgeometry"}

	// 首先尝试从 geometry_columns 系统表查询
	query := `
		SELECT f_geometry_column
		FROM geometry_columns
		WHERE f_table_schema = $1 AND f_table_name = $2
		LIMIT 1
	`

	var geomColumn string
	err := db.QueryRow(query, schema, table).Scan(&geomColumn)
	if err == nil && geomColumn != "" {
		return geomColumn, nil
	}

	// 如果失败，尝试常见名称
	for _, name := range commonNames {
		checkQuery := fmt.Sprintf(`
			SELECT column_name
			FROM information_schema.columns
			WHERE table_schema = $1 AND table_name = $2 AND column_name = $3
			  AND (udt_name LIKE '%%geometry%%' OR udt_name LIKE '%%geography%%')
		`)

		var col string
		err := db.QueryRow(checkQuery, schema, table, name).Scan(&col)
		if err == nil {
			return col, nil
		}
	}

	return "", nil // 未找到几何列
}

// querySRID 查询空间参考系统 ID
func (s *SpatialMetadataService) querySRID(db *sql.DB, schema, table, geomColumn string) (int, error) {
	query := `
		SELECT Find_SRID($1, $2, $3)
	`

	var srid int
	err := db.QueryRow(query, schema, table, geomColumn).Scan(&srid)
	if err != nil {
		return 0, fmt.Errorf("failed to query SRID: %w", err)
	}

	return srid, nil
}

// calculateExtent 计算空间范围（WGS84）
// 采用三级降级策略：
// 1. 优先使用 ST_EstimatedExtent（从空间索引统计信息获取，毫秒级）
// 2. 小表使用完整计算（准确，<100万行）
// 3. 大表无索引时使用空间网格采样（保证地理分布均匀）
func (s *SpatialMetadataService) calculateExtent(db *sql.DB, schema, table, geomColumn string, srid int) ([]float64, error) {
	// 1. 查询表行数
	rowCount, err := s.getTableRowCount(db, schema, table)
	if err != nil {
		s.log.Warn("Failed to get table row count, assuming small table", "error", err)
		rowCount = 0 // 降级到小表处理
	}

	// 2. 获取配置参数
	largeTableThreshold := 1000000 // 默认阈值 100 万行
	if s.config != nil {
		largeTableThreshold = s.config.LargeTableThreshold
	}

	// 3. 检查是否有空间索引
	hasIndex, indexName, err := s.checkSpatialIndex(db, schema, table, geomColumn)
	if err != nil {
		s.log.Warn("Failed to check spatial index, assuming no index", "error", err)
		hasIndex = false
	}

	// ========== 策略1: 优先使用 ST_EstimatedExtent（如果有空间索引）==========
	if hasIndex {
		extent, err := s.calculateExtentFromEstimate(db, schema, table, geomColumn, srid)
		if err == nil && extent != nil {
			s.log.Info("Using ST_EstimatedExtent for extent calculation",
				"schema", schema, "table", table, "rows", rowCount,
				"index_name", indexName, "method", "estimated")
			return extent, nil
		}
		// 如果估算失败，记录并降级到其他方法
		s.log.Warn("ST_EstimatedExtent failed, falling back to other methods",
			"schema", schema, "table", table, "error", err)
	}

	// ========== 策略2: 小表使用完整计算 ==========
	if rowCount <= int64(largeTableThreshold) && rowCount > 0 {
		extent, err := s.calculateExtentFullScan(db, schema, table, geomColumn, srid)
		if err == nil && extent != nil {
			s.log.Info("Using full extent calculation",
				"schema", schema, "table", table, "rows", rowCount, "method", "full_scan")
			return extent, nil
		}
		// 如果全表扫描失败，记录并继续
		s.log.Warn("Full extent calculation failed", "schema", schema, "table", table, "error", err)
	}

	// ========== 策略3: 大表使用空间网格采样 ==========
	s.log.Info("Using spatial grid sampling for extent calculation",
		"schema", schema, "table", table, "rows", rowCount, "method", "grid_sampling")
	extent, err := s.calculateExtentGridSampling(db, schema, table, geomColumn, srid)
	if err != nil {
		return nil, fmt.Errorf("all extent calculation methods failed: %w", err)
	}

	return extent, nil
}

// calculateExtentFromEstimate 使用 ST_EstimatedExtent 计算范围（毫秒级，需要空间索引）
func (s *SpatialMetadataService) calculateExtentFromEstimate(db *sql.DB, schema, table, geomColumn string, srid int) ([]float64, error) {
	// 使用 ST_EstimatedExtent 从空间索引的统计信息获取边界框
	// 这个方法不需要扫描数据，毫秒级响应
	query := fmt.Sprintf(`
		WITH estimated_box AS (
			SELECT ST_SetSRID(ST_EstimatedExtent($1, $2, $3), $4) as extent_geom
		)
		SELECT
			round(ST_XMin(ST_Transform(extent_geom, 4326))::numeric, 6)::float8,
			round(ST_YMin(ST_Transform(extent_geom, 4326))::numeric, 6)::float8,
			round(ST_XMax(ST_Transform(extent_geom, 4326))::numeric, 6)::float8,
			round(ST_YMax(ST_Transform(extent_geom, 4326))::numeric, 6)::float8
		FROM estimated_box
		WHERE extent_geom IS NOT NULL
	`)

	var minLng, minLat, maxLng, maxLat sql.NullFloat64

	// 设置短超时（估算应该很快）
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := db.QueryRowContext(ctx, query, schema, table, geomColumn, srid).
		Scan(&minLng, &minLat, &maxLng, &maxLat)
	if err != nil {
		return nil, fmt.Errorf("ST_EstimatedExtent failed: %w", err)
	}

	if !minLng.Valid {
		return nil, fmt.Errorf("ST_EstimatedExtent returned null")
	}

	return []float64{minLng.Float64, minLat.Float64, maxLng.Float64, maxLat.Float64}, nil
}

// calculateExtentFullScan 完整扫描表计算范围（准确，但可能耗时）
func (s *SpatialMetadataService) calculateExtentFullScan(db *sql.DB, schema, table, geomColumn string, srid int) ([]float64, error) {
	query := fmt.Sprintf(`
		SELECT
			ST_XMin(extent) as min_lng,
			ST_YMin(extent) as min_lat,
			ST_XMax(extent) as max_lng,
			ST_YMax(extent) as max_lat
		FROM (
			SELECT ST_Extent(ST_Transform("%s", 4326)) as extent
			FROM "%s"."%s"
			WHERE "%s" IS NOT NULL
		) t
	`, geomColumn, schema, table, geomColumn)

	var minLng, minLat, maxLng, maxLat sql.NullFloat64

	// 设置较长的超时（完整扫描可能耗时）
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	err := db.QueryRowContext(ctx, query).Scan(&minLng, &minLat, &maxLng, &maxLat)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("extent calculation timeout after 5 minutes")
		}
		return nil, fmt.Errorf("failed to calculate extent: %w", err)
	}

	if !minLng.Valid {
		return nil, fmt.Errorf("no valid extent found")
	}

	return []float64{minLng.Float64, minLat.Float64, maxLng.Float64, maxLat.Float64}, nil
}

// calculateExtentGridSampling 使用空间网格采样计算范围（保证地理分布均匀）
// 避免了 TABLESAMPLE SYSTEM 可能只采样某个局部区域的问题
func (s *SpatialMetadataService) calculateExtentGridSampling(db *sql.DB, schema, table, geomColumn string, srid int) ([]float64, error) {
	// 先从索引估算得到粗略范围，然后分成多个网格点采样
	// 这样既保证了地理分布均匀性，又避免了全表扫描的性能问题

	// 1. 先用快速估算获取粗略范围
	estimateQuery := fmt.Sprintf(`
		SELECT ST_EstimatedExtent($1, $2, $3)
	`)

	var roughExtent string
	err := db.QueryRow(estimateQuery, schema, table, geomColumn).Scan(&roughExtent)
	if err == nil && roughExtent != "" {
		// 如果估算成功，直接返回（转换到4326）
		// 这样即使没有索引也能快速获得合理的范围估计
		query := fmt.Sprintf(`
			WITH estimated_box AS (
				SELECT ST_SetSRID(ST_EstimatedExtent($1, $2, $3), $4) as extent_geom
			)
			SELECT
				round(ST_XMin(ST_Transform(extent_geom, 4326))::numeric, 6)::float8,
				round(ST_YMin(ST_Transform(extent_geom, 4326))::numeric, 6)::float8,
				round(ST_XMax(ST_Transform(extent_geom, 4326))::numeric, 6)::float8,
				round(ST_YMax(ST_Transform(extent_geom, 4326))::numeric, 6)::float8
			FROM estimated_box
			WHERE extent_geom IS NOT NULL
		`)

		var minLng, minLat, maxLng, maxLat sql.NullFloat64
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		err := db.QueryRowContext(ctx, query, schema, table, geomColumn, srid).
			Scan(&minLng, &minLat, &maxLng, &maxLat)
		if err == nil && minLng.Valid {
			return []float64{minLng.Float64, minLat.Float64, maxLng.Float64, maxLat.Float64}, nil
		}
	}

	// 2. 如果估算失败，使用分块采样：采样多个随机块，每块取一些行
	// 这比 TABLESAMPLE SYSTEM 更能保证地理分布均匀性
	query := fmt.Sprintf(`
		SELECT
			round(ST_XMin(ST_Extent(ST_Transform("%s", 4326)))::numeric, 6)::float8,
			round(ST_YMin(ST_Extent(ST_Transform("%s", 4326)))::numeric, 6)::float8,
			round(ST_XMax(ST_Extent(ST_Transform("%s", 4326)))::numeric, 6)::float8,
			round(ST_YMax(ST_Extent(ST_Transform("%s", 4326)))::numeric, 6)::float8
		FROM (
			-- 分块采样：采样多个不同的块，每块采样500行，避免只采样某个局部区域
			SELECT "%s" FROM "%s"."%s"
			TABLESAMPLE BERNOULLI (5)  -- 伯努利采样，保证地理分布更均匀
			WHERE "%s" IS NOT NULL
			LIMIT 50000  -- 采样最多50000行
		) t
	`, geomColumn, geomColumn, geomColumn, geomColumn, geomColumn, schema, table, geomColumn)

	var minLng, minLat, maxLng, maxLat sql.NullFloat64

	// 设置较短的超时（采样应该很快）
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err = db.QueryRowContext(ctx, query).Scan(&minLng, &minLat, &maxLng, &maxLat)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("extent calculation timeout after 30 seconds")
		}
		return nil, fmt.Errorf("spatial grid sampling failed: %w", err)
	}

	if !minLng.Valid {
		return nil, fmt.Errorf("no valid extent found in grid sampling")
	}

	return []float64{minLng.Float64, minLat.Float64, maxLng.Float64, maxLat.Float64}, nil
}

// getTableRowCount 查询表的行数（使用统计信息，快速）
func (s *SpatialMetadataService) getTableRowCount(db *sql.DB, schema, table string) (int64, error) {
	// 优先使用 pg_class 统计信息（快速，但可能不精确）
	query := `
		SELECT c.reltuples::bigint
		FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = $1 AND c.relname = $2
	`

	var rowCount int64
	err := db.QueryRow(query, schema, table).Scan(&rowCount)
	if err != nil {
		// 降级到 COUNT(*) 查询（精确，但慢）
		countQuery := fmt.Sprintf(`SELECT COUNT(*) FROM "%s"."%s"`, schema, table)
		err = db.QueryRow(countQuery).Scan(&rowCount)
		if err != nil {
			return 0, fmt.Errorf("failed to count rows: %w", err)
		}
	}

	return rowCount, nil
}

// getGeometryTypes 统计几何类型
func (s *SpatialMetadataService) getGeometryTypes(db *sql.DB, schema, table, geomColumn string) ([]string, error) {
	query := fmt.Sprintf(`
		SELECT DISTINCT ST_GeometryType("%s") as geom_type
		FROM "%s"."%s"
		WHERE "%s" IS NOT NULL
		LIMIT 10
	`, geomColumn, schema, table, geomColumn)

	rows, err := db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to query geometry types: %w", err)
	}
	defer rows.Close()

	var types []string
	for rows.Next() {
		var geomType string
		if err := rows.Scan(&geomType); err != nil {
			continue
		}
		types = append(types, geomType)
	}

	return types, nil
}

// checkSpatialIndex 检查空间索引
func (s *SpatialMetadataService) checkSpatialIndex(db *sql.DB, schema, table, geomColumn string) (bool, string, error) {
	query := `
		SELECT indexname
		FROM pg_indexes
		WHERE schemaname = $1 AND tablename = $2
		  AND indexdef LIKE '%USING gist%'
		  AND indexdef LIKE '%' || $3 || '%'
		LIMIT 1
	`

	var indexName string
	err := db.QueryRow(query, schema, table, geomColumn).Scan(&indexName)
	if err != nil {
		if err == sql.ErrNoRows {
			return false, "", nil
		}
		return false, "", fmt.Errorf("failed to check spatial index: %w", err)
	}

	return true, indexName, nil
}

// detectUpdatedAtColumn 检测 updated_at 字段
func (s *SpatialMetadataService) detectUpdatedAtColumn(db *sql.DB, schema, table string) (bool, string) {
	// 常见的更新时间列名
	commonNames := []string{"updated_at", "update_time", "modified_at", "modify_time", "last_modified"}

	query := `
		SELECT column_name
		FROM information_schema.columns
		WHERE table_schema = $1 AND table_name = $2 AND column_name = $3
		  AND (data_type = 'timestamp' OR data_type = 'timestamptz' OR data_type LIKE '%time%')
	`

	for _, name := range commonNames {
		var col string
		err := db.QueryRow(query, schema, table, name).Scan(&col)
		if err == nil {
			return true, col
		}
	}

	return false, ""
}

// GetTableStats 查询表统计信息（用于变更检测）
func (s *SpatialMetadataService) GetTableStats(db *sql.DB, schema, table string) (*models.TableStats, error) {
	query := `
		SELECT
			n_tup_ins + n_tup_upd + n_tup_del as total_changes,
			n_live_tup,
			n_dead_tup,
			COALESCE(last_analyze, '1970-01-01'::timestamp) as last_analyze
		FROM pg_stat_user_tables
		WHERE schemaname = $1 AND relname = $2
	`

	stats := &models.TableStats{}
	var lastAnalyze time.Time
	err := db.QueryRow(query, schema, table).Scan(
		&stats.TotalChanges,
		&stats.LiveTuples,
		&stats.DeadTuples,
		&lastAnalyze,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil // 表不在统计中
		}
		return nil, fmt.Errorf("failed to query table stats: %w", err)
	}

	stats.LastAnalyze = lastAnalyze
	return stats, nil
}
