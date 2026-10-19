package api

import (
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/addp/common/client"
	commonAuth "github.com/addp/common/middleware/auth"
	commoni18n "github.com/addp/common/middleware/i18n"
	"github.com/addp/common/resourcetree"
	manageri18n "github.com/addp/manager/i18n"
	"github.com/addp/manager/internal/engineaccess"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/service"
	"github.com/gin-gonic/gin"
)

type MetadataHandler struct {
	metadataService *service.MetadataService
}

func NewMetadataHandler(metadataService *service.MetadataService) *MetadataHandler {
	return &MetadataHandler{
		metadataService: metadataService,
	}
}

// RefreshItem 强制刷新指定 item 的元数据
// @Summary 刷新数据项元数据 | Refresh item metadata
// @Description 当前租户用户以本人凭据同步提交精确 item 深度刷新并等待完成，Meta 独立核验扫描权限 | Current tenant user submits an exact item deep refresh with their own credential and waits; Meta independently checks scan permission
// @Tags Manager
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["manager.data_item.update","meta.scan_task.execute"]
// @Router /engines/{id}/items/refresh [post]
// @Accept json
// @Produce json
// @Param id path int true "存储引擎ID | Engine ID"
// @Param locator query string false "资源定位符 URI | Resource locator URI"
// @Param body body models.MetaManualScanRequest false "扫描定位及 CSV/TSV 读取设置（可选）| Item target and CSV/TSV read settings (optional)"
// @Success 200 {object} models.MetaScanResponse "同步扫描结果 | Synchronous scan result"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 403 {object} map[string]interface{} "当前用户无权刷新 | Current user cannot refresh"
// @Failure 404 {object} map[string]interface{} "数据项不存在 | Item not found"
// @Failure 409 {object} map[string]interface{} "同范围扫描正在执行 | Scan scope is active"
// @Failure 500 {object} map[string]interface{} "服务器内部错误 | Internal server error"
// @Failure 503 {object} map[string]interface{} "引擎不可用 | Engine unavailable"
// @Failure 504 {object} map[string]interface{} "刷新等待超时 | Refresh wait timed out"
// @Security BearerAuth
func (h *MetadataHandler) RefreshItem(c *gin.Context) {
	principal, ok := commonAuth.PrincipalFromGin(c)
	if !ok || principal.Type != "user" {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error_code": "permission_denied", "error": commoni18n.T(c, commoni18n.MsgForbidden)})
		return
	}
	engineID, ok := parseUintParam(c, "id")
	if !ok {
		return
	}

	var req models.MetaManualScanRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		if !errors.Is(err, io.EOF) {
			c.JSON(http.StatusBadRequest, gin.H{"error_code": "invalid_params", "error": commoni18n.T(c, commoni18n.MsgInvalidParams)})
			return
		}
	}
	if locatorURI := strings.TrimSpace(c.Query("locator")); locatorURI != "" {
		loc, err := resourcetree.ParseURI(locatorURI)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if loc.EngineID != engineID {
			managerError(c, http.StatusBadRequest, manageri18n.MsgLocatorEngineMismatch)
			return
		}
		if loc.ItemID != nil && *loc.ItemID > 0 {
			req.ItemID = *loc.ItemID
		} else if loc.NodeID != nil && *loc.NodeID > 0 {
			managerError(c, http.StatusBadRequest, manageri18n.MsgItemRefreshRequiresLocator)
			return
		}
	}
	if req.ItemID == 0 {
		managerError(c, http.StatusBadRequest, manageri18n.MsgItemRefreshTargetRequired)
		return
	}
	req.ScanDepth = "deep"
	req.Force = true

	tenantID := commonAuth.GetTenantID(c)
	result, err := h.metadataService.RefreshItem(c.Request.Context(), &tenantID, engineID, &req, commonAuth.CanonicalBearerToken(c.GetHeader("Authorization")))
	if err != nil {
		if errors.Is(err, engineaccess.ErrUnavailable) {
			engineUnavailable(c)
			return
		}
		if errors.Is(err, service.ErrEngineAccessDenied) {
			accessDeniedToEngine(c)
			return
		}
		if code, ok := client.TenantAPIErrorCode(err); ok && code == "scan_delimited_options_invalid" {
			c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, manageri18n.MsgScanDelimitedOptionsInvalid), "error_code": code})
			return
		}
		status := http.StatusServiceUnavailable
		if downstream, ok := client.TenantAPIStatusCode(err); ok {
			switch downstream {
			case http.StatusBadRequest, http.StatusForbidden, http.StatusNotFound, http.StatusConflict, http.StatusGatewayTimeout:
				status = downstream
			}
		}
		c.JSON(status, gin.H{"error_code": "item_refresh_failed", "error": commoni18n.T(c, manageri18n.MsgItemRefreshFailed)})
		return
	}

	c.JSON(http.StatusOK, result)
}

func parseUintParam(c *gin.Context, key string) (uint, bool) {
	value := c.Param(key)
	if strings.TrimSpace(value) == "" {
		managerErrorWithDetail(c, http.StatusBadRequest, manageri18n.MsgMissingParam, key)
		return 0, false
	}

	parsed, err := strconv.ParseUint(value, 10, 32)
	if err != nil {
		managerErrorWithDetail(c, http.StatusBadRequest, manageri18n.MsgInvalidParam, key)
		return 0, false
	}

	return uint(parsed), true
}
