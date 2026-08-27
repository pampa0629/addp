package api

import (
	"fmt"
	"net/http"
	"strconv"

	commonAuth "github.com/addp/common/middleware/auth"
	metaErrors "github.com/addp/meta/internal/errors"
	metaModels "github.com/addp/meta/internal/models"
	"github.com/gin-gonic/gin"
)

// ListDataItemChanges 返回当前租户可重放的 DataItem 变化流。
// @Summary 拉取 DataItem 变化 | Pull DataItem changes
// @Description 按不透明游标顺序返回当前租户的 DataItem 创建、摘要更新和失效变化，仅供 Catalog 模块消费 | Return ordered DataItem creation, summary update, and missing changes for the current tenant; Catalog service only
// @Tags Meta
// @Produce json
// @Param after_cursor query string false "上次成功提交的不透明游标，空值表示从历史起点开始 | Opaque cursor committed by the consumer; empty starts from the beginning"
// @Param limit query int false "批大小，默认 200，最大 500 | Batch size, default 200 and maximum 500"
// @Success 200 {object} metaModels.DataItemChangesResponse "DataItem 变化批次 | DataItem change batch"
// @Failure 400 {object} map[string]interface{} "无效游标或批大小 | Invalid cursor or batch size"
// @Failure 401 {object} map[string]interface{} "未认证 | Unauthorized"
// @Failure 403 {object} map[string]interface{} "非 Catalog 服务或权限不足 | Catalog service or permission required"
// @Failure 500 {object} map[string]interface{} "服务器内部错误 | Internal server error"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["meta.catalog.read"]
// @Router /data-items/changes [get]
// @Security BearerAuth
func (h *Handler) ListDataItemChanges(c *gin.Context) {
	limit := 0
	if rawLimit := c.Query("limit"); rawLimit != "" {
		parsed, err := strconv.Atoi(rawLimit)
		if err != nil {
			h.handleServiceError(c, fmt.Errorf("%w: limit must be an integer", metaErrors.ErrInvalidChangeRequest))
			return
		}
		limit = parsed
	}
	result, err := h.dataItemChangeService.List(commonAuth.GetTenantID(c), c.Query("after_cursor"), limit)
	if err != nil {
		h.handleServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

var _ = metaModels.DataItemChangesResponse{}
