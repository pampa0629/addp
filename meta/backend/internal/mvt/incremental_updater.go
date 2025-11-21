package mvt

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/addp/common/logger"
	commonModels "github.com/addp/common/models"
	metaModels "github.com/addp/meta/internal/models"
)

// IncrementalUpdater 增量更新服务
// 负责检测数据变更并只更新受影响的瓦片
type IncrementalUpdater struct {
	tileGen      *TileGenerator
	cache        *CacheService
	preprocessor *PreprocessService
	resourceSvc  ResourceService
}

// NewIncrementalUpdater 创建增量更新服务
func NewIncrementalUpdater(
	tileGen *TileGenerator,
	cache *CacheService,
	preprocessor *PreprocessService,
	resourceSvc ResourceService,
) *IncrementalUpdater {
	return &IncrementalUpdater{
		tileGen:      tileGen,
		cache:        cache,
		preprocessor: preprocessor,
		resourceSvc:  resourceSvc,
	}
}

// DetectChanges 检测表数据是否变更（基于 pg_stat_user_tables）
func (u *IncrementalUpdater) DetectChanges(
	ctx context.Context,
	item *metaModels.MetaItem,
	tenantID uint,
	oldStats *TableStats,
) (changed bool, newStats *TableStats, err error) {
	// 1. 获取资源连接
	resource, err := u.resourceSvc.GetResource(item.ResID, tenantID)
	if err != nil {
		return false, nil, fmt.Errorf("failed to get resource: %w", err)
	}

	connStr, err := commonModels.BuildConnectionString(resource)
	if err != nil {
		return false, nil, fmt.Errorf("failed to build connection string: %w", err)
	}

	db, err := sql.Open("pgx", connStr)
	if err != nil {
		return false, nil, fmt.Errorf("failed to connect to database: %w", err)
	}
	defer db.Close()

	// 2. 查询 pg_stat_user_tables
	schema, _ := item.Attributes["schema_name"].(string)
	tableName := item.Name

	query := `
		SELECT
			n_tup_ins + n_tup_upd + n_tup_del as total_changes,
			n_live_tup,
			n_dead_tup,
			COALESCE(last_analyze, '1970-01-01'::timestamp) as last_analyze
		FROM pg_stat_user_tables
		WHERE schemaname = $1 AND relname = $2
	`

	newStats = &TableStats{}
	var lastAnalyze time.Time
	err = db.QueryRowContext(ctx, query, schema, tableName).Scan(
		&newStats.TotalChanges,
		&newStats.LiveTuples,
		&newStats.DeadTuples,
		&lastAnalyze,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return false, nil, fmt.Errorf("table not found in pg_stat_user_tables")
		}
		return false, nil, fmt.Errorf("failed to query table stats: %w", err)
	}

	newStats.LastAnalyze = lastAnalyze

	// 3. 比较变更
	if oldStats == nil {
		// 首次扫描，认为有变更
		return true, newStats, nil
	}

	changed = newStats.TotalChanges > oldStats.TotalChanges ||
		newStats.LiveTuples != oldStats.LiveTuples

	return changed, newStats, nil
}

// GetChangedExtent 获取变更区域（基于 updated_at 字段）
// 返回: extent [minLng, minLat, maxLng, maxLat], changedCount, error
func (u *IncrementalUpdater) GetChangedExtent(
	ctx context.Context,
	item *metaModels.MetaItem,
	tenantID uint,
	lastPreprocessTime time.Time,
) ([]float64, int, error) {
	// 1. 检查是否有 updated_at 字段
	spatialMeta, ok := item.Attributes["spatial_metadata"].(map[string]interface{})
	if !ok {
		return nil, 0, fmt.Errorf("not a spatial table")
	}

	hasUpdatedAt, _ := spatialMeta["has_updated_at"].(bool)
	if !hasUpdatedAt {
		return nil, 0, fmt.Errorf("table does not have updated_at column")
	}

	updatedAtColumn, _ := spatialMeta["updated_at_column"].(string)
	geomColumn, _ := spatialMeta["geometry_column"].(string)

	// 2. 获取资源连接
	resource, err := u.resourceSvc.GetResource(item.ResID, tenantID)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to get resource: %w", err)
	}

	connStr, err := commonModels.BuildConnectionString(resource)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to build connection string: %w", err)
	}

	db, err := sql.Open("pgx", connStr)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to connect to database: %w", err)
	}
	defer db.Close()

	// 3. 查询变更区域
	schema, _ := item.Attributes["schema_name"].(string)
	tableName := item.Name

	query := fmt.Sprintf(`
		SELECT
			ST_XMin(extent) as min_lng,
			ST_YMin(extent) as min_lat,
			ST_XMax(extent) as max_lng,
			ST_YMax(extent) as max_lat,
			count
		FROM (
			SELECT
				ST_Extent(ST_Transform("%s", 4326)) as extent,
				COUNT(*) as count
			FROM "%s"."%s"
			WHERE "%s" > $1
		) t
	`, geomColumn, schema, tableName, updatedAtColumn)

	var minLng, minLat, maxLng, maxLat sql.NullFloat64
	var count int

	err = db.QueryRowContext(ctx, query, lastPreprocessTime).Scan(
		&minLng, &minLat, &maxLng, &maxLat, &count)

	if err != nil {
		return nil, 0, fmt.Errorf("failed to query changed extent: %w", err)
	}

	if !minLng.Valid || count == 0 {
		return nil, 0, nil // 无变更
	}

	extent := []float64{minLng.Float64, minLat.Float64, maxLng.Float64, maxLat.Float64}
	return extent, count, nil
}

