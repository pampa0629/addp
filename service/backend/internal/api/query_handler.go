package api

import (
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
	"github.com/gin-gonic/gin/binding"
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
// @Success 201 {object} models.QueryServiceDTO
// @Failure 400 {object} map[string]string
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["service.definition.create"]
// @Router /query [post]
// @Security BearerAuth
func (h *QueryServiceHandler) CreateService(c *gin.Context) {
	var req models.CreateQueryServiceRequest

	if err := bindQueryDefinition(c, &req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, servicei18n.MsgInvalidQueryRequest), "error_code": "invalid_query_request"})
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
		if errors.Is(err, svc.ErrInvalidParameterOptions) {
			c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, servicei18n.MsgInvalidParameterOptions)})
		} else if errors.Is(err, commonapi.ErrNotFound) {
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
// @Param metric_implementation_id query int false "指标实现 ID，须与修订 ID 同时提供 | Metric implementation ID, paired with revision ID" minimum(1)
// @Param metric_revision_id query int false "精确修订 ID，须与实现 ID 同时提供 | Exact revision ID, paired with implementation ID" minimum(1)
// @Failure 400 {object} map[string]string
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

	metricSource, err := queryServiceMetricFilter(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, servicei18n.MsgInvalidQueryRequest), "error_code": "invalid_query_request"})
		return
	}
	results, total, err := h.svc.ListServices(tenantID, offset, limit, models.QueryServiceListFilter{Search: c.Query("search"), MetricSource: metricSource})

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

func queryServiceMetricFilter(c *gin.Context) (*models.MetricSourceRequest, error) {
	values := c.Request.URL.Query()
	implementation, hasImplementation := values["metric_implementation_id"]
	revision, hasRevision := values["metric_revision_id"]
	if !hasImplementation && !hasRevision {
		return nil, nil
	}
	if len(implementation) != 1 || len(revision) != 1 {
		return nil, errors.New("invalid metric reference")
	}
	parseID := func(raw string) (int64, error) {
		for _, ch := range raw {
			if ch < '0' || ch > '9' {
				return 0, errors.New("invalid metric reference")
			}
		}
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || id <= 0 {
			return 0, errors.New("invalid metric reference")
		}
		return id, nil
	}
	implementationID, err := parseID(implementation[0])
	if err != nil {
		return nil, err
	}
	revisionID, err := parseID(revision[0])
	if err != nil {
		return nil, err
	}
	return &models.MetricSourceRequest{ImplementationID: implementationID, RevisionID: revisionID}, nil
}

