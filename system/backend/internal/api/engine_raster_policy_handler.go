package api

import (
	"bytes"
	"encoding/json"
	"errors"
	commonapi "github.com/addp/common/api"

	commoni18n "github.com/addp/common/middleware/i18n"

	"github.com/addp/system/internal/models"
	"github.com/addp/system/internal/service"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"io"
	"net/http"
)

type EngineRasterPolicyHandler struct {
	service *service.EngineRasterPolicyService
}
type RasterPlatformUpdate struct {
	Version              int64 `json:"version" binding:"required"`
	Running              int   `json:"running" binding:"required"`
	Waiting              int   `json:"waiting" binding:"required"`
	CacheMiB             int   `json:"cache_mib" binding:"required"`
	DefaultTenantRunning int   `json:"default_tenant_running" binding:"required"`
	DefaultTenantWaiting int   `json:"default_tenant_waiting" binding:"required"`
}
type RasterTenantUpdate struct {
	Version int64 `json:"version" binding:"required"`
	// Running 为 null 时继承平台默认值 | Null inherits the platform default.
	Running *int `json:"running" binding:"required" extensions:"x-nullable"`
	// Waiting 为 null 时继承平台默认值 | Null inherits the platform default.
	Waiting *int `json:"waiting" binding:"required" extensions:"x-nullable"`
}
type RuntimeRasterPolicyRequest struct {
	ConnectionInfo models.ConnectionInfo `json:"connection_info" binding:"required"`
}

func strictRasterJSON(c *gin.Context, v interface{}) error {
	d := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, 65536))
	var raw json.RawMessage
	if d.Decode(&raw) != nil || d.Decode(new(interface{})) != io.EOF {
		return commonapi.ErrBadRequest
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(v) != nil {
		return commonapi.ErrBadRequest
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || fields == nil {
		return commonapi.ErrBadRequest
	}
	required := []string{"version", "running", "waiting"}
	switch v.(type) {
	case *RasterPlatformUpdate:
		required = append(required, "cache_mib", "default_tenant_running", "default_tenant_waiting")
	case *RuntimeRasterPolicyRequest:
		required = []string{"connection_info"}
	}
	for _, field := range required {
		value, exists := fields[field]
		if !exists {
			return commonapi.ErrBadRequest
		}
		if _, tenant := v.(*RasterTenantUpdate); tenant && (field == "running" || field == "waiting") {
			continue
		}
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return commonapi.ErrBadRequest
		}
	}
	return nil
}
func rasterError(c *gin.Context, err error) {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(404, gin.H{"error": commoni18n.T(c, "system.raster_policy.not_found")})
		return
	}
	if errors.Is(err, commonapi.ErrConflict) {
		c.JSON(409, gin.H{"error": commoni18n.T(c, "system.raster_policy.version_conflict"), "error_code": "resource_version_conflict"})
		return
	}
	respondIAMError(c, err)
}
func (h *EngineRasterPolicyHandler) list(c *gin.Context) {
	values, err := h.service.Engines(c.Request.Context())
	if err != nil {
		rasterError(c, err)
		return
	}
	c.JSON(200, values)
}
func (h *EngineRasterPolicyHandler) get(c *gin.Context, tenant uint) {
	id, err := parseIAMActorID(c.Param("id"))
	if err != nil {
		rasterError(c, commonapi.ErrBadRequest)
		return
	}
	view, err := h.service.Get(c.Request.Context(), id, tenant)
	if err != nil {
		rasterError(c, err)
		return
	}
	view.Runtime = h.service.RuntimeHealth(c.Request.Context(), id)
	c.JSON(200, view)
}
func (h *EngineRasterPolicyHandler) tenant(c *gin.Context) (uint, bool) {
	_, tenant, err := iamTenantUserActor(c)
	if err != nil {
		rasterError(c, err)
		return 0, false
	}
	return tenant, true
}

// ListPlatform godoc
// @Summary 栅格资源配置 | List raster runtimes
// @Tags 栅格资源配置 | Raster Resource Policy
// @Produce json
// @Security BearerAuth
// @Success 200 {array} service.RasterEngineSummary
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 403 {object} models.ErrorResponse
// @Failure 404 {object} models.ErrorResponse
// @Failure 409 {object} models.ErrorResponse
// @Failure 500 {object} models.ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["system.engine_raster_policy.read"]
// @Router /platform/engine-raster-policies/engines [get]
func (h *EngineRasterPolicyHandler) ListPlatform(c *gin.Context) { h.list(c) }

// ListTenant godoc
// @Summary 栅格资源配置 | List raster runtimes
// @Tags 栅格资源配置 | Raster Resource Policy
// @Produce json
// @Security BearerAuth
// @Success 200 {array} service.RasterEngineSummary
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 403 {object} models.ErrorResponse
// @Failure 404 {object} models.ErrorResponse
// @Failure 409 {object} models.ErrorResponse
// @Failure 500 {object} models.ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["system.engine_raster_policy.read"]
// @Router /tenant/engine-raster-policies/engines [get]
func (h *EngineRasterPolicyHandler) ListTenant(c *gin.Context) { h.list(c) }

// GetPlatform godoc
// @Summary 栅格资源配置 | Get raster resource policy
// @Tags 栅格资源配置 | Raster Resource Policy
// @Produce json
// @Security BearerAuth
// @Param id path int true "引擎实例 ID | Engine instance ID"
// @Success 200 {object} models.RasterPolicyView
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 403 {object} models.ErrorResponse
// @Failure 404 {object} models.ErrorResponse
// @Failure 409 {object} models.ErrorResponse
// @Failure 500 {object} models.ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["system.engine_raster_policy.read"]
// @Router /platform/engine-raster-policies/{id} [get]
func (h *EngineRasterPolicyHandler) GetPlatform(c *gin.Context) { h.get(c, 0) }

