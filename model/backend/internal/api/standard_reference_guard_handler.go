package api

import (
	"net/http"
	"strconv"

	commoni18n "github.com/addp/common/middleware/i18n"
	modeli18n "github.com/addp/model/i18n"
	"github.com/addp/model/internal/models"
	"github.com/addp/model/internal/service"
	"github.com/gin-gonic/gin"
)

type StandardReferenceGuardHandler struct {
	svc *service.StandardReferenceGuardService
}

func NewStandardReferenceGuardHandler(svc *service.StandardReferenceGuardService) *StandardReferenceGuardHandler {
	return &StandardReferenceGuardHandler{svc: svc}
}

// SetState godoc
// @Summary 设置标准引用删除屏障 | Set Standard reference deletion guard
// @Description 由 Standard 在删除资源时冻结、释放或终止 Model 引用键，并在冻结响应中返回权威引用影响。
// @Tags StandardReference
// @Accept json
// @Produce json
// @Param resource_type path string true "标准资源类型 | Standard resource type" Enums(domain,element,metric)
// @Param resource_id path int true "标准资源 ID | Standard resource ID" minimum(1)
// @Param request body models.SetStandardReferenceGuardRequest true "目标屏障状态 | Desired guard state"
// @Success 200 {object} models.StandardReferenceGuardResponse
// @Failure 400 {object} models.ErrorResponse "请求无效 | Invalid request"
// @Failure 401 {object} models.ErrorResponse "未认证 | Authentication required"
// @Failure 403 {object} models.ErrorResponse "权限不足 | Permission denied"
// @Failure 404 {object} models.ErrorResponse "标准引用键不存在 | Standard reference key not found"
// @Failure 409 {object} models.ErrorResponse "屏障状态冲突 | Guard state conflict"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["model.standard_reference.update"]
// @Router /standard-reference-guards/{resource_type}/{resource_id} [put]
// @Security BearerAuth
func (h *StandardReferenceGuardHandler) SetState(c *gin.Context) {
	resourceID, err := strconv.ParseInt(c.Param("resource_id"), 10, 64)
	if err != nil || resourceID <= 0 {
		c.JSON(http.StatusBadRequest, errorResponseWithCode(commoni18n.T(c, modeli18n.MsgInvalidID), "invalid_id"))
		return
	}
	var req models.SetStandardReferenceGuardRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, invalidParamsResponse(c))
		return
	}
	response, err := h.svc.SetState(getTenantID(c), c.Param("resource_type"), resourceID, req.State)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, response)
}
