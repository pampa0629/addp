package api

import (
	"net/http"

	commonAPI "github.com/addp/common/api"
	commonAuth "github.com/addp/common/middleware/auth"
	"github.com/addp/model/internal/service"
	"github.com/gin-gonic/gin"
)

type MaterializationReadContextHandler struct {
	materialization *service.MaterializationService
}

func NewMaterializationReadContextHandler(materialization *service.MaterializationService) *MaterializationReadContextHandler {
	return &MaterializationReadContextHandler{materialization: materialization}
}

type materializationReadContextRequest struct {
	ParentExecutionID string  `json:"parent_execution_id"`
	ReaderExecutionID string  `json:"reader_execution_id"`
	ReaderAttempt     int     `json:"reader_attempt"`
	ReaderLeaseToken  string  `json:"reader_lease_token"`
	LogicalTableIDs   []int64 `json:"logical_table_ids"`
}

// Resolve godoc
// @Summary 解析物化读上下文 | Resolve materialization read context
// @Description 仅为 Develop 或 Quality 当前 worker lease 返回同一父编排中已完成物化批次的只读 staging 定位、列和结构指纹；不返回凭据、授权、DDL 或写能力。| Return read-only staging locators, columns, and schema fingerprints for completed materialization batches in the same parent orchestration, only to the current Develop or Quality worker lease; credentials, authorization, DDL, and write capability are never returned.
// @Tags Materialization
// @Accept json
// @Produce json
// @Param request body materializationReadContextRequest true "解析请求 | Resolve request"
// @Success 200 {object} service.MaterializationReadContext
// @Failure 400 {object} map[string]interface{}
// @Failure 401 {object} map[string]interface{}
// @Failure 403 {object} map[string]interface{}
// @Failure 404 {object} map[string]interface{}
// @Failure 409 {object} map[string]interface{}
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["model.materialization_read.execute"]
// @Router /materialization-read-contexts [post]
// @Security BearerAuth
func (h *MaterializationReadContextHandler) Resolve(c *gin.Context) {
	var request materializationReadContextRequest
	if err := commonAPI.BindOptionalJSONStrict(c, &request); err != nil {
		c.JSON(http.StatusBadRequest, invalidParamsResponse(c))
		return
	}
	result, err := h.materialization.ResolveReadContext(
		c.Request.Context(), getTenantID(c), request.ParentExecutionID, request.ReaderExecutionID,
		request.ReaderAttempt, request.ReaderLeaseToken, request.LogicalTableIDs, commonAuth.GetClientID(c),
	)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}
