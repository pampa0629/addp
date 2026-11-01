package api

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	commonapi "github.com/addp/common/api"
	commonClient "github.com/addp/common/client"
	commonAuth "github.com/addp/common/middleware/auth"
	commoni18n "github.com/addp/common/middleware/i18n"
	metai18n "github.com/addp/meta/i18n"
	metaErrors "github.com/addp/meta/internal/errors"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/service"
	"github.com/gin-gonic/gin"
)

type Handler struct {
	metadataVisibility    metadataVisibilityClient
	lineageVisibility     lineageVisibilityClient
	scanManagementScope   scanManagementScopeClient
	engineService         *service.EngineService
	scanService           *service.ScanService
	taskService           *service.ScanTaskService
	executionService      *service.ScanExecutionService
	metadataQueryService  *service.MetadataQueryService
	lineageService        *service.LineageService
	resourceTreeService   *service.ResourceTreeService
	inspectService        *service.InspectService
	dataItemChangeService *service.DataItemChangeService
}

func NewHandler(
	engineService *service.EngineService,
	scanService *service.ScanService,
	taskService *service.ScanTaskService,
	executionService *service.ScanExecutionService,
	metadataQueryService *service.MetadataQueryService,
	inspectService *service.InspectService,
	lineageServices ...*service.LineageService,
) *Handler {
	var lineageService *service.LineageService
	if len(lineageServices) > 0 {
		lineageService = lineageServices[0]
	}
	return &Handler{
		engineService:        engineService,
		scanService:          scanService,
		taskService:          taskService,
		executionService:     executionService,
		metadataQueryService: metadataQueryService,
		lineageService:       lineageService,
		resourceTreeService:  service.NewResourceTreeService(engineService, metadataQueryService),
		inspectService:       inspectService,
	}
}

func validateManualScanRequestTriggerType(triggerType string) error {
	normalized := strings.ToLower(strings.TrimSpace(triggerType))
	if normalized == "" {
		return nil
	}
	if normalized == models.TriggerTypeManual {
		return nil
	}
	return fmt.Errorf("unsupported trigger_type %q: use manual", triggerType)
}

// handleServiceError 统一处理 Service 层错误，返回合适的 HTTP 状态码
func (h *Handler) handleServiceError(c *gin.Context, err error) {
	if errors.Is(err, commonClient.ErrMetadataCredentialRejected) {
		commonapi.UnauthorizedError(c, commoni18n.T(c, commoni18n.MsgUnauthorized))
		return
	}
	if errors.Is(err, commonClient.ErrMetadataQueryUnavailable) {
		commonapi.ErrorResponse(c, http.StatusServiceUnavailable, commoni18n.T(c, commoni18n.MsgAuthorizationServiceUnavailable))
		return
	}
	if errors.Is(err, commonapi.ErrForbidden) || errors.Is(err, commonClient.ErrMetadataQueryDenied) {
		commonapi.ForbiddenError(c, commoni18n.T(c, commoni18n.MsgForbidden))
		return
	}
	if errors.Is(err, metaErrors.ErrInvalidDelimitedOptions) {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, metai18n.MsgScanDelimitedOptionsInvalid), "error_code": "scan_delimited_options_invalid"})
		return
	}
	if errors.Is(err, metaErrors.ErrScanScopeActive) {
		c.JSON(http.StatusConflict, gin.H{
			"error":      commoni18n.T(c, metai18n.MsgScanScopeActive),
			"error_code": "scan_scope_active",
		})
		return
	}
	statusCode := metaErrors.HTTPStatusCode(err)
	message := metaErrors.ErrorMessage(err)
	c.JSON(statusCode, gin.H{"error": message})
}

// GetStats 获取元数据统计
// @Summary 获取元数据统计 | Get metadata stats
// @Description 获取当前租户有效元数据项数；PG 用户仅统计当前可见数据项 | Count active tenant metadata items; PG user counts include only currently visible items
// @Tags Meta
// @Produce json
// @Success 200 {object} map[string]interface{} "统计信息 | Stats"
// @Failure 500 {object} map[string]interface{} "服务器内部错误 | Internal server error"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["meta.catalog.read"]
// @Failure 401 {object} map[string]interface{} "认证失效 | Invalid credential"
// @Failure 403 {object} map[string]interface{} "无查询权限 | Query forbidden"
// @Failure 503 {object} map[string]interface{} "授权核验不可用 | Authorization check unavailable"
// @Router /stats [get]
// @Security BearerAuth
func (h *Handler) GetStats(c *gin.Context) {
	tenantID := commonAuth.GetTenantID(c)
	itemCount, err := h.resourceTreeService.CountVisibleMetadataItems(h.metadataBrowsingContext(c), tenantID)
	if err != nil {
		h.respondMetadataQueryError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"total": itemCount})
}

// GetResources 获取存储引擎列表及 catalog 扫描统计
// GET /api/v1/meta/engines
// @Summary 获取引擎列表 | Get engine list
// @Description 获取当前租户 active 存储引擎选择项、标准能力声明、连接状态及统计信息；PG 用户扫描统计仅计可见分支；非 online 项由前端展示并禁选 | Get active storage engine options with canonical capabilities, connection status, and statistics for the current tenant; PG user scan statistics include visible branches only; clients must show but disable non-online options
// @Tags Meta
// @Produce json
// @Success 200 {array} models.ResourceWithStats "引擎列表 | Engine list"
// @Failure 500 {object} map[string]interface{} "服务器内部错误 | Internal server error"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["meta.catalog.read"]
// @Failure 401 {object} map[string]interface{} "认证失效 | Invalid credential"
// @Failure 403 {object} map[string]interface{} "无查询权限 | Query forbidden"
// @Failure 503 {object} map[string]interface{} "授权核验不可用 | Authorization check unavailable"
// @Router /engines [get]
// @Security BearerAuth
func (h *Handler) GetEngines(c *gin.Context) {
	tenantID := commonAuth.GetTenantID(c)

	engines, err := h.engineService.GetEnginesWithStats(tenantID)
	if err != nil {
		h.respondMetadataQueryError(c, err)
		return
	}

	engines, err = h.resourceTreeService.FilterMetadataEngineStats(h.metadataBrowsingContext(c), tenantID, engines)
	if err != nil {
		h.respondMetadataQueryError(c, err)
		return
	}
	c.JSON(http.StatusOK, engines)
}

func (h *Handler) effectiveTenantIDForEngine(c *gin.Context, _ uint) (uint, error) {
	tenantID := commonAuth.GetTenantID(c)
	if tenantID != 0 {
		return tenantID, nil
	}
	return 0, fmt.Errorf("tenant context is required")
}
