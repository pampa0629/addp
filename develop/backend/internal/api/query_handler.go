package api

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	commonAuth "github.com/addp/common/middleware/auth"
	commoni18n "github.com/addp/common/middleware/i18n"
	"github.com/addp/common/resourcetree"
	developi18n "github.com/addp/develop/backend/i18n"
	developauthorization "github.com/addp/develop/backend/internal/authorization"
	"github.com/addp/develop/backend/internal/models"
	"github.com/addp/develop/backend/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// QueryHandler 查询开发 API 处理器
type QueryHandler struct {
	sqlEngine *service.SQLEngineService
	federated *service.FederatedQueryService
}

// PreflightQuery 分析查询语句并返回执行前的权限与风险事实。
// @Summary 查询执行预检 | Preflight a query
// @Tags Query
// @Accept json
// @Produce json
// @Param body body models.QueryPreflightRequest true "查询预检请求 | Query preflight request"
// @Success 200 {object} models.QueryPreflightResponse "预检结果 | Preflight result"
// @Failure 400 {object} models.ErrorResponse "查询语句无效 | Invalid query"
// @Failure 401 {object} models.ErrorResponse "需要登录 | Authentication required"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["develop.task.execute"]
// @Router /query-preflight [post]
// @Security BearerAuth
func (h *QueryHandler) PreflightQuery(c *gin.Context) {
	var req models.QueryPreflightRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "error_code": "invalid_query_preflight_request"})
		return
	}
	if locator := strings.TrimSpace(req.TargetLocator); locator != "" {
		parsed, parseErr := resourcetree.ParseURI(locator)
		if parseErr != nil || parsed.EngineID != req.EngineID {
			c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, developi18n.MsgQueryTemplateResourceInvalid), "error_code": "query_target_invalid"})
			return
		}
	}
	queryType := strings.ToLower(strings.TrimSpace(req.QueryType))
	if queryType == "" {
		queryType = "sql"
	}
	analysis, err := service.AnalyzeQuery(queryType, req.Query)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": commoni18n.T(c, developi18n.MsgSQLClassificationFailed), "error_code": "query_effect_unclassifiable", "details": err.Error(),
		})
		return
	}

	permissionKey := developauthorization.PermissionDevelopDataReadExecute
	switch service.SQLExecutionEffect(analysis.Effect) {
	case service.SQLExecutionEffectWrite:
		permissionKey = developauthorization.PermissionDevelopDataWriteExecute
	case service.SQLExecutionEffectDDL:
		permissionKey = developauthorization.PermissionDevelopDataDdlExecute
	case service.SQLExecutionEffectExternalEffect:
		permissionKey = developauthorization.PermissionDevelopDataExternalEffectExecute
	}
	allowed := commonAuth.HasRolePermission(c, permissionKey)
	if analysis.ClassificationConfidence == "unknown" && service.SQLExecutionEffect(analysis.Effect) != service.SQLExecutionEffectRead && strings.TrimSpace(req.TargetLocator) == "" {
		allowed = false
	}
	response := models.QueryPreflightResponse{
		Allowed:                  allowed,
		Effect:                   analysis.Effect,
		Statement:                analysis.Statement,
		ClassificationConfidence: analysis.ClassificationConfidence,
		TargetObjects:            analysis.TargetObjects,
		TargetLocator:            strings.TrimSpace(req.TargetLocator),
		Warnings:                 analysis.Warnings,
		Fingerprint:              analysis.Fingerprint,
		RiskLevel:                analysis.RiskLevel,
		RequiresConfirmation:     analysis.RequiresConfirmation,
		RequiredPermission:       permissionKey,
	}
	if !allowed {
		response.Warnings = append(response.Warnings, commoni18n.T(c, developi18n.MsgExecutionEffectForbidden))
		if analysis.ClassificationConfidence == "unknown" && service.SQLExecutionEffect(analysis.Effect) != service.SQLExecutionEffectRead && strings.TrimSpace(req.TargetLocator) == "" {
			response.Warnings = append(response.Warnings, "target_required")
		}
	}
	if allowed && analysis.RequiresConfirmation {
		token, expiresAt, tokenErr := h.sqlEngine.IssueQueryConfirmationToken(
			tenantIDValue(c), userIDValue(c), req.EngineID, req.TargetLocator, analysis.Fingerprint, analysis.Effect,
		)
		if tokenErr != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": commoni18n.T(c, developi18n.MsgExecutionAuthorizationUnavailable), "error_code": "query_confirmation_unavailable"})
			return
		}
		response.ConfirmationToken = token
		response.ConfirmationExpiresAt = &expiresAt
	}
	c.JSON(http.StatusOK, response)
}

