package api

import (
	"context"
	"errors"
	"mime/multipart"
	"net/http"
	"strconv"

	commonAPI "github.com/addp/common/api"
	commonExecution "github.com/addp/common/execution"
	commonMiddleware "github.com/addp/common/middleware/auth"
	commoni18n "github.com/addp/common/middleware/i18n"
	graphi18n "github.com/addp/graph/i18n"
	"github.com/addp/graph/internal/models"
	"github.com/addp/graph/internal/repository"
	"github.com/addp/graph/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
)

func graphBuildUserContext(c *gin.Context) context.Context {
	ctx := c.Request.Context()
	if facts, ok := commonMiddleware.AuthContextFromGin(c); ok {
		return commonExecution.WithUserOrigin(ctx, facts)
	}
	return ctx
}

type BuildHandler struct {
	buildSvc *service.BuildService
}

func NewBuildHandler(buildSvc *service.BuildService) *BuildHandler {
	return &BuildHandler{buildSvc: buildSvc}
}

// ——— 任务管理 ———

// ListTasks godoc
// @Summary      构建任务列表 | List build tasks
// @Description  获取知识图谱的所有构建任务 | Get all build tasks for a knowledge graph
// @Tags         图谱构建 | Graph Build
// @Produce      json
// @Security     BearerAuth
// @Param        id path int true "知识图谱 ID | Knowledge graph ID"
// @Success      200 {array}  models.BuildTask
// @Failure      500 {object} models.ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["graph.build_task.read"]
// @Router       /graphs/{id}/build/tasks [get]
func (h *BuildHandler) ListTasks(c *gin.Context) {
	graphID, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	tenantID := commonMiddleware.GetTenantID(c)

	tasks, err := h.buildSvc.ListTasks(uint(graphID), uint(tenantID))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, tasks)
}

