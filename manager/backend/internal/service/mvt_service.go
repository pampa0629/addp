package service

import (
	"context"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/logger"
	commonModels "github.com/addp/common/models"
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

func NewMVTService(meta *repository.MetadataRepository, systemClient *commonClient.SystemClient) *MVTService {
	// 创建 TileGenerator（通过 SystemClient 适配器获取引擎信息）
	tileGen := mvt.NewTileGenerator(&engineServiceAdapter{systemClient: systemClient})

	return &MVTService{
		metadataRepo:  meta,
		systemClient:  systemClient,
		tileGenerator: tileGen,
	}
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
	// 1. 验证租户权限
	if s.systemClient == nil {
		return nil, ErrEngineAccessDenied
	}

	res, err := s.systemClient.GetEngine(resourceID)
	if err != nil {
		return nil, err
	}

	// 转换为 manager/internal/models.Engine 以使用 resourceAccessible 函数
	managerEngine := convertResource(res)
	if !resourceAccessible(managerEngine, tenantID) {
		return nil, ErrEngineAccessDenied
	}

	// 2. 解引用 tenantID（TileGenerator 需要具体值）
	var tid uint
	if tenantID != nil {
		tid = *tenantID
	} else if res.TenantID != nil {
		tid = *res.TenantID // 使用资源所属的 tenantID
	} else {
		tid = 1 // 默认租户 ID
	}

	// 3. 查询主键列名（用于 MVT 生成的 feature ID）
	db, err := s.tileGenerator.GetOrCreateDBPool(ctx, resourceID, tid)
	if err != nil {
		logger.L().Warn("Failed to get db pool for primary key query",
			"error", err,
			"engine_id", resourceID)
		// 使用默认主键
		return s.generateTileWithPK(ctx, resourceID, tid, schema, table, geomCol, cols, z, x, y, srid, "id")
	}

	primaryKey, err := s.tileGenerator.GetPrimaryKeyColumn(ctx, db, schema, table)
	if err != nil {
		logger.L().Warn("Failed to get primary key, using 'id' as fallback",
			"error", err,
			"schema", schema,
			"table", table)
		primaryKey = "id"
	}
	if primaryKey == "" {
		primaryKey = "id"
	}

	// 4. 如果未指定列，查询所有列
	if len(cols) == 0 {
		allCols, err := s.tileGenerator.GetAllColumns(ctx, db, schema, table, geomCol)
		if err != nil {
			logger.L().Warn("Failed to get all columns", "error", err)
		} else {
			cols = allCols
		}
	}

	// 5. ✅ 调用 TileGenerator 生成瓦片（复用底层实现）
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
	tileData, err := s.tileGenerator.GenerateTile(ctx, mvt.TileGenerationParams{
		EngineID:   resourceID,
		TenantID:   tenantID,
		Schema:     schema,
		Table:      table,
		GeomColumn: geomCol,
		SRID:       srid,
		PrimaryKey: primaryKey,
		Z:          z,
		X:          x,
		Y:          y,
	})

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

// ============================================================================
// 适配器：将 SystemClient 适配为 mvt.ResourceService 接口
// ============================================================================

type engineServiceAdapter struct {
	systemClient *commonClient.SystemClient
}

func (a *engineServiceAdapter) GetEngine(engineID, tenantID uint) (*commonModels.Engine, error) {
	if a.systemClient == nil {
		return nil, ErrEngineAccessDenied
	}

	// SystemClient.GetEngine 直接返回 *commonModels.Engine
	return a.systemClient.GetEngine(engineID)
}

// Note: access policy and ErrEngineAccessDenied are defined in metadata_service.go
