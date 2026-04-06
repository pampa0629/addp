package api

import (
	"mime/multipart"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	commonMiddleware "github.com/addp/common/middleware/auth"
	"github.com/addp/graph/internal/models"
	"github.com/addp/graph/internal/repository"
	"github.com/addp/graph/internal/service"
)

type BuildHandler struct {
	buildSvc *service.BuildService
}

func NewBuildHandler(buildSvc *service.BuildService) *BuildHandler {
	return &BuildHandler{buildSvc: buildSvc}
}

// ——— 任务管理 ———

// ListTasks godoc
// @Summary      构建任务列表
// @Description  获取知识图谱的所有构建任务
// @Tags         图谱构建
// @Produce      json
// @Security     BearerAuth
// @Param        id path int true "知识图谱 ID"
// @Success      200 {array}  models.BuildTask
// @Failure      500 {object} models.ErrorResponse
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
// @Summary      创建构建任务
// @Description  创建新的知识图谱构建任务
// @Tags         图谱构建
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id path int true "知识图谱 ID"
// @Success      201 {object} models.BuildTask
// @Failure      400 {object} models.ErrorResponse
// @Failure      500 {object} models.ErrorResponse
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

// GetTask GET /graphs/:id/build/tasks/:tid
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

// DeleteTask DELETE /graphs/:id/build/tasks/:tid
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
// @Summary      执行构建任务
// @Description  启动知识图谱构建任务
// @Tags         图谱构建
// @Produce      json
// @Security     BearerAuth
// @Param        id  path int true "知识图谱 ID"
// @Param        tid path int true "任务 ID"
// @Success      200 {object} models.SuccessResponse
// @Failure      400 {object} models.ErrorResponse
// @Router       /graphs/{id}/build/tasks/{tid}/run [post]
func (h *BuildHandler) RunTask(c *gin.Context) {
	graphID, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	tid, _ := strconv.ParseUint(c.Param("tid"), 10, 64)
	tenantID := commonMiddleware.GetTenantID(c)
	userID := commonMiddleware.GetUserID(c)

	if err := h.buildSvc.RunTask(c.Request.Context(), uint(tid), uint(graphID), uint(tenantID), uint(userID)); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "任务已启动"})
}

// CancelTask POST /graphs/:id/build/tasks/:tid/cancel
func (h *BuildHandler) CancelTask(c *gin.Context) {
	graphID, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	tid, _ := strconv.ParseUint(c.Param("tid"), 10, 64)
	tenantID := commonMiddleware.GetTenantID(c)

	if err := h.buildSvc.CancelTask(uint(tid), uint(graphID), uint(tenantID)); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "任务已取消"})
}

// ——— 材料管理 ———

// ListMaterials GET /graphs/:id/build/tasks/:tid/materials
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

// UploadMaterial POST /graphs/:id/build/tasks/:tid/materials (multipart)
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

// DeleteMaterial DELETE /graphs/:id/build/tasks/:tid/materials/:mid
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

// ListReviewItems GET /graphs/:id/review
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

// ApproveReviewItem POST /graphs/:id/review/:iid/approve
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

// RejectReviewItem POST /graphs/:id/review/:iid/reject
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

// ModifyReviewItem PUT /graphs/:id/review/:iid
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

// BatchReview POST /graphs/:id/review/batch
func (h *BuildHandler) BatchReview(c *gin.Context) {
	graphID, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	tenantID := commonMiddleware.GetTenantID(c)
	userID := commonMiddleware.GetUserID(c)

	var req struct {
		IDs    []uint `json:"ids" binding:"required"`
		Action string `json:"action" binding:"required"` // approve/reject
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.Action != "approve" && req.Action != "reject" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "action 只能是 approve 或 reject"})
		return
	}

	if err := h.buildSvc.BatchReview(c.Request.Context(), uint(graphID), uint(tenantID), uint(userID), req.IDs, req.Action); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "批量操作完成"})
}

// PendingReviewCount GET /graphs/:id/review/pending-count
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
