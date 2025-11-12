package api

import (
	"context"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/addp/mvt/internal/models"
	"github.com/addp/mvt/internal/service"
	"github.com/gin-gonic/gin"
)

// Handler API 处理器
type Handler struct {
	tileService  *service.TileService
	cacheService *service.CacheService
}

// NewHandler 创建处理器
func NewHandler(tileService *service.TileService, cacheService *service.CacheService) *Handler {
	return &Handler{
		tileService:  tileService,
		cacheService: cacheService,
	}
}

// GetTile 获取 MVT 瓦片
func (h *Handler) GetTile(c *gin.Context) {
	datasourceID := c.Param("datasource_id")
	z, _ := strconv.Atoi(c.Param("z"))
	x, _ := strconv.Atoi(c.Param("x"))

	// filepath includes leading slash and .mvt extension, e.g., "/1551.mvt"
	filepath := c.Param("filepath")
	// Remove leading slash and .mvt extension
	yStr := filepath[1:] // Remove leading "/"
	if len(yStr) > 4 && yStr[len(yStr)-4:] == ".mvt" {
		yStr = yStr[:len(yStr)-4]
	}
	y, _ := strconv.Atoi(yStr)

	log.Printf("[DEBUG HANDLER] URL=%s, datasourceID=%s, z=%d, x=%d, y=%d",
		c.Request.URL.Path, datasourceID, z, x, y)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 1. 尝试从缓存获取
	cachedTile, err := h.cacheService.GetTile(ctx, datasourceID, z, x, y)
	if err != nil {
		log.Printf("Cache error: %v", err)
	}

	if cachedTile != nil {
		c.Header("Content-Type", "application/vnd.mapbox-vector-tile")
		c.Header("Cache-Control", "public, max-age=86400")
		c.Header("X-Cache", "HIT")
		c.Data(http.StatusOK, "application/vnd.mapbox-vector-tile", cachedTile)
		return
	}

	// 2. 从 PostGIS 生成瓦片
	tile, err := h.tileService.GenerateTile(ctx, datasourceID, z, x, y)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 3. 异步写入缓存
	go func() {
		cacheCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := h.cacheService.SetTile(cacheCtx, datasourceID, z, x, y, tile); err != nil {
			log.Printf("Failed to cache tile: %v", err)
		}
	}()

	// 4. 返回瓦片
	c.Header("Content-Type", "application/vnd.mapbox-vector-tile")
	c.Header("Cache-Control", "public, max-age=86400")
	c.Header("X-Cache", "MISS")
	c.Data(http.StatusOK, "application/vnd.mapbox-vector-tile", tile)
}

// ListDataSources 列出所有数据源
func (h *Handler) ListDataSources(c *gin.Context) {
	dataSources := h.tileService.ListDataSources()

	// 为每个数据源添加extent信息
	type DataSourceWithExtent struct {
		models.DataSource
		Extent []float64 `json:"extent,omitempty"`
	}

	result := make([]DataSourceWithExtent, 0, len(dataSources))
	for _, ds := range dataSources {
		dsWithExtent := DataSourceWithExtent{DataSource: ds}

		// 尝试获取extent
		if extent, err := h.tileService.GetDataSourceExtent(ds.ID); err == nil {
			dsWithExtent.Extent = extent
		} else {
			log.Printf("[WARN] Failed to get extent for %s: %v", ds.ID, err)
		}

		result = append(result, dsWithExtent)
	}

	c.JSON(http.StatusOK, gin.H{"datasources": result})
}

// GetDataSource 获取数据源详情
func (h *Handler) GetDataSource(c *gin.Context) {
	datasourceID := c.Param("id")

	ds, err := h.tileService.GetDataSource(datasourceID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, ds)
}

// ClearCache 清空缓存
func (h *Handler) ClearCache(c *gin.Context) {
	datasourceID := c.Param("datasource_id")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var err error
	if datasourceID != "" {
		err = h.cacheService.ClearDataSource(ctx, datasourceID)
	} else {
		err = h.cacheService.ClearAll(ctx)
	}

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Cache cleared successfully"})
}

// GetCacheStats 获取缓存统计
func (h *Handler) GetCacheStats(c *gin.Context) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	stats, err := h.cacheService.GetStats(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, stats)
}

// Health 健康检查
func (h *Handler) Health(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status": "ok",
		"time":   time.Now().Format(time.RFC3339),
	})
}
