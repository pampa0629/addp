package api

import (
	"bytes"
	"io"
	"net/http"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/jsonmap"
	commonauth "github.com/addp/common/middleware/auth"
	commoni18n "github.com/addp/common/middleware/i18n"
	metai18n "github.com/addp/meta/i18n"
	"github.com/gin-gonic/gin"
)

// ResolveNodeIdentity godoc
// @Summary 核对节点身份事实 | Resolve node identity facts
// @Description 仅 addp-system Tenant Service，以完整类型化路径读取当前租户精确引擎的 PG 根或 schema 节点存在事实；不返回属性、内容或统计，不扫描、不签发 Grant。正文最多16KiB，不接受query或额外字段 | Fixed addp-system Tenant Service; resolves the existence of one PG root or schema node in the current tenant and exact engine by typed path. No attributes, content, statistics, scans or grants. Body is at most 16KiB; query and extra fields are rejected
// @Tags Meta Runtime
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body authorizationfacts.NodeIdentityQuery true "节点身份查询 | Node identity query"
// @Success 200 {object} authorizationfacts.NodeIdentity
// @Failure 400,401,403,413,500 {object} map[string]interface{} "核对失败 | Resolution failed"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["meta.node_identity.read"]
// @Router /runtime/node-identities/resolve [post]
func (h *Handler) ResolveNodeIdentity(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	body, readErr := io.ReadAll(io.LimitReader(c.Request.Body, authorizationfacts.MaxNodeIdentityBytes+1))
	if len(body) > authorizationfacts.MaxNodeIdentityBytes {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error_code": "meta_authorization_facts_invalid", "error": commoni18n.T(c, metai18n.MsgAuthorizationFactsInvalid)})
		return
	}
	_, shapeErr := jsonmap.DecodeObjectStrict(body, authorizationfacts.MaxNodeIdentityBytes, 8)
	c.Request.Body = io.NopCloser(bytes.NewReader(body))
	var input authorizationfacts.NodeIdentityQuery
	bindErr := commonapi.BindOptionalJSONStrict(c, &input)
	if c.Request.URL.RawQuery != "" || readErr != nil || shapeErr != nil || bindErr != nil || input.Validate() != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error_code": "meta_authorization_facts_invalid", "error": commoni18n.T(c, metai18n.MsgAuthorizationFactsInvalid)})
		return
	}
	result, err := h.metadataQueryService.ResolveNodeIdentity(c.Request.Context(), commonauth.GetTenantID(c), input)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error_code": "meta_authorization_facts_unavailable", "error": commoni18n.T(c, metai18n.MsgAuthorizationFactsUnavailable)})
		return
	}
	c.JSON(http.StatusOK, result)
}
