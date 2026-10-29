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

// QueryDataItemIdentities godoc
// @Summary 查询数据项身份清单 | Query DataItem identities
// @Description 仅供 addp-system Tenant Service 查询当前租户精确引擎内已识别数据项；返回最小身份及同快照的完整父节点链（最多128级），缺失或循环父链失败，不读取源内容、不扫描、不裁决权限。先过滤再计数分页；最多 20000 个不重复指纹、每页最多 100 项，正文最多 2 MiB。include 空集合返回空结果 | Restricted to addp-system Tenant Service. Reads minimal identified DataItem identities with complete same-snapshot ancestry (at most 128 levels; missing or cyclic parents fail) for one engine in the current tenant, without source reads, scans or access decisions. Filters precede counting and pagination. At most 20000 unique fingerprints, 100 rows per page and 2 MiB body. Empty include means no matches
// @Tags Meta Runtime
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body authorizationfacts.IdentityQuery true "身份查询 | Identity query"
// @Success 200 {object} authorizationfacts.IdentityPage
// @Failure 400,401,403,413,500 {object} map[string]interface{} "查询失败 | Query failed"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["meta.data_item_identity.read"]
// @Router /runtime/data-item-identities/query [post]
func (h *Handler) QueryDataItemIdentities(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	if c.Request.URL.RawQuery != "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, metai18n.MsgAuthorizationFactsInvalid), "error_code": "meta_authorization_facts_invalid"})
		return
	}
	body, readErr := io.ReadAll(io.LimitReader(c.Request.Body, authorizationfacts.MaxIdentityQueryBytes+1))
	if len(body) > authorizationfacts.MaxIdentityQueryBytes {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": commoni18n.T(c, metai18n.MsgAuthorizationFactsInvalid), "error_code": "meta_authorization_facts_invalid"})
		return
	}
	_, shapeErr := jsonmap.DecodeObjectStrict(body, authorizationfacts.MaxIdentityQueryBytes, 8)
	c.Request.Body = io.NopCloser(bytes.NewReader(body))
	var input authorizationfacts.IdentityQuery
	if err := commonapi.BindOptionalJSONStrict(c, &input); readErr != nil || shapeErr != nil || err != nil || input.Validate() != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, metai18n.MsgAuthorizationFactsInvalid), "error_code": "meta_authorization_facts_invalid"})
		return
	}
	result, err := h.metadataQueryService.QueryDataItemIdentities(c.Request.Context(), commonAuth.GetTenantID(c), input)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": commoni18n.T(c, metai18n.MsgAuthorizationFactsUnavailable), "error_code": "meta_authorization_facts_unavailable"})
		return
	}
	c.JSON(http.StatusOK, result)
}
