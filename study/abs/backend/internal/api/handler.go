package api

import (
	"abs/internal/models"
	"abs/internal/service"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all origins in development
	},
}

// Handler handles HTTP requests
type Handler struct {
	taskService *service.TaskService
	appService  *service.AppService
	wsManager   *service.WebSocketManager
}

// NewHandler creates a new API handler
func NewHandler(taskService *service.TaskService, appService *service.AppService, wsManager *service.WebSocketManager) *Handler {
	return &Handler{
		taskService: taskService,
		appService:  appService,
		wsManager:   wsManager,
	}
}

// CreateTask handles POST /api/tasks
func (h *Handler) CreateTask(c *gin.Context) {
	var req models.CreateTaskRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	task, err := h.taskService.CreateTask(req.Prompt)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, models.TaskResponse{
		Task:    task,
		Message: "Task created successfully",
	})
}

// GetTask handles GET /api/tasks/:id
func (h *Handler) GetTask(c *gin.Context) {
	id := c.Param("id")

	task, err := h.taskService.GetTask(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, models.TaskResponse{
		Task: task,
	})
}

// ListTasks handles GET /api/tasks
func (h *Handler) ListTasks(c *gin.Context) {
	tasks := h.taskService.ListTasks()
	c.JSON(http.StatusOK, gin.H{"tasks": tasks})
}

// CreateApp handles POST /api/apps
func (h *Handler) CreateApp(c *gin.Context) {
	var req models.CreateAppRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	app, err := h.appService.CreateApp(&req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"app": app})
}

// ListApps handles GET /api/apps
func (h *Handler) ListApps(c *gin.Context) {
	apps := h.appService.ListApps()
	c.JSON(http.StatusOK, gin.H{"apps": apps})
}

// GetApp handles GET /api/apps/:id
func (h *Handler) GetApp(c *gin.Context) {
	app, err := h.appService.GetApp(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"app": app})
}

// LaunchApp handles POST /api/apps/:id/launch
func (h *Handler) LaunchApp(c *gin.Context) {
	app, err := h.appService.LaunchApp(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	resp := models.LaunchResponse{
		App:       app,
		Message:   "应用启动指令已下发",
		ProcessID: app.RunningPID,
	}
	c.JSON(http.StatusOK, resp)
}

// DeleteApp handles DELETE /api/apps/:id
func (h *Handler) DeleteApp(c *gin.Context) {
	id := c.Param("id")

	if err := h.appService.DeleteApp(id); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "应用已成功删除"})
}

// ModifyApp handles POST /api/apps/:id/modify
func (h *Handler) ModifyApp(c *gin.Context) {
	appID := c.Param("id")

	var req models.ModifyAppRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// 创建修改任务
	task, err := h.taskService.CreateModificationTask(appID, req.Prompt)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, models.TaskResponse{
		Task:    task,
		Message: "修改任务已创建，正在后台处理",
	})
}

// WebSocket handles WebSocket connections at /ws
func (h *Handler) WebSocket(c *gin.Context) {
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to upgrade connection"})
		return
	}

	h.wsManager.AddClient(conn)

	// Keep connection alive and handle disconnection
	defer h.wsManager.RemoveClient(conn)

	for {
		_, _, err := conn.ReadMessage()
		if err != nil {
			break
		}
	}
}

// Health handles GET /health
func (h *Handler) Health(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status":  "healthy",
		"service": "abs-backend",
	})
}

// ServeWorkspaceFile 提供 workspace 文件服务 (用于前端 iframe)
func ServeWorkspaceFile(workspaceDir string) gin.HandlerFunc {
	return func(c *gin.Context) {
		filepath := c.Param("filepath")
		// 确保路径以 / 开头
		if len(filepath) > 0 && filepath[0] != '/' {
			filepath = "/" + filepath
		}
		fullPath := workspaceDir + filepath

		// Debug log
		println("ServeWorkspaceFile:", fullPath)

		c.File(fullPath)
	}
}