// CreateTask godoc
// @Summary      创建构建任务 | Create build task
// @Description  创建新的知识图谱构建任务 | Create a new knowledge graph build task
// @Tags         图谱构建 | Graph Build
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id path int true "知识图谱 ID | Knowledge graph ID"
// @Success      201 {object} models.BuildTask
// @Failure      400 {object} models.ErrorResponse
// @Failure      404 {object} models.ErrorResponse
// @Failure      409 {object} models.ErrorResponse
// @Failure      500 {object} models.ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["graph.build_task.create"]
// @Router       /graphs/{id}/build/tasks [post]
func (h *BuildHandler) CreateTask(c *gin.Context) {
	graphID, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	tenantID := commonMiddleware.GetTenantID(c)

	var req struct {
		Name                string  `json:"name" binding:"required"`
		Description         string  `json:"description"`
		ConfidenceThreshold float64 `json:"confidence_threshold"`
		ChunkSize           int     `json:"chunk_size"`
		ChunkOverlap        int     `json:"chunk_overlap"`
		DocContextSize      int     `json:"doc_context_size"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, graphi18n.MsgInvalidRequest)})
		return
	}

	task := &models.BuildTask{
		TenantID:            uint(tenantID),
		GraphID:             uint(graphID),
		Name:                req.Name,
		Description:         req.Description,
		ConfidenceThreshold: req.ConfidenceThreshold,
		ChunkSize:           req.ChunkSize,
		ChunkOverlap:        req.ChunkOverlap,
		DocContextSize:      req.DocContextSize,
		Status:              models.BuildStatusPending,
	}
	if task.ConfidenceThreshold == 0 {
		task.ConfidenceThreshold = 0.7
	}
	if task.ChunkSize == 0 {
		task.ChunkSize = 1000
	}
	if task.ChunkOverlap == 0 {
		task.ChunkOverlap = 200
	}
	if task.DocContextSize == 0 {
		task.DocContextSize = 200
	}

	if err := h.buildSvc.CreateTask(task); err != nil {
		graphOperationError(c, err)
		return
	}
	c.JSON(http.StatusCreated, task)
}

// GetTask godoc
// @Summary      获取构建任务 | Get build task
// @Tags         图谱构建 | Graph Build
// @Produce      json
// @Security     BearerAuth
// @Param        id  path int true "知识图谱 ID | Knowledge graph ID"
// @Param        tid path int true "任务 ID | Task ID"
// @Success      200 {object} models.BuildTask
// @Failure      404 {object} models.ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["graph.build_task.read"]
// @Router       /graphs/{id}/build/tasks/{tid} [get]
func (h *BuildHandler) GetTask(c *gin.Context) {
	tenantID := commonMiddleware.GetTenantID(c)
	tid, _ := strconv.ParseUint(c.Param("tid"), 10, 64)

	task, err := h.buildSvc.GetTask(uint(tid), uint(tenantID))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "任务不存在"})
		return
	}
	c.JSON(http.StatusOK, task)
}

// DeleteTask godoc
// @Summary      删除构建任务 | Delete build task
// @Tags         图谱构建 | Graph Build
// @Produce      json
// @Security     BearerAuth
// @Param        id  path int true "知识图谱 ID | Knowledge graph ID"
// @Param        tid path int true "任务 ID | Task ID"
// @Success      200 {object} models.SuccessResponse
// @Failure      404 {object} models.ErrorResponse
// @Failure      409 {object} models.ErrorResponse
// @Failure      500 {object} models.ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["graph.build_task.delete"]
// @Router       /graphs/{id}/build/tasks/{tid} [delete]
func (h *BuildHandler) DeleteTask(c *gin.Context) {
	tenantID := commonMiddleware.GetTenantID(c)
	tid, _ := strconv.ParseUint(c.Param("tid"), 10, 64)

	if err := h.buildSvc.DeleteTask(c.Request.Context(), uint(tid), parseUintParam(c, "id"), uint(tenantID)); err != nil {
		graphOperationError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "删除成功"})
}

// RunTask godoc
// @Summary      执行构建任务 | Run build task
// @Description  启动知识图谱构建任务 | Start a knowledge graph build task
// @Tags         图谱构建 | Graph Build
// @Produce      json
// @Security     BearerAuth
// @Param        id  path int true "知识图谱 ID | Knowledge graph ID"
// @Param        tid path int true "任务 ID | Task ID"
// @Success      202 {object} models.BuildEnqueueResponse "已持久入队，尚未开始 | Durably queued, not yet started"
// @Failure      400 {object} models.ErrorResponse
// @Failure      409 {object} models.ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["graph.build_task.execute"]
// @Router       /graphs/{id}/build/tasks/{tid}/run [post]
func (h *BuildHandler) RunTask(c *gin.Context) {
	graphID, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	tid, _ := strconv.ParseUint(c.Param("tid"), 10, 64)
	tenantID := commonMiddleware.GetTenantID(c)
	userID := commonMiddleware.GetUserID(c)

	executionID, err := h.buildSvc.RunTaskWithContext(graphBuildUserContext(c), uint(tid), uint(graphID), uint(tenantID), uint(userID), commonExecution.TriggerTypeManual, commonExecution.ModuleGraph, nil)
	if err != nil {
		respondBuildActionError(c, err)
		return
	}
	c.JSON(http.StatusAccepted, models.BuildEnqueueResponse{ExecutionID: executionID, Status: commonExecution.ExecutionStatusPending})
}

// CancelTask godoc
// @Summary      取消构建任务 | Cancel build task
// @Tags         图谱构建 | Graph Build
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id  path int true "知识图谱 ID | Knowledge graph ID"
// @Param        tid path int true "任务 ID | Task ID"
// @Param        request body models.CancelBuildRequest true "准确执行及观察到的 attempt | Exact execution and observed attempt"
// @Success      200 {object} models.CancelBuildResponse "未启动取消或已终态 | Cancelled before start or already terminal"
// @Success      202 {object} models.CancelBuildResponse "取消意图已持久受理，运行尚未停止 | Cancellation durably accepted; execution has not yet stopped"
// @Failure      400 {object} models.ErrorResponse
// @Failure      409 {object} models.ErrorResponse "执行身份已变化 | Execution identity has changed"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["graph.build_task.cancel"]
// @Router       /graphs/{id}/build/tasks/{tid}/cancel [post]
func (h *BuildHandler) CancelTask(c *gin.Context) {
	graphID, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	tid, _ := strconv.ParseUint(c.Param("tid"), 10, 64)
	tenantID := commonMiddleware.GetTenantID(c)

	var req models.CancelBuildRequest
	if err := commonAPI.BindOptionalJSONStrict(c, &req); err != nil || binding.Validator.ValidateStruct(&req) != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error_code": "invalid_cancel_identity", "error": commoni18n.T(c, graphi18n.MsgInvalidRequest)})
		return
	}
	e, err := h.buildSvc.CancelTask(c.Request.Context(), uint(tid), uint(graphID), uint(tenantID), commonMiddleware.GetUserID(c), req.ExecutionID, *req.Attempt)
	if err != nil {
		respondBuildActionError(c, err)
		return
	}
	status := http.StatusOK
	if e.Status == commonExecution.ExecutionStatusRunning {
		status = http.StatusAccepted
	}
	c.JSON(status, models.CancelBuildResponse{ExecutionID: e.ExecutionID, Attempt: e.Attempt, Status: e.Status, CancellationRequested: e.Status == commonExecution.ExecutionStatusRunning || e.Status == commonExecution.ExecutionStatusCancelled})
}

// RerunTask godoc
// @Summary      重新运行构建任务 | Rerun build task
// @Tags         图谱构建 | Graph Build
// @Produce      json
// @Security     BearerAuth
// @Param        id  path int true "知识图谱 ID | Knowledge graph ID"
// @Param        tid path int true "任务 ID | Task ID"
// @Success      202 {object} models.BuildEnqueueResponse "已持久入队，尚未开始 | Durably queued, not yet started"
// @Failure      400 {object} models.ErrorResponse
// @Failure      409 {object} models.ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["graph.build_task.execute"]
// @Router       /graphs/{id}/build/tasks/{tid}/rerun [post]
func (h *BuildHandler) RerunTask(c *gin.Context) {
	graphID, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	tid, _ := strconv.ParseUint(c.Param("tid"), 10, 64)
	tenantID := commonMiddleware.GetTenantID(c)
	userID := commonMiddleware.GetUserID(c)

	executionID, err := h.buildSvc.RerunTask(graphBuildUserContext(c), uint(tid), uint(graphID), uint(tenantID), uint(userID))
	if err != nil {
		respondBuildActionError(c, err)
		return
	}
	c.JSON(http.StatusAccepted, models.BuildEnqueueResponse{ExecutionID: executionID, Status: commonExecution.ExecutionStatusPending})
}

func respondBuildActionError(c *gin.Context, err error) {
	if errors.Is(err, repository.ErrGraphAdmissionClosed) {
		graphOperationError(c, err)
		return
	}
	if errors.Is(err, commonAPI.ErrConflict) {
		c.JSON(http.StatusConflict, gin.H{"error": commoni18n.T(c, graphi18n.MsgTaskActive)})
		return
	}
	c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, graphi18n.MsgTaskRunFailed)})
}

// ——— 材料管理 ———

// ListMaterials godoc
// @Summary      列出构建材料 | List build materials
// @Tags         图谱构建 | Graph Build
// @Produce      json
// @Security     BearerAuth
// @Param        id  path int true "知识图谱 ID | Knowledge graph ID"
// @Param        tid path int true "任务 ID | Task ID"
// @Success      200 {array} models.BuildMaterial
// @Failure      500 {object} models.ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["graph.build_task.read"]
// @Router       /graphs/{id}/build/tasks/{tid}/materials [get]
func (h *BuildHandler) ListMaterials(c *gin.Context) {
	tenantID := commonMiddleware.GetTenantID(c)
	tid, _ := strconv.ParseUint(c.Param("tid"), 10, 64)

	materials, err := h.buildSvc.ListMaterials(uint(tid), uint(tenantID))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, materials)
}

// UploadMaterial godoc
// @Summary      上传构建材料 | Upload build material
// @Tags         图谱构建 | Graph Build
// @Accept       multipart/form-data
// @Produce      json
// @Security     BearerAuth
// @Param        id    path int true "知识图谱 ID | Knowledge graph ID"
// @Param        tid   path int true "任务 ID | Task ID"
// @Param        files formData file true "材料文件 | Material files"
// @Success      201 {array} models.BuildMaterial
// @Failure      400 {object} models.ErrorResponse
// @Failure      404 {object} models.ErrorResponse
// @Failure      409 {object} models.ErrorResponse
// @Failure      500 {object} models.ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["graph.build_task.update"]
// @Router       /graphs/{id}/build/tasks/{tid}/materials [post]
func (h *BuildHandler) UploadMaterial(c *gin.Context) {
	graphID, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	tid, _ := strconv.ParseUint(c.Param("tid"), 10, 64)
	tenantID := commonMiddleware.GetTenantID(c)

	form, err := c.MultipartForm()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, graphi18n.MsgInvalidRequest)})
		return
	}

	files := form.File["files"]
	if len(files) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, graphi18n.MsgInvalidRequest)})
		return
	}

	var created []models.BuildMaterial
	for _, fh := range files {
		mat, err := h.uploadSingleFile(c, fh, uint(tid), uint(graphID), uint(tenantID))
		if err != nil {
			graphOperationError(c, err)
			return
		}
		created = append(created, *mat)
	}
	c.JSON(http.StatusCreated, created)
}

func (h *BuildHandler) uploadSingleFile(c *gin.Context, fh *multipart.FileHeader, taskID, graphID, tenantID uint) (*models.BuildMaterial, error) {
	f, err := fh.Open()
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return h.buildSvc.UploadMaterial(c.Request.Context(), taskID, tenantID, graphID, fh.Filename, f, fh.Size)
}

// DeleteMaterial godoc
// @Summary      删除构建材料 | Delete build material
// @Tags         图谱构建 | Graph Build
// @Produce      json
// @Security     BearerAuth
// @Param        id  path int true "知识图谱 ID | Knowledge graph ID"
// @Param        tid path int true "任务 ID | Task ID"
// @Param        mid path int true "材料 ID | Material ID"
// @Success      200 {object} models.SuccessResponse
// @Failure      404 {object} models.ErrorResponse
// @Failure      409 {object} models.ErrorResponse
// @Failure      500 {object} models.ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["graph.build_task.update"]
// @Router       /graphs/{id}/build/tasks/{tid}/materials/{mid} [delete]
func (h *BuildHandler) DeleteMaterial(c *gin.Context) {
	tenantID := commonMiddleware.GetTenantID(c)
	mid, _ := strconv.ParseUint(c.Param("mid"), 10, 64)

	if err := h.buildSvc.DeleteMaterial(c.Request.Context(), uint(mid), parseUintParam(c, "tid"), parseUintParam(c, "id"), uint(tenantID)); err != nil {
		graphOperationError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "删除成功"})
}

// ——— 审核队列 ———

// ListReviewItems godoc
// @Summary      列出审核项 | List review items
// @Tags         图谱构建 | Graph Build
// @Produce      json
// @Security     BearerAuth
// @Param        id        path  int    true  "知识图谱 ID | Knowledge graph ID"
// @Param        task_id   query int    false "任务 ID | Task ID"
// @Param        item_type query string false "项目类型 | Item type"
// @Param        status    query string false "状态 | Status"
// @Param        page      query int    false "页码 | Page" default(1)
// @Param        page_size query int    false "每页数量 | Page size" default(20)
// @Success      200 {object} map[string]interface{}
// @Failure      500 {object} models.ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["graph.review.read"]
// @Router       /graphs/{id}/review [get]
func (h *BuildHandler) ListReviewItems(c *gin.Context) {
	graphID, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	tenantID := commonMiddleware.GetTenantID(c)

	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))

	var taskID uint
	if tidStr := c.Query("task_id"); tidStr != "" {
		tid, _ := strconv.ParseUint(tidStr, 10, 64)
		taskID = uint(tid)
	}

	filter := repository.ReviewFilter{
		GraphID:  uint(graphID),
		TenantID: uint(tenantID),
		TaskID:   taskID,
		ItemType: c.Query("item_type"),
		Status:   c.Query("status"),
		Page:     page,
		PageSize: pageSize,
	}

	items, total, err := h.buildSvc.ListReviewItems(filter)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"data":  items,
		"total": total,
		"page":  page,
	})
}

// ApproveReviewItem godoc
// @Summary      通过审核项 | Approve review item
// @Tags         图谱构建 | Graph Build
// @Produce      json
// @Security     BearerAuth
// @Param        id  path int true "知识图谱 ID | Knowledge graph ID"
// @Param        iid path int true "审核项 ID | Review item ID"
// @Success      200 {object} models.SuccessResponse
// @Failure      400 {object} models.ErrorResponse "请求参数无效 | Invalid request parameters"
// @Failure      403 {object} models.ErrorResponse "访问被拒绝 | Access denied"
// @Failure      404 {object} models.ErrorResponse "当前租户及图谱内不存在审核项 | Review item not found in this tenant and graph"
// @Failure      409 {object} models.ErrorResponse "审核项已处理 | Review item already processed"
// @Failure      500 {object} models.ErrorResponse "审核处理失败 | Review processing failed"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["graph.review.approve"]
// @Router       /graphs/{id}/review/{iid}/approve [post]
func (h *BuildHandler) ApproveReviewItem(c *gin.Context) {
	tenantID := commonMiddleware.GetTenantID(c)
	userID := commonMiddleware.GetUserID(c)
	graphID, iid, ok := reviewItemIDs(c)
	if !ok {
		return
	}

	if err := h.buildSvc.ApproveReviewItem(c.Request.Context(), graphID, iid, uint(tenantID), uint(userID)); err != nil {
		reviewOperationError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": commoni18n.T(c, graphi18n.MsgApproved)})
}

// RejectReviewItem godoc
// @Summary      拒绝审核项 | Reject review item
// @Tags         图谱构建 | Graph Build
// @Produce      json
// @Security     BearerAuth
// @Param        id  path int true "知识图谱 ID | Knowledge graph ID"
// @Param        iid path int true "审核项 ID | Review item ID"
// @Success      200 {object} models.SuccessResponse
// @Failure      400 {object} models.ErrorResponse "请求参数无效 | Invalid request parameters"
// @Failure      403 {object} models.ErrorResponse "访问被拒绝 | Access denied"
// @Failure      404 {object} models.ErrorResponse "当前租户及图谱内不存在审核项 | Review item not found in this tenant and graph"
// @Failure      409 {object} models.ErrorResponse "审核项已处理 | Review item already processed"
// @Failure      500 {object} models.ErrorResponse "审核处理失败 | Review processing failed"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["graph.review.reject"]
// @Router       /graphs/{id}/review/{iid}/reject [post]
func (h *BuildHandler) RejectReviewItem(c *gin.Context) {
	tenantID := commonMiddleware.GetTenantID(c)
	userID := commonMiddleware.GetUserID(c)
	graphID, iid, ok := reviewItemIDs(c)
	if !ok {
		return
	}

	if err := h.buildSvc.RejectReviewItem(c.Request.Context(), graphID, iid, uint(tenantID), uint(userID)); err != nil {
		reviewOperationError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": commoni18n.T(c, graphi18n.MsgRejected)})
}

// ModifyReviewItem godoc
// @Summary      修改审核项 | Modify review item
// @Tags         图谱构建 | Graph Build
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id      path int true "知识图谱 ID | Knowledge graph ID"
// @Param        iid     path int true "审核项 ID | Review item ID"
// @Param        request body map[string]interface{} true "修改内容 | Modify content"
// @Success      200 {object} models.SuccessResponse
// @Failure      400 {object} models.ErrorResponse "请求参数无效 | Invalid request parameters"
// @Failure      403 {object} models.ErrorResponse "访问被拒绝 | Access denied"
// @Failure      404 {object} models.ErrorResponse "当前租户及图谱内不存在审核项 | Review item not found in this tenant and graph"
// @Failure      409 {object} models.ErrorResponse "审核项已处理 | Review item already processed"
// @Failure      500 {object} models.ErrorResponse "审核处理失败 | Review processing failed"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["graph.review.update"]
// @Router       /graphs/{id}/review/{iid} [put]
func (h *BuildHandler) ModifyReviewItem(c *gin.Context) {
	tenantID := commonMiddleware.GetTenantID(c)
	userID := commonMiddleware.GetUserID(c)
	graphID, iid, ok := reviewItemIDs(c)
	if !ok {
		return
	}

	var req struct {
		FinalContent map[string]interface{} `json:"final_content" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, commoni18n.MsgInvalidParams)})
		return
	}

	if err := h.buildSvc.ModifyAndApproveReviewItem(c.Request.Context(), graphID, iid, uint(tenantID), uint(userID), req.FinalContent); err != nil {
		reviewOperationError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": commoni18n.T(c, graphi18n.MsgModified)})
}

