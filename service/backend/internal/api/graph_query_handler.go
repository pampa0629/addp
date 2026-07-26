package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	commonapi "github.com/addp/common/api"
	commoni18n "github.com/addp/common/middleware/i18n"
	servicei18n "github.com/addp/service/i18n"
	"github.com/addp/service/internal/models"
	svc "github.com/addp/service/internal/service"
	"github.com/addp/service/internal/service/data"
	"github.com/gin-gonic/gin"
)

// GraphQueryHandler 处理图查询服务相关 HTTP 请求
type GraphQueryHandler struct {
	svc      *svc.GraphQueryServiceService
	executor *data.GraphQueryExecutor
}

func NewGraphQueryHandler(s *svc.GraphQueryServiceService, executor *data.GraphQueryExecutor) *GraphQueryHandler {
	return &GraphQueryHandler{svc: s, executor: executor}
}

// ===== 服务管理 API =====

// CreateService 创建图查询服务
// @Summary 创建图查询服务 | Create graph query service
// @Tags GraphQueryService
// @Accept json
// @Produce json
// @Param request body models.CreateGraphQueryServiceRequest true "创建请求 | Create request"
// @Success 201 {object} map[string]interface{}
// @Failure 400 {object} map[string]string
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["service.definition.create"]
// @Router /graph [post]
// @Security BearerAuth
func (h *GraphQueryHandler) CreateService(c *gin.Context) {
	tenantID := tenantIDValue(c)
	userID := userIDValue(c)
	if tenantID == 0 || userID == 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Missing tenant_id or user_id in token"})
		return
	}

	var req models.CreateGraphQueryServiceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body: " + err.Error()})
		return
	}

	result, err := h.svc.CreateService(&req, tenantID, userID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, result)
}

// ListServices 获取图查询服务列表
// @Summary 图查询服务列表 | List graph query services
// @Tags GraphQueryService
// @Produce json
// @Param page query int false "页码 | Page" default(1)
// @Param page_size query int false "每页数量 | Page size" default(20)
// @Param search query string false "搜索词 | Search"
// @Success 200 {object} map[string]interface{}
// @Failure 500 {object} map[string]string
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["service.definition.read"]
// @Router /graph [get]
// @Security BearerAuth
func (h *GraphQueryHandler) ListServices(c *gin.Context) {
	tenantID := tenantIDValue(c)
	if tenantID == 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Missing tenant_id in token"})
		return
	}

	page := 1
	if p := c.Query("page"); p != "" {
		if parsed, err := strconv.Atoi(p); err == nil && parsed > 0 {
			page = parsed
		}
	}
	pageSize := 20
	if ps := c.Query("page_size"); ps != "" {
		if parsed, err := strconv.Atoi(ps); err == nil && parsed > 0 && parsed <= 100 {
			pageSize = parsed
		}
	}
	offset := (page - 1) * pageSize
	search := c.Query("search")

	var (
		results []models.GraphQueryServiceDTO
		total   int64
		err     error
	)
	if search != "" {
		results, total, err = h.svc.SearchServices(tenantID, search, offset, pageSize)
	} else {
		results, total, err = h.svc.ListServices(tenantID, offset, pageSize)
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to list services: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"data":        results,
		"total":       total,
		"page":        page,
		"page_size":   pageSize,
		"total_pages": (total + int64(pageSize) - 1) / int64(pageSize),
	})
}

// GetService 获取图查询服务详情
// @Summary 获取图查询服务 | Get graph query service
// @Tags GraphQueryService
// @Produce json
// @Param id path int true "服务ID | Service ID"
// @Success 200 {object} map[string]interface{}
// @Failure 404 {object} map[string]string
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["service.definition.read"]
// @Router /graph/{id} [get]
// @Security BearerAuth
func (h *GraphQueryHandler) GetService(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid service ID"})
		return
	}

	result, err := h.svc.GetService(id)
	if err != nil {
		if errors.Is(err, commonapi.ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Service not found"})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		}
		return
	}

	c.JSON(http.StatusOK, result)
}

// UpdateService 更新图查询服务
// @Summary 更新图查询服务 | Update graph query service
// @Tags GraphQueryService
// @Accept json
// @Produce json
// @Param id path int true "服务ID | Service ID"
// @Param request body models.UpdateGraphQueryServiceRequest true "更新请求 | Update request"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["service.definition.update"]
// @Router /graph/{id} [put]
// @Security BearerAuth
func (h *GraphQueryHandler) UpdateService(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid service ID"})
		return
	}

	var req models.UpdateGraphQueryServiceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body: " + err.Error()})
		return
	}

	result, err := h.svc.UpdateService(id, &req)
	if err != nil {
		if errors.Is(err, commonapi.ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Service not found"})
		} else {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		}
		return
	}

	c.JSON(http.StatusOK, result)
}

// DeleteService 删除图查询服务
// @Summary 删除图查询服务 | Delete graph query service
// @Tags GraphQueryService
// @Produce json
// @Param id path int true "服务ID | Service ID"
// @Success 200 {object} map[string]string
// @Failure 500 {object} map[string]string
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["service.definition.delete"]
// @Router /graph/{id} [delete]
// @Security BearerAuth
func (h *GraphQueryHandler) DeleteService(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid service ID"})
		return
	}

	if err := h.svc.DeleteService(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": commoni18n.T(c, servicei18n.MsgDeleteSuccess)})
}

// ===== 数据执行 API =====

// ExecuteQuery POST /api/graph/:serviceName
// 公开端点（认证可选）：从 JWT 中提取 tenantID；若无 token 则 tenantID=0（要求 public_access=true）
// @Summary 执行图查询服务 | Execute graph query service
// @Tags GraphQueryExecution
// @Accept json
// @Produce json
// @Param serviceName path string true "服务名称 | Service name"
// @Param request body models.GraphQueryExecuteRequest false "执行请求 | Execute request"
// @Success 200 {object} models.GraphQueryResponse
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 500 {object} map[string]string
// @x-addp-auth-mode "public"
// @Router /api/gquery/{serviceName} [post]
func (h *GraphQueryHandler) ExecuteQuery(c *gin.Context) {
	serviceName := c.Param("serviceName")
	if serviceName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "serviceName is required"})
		return
	}

	// tenantID 可能为 0（公开访问）
	tenantID := tenantIDValue(c)

	var req models.GraphQueryExecuteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		// 允许空 body（无参数查询）
		req = models.GraphQueryExecuteRequest{}
	}

	result, err := h.executor.Execute(c.Request.Context(), serviceName, tenantID, &req)
	if err != nil {
		statusCode := http.StatusInternalServerError
		msg := err.Error()
		if errors.Is(err, commonapi.ErrNotFound) || strings.Contains(msg, "not found") {
			statusCode = http.StatusNotFound
		} else if strings.Contains(msg, "requires authentication") {
			statusCode = http.StatusUnauthorized
		} else if strings.Contains(msg, "required but not provided") || strings.Contains(msg, "is inactive") || strings.Contains(msg, "is error") {
			statusCode = http.StatusBadRequest
		}
		c.JSON(statusCode, gin.H{"error": msg})
		return
	}

	c.JSON(http.StatusOK, result)
}

// parseID 从路径参数解析 uint ID
func parseID(c *gin.Context) (uint, error) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		return 0, err
	}
	return uint(id), nil
}
