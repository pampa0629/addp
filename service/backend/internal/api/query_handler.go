package api

import (
	"bytes"
	"errors"
	"io"
	"log"
	"net/http"
	"strconv"

	"github.com/addp/service/internal/models"
	svc "github.com/addp/service/internal/service"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// QueryServiceHandler 处理查询服务相关的 HTTP 请求
type QueryServiceHandler struct {
	svc         *svc.QueryServiceService
	executorSvc *svc.QueryExecutorService
}

// NewQueryServiceHandler 创建新的查询服务处理器
func NewQueryServiceHandler(s *svc.QueryServiceService, executorSvc *svc.QueryExecutorService) *QueryServiceHandler {
	return &QueryServiceHandler{
		svc:         s,
		executorSvc: executorSvc,
	}
}

// ===== 服务管理 API =====

// CreateService 创建新的查询服务
// POST /api/service/query
func (h *QueryServiceHandler) CreateService(c *gin.Context) {
	var req models.CreateQueryServiceRequest

	// 先读取原始请求体用于调试
	bodyBytes, _ := io.ReadAll(c.Request.Body)
	log.Printf("[QueryService] CreateService request body: %s", string(bodyBytes))

	// 重新设置请求体以便绑定
	c.Request.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

	if err := c.ShouldBindJSON(&req); err != nil {
		log.Printf("[QueryService] Failed to bind request: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body: " + err.Error()})
		return
	}

	// 从 JWT token 中获取租户 ID 和用户 ID
	tenantID := c.GetUint("tenant_id")
	userID := c.GetUint("user_id")

	if tenantID == 0 || userID == 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Missing tenant_id or user_id in token"})
		return
	}

	result, err := h.svc.CreateService(&req, tenantID, userID)
	if err != nil {
		// 区分不同的错误类型
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		} else {
			// 验证错误和业务错误都返回 400
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		}
		return
	}

	c.JSON(http.StatusCreated, result)
}

// ListServices 列出租户下的所有查询服务
// GET /api/service/query?page=1&limit=20&search=...&config_type=table
func (h *QueryServiceHandler) ListServices(c *gin.Context) {
	tenantID := c.GetUint("tenant_id")
	if tenantID == 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Missing tenant_id in token"})
		return
	}

	// 分页参数
	page := 1
	if p := c.Query("page"); p != "" {
		if parsed, err := strconv.Atoi(p); err == nil && parsed > 0 {
			page = parsed
		}
	}

	limit := 20
	if l := c.Query("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 && parsed <= 100 {
			limit = parsed
		}
	}

	offset := (page - 1) * limit

	// 搜索参数
	search := c.Query("search")

	var results []models.QueryServiceDTO
	var total int64
	var err error

	if search != "" {
		// 如果有搜索词，使用 SearchServices
		results, total, err = h.svc.SearchServices(tenantID, search, offset, limit)
	} else {
		// 否则使用 ListServices
		results, total, err = h.svc.ListServices(tenantID, offset, limit)
	}

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to list services: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"data":  results,
		"total": total,
		"page":  page,
		"limit": limit,
		"pages": (total + int64(limit) - 1) / int64(limit),
	})
}

// GetService 获取服务详情
// GET /api/service/query/:id
func (h *QueryServiceHandler) GetService(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid service ID"})
		return
	}

	result, err := h.svc.GetService(uint(id))
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Service not found"})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get service: " + err.Error()})
		}
		return
	}

	c.JSON(http.StatusOK, result)
}

// UpdateService 更新服务
// PUT /api/service/query/:id
func (h *QueryServiceHandler) UpdateService(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid service ID"})
		return
	}

	var req models.UpdateQueryServiceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body: " + err.Error()})
		return
	}

	result, err := h.svc.UpdateService(uint(id), &req)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Service not found"})
		} else {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		}
		return
	}

	c.JSON(http.StatusOK, result)
}

// DeleteService 删除服务
// DELETE /api/service/query/:id
func (h *QueryServiceHandler) DeleteService(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid service ID"})
		return
	}

	if err := h.svc.DeleteService(uint(id)); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Service not found"})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete service: " + err.Error()})
		}
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Service deleted successfully"})
}