// GetTenant godoc
// @Summary 栅格资源配置 | Get tenant raster quota
// @Tags 栅格资源配置 | Raster Resource Policy
// @Produce json
// @Security BearerAuth
// @Param id path int true "引擎实例 ID | Engine instance ID"
// @Success 200 {object} models.RasterPolicyView
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 403 {object} models.ErrorResponse
// @Failure 404 {object} models.ErrorResponse
// @Failure 409 {object} models.ErrorResponse
// @Failure 500 {object} models.ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["system.engine_raster_policy.read"]
// @Router /tenant/engine-raster-policies/{id} [get]
func (h *EngineRasterPolicyHandler) GetTenant(c *gin.Context) {
	if tenant, ok := h.tenant(c); ok {
		h.get(c, tenant)
	}
}

// PutPlatform godoc
// @Summary 栅格资源配置 | Update raster resource policy
// @Tags 栅格资源配置 | Raster Resource Policy
// @Produce json
// @Security BearerAuth
// @Param id path int true "引擎实例 ID | Engine instance ID"
// @Accept json
// @Param request body RasterPlatformUpdate true "配置 | Configuration"
// @Success 200 {object} models.RasterPolicyView
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 403 {object} models.ErrorResponse
// @Failure 404 {object} models.ErrorResponse
// @Failure 409 {object} models.ErrorResponse
// @Failure 500 {object} models.ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["system.engine_raster_policy.update"]
// @Router /platform/engine-raster-policies/{id} [put]
func (h *EngineRasterPolicyHandler) PutPlatform(c *gin.Context) {
	if _, err := iamPlatformUserActor(c); err != nil {
		rasterError(c, err)
		return
	}
	id, err := parseIAMActorID(c.Param("id"))
	if err != nil {
		rasterError(c, commonapi.ErrBadRequest)
		return
	}
	var req RasterPlatformUpdate
	if err := strictRasterJSON(c, &req); err != nil {
		rasterError(c, err)
		return
	}
	view, err := h.service.SavePlatform(c.Request.Context(), id, models.EngineRasterPolicy{Version: req.Version, Running: req.Running, Waiting: req.Waiting, CacheMiB: req.CacheMiB, DefaultTenantRunning: req.DefaultTenantRunning, DefaultTenantWaiting: req.DefaultTenantWaiting}, iamAuditMetadataWithStatus(c, 200))
	if err != nil {
		rasterError(c, err)
		return
	}
	view.Runtime = h.service.RuntimeHealth(c.Request.Context(), id)
	c.JSON(200, view)
}

// PutTenant godoc
// @Summary 栅格资源配置 | Update tenant raster quota
// @Tags 栅格资源配置 | Raster Resource Policy
// @Produce json
// @Security BearerAuth
// @Param id path int true "引擎实例 ID | Engine instance ID"
// @Accept json
// @Param request body RasterTenantUpdate true "配置 | Configuration"
// @Success 200 {object} models.RasterPolicyView
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 403 {object} models.ErrorResponse
// @Failure 404 {object} models.ErrorResponse
// @Failure 409 {object} models.ErrorResponse
// @Failure 500 {object} models.ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["system.engine_raster_policy.update"]
// @Router /tenant/engine-raster-policies/{id} [put]
func (h *EngineRasterPolicyHandler) PutTenant(c *gin.Context) {
	tenant, ok := h.tenant(c)
	if !ok {
		return
	}
	id, err := parseIAMActorID(c.Param("id"))
	if err != nil {
		rasterError(c, commonapi.ErrBadRequest)
		return
	}
	var req RasterTenantUpdate
	if err := strictRasterJSON(c, &req); err != nil {
		rasterError(c, err)
		return
	}
	view, err := h.service.SaveTenant(c.Request.Context(), id, tenant, models.EngineRasterQuota{Version: req.Version, Running: req.Running, Waiting: req.Waiting}, iamAuditMetadataWithStatus(c, 200))
	if err != nil {
		rasterError(c, err)
		return
	}
	view.Runtime = h.service.RuntimeHealth(c.Request.Context(), id)
	c.JSON(200, view)
}

// Resolve godoc
// @Summary 栅格资源配置 | Resolve runtime raster policy
// @Tags 栅格资源配置 | Raster Resource Policy
// @Produce json
// @Security BearerAuth
// @Accept json
// @Param request body RuntimeRasterPolicyRequest true "配置 | Configuration"
// @Success 200 {object} service.RuntimeRasterPolicy
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 403 {object} models.ErrorResponse
// @Failure 404 {object} models.ErrorResponse
// @Failure 409 {object} models.ErrorResponse
// @Failure 500 {object} models.ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["system.engine_raster_policy_runtime.read"]
// @Router /runtime/engine-raster-policy [post]
func (h *EngineRasterPolicyHandler) Resolve(c *gin.Context) {
	var req RuntimeRasterPolicyRequest
	if err := strictRasterJSON(c, &req); err != nil {
		rasterError(c, err)
		return
	}
	view, err := h.service.Resolve(c.Request.Context(), req.ConnectionInfo)
	if err != nil {
		rasterError(c, err)
		return
	}
	c.JSON(200, view)
}
