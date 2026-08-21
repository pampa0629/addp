package api

import (
	"net/http"
	"strings"

	commonClient "github.com/addp/common/client"
	engineselection "github.com/addp/common/engine/selection"
	commonAuth "github.com/addp/common/middleware/auth"
	commonModels "github.com/addp/common/models"
	"github.com/addp/transfer/internal/service"
	"github.com/gin-gonic/gin"
)

// SystemEngineHandler exposes the small system-engine surface needed by Transfer.
type SystemEngineHandler struct {
	systemClient *commonClient.SystemClient
}

func NewSystemEngineHandler(systemClient *commonClient.SystemClient) *SystemEngineHandler {
	return &SystemEngineHandler{systemClient: systemClient}
}

// List returns system engines visible to the current tenant.
// @Summary 列出系统引擎 | List system engines
// @Description 返回当前租户可见、active、online 且具备存储能力的 System 引擎，用于 Transfer 任务选择 | Returns visible, active, online, storage-capable System engines for Transfer task configuration
// @Tags 系统引擎 | System Engines
// @Produce json
// @Param engine_type query string false "引擎类型 | Engine type"
// @Success 200 {array} github_com_addp_transfer_internal_models.SystemEngineDoc
// @Failure 502 {object} map[string]string
// @Failure 503 {object} map[string]string
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["transfer.task.read"]
// @Router /system-engines [get]
// @Security BearerAuth
func (h *SystemEngineHandler) List(c *gin.Context) {
	if h.systemClient == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": service.ErrSystemIntegrationDisabled.Error()})
		return
	}

	tenantID := commonAuth.GetTenantID(c)
	engineType := strings.TrimSpace(c.Query("engine_type"))

	engines, err := h.systemClient.ListEngines(engineType, tenantID)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to list system engines: " + err.Error()})
		return
	}

	result := make([]commonModels.Engine, 0, len(engines))
	for _, engine := range engines {
		if engine.TenantID != nil && *engine.TenantID != 0 && *engine.TenantID != tenantID {
			continue
		}
		if !engineselection.IsAvailableStorageEngine(&engine) {
			continue
		}
		sanitizeEngineConnectionInfo(&engine)
		result = append(result, engine)
	}

	c.JSON(http.StatusOK, result)
}

func sanitizeEngineConnectionInfo(engine *commonModels.Engine) {
	if engine == nil || engine.ConnectionInfo == nil {
		return
	}
	for _, key := range []string{
		"password",
		"secret_key",
		"access_secret",
		"access_token",
		"token",
		"private_key",
	} {
		if _, ok := engine.ConnectionInfo[key]; ok {
			engine.ConnectionInfo[key] = ""
		}
	}
}
