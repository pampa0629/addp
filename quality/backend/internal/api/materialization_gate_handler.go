package api

import (
	"net/http"

	commonAPI "github.com/addp/common/api"
	qualityi18n "github.com/addp/quality/i18n"
	"github.com/addp/quality/internal/service"
	"github.com/gin-gonic/gin"
)

type MaterializationGateHandler struct {
	service *service.MaterializationGateService
}

func NewMaterializationGateHandler(gateService *service.MaterializationGateService) *MaterializationGateHandler {
	return &MaterializationGateHandler{service: gateService}
}

type materializationGateDeleteRequest struct {
	Version int64 `json:"version"`
}

// @Summary 列出物化门禁任务 | List materialization gate tasks
// @Tags MaterializationGate
// @Produce json
// @Param page query int false "页码 | Page"
// @Param page_size query int false "每页数量 | Page size"
// @Success 200 {object} qualityMaterializationGateTaskListResponse
// @Failure 500 {object} qualityErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["quality.materialization_gate.read"]
// @Router /materialization-gate-tasks [get]
// @Security BearerAuth
func (h *MaterializationGateHandler) List(c *gin.Context) {
	page, pageSize := pageParams(c.Query("page"), c.Query("page_size"))
	items, total, err := h.service.List(c.Request.Context(), getTenantID(c), page, pageSize)
	if err != nil {
		respondQualityServiceError(c, err, "", qualityi18n.MsgInternal)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": items, "total": total, "page": page, "page_size": pageSize, "total_pages": totalPages(total, pageSize)})
}

// @Summary 创建物化门禁任务 | Create materialization gate task
// @Tags MaterializationGate
// @Accept json
// @Produce json
// @Param request body service.MaterializationGateWriteRequest true "物化门禁定义 | Materialization gate definition"
// @Success 201 {object} qualityMaterializationGateTaskResponse
// @Failure 400 {object} qualityErrorResponse
// @Failure 409 {object} qualityErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["quality.materialization_gate.create"]
// @Router /materialization-gate-tasks [post]
// @Security BearerAuth
func (h *MaterializationGateHandler) Create(c *gin.Context) {
	var request service.MaterializationGateWriteRequest
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

// @Summary 获取物化门禁任务 | Get materialization gate task
// @Tags MaterializationGate
// @Produce json
// @Param id path int true "任务 ID | Task ID"
// @Success 200 {object} qualityMaterializationGateTaskResponse
// @Failure 400 {object} qualityErrorResponse
// @Failure 404 {object} qualityErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["quality.materialization_gate.read"]
// @Router /materialization-gate-tasks/{id} [get]
// @Security BearerAuth
func (h *MaterializationGateHandler) Get(c *gin.Context) {
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

// @Summary 更新物化门禁任务 | Update materialization gate task
// @Tags MaterializationGate
// @Accept json
// @Produce json
// @Param id path int true "任务 ID | Task ID"
// @Param request body service.MaterializationGateWriteRequest true "完整物化门禁定义 | Complete materialization gate definition"
// @Success 200 {object} qualityMaterializationGateTaskResponse
// @Failure 400 {object} qualityErrorResponse
// @Failure 404 {object} qualityErrorResponse
// @Failure 409 {object} qualityErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["quality.materialization_gate.update"]
// @Router /materialization-gate-tasks/{id} [put]
// @Security BearerAuth
func (h *MaterializationGateHandler) Update(c *gin.Context) {
	id, err := requiredPositiveID(c.Param("id"))
	if err != nil {
		respondInvalidRequest(c, "")
		return
	}
	var request service.MaterializationGateWriteRequest
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

// @Summary 删除物化门禁任务 | Delete materialization gate task
// @Tags MaterializationGate
// @Accept json
// @Produce json
// @Param id path int true "任务 ID | Task ID"
// @Param request body materializationGateDeleteRequest true "删除版本 | Delete version"
// @Success 200 {object} qualityMessageResponse
// @Failure 400 {object} qualityErrorResponse
// @Failure 404 {object} qualityErrorResponse
// @Failure 409 {object} qualityErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["quality.materialization_gate.delete"]
// @Router /materialization-gate-tasks/{id} [delete]
// @Security BearerAuth
func (h *MaterializationGateHandler) Delete(c *gin.Context) {
	id, err := requiredPositiveID(c.Param("id"))
	if err != nil {
		respondInvalidRequest(c, "")
		return
	}
	var request materializationGateDeleteRequest
	if err := commonAPI.BindOptionalJSONStrict(c, &request); err != nil {
		respondInvalidRequest(c, err.Error())
		return
	}
	if err := h.service.Delete(c.Request.Context(), getTenantID(c), id, request.Version); err != nil {
		respondQualityServiceError(c, err, "", qualityi18n.MsgInternal)
		return
	}
	c.JSON(http.StatusOK, qualityMessageResponse{Message: "deleted"})
}
