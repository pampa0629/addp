package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"

	commonapi "github.com/addp/common/api"
	commoni18n "github.com/addp/common/middleware/i18n"
	servicei18n "github.com/addp/service/i18n"
	"github.com/addp/service/internal/models"
	svc "github.com/addp/service/internal/service"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// QueryServiceHandler 处理查询服务相关的 HTTP 请求
type QueryServiceHandler struct {
	svc                  *svc.QueryServiceService
	executorSvc          *svc.QueryExecutorService
	executionAuditWriter QueryExecutionAuditWriter
}

func (h *QueryServiceHandler) SetExecutionAuditWriter(writer QueryExecutionAuditWriter) {
	h.executionAuditWriter = writer
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
// @Summary 创建查询服务 | Create query service
// @Tags QueryService
// @Accept json
// @Produce json
// @Param request body models.CreateQueryServiceRequest true "创建请求 | Create request"
// @Success 201 {object} map[string]interface{}
// @Failure 400 {object} map[string]string
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["service.definition.create"]
// @Router /query [post]
// @Security BearerAuth
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
	tenantID := tenantIDValue(c)
	userID := userIDValue(c)

	if tenantID == 0 || userID == 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Missing tenant_id or user_id in token"})
		return
	}

	result, err := h.svc.CreateService(c.Request.Context(), &req, tenantID, userID)
	if err != nil {
		// 区分不同的错误类型
		if errors.Is(err, gorm.ErrRecordNotFound) || errors.Is(err, commonapi.ErrNotFound) {
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
// @Summary 查询服务列表 | List query services
// @Tags QueryService
// @Produce json
// @Param page query int false "页码 | Page" default(1)
// @Param limit query int false "每页数量 | Limit" default(20)
// @Param search query string false "搜索词 | Search"
// @Success 200 {object} map[string]interface{}
// @Failure 500 {object} map[string]string
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["service.definition.read"]
// @Router /query [get]
// @Security BearerAuth
func (h *QueryServiceHandler) ListServices(c *gin.Context) {
	tenantID := tenantIDValue(c)
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
// @Summary 获取查询服务详情 | Get query service
// @Tags QueryService
// @Produce json
// @Param id path int true "服务ID | Service ID"
// @Success 200 {object} map[string]interface{}
// @Failure 404 {object} map[string]string
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["service.definition.read"]
// @Router /query/{id} [get]
// @Security BearerAuth
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
// @Summary 更新查询服务 | Update query service
// @Tags QueryService
// @Accept json
// @Produce json
// @Param id path int true "服务ID | Service ID"
// @Param request body models.UpdateQueryServiceRequest true "更新请求 | Update request"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["service.definition.update"]
// @Router /query/{id} [put]
// @Security BearerAuth
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
// @Summary 删除查询服务 | Delete query service
// @Tags QueryService
// @Produce json
// @Param id path int true "服务ID | Service ID"
// @Success 200 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 500 {object} map[string]string
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["service.definition.delete"]
// @Router /query/{id} [delete]
// @Security BearerAuth
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

// CheckSourceSnapshot 检查查询服务依赖快照。
// @Summary 检查查询服务依赖快照 | Check query service dependency snapshot
// @Description 仅在显式管理动作中读取 Meta 当前事实并与已发布快照比较 | Read current Meta facts only during an explicit management action and compare them with the published snapshot
// @Tags QueryService
// @Produce json
// @Param id path int true "服务ID | Service ID"
// @Success 200 {object} models.QueryServiceSnapshotDiff "快照差异 | Snapshot diff"
// @Failure 400 {object} map[string]string "请求错误 | Bad request"
// @Failure 404 {object} map[string]string "服务不存在 | Service not found"
// @Failure 500 {object} map[string]string "检查失败 | Check failed"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["service.definition.read"]
// @Router /query/{id}/source-snapshot-diff [get]
// @Security BearerAuth
func (h *QueryServiceHandler) CheckSourceSnapshot(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, commoni18n.MsgInvalidID)})
		return
	}
	result, err := h.svc.CheckSourceSnapshot(uint(id), tenantIDValue(c))
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, gorm.ErrRecordNotFound) {
			status = http.StatusNotFound
		}
		c.JSON(status, gin.H{"error": commoni18n.TWithDetail(c, servicei18n.MsgSnapshotCheckFailed, err.Error())})
		return
	}
	c.JSON(http.StatusOK, result)
}

