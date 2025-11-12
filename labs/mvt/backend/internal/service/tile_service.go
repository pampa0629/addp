package service

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/addp/mvt/internal/config"
	"github.com/addp/mvt/internal/models"
	_ "github.com/jackc/pgx/v5/stdlib"
)

// TileService 瓦片服务
type TileService struct {
	db          *sql.DB
	dataSources map[string]*models.DataSource
}

// NewTileService 创建瓦片服务
func NewTileService(cfg *config.Config, dataSources []models.DataSource) (*TileService, error) {
	// 连接 PostgreSQL
	db, err := sql.Open("pgx", cfg.GetDSN())
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	// 配置连接池
	db.SetMaxOpenConns(cfg.Database.MaxOpenConns)
	db.SetMaxIdleConns(cfg.Database.MaxIdleConns)
	db.SetConnMaxLifetime(cfg.Database.ConnMaxLifetime)

	// 验证连接
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	log.Printf("[INFO] Connected to database successfully")

	// 构建数据源映射
	dsMap := make(map[string]*models.DataSource)
	for i := range dataSources {
		dsMap[dataSources[i].ID] = &dataSources[i]
	}

	return &TileService{
		db:          db,
		dataSources: dsMap,
	}, nil
}

// GenerateTile 生成 MVT 瓦片
func (s *TileService) GenerateTile(ctx context.Context, datasourceID string, z, x, y int) ([]byte, error) {
	// 获取数据源配置
	ds, exists := s.dataSources[datasourceID]
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
	row := s.db.QueryRowContext(ctx, testQuery)

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
	query := fmt.Sprintf(`
WITH mvtgeom AS (
    SELECT
        ST_AsMVTGeom(
            ST_Transform(ST_SetSRID("%s", %d), 3857),
            ST_TileEnvelope($1, $2, $3),
            extent => %d,
            buffer => %d,
            clip_geom => true
        ) AS geom
        %s
    FROM "%s"."%s"
    WHERE ST_SetSRID("%s", %d) && ST_Transform(ST_TileEnvelope($1, $2, $3), %d)
      %s
)
SELECT ST_AsMVT(mvtgeom.*, '%s', %d) FROM mvtgeom WHERE geom IS NOT NULL;
`,
		ds.Connection.GeometryColumn, ds.Connection.SRID, // ST_SetSRID(geom, srid)
		ds.TileConfig.Extent,                             // extent
		ds.TileConfig.Buffer,                             // buffer
		propColumns,                                      // property columns
		ds.Connection.Schema, ds.Connection.Table,        // FROM schema.table
		ds.Connection.GeometryColumn, ds.Connection.SRID, // WHERE ST_SetSRID(geom, srid)
		ds.Connection.SRID,                               // tile envelope SRID
		filterClause,                                     // optional filters
		ds.ID,                                            // layer name
		ds.TileConfig.Extent,                             // MVT extent
	)

	return query
}

// GetDataSource 获取数据源配置
func (s *TileService) GetDataSource(datasourceID string) (*models.DataSource, error) {
	ds, exists := s.dataSources[datasourceID]
	if !exists {
		return nil, fmt.Errorf("datasource not found: %s", datasourceID)
	}
	return ds, nil
}

// ListDataSources 列出所有数据源
func (s *TileService) ListDataSources() []models.DataSource {
	result := make([]models.DataSource, 0, len(s.dataSources))
	for _, ds := range s.dataSources {
		result = append(result, *ds)
	}
	return result
}

// GetDataSourceExtent 获取数据源的空间范围（返回 WGS84 经纬度坐标）
func (s *TileService) GetDataSourceExtent(datasourceID string) ([]float64, error) {
    ds, exists := s.dataSources[datasourceID]
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
	err := s.db.QueryRowContext(ctx, query).Scan(&minLng, &minLat, &maxLng, &maxLat)
	if err != nil {
		return nil, fmt.Errorf("failed to query extent: %w", err)
	}

	// 返回格式: [minLng, minLat, maxLng, maxLat] (WGS84 经纬度)
	return []float64{minLng, minLat, maxLng, maxLat}, nil
}

// Close 关闭数据库连接
func (s *TileService) Close() error {
	return s.db.Close()
}
