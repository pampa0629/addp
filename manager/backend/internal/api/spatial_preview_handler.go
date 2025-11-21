package api

import (
	"net/http"
	"strconv"

	"github.com/addp/manager/internal/service"
	"github.com/gin-gonic/gin"
)

// SpatialPreviewHandler MVT 空间预览 HTTP 处理器
type SpatialPreviewHandler struct {
	spatialService *service.SpatialPreviewService
}

// NewSpatialPreviewHandler 创建空间预览处理器
func NewSpatialPreviewHandler(spatialService *service.SpatialPreviewService) *SpatialPreviewHandler {
	return &SpatialPreviewHandler{
		spatialService: spatialService,
	}
}

// GetTileMetadata 获取瓦片元数据
// GET /api/manager/spatial/:fingerprint/metadata
func (h *SpatialPreviewHandler) GetTileMetadata(c *gin.Context) {
	fingerprint := c.Param("fingerprint")
	if fingerprint == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "fingerprint is required"})
		return
	}

	metadata, err := h.spatialService.GetTileMetadata(c.Request.Context(), fingerprint)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, metadata)
}

// GetTile 获取MVT瓦片
// GET /api/manager/spatial/:fingerprint/tiles/:z/:x/:y.mvt
func (h *SpatialPreviewHandler) GetTile(c *gin.Context) {
	fingerprint := c.Param("fingerprint")
	zStr := c.Param("z")
	xStr := c.Param("x")
	yStr := c.Param("y")

	z, err := strconv.Atoi(zStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid z parameter"})
		return
	}

	x, err := strconv.Atoi(xStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid x parameter"})
		return
	}

	y, err := strconv.Atoi(yStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid y parameter"})
		return
	}

	// 获取瓦片数据
	tileData, err := h.spatialService.GetTile(c.Request.Context(), fingerprint, z, x, y)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "tile not found"})
		return
	}

	// 返回 MVT 数据（gzip 压缩）
	c.Header("Content-Type", "application/vnd.mapbox-vector-tile")
	c.Header("Content-Encoding", "gzip")
	c.Header("Cache-Control", "public, max-age=86400") // 缓存 1 天
	c.Data(http.StatusOK, "application/vnd.mapbox-vector-tile", tileData)
}

// CheckTileExists 检查瓦片是否存在
// HEAD /api/manager/spatial/:fingerprint/tiles/:z/:x/:y.mvt
func (h *SpatialPreviewHandler) CheckTileExists(c *gin.Context) {
	fingerprint := c.Param("fingerprint")
	zStr := c.Param("z")
	xStr := c.Param("x")
	yStr := c.Param("y")

	z, err := strconv.Atoi(zStr)
	if err != nil {
		c.Status(http.StatusBadRequest)
		return
	}

	x, err := strconv.Atoi(xStr)
	if err != nil {
		c.Status(http.StatusBadRequest)
		return
	}

	y, err := strconv.Atoi(yStr)
	if err != nil {
		c.Status(http.StatusBadRequest)
		return
	}

	exists, err := h.spatialService.CheckTileExists(c.Request.Context(), fingerprint, z, x, y)
	if err != nil || !exists {
		c.Status(http.StatusNotFound)
		return
	}

	c.Status(http.StatusOK)
}
