package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	commonAPI "github.com/addp/common/api"
	commonExecution "github.com/addp/common/execution"
	commoni18n "github.com/addp/common/middleware/i18n"
	qualityi18n "github.com/addp/quality/i18n"
	"github.com/addp/quality/internal/service"
	"github.com/gin-gonic/gin"
)

type CheckTaskHandler struct {
	svc      *service.CheckTaskService
	executor *service.CheckExecutor
}

func NewCheckTaskHandler(svc *service.CheckTaskService, executor *service.CheckExecutor) *CheckTaskHandler {
	return &CheckTaskHandler{svc: svc, executor: executor}
}

// @Summary 获取检查任务列表 | List check tasks
// @Tags CheckTask
// @Produce json
// @Param page query int false "页码 | Page" default(1)
// @Param page_size query int false "每页数量 | Page size" default(20) maximum(100)
// @Success 200 {object} qualityCheckTaskListResponse
// @Failure 500 {object} qualityErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["quality.check_task.read"]
// @Router /check-tasks [get]
// @Security BearerAuth
func (h *CheckTaskHandler) List(c *gin.Context) {
	tenantID := getTenantID(c)
	page, pageSize := pageParams(c.Query("page"), c.Query("page_size"))
	items, total, err := h.svc.List(tenantID, page, pageSize)
	if err != nil {
		respondQualityServiceError(c, err, "", qualityi18n.MsgInternal)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": items, "total": total, "page": page, "page_size": pageSize, "total_pages": totalPages(total, pageSize)})
}

// @Summary 获取检查任务详情 | Get check task detail
// @Tags CheckTask
// @Produce json
// @Param id path int true "任务ID | Task ID"
// @Success 200 {object} qualityCheckTaskResponse
// @Failure 400 {object} qualityErrorResponse
// @Failure 404 {object} qualityErrorResponse
// @Failure 500 {object} qualityErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["quality.check_task.read"]
// @Router /check-tasks/{id} [get]
// @Security BearerAuth
func (h *CheckTaskHandler) Get(c *gin.Context) {
	tenantID := getTenantID(c)
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		respondInvalidRequest(c, "")
		return
	}
	item, err := h.svc.Get(id, tenantID)
	if err != nil {
		respondQualityServiceError(c, err, qualityi18n.MsgCheckTaskNotFound, qualityi18n.MsgInternal)
		return
	}
	c.JSON(http.StatusOK, item)
}

// @Summary 创建检查任务 | Create check task
// @Tags CheckTask
// @Accept json
// @Produce json
// @Param body body service.CreateCheckTaskRequest true "任务信息 | Task info"
// @Success 201 {object} qualityCheckTaskResponse
// @Failure 400 {object} qualityErrorResponse
// @Failure 409 {object} qualityErrorResponse
// @Failure 500 {object} qualityErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["quality.check_task.create"]
// @Router /check-tasks [post]
// @Security BearerAuth
func (h *CheckTaskHandler) Create(c *gin.Context) {
	tenantID := getTenantID(c)
	userID := getUserID(c)
	var req service.CreateCheckTaskRequest
	if err := commonAPI.BindOptionalJSONStrict(c, &req); err != nil {
		respondInvalidRequest(c, err.Error())
		return
	}
	item, err := h.svc.Create(c.Request.Context(), tenantID, userID, &req)
	if err != nil {
		respondQualityServiceError(c, err, qualityi18n.MsgCheckTaskNotFound, qualityi18n.MsgCheckTaskCreateFailed)
		return
	}
	c.JSON(http.StatusCreated, item)
}

