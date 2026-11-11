package api

import (
	"bytes"
	"io"
	"net/http"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/jsonmap"
	commonAuth "github.com/addp/common/middleware/auth"
	commoni18n "github.com/addp/common/middleware/i18n"
	metai18n "github.com/addp/meta/i18n"
	"github.com/gin-gonic/gin"
)

// QueryDataItemAuthorizationFacts godoc
// @Summary 查询数据项授权资源事实 | Query DataItem authorization resource facts
// @Description 固定 System/Catalog/Manager Tenant Service 精确读取 1—64 个不重复指纹，正文最多 8 KiB、每项最多 128 KiB；按请求顺序返回，当前租户缺失项facts为null，故障整批失败。200不是许可，不扫描、不读源内容、不缓存 | Restricted to System/Catalog/Manager Tenant Services. Reads 1-64 unique exact fingerprints, at most 8 KiB request and 128 KiB per fact. Results retain request order; absent items have null facts, failures reject the entire query. HTTP 200 is not an Allow. No scans, source reads or caching
// @Tags Meta Runtime
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body authorizationfacts.AuthorizationFactsQuery true "精确事实查询 | Exact facts query"
// @Success 200 {object} authorizationfacts.AuthorizationFactsBatch
// @Failure 400,401,403,413,500 {object} map[string]interface{} "查询失败 | Query failed"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["meta.authorization_facts.read"]
// @Router /runtime/data-items/authorization-facts/query [post]
func (h *Handler) QueryDataItemAuthorizationFacts(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	body, readErr := io.ReadAll(io.LimitReader(c.Request.Body, authorizationfacts.MaxAuthorizationFactsQueryBytes+1))
	if len(body) > authorizationfacts.MaxAuthorizationFactsQueryBytes {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": commoni18n.T(c, metai18n.MsgAuthorizationFactsInvalid), "error_code": "meta_authorization_facts_invalid"})
		return
	}
	_, shapeErr := jsonmap.DecodeObjectStrict(body, authorizationfacts.MaxAuthorizationFactsQueryBytes, 4)
	c.Request.Body = io.NopCloser(bytes.NewReader(body))
	var input authorizationfacts.AuthorizationFactsQuery
	if err := commonapi.BindOptionalJSONStrict(c, &input); c.Request.URL.RawQuery != "" || readErr != nil || shapeErr != nil || err != nil || input.Validate() != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, metai18n.MsgAuthorizationFactsInvalid), "error_code": "meta_authorization_facts_invalid"})
		return
	}
	facts, err := h.metadataQueryService.QueryDataItemAuthorizationFacts(c.Request.Context(), commonAuth.GetTenantID(c), input)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": commoni18n.T(c, metai18n.MsgAuthorizationFactsUnavailable), "error_code": "meta_authorization_facts_unavailable"})
		return
	}
	c.JSON(http.StatusOK, facts)
}
