package api

import (
	"database/sql"
	"fmt"
	"net/http"
	"strconv"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/logger"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/spatial"
	"github.com/addp/manager/internal/config"
	"github.com/addp/manager/internal/service"
	"github.com/gin-gonic/gin"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// TileConfigHandler 瓦片配置处理器
type TileConfigHandler struct {
	quickViewService *service.QuickViewService
	systemClient     *commonClient.SystemClient
	cfg              *config.Config
}

// NewTileConfigHandler 创建瓦片配置处理器
func NewTileConfigHandler(
	quickViewService *service.QuickViewService,
	systemClient *commonClient.SystemClient,
	cfg *config.Config,
) *TileConfigHandler {
	return &TileConfigHandler{
		quickViewService: quickViewService,
		systemClient:     systemClient,
		cfg:              cfg,
	}
}

// TileConfigResponse 瓦片配置响应
type TileConfigResponse struct {
	MinZoom int       `json:"min_zoom"` // 最小 zoom 层级
	MaxZoom int       `json:"max_zoom"` // 最大 zoom 层级
	Extent  []float64 `json:"extent,omitempty"` // 地理范围（可选）
	SRID    int       `json:"srid,omitempty"`   // 坐标系（可选）
}

// GetTileConfig 获取指定表的瓦片配置
// GET /api/resources/:id/spatial/:schema/:table/tile-config
//
// 返回值：
// - min_zoom: 根据数据范围计算的最小 zoom 层级
// - max_zoom: 根据记录数智能计算的最大 zoom 层级
// - extent: 数据的地理范围（用于调试）
// - srid: 数据的坐标系
func (h *TileConfigHandler) GetTileConfig(c *gin.Context) {
	// 1. 解析参数
	resourceIDStr := c.Param("id")
	resourceID, err := strconv.ParseUint(resourceIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid resource id parameter"})
		return
	}

	schema := c.Param("schema")
	if schema == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "schema parameter is required"})
		return
	}

	table := c.Param("table")
	if table == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "table parameter is required"})
		return
	}

	// 2. 获取租户 ID（从 JWT 中间件注入）
	var tenantID uint
	if tid, exists := c.Get("tenant_id"); exists {
		if tidUint, ok := tid.(uint); ok {
			tenantID = tidUint
		}
	}

	// 3. 获取快显状态（包含 extent 和 srid）
	qv, err := h.quickViewService.GetStatus(
		c.Request.Context(),
		tenantID,
		uint(resourceID),
		schema,
		table,
	)

	if err != nil {
		logger.L().Error("Failed to get quick view status for tile config",
			"error", err,
			"resource_id", resourceID,
			"schema", schema,
			"table", table)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get tile config"})
		return
	}

	// 4. 获取 extent（优先从快显记录，回退到数据库查询）
	minZoom := 6 // 默认值
	srid := 4326 // 默认 WGS84
	extent := []float64{}

	if qv != nil && len(qv.Extent) == 4 {
		// 快显记录中有 extent
		extent = qv.Extent
		logger.L().Debug("Using extent from quick view",
			"extent", extent,
			"table", table)
	} else {
		// 没有快显记录或 extent 为空，从数据库动态查询
		queriedExtent, geomCol, err := h.queryExtentFromDB(uint(resourceID), schema, table)
		if err != nil {
			logger.L().Warn("Failed to query extent from database",
				"error", err,
				"table", table)
		} else if len(queriedExtent) == 4 {
			extent = queriedExtent
			logger.L().Info("Queried extent from database (no quick view)",
				"extent", extent,
				"geometry_column", geomCol,
				"table", table)
		}
	}

	// 5. 计算 MinZoom
	if qv != nil && qv.Status == "completed" && qv.MinZoom != nil {
		// 如果快显已完成，使用预计算的 minZoom
		minZoom = *qv.MinZoom
		logger.L().Debug("Using configured MinZoom from quick view (completed)",
			"min_zoom", minZoom,
			"table", table,
			"status", qv.Status)
	} else if len(extent) == 4 {
		// 有 extent 但未启用快显：计算参考值，使用更宽松的范围
		calculatedMin := spatial.CalculateMinZoomFromExtent(extent, srid)
		minZoom = calculatedMin - 2
		if minZoom < 1 {
			minZoom = 1
		}
		logger.L().Info("Calculated flexible MinZoom from extent",
			"calculated_min", calculatedMin,
			"flexible_min", minZoom,
			"extent", extent,
			"table", table)
	}

	// 5. 智能计算 MaxZoom（基于记录数）
	maxZoom := h.cfg.PreCache.MaxZoom // 默认18

	// 如果快显已完成且有 maxZoom，使用预计算值
	if qv != nil && qv.Status == "completed" && qv.MaxZoom > 0 {
		maxZoom = qv.MaxZoom
		logger.L().Debug("Using pre-computed MaxZoom from quick view",
			"max_zoom", maxZoom,
			"table", table)
	} else if len(extent) == 4 {
		// 动态计算 maxZoom（基于记录数和数据范围）
		recordCount, err := h.getTableRecordCount(uint(resourceID), schema, table)
		if err != nil {
			logger.L().Warn("Failed to get record count, using default maxZoom",
				"error", err,
				"table", table,
				"default_max_zoom", maxZoom)
		} else if recordCount > 0 {
			// 使用智能计算函数
			calculatedMaxZoom := spatial.CalculateMaxZoomByRecordCount(
				recordCount,
				h.cfg.PreCache.TargetRecordsPerTile,
				[4]float64{extent[0], extent[1], extent[2], extent[3]},
				srid,
			)

			// 确保不超过全局最大值
			if calculatedMaxZoom > h.cfg.PreCache.MaxZoom {
				calculatedMaxZoom = h.cfg.PreCache.MaxZoom
			}

			maxZoom = calculatedMaxZoom

			logger.L().Info("Calculated MaxZoom based on record count",
				"record_count", recordCount,
				"target_records_per_tile", h.cfg.PreCache.TargetRecordsPerTile,
				"calculated_max_zoom", maxZoom,
				"table", table)
		}
	}

	// 6. 返回配置
	response := TileConfigResponse{
		MinZoom: minZoom,
		MaxZoom: maxZoom,
		Extent:  extent,
		SRID:    srid,
	}

	logger.L().Info("Tile config returned",
		"resource_id", resourceID,
		"schema", schema,
		"table", table,
		"min_zoom", minZoom,
		"max_zoom", maxZoom)

	c.JSON(http.StatusOK, response)
}

