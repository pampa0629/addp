package api

import (
	commonExecution "github.com/addp/common/execution"
	commoni18n "github.com/addp/common/middleware/i18n"
	"net/http"
	"strings"

	commonAPI "github.com/addp/common/api"
	qualityi18n "github.com/addp/quality/i18n"
	"github.com/addp/quality/internal/service"
	"github.com/gin-gonic/gin"
)

type PlanHandler struct {
	service *service.PlanService
}

func NewPlanHandler(planService *service.PlanService) *PlanHandler {
	return &PlanHandler{service: planService}
}

type planDeleteRequest struct {
	Version int64 `json:"version"`
}

// @Summary 列出质量检查方案 | List quality plan tasks
// @Tags Plan
// @Produce json
// @Param page query int false "页码 | Page"
// @Param page_size query int false "每页数量 | Page size"
// @Success 200 {object} qualityPlanListResponse
// @Failure 500 {object} qualityErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["quality.plan.read"]
// @Router /plans [get]
// @Security BearerAuth
func (h *PlanHandler) List(c *gin.Context) {
	page, pageSize := pageParams(c.Query("page"), c.Query("page_size"))
	items, total, err := h.service.List(c.Request.Context(), getTenantID(c), page, pageSize)
	if err != nil {
		respondQualityServiceError(c, err, "", qualityi18n.MsgInternal)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": items, "total": total, "page": page, "page_size": pageSize, "total_pages": totalPages(total, pageSize)})
}

// @Summary 创建质量检查方案 | Create quality plan task
// @Tags Plan
// @Accept json
// @Produce json
// @Param request body service.PlanWriteRequest true "质量检查定义 | Quality plan definition"
// @Success 201 {object} qualityPlanResponse
// @Failure 400 {object} qualityErrorResponse
// @Failure 409 {object} qualityErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["quality.plan.create","quality.rule.read"]
// @Router /plans [post]
// @Security BearerAuth
func (h *PlanHandler) Create(c *gin.Context) {
	var request service.PlanWriteRequest
	if err := commonAPI.BindOptionalJSONStrict(c, &request); err != nil {
		respondInvalidRequest(c, err.Error())
		return
	}
	result, err := h.service.Create(c.Request.Context(), getTenantID(c), getUserID(c), request)
	if err != nil {
		respondQualityServiceError(c, err, "", qualityi18n.MsgInternal)
		return
	}
	c.JSON(http.StatusCreated, result)
}

// @Summary 获取质量检查方案 | Get quality plan task
// @Tags Plan
// @Produce json
// @Param id path int true "任务 ID | Task ID"
// @Success 200 {object} qualityPlanResponse
// @Failure 400 {object} qualityErrorResponse
// @Failure 404 {object} qualityErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["quality.plan.read"]
// @Router /plans/{id} [get]
// @Security BearerAuth
func (h *PlanHandler) Get(c *gin.Context) {
	id, err := requiredPositiveID(c.Param("id"))
	if err != nil {
		respondInvalidRequest(c, "")
		return
	}
	result, err := h.service.Get(c.Request.Context(), getTenantID(c), id)
	if err != nil {
		respondQualityServiceError(c, err, "", qualityi18n.MsgInternal)
		return
	}
	c.JSON(http.StatusOK, result)
}

// @Summary 更新质量检查方案 | Update quality plan task
// @Tags Plan
// @Accept json
// @Produce json
// @Param id path int true "任务 ID | Task ID"
// @Param request body service.PlanWriteRequest true "完整质量检查定义 | Complete quality plan definition"
// @Success 200 {object} qualityPlanResponse
// @Failure 400 {object} qualityErrorResponse
// @Failure 404 {object} qualityErrorResponse
// @Failure 409 {object} qualityErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["quality.plan.update","quality.rule.read"]
// @Router /plans/{id} [put]
// @Security BearerAuth
func (h *PlanHandler) Update(c *gin.Context) {
	id, err := requiredPositiveID(c.Param("id"))
	if err != nil {
		respondInvalidRequest(c, "")
		return
	}
	var request service.PlanWriteRequest
	if err := commonAPI.BindOptionalJSONStrict(c, &request); err != nil {
		respondInvalidRequest(c, err.Error())
		return
	}
	result, err := h.service.Update(c.Request.Context(), getTenantID(c), getUserID(c), id, request)
	if err != nil {
		respondQualityServiceError(c, err, "", qualityi18n.MsgInternal)
		return
	}
	c.JSON(http.StatusOK, result)
}

// @Summary 删除质量检查方案 | Delete quality plan task
// @Tags Plan
// @Accept json
// @Produce json
// @Param id path int true "任务 ID | Task ID"
// @Param request body planDeleteRequest true "删除版本 | Delete version"
// @Success 200 {object} qualityMessageResponse
// @Failure 400 {object} qualityErrorResponse
// @Failure 404 {object} qualityErrorResponse
// @Failure 409 {object} qualityErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["quality.plan.delete"]
// @Router /plans/{id} [delete]
// @Security BearerAuth
func (h *PlanHandler) Delete(c *gin.Context) {
	id, err := requiredPositiveID(c.Param("id"))
	if err != nil {
		respondInvalidRequest(c, "")
		return
	}
	var request planDeleteRequest
	if err := commonAPI.BindOptionalJSONStrict(c, &request); err != nil {
		respondInvalidRequest(c, err.Error())
		return
	}
	if err := h.service.Delete(c.Request.Context(), getTenantID(c), id, request.Version); err != nil {
		respondQualityServiceError(c, err, "", qualityi18n.MsgInternal)
		return
	}
	c.JSON(http.StatusOK, qualityMessageResponse{Message: commoni18n.T(c, qualityi18n.MsgDeleted)})
}

// @Summary 执行质量检查方案 | Run quality plan
// @Tags QualityPlan
// @Produce json
// @Param id path int true "方案 ID | Plan ID"
// @Success 202 {object} qualityTaskProviderExecuteResponse
// @Failure 400 {object} qualityErrorResponse
// @Failure 404 {object} qualityErrorResponse
// @Failure 409 {object} qualityErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["quality.plan.execute"]
// @Router /plans/{id}/run [post]
// @Security BearerAuth
func (h *PlanHandler) Run(c *gin.Context) {
	id, err := requiredPositiveID(c.Param("id"))
	if err != nil {
		respondInvalidRequest(c, "")
		return
	}
	var request struct{}
	if err := commonAPI.BindOptionalJSONStrict(c, &request); err != nil {
		respondInvalidRequest(c, "")
		return
	}
	token := strings.TrimSpace(strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer "))
	executionID, err := h.service.Run(c.Request.Context(), getTenantID(c), id, getUserID(c), token)
	if err != nil {
		respondQualityServiceError(c, err, qualityi18n.MsgPlanNotFound, qualityi18n.MsgInternal)
		return
	}
	c.JSON(http.StatusAccepted, qualityTaskProviderExecuteResponse{ExecutionID: executionID, Status: commonExecution.ExecutionStatusPending})
}
