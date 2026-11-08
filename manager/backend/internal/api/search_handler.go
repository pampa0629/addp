package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/logger"
	"github.com/addp/common/middleware/auth"
	commoni18n "github.com/addp/common/middleware/i18n"
	manageri18n "github.com/addp/manager/i18n"
	"github.com/addp/manager/internal/service"
	"github.com/gin-gonic/gin"
)

type SearchHandler struct {
	searchService  *service.HybridSearchService
	historyService *service.SearchHistoryService
	visibility     service.SearchVisibilityChecker
}

func NewSearchHandler(searchService *service.HybridSearchService, historyService *service.SearchHistoryService, visibility service.SearchVisibilityChecker) *SearchHandler {
	return &SearchHandler{
		searchService:  searchService,
		historyService: historyService,
		visibility:     visibility,
	}
}

// Search executes the one sequential candidate/ranking continuation contract.
// @Summary 执行混合检索 | Execute hybrid search
// @Description 先召回候选并按当前权限筛选，再按完整RRF相关性顺序交付。不返回精确总数；空批带next_cursor仍可继续。依据变化使整个查询失效 | Retrieves candidates, applies current permissions, then delivers a proven prefix of complete RRF order. Returns no exact total; empty batches with next_cursor may continue. Basis changes invalidate the entire query
// @Tags Manager
// @Produce json
// @Param q query string true "固定查询词 | Fixed query"
// @Param engine_id query int false "固定引擎范围 | Fixed engine scope"
// @Param page_size query int false "固定批次大小，1至50，默认10 | Fixed batch size, 1-50, default 10"
// @Param cursor query string false "不透明续查游标；初次不传 | Opaque continuation cursor; omit initially"
// @Success 200 {object} service.SearchContinuationResult "当前交付批次 | Current delivery batch"
// @Failure 400,401,403,404,409,410,502,503,504 {object} map[string]interface{} "检索失败或状态失效 | Search failed or context invalidated"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["manager.search.execute"]
// @Router /search [get]
// @Security BearerAuth
func (h *SearchHandler) Search(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	current, ok := auth.AuthContextFromGin(c)
	if !ok || current.Principal.Type != "user" || current.Context.Type != "tenant" || (current.Token.Type != "first_party_access_token" && current.Token.Type != "oauth_access_token" && current.Token.Type != "delegated_access_token") {
		managerError(c, http.StatusForbidden, manageri18n.MsgUnauthorized)
		return
	}
	for key, values := range c.Request.URL.Query() {
		if len(values) != 1 || (key != "q" && key != "engine_id" && key != "page_size" && key != "cursor") {
			c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, manageri18n.MsgSearchParametersInvalid), "error_code": "search_parameters_invalid"})
			return
		}
	}
	input, err := service.ParseSearchContinuation(c.Query("q"), c.Query("page_size"), c.Query("cursor"))
	if err != nil {
		managerError(c, http.StatusBadRequest, manageri18n.MsgSearchParametersInvalid)
		return
	}
	var engineID *uint
	if raw := c.Query("engine_id"); raw != "" {
		parsed, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || parsed <= 0 {
			managerError(c, http.StatusBadRequest, manageri18n.MsgInvalidEngineIDParam)
			return
		}
		value := uint(parsed)
		engineID = &value
	}
	if h.searchService == nil || !h.searchService.Enabled() {
		managerError(c, http.StatusServiceUnavailable, manageri18n.MsgHybridSearchNotConfigured)
		return
	}
	result, err := h.searchService.SearchDocuments(c.Request.Context(), current, engineID, auth.CanonicalBearerToken(c.GetHeader("Authorization")), h.visibility, input)
	if err != nil {
		status, key, code := http.StatusServiceUnavailable, manageri18n.MsgHybridSearchFailed, "manager_search_failed"
		switch {
		case errors.Is(err, service.ErrSearchContextChanged):
			status, key, code = http.StatusConflict, manageri18n.MsgSearchContextChanged, "search_context_changed"
		case errors.Is(err, service.ErrSearchContinuationExpired):
			status, key, code = http.StatusGone, manageri18n.MsgSearchContinuationExpired, "search_continuation_expired"
		case errors.Is(err, service.ErrSearchContinuationNotFound):
			status, key, code = http.StatusNotFound, manageri18n.MsgSearchContinuationExpired, "search_continuation_not_found"
		case errors.Is(err, service.ErrSearchContinuationParameters):
			status, key, code = http.StatusBadRequest, manageri18n.MsgSearchParametersInvalid, "search_parameters_invalid"
		case errors.Is(err, service.ErrSearchCapacityExceeded):
			key, code = manageri18n.MsgSearchCapacityExceeded, "search_capacity_exceeded"
		case errors.Is(err, commonClient.ErrMetadataCredentialRejected):
			status, key, code = http.StatusUnauthorized, manageri18n.MsgUnauthorized, "unauthorized"
		case errors.Is(err, commonClient.ErrMetadataQueryDenied):
			status, key, code = http.StatusForbidden, manageri18n.MsgUnauthorized, "forbidden"
		case errors.Is(err, service.ErrSearchIsolated):
			key, code = manageri18n.MsgHybridSearchIsolated, "manager_search_isolated"
		case errors.Is(err, service.ErrSearchDisabled):
			key = manageri18n.MsgHybridSearchNotConfigured
		case errors.Is(err, context.DeadlineExceeded):
			status = http.StatusGatewayTimeout
		}
		logger.L().Error("混合检索失败", "error_type", fmt.Sprintf("%T", err))
		c.JSON(status, gin.H{"error": commoni18n.T(c, key), "error_code": code})
		return
	}
	if h.historyService != nil && c.Query("cursor") == "" {
		if userID, ok := userIDFromContext(c); ok {
			if err := h.historyService.Record(userID, tenantFilterIDFromContext(c), c.Query("q")); err != nil {
				logger.L().Warn("记录搜索历史失败", "user_id", userID)
			}
		}
	}
	logger.L().Info("混合检索返回", "engine_id", engineID, "results_count", len(result.Hits), "page_size", result.PageSize, "exhausted", result.Exhausted)
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
// @x-addp-auth-mode "self"
// @Router /search/history [get]
// @Security BearerAuth
func (h *SearchHandler) ListHistory(c *gin.Context) {
	if h.historyService == nil {
		managerError(c, http.StatusServiceUnavailable, manageri18n.MsgSearchHistoryUnavailable)
		return
	}

	userID, ok := userIDFromContext(c)
	if !ok {
		managerError(c, http.StatusUnauthorized, manageri18n.MsgUnauthorized)
		return
	}

	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "10"))
	if limit <= 0 {
		limit = 10
	}

	histories, err := h.historyService.List(userID, limit)
	if err != nil {
		logger.L().Error("查询搜索历史失败", "error", err, "user_id", userID)
		managerError(c, http.StatusInternalServerError, manageri18n.MsgLoadHistoryFailed)
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
// @x-addp-auth-mode "self"
// @Router /search/history/{id} [delete]
// @Security BearerAuth
func (h *SearchHandler) DeleteHistoryItem(c *gin.Context) {
	if h.historyService == nil {
		managerError(c, http.StatusServiceUnavailable, manageri18n.MsgSearchHistoryUnavailable)
		return
	}

	userID, ok := userIDFromContext(c)
	if !ok {
		managerError(c, http.StatusUnauthorized, manageri18n.MsgUnauthorized)
		return
	}

	rawID := c.Param("id")
	historyID64, err := strconv.ParseUint(strings.TrimSpace(rawID), 10, 64)
	if err != nil || historyID64 == 0 {
		managerError(c, http.StatusBadRequest, manageri18n.MsgInvalidHistoryID)
		return
	}

	if err := h.historyService.Delete(userID, uint(historyID64)); err != nil {
		logger.L().Error("删除搜索历史失败", "error", err, "user_id", userID, "history_id", historyID64)
		managerError(c, http.StatusInternalServerError, manageri18n.MsgDeleteHistoryFailed)
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
// @x-addp-auth-mode "self"
// @Router /search/history [delete]
// @Security BearerAuth
func (h *SearchHandler) ClearHistory(c *gin.Context) {
	if h.historyService == nil {
		managerError(c, http.StatusServiceUnavailable, manageri18n.MsgSearchHistoryUnavailable)
		return
	}

	userID, ok := userIDFromContext(c)
	if !ok {
		managerError(c, http.StatusUnauthorized, manageri18n.MsgUnauthorized)
		return
	}

	if err := h.historyService.Clear(userID); err != nil {
		logger.L().Error("清空搜索历史失败", "error", err, "user_id", userID)
		managerError(c, http.StatusInternalServerError, manageri18n.MsgClearHistoryFailed)
		return
	}

	c.Status(http.StatusNoContent)
}

func userIDFromContext(c *gin.Context) (uint, bool) {
	userID := userIDValue(c)
	return userID, userID > 0
}
