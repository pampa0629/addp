package api

import (
	"errors"
	"github.com/addp/common/authorization"
	commonClient "github.com/addp/common/client"
	"github.com/addp/common/dbbridge"
	"github.com/addp/common/engine/plugin"
	engineselection "github.com/addp/common/engine/selection"
	commonAuth "github.com/addp/common/middleware/auth"
	"github.com/addp/common/models"
	developi18n "github.com/addp/develop/backend/i18n"
	"github.com/gin-gonic/gin"
	"net/http"
)

// Query runtimes and their data sources are distinct; only storage candidates
// enter the current User data-visibility observation.
func (h *EngineHandler) filterVisibleStorageEngines(c *gin.Context, candidates []models.EngineRuntimeDescriptor) ([]models.EngineRuntimeDescriptor, error) {
	targets := []plugin.EngineCatalogPath{}
	visible := map[uint]bool{}
	for _, d := range candidates {
		if !engineselection.HasStorageCapability(d.AsEngine()) {
			visible[d.ID] = true
			continue
		}
		model, err := dbbridge.EngineCatalogModel(d.EngineType)
		if err != nil {
			continue
		}
		targets = append(targets, plugin.EngineCatalogRootPath(model, d.ID))
	}
	for start := 0; start < len(targets); start += 200 {
		query := authorization.SourceNavigationCheckRequest{Targets: targets[start:min(start+200, len(targets))]}
		observed, err := h.systemClient.CheckDevelopNavigation(c.Request.Context(), commonAuth.CanonicalBearerToken(c.GetHeader("Authorization")), query)
		if err != nil {
			return nil, err
		}
		for _, target := range observed.Targets {
			visible[target.Target.EngineID] = target.Visible
		}
	}
	result := make([]models.EngineRuntimeDescriptor, 0, len(candidates))
	for _, d := range candidates {
		if visible[d.ID] {
			result = append(result, d)
		}
	}
	return result, nil
}

func writeEngineVisibilityError(c *gin.Context, err error) {
	status, code := http.StatusServiceUnavailable, "authorization_unavailable"
	if errors.Is(err, commonClient.ErrMetadataCredentialRejected) {
		status, code = http.StatusUnauthorized, "invalid_credential"
	} else if errors.Is(err, commonClient.ErrMetadataQueryDenied) {
		status, code = http.StatusForbidden, "permission_denied"
	}
	respondDevelopCatalogError(c, status, code, developi18n.MsgEngineListFailed)
}

// ListSourceEngines godoc
// @Summary 获取当前可见查询来源引擎 | List currently visible query source engines
// @Description 当前原用户的有效元数据或读取来源决定存储引擎可见性；仅返回无凭据运行描述，不授予连接或执行权 | Current original User metadata or read sources determine storage visibility. Returns credential-free runtime descriptors, not connection or execution authority
// @Tags Engines
// @Produce json
// @Success 200 {array} models.EngineRuntimeDescriptor "当前可见来源 | Currently visible sources"
// @Failure 401,403,503 {object} map[string]string "核验失败 | Visibility check failed"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["develop.task.read"]
// @Router /source-engines [get]
// @Security BearerAuth
func (h *EngineHandler) ListSourceEngines(c *gin.Context) {
	descriptors, err := h.systemClient.WithTenantID(tenantIDValue(c)).ListEngineRuntimeDescriptors(c.Request.Context())
	if err != nil {
		writeEngineVisibilityError(c, err)
		return
	}
	candidates := filterDevelopEngineDescriptors(descriptors, engineselection.HasStorageCapability)
	visible, err := h.filterVisibleStorageEngines(c, candidates)
	if err != nil {
		writeEngineVisibilityError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, visible)
}