// BatchApproveReviewItems godoc
// @Summary      批量通过审核项 | Batch approve review items
// @Description  先核对全部 ID 归属，再逐项处理；任一失败返回 500，保留成功结果 | Validate ownership of all IDs before processing; any item failure returns 500 and preserves successful results
// @Tags         图谱构建 | Graph Build
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id      path int true "知识图谱 ID | Knowledge graph ID"
// @Param        request body models.BatchReviewRequest true "审核项 ID | Review item IDs"
// @Success      200 {object} models.SuccessResponse
// @Failure      400 {object} models.ErrorResponse "请求参数无效 | Invalid request parameters"
// @Failure      403 {object} models.ErrorResponse "访问被拒绝 | Access denied"
// @Failure      404 {object} models.ErrorResponse "当前租户及图谱内不存在审核项 | Review item not found in this tenant and graph"
// @Failure      500 {object} models.ErrorResponse "审核处理失败 | Review processing failed"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["graph.review.approve"]
// @Router       /graphs/{id}/review/batch/approve [post]
func (h *BuildHandler) BatchApproveReviewItems(c *gin.Context) {
	h.batchReview(c, "approve")
}

// BatchRejectReviewItems godoc
// @Summary      批量拒绝审核项 | Batch reject review items
// @Description  先核对全部 ID 归属，再逐项处理；任一失败返回 500，保留成功结果 | Validate ownership of all IDs before processing; any item failure returns 500 and preserves successful results
// @Tags         图谱构建 | Graph Build
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id      path int true "知识图谱 ID | Knowledge graph ID"
// @Param        request body models.BatchReviewRequest true "审核项 ID | Review item IDs"
// @Success      200 {object} models.SuccessResponse
// @Failure      400 {object} models.ErrorResponse "请求参数无效 | Invalid request parameters"
// @Failure      403 {object} models.ErrorResponse "访问被拒绝 | Access denied"
// @Failure      404 {object} models.ErrorResponse "当前租户及图谱内不存在审核项 | Review item not found in this tenant and graph"
// @Failure      500 {object} models.ErrorResponse "审核处理失败 | Review processing failed"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["graph.review.reject"]
// @Router       /graphs/{id}/review/batch/reject [post]
func (h *BuildHandler) BatchRejectReviewItems(c *gin.Context) {
	h.batchReview(c, "reject")
}

