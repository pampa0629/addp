package api

import (
	"errors"
	"mime/multipart"
	"net/http"
	"strconv"

	commonAPI "github.com/addp/common/api"
	commonMiddleware "github.com/addp/common/middleware/auth"
	commoni18n "github.com/addp/common/middleware/i18n"
	graphi18n "github.com/addp/graph/i18n"
	"github.com/addp/graph/internal/models"
	"github.com/addp/graph/internal/repository"
	"github.com/addp/graph/internal/service"
	"github.com/gin-gonic/gin"
)

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
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
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
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
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
// @Failure      500 {object} models.ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["graph.build_task.delete"]
// @Router       /graphs/{id}/build/tasks/{tid} [delete]
func (h *BuildHandler) DeleteTask(c *gin.Context) {
	tenantID := commonMiddleware.GetTenantID(c)
	tid, _ := strconv.ParseUint(c.Param("tid"), 10, 64)

	if err := h.buildSvc.DeleteTask(uint(tid), uint(tenantID)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
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
// @Success      200 {object} models.SuccessResponse
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

	if err := h.buildSvc.RunTask(c.Request.Context(), uint(tid), uint(graphID), uint(tenantID), uint(userID)); err != nil {
		respondBuildActionError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "任务已启动"})
}

// CancelTask godoc
// @Summary      取消构建任务 | Cancel build task
// @Tags         图谱构建 | Graph Build
// @Produce      json
// @Security     BearerAuth
// @Param        id  path int true "知识图谱 ID | Knowledge graph ID"
// @Param        tid path int true "任务 ID | Task ID"
// @Success      200 {object} models.SuccessResponse
// @Failure      400 {object} models.ErrorResponse
// @Failure      409 {object} models.ErrorResponse "任务冲突或当前进程不持有运行实例 | Task conflict or runtime not owned by current process"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["graph.build_task.cancel"]
// @Router       /graphs/{id}/build/tasks/{tid}/cancel [post]
func (h *BuildHandler) CancelTask(c *gin.Context) {
	graphID, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	tid, _ := strconv.ParseUint(c.Param("tid"), 10, 64)
	tenantID := commonMiddleware.GetTenantID(c)

	if err := h.buildSvc.CancelTask(uint(tid), uint(graphID), uint(tenantID)); err != nil {
		respondBuildActionError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "任务已取消"})
}

// RerunTask godoc
// @Summary      重新运行构建任务 | Rerun build task
// @Tags         图谱构建 | Graph Build
// @Produce      json
// @Security     BearerAuth
// @Param        id  path int true "知识图谱 ID | Knowledge graph ID"
// @Param        tid path int true "任务 ID | Task ID"
// @Success      200 {object} models.SuccessResponse
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

	if err := h.buildSvc.RerunTask(c.Request.Context(), uint(tid), uint(graphID), uint(tenantID), uint(userID)); err != nil {
		respondBuildActionError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "任务已重新启动"})
}