// NewQueryHandler 创建 查询处理器
func NewQueryHandler(sqlEngine *service.SQLEngineService, federated *service.FederatedQueryService) *QueryHandler {
	return &QueryHandler{sqlEngine: sqlEngine, federated: federated}
}

// TestConnectionRequest 测试连接请求
type TestConnectionRequest struct {
	EngineID uint `json:"engine_id" binding:"required"`
}

// GetSampleQuery 获取所选数据资源的可执行查询模板
// @Summary 获取查询模板 | Get query template
// @Tags Query
// @Produce json
// @Param id path int true "引擎ID | Engine ID"
// @Param locator query string false "标准资源定位符，指定后生成该数据项的查询模板 | Standard resource locator; when provided, generate a query template for that data item"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]interface{}
// @Failure 401 {object} map[string]interface{} "需要登录 | Authentication required"
// @Failure 403 {object} map[string]interface{} "无执行或数据读取权限 | Execution or data-read permission denied"
// @Failure 409 {object} map[string]interface{} "授权状态冲突 | Authorization conflict"
// @Failure 422 {object} map[string]interface{} "所选数据项为空或当前业务库没有可用真实样例 | The selected data item is empty or no real sample data is available"
// @Failure 500 {object} map[string]interface{}
// @Failure 503 {object} map[string]interface{} "授权服务不可用 | Authorization service unavailable"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["develop.task.execute","develop.data_read.execute"]
// @Router /engines/{id}/sample-query [get]
// @Security BearerAuth
func (h *QueryHandler) GetSampleQuery(c *gin.Context) {
	idStr := c.Param("id")
	engineID, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的引擎ID"})
		return
	}
	var locator *resourcetree.ResourceLocator
	if locatorValue := strings.TrimSpace(c.Query("locator")); locatorValue != "" {
		locator, err = resourcetree.ParseURI(locatorValue)
		if err != nil || locator.EngineID != uint(engineID) {
			c.JSON(http.StatusBadRequest, gin.H{
				"error":      commoni18n.T(c, developi18n.MsgQueryTemplateResourceInvalid),
				"error_code": "query_template_resource_invalid",
			})
			return
		}
	}

	userAccessToken, err := requestUserAccessToken(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{
			"error": commoni18n.T(c, developi18n.MsgAuthenticationRequired), "error_code": "authentication_required",
		})
		return
	}
	tenantID := tenantIDValue(c)
	if h.federated != nil && h.federated.IsRuntime(c.Request.Context(), tenantID, uint(engineID)) {
		if locator != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error":      commoni18n.T(c, developi18n.MsgQueryTemplateResourceInvalid),
				"error_code": "query_template_resource_invalid",
			})
			return
		}
		h.getFederatedSampleQuery(c, tenantID, uint(engineID), userAccessToken)
		return
	}
	query, language, err := h.sqlEngine.GenerateAuthorizedSampleQuery(
		c.Request.Context(), tenantID, userAccessToken, uuid.New(), uint(engineID),
		locator,
	)
	if err != nil {
		if h.writeExecutionAuthorizationError(c, err) {
			return
		}
		if errors.Is(err, service.ErrSampleQueryResourceEmpty) {
			c.JSON(http.StatusUnprocessableEntity, gin.H{
				"error": commoni18n.T(c, developi18n.MsgSampleQueryResourceEmpty), "error_code": "sample_query_resource_empty",
			})
			return
		}
		if errors.Is(err, service.ErrSampleQueryUnavailable) {
			c.JSON(http.StatusUnprocessableEntity, gin.H{
				"error": commoni18n.T(c, developi18n.MsgSampleQueryUnavailable), "error_code": "sample_query_unavailable",
			})
			return
		}
		if errors.Is(err, service.ErrSampleQueryResourceInvalid) {
			c.JSON(http.StatusBadRequest, gin.H{
				"error":      commoni18n.T(c, developi18n.MsgQueryTemplateResourceInvalid),
				"error_code": "query_template_resource_invalid",
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"query":    query,
		"language": language,
	})
}

func (h *QueryHandler) getFederatedSampleQuery(c *gin.Context, tenantID, runtimeEngineID uint, userAccessToken string) {
	sources, err := h.federated.GetSources(c.Request.Context(), tenantID, runtimeEngineID)
	if err != nil {
		h.writeSQLExecutionError(c, err)
		return
	}
	candidates := h.federated.CandidateQueries(sources)
	var firstFailure error
	for _, candidate := range candidates {
		executionID := uuid.New()
		authorization, issueErr := h.sqlEngine.IssueFederatedReadExecutionAuthorization(
			c.Request.Context(), tenantID, userAccessToken, executionID, []uint{candidate.EngineID}, 30,
		)
		if issueErr != nil {
			if h.writeExecutionAuthorizationError(c, issueErr) {
				return
			}
			if firstFailure == nil {
				firstFailure = issueErr
			}
			continue
		}
		result, executeErr := h.federated.ExecuteQuery(
			c.Request.Context(), tenantID, runtimeEngineID, executionID,
			authorization.AuthorizationID, candidate.Query, 30, 1, []uint{candidate.EngineID},
		)
		if executeErr == nil && result != nil && result.RowCount > 0 {
			c.JSON(http.StatusOK, gin.H{"query": candidate.Query, "language": "sql"})
			return
		}
		if firstFailure == nil {
			if executeErr != nil {
				firstFailure = executeErr
			} else {
				firstFailure = fmt.Errorf("样例查询没有返回数据")
			}
		}
	}
	slog.WarnContext(c.Request.Context(), "DuckDB 样例查询没有可执行候选",
		"runtime_engine_id", runtimeEngineID, "source_count", len(sources),
		"candidate_count", len(candidates), "first_failure", firstFailure)
	c.JSON(http.StatusUnprocessableEntity, gin.H{
		"error": commoni18n.T(c, developi18n.MsgSampleQueryUnavailable), "error_code": "sample_query_unavailable",
	})
}

// TestConnection 测试数据源连接
// @Summary 测试数据源连接 | Test data source connection
// @Tags Query
// @Accept json
// @Produce json
// @Param id path int true "资源ID | Resource ID"
// @Success 200 {object} map[string]string "连接测试成功 | Connection test successful"
// @Failure 400 {object} map[string]interface{} "资源ID无效 | Invalid resource ID"
// @Failure 401 {object} map[string]interface{} "需要登录 | Authentication required"
// @Failure 403 {object} map[string]interface{} "无执行或数据读取权限 | Execution or data-read permission denied"
// @Failure 409 {object} map[string]interface{} "授权状态冲突 | Authorization conflict"
// @Failure 502 {object} map[string]interface{} "数据源连接失败 | Data source connection failed"
// @Failure 503 {object} map[string]interface{} "授权服务不可用 | Authorization service unavailable"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["develop.task.execute","develop.data_read.execute"]
// @Router /test/{id} [get]
func (h *QueryHandler) TestConnection(c *gin.Context) {
	idStr := c.Param("id")
	engineID, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的资源ID"})
		return
	}

	userAccessToken, err := requestUserAccessToken(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{
			"error":      commoni18n.T(c, developi18n.MsgAuthenticationRequired),
			"error_code": "authentication_required",
		})
		return
	}
	if err := h.sqlEngine.TestAuthorizedConnection(
		c.Request.Context(), tenantIDValue(c), userAccessToken, uuid.New(), uint(engineID),
	); err != nil {
		h.writeConnectionTestError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": commoni18n.T(c, developi18n.MsgConnectionTestSuccess),
	})
}