func (h *BuildHandler) batchReview(c *gin.Context, action string) {
	graphID, ok := reviewPositiveID(c, "id")
	if !ok {
		return
	}
	tenantID := commonMiddleware.GetTenantID(c)
	userID := commonMiddleware.GetUserID(c)

	var req models.BatchReviewRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, commoni18n.MsgInvalidParams)})
		return
	}

	if err := h.buildSvc.BatchReview(c.Request.Context(), uint(graphID), uint(tenantID), uint(userID), req.IDs, action); err != nil {
		reviewOperationError(c, err)
		return
	}
	key := graphi18n.MsgBatchApproved
	if action == "reject" {
		key = graphi18n.MsgBatchRejected
	}
	c.JSON(http.StatusOK, gin.H{"message": commoni18n.T(c, key)})
}

// PendingReviewCount godoc
// @Summary      待审核数量 | Pending review count
// @Tags         图谱构建 | Graph Build
// @Produce      json
// @Security     BearerAuth
// @Param        id path int true "知识图谱 ID | Knowledge graph ID"
// @Success      200 {object} map[string]int
// @Failure      500 {object} models.ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["graph.review.read"]
// @Router       /graphs/{id}/review/pending-count [get]
func (h *BuildHandler) PendingReviewCount(c *gin.Context) {
	graphID, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	tenantID := commonMiddleware.GetTenantID(c)

	count, err := h.buildSvc.CountPendingReview(uint(graphID), uint(tenantID))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"count": count})
}