// CalculateAffectedTiles 计算受影响的瓦片
func (u *IncrementalUpdater) CalculateAffectedTiles(
	changedExtent []float64,
	maxZoom int,
) []TileCoord {
	var tiles []TileCoord

	for z := 0; z <= maxZoom; z++ {
		minX, minY, maxX, maxY := calculateTileBounds(changedExtent, z)

		for x := minX; x <= maxX; x++ {
			for y := minY; y <= maxY; y++ {
				tiles = append(tiles, TileCoord{Z: z, X: x, Y: y})
			}
		}
	}

	return tiles
}

// PerformIncrementalUpdate 执行增量更新
func (u *IncrementalUpdater) PerformIncrementalUpdate(
	ctx context.Context,
	item *metaModels.MetaItem,
	tenantID uint,
	affectedTiles []TileCoord,
) error {
	logger.L().Info("开始增量更新",
		"item_id", item.ID,
		"affected_tiles", len(affectedTiles))

	// 1. 删除受影响的瓦片
	for _, coord := range affectedTiles {
		err := u.cache.DeleteTile(ctx, item.Fingerprint, coord.Z, coord.X, coord.Y)
		if err != nil {
			logger.L().Warn("Failed to delete tile", "coord", coord, "error", err)
		}
	}

	// 2. 并发重新生成受影响的瓦片
	concurrency := 10
	jobs := make(chan TileCoord, len(affectedTiles))
	errors := make(chan error, len(affectedTiles))

	// 启动 worker
	for i := 0; i < concurrency; i++ {
		go func() {
			for coord := range jobs {
				// 生成瓦片
				mvtData, err := u.tileGen.GenerateTile(ctx, item, tenantID, coord.Z, coord.X, coord.Y)
				if err != nil {
					errors <- fmt.Errorf("failed to generate tile %v: %w", coord, err)
					continue
				}

				// 空瓦片，跳过
				if len(mvtData) == 0 {
					errors <- nil
					continue
				}

				// 压缩
				gzData, err := gzipCompress(mvtData)
				if err != nil {
					errors <- fmt.Errorf("failed to compress tile %v: %w", coord, err)
					continue
				}

				// 存储
				err = u.cache.PutTile(ctx, item.Fingerprint, coord.Z, coord.X, coord.Y, gzData)
				if err != nil {
					errors <- fmt.Errorf("failed to put tile %v: %w", coord, err)
					continue
				}

				errors <- nil
			}
		}()
	}

	// 投递任务
	for _, coord := range affectedTiles {
		jobs <- coord
	}
	close(jobs)

	// 收集错误
	var lastError error
	for i := 0; i < len(affectedTiles); i++ {
		if err := <-errors; err != nil {
			lastError = err
			logger.L().Error("Tile generation error", "error", err)
		}
	}

	if lastError != nil {
		return fmt.Errorf("some tiles failed to generate: %w", lastError)
	}

	logger.L().Info("增量更新完成",
		"item_id", item.ID,
		"tiles_updated", len(affectedTiles))

	return nil
}

// PerformFullRegenerate 执行全量重新生成
func (u *IncrementalUpdater) PerformFullRegenerate(
	ctx context.Context,
	item *metaModels.MetaItem,
	tenantID uint,
	cfg PreprocessConfig,
) (*PreprocessMetadata, error) {
	logger.L().Info("开始全量重新生成",
		"item_id", item.ID)

	// 1. 删除所有旧瓦片
	err := u.cache.DeleteAllTiles(ctx, item.Fingerprint)
	if err != nil {
		logger.L().Warn("Failed to delete all tiles", "error", err)
		// 继续执行，不阻塞
	}

	// 2. 调用预处理服务重新生成
	metadata, err := u.preprocessor.StartPreprocess(ctx, item, tenantID, cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to regenerate: %w", err)
	}

	logger.L().Info("全量重新生成完成",
		"item_id", item.ID,
		"total_tiles", metadata.TotalTiles)

	return metadata, nil
}