// GetService 获取服务详情
// @Summary 获取查询服务详情 | Get query service
// @Tags QueryService
// @Produce json
// @Param id path int true "服务ID | Service ID"
// @Success 200 {object} models.QueryServiceDTO
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
		if errors.Is(err, commonapi.ErrNotFound) {
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
// @Success 200 {object} models.QueryServiceDTO
// @Failure 400 {object} map[string]string
// @Failure 409 {object} map[string]string "资源版本冲突 | Resource version conflict"
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
	if err := bindQueryDefinition(c, &req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body: " + err.Error()})
		return
	}

	result, err := h.svc.UpdateService(c.Request.Context(), uint(id), tenantIDValue(c), &req)
	if err != nil {
		if writeQueryVersionConflict(c, err) {
			return
		}
		if errors.Is(err, commonapi.ErrNotFound) {
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
// @Accept json
// @Produce json
// @Param id path int true "服务ID | Service ID"
// @Param request body models.QueryServiceVersionRequest true "当前资源版本 | Current resource version"
// @Success 200 {object} map[string]string
// @Failure 409 {object} map[string]string "资源版本冲突 | Resource version conflict"
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

	var req models.QueryServiceVersionRequest
	if err := bindQueryDefinition(c, &req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, servicei18n.MsgInvalidQueryRequest), "error_code": "invalid_query_request"})
		return
	}
	if err := h.svc.DeleteService(c.Request.Context(), uint(id), tenantIDValue(c), req.Version); err != nil {
		if writeQueryVersionConflict(c, err) {
			return
		}
		if errors.Is(err, commonapi.ErrNotFound) {
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
		if errors.Is(err, commonapi.ErrNotFound) {
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
// @Accept json
// @Produce json
// @Param id path int true "服务ID | Service ID"
// @Param request body models.QueryServiceVersionRequest true "当前资源版本 | Current resource version"
// @Success 200 {object} models.QueryServiceDTO "刷新后的查询服务 | Refreshed query service"
// @Failure 400 {object} map[string]string "请求错误 | Bad request"
// @Failure 409 {object} map[string]string "资源版本冲突 | Resource version conflict"
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
	var req models.QueryServiceVersionRequest
	if err := bindQueryDefinition(c, &req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, servicei18n.MsgInvalidQueryRequest), "error_code": "invalid_query_request"})
		return
	}
	result, err := h.svc.RefreshSourceSnapshot(c.Request.Context(), uint(id), tenantIDValue(c), req.Version)
	if err != nil {
		if writeQueryVersionConflict(c, err) {
			return
		}
		status := http.StatusBadRequest
		if errors.Is(err, commonapi.ErrNotFound) {
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
		if errors.Is(err, commonapi.ErrNotFound) {
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

	// API Consumer Credential 始终要求当前 Tenant 与精确 Query Service Grant 匹配；
	// 未携带 API Consumer Credential 时继续执行公开访问或 User AuthContext 路径。
	if status, apiConsumerRequest := apiConsumerServiceAccessStatus(
		c, service.TenantID, models.ConsumerServiceTypeQuery, service.ID,
	); apiConsumerRequest {
		if status != 0 {
			auditState.errorCode = "api_consumer_service_denied"
			c.JSON(status, gin.H{
				"error": commoni18n.T(c, commoni18n.MsgForbidden), "error_code": auditState.errorCode,
			})
			return
		}
	} else if !service.PublicAccess {
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
	if errors.Is(err, svc.ErrInvalidParameterOptions) {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, servicei18n.MsgInvalidParameterOptions), "error_code": queryExecutionErrorCode(err)})
		return
	}
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

// RebindMetricSource 显式切换查询服务的指标来源修订。
// @Summary 切换指标来源修订 | Rebind metric source revision
// @Description 保留服务身份，校验资源 version 后原子替换计算契约并递增版本 | Preserve service identity and atomically replace the computation contract after checking and incrementing the resource version
// @Tags QueryService
// @Accept json
// @Produce json
// @Param id path int true "服务 ID | Service ID"
// @Param request body models.RebindMetricSourceRequest true "指标绑定与当前定义并发版本 | Metric binding and current definition concurrency version"
// @Success 200 {object} models.QueryServiceDTO
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 409 {object} map[string]string
// @Failure 500 {object} map[string]string
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["service.definition.update"]
// @Router /query/{id}/metric-source [put]
// @Security BearerAuth
func (h *QueryServiceHandler) RebindMetricSource(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	var req models.RebindMetricSourceRequest
	if err != nil || id == 0 || bindQueryDefinition(c, &req) != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, servicei18n.MsgInvalidQueryRequest), "error_code": "invalid_metric_publication"})
		return
	}
	result, err := h.svc.RebindMetricSource(c.Request.Context(), uint(id), tenantIDValue(c), &req)
	if err != nil {
		status, key, code := http.StatusInternalServerError, servicei18n.MsgMetricPublicationFailed, "metric_publication_failed"
		if errors.Is(err, svc.ErrInvalidStructuredQuery) || errors.Is(err, svc.ErrInvalidConsumerContract) {
			status, key, code = http.StatusBadRequest, servicei18n.MsgInvalidStructuredQuery, "invalid_metric_publication"
		}
		if errors.Is(err, commonapi.ErrNotFound) {
			status, key, code = http.StatusNotFound, servicei18n.MsgServiceNotFound, "service_not_found"
		}
		if errors.Is(err, svc.ErrInvalidParameterOptions) {
			status, key, code = http.StatusBadRequest, servicei18n.MsgInvalidParameterOptions, "invalid_metric_publication"
		}
		if errors.Is(err, commonapi.ErrConflict) {
			status, key, code = http.StatusConflict, servicei18n.MsgQueryVersionConflict, "resource_version_conflict"
		}
		c.JSON(status, gin.H{"error": commoni18n.T(c, key), "error_code": code})
		return
	}
	c.JSON(http.StatusOK, result)
}

// Publications accept owner references, never arbitrary plan or source overrides.
func bindQueryDefinition(c *gin.Context, destination interface{}) error {
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	decoder.UseNumber()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return errors.New("expected a single JSON object")
	}
	return binding.Validator.ValidateStruct(destination)
}

func writeQueryVersionConflict(c *gin.Context, err error) bool {
	if !errors.Is(err, commonapi.ErrConflict) {
		return false
	}
	c.JSON(http.StatusConflict, gin.H{"error": commoni18n.T(c, servicei18n.MsgQueryVersionConflict), "error_code": "resource_version_conflict"})
	return true
}
