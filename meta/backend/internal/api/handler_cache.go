package api

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

// ClearResourceCache 清除存储引擎缓存
// @Summary 清除引擎缓存 | Clear engine cache
// @Description 清除指定存储引擎缓存，engine_id 为 all 时清除全部缓存 | Clear storage engine cache
// @Tags Meta Cache
// @Produce json
// @Param engine_id path string true "存储引擎ID或all | Engine ID or all"
// @Success 200 {object} map[string]interface{} "清除结果 | Clear result"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Router /cache/engines/{engine_id} [delete]
// @Security BearerAuth
func (h *Handler) ClearResourceCache(c *gin.Context) {
	engineIDStr := c.Param("engine_id")
	if engineIDStr == "all" {
		h.engineService.ClearCache()
		c.JSON(http.StatusOK, gin.H{
			"message": "已清除所有存储引擎缓存",
		})
		return
	}

	engineID, err := strconv.ParseUint(engineIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid engine_id"})
		return
	}

	h.engineService.ClearEngineCache(uint(engineID))
	c.JSON(http.StatusOK, gin.H{
		"message": fmt.Sprintf("已清除存储引擎 %d 的缓存", engineID),
	})
}

// RefreshResourceCache 刷新存储引擎缓存（先清除再重新加载）
// @Summary 刷新存储引擎缓存 | Refresh storage engine cache
// @Description 清除并重新预加载存储引擎缓存 | Clear and preload storage engine cache
// @Tags Meta Cache
// @Produce json
// @Success 200 {object} map[string]interface{} "刷新结果 | Refresh result"
// @Failure 500 {object} map[string]interface{} "服务器内部错误 | Internal server error"
// @Router /cache/refresh [post]
// @Security BearerAuth
func (h *Handler) RefreshResourceCache(c *gin.Context) {
	h.engineService.ClearCache()
	if err := h.engineService.PreloadResources(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": fmt.Sprintf("刷新缓存失败: %v", err),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "存储引擎缓存已刷新",
	})
}
