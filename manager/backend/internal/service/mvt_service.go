package service

import (
	"context"
	"database/sql"
	"fmt"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/logger"
	"github.com/addp/common/spatial"
	"github.com/addp/manager/internal/mvt"
	"github.com/addp/manager/internal/repository"
)

// MVTService generates Mapbox Vector Tiles (MVT) from PostGIS tables for preview.
// ✅ 重构：复用 mvt.TileGenerator，消除重复代码
type MVTService struct {
	metadataRepo  *repository.MetadataRepository
	systemClient  *commonClient.SystemClient
	tileGenerator *mvt.TileGenerator // ✅ 复用底层实现
}

func NewMVTService(meta *repository.MetadataRepository, systemClient *commonClient.SystemClient, maxDBConns int) *MVTService {
	// 传入连接池配置（实时生成瓦片也需要合理的连接池大小）
	tileGen := mvt.NewTileGenerator(systemClient, maxDBConns)

	return &MVTService{
		metadataRepo:  meta,
		systemClient:  systemClient,
		tileGenerator: tileGen,
	}
}

func (s *MVTService) tenantIDForEngine(engineID uint, tenantID *uint) (uint, error) {
	if s.systemClient == nil {
		return 0, ErrEngineAccessDenied
	}

	res, err := s.systemClient.GetEngine(engineID)
	if err != nil {
		return 0, err
	}

	managerEngine := convertResource(res)
	if !resourceAccessible(managerEngine, tenantID) {
		return 0, ErrEngineAccessDenied
	}

	if tenantID != nil {
		return *tenantID, nil
	}
	if res.TenantID != nil {
		return *res.TenantID, nil
	}
	return 1, nil
}

func (s *MVTService) GetSpatialExtentWGS84(
	ctx context.Context,
	tenantID *uint,
	resourceID uint,
	schema, table, geomCol string,
) ([]float64, error) {
	tid, err := s.tenantIDForEngine(resourceID, tenantID)
	if err != nil {
		return nil, err
	}
	extent, err := s.tileGenerator.GetSpatialExtent(ctx, resourceID, tid, schema, table, geomCol)
	if err != nil {
		return nil, fmt.Errorf("resolve MVT WGS84 extent: %w", err)
	}
	return extent, nil
}

func (s *MVTService) TransformExtentWGS84(
	ctx context.Context,
	tenantID *uint,
	resourceID uint,
	extent []float64,
	extentSRID int,
) ([]float64, error) {
	if len(extent) != 4 {
		return nil, fmt.Errorf("extent must contain 4 coordinates")
	}
	if extentSRID == spatial.SRIDWGS84 {
		return append([]float64(nil), extent...), nil
	}
	if extentSRID <= 0 {
		return nil, fmt.Errorf("extent_srid is required")
	}
	tid, err := s.tenantIDForEngine(resourceID, tenantID)
	if err != nil {
		return nil, err
	}
	db, err := s.tileGenerator.GetOrCreateDBPool(ctx, resourceID, tid)
	if err != nil {
		return nil, err
	}
	width := extent[2] - extent[0]
	height := extent[3] - extent[1]
	segmentLength := (width + height) / 100
	if segmentLength <= 0 {
		segmentLength = 1
	}
	const query = `
		WITH g AS (
			SELECT ST_Transform(
				ST_Segmentize(
					ST_SetSRID(ST_MakeEnvelope($1, $2, $3, $4), $5),
					$6
				),
				4326
			) AS geom
		)
		SELECT
			ST_XMin(Box2D(geom)),
			ST_YMin(Box2D(geom)),
			ST_XMax(Box2D(geom)),
			ST_YMax(Box2D(geom))
		FROM g
	`
	var minX, minY, maxX, maxY float64
	if err := db.QueryRowContext(ctx, query, extent[0], extent[1], extent[2], extent[3], extentSRID, segmentLength).Scan(&minX, &minY, &maxX, &maxY); err != nil {
		return nil, fmt.Errorf("transform extent to WGS84 failed: %w", err)
	}
	return []float64{minX, minY, maxX, maxY}, nil
}

// ResolveRealtimeTileTarget returns a tile source that can use a 3857 GiST path.
// It only checks lightweight PostgreSQL catalog metadata and never scans source data.
func (s *MVTService) ResolveRealtimeTileTarget(
	ctx context.Context,
	tenantID *uint,
	resourceID uint,
	schema, table, geomCol string,
	sourceSRID int,
) (*RealtimeTileTarget, error) {
	tid, err := s.tenantIDForEngine(resourceID, tenantID)
	if err != nil {
		return nil, err
	}
	db, err := s.tileGenerator.GetOrCreateDBPool(ctx, resourceID, tid)
	if err != nil {
		return nil, err
	}

	if sourceSRID == spatial.SRIDWebMercator {
		ok, err := hasValidGiSTIndex(ctx, db, schema, table, geomCol)
		if err != nil {
			return nil, err
		}
		if ok {
			return &RealtimeTileTarget{
				Schema:     schema,
				Table:      table,
				GeomColumn: geomCol,
				SRID:       spatial.SRIDWebMercator,
			}, nil
		}
		return nil, nil
	}

	mvName := spatial.PostGISMaterializedViewName(table)
	populated, err := materializedViewPopulated(ctx, db, schema, mvName)
	if err != nil || !populated {
		return nil, err
	}
	const mvGeomColumn = "geom_3857"
	ok, err := hasValidGiSTIndex(ctx, db, schema, mvName, mvGeomColumn)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}
	return &RealtimeTileTarget{
		Schema:       schema,
		Table:        mvName,
		GeomColumn:   mvGeomColumn,
		SRID:         spatial.SRIDWebMercator,
		Prepared3857: true,
	}, nil
}