// getTableRecordCount 获取表的记录数
func (h *TileConfigHandler) getTableRecordCount(resourceID uint, schema, table string) (int64, error) {
	// 1. 获取资源连接信息
	resource, err := h.systemClient.GetResource(resourceID)
	if err != nil {
		return 0, fmt.Errorf("failed to get resource: %w", err)
	}

	// 2. 构建连接字符串
	connStr, err := commonModels.BuildConnectionString(resource)
	if err != nil {
		return 0, fmt.Errorf("failed to build connection string: %w", err)
	}

	// 3. 连接数据库
	db, err := gorm.Open(postgres.Open(connStr), &gorm.Config{})
	if err != nil {
		return 0, fmt.Errorf("failed to connect to database: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return 0, fmt.Errorf("failed to get sql.DB: %w", err)
	}
	defer sqlDB.Close()

	// 4. 查询记录数
	var count int64
	query := fmt.Sprintf("SELECT COUNT(*) FROM %s.%s", schema, table)
	err = db.Raw(query).Scan(&count).Error
	if err != nil {
		return 0, fmt.Errorf("failed to query record count: %w", err)
	}

	return count, nil
}

// queryExtentFromDB 从数据库直接查询表的地理范围
// 返回: extent (4个坐标), geometry_column_name, error
func (h *TileConfigHandler) queryExtentFromDB(resourceID uint, schema, table string) ([]float64, string, error) {
	// 1. 获取资源连接信息
	resource, err := h.systemClient.GetResource(resourceID)
	if err != nil {
		return nil, "", fmt.Errorf("failed to get resource: %w", err)
	}

	// 2. 构建连接字符串
	connStr, err := commonModels.BuildConnectionString(resource)
	if err != nil {
		return nil, "", fmt.Errorf("failed to build connection string: %w", err)
	}

	// 3. 连接数据库
	db, err := gorm.Open(postgres.Open(connStr), &gorm.Config{})
	if err != nil {
		return nil, "", fmt.Errorf("failed to connect to database: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, "", fmt.Errorf("failed to get sql.DB: %w", err)
	}
	defer sqlDB.Close()

	// 4. 查找几何字段（从 information_schema）
	geomColumn, err := h.findGeometryColumn(db, schema, table)
	if err != nil {
		return nil, "", fmt.Errorf("failed to find geometry column: %w", err)
	}

	if geomColumn == "" {
		return nil, "", fmt.Errorf("no geometry column found in table %s.%s", schema, table)
	}

	// 5. 查询 extent（使用 ST_Extent 和 ST_Transform 转换为 WGS84）
	query := fmt.Sprintf(`
		SELECT
			ST_XMin(extent) as min_lng,
			ST_YMin(extent) as min_lat,
			ST_XMax(extent) as max_lng,
			ST_YMax(extent) as max_lat
		FROM (
			SELECT ST_Extent(ST_Transform("%s", 4326)) as extent
			FROM "%s"."%s"
		) t
	`, geomColumn, schema, table)

	var minLng, minLat, maxLng, maxLat sql.NullFloat64
	err = sqlDB.QueryRow(query).Scan(&minLng, &minLat, &maxLng, &maxLat)
	if err != nil {
		return nil, geomColumn, fmt.Errorf("failed to calculate extent: %w", err)
	}

	if !minLng.Valid || !minLat.Valid || !maxLng.Valid || !maxLat.Valid {
		return nil, geomColumn, fmt.Errorf("no valid extent found (empty geometry)")
	}

	extent := []float64{minLng.Float64, minLat.Float64, maxLng.Float64, maxLat.Float64}
	return extent, geomColumn, nil
}

// findGeometryColumn 查找表中的几何字段
func (h *TileConfigHandler) findGeometryColumn(db *gorm.DB, schema, table string) (string, error) {
	query := `
		SELECT column_name
		FROM information_schema.columns
		WHERE table_schema = $1
			AND table_name = $2
			AND udt_name IN ('geometry', 'geography')
		LIMIT 1
	`

	var geomColumn string
	err := db.Raw(query, schema, table).Scan(&geomColumn).Error
	if err != nil {
		// 没有找到几何字段，不是错误，只是返回空字符串
		if err == gorm.ErrRecordNotFound {
			return "", nil
		}
		return "", fmt.Errorf("failed to query geometry column: %w", err)
	}

	return geomColumn, nil
}