// Both native write failures and review persistence errors stay in server logs.
func reviewOperationError(c *gin.Context, err error) {
	status, key := 0, ""
	switch {
	case errors.Is(err, service.ErrReviewBatchFailed):
		status, key = http.StatusInternalServerError, graphi18n.MsgReviewBatchFailed
	case errors.Is(err, commonAPI.ErrConflict):
		status, key = http.StatusConflict, graphi18n.MsgReviewAlreadyProcessed
	case errors.Is(err, commonAPI.ErrBadRequest):
		status, key = http.StatusBadRequest, commoni18n.MsgInvalidParams
	default:
		graphOperationError(c, err)
		return
	}
	_ = c.Error(err)
	c.JSON(status, gin.H{"error": commoni18n.T(c, key)})
}

func reviewPositiveID(c *gin.Context, name string) (uint, bool) {
	id, err := strconv.ParseUint(c.Param(name), 10, strconv.IntSize)
	if err != nil || id == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, commoni18n.MsgInvalidParams)})
		return 0, false
	}
	return uint(id), true
}

func reviewItemIDs(c *gin.Context) (uint, uint, bool) {
	graphID, ok := reviewPositiveID(c, "id")
	if !ok {
		return 0, 0, false
	}
	itemID, ok := reviewPositiveID(c, "iid")
	return graphID, itemID, ok
}
