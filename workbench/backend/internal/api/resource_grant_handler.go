package api

import (
	"net/http"
	"strings"

	commonapi "github.com/addp/common/api"
	commonAuth "github.com/addp/common/middleware/auth"
	"github.com/addp/workbench/internal/models"
	"github.com/addp/workbench/internal/service"
	"github.com/gin-gonic/gin"
)

type ResourceGrantHandler struct{ grants *service.ResourceGrantService }

func NewResourceGrantHandler(grants *service.ResourceGrantService) *ResourceGrantHandler {
	return &ResourceGrantHandler{grants: grants}
}

// FulfillAssetGrant creates one idempotent Workbench owner Resource Grant from an Asset authorization.
// @Summary 履约 Asset 资源授权 | Fulfill an Asset resource grant
// @Description 使用 Asset Authorization ID 作为幂等来源，为指定 User 建立 Data Application execute Allow Rule；只允许 addp-asset | Use the Asset Authorization ID as the idempotent source and create a Data Application execute Allow Rule for one User; restricted to addp-asset
// @Tags Workbench Resource Grants
// @Accept json
// @Produce json
// @Param source_identity path string true "Asset Authorization ID"
// @Param request body models.AssetResourceGrantRequest true "完整授权目标 | Complete grant target"
// @Success 200 {object} models.AssetResourceGrantResponse "已生效授权 | Effective grant"
// @Failure 400 {object} map[string]interface{} "请求无效 | Invalid request"
// @Failure 401 {object} map[string]interface{} "未认证 | Unauthorized"
// @Failure 403 {object} map[string]interface{} "非 addp-asset 或权限不足 | Wrong client or insufficient permission"
// @Failure 409 {object} map[string]interface{} "来源身份冲突或已撤销 | Source identity conflict or already revoked"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["workbench.resource_grant.create"]
// @Router /runtime/resource-grants/{source_identity} [put]
// @Security BearerAuth
func (h *ResourceGrantHandler) FulfillAssetGrant(c *gin.Context) {
	tenantID, ok := commonAuth.TenantIDFromGin(c)
	if !ok {
		respondError(c, service.ErrInvalidResourceGrant)
		return
	}
	var request models.AssetResourceGrantRequest
	if err := commonapi.BindOptionalJSONStrict(c, &request); err != nil {
		respondError(c, service.ErrInvalidResourceGrant)
		return
	}
	response, err := h.grants.FulfillAssetGrant(tenantID, strings.TrimSpace(c.Param("source_identity")), request)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, response)
}

// RevokeAssetGrant idempotently revokes one Workbench owner Resource Grant.
// @Summary 撤销 Asset 资源授权 | Revoke an Asset resource grant
// @Description 使用完整授权目标写入可重放撤销墓碑，阻止延迟的旧履约重新放开访问；只允许 addp-asset | Persist a replayable revocation tombstone from the complete target so delayed fulfillment cannot reopen access; restricted to addp-asset
// @Tags Workbench Resource Grants
// @Accept json
// @Produce json
// @Param source_identity path string true "Asset Authorization ID"
// @Param request body models.AssetResourceGrantRequest true "完整授权目标 | Complete grant target"
// @Success 200 {object} models.AssetResourceGrantResponse "已撤销授权 | Revoked grant"
// @Failure 400 {object} map[string]interface{} "请求无效 | Invalid request"
// @Failure 401 {object} map[string]interface{} "未认证 | Unauthorized"
// @Failure 403 {object} map[string]interface{} "非 addp-asset 或权限不足 | Wrong client or insufficient permission"
// @Failure 409 {object} map[string]interface{} "来源身份冲突 | Source identity conflict"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["workbench.resource_grant.revoke"]
// @Router /runtime/resource-grants/{source_identity} [delete]
// @Security BearerAuth
func (h *ResourceGrantHandler) RevokeAssetGrant(c *gin.Context) {
	tenantID, ok := commonAuth.TenantIDFromGin(c)
	if !ok {
		respondError(c, service.ErrInvalidResourceGrant)
		return
	}
	var request models.AssetResourceGrantRequest
	if err := commonapi.BindOptionalJSONStrict(c, &request); err != nil {
		respondError(c, service.ErrInvalidResourceGrant)
		return
	}
	response, err := h.grants.RevokeAssetGrant(tenantID, strings.TrimSpace(c.Param("source_identity")), request)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, response)
}