// ===== REST 查询 API =====

// QueryData 查询服务数据
// GET /api/query/:serviceName
func (h *QueryServiceHandler) QueryData(c *gin.Context) {
	serviceName := c.Param("serviceName")

	// 从 JWT token 中获取租户 ID（如果是公开服务则可能没有）
	tenantID := c.GetUint("tenant_id")

	// 获取服务配置（获取模型而不是 DTO）
	service, err := h.svc.GetServiceModelByName(serviceName, tenantID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Service not found"})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get service: " + err.Error()})
		}
		return
	}

	// 检查服务状态
	if service.Status != "active" {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Service is not active"})
		return
	}

	// 检查访问权限
	if !service.PublicAccess && tenantID == 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Authentication required"})
		return
	}

	// 检查 REST API 是否启用
	if !service.IsRESTAPIEnabled() {
		c.JSON(http.StatusNotImplemented, gin.H{"error": "REST API is not enabled for this service"})
		return
	}

	// 解析查询参数
	var params svc.QueryParams
	if err := c.ShouldBindQuery(&params); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid query parameters: " + err.Error()})
		return
	}

	// 设置默认值
	if params.Page < 1 {
		params.Page = 1
	}
	if params.PageSize <= 0 {
		params.PageSize = 50
	}
	if params.Format == "" {
		params.Format = "json"
	}

	// 执行查询
	result, err := h.executorSvc.ExecuteQuery(c.Request.Context(), service, &params)
	if err != nil {
		log.Printf("[QueryService] Query execution failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Query execution failed: " + err.Error()})
		return
	}

	// 根据格式返回结果
	switch params.Format {
	case "csv":
		// 转换为 CSV 格式
		rows, ok := result.Data.([]map[string]interface{})
		if !ok {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Invalid data format for CSV export"})
			return
		}
		csvData, err := h.executorSvc.FormatAsCSV(rows)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to format as CSV: " + err.Error()})
			return
		}
		c.Header("Content-Type", "text/csv")
		c.Header("Content-Disposition", "attachment; filename="+serviceName+".csv")
		c.Data(http.StatusOK, "text/csv", csvData)

	case "geojson":
		// 转换为 GeoJSON 格式
		rows, ok := result.Data.([]map[string]interface{})
		if !ok {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Invalid data format for GeoJSON export"})
			return
		}
		geojsonData, err := h.executorSvc.FormatAsGeoJSON(rows, service)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to format as GeoJSON: " + err.Error()})
			return
		}
		c.Header("Content-Type", "application/geo+json")
		c.Data(http.StatusOK, "application/geo+json", geojsonData)

	default: // json
		c.JSON(http.StatusOK, result)
	}
}

// parseQueryParams 解析查询参数
func (h *QueryServiceHandler) parseQueryParams(c *gin.Context, configType string) map[string]interface{} {
	params := make(map[string]interface{})

	// 通用参数（所有模式都支持）
	if page := c.Query("page"); page != "" {
		if parsed, err := strconv.Atoi(page); err == nil && parsed > 0 {
			params["page"] = parsed
		} else {
			params["page"] = 1
		}
	} else {
		params["page"] = 1
	}

	if pageSize := c.Query("page_size"); pageSize != "" {
		if parsed, err := strconv.Atoi(pageSize); err == nil && parsed > 0 && parsed <= 1000 {
			params["page_size"] = parsed
		} else {
			params["page_size"] = 50
		}
	} else {
		params["page_size"] = 50
	}

	if format := c.Query("format"); format != "" {
		params["format"] = format
	} else {
		params["format"] = "json"
	}

	// Table 模式支持的额外参数
	if configType == "table" {
		if filter := c.Query("filter"); filter != "" {
			params["filter"] = filter
		}
		if fields := c.Query("fields"); fields != "" {
			params["fields"] = fields
		}
		if orderBy := c.Query("orderBy"); orderBy != "" {
			params["order_by"] = orderBy
		}
	}

	return params
}
