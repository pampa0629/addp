package api

import (
	"net/http"
	"net/url"
	"strconv"

	"github.com/addp/catalog/internal/service"
	commonapi "github.com/addp/common/api"
	commonAuth "github.com/addp/common/middleware/auth"
	"github.com/gin-gonic/gin"
)

// ListSharingFulfillments godoc
// @Summary 找回本人原办理请求 | Find my original fulfillment requests
// @Description 当前 Tenant User 须具备条目读取、独立办理权及原引擎当前管理范围。仅列本人在仍可见条目下的原参数，资格过滤后分页，不查询他人历史；不提交、关闭、核清或授权 | Requires current tenant user entry-read and independent fulfillment permissions plus current management scope for each original engine. Paginate only my original parameters under a still-visible entry after scope filtering; never submit, close, settle or grant access
// @Tags Catalog Sharing
// @Produce json
// @Security BearerAuth
// @Param id path string true "条目 UUID | Entry UUID"
// @Param page query int false "页码，默认 1 | Page, default 1" default(1)
// @Param page_size query int false "每页数量，默认 20，最多 100 | Page size, default 20, maximum 100" default(20)
// @Success 200 {object} object{data=[]service.SharingFulfillmentRequest,total=int64,page=int,page_size=int,total_pages=int} "本人原请求 | My original requests"
// @Failure 400,401,403,404,500,503 {object} map[string]interface{} "请求、资格或依赖错误 | Request, eligibility or dependency error"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["catalog.entry.read","system.engine_access_fulfillment.create"]
// @Router /entries/{id}/sharing_fulfillments [get]
func (h *Handler) ListSharingFulfillments(c *gin.Context) {
	tenant, ok := commonAuth.TenantIDFromGin(c)
	auth, authenticated := commonAuth.AuthContextFromGin(c)
	if !ok || !authenticated {
		respondError(c, http.StatusUnauthorized, service.ErrUserPrincipalRequired)
		return
	}
	id, err := parseCanonicalUUID(c.Param("id"))
	if err != nil {
		respondError(c, http.StatusBadRequest, service.ErrInvalidEntryUpdate)
		return
	}
	query, err := url.ParseQuery(c.Request.URL.RawQuery)
	if err != nil {
		respondError(c, http.StatusBadRequest, service.ErrInvalidPage)
		return
	}
	for key, values := range query {
		if (key != "page" && key != "page_size") || len(values) != 1 || values[0] == "" {
			respondError(c, http.StatusBadRequest, service.ErrInvalidPage)
			return
		}
	}
	page, pageErr := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, sizeErr := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if pageErr != nil || sizeErr != nil {
		respondError(c, http.StatusBadRequest, service.ErrInvalidPage)
		return
	}
	rows, total, err := h.entries.ListSharingFulfillments(c.Request.Context(), tenant, entryAccess(c), id, auth,
		commonAuth.CanonicalBearerToken(c.GetHeader("Authorization")), page, size)
	if err != nil {
		respondError(c, http.StatusInternalServerError, err)
		return
	}
	commonapi.RespondPaginated(c, rows, total, page, size)
}

// GetSharingFulfillment godoc
// @Summary 只读查询本人原办理结果 | Read my original fulfillment outcome
// @Description 按持久完整绑定读取 System 权威结果；仅本人、当前条目可见、独立办理权及原引擎当前管理范围。pending 表示权威未找到，不等于关闭；accepted 是历史受理而非 Grant。查询不提交、关闭或延长窗口 | Resolve the persisted exact binding against System; requires original operator, current entry visibility, independent fulfillment permission and original-engine management scope. Pending means authoritatively not found, not closed; accepted is historical acceptance, not a Grant. Never submits, closes or renews the window
// @Tags Catalog Sharing
// @Produce json
// @Security BearerAuth
// @Param id path string true "条目 UUID | Entry UUID"
// @Param request_id path string true "原请求 UUID，由本人原请求列表取得 | Original request UUID from my request list"
// @Success 200 {object} service.SharingFulfillmentHistory "原请求及只读权威结果，不是 Grant | Original request and read-only authoritative outcome, not a Grant"
// @Failure 400,401,403,404,409,500,503 {object} map[string]interface{} "请求、资格、绑定或依赖错误 | Request, eligibility, binding or dependency error"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["catalog.entry.read","system.engine_access_fulfillment.create"]
// @Router /entries/{id}/sharing_fulfillments/{request_id} [get]
func (h *Handler) GetSharingFulfillment(c *gin.Context) {
	tenant, ok := commonAuth.TenantIDFromGin(c)
	auth, authenticated := commonAuth.AuthContextFromGin(c)
	if !ok || !authenticated {
		respondError(c, http.StatusUnauthorized, service.ErrUserPrincipalRequired)
		return
	}
	id, err := parseCanonicalUUID(c.Param("id"))
	requestID, requestErr := parseCanonicalUUID(c.Param("request_id"))
	if err != nil || requestErr != nil || c.Request.URL.RawQuery != "" {
		respondError(c, http.StatusBadRequest, service.ErrInvalidEntryUpdate)
		return
	}
	result, err := h.entries.GetSharingFulfillment(c.Request.Context(), tenant, entryAccess(c), id, requestID, auth,
		commonAuth.CanonicalBearerToken(c.GetHeader("Authorization")))
	if err != nil {
		respondError(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, result)
}