// RefreshSourceSnapshot 刷新查询服务依赖快照。
// @Summary 刷新查询服务依赖快照 | Refresh query service dependency snapshot
// @Description 用 Meta 当前事实替换表模式查询服务已发布快照 | Replace a table-mode query service snapshot with current Meta facts
// @Tags QueryService
// @Produce json
// @Param id path int true "服务ID | Service ID"
// @Success 200 {object} models.QueryServiceDTO "刷新后的查询服务 | Refreshed query service"
// @Failure 400 {object} map[string]string "请求错误 | Bad request"
// @Failure 404 {object} map[string]string "服务不存在 | Service not found"
// @Failure 500 {object} map[string]string "刷新失败 | Refresh failed"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["service.definition.update"]
// @Router /query/{id}/refresh-source-snapshot [post]
// @Security BearerAuth
func (h *QueryServiceHandler) RefreshSourceSnapshot(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, commoni18n.MsgInvalidID)})
		return
	}
	result, err := h.svc.RefreshSourceSnapshot(uint(id), tenantIDValue(c))
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, gorm.ErrRecordNotFound) {
			status = http.StatusNotFound
		}
		c.JSON(status, gin.H{"error": commoni18n.TWithDetail(c, servicei18n.MsgSnapshotRefreshFailed, err.Error())})
		return
	}
	c.JSON(http.StatusOK, result)
}

// ===== REST 查询 API =====

