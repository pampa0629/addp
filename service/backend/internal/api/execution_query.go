package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	commonapi "github.com/addp/common/api"
	commoni18n "github.com/addp/common/middleware/i18n"
	servicei18n "github.com/addp/service/i18n"
	"github.com/addp/service/internal/models"
	svc "github.com/addp/service/internal/service"
	"github.com/gin-gonic/gin"
)

// PreviewExecutionQuery previews a compiled metric query without executing it.
// @Summary 查看指标执行查询 | Preview metric execution query
// @Description 按当前请求及绑定修订编译只读查询文本和参数，不执行数据库查询；停用服务也可查看 | Compile query text and parameters for the current request and bound revision without database execution; inactive services may also be inspected
// @Tags QueryService
// @Accept json
// @Produce json
// @Param id path int true "服务 ID | Service ID"
// @Param request body models.QueryExecutionRequest true "当前测试条件 | Current test request"
// @Success 200 {object} models.ExecutionQueryPreview
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Failure 403 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 500 {object} map[string]string
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["service.definition.read"]
// @Router /query/{id}/execution-query [post]
// @Security BearerAuth
func (h *QueryServiceHandler) PreviewExecutionQuery(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	tenantID := tenantIDValue(c)
	if tenantID == 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": commoni18n.T(c, commoni18n.MsgUnauthorized)})
		return
	}
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	var request models.QueryExecutionRequest
	decoder := json.NewDecoder(c.Request.Body)
	decoder.UseNumber()
	decoder.DisallowUnknownFields()
	if err != nil || id == 0 || decoder.Decode(&request) != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, servicei18n.MsgInvalidQueryRequest), "error_code": "invalid_query_request"})
		return
	}
	var trailing interface{}
	if decoder.Decode(&trailing) != io.EOF {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, servicei18n.MsgInvalidQueryRequest), "error_code": "invalid_query_request"})
		return
	}
	item, err := h.svc.GetExecutionQueryService(uint(id), tenantID)
	if err == nil {
		var result *models.ExecutionQueryPreview
		result, err = h.executorSvc.PreviewExecutionQuery(c.Request.Context(), item, &request)
		if err == nil {
			c.JSON(http.StatusOK, result)
			return
		}
	}
	status, code, key := http.StatusInternalServerError, "execution_query_failed", servicei18n.MsgExecutionQueryFailed
	if errors.Is(err, commonapi.ErrNotFound) {
		status, code, key = http.StatusNotFound, "service_not_found", servicei18n.MsgServiceNotFound
	} else if errors.Is(err, svc.ErrInvalidStructuredQuery) || errors.Is(err, svc.ErrInvalidQueryCursor) {
		status, code, key = http.StatusBadRequest, "invalid_structured_query", servicei18n.MsgInvalidStructuredQuery
	}
	c.JSON(status, gin.H{"error": commoni18n.T(c, key), "error_code": code})
}
