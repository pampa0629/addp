package api

import (
	"errors"
	"net/http"

	commonapi "github.com/addp/common/api"
	commonAuth "github.com/addp/common/middleware/auth"
	commoni18n "github.com/addp/common/middleware/i18n"
	metai18n "github.com/addp/meta/i18n"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// GetDataItemAuthorizationFacts godoc
// @Summary 查询数据项授权资源事实 | Read DataItem authorization resource facts
// @Description 仅供 addp-system Tenant Service 精确读取已提交的 Meta 身份与边界，200 不表示访问许可；不扫描、不读取源内容、不返回凭据或完整 attributes | Restricted to addp-system Tenant Service. Reads committed Meta identity and boundary; HTTP 200 is not an Allow. No scan, source content, credentials or full attributes
// @Tags Meta Runtime
// @Produce json
// @Security BearerAuth
// @Param fingerprint path string true "数据项指纹 | DataItem fingerprint"
// @Success 200 {object} authorizationfacts.AuthorizationFacts "当前授权资源事实 | Current authorization resource facts"
// @Failure 400,401,403,404,500 {object} map[string]interface{} "查询失败 | Query failed"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["meta.authorization_facts.read"]
// @Router /runtime/data-items/{fingerprint}/authorization-facts [get]
func (h *Handler) GetDataItemAuthorizationFacts(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	if c.Request.URL.RawQuery != "" || c.Request.ContentLength != 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, metai18n.MsgAuthorizationFactsInvalid), "error_code": "meta_authorization_facts_invalid"})
		return
	}
	facts, err := h.metadataQueryService.GetDataItemAuthorizationFacts(c.Request.Context(), commonAuth.GetTenantID(c), c.Param("fingerprint"))
	if err != nil {
		status, message, code := http.StatusInternalServerError, metai18n.MsgAuthorizationFactsUnavailable, "meta_authorization_facts_unavailable"
		if errors.Is(err, commonapi.ErrBadRequest) {
			status, message, code = http.StatusBadRequest, metai18n.MsgAuthorizationFactsInvalid, "meta_authorization_facts_invalid"
		}
		if errors.Is(err, gorm.ErrRecordNotFound) {
			status, message, code = http.StatusNotFound, metai18n.MsgItemSnapshotNotFound, "meta_authorization_target_not_found"
		}
		c.JSON(status, gin.H{"error": commoni18n.T(c, message), "error_code": code})
		return
	}
	c.JSON(http.StatusOK, facts)
}
