package api

import (
	"fmt"
	"net/http"
	"strings"

	commonAuth "github.com/addp/common/middleware/auth"
	metaErrors "github.com/addp/meta/internal/errors"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/service"
	"github.com/gin-gonic/gin"
)

type Handler struct {
	engineService        *service.EngineService
	scanService          *service.ScanService
	taskService          *service.ScanTaskService
	executionService     *service.ScanExecutionService
	metadataQueryService *service.MetadataQueryService
	resourceTreeService  *service.ResourceTreeService
	inspectService       *service.InspectService
}

func NewHandler(
	engineService *service.EngineService,
	scanService *service.ScanService,
	taskService *service.ScanTaskService,
	executionService *service.ScanExecutionService,
	metadataQueryService *service.MetadataQueryService,
	inspectService *service.InspectService,
) *Handler {
	return &Handler{
		engineService:        engineService,
		scanService:          scanService,
		taskService:          taskService,
		executionService:     executionService,
		metadataQueryService: metadataQueryService,
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
	statusCode := metaErrors.HTTPStatusCode(err)
	message := metaErrors.ErrorMessage(err)
	c.JSON(statusCode, gin.H{"error": message})
}

// GetStats 获取元数据统计
// @Summary 获取元数据统计 | Get metadata stats
// @Description 获取当前租户的元数据项总数 | Get metadata item count for current tenant
// @Tags Meta
// @Produce json
// @Success 200 {object} map[string]interface{} "统计信息 | Stats"
// @Failure 500 {object} map[string]interface{} "服务器内部错误 | Internal server error"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["meta.catalog.read"]
// @Router /stats [get]
// @Security BearerAuth
func (h *Handler) GetStats(c *gin.Context) {
	tenantID := commonAuth.GetTenantID(c)
	itemCount, err := h.metadataQueryService.CountItems(tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"total": itemCount})
}

// GetResources 获取存储引擎列表及 catalog 扫描统计
// GET /api/v1/meta/engines
// @Summary 获取引擎列表 | Get engine list
// @Description 获取当前租户的存储引擎列表及统计信息 | Get storage engine list with statistics for the current tenant
// @Tags Meta
// @Produce json
// @Success 200 {array} models.ResourceWithStats "引擎列表 | Engine list"
// @Failure 500 {object} map[string]interface{} "服务器内部错误 | Internal server error"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["meta.catalog.read"]
// @Router /engines [get]
// @Security BearerAuth
func (h *Handler) GetEngines(c *gin.Context) {
	tenantID := commonAuth.GetTenantID(c)

	engines, err := h.engineService.GetEnginesWithStats(tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, engines)
}

func extractBearerToken(c *gin.Context) (string, bool) {
	token := c.GetHeader("Authorization")
	if token == "" {
		return "", false
	}
	if len(token) > 7 && strings.HasPrefix(token, "Bearer ") {
		token = token[7:]
	}
	return token, token != ""
}

func (h *Handler) effectiveTenantIDForEngine(c *gin.Context, engineID uint) (uint, error) {
	tenantID := commonAuth.GetTenantID(c)
	if tenantID != 0 {
		return tenantID, nil
	}

	token, _ := extractBearerToken(c)
	engine, err := h.engineService.GetResourceByID(engineID, tenantID, token)
	if err != nil {
		return 0, err
	}
	if engine.TenantID != nil && *engine.TenantID > 0 {
		return *engine.TenantID, nil
	}
	return tenantID, nil
}