// @Summary 更新检查任务 | Update check task
// @Tags CheckTask
// @Accept json
// @Produce json
// @Param id path int true "任务ID | Task ID"
// @Param body body service.UpdateCheckTaskRequest true "更新信息 | Update info"
// @Success 200 {object} qualityCheckTaskResponse
// @Failure 400 {object} qualityErrorResponse
// @Failure 404 {object} qualityErrorResponse
// @Failure 409 {object} qualityErrorResponse
// @Failure 500 {object} qualityErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["quality.check_task.update"]
// @Router /check-tasks/{id} [put]
// @Security BearerAuth
func (h *CheckTaskHandler) Update(c *gin.Context) {
	tenantID := getTenantID(c)
	userID := getUserID(c)
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		respondInvalidRequest(c, "")
		return
	}
	var req service.UpdateCheckTaskRequest
	if err := commonAPI.BindOptionalJSONStrict(c, &req); err != nil {
		respondInvalidRequest(c, err.Error())
		return
	}
	item, err := h.svc.Update(c.Request.Context(), id, tenantID, userID, &req)
	if err != nil {
		respondQualityServiceError(c, err, qualityi18n.MsgCheckTaskNotFound, qualityi18n.MsgCheckTaskUpdateFailed)
		return
	}
	c.JSON(http.StatusOK, item)
}

// @Summary 删除检查任务 | Delete check task
// @Tags CheckTask
// @Produce json
// @Param id path int true "任务ID | Task ID"
// @Success 200 {object} qualityMessageResponse
// @Failure 404 {object} qualityErrorResponse
// @Failure 409 {object} qualityErrorResponse
// @Failure 500 {object} qualityErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["quality.check_task.delete"]
// @Router /check-tasks/{id} [delete]
// @Security BearerAuth
func (h *CheckTaskHandler) Delete(c *gin.Context) {
	tenantID := getTenantID(c)
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		respondInvalidRequest(c, "")
		return
	}
	if err := h.svc.Delete(id, tenantID); err != nil {
		respondQualityServiceError(c, err, qualityi18n.MsgCheckTaskNotFound, qualityi18n.MsgCheckTaskDeleteFailed)
		return
	}
	c.JSON(http.StatusOK, qualityMessageResponse{Message: commoni18n.T(c, qualityi18n.MsgDeleted)})
}

// @Summary 执行检查任务 | Run check task
// @Tags CheckTask
// @Produce json
// @Param id path int true "任务ID | Task ID"
// @Success 202 {object} qualityTaskProviderExecuteResponse
// @Failure 400 {object} qualityErrorResponse
// @Failure 409 {object} qualityErrorResponse "任务已有活动 execution | Task already has an active execution"
// @Failure 500 {object} qualityErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["quality.check_task.execute"]
// @Router /check-tasks/{id}/run [post]
// @Security BearerAuth
func (h *CheckTaskHandler) Run(c *gin.Context) {
	tenantID := getTenantID(c)
	userID := getUserID(c)
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		respondInvalidRequest(c, "")
		return
	}
	executionID, err := h.executor.RunCheck(c.Request.Context(), id, tenantID, userID, bearerToken(c))
	if err != nil {
		respondCheckRunError(c, err)
		return
	}
	c.JSON(http.StatusAccepted, qualityTaskProviderExecuteResponse{ExecutionID: executionID, Status: commonExecution.ExecutionStatusPending})
}

func bearerToken(c *gin.Context) string {
	value := strings.TrimSpace(c.GetHeader("Authorization"))
	if len(value) >= 7 && strings.EqualFold(value[:7], "Bearer ") {
		return strings.TrimSpace(value[7:])
	}
	return ""
}

func respondCheckRunError(c *gin.Context, err error) {
	if errors.Is(err, commonAPI.ErrConflict) {
		respondQualityError(c, http.StatusConflict, "execution_already_active", commoni18n.T(c, qualityi18n.MsgCheckTaskActive))
		return
	}
	if errors.Is(err, commonAPI.ErrBadRequest) {
		respondInvalidRequest(c, "")
		return
	}
	if errors.Is(err, commonAPI.ErrNotFound) {
		respondQualityError(c, http.StatusNotFound, "check_task_not_found", commoni18n.T(c, qualityi18n.MsgCheckTaskNotFound))
		return
	}
	respondQualityError(c, http.StatusInternalServerError, "execution_start_failed", commoni18n.T(c, qualityi18n.MsgCheckTaskRunFailed))
}
