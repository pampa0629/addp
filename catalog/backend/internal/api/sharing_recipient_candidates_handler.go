package api

import (
	"net/http"
	"strconv"

	"github.com/addp/catalog/internal/service"
	commonapi "github.com/addp/common/api"
	commonAuth "github.com/addp/common/middleware/auth"
	"github.com/gin-gonic/gin"
)

// ListSharingRecipientCandidates godoc
// @Summary 查询本条目的共享接收方候选 | List sharing recipient candidates for this entry
// @Description 当前 Tenant User 须具备条目读取及独立共享确认权限，并是可见有效 Meta 数据项的当前业务负责人。可选择同 Tenant 的有效账号或项目组，不要求本人加入该组；仅返回最小显示摘要，不返回成员、角色或数据，不授予共享或访问 | The current tenant user needs entry read, independent sharing confirmation permission and current business ownership of a visible active Meta data item. Active users or project groups in this tenant may be selected without group membership. Only minimal labels are returned, never members, roles, data or access grants
// @Tags Catalog Sharing
// @Produce json
// @Param id path string true "条目 UUID | Entry UUID"
// @Param recipient_type query string true "接收主体类型 | Recipient type" Enums(user,project_group)
// @Param search query string false "名称或编码，最多 100 字符 | Name or code, maximum 100 characters"
// @Param page query int false "页码，默认 1 | Page, default 1"
// @Param page_size query int false "每页数量，默认 20，最多 50 | Page size, default 20, maximum 50"
// @Success 200 {object} object{data=[]service.SharingRecipientCandidate,total=int64,page=int,page_size=int,total_pages=int}
// @Failure 400,401,403,404,409,500,503 {object} map[string]interface{} "请求、资格、来源变化或依赖错误 | Request, eligibility, source change or dependency error"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["catalog.entry.read","catalog.sharing_decision.create"]
// @Router /entries/{id}/sharing_recipient_candidates [get]
// @Security BearerAuth
func (h *Handler) ListSharingRecipientCandidates(c *gin.Context) {
	tenantID, ok := commonAuth.TenantIDFromGin(c)
	auth, authenticated := commonAuth.AuthContextFromGin(c)
	if !ok || !authenticated {
		respondError(c, http.StatusUnauthorized, service.ErrUserPrincipalRequired)
		return
	}
	id, err := parseCanonicalUUID(c.Param("id"))
	page, pageErr := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, sizeErr := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if err != nil || pageErr != nil || sizeErr != nil {
		respondError(c, http.StatusBadRequest, service.ErrInvalidPage)
		return
	}
	rows, total, err := h.entries.ListSharingRecipientCandidates(c.Request.Context(), tenantID, entryAccess(c), id, auth, c.Query("recipient_type"), c.Query("search"), page, size)
	if err != nil {
		respondError(c, http.StatusInternalServerError, err)
		return
	}
	commonapi.RespondPaginated(c, rows, total, page, size)
}
