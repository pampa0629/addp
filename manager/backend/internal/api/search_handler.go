package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/addp/common/logger"
	auth "github.com/addp/common/middleware/auth"
	"github.com/addp/manager/internal/service"
	"github.com/gin-gonic/gin"
)

type SearchHandler struct {
	searchService  *service.HybridSearchService
	historyService *service.SearchHistoryService
}

func NewSearchHandler(searchService *service.HybridSearchService, historyService *service.SearchHistoryService) *SearchHandler {
	return &SearchHandler{
		searchService:  searchService,
		historyService: historyService,
	}
}

// Search 执行混合检索（全文检索 + 向量语义检索）
// @Summary 执行混合检索 | Execute hybrid search
// @Description 执行全文检索与向量语义检索的混合搜索 | Execute hybrid search combining full-text and vector semantic search
// @Tags Manager
// @Produce json
// @Param q query string true "搜索关键词 | Search query"
// @Param page query int false "页码，默认1 | Page number, default 1"
// @Param page_size query int false "每页数量，默认10 | Page size, default 10"
// @Success 200 {object} map[string]interface{} "搜索结果 | Search results"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 503 {object} map[string]interface{} "搜索服务不可用 | Search service unavailable"
// @Router /search [get]
// @Security BearerAuth
func (h *SearchHandler) Search(c *gin.Context) {
	if h.searchService == nil || !h.searchService.Enabled() {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "hybrid search not configured",
		})
		return
	}

	query := strings.TrimSpace(c.Query("q"))
	if query == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing query"})
		return
	}

	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "10"))

	tenantID := tenantIDFromContext(c)
	result, err := h.searchService.SearchDocuments(c.Request.Context(), tenantID, query, page, pageSize)
	if err != nil {
		if errors.Is(err, service.ErrSearchDisabled) {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
			return
		}
		logger.L().Error("混合检索失败", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if h.historyService != nil {
		if userID, ok := userIDFromContext(c); ok {
			if err := h.historyService.Record(userID, tenantID, query); err != nil {
				logger.L().Warn("记录搜索历史失败", "error", err, "user_id", userID)
			}
		}
	}

	// 调试日志
	logger.L().Info("混合检索返回",
		"query", query,
		"total", result.Total,
		"results_count", len(result.Hits),
		"page", result.Page,
		"page_size", result.PageSize,
	)

	c.JSON(http.StatusOK, gin.H{"data": result})
}

// @Summary 列出搜索历史 | List search history
// @Description 获取当前用户的搜索历史记录 | Get search history for the current user
// @Tags Manager
// @Produce json
// @Param limit query int false "返回数量限制，默认10 | Result limit, default 10"
// @Success 200 {object} map[string]interface{} "搜索历史列表 | Search history list"
// @Failure 401 {object} map[string]interface{} "未授权 | Unauthorized"
// @Failure 503 {object} map[string]interface{} "服务不可用 | Service unavailable"
// @Router /listhistory [get]
// @Security BearerAuth
func (h *SearchHandler) ListHistory(c *gin.Context) {
	if h.historyService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "search history is not available"})
		return
	}

	userID, ok := userIDFromContext(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "10"))
	if limit <= 0 {
		limit = 10
	}

	histories, err := h.historyService.List(userID, limit)
	if err != nil {
		logger.L().Error("查询搜索历史失败", "error", err, "user_id", userID)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load history"})
		return
	}

	items := make([]gin.H, 0, len(histories))
	for _, history := range histories {
		items = append(items, gin.H{
			"id":         history.ID,
			"query":      history.Query,
			"created_at": history.CreatedAt,
			"updated_at": history.UpdatedAt,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"data": gin.H{
			"items": items,
		},
	})
}

// @Summary 删除搜索历史记录 | Delete search history item
// @Description 删除指定的搜索历史记录 | Delete a specific search history item
// @Tags Manager
// @Produce json
// @Param id path int true "历史记录ID | History item ID"
// @Success 204 "删除成功 | Deleted successfully"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 401 {object} map[string]interface{} "未授权 | Unauthorized"
// @Router /deletehistoryitem [delete]
// @Security BearerAuth
func (h *SearchHandler) DeleteHistoryItem(c *gin.Context) {
	if h.historyService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "search history is not available"})
		return
	}

	userID, ok := userIDFromContext(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	rawID := c.Param("id")
	historyID64, err := strconv.ParseUint(strings.TrimSpace(rawID), 10, 64)
	if err != nil || historyID64 == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid history id"})
		return
	}

	if err := h.historyService.Delete(userID, uint(historyID64)); err != nil {
		logger.L().Error("删除搜索历史失败", "error", err, "user_id", userID, "history_id", historyID64)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete history"})
		return
	}

	c.Status(http.StatusNoContent)
}

// @Summary 清空搜索历史 | Clear search history
// @Description 清空当前用户的所有搜索历史记录 | Clear all search history for the current user
// @Tags Manager
// @Produce json
// @Success 204 "清空成功 | Cleared successfully"
// @Failure 401 {object} map[string]interface{} "未授权 | Unauthorized"
// @Failure 503 {object} map[string]interface{} "服务不可用 | Service unavailable"
// @Router /clearhistory [delete]
// @Security BearerAuth
func (h *SearchHandler) ClearHistory(c *gin.Context) {
	if h.historyService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "search history is not available"})
		return
	}

	userID, ok := userIDFromContext(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	if err := h.historyService.Clear(userID); err != nil {
		logger.L().Error("清空搜索历史失败", "error", err, "user_id", userID)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to clear history"})
		return
	}

	c.Status(http.StatusNoContent)
}

func userIDFromContext(c *gin.Context) (uint, bool) {
	value, exists := c.Get(auth.ContextUserIDKey)
	if !exists {
		return 0, false
	}

	switch v := value.(type) {
	case uint:
		if v == 0 {
			return 0, false
		}
		return v, true
	case int:
		if v <= 0 {
			return 0, false
		}
		return uint(v), true
	default:
		return 0, false
	}
}
