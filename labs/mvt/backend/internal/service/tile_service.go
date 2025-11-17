package service

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"strings"
	"time"
    "sync"

	"github.com/addp/mvt/internal/config"
	"github.com/addp/mvt/internal/models"
	_ "github.com/jackc/pgx/v5/stdlib"
)

// TileService 瓦片服务
type TileService struct {
	db          *sql.DB  // 主连接池（预热使用）
	priorityDB  *sql.DB  // 优先级连接池（浏览请求使用）
	dataSources map[string]*models.DataSource
    mu          sync.RWMutex
}

// NewTileService 创建瓦片服务
func NewTileService(cfg *config.Config, dataSources []models.DataSource) (*TileService, error) {
	// 主连接池（预热使用，较小连接数避免占用过多资源）
	db, err := sql.Open("pgx", cfg.GetDSN())
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	// 配置主连接池（预热用，限制连接数）
	prewarmMaxConns := cfg.Database.MaxOpenConns / 2  // 预热最多用一半连接
	if prewarmMaxConns < 5 { prewarmMaxConns = 5 }
	db.SetMaxOpenConns(prewarmMaxConns)
	db.SetMaxIdleConns(cfg.Database.MaxIdleConns / 2)
	db.SetConnMaxLifetime(cfg.Database.ConnMaxLifetime)

	// 优先级连接池（浏览请求使用，保证响应速度）
	priorityDB, err := sql.Open("pgx", cfg.GetDSN())
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to connect to priority database: %w", err)
	}

	// 配置优先级连接池（浏览请求用，保证足够连接）
	priorityDB.SetMaxOpenConns(cfg.Database.MaxOpenConns)
	priorityDB.SetMaxIdleConns(cfg.Database.MaxIdleConns)
	priorityDB.SetConnMaxLifetime(cfg.Database.ConnMaxLifetime)

	// 验证连接
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		db.Close()
		priorityDB.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	if err := priorityDB.PingContext(ctx); err != nil {
		db.Close()
		priorityDB.Close()
		return nil, fmt.Errorf("failed to ping priority database: %w", err)
	}

	log.Printf("[INFO] Connected to database successfully (main pool: %d, priority pool: %d)",
		prewarmMaxConns, cfg.Database.MaxOpenConns)

	// 构建数据源映射
	dsMap := make(map[string]*models.DataSource)
	for i := range dataSources {
		dsMap[dataSources[i].ID] = &dataSources[i]
	}

	return &TileService{
		db:          db,
		priorityDB:  priorityDB,
		dataSources: dsMap,
	}, nil
}

// GenerateTile 生成 MVT 瓦片
func (s *TileService) GenerateTile(ctx context.Context, datasourceID string, z, x, y int) ([]byte, error) {
	return s.generateTileWithDB(ctx, datasourceID, z, x, y, s.priorityDB)
}

// GenerateTilePrewarm 预热专用：生成 MVT 瓦片（使用主连接池）
func (s *TileService) GenerateTilePrewarm(ctx context.Context, datasourceID string, z, x, y int) ([]byte, error) {
	return s.generateTileWithDB(ctx, datasourceID, z, x, y, s.db)
}