func materializedViewPopulated(ctx context.Context, db *sql.DB, schema, view string) (bool, error) {
	const query = `
		SELECT ispopulated
		FROM pg_matviews
		WHERE schemaname = $1 AND matviewname = $2
	`
	var populated bool
	if err := db.QueryRowContext(ctx, query, schema, view).Scan(&populated); err != nil {
		if err == sql.ErrNoRows {
			return false, nil
		}
		return false, fmt.Errorf("query materialized view failed: %w", err)
	}
	return populated, nil
}

func hasValidGiSTIndex(ctx context.Context, db *sql.DB, schema, table, geomColumn string) (bool, error) {
	const query = `
		SELECT EXISTS (
			SELECT 1
			FROM pg_index i
			JOIN pg_class idx ON idx.oid = i.indexrelid
			JOIN pg_class tbl ON tbl.oid = i.indrelid
			JOIN pg_namespace ns ON ns.oid = tbl.relnamespace
			JOIN pg_am am ON am.oid = idx.relam
			JOIN pg_attribute attr ON attr.attrelid = tbl.oid AND attr.attnum = ANY(i.indkey)
			WHERE ns.nspname = $1
			  AND tbl.relname = $2
			  AND attr.attname = $3
			  AND am.amname = 'gist'
			  AND i.indisvalid
		)
	`
	var exists bool
	if err := db.QueryRowContext(ctx, query, schema, table, geomColumn).Scan(&exists); err != nil {
		return false, fmt.Errorf("query gist index failed: %w", err)
	}
	return exists, nil
}

// GetTile produces a single MVT tile for given z/x/y and table.
// Validates tenant access against the resource.
func (s *MVTService) GetTile(
	ctx context.Context,
	tenantID *uint,
	resourceID uint,
	schema, table, geomCol string,
	cols []string,
	z, x, y int,
	srid int,
) ([]byte, error) {
	// 1. 验证租户权限，并解析瓦片生成使用的租户 ID。
	tid, err := s.tenantIDForEngine(resourceID, tenantID)
	if err != nil {
		return nil, err
	}

	// 2. 查询主键列名（用于 MVT 生成的 feature ID）
	db, err := s.tileGenerator.GetOrCreateDBPool(ctx, resourceID, tid)
	if err != nil {
		logger.L().Warn("Failed to get db pool for primary key query",
			"error", err,
			"engine_id", resourceID)
		// 无主键时传空字符串，让 BuildMVTQuery 跳过 ID 列
		return s.generateTileWithPK(ctx, resourceID, tid, schema, table, geomCol, cols, z, x, y, srid, "")
	}

	primaryKey, err := s.tileGenerator.GetPrimaryKeyColumn(ctx, db, schema, table)
	if err != nil {
		logger.L().Warn("Failed to get primary key, will generate MVT without ID column",
			"error", err,
			"schema", schema,
			"table", table)
		primaryKey = ""
	}
	if primaryKey == "" {
		logger.L().Info("Table has no primary key, MVT will not include feature ID",
			"schema", schema,
			"table", table)
	}

	// 3. 如果未指定列，查询所有列
	if len(cols) == 0 {
		allCols, err := s.tileGenerator.GetAllColumns(ctx, db, schema, table, geomCol)
		if err != nil {
			logger.L().Warn("Failed to get all columns", "error", err)
		} else {
			cols = allCols
		}
	}

	// 4. ✅ 调用 TileGenerator 生成瓦片（复用底层实现）
	return s.generateTileWithPK(ctx, resourceID, tid, schema, table, geomCol, cols, z, x, y, srid, primaryKey)
}

// generateTileWithPK 使用指定的主键生成 MVT 瓦片
func (s *MVTService) generateTileWithPK(
	ctx context.Context,
	resourceID, tenantID uint,
	schema, table, geomCol string,
	cols []string,
	z, x, y int,
	srid int,
	primaryKey string,
) ([]byte, error) {
	// ✅ 调用 TileGenerator（统一实现，带连接池）
	tileData, err := s.tileGenerator.GenerateTile(ctx, mvt.TileGenerationSource{
		EngineID:   resourceID,
		TenantID:   tenantID,
		Schema:     schema,
		Table:      table,
		GeomColumn: geomCol,
		SRID:       srid,
		PrimaryKey: primaryKey,
	}.Params(mvt.TileCoord{Z: z, X: x, Y: y}))

	if err != nil {
		logger.L().Error("MVT tile generation failed",
			"error", err,
			"engine_id", resourceID,
			"schema", schema,
			"table", table,
			"z", z, "x", x, "y", y)
		return nil, err
	}

	if tileData == nil {
		return []byte{}, nil
	}

	return tileData, nil
}

// Close 关闭所有连接池 (服务关闭时调用)
func (s *MVTService) Close() error {
	return s.tileGenerator.Close()
}

// Note: access policy and ErrEngineAccessDenied are defined in metadata_service.go
