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
// 大表自动使用采样避免 OOM
func (s *SpatialMetadataService) calculateExtent(db *sql.DB, schema, table, geomColumn string, srid int) ([]float64, error) {
	// 1. 查询表行数
	rowCount, err := s.getTableRowCount(db, schema, table)
	if err != nil {
		s.log.Warn("Failed to get table row count, using full calculation", "error", err)
		rowCount = 0 // 降级到全表计算
	}

	// 2. 获取配置参数
	largeTableThreshold := 1000000 // 默认阈值 100 万行
	samplePercent := 1             // 默认采样 1%
	if s.config != nil {
		largeTableThreshold = s.config.LargeTableThreshold
		samplePercent = s.config.ExtentSamplePercent
	}

	var extentQuery string

	// 3. 根据行数选择计算策略
	if rowCount > int64(largeTableThreshold) {
		// 大表：使用采样（TABLESAMPLE + LIMIT）
		s.log.Info("Large table detected, using sampling for extent calculation",
			"schema", schema, "table", table, "rows", rowCount,
			"sample_percent", samplePercent)

		extentQuery = fmt.Sprintf(`
			SELECT
				ST_XMin(extent) as min_lng,
				ST_YMin(extent) as min_lat,
				ST_XMax(extent) as max_lng,
				ST_YMax(extent) as max_lat
			FROM (
				SELECT ST_Extent(ST_Transform("%s", 4326)) as extent
				FROM (
					SELECT "%s" FROM "%s"."%s"
					WHERE "%s" IS NOT NULL
					TABLESAMPLE SYSTEM (%d)
					LIMIT 10000
				) AS sample
			) t
		`, geomColumn, geomColumn, schema, table, geomColumn, samplePercent)
	} else {
		// 小表：完整计算
		extentQuery = fmt.Sprintf(`
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
	}

	var minLng, minLat, maxLng, maxLat sql.NullFloat64
	err = db.QueryRow(extentQuery).Scan(&minLng, &minLat, &maxLng, &maxLat)
	if err != nil {
		return nil, fmt.Errorf("failed to calculate extent: %w", err)
	}

	if !minLng.Valid {
		return nil, fmt.Errorf("no valid extent found")
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