func respondBuildActionError(c *gin.Context, err error) {
	if errors.Is(err, service.ErrBuildRuntimeNotOwned) {
		c.JSON(http.StatusConflict, gin.H{"error": commoni18n.T(c, graphi18n.MsgTaskRuntimeNotOwned)})
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
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的 multipart 表单"})
		return
	}

	files := form.File["files"]
	if len(files) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "未上传文件"})
		return
	}

	var created []models.BuildMaterial
	for _, fh := range files {
		mat, err := h.uploadSingleFile(c, fh, uint(tid), uint(graphID), uint(tenantID))
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
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
	return h.buildSvc.UploadMaterial(taskID, tenantID, graphID, fh.Filename, f, fh.Size)
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
// @Failure      500 {object} models.ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["graph.build_task.update"]
// @Router       /graphs/{id}/build/tasks/{tid}/materials/{mid} [delete]
func (h *BuildHandler) DeleteMaterial(c *gin.Context) {
	tenantID := commonMiddleware.GetTenantID(c)
	mid, _ := strconv.ParseUint(c.Param("mid"), 10, 64)

	if err := h.buildSvc.DeleteMaterial(uint(mid), uint(tenantID)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
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
// @Failure      500 {object} models.ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["graph.review.approve"]
// @Router       /graphs/{id}/review/{iid}/approve [post]
func (h *BuildHandler) ApproveReviewItem(c *gin.Context) {
	tenantID := commonMiddleware.GetTenantID(c)
	userID := commonMiddleware.GetUserID(c)
	iid, _ := strconv.ParseUint(c.Param("iid"), 10, 64)

	if err := h.buildSvc.ApproveReviewItem(c.Request.Context(), uint(iid), uint(tenantID), uint(userID)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "已确认写入"})
}

// RejectReviewItem godoc
// @Summary      拒绝审核项 | Reject review item
// @Tags         图谱构建 | Graph Build
// @Produce      json
// @Security     BearerAuth
// @Param        id  path int true "知识图谱 ID | Knowledge graph ID"
// @Param        iid path int true "审核项 ID | Review item ID"
// @Success      200 {object} models.SuccessResponse
// @Failure      500 {object} models.ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["graph.review.reject"]
// @Router       /graphs/{id}/review/{iid}/reject [post]
func (h *BuildHandler) RejectReviewItem(c *gin.Context) {
	tenantID := commonMiddleware.GetTenantID(c)
	userID := commonMiddleware.GetUserID(c)
	iid, _ := strconv.ParseUint(c.Param("iid"), 10, 64)

	if err := h.buildSvc.RejectReviewItem(c.Request.Context(), uint(iid), uint(tenantID), uint(userID)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "已拒绝"})
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
// @Failure      400 {object} models.ErrorResponse
// @Failure      500 {object} models.ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["graph.review.update"]
// @Router       /graphs/{id}/review/{iid} [put]
func (h *BuildHandler) ModifyReviewItem(c *gin.Context) {
	tenantID := commonMiddleware.GetTenantID(c)
	userID := commonMiddleware.GetUserID(c)
	iid, _ := strconv.ParseUint(c.Param("iid"), 10, 64)

	var req struct {
		FinalContent map[string]interface{} `json:"final_content" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := h.buildSvc.ModifyAndApproveReviewItem(c.Request.Context(), uint(iid), uint(tenantID), uint(userID), req.FinalContent); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "已修改并写入"})
}

// BatchApproveReviewItems godoc
// @Summary      批量通过审核项 | Batch approve review items
// @Tags         图谱构建 | Graph Build
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id      path int true "知识图谱 ID | Knowledge graph ID"
// @Param        request body map[string][]uint true "审核项 ID | Review item IDs"
// @Success      200 {object} models.SuccessResponse
// @Failure      400 {object} models.ErrorResponse
// @Failure      500 {object} models.ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["graph.review.approve"]
// @Router       /graphs/{id}/review/batch/approve [post]
func (h *BuildHandler) BatchApproveReviewItems(c *gin.Context) {
	h.batchReview(c, "approve")
}

// BatchRejectReviewItems godoc
// @Summary      批量拒绝审核项 | Batch reject review items
// @Tags         图谱构建 | Graph Build
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id      path int true "知识图谱 ID | Knowledge graph ID"
// @Param        request body map[string][]uint true "审核项 ID | Review item IDs"
// @Success      200 {object} models.SuccessResponse
// @Failure      400 {object} models.ErrorResponse
// @Failure      500 {object} models.ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["graph.review.reject"]
// @Router       /graphs/{id}/review/batch/reject [post]
func (h *BuildHandler) BatchRejectReviewItems(c *gin.Context) {
	h.batchReview(c, "reject")
}

func (h *BuildHandler) batchReview(c *gin.Context, action string) {
	graphID, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	tenantID := commonMiddleware.GetTenantID(c)
	userID := commonMiddleware.GetUserID(c)

	var req struct {
		IDs []uint `json:"ids" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := h.buildSvc.BatchReview(c.Request.Context(), uint(graphID), uint(tenantID), uint(userID), req.IDs, action); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "批量操作完成"})
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
