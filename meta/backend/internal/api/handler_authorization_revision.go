package api

import (
	"net/http"

	commonAuth "github.com/addp/common/middleware/auth"
	commoni18n "github.com/addp/common/middleware/i18n"
	metai18n "github.com/addp/meta/i18n"
	"github.com/gin-gonic/gin"
)

// GetAuthorizationBasisRevision godoc
// @Summary 读取授权事实提交版本 | Read committed authorization facts revision
// @Description 仅 System Tenant Service 读取当前租户不透明依据版本，不返回身份或许可 | Only the System Tenant Service may read the current tenant basis revision; returns no identities or Allow
// @Tags Meta Runtime
// @Produce json
// @Security BearerAuth
// @Success 200 {object} authorizationfacts.Revision
// @Failure 400,401,403,500 {object} map[string]interface{} "读取失败 | Read failed"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["meta.authorization_facts.read"]
// @Router /runtime/data-items/authorization-facts/revision [get]
func (h *Handler) GetAuthorizationBasisRevision(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	if c.Request.URL.RawQuery != "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, metai18n.MsgAuthorizationFactsInvalid), "error_code": "meta_authorization_facts_invalid"})
		return
	}
	result, err := h.metadataQueryService.GetAuthorizationBasisRevision(c.Request.Context(), commonAuth.GetTenantID(c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": commoni18n.T(c, metai18n.MsgAuthorizationFactsUnavailable), "error_code": "meta_authorization_facts_unavailable"})
		return
	}
	c.JSON(http.StatusOK, result)
}