// QueryData 查询服务数据
// @Summary 执行查询服务 | Execute query service
// @Tags QueryExecution
// @Accept json
// @Produce json
// @Param serviceName path string true "服务名称 | Service name"
// @Param X-ADDP-Query-Intent header string false "查询用途 | Query intent" Enums(query,export) default(query)
// @Param request body models.QueryExecutionRequest true "结构化查询请求 | Structured query request"
// @Success 200 {object} models.QueryExecutionResult
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Failure 403 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 500 {object} map[string]string
// @x-addp-auth-mode "public"
// @Router /api/query/{serviceName}/query [post]
func (h *QueryServiceHandler) QueryData(c *gin.Context) {
	ensureQueryRequestID(c)
	serviceName := c.Param("serviceName")

	// 从 JWT token 中获取租户 ID（如果是公开服务则可能没有）
	tenantID := tenantIDValue(c)

	// 先通过服务名称查找服务(不过滤租户),然后检查权限
	service, err := h.svc.GetServiceModelByNameOnly(serviceName)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) || errors.Is(err, commonapi.ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": commoni18n.T(c, servicei18n.MsgServiceNotFound), "error_code": "service_not_found"})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": commoni18n.T(c, servicei18n.MsgServiceLookupFailed), "error_code": "service_lookup_failed"})
		}
		return
	}
	auditState := &queryExecutionAuditState{
		service: service, intent: "query", serviceVersion: svc.QueryServiceVersion(service),
	}
	defer h.writeQueryExecutionAudit(c, auditState)
	intentHeaders := c.Request.Header.Values(svc.ConsumerQueryIntentHeader)
	intent := ""
	if len(intentHeaders) == 1 {
		intent = strings.ToLower(strings.TrimSpace(intentHeaders[0]))
	}
	if intent == "" {
		intent = "query"
	}
	auditState.intent = intent
	if len(intentHeaders) > 1 || (intent != "query" && intent != "export") {
		auditState.errorCode = "invalid_query_intent"
		c.JSON(http.StatusBadRequest, gin.H{
			"error": commoni18n.T(c, servicei18n.MsgInvalidQueryIntent), "error_code": auditState.errorCode,
		})
		return
	}

	// 检查服务状态
	if service.Status != "active" {
		auditState.errorCode = "service_inactive"
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": commoni18n.T(c, servicei18n.MsgServiceInactive), "error_code": auditState.errorCode})
		return
	}

	// 检查访问权限
	if !service.PublicAccess {
		// 非公开服务需要认证且租户匹配
		if tenantID == 0 {
			auditState.errorCode = "authentication_required"
			c.JSON(http.StatusUnauthorized, gin.H{"error": commoni18n.T(c, commoni18n.MsgUnauthorized), "error_code": auditState.errorCode})
			return
		}
		if service.TenantID != tenantID {
			auditState.errorCode = "service_access_denied"
			c.JSON(http.StatusForbidden, gin.H{"error": commoni18n.T(c, commoni18n.MsgForbidden), "error_code": auditState.errorCode})
			return
		}
		if !hasTenantQueryExecutionPermission(c, tenantID) {
			auditState.errorCode = "permission_denied"
			c.JSON(http.StatusForbidden, gin.H{
				"error": commoni18n.T(c, commoni18n.MsgForbidden), "error_code": auditState.errorCode,
			})
			return
		}
	}

	// 检查 REST API 是否启用
	if !service.IsRESTAPIEnabled() {
		auditState.errorCode = "rest_api_disabled"
		c.JSON(http.StatusNotImplemented, gin.H{"error": commoni18n.T(c, servicei18n.MsgRESTAPIDisabled), "error_code": auditState.errorCode})
		return
	}

	var request models.QueryExecutionRequest
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	decoder.UseNumber()
	if err := decoder.Decode(&request); err != nil {
		auditState.errorCode = "invalid_query_request"
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.TWithDetail(c, servicei18n.MsgInvalidQueryRequest, err.Error()), "error_code": auditState.errorCode})
		return
	}
	auditState.request = &request
	var trailing interface{}
	if err := decoder.Decode(&trailing); err != io.EOF {
		auditState.errorCode = "invalid_query_request"
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, servicei18n.MsgInvalidQueryRequest), "error_code": auditState.errorCode})
		return
	}
	request.Format = strings.ToLower(strings.TrimSpace(request.Format))
	if request.Format == "" {
		request.Format = "json"
	}
	if !svc.QueryServiceSupportsRESTFormat(service, request.Format) {
		auditState.errorCode = "invalid_query_format"
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, servicei18n.MsgInvalidQueryFormat), "error_code": auditState.errorCode})
		return
	}

	result, err := h.executorSvc.ExecuteQuery(c.Request.Context(), service, &request)
	if err != nil {
		log.Printf("[QueryService] Query execution failed: %v", err)
		auditState.errorCode = queryExecutionErrorCode(err)
		writeQueryExecutionError(c, err)
		return
	}
	auditState.serviceVersion = result.ServiceVersion
	auditState.rowCount = len(result.Data)
	auditState.hasMore = result.Page.HasMore

	switch request.Format {
	case "csv":
		csvData, err := h.executorSvc.FormatAsCSV(result)
		if err != nil {
			auditState.errorCode = "query_format_failed"
			c.JSON(http.StatusInternalServerError, gin.H{"error": commoni18n.TWithDetail(c, servicei18n.MsgQueryFormatFailed, err.Error()), "error_code": auditState.errorCode})
			return
		}
		c.Header("Content-Type", "text/csv")
		c.Header("Content-Disposition", "attachment; filename="+serviceName+".csv")
		c.Header("X-ADDP-Has-More", strconv.FormatBool(result.Page.HasMore))
		c.Header("X-ADDP-Next-Cursor", result.Page.NextCursor)
		c.Header("X-ADDP-Service-Version", result.ServiceVersion)
		c.Data(http.StatusOK, "text/csv", csvData)

	case "geojson":
		geojsonData, err := h.executorSvc.FormatAsGeoJSON(result, service)
		if err != nil {
			auditState.errorCode = "query_format_failed"
			c.JSON(http.StatusInternalServerError, gin.H{"error": commoni18n.TWithDetail(c, servicei18n.MsgQueryFormatFailed, err.Error()), "error_code": auditState.errorCode})
			return
		}
		c.Header("Content-Type", "application/geo+json")
		c.Header("X-ADDP-Has-More", strconv.FormatBool(result.Page.HasMore))
		c.Header("X-ADDP-Next-Cursor", result.Page.NextCursor)
		c.Header("X-ADDP-Service-Version", result.ServiceVersion)
		c.Data(http.StatusOK, "application/geo+json", geojsonData)

	default: // json
		c.JSON(http.StatusOK, result)
	}
	auditState.result = "succeeded"
}

func writeQueryExecutionError(c *gin.Context, err error) {
	errorCode := queryExecutionErrorCode(err)
	if errors.Is(err, svc.ErrInvalidStructuredQuery) || errors.Is(err, svc.ErrInvalidQueryCursor) || errors.Is(err, svc.ErrInvalidFeatureID) {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.TWithDetail(c, servicei18n.MsgInvalidStructuredQuery, err.Error()), "error_code": errorCode})
		return
	}
	c.JSON(http.StatusInternalServerError, gin.H{"error": commoni18n.TWithDetail(c, servicei18n.MsgQueryExecutionFailed, err.Error()), "error_code": errorCode})
}

func queryExecutionErrorCode(err error) string {
	if errors.Is(err, svc.ErrInvalidStructuredQuery) || errors.Is(err, svc.ErrInvalidQueryCursor) || errors.Is(err, svc.ErrInvalidFeatureID) {
		return "invalid_structured_query"
	}
	return "query_execution_failed"
}
