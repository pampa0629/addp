package api

import (
	"net/http"

	commonAPI "github.com/addp/common/api"
	"github.com/addp/model/internal/service"
	"github.com/gin-gonic/gin"
)

type MaterializationContextHandler struct {
	materialization *service.MaterializationService
}

func NewMaterializationContextHandler(materialization *service.MaterializationService) *MaterializationContextHandler {
	return &MaterializationContextHandler{materialization: materialization}
}

type materializationWriteContextRequest struct {
	ParentExecutionID string `json:"parent_execution_id"`
	LogicalTableID    int64  `json:"logical_table_id"`
}

// ResolveWriteContext godoc
// @Summary 解析物化写入上下文 | Resolve materialization write context
// @Description 为固定 Develop 服务主体解析同一父编排 execution 下唯一 prepared 批次的最小 staging 写入事实；不返回 DDL、凭据或最终目标。| Resolve the minimal staging write facts for the unique prepared batch under the same parent orchestration execution for the fixed Develop service principal; DDL, credentials, and final targets are never returned.
// @Tags Materialization
// @Accept json
// @Produce json
// @Param request body materializationWriteContextRequest true "解析请求 | Resolve request"
// @Success 200 {object} service.MaterializationWriteContext
// @Failure 400 {object} map[string]interface{}
// @Failure 401 {object} map[string]interface{}
// @Failure 403 {object} map[string]interface{}
// @Failure 404 {object} map[string]interface{}
// @Failure 409 {object} map[string]interface{}
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["model.materialization_context.read"]
// @Router /materialization-write-contexts/resolve [post]
// @Security BearerAuth
func (h *MaterializationContextHandler) ResolveWriteContext(c *gin.Context) {
	var request materializationWriteContextRequest
	if err := commonAPI.BindOptionalJSONStrict(c, &request); err != nil {
		c.JSON(http.StatusBadRequest, invalidParamsResponse(c))
		return
	}
	context, err := h.materialization.ResolveWriteContext(
		c.Request.Context(), getTenantID(c), request.LogicalTableID, request.ParentExecutionID,
	)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, context)
}
