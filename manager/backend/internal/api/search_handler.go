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
	searchService  *service.FullTextSearchService
	historyService *service.SearchHistoryService
}

func NewSearchHandler(searchService *service.FullTextSearchService, historyService *service.SearchHistoryService) *SearchHandler {
	return &SearchHandler{
		searchService:  searchService,
		historyService: historyService,
	}
}

func (h *SearchHandler) FullTextSearch(c *gin.Context) {
	if h.searchService == nil || !h.searchService.Enabled() {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "full text search not configured",
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
		if errors.Is(err, service.ErrFullTextSearchDisabled) {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
			return
		}
		logger.L().Error("全文检索失败", "error", err)
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
	logger.L().Info("全文搜索返回",
		"query", query,
		"total", result.Total,
		"results_count", len(result.Hits),
		"page", result.Page,
		"page_size", result.PageSize,
	)

	c.JSON(http.StatusOK, gin.H{"data": result})
}

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
