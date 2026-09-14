package api

import (
	"net/http"

	commonAPI "github.com/addp/common/api"
	qualityi18n "github.com/addp/quality/i18n"
	"github.com/addp/quality/internal/service"
	"github.com/gin-gonic/gin"
)

type DataValidationHandler struct {
	service *service.DataValidationService
}

func NewDataValidationHandler(gateService *service.DataValidationService) *DataValidationHandler {
	return &DataValidationHandler{service: gateService}
}

type dataValidationDeleteRequest struct {
	Version int64 `json:"version"`
}

// @Summary 列出数据校验任务 | List data validation tasks
// @Tags DataValidation
// @Produce json
// @Param page query int false "页码 | Page"
// @Param page_size query int false "每页数量 | Page size"
// @Success 200 {object} qualityDataValidationTaskListResponse
// @Failure 500 {object} qualityErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["quality.data_validation.read"]
// @Router /data-validation-tasks [get]
// @Security BearerAuth
func (h *DataValidationHandler) List(c *gin.Context) {
	page, pageSize := pageParams(c.Query("page"), c.Query("page_size"))
	items, total, err := h.service.List(c.Request.Context(), getTenantID(c), page, pageSize)
	if err != nil {
		respondQualityServiceError(c, err, "", qualityi18n.MsgInternal)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": items, "total": total, "page": page, "page_size": pageSize, "total_pages": totalPages(total, pageSize)})
}

// @Summary 创建数据校验任务 | Create data validation task
// @Tags DataValidation
// @Accept json
// @Produce json
// @Param request body service.DataValidationWriteRequest true "数据校验定义 | Data validation definition"
// @Success 201 {object} qualityDataValidationTaskResponse
// @Failure 400 {object} qualityErrorResponse
// @Failure 409 {object} qualityErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["quality.data_validation.create"]
// @Router /data-validation-tasks [post]
// @Security BearerAuth
func (h *DataValidationHandler) Create(c *gin.Context) {
	var request service.DataValidationWriteRequest
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

// @Summary 获取数据校验任务 | Get data validation task
// @Tags DataValidation
// @Produce json
// @Param id path int true "任务 ID | Task ID"
// @Success 200 {object} qualityDataValidationTaskResponse
// @Failure 400 {object} qualityErrorResponse
// @Failure 404 {object} qualityErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["quality.data_validation.read"]
// @Router /data-validation-tasks/{id} [get]
// @Security BearerAuth
func (h *DataValidationHandler) Get(c *gin.Context) {
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

// @Summary 更新数据校验任务 | Update data validation task
// @Tags DataValidation
// @Accept json
// @Produce json
// @Param id path int true "任务 ID | Task ID"
// @Param request body service.DataValidationWriteRequest true "完整数据校验定义 | Complete data validation definition"
// @Success 200 {object} qualityDataValidationTaskResponse
// @Failure 400 {object} qualityErrorResponse
// @Failure 404 {object} qualityErrorResponse
// @Failure 409 {object} qualityErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["quality.data_validation.update"]
// @Router /data-validation-tasks/{id} [put]
// @Security BearerAuth
func (h *DataValidationHandler) Update(c *gin.Context) {
	id, err := requiredPositiveID(c.Param("id"))
	if err != nil {
		respondInvalidRequest(c, "")
		return
	}
	var request service.DataValidationWriteRequest
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

// @Summary 删除数据校验任务 | Delete data validation task
// @Tags DataValidation
// @Accept json
// @Produce json
// @Param id path int true "任务 ID | Task ID"
// @Param request body dataValidationDeleteRequest true "删除版本 | Delete version"
// @Success 200 {object} qualityMessageResponse
// @Failure 400 {object} qualityErrorResponse
// @Failure 404 {object} qualityErrorResponse
// @Failure 409 {object} qualityErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["quality.data_validation.delete"]
// @Router /data-validation-tasks/{id} [delete]
// @Security BearerAuth
func (h *DataValidationHandler) Delete(c *gin.Context) {
	id, err := requiredPositiveID(c.Param("id"))
	if err != nil {
		respondInvalidRequest(c, "")
		return
	}
	var request dataValidationDeleteRequest
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