// generateTileWithDB 内部实现：使用指定数据库连接生成瓦片
func (s *TileService) generateTileWithDB(ctx context.Context, datasourceID string, z, x, y int, db *sql.DB) ([]byte, error) {
	// 获取数据源配置
	s.mu.RLock()
	ds, exists := s.dataSources[datasourceID]
	s.mu.RUnlock()
	if !exists {
		return nil, fmt.Errorf("datasource not found: %s", datasourceID)
	}

	// 检查缩放级别
	if z < ds.TileConfig.MinZoom || z > ds.TileConfig.MaxZoom {
		return []byte{}, nil // 返回空瓦片
	}

	// 构建 SQL 查询
	query := s.buildMVTQuery(ds, z, x, y)

	// 打印SQL用于调试
	log.Printf("[DEBUG] Generating tile for datasource=%s, z=%d, x=%d, y=%d", datasourceID, z, x, y)

	// 临时测试：使用硬编码参数而不是占位符
	testQuery := strings.ReplaceAll(query, "$1", fmt.Sprintf("%d", z))
	testQuery = strings.ReplaceAll(testQuery, "$2", fmt.Sprintf("%d", x))
	testQuery = strings.ReplaceAll(testQuery, "$3", fmt.Sprintf("%d", y))
	log.Printf("[DEBUG] SQL Query with hardcoded params:\n%s", testQuery)

	// 执行查询（先用硬编码测试）
	var mvtData []byte
	row := db.QueryRowContext(ctx, testQuery)

	// 尝试扫描并详细记录错误
	err := row.Scan(&mvtData)
	if err != nil {
		if err == sql.ErrNoRows {
			log.Printf("[DEBUG] No rows returned from MVT query")
			return []byte{}, nil // 空瓦片
		}
		log.Printf("[ERROR] ====== MVT QUERY FAILED ======")
		log.Printf("[ERROR] Error: %v", err)
		log.Printf("[ERROR] Error Type: %T", err)
		log.Printf("[ERROR] Datasource: %s, z=%d, x=%d, y=%d", datasourceID, z, x, y)
		log.Printf("[ERROR] SRID: %d", ds.Connection.SRID)
		log.Printf("[ERROR] Table: %s.%s", ds.Connection.Schema, ds.Connection.Table)
		log.Printf("[ERROR] Geometry Column: %s", ds.Connection.GeometryColumn)
		log.Printf("[ERROR] Full Query:\n%s", testQuery)
		log.Printf("[ERROR] ================================")
		return nil, fmt.Errorf("failed to generate tile: %w", err)
	}

	log.Printf("[DEBUG] MVT data size: %d bytes", len(mvtData))
	if len(mvtData) == 0 {
		log.Printf("[WARN] MVT query returned 0 bytes")
	}

	return mvtData, nil
}

// buildMVTQuery 构建 MVT 查询 SQL（简化版，不使用extent CTE）
func (s *TileService) buildMVTQuery(ds *models.DataSource, z, x, y int) string {
	// 构建属性字段列表，需要加引号以支持中文字段名
	properties := make([]string, 0, len(ds.Properties))
	for _, prop := range ds.Properties {
		properties = append(properties, fmt.Sprintf(`"%s"`, prop.Name))
	}
	propColumns := strings.Join(properties, ", ")
	if propColumns != "" {
		propColumns = ", " + propColumns
	}

	// 构建过滤条件
	filterClause := ""
	if len(ds.Filters) > 0 {
		filterClause = " AND " + strings.Join(ds.Filters, " AND ")
	}

    // 简化SQL：直接使用ST_TileEnvelope作为bbox
    // 计算当前缩放的有效像素阈值（按区间规则优先，其次全局阈值）
    effectiveMinPxLine := ds.TileConfig.MinPxLine
    effectiveMinPxPoly := ds.TileConfig.MinPxPoly
    for _, r := range ds.TileConfig.PixelRules {
        if z >= r.MinZoom && z <= r.MaxZoom {
            if r.MinPxLine > 0 {
                effectiveMinPxLine = r.MinPxLine
            }
            if r.MinPxPoly > 0 {
                effectiveMinPxPoly = r.MinPxPoly
            }
            break
        }
    }
    // 根据配置决定是否启用基于像素阈值的小要素过滤
    pixelFilterEnabled := effectiveMinPxLine > 0 || effectiveMinPxPoly > 0

    baseCTE := fmt.Sprintf(`
WITH mvtgeom AS (
    SELECT
        ST_AsMVTGeom(
            ST_Transform("%s", 3857),
            ST_TileEnvelope($1, $2, $3),
            extent => %d,
            buffer => %d,
            clip_geom => true
        ) AS geom
        %s
    FROM "%s"."%s"
    WHERE "%s" && ST_Transform(ST_TileEnvelope($1, $2, $3), %d)
      %s
)
`,
        ds.Connection.GeometryColumn,                     // geometry column
        ds.TileConfig.Extent,                             // extent
        ds.TileConfig.Buffer,                             // buffer
        propColumns,                                      // property columns
        ds.Connection.Schema, ds.Connection.Table,        // FROM schema.table
        ds.Connection.GeometryColumn,                     // WHERE geom && bbox
        ds.Connection.SRID,                               // tile envelope SRID
        filterClause,                                     // optional filters
    )

    var query string
    if pixelFilterEnabled {
        // 瓦片空间后过滤：按像素阈值剔除极小线/面
        // 1px = (extent/256) 瓦片单位
        query = fmt.Sprintf(`%s
SELECT ST_AsMVT(t.*, '%s', %d)
FROM (
  SELECT *
  FROM mvtgeom
  WHERE geom IS NOT NULL
    AND (
      CASE
        WHEN ST_Dimension(geom) = 1 THEN ST_Length(geom) >= (%f * (%d.0/256.0))
        WHEN ST_Dimension(geom) = 2 THEN ST_Area(geom)  >= POWER((%f * (%d.0/256.0)), 2)
        ELSE TRUE
      END
    )
) AS t;`,
            baseCTE,
            ds.ID,                    // layer name
            ds.TileConfig.Extent,     // MVT extent
            effectiveMinPxLine,       // min pixels for line
            ds.TileConfig.Extent,
            effectiveMinPxPoly,       // min pixels for polygon
            ds.TileConfig.Extent,
        )
    } else {
        // 不启用像素过滤：保持原逻辑
        query = fmt.Sprintf(`%s
SELECT ST_AsMVT(mvtgeom.*, '%s', %d) FROM mvtgeom WHERE geom IS NOT NULL;`,
            baseCTE,
            ds.ID,
            ds.TileConfig.Extent,
        )
    }

    return query
}

