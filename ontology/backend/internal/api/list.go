package api

import (
	"net/url"
	"strconv"

	commonapi "github.com/addp/common/api"
	"github.com/addp/ontology/internal/models"
	"github.com/addp/ontology/internal/repository"
	"github.com/gin-gonic/gin"
)

// listPage rejects ambiguous or unsupported selectors; generic pagination
// helpers intentionally normalize invalid values and do not fit this contract.
func listPage(c *gin.Context) (models.ListPage, bool) {
	p := models.ListPage{Page: 1, PageSize: 20}
	query, err := url.ParseQuery(c.Request.URL.RawQuery)
	if err != nil {
		fail(c, repository.ErrInvalid)
		return p, false
	}
	for key, values := range query {
		if (key != "page" && key != "page_size") || len(values) != 1 {
			fail(c, repository.ErrInvalid)
			return p, false
		}
		n, err := strconv.Atoi(values[0])
		if err != nil || strconv.Itoa(n) != values[0] {
			fail(c, repository.ErrInvalid)
			return p, false
		}
		if key == "page" {
			p.Page = n
		} else {
			p.PageSize = n
		}
	}
	if !p.Valid() {
		fail(c, repository.ErrInvalid)
		return p, false
	}
	return p, true
}

// ListOntologies lists only the authenticated Tenant's ontology heads.
// @Summary 分页浏览本体 | List ontology heads
// @Description 固定按 ontology_id 升序，仅接受 page/page_size；重复、未知参数或 OFFSET 超过 2147483647 时拒绝 | Ordered by ontology_id ascending; only page/page_size are accepted; duplicate or unknown parameters and OFFSET above 2147483647 are rejected
// @Tags Ontology
// @Produce json
// @Security BearerAuth
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["ontology.revision.read"]
// @Param page query int false "规范正整数页码 | Canonical positive page number" default(1) minimum(1)
// @Param page_size query int false "每页数量 | Page size" default(20) minimum(1) maximum(100)
// @Success 200 {object} commonapi.PaginatedResponse{data=[]HeadResponse} "本体头列表，空页为数组 | Ontology heads; empty pages are arrays"
// @Failure 400 {object} ErrorResponse "请求无效 | Invalid request"
// @Failure 401 {object} ErrorResponse "未认证 | Unauthenticated"
// @Failure 403 {object} ErrorResponse "禁止访问 | Forbidden"
// @Failure 500 {object} ErrorResponse "操作失败 | Operation failed"
// @Failure 503 {object} ErrorResponse "服务未就绪 | Module not ready"
// @Router /ontologies [get]
func (h *Handler) ListOntologies(c *gin.Context) {
	p, ok := listPage(c)
	if !ok {
		return
	}
	rows, total, err := h.revisions.ListOntologies(c.Request.Context(), requestActor(c), p)
	if err != nil {
		fail(c, err)
		return
	}
	data := make([]HeadResponse, 0, len(rows))
	for _, r := range rows {
		data = append(data, headResponse(&r))
	}
	commonapi.RespondPaginated(c, data, total, p.Page, p.PageSize)
}

// ListRevisions lists metadata, never executable definition payloads.
// @Summary 分页浏览修订历史 | List revision history
// @Description 固定按 revision 降序；不返回定义，published 不表示 active。仅接受 page/page_size，重复、未知参数或 OFFSET 超过 2147483647 时拒绝 | Ordered by revision descending; definitions are excluded and published does not mean active. Only page/page_size are accepted; duplicate or unknown parameters and OFFSET above 2147483647 are rejected
// @Tags Ontology
// @Produce json
// @Security BearerAuth
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["ontology.revision.read"]
// @Param ontology_id path string true "本体标识 | Ontology identifier"
// @Param page query int false "规范正整数页码 | Canonical positive page number" default(1) minimum(1)
// @Param page_size query int false "每页数量 | Page size" default(20) minimum(1) maximum(100)
// @Success 200 {object} commonapi.PaginatedResponse{data=[]models.RevisionSummary} "修订摘要，空页为数组 | Revision summaries; empty pages are arrays"
// @Failure 400 {object} ErrorResponse "请求无效 | Invalid request"
// @Failure 401 {object} ErrorResponse "未认证 | Unauthenticated"
// @Failure 403 {object} ErrorResponse "禁止访问 | Forbidden"
// @Failure 404 {object} ErrorResponse "本体不存在 | Ontology not found"
// @Failure 500 {object} ErrorResponse "操作失败 | Operation failed"
// @Failure 503 {object} ErrorResponse "服务未就绪 | Module not ready"
// @Router /ontologies/{ontology_id}/revisions [get]
func (h *Handler) ListRevisions(c *gin.Context) {
	a, s, ok := scope(c)
	if !ok {
		return
	}
	p, ok := listPage(c)
	if !ok {
		return
	}
	rows, total, err := h.revisions.ListRevisions(c.Request.Context(), a, s.OntologyID, p)
	if err != nil {
		fail(c, err)
		return
	}
	if rows == nil {
		rows = []models.RevisionSummary{}
	}
	commonapi.RespondPaginated(c, rows, total, p.Page, p.PageSize)
}
