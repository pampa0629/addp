package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	commonAPI "github.com/addp/common/api"
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
// @Success 200 {array} map[string]interface{}
// @Router /check-tasks [get]
// @Security BearerAuth
func (h *CheckTaskHandler) List(c *gin.Context) {
	tenantID := getTenantID(c)
	items, err := h.svc.List(tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, items)
}

// @Summary 获取检查任务详情 | Get check task detail
// @Tags CheckTask
// @Produce json
// @Param id path int true "任务ID | Task ID"
// @Success 200 {object} map[string]interface{}
// @Router /check-tasks/{id} [get]
// @Security BearerAuth
func (h *CheckTaskHandler) Get(c *gin.Context) {
	tenantID := getTenantID(c)
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	item, err := h.svc.Get(id, tenantID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, item)
}

// @Summary 创建检查任务 | Create check task
// @Tags CheckTask
// @Accept json
// @Produce json
// @Param body body map[string]interface{} true "任务信息 | Task info"
// @Success 201 {object} map[string]interface{}
// @Router /check-tasks [post]
// @Security BearerAuth
func (h *CheckTaskHandler) Create(c *gin.Context) {
	tenantID := getTenantID(c)
	userID := getUserID(c)
	var req service.CreateCheckTaskRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	item, err := h.svc.Create(tenantID, userID, &req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, item)
}

// @Summary 更新检查任务 | Update check task
// @Tags CheckTask
// @Accept json
// @Produce json
// @Param id path int true "任务ID | Task ID"
// @Param body body map[string]interface{} true "更新信息 | Update info"
// @Success 200 {object} map[string]interface{}
// @Router /check-tasks/{id} [put]
// @Security BearerAuth
func (h *CheckTaskHandler) Update(c *gin.Context) {
	tenantID := getTenantID(c)
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	var req service.UpdateCheckTaskRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	item, err := h.svc.Update(id, tenantID, &req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, item)
}

// @Summary 删除检查任务 | Delete check task
// @Tags CheckTask
// @Produce json
// @Param id path int true "任务ID | Task ID"
// @Success 200 {object} map[string]string
// @Router /check-tasks/{id} [delete]
// @Security BearerAuth
func (h *CheckTaskHandler) Delete(c *gin.Context) {
	tenantID := getTenantID(c)
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	if err := h.svc.Delete(id, tenantID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "deleted"})
}

// @Summary 执行检查任务 | Run check task
// @Tags CheckTask
// @Produce json
// @Param id path int true "任务ID | Task ID"
// @Success 202 {object} map[string]string
// @Failure 409 {object} map[string]string "任务已有活动 execution | Task already has an active execution"
// @Router /check-tasks/{id}/run [post]
// @Security BearerAuth
func (h *CheckTaskHandler) Run(c *gin.Context) {
	tenantID := getTenantID(c)
	userID := getUserID(c)
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	executionID, err := h.executor.RunCheck(context.Background(), id, tenantID, userID)
	if err != nil {
		respondCheckRunError(c, err)
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"execution_id": executionID, "message": "check started"})
}

func respondCheckRunError(c *gin.Context, err error) {
	if errors.Is(err, commonAPI.ErrConflict) {
		c.JSON(http.StatusConflict, gin.H{"error": commoni18n.T(c, qualityi18n.MsgCheckTaskActive)})
		return
	}
	c.JSON(http.StatusInternalServerError, gin.H{"error": commoni18n.T(c, qualityi18n.MsgCheckTaskRunFailed)})
}