// GetDataSource 获取数据源配置
func (s *TileService) GetDataSource(datasourceID string) (*models.DataSource, error) {
	s.mu.RLock()
	ds, exists := s.dataSources[datasourceID]
	s.mu.RUnlock()
	if !exists {
		return nil, fmt.Errorf("datasource not found: %s", datasourceID)
	}
	return ds, nil
}

// ListDataSources 列出所有数据源
func (s *TileService) ListDataSources() []models.DataSource {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]models.DataSource, 0, len(s.dataSources))
	for _, ds := range s.dataSources {
		result = append(result, *ds)
	}
	return result
}

// GetDataSourceExtent 获取数据源的空间范围（返回 WGS84 经纬度坐标）
func (s *TileService) GetDataSourceExtent(datasourceID string) ([]float64, error) {
    s.mu.RLock()
    ds, exists := s.dataSources[datasourceID]
    s.mu.RUnlock()
    if !exists {
        return nil, fmt.Errorf("datasource not found: %s", datasourceID)
    }

    // 查询数据的空间范围
    // 如果 SRID 不是 4326，则转换到 WGS84 (EPSG:4326) 供前端地图使用
    var query string
    if ds.Connection.SRID == 4326 {
        // 已经是 WGS84，直接查询
        query = fmt.Sprintf(`
            SELECT
                ST_XMin(extent) AS minLng,
                ST_YMin(extent) AS minLat,
                ST_XMax(extent) AS maxLng,
                ST_YMax(extent) AS maxLat
            FROM (
                SELECT ST_Extent("%s") AS extent
                FROM "%s"."%s"
            ) t
        `, ds.Connection.GeometryColumn, ds.Connection.Schema, ds.Connection.Table)
    } else {
        // 需要投影转换到 WGS84
        // 使用 ST_Transform 将 extent 的边界点转换到 4326
        query = fmt.Sprintf(`
            SELECT
                ST_XMin(extent_4326) AS minLng,
                ST_YMin(extent_4326) AS minLat,
                ST_XMax(extent_4326) AS maxLng,
                ST_YMax(extent_4326) AS maxLat
            FROM (
                SELECT ST_Transform(ST_SetSRID(ST_Extent("%s"), %d), 4326) AS extent_4326
                FROM "%s"."%s"
            ) t
        `, ds.Connection.GeometryColumn, ds.Connection.SRID, ds.Connection.Schema, ds.Connection.Table)
    }

	// 添加超时上下文，避免长时间阻塞（千万级数据需要更长时间）
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var minLng, minLat, maxLng, maxLat float64
	err := s.db.QueryRowContext(ctx, query).Scan(&minLng, &minLat, &maxLng, &maxLat)  // 使用主连接池（低优先级）
	if err != nil {
		return nil, fmt.Errorf("failed to query extent: %w", err)
	}

	// 返回格式: [minLng, minLat, maxLng, maxLat] (WGS84 经纬度)
	return []float64{minLng, minLat, maxLng, maxLat}, nil
}

// Close 关闭数据库连接
func (s *TileService) Close() error {
	var err1, err2 error
	if s.db != nil {
		err1 = s.db.Close()
	}
	if s.priorityDB != nil {
		err2 = s.priorityDB.Close()
	}
	if err1 != nil {
		return err1
	}
	return err2
}

// UpdateDataSources 热更新数据源配置
func (s *TileService) UpdateDataSources(newSources []models.DataSource) {
    dsMap := make(map[string]*models.DataSource)
    for i := range newSources {
        dsMap[newSources[i].ID] = &newSources[i]
    }
    s.mu.Lock()
    s.dataSources = dsMap
    s.mu.Unlock()
    log.Printf("[INFO] Datasources reloaded: %d entries", len(dsMap))
}
