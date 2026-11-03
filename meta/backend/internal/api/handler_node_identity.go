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
// @Description 仅 addp-system Tenant Service，以完整类型化路径读取当前租户精确引擎的 PG 根／schema、NFS 普通根／目录、MinIO service／bucket／普通 prefix 节点存在事实；不返回属性、内容或统计，不扫描、不签发 Grant。正文最多16KiB，不接受query或额外字段 | Fixed addp-system Tenant Service; resolves the existence of one PG root/schema, ordinary NFS root/directory or MinIO service/bucket/ordinary prefix node in the current tenant and exact engine by typed path. No attributes, content, statistics, scans or grants. Body is at most 16KiB; query and extra fields are rejected
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
	var input authorizationfacts.NodeIdentityQuery
	if !bindNodeIdentityJSON(c, &input) {
		return
	}
	result, err := h.metadataQueryService.ResolveNodeIdentity(c.Request.Context(), commonauth.GetTenantID(c), input)
	respondNodeIdentity(c, result, err)
}

// QueryNodeIdentities godoc
// @Summary 查询直接子节点身份 | Query immediate child node identities
// @Description 仅当前租户 addp-system Service 通过独立机器权限查询指定 PG server/schema、NFS root/directory 或 MinIO service/bucket/prefix 的直接子节点；不返回数据项、内容、属性、统计或权限裁决。分页最多100项，正文最多16KiB，不接受query或额外字段 | Fixed tenant addp-system Service with the node identity permission. Returns immediate PG, NFS or MinIO owner nodes only; no DataItems, content, attributes, statistics or access decisions. At most 100 nodes per page and 16KiB request body; query and extra fields are rejected
// @Tags Meta Runtime
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body authorizationfacts.NodeIdentityPageQuery true "节点查询及分页 | Node query and pagination"
// @Success 200 {object} authorizationfacts.NodeIdentityPage
// @Failure 400,401,403,413,500 {object} map[string]interface{} "查询失败 | Query failed"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["meta.node_identity.read"]
// @Router /runtime/node-identities/query [post]
func (h *Handler) QueryNodeIdentities(c *gin.Context) {
	var input authorizationfacts.NodeIdentityPageQuery
	if !bindNodeIdentityJSON(c, &input) {
		return
	}
	result, err := h.metadataQueryService.QueryNodeIdentities(c.Request.Context(), commonauth.GetTenantID(c), input)
	respondNodeIdentity(c, result, err)
}

func bindNodeIdentityJSON(c *gin.Context, input interface{ Validate() error }) bool {
	c.Header("Cache-Control", "no-store")
	body, readErr := io.ReadAll(io.LimitReader(c.Request.Body, authorizationfacts.MaxNodeIdentityBytes+1))
	if len(body) > authorizationfacts.MaxNodeIdentityBytes {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error_code": "meta_authorization_facts_invalid", "error": commoni18n.T(c, metai18n.MsgAuthorizationFactsInvalid)})
		return false
	}
	_, shapeErr := jsonmap.DecodeObjectStrict(body, authorizationfacts.MaxNodeIdentityBytes, 8)
	c.Request.Body = io.NopCloser(bytes.NewReader(body))
	bindErr := commonapi.BindOptionalJSONStrict(c, input)
	if c.Request.URL.RawQuery != "" || readErr != nil || shapeErr != nil || bindErr != nil || input.Validate() != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error_code": "meta_authorization_facts_invalid", "error": commoni18n.T(c, metai18n.MsgAuthorizationFactsInvalid)})
		return false
	}
	return true
}
func respondNodeIdentity(c *gin.Context, result any, err error) {
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error_code": "meta_authorization_facts_unavailable", "error": commoni18n.T(c, metai18n.MsgAuthorizationFactsUnavailable)})
		return
	}
	c.JSON(http.StatusOK, result)
}
