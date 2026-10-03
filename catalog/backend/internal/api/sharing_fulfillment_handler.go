package api

import (
	"github.com/addp/catalog/internal/service"
	commonapi "github.com/addp/common/api"
	"github.com/addp/common/authorization"
	commonAuth "github.com/addp/common/middleware/auth"
	"github.com/gin-gonic/gin"
	"net/http"
)

type prepareSharingFulfillmentRequest struct {
	RequestID          string `json:"request_id"`
	DecisionID         string `json:"decision_id"`
	RequirementVersion string `json:"requirement_version"`
}

// PrepareSharingFulfillment godoc
// @Summary 正式准备并发送源读取授权办理 | Prepare and send source-read access fulfillment
// @Description 从当前 User 派生办理人，提交 pending 后发送；202 表示等待权威核清，不表示授权成功 | Derive operator from the current User; send only after pending commits; 202 means awaiting authoritative settlement, not granted access
// @Tags Catalog Sharing
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "条目 UUID | Entry UUID"
// @Param request body prepareSharingFulfillmentRequest true "准备参数 | Preparation input"
// @Success 200,202 {object} service.SharingFulfillmentResult "办理或待核清结果，不是 Grant | Outcome or pending settlement, not a Grant"
// @Failure 400,401,403,404,409,500,503 {object} map[string]interface{} "请求失败 | Request failed"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["catalog.entry.read","system.engine_access_fulfillment.create"]
// @Router /entries/{id}/sharing_fulfillments [post]
func (h *Handler) PrepareSharingFulfillment(c *gin.Context) {
	tenant, ok := commonAuth.TenantIDFromGin(c)
	ac, authenticated := commonAuth.AuthContextFromGin(c)
	if !ok || !authenticated {
		respondError(c, http.StatusUnauthorized, service.ErrUserPrincipalRequired)
		return
	}
	entryID, err := parseCanonicalUUID(c.Param("id"))
	var input prepareSharingFulfillmentRequest
	if err != nil || commonapi.BindOptionalJSONStrict(c, &input) != nil {
		respondError(c, 400, service.ErrInvalidEntryUpdate)
		return
	}
	requestID, err := parseCanonicalUUID(input.RequestID)
	if err != nil {
		respondError(c, 400, service.ErrInvalidEntryUpdate)
		return
	}
	decisionID, err := parseCanonicalUUID(input.DecisionID)
	if err != nil {
		respondError(c, 400, service.ErrInvalidEntryUpdate)
		return
	}
	version, err := parseCanonicalPositiveInt64(input.RequirementVersion)
	if err != nil {
		respondError(c, 400, service.ErrInvalidEntryUpdate)
		return
	}
	result, err := h.entries.PrepareSharingFulfillment(c.Request.Context(), tenant, entryAccess(c), entryID,
		service.SharingFulfillmentInput{RequestID: requestID, DecisionID: decisionID, RequirementVersion: version}, ac, commonAuth.CanonicalBearerToken(c.GetHeader("Authorization")))
	if err != nil {
		respondError(c, 500, err)
		return
	}
	status := http.StatusOK
	if result.State == "pending" {
		status = http.StatusAccepted
	}
	c.JSON(status, result)
}

// ReadSharingFulfillmentBasis godoc
// @Summary 反查精确持久办理依据 | Read exact persisted fulfillment basis
// @Description 仅 addp-system Tenant Service；完整匹配仍待核清的请求，复核原来源和业务责任，不返回用途正文或访问权限 | addp-system Tenant Service only; match the complete pending request and continuous source/ownership; no purpose text or access grant
// @Tags Catalog Runtime
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request_id path string true "请求 UUID | Request UUID"
// @Param request body authorization.SharingFulfillmentBinding true "完整原绑定 | Complete original binding"
// @Success 200 {object} authorization.SharingFulfillmentBasis "持久依据 | Persisted basis"
// @Failure 400,401,403,404,409,500 {object} map[string]interface{} "请求失败 | Request failed"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["catalog.sharing_fulfillment.read"]
// @Router /runtime/sharing-fulfillments/{request_id}/basis [post]
func (h *Handler) ReadSharingFulfillmentBasis(c *gin.Context) {
	tenant, ok := commonAuth.TenantIDFromGin(c)
	if !ok {
		respondError(c, 401, service.ErrUserPrincipalRequired)
		return
	}
	id, err := parseCanonicalUUID(c.Param("request_id"))
	var binding authorization.SharingFulfillmentBinding
	if err != nil || commonapi.BindOptionalJSONStrict(c, &binding) != nil || binding.Validate() != nil {
		respondError(c, 400, service.ErrInvalidEntryUpdate)
		return
	}
	result, err := h.entries.ReadSharingFulfillmentBasis(c.Request.Context(), tenant, id, binding)
	if err != nil {
		respondError(c, 500, err)
		return
	}
	c.JSON(200, result)
}
