package api

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	commonAuth "github.com/addp/common/middleware/auth"
	metaErrors "github.com/addp/meta/internal/errors"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/service"
	"github.com/gin-gonic/gin"
)

type Handler struct {
	engineService         *service.EngineService
	scanService           *service.ScanService
	taskService           *service.ScanTaskService
	executionService      *service.ScanExecutionService
	metadataQueryService  *service.MetadataQueryService
	objectMetadataService *service.ObjectMetadataService
}

func NewHandler(
	engineService *service.EngineService,
	scanService *service.ScanService,
	taskService *service.ScanTaskService,
	executionService *service.ScanExecutionService,
	metadataQueryService *service.MetadataQueryService,
	objectMetadataService *service.ObjectMetadataService,
) *Handler {
	return &Handler{
		engineService:         engineService,
		scanService:           scanService,
		taskService:           taskService,
		executionService:      executionService,
		metadataQueryService:  metadataQueryService,
		objectMetadataService: objectMetadataService,
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

// GetObjectMetadata 获取对象的元数据
// GET /api/meta/metadata/object
// Query params: engine_id, object_key
// @Summary 获取对象元数据 | Get object metadata
// @Description 获取指定对象存储文件的元数据信息 | Get metadata information for a specific object storage file
// @Tags Meta
// @Produce json
// @Param engine_id query int true "存储引擎ID | Engine ID"
// @Param object_key query string true "对象存储路径 | Object storage path"
// @Success 200 {object} map[string]interface{} "对象元数据 | Object metadata"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 404 {object} map[string]interface{} "对象不存在 | Object not found"
// @Router /metadata/object [get]
// @Security BearerAuth
func (h *Handler) GetObjectMetadata(c *gin.Context) {
	tenantID := commonAuth.GetTenantID(c)

	engineIDStr := c.Query("engine_id")
	engineID, err := strconv.ParseUint(engineIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid engine_id"})
		return
	}

	objectKey := c.Query("object_key")
	if objectKey == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing object_key"})
		return
	}

	item, err := h.objectMetadataService.GetObjectMetadata(tenantID, uint(engineID), objectKey)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, item)
}

// GetResources 获取存储引擎列表及 catalog 扫描统计
// GET /api/meta/engines
// @Summary 获取引擎列表 | Get engine list
// @Description 获取当前租户的存储引擎列表及统计信息 | Get storage engine list with statistics for the current tenant
// @Tags Meta
// @Produce json
// @Success 200 {array} models.ResourceWithStats "引擎列表 | Engine list"
// @Failure 500 {object} map[string]interface{} "服务器内部错误 | Internal server error"
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
