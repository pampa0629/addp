package api

import (
    "context"
    "log"
    "net/http"
    "strconv"
    "time"

    appcfg "github.com/addp/mvt/internal/config"
    "github.com/addp/mvt/internal/models"
    "github.com/addp/mvt/internal/service"
    "github.com/gin-gonic/gin"
    "golang.org/x/sync/singleflight"
)

// Handler API 处理器
type Handler struct {
    tileService  *service.TileService
    cacheService *service.CacheService
    sf           singleflight.Group
    cfg          *appcfg.Config
}

// NewHandler 创建处理器
func NewHandler(tileService *service.TileService, cacheService *service.CacheService, cfg *appcfg.Config) *Handler {
    return &Handler{
        tileService:  tileService,
        cacheService: cacheService,
        cfg:          cfg,
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

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Second)
	defer cancel()

    // 1. 尝试从缓存获取（内存→Redis→PG），返回 gzip
    cachedTile, err := h.cacheService.GetTile(ctx, datasourceID, z, x, y)
    if err != nil {
        log.Printf("Cache error: %v", err)
    }

    if cachedTile != nil {
        c.Header("Content-Type", "application/vnd.mapbox-vector-tile")
        c.Header("Content-Encoding", "gzip")
        c.Header("Cache-Control", "public, max-age=86400")
        c.Header("X-Cache", "HIT")
        c.Header("Vary", "Accept-Encoding")
        c.Data(http.StatusOK, "application/vnd.mapbox-vector-tile", cachedTile)
        return
    }

    // 2. singleflight 合并并生成→gzip→缓存
    v, err, _ := h.sf.Do(
        datasourceID+":"+strconv.Itoa(z)+":"+strconv.Itoa(x)+":"+strconv.Itoa(y),
        func() (interface{}, error) {
            genStart := time.Now()
            raw, err := h.tileService.GenerateTile(ctx, datasourceID, z, x, y)
            if err != nil { return nil, &service.GenError{Err: err, Elapsed: time.Since(genStart)} }
            // 根据耗时与大小决定是否持久化到 PG（配置化阈值）
            dur := time.Since(genStart)
            minDur := h.cfg.CachePolicy.PersistMinDuration
            minKB  := h.cfg.CachePolicy.PersistMinRawKB
            if minDur <= 0 { minDur = 3 * time.Second }
            if minKB <= 0 { minKB = 100 }
            persist := dur >= minDur || len(raw) >= minKB*1024

            // 压缩为 gzip
            gz, err := h.cacheService.Gzip(raw)
            if err != nil { return nil, err }

            // 异步写缓存（容错）
            go func(gzData []byte, persist bool) {
                cacheCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
                defer cancel()
                if err := h.cacheService.SetTileWithOptions(cacheCtx, datasourceID, z, x, y, gzData, service.SetTileOptions{PersistToPG: persist}); err != nil {
                    log.Printf("Failed to cache tile: %v", err)
                }
            }(gz, persist)

            return gz, nil
        })
    if err != nil {
        if ge, ok := err.(*service.GenError); ok {
            log.Printf("[ERROR] Tile generation failed (%s): ds=%s z=%d x=%d y=%d err=%v", ge.Elapsed, datasourceID, z, x, y, ge.Err)
            c.JSON(http.StatusInternalServerError, gin.H{"error": ge.Err.Error(), "elapsed_ms": ge.Elapsed.Milliseconds()})
        } else {
            log.Printf("[ERROR] Tile generation failed: ds=%s z=%d x=%d y=%d err=%v", datasourceID, z, x, y, err)
            c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
        }
        return
    }

    // 3. 返回 gzip 瓦片
    gz := v.([]byte)
    c.Header("Content-Type", "application/vnd.mapbox-vector-tile")
    c.Header("Content-Encoding", "gzip")
    c.Header("Cache-Control", "public, max-age=86400")
    c.Header("X-Cache", "MISS")
    c.Header("Vary", "Accept-Encoding")
    c.Data(http.StatusOK, "application/vnd.mapbox-vector-tile", gz)
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

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Second)
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