func (h *QueryHandler) writeConnectionTestError(c *gin.Context, err error) {
	if h.writeExecutionAuthorizationError(c, err) {
		return
	}
	status := http.StatusInternalServerError
	if errors.Is(err, service.ErrSQLConnectionTestFailed) {
		status = http.StatusBadGateway
	}
	c.JSON(status, gin.H{
		"error":      commoni18n.T(c, developi18n.MsgConnectionTestFailed),
		"error_code": "connection_test_failed",
	})
}

func (h *QueryHandler) writeSQLExecutionError(c *gin.Context, err error) {
	if errors.Is(err, service.ErrControlledSQLExecutionUnsupported) {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":      commoni18n.T(c, developi18n.MsgControlledSQLEngineUnsupported),
			"error_code": "controlled_sql_engine_unsupported",
		})
		return
	}
	if h.writeExecutionAuthorizationError(c, err) {
		return
	}
	if errors.Is(err, service.ErrSQLExecutionUnclassifiable) {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":      commoni18n.T(c, developi18n.MsgSQLClassificationFailed),
			"error_code": "sql_effect_unclassifiable", "details": err.Error(),
		})
		return
	}
	c.JSON(http.StatusInternalServerError, gin.H{
		"error":      commoni18n.T(c, developi18n.MsgSQLExecutionFailed),
		"error_code": "sql_execution_failed",
	})
}

func (h *QueryHandler) writeExecutionAuthorizationError(c *gin.Context, err error) bool {
	return writeExecutionAuthorizationError(c, err)
}

func requestUserAccessToken(c *gin.Context) (string, error) {
	values := c.Request.Header.Values("Authorization")
	if len(values) != 1 {
		return "", fmt.Errorf("Authorization header must be singular")
	}
	parts := strings.Fields(values[0])
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || !strings.HasPrefix(parts[1], "addp_at_") || len(parts[1]) == len("addp_at_") {
		return "", fmt.Errorf("Authorization header must contain a canonical Bearer token")
	}
	return parts[1], nil
}
