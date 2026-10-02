package api

import (
	"net/http"

	"github.com/addp/catalog/internal/service"
	commonapi "github.com/addp/common/api"
	commonAuth "github.com/addp/common/middleware/auth"
	"github.com/gin-gonic/gin"
)

// ListSharingDecisionCandidates godoc
// @Summary 读取可见条目的共享办理候选 | Read sharing handling candidates for a visible entry
// @Description 当前 Tenant User 需条目读取、独立源授权办理权限与目标引擎管理委派。只返回当前来源、原负责人关系及期限有效的分页摘要，不返回完整业务用途或全租户历史；不是受理依据，不授予确认或数据访问，提交仍须重新核验确认人的当前 IAM 资格 | The current Tenant User needs entry read, independent source authorization handling permission and target engine management delegation. Returns paginated summaries with a current source, original owner relation and valid expiry, not purpose text or tenant-wide history. Not acceptance evidence, confirmation permission or data access; submission still rechecks the confirmer's current IAM eligibility
// @Tags Catalog Sharing
// @Produce json
// @Param id path string true "条目 UUID | Entry UUID"
// @Param page query int false "页码，默认 1 | Page, default 1"
// @Param page_size query int false "每页数量，默认 10，最多 100 | Page size, default 10, maximum 100"
// @Success 200 {object} object{data=[]service.SharingDecisionCandidate,total=int64,page=int,page_size=int,total_pages=int} "当前候选摘要 | Current candidate summaries"
// @Failure 400,401,403,404,500,503 {object} map[string]interface{} "请求、资格或依赖错误 | Request, eligibility or dependency error"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["catalog.entry.read","system.engine_access_fulfillment.create"]
// @Router /entries/{id}/sharing_decision_candidates [get]
// @Security BearerAuth
func (h *Handler) ListSharingDecisionCandidates(c *gin.Context) {
	tenantID, ok := commonAuth.TenantIDFromGin(c)
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
	page, size := commonapi.ParsePagination(c)
	// The auth middleware has verified this exact Bearer. Never accept body
	// identities, X-Tenant-ID or a Service token as the human's provenance.
	rows, total, err := h.entries.ListSharingDecisionCandidates(c.Request.Context(), tenantID, entryAccess(c), id, auth,
		commonAuth.CanonicalBearerToken(c.GetHeader("Authorization")), page, size)
	if err != nil {
		respondError(c, http.StatusInternalServerError, err)
		return
	}
	commonapi.RespondPaginated(c, rows, total, page, size)
}
