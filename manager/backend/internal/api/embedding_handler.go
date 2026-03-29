package api

import (
	"context"
	"fmt"
	"net/http"
	"time"

	commonModels "github.com/addp/common/models"
	"github.com/addp/manager/internal/service"
	"github.com/gin-gonic/gin"
)

// EmbeddingHandler 向量化API处理器
type EmbeddingHandler struct {
	embeddingService *service.EmbeddingService
	taskTracker      *service.TaskTracker
}

// NewEmbeddingHandler 创建向量化处理器
func NewEmbeddingHandler(embeddingService *service.EmbeddingService, taskTracker *service.TaskTracker) *EmbeddingHandler {
	return &EmbeddingHandler{
		embeddingService: embeddingService,
		taskTracker:      taskTracker,
	}
}

// CreateEmbeddingRequest 创建向量化任务请求
type CreateEmbeddingRequest struct {
	EngineID  uint   `json:"engine_id" binding:"required"`
	Bucket    string `json:"bucket" binding:"required"`
	Scope     string `json:"scope"`                      // object|directory|bucket，默认 object
	ObjectKey string `json:"object_key,omitempty"`       // scope=object 时必填
	Prefix    string `json:"prefix,omitempty"`           // scope=directory 时使用
	Recursive bool   `json:"recursive"`                  // 是否递归，默认 true
	TaskName  string `json:"task_name,omitempty"`        // 任务名称（可选）
}

// CreateEmbedding POST /api/embedding
// 创建向量化任务（统一入口）
// @Summary CreateEmbedding
// @Tags Manager
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /createembedding [get]
// @Security BearerAuth
func (h *EmbeddingHandler) CreateEmbedding(c *gin.Context) {
	tenantID := c.GetUint("tenant_id")

	var req CreateEmbeddingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"status":  "error",
			"message": fmt.Sprintf("参数错误: %v", err),
		})
		return
	}

	// 设置默认值
	if req.Scope == "" {
		req.Scope = "object"
	}

	// 参数验证
	if req.Scope == "object" && req.ObjectKey == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"status":  "error",
			"message": "scope=object 时，object_key 必填",
		})
		return
	}

	// 构建任务名称
	taskName := req.TaskName
	if taskName == "" {
		taskName = fmt.Sprintf("向量化-%s-%s", req.Bucket, req.Scope)
	}

	// 创建任务ID
	taskID := fmt.Sprintf("embedding-%d-%s-%d", req.EngineID, req.Scope, time.Now().Unix())

	// 创建任务追踪
	h.taskTracker.CreateTask(taskID, "向量化任务已创建")
	h.taskTracker.UpdateStatus(taskID, service.TaskStatusRunning, "正在向量化...")

	// 异步执行向量化任务
	go func() {
		bgCtx := context.Background()

		var err error
		var result interface{}

		switch req.Scope {
		case "object":
			// 拆分 ObjectKey 为 Path 和 Name
			path, name := commonModels.SplitObjectPath(req.ObjectKey)
			result, err = h.embeddingService.EmbedObject(bgCtx, service.EmbedObjectRequest{
				EngineID: req.EngineID,
				Bucket:   req.Bucket,
				Path:     path,
				Name:     name,
				TenantID: &tenantID,
			})

		case "directory":
			recursive := req.Recursive
			if !recursive {
				recursive = true // 默认递归
			}
			result, err = h.embeddingService.EmbedDirectory(bgCtx, service.EmbedDirectoryRequest{
				EngineID:  req.EngineID,
				Bucket:    req.Bucket,
				Prefix:    req.Prefix,
				Recursive: recursive,
				TenantID:  &tenantID,
			})

		case "bucket":
			result, err = h.embeddingService.EmbedBucket(bgCtx, service.EmbedBucketRequest{
				EngineID: req.EngineID,
				Bucket:   req.Bucket,
				TenantID: &tenantID,
			})

		default:
			h.taskTracker.SetTaskError(taskID, fmt.Errorf("未知的 scope: %s", req.Scope))
			return
		}

		if err != nil {
			h.taskTracker.SetTaskError(taskID, err)
		} else {
			h.taskTracker.SetTaskResult(taskID, result)
			h.taskTracker.UpdateStatus(taskID, service.TaskStatusCompleted, "向量化完成")
		}
	}()

	// 返回响应
	c.JSON(http.StatusOK, gin.H{
		"status":      "success",
		"task_id":     taskID,
		"task_status": "running",
		"message":     "向量化任务已创建并开始执行",
		"created_at":  time.Now().Format(time.RFC3339),
	})
}

// GetEmbeddingTaskStatus GET /api/embedding/tasks/:task_id
// 查询向量化任务状态（仅查内存中的实时状态）
// @Summary GetEmbeddingTaskStatus
// @Tags Manager
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /getembeddingtaskstatus [get]
// @Security BearerAuth
func (h *EmbeddingHandler) GetEmbeddingTaskStatus(c *gin.Context) {
	taskID := c.Param("task_id")

	// 查询内存中的实时任务状态
	task, ok := h.taskTracker.GetTask(taskID)
	if ok {
		// 返回实时任务状态（正在运行的任务）
		c.JSON(http.StatusOK, gin.H{
			"status":      "success",
			"task_id":     task.TaskID,
			"task_status": string(task.Status),
			"message":     task.Message,
			"progress":    task.Progress,
			"start_time":  task.StartTime,
			"end_time":    task.EndTime,
			"result":      task.Result,
			"error":       task.Error,
		})
		return
	}

	c.JSON(http.StatusNotFound, gin.H{
		"status":  "error",
		"message": "任务不存在或已过期，请通过 /api/manager/executions/:execution_id 查询历史执行记录",
	})
}
