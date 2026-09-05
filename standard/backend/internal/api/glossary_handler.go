package api

import (
	"fmt"
	"net/http"
	"strconv"

	commoni18n "github.com/addp/common/middleware/i18n"
	sysi18n "github.com/addp/standard/i18n"
	"github.com/addp/standard/internal/models"
	"github.com/addp/standard/internal/repository"
	"github.com/addp/standard/internal/service"
	"github.com/gin-gonic/gin"
)

type GlossaryHandler struct{ svc *service.GlossaryService }

func NewGlossaryHandler(svc *service.GlossaryService) *GlossaryHandler {
	return &GlossaryHandler{svc: svc}
}

// ListGlossaries godoc
// @Summary 获取业务术语列表 | List glossaries
// @Tags Standard
// @Produce json
// @Param element_id query int false "关联数据元 ID | Related data element ID"
// @Param scope_type query string false "适用范围 | Scope" Enums(platform,tenant_common,domain)
// @Param owner_domain_id query int false "归属业务域 ID，仅 domain 范围适用 | Owning domain ID, only for domain scope"
// @Param status query string false "修订状态 | Revision status" Enums(draft,in_review,published,withdrawn)
// @Param keyword query string false "关键字 | Keyword"
// @Param as_of query string false "生效时点（RFC3339，默认服务端当前时间） | Effective point in time (RFC3339, defaults to server time)"
// @Success 200 {object} models.PaginatedGlossaryResponse
// @Failure 400 {object} map[string]string "请求无效 | Invalid request"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.glossary.read"]
// @Router /glossaries [get]
// @Security BearerAuth
func (h *GlossaryHandler) ListGlossaries(c *gin.Context) {
	tenantID := getTenantID(c)
	status, err := parseOptionalRevisionStatus(c)
	if err != nil {
		respondError(c, http.StatusBadRequest, err)
		return
	}
	opts := repository.ListGlossaryOptions{Status: status, Keyword: c.Query("keyword")}
	if values := c.Request.URL.Query()["element_id"]; len(values) > 1 {
		respondError(c, http.StatusBadRequest, fmt.Errorf("duplicate element_id query parameter"))
		return
	}
	if value := c.Query("element_id"); value != "" {
		id, parseErr := strconv.ParseInt(value, 10, 64)
		if parseErr != nil || id <= 0 {
			respondError(c, http.StatusBadRequest, fmt.Errorf("invalid element_id"))
			return
		}
		opts.ElementID = &id
	}
	if opts.ScopeType, err = parseOptionalStandardScope(c); err != nil {
		respondError(c, http.StatusBadRequest, err)
		return
	}
	if opts.AsOf, err = parseOptionalAsOf(c); err != nil {
		respondError(c, http.StatusBadRequest, err)
		return
	}
	if values := c.Request.URL.Query()["owner_domain_id"]; len(values) > 1 {
		respondError(c, http.StatusBadRequest, fmt.Errorf("duplicate owner_domain_id query parameter"))
		return
	}
	if value := c.Query("owner_domain_id"); value != "" {
		id, parseErr := strconv.ParseInt(value, 10, 64)
		if parseErr != nil || id <= 0 {
			respondError(c, http.StatusBadRequest, fmt.Errorf("invalid owner_domain_id"))
			return
		}
		opts.OwnerDomainID = &id
	}
	if value := c.Query("page"); value != "" {
		opts.Page, _ = strconv.Atoi(value)
	}
	if value := c.Query("page_size"); value != "" {
		opts.PageSize, _ = strconv.Atoi(value)
	}
	items, total, err := h.svc.ListGlossaries(tenantID, opts)
	if err != nil {
		respondError(c, http.StatusInternalServerError, err)
		return
	}
	page, pageSize := opts.Page, opts.PageSize
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	totalPages := int((total + int64(pageSize) - 1) / int64(pageSize))
	if totalPages < 1 {
		totalPages = 1
	}
	c.JSON(http.StatusOK, gin.H{"data": items, "total": total, "page": page, "page_size": pageSize, "total_pages": totalPages})
}

// CreateGlossary godoc
// @Summary 创建业务术语及首个草稿修订 | Create glossary with initial draft revision
// @Tags Standard
// @Accept json
// @Produce json
// @Param request body models.CreateGlossaryRequest true "业务术语和首个草稿 | Glossary and initial draft"
// @Success 201 {object} models.GlossaryAggregate
// @Failure 400 {object} map[string]string "请求无效 | Invalid request"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.glossary.create"]
// @Router /glossaries [post]
// @Security BearerAuth
func (h *GlossaryHandler) CreateGlossary(c *gin.Context) {
	var req models.CreateGlossaryRequest
	if !bindJSON(c, &req) {
		return
	}
	result, err := h.svc.CreateGlossary(&req, getTenantID(c), getUserID(c))
	if err != nil {
		respondError(c, http.StatusBadRequest, err)
		return
	}
	c.JSON(http.StatusCreated, result)
}

// GetGlossary godoc
// @Summary 获取业务术语聚合 | Get glossary aggregate
// @Tags Standard
// @Produce json
// @Param as_of query string false "生效时点（RFC3339，默认服务端当前时间） | Effective point in time (RFC3339, defaults to server time)"
// @Success 200 {object} models.GlossaryAggregate
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.glossary.read"]
// @Router /glossaries/{id} [get]
// @Security BearerAuth
func (h *GlossaryHandler) GetGlossary(c *gin.Context) {
	id, ok := elementPathID(c, "id")
	if !ok {
		return
	}
	asOf, err := parseOptionalAsOf(c)
	if err != nil {
		respondError(c, http.StatusBadRequest, err)
		return
	}
	result, err := h.svc.GetGlossaryAt(id, getTenantID(c), asOf)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": commoni18n.T(c, sysi18n.MsgGlossaryNotFound)})
		return
	}
	c.JSON(http.StatusOK, result)
}

// UpdateGlossary godoc
// @Summary 更新业务术语归属信息 | Update glossary ownership
// @Tags Standard
// @Accept json
// @Produce json
// @Param request body models.UpdateGlossaryRequest true "归属信息及并发版本 | Ownership and concurrency version"
// @Success 200 {object} models.GlossaryAggregate
// @Failure 409 {object} map[string]string "资源版本冲突 | Resource version conflict"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.glossary.update"]
// @Router /glossaries/{id} [put]
// @Security BearerAuth
func (h *GlossaryHandler) UpdateGlossary(c *gin.Context) {
	id, ok := elementPathID(c, "id")
	if !ok {
		return
	}
	var req models.UpdateGlossaryRequest
	if !bindJSON(c, &req) {
		return
	}
	result, err := h.svc.UpdateGlossary(id, getTenantID(c), getUserID(c), &req)
	if err != nil {
		respondError(c, http.StatusBadRequest, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// DeleteGlossary godoc
// @Summary 删除从未发布的业务术语 | Delete a never-published glossary
// @Tags Standard
// @Produce json
// @Success 200 {object} map[string]string
// @Failure 409 {object} map[string]string "存在发布历史 | Publication history exists"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.glossary.delete"]
// @Router /glossaries/{id} [delete]
// @Security BearerAuth
func (h *GlossaryHandler) DeleteGlossary(c *gin.Context) {
	id, ok := elementPathID(c, "id")
	if !ok {
		return
	}
	if err := h.svc.DeleteGlossary(id, getTenantID(c)); err != nil {
		respondError(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": commoni18n.T(c, sysi18n.MsgDeleteSuccess)})
}

// ListGlossaryRevisions godoc
// @Summary 获取业务术语修订历史 | List glossary revisions
// @Tags Standard
// @Produce json
// @Success 200 {array} models.GlossaryRevision
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.glossary.read"]
// @Router /glossaries/{id}/revisions [get]
// @Security BearerAuth
func (h *GlossaryHandler) ListGlossaryRevisions(c *gin.Context) {
	id, ok := elementPathID(c, "id")
	if !ok {
		return
	}
	items, err := h.svc.ListRevisions(id, getTenantID(c))
	if err != nil {
		respondError(c, http.StatusNotFound, err)
		return
	}
	c.JSON(http.StatusOK, items)
}

// CreateGlossaryRevision godoc
// @Summary 从最近修订创建新草稿 | Create a new glossary draft from latest revision
// @Tags Standard
// @Accept json
// @Produce json
// @Param request body models.CreateGlossaryRevisionRequest true "并发版本和变更说明 | Version and change summary"
// @Success 201 {object} models.GlossaryAggregate
// @Failure 409 {object} map[string]string "草稿已存在或版本冲突 | Draft exists or version conflict"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.glossary.update"]
// @Router /glossaries/{id}/revisions [post]
// @Security BearerAuth
func (h *GlossaryHandler) CreateGlossaryRevision(c *gin.Context) {
	id, ok := elementPathID(c, "id")
	if !ok {
		return
	}
	var req models.CreateGlossaryRevisionRequest
	if !bindJSON(c, &req) {
		return
	}
	result, err := h.svc.CreateRevision(id, getTenantID(c), getUserID(c), &req)
	if err != nil {
		respondError(c, http.StatusConflict, err)
		return
	}
	c.JSON(http.StatusCreated, result)
}

// GetGlossaryRevision godoc
// @Summary 获取业务术语修订 | Get glossary revision
// @Tags Standard
// @Produce json
// @Success 200 {object} models.GlossaryRevision
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.glossary.read"]
// @Router /glossaries/{id}/revisions/{revision_id} [get]
// @Security BearerAuth
func (h *GlossaryHandler) GetGlossaryRevision(c *gin.Context) {
	id, revisionID, ok := glossaryRevisionPath(c)
	if !ok {
		return
	}
	result, err := h.svc.GetRevision(id, revisionID, getTenantID(c))
	if err != nil {
		respondError(c, http.StatusNotFound, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// UpdateGlossaryRevision godoc
// @Summary 更新业务术语草稿修订 | Update glossary draft revision
// @Tags Standard
// @Accept json
// @Produce json
// @Param request body models.UpdateGlossaryRevisionRequest true "完整草稿及并发版本 | Full draft and concurrency version"
// @Success 200 {object} models.GlossaryAggregate
// @Failure 409 {object} map[string]string "状态或版本冲突 | State or version conflict"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.glossary.update"]
// @Router /glossaries/{id}/revisions/{revision_id} [put]
// @Security BearerAuth
func (h *GlossaryHandler) UpdateGlossaryRevision(c *gin.Context) {
	id, revisionID, ok := glossaryRevisionPath(c)
	if !ok {
		return
	}
	var req models.UpdateGlossaryRevisionRequest
	if !bindJSON(c, &req) {
		return
	}
	result, err := h.svc.UpdateRevision(id, revisionID, getTenantID(c), getUserID(c), &req)
	if err != nil {
		respondError(c, http.StatusBadRequest, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// SubmitGlossaryRevision godoc
// @Summary 提交业务术语修订审核 | Submit glossary revision for review
// @Tags Standard
// @Accept json
// @Produce json
// @Param request body models.RevisionActionRequest true "当前资源版本 | Current resource version"
// @Success 200 {object} models.GlossaryAggregate
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.glossary.update"]
// @Router /glossaries/{id}/revisions/{revision_id}/submit [post]
// @Security BearerAuth
func (h *GlossaryHandler) SubmitGlossaryRevision(c *gin.Context) {
	h.revisionAction(c, h.svc.SubmitRevision)
}

// ReturnGlossaryRevision godoc
// @Summary 退回业务术语修订 | Return glossary revision to draft
// @Tags Standard
// @Accept json
// @Produce json
// @Param request body models.RevisionActionRequest true "当前资源版本 | Current resource version"
// @Success 200 {object} models.GlossaryAggregate
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.glossary.publish"]
// @Router /glossaries/{id}/revisions/{revision_id}/return [post]
// @Security BearerAuth
func (h *GlossaryHandler) ReturnGlossaryRevision(c *gin.Context) {
	h.revisionAction(c, h.svc.ReturnRevision)
}

// PublishGlossaryRevision godoc
// @Summary 发布业务术语修订 | Publish glossary revision
// @Tags Standard
// @Accept json
// @Produce json
// @Param request body models.RevisionActionRequest true "当前资源版本 | Current resource version"
// @Success 200 {object} models.GlossaryAggregate
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.glossary.publish"]
// @Router /glossaries/{id}/revisions/{revision_id}/publish [post]
// @Security BearerAuth
func (h *GlossaryHandler) PublishGlossaryRevision(c *gin.Context) {
	h.revisionAction(c, h.svc.PublishRevision)
}

// WithdrawGlossaryRevision godoc
// @Summary 撤回业务术语发布修订 | Withdraw published glossary revision
// @Tags Standard
// @Accept json
// @Produce json
// @Param request body models.RevisionActionRequest true "当前资源版本 | Current resource version"
// @Success 200 {object} models.GlossaryAggregate
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.glossary.publish"]
// @Router /glossaries/{id}/revisions/{revision_id}/withdraw [post]
// @Security BearerAuth
func (h *GlossaryHandler) WithdrawGlossaryRevision(c *gin.Context) {
	h.revisionAction(c, h.svc.WithdrawRevision)
}

// GetElementMappings godoc
// @Summary 获取术语关联的数据元 | Get element mappings of glossary
// @Tags Standard
// @Produce json
// @Success 200 {array} models.PublishedElementReference
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.glossary.read","standard.element.read"]
// @Router /glossaries/{id}/elements [get]
// @Security BearerAuth
func (h *GlossaryHandler) GetElementMappings(c *gin.Context) {
	id, ok := elementPathID(c, "id")
	if !ok {
		return
	}
	items, err := h.svc.GetMappedElements(id, getTenantID(c))
	if err != nil {
		respondError(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, items)
}

// UpdateElementMappings godoc
// @Summary 替换术语关联的数据元 | Replace element mappings of glossary
// @Tags Standard
// @Accept json
// @Produce json
// @Param request body models.UpdateGlossaryElementsRequest true "数据元身份及并发版本 | Element identities and concurrency version"
// @Success 200 {object} models.GlossaryAggregate
// @Failure 409 {object} map[string]string "资源版本冲突 | Resource version conflict"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.glossary.update","standard.element.read"]
// @Router /glossaries/{id}/elements [put]
// @Security BearerAuth
func (h *GlossaryHandler) UpdateElementMappings(c *gin.Context) {
	id, ok := elementPathID(c, "id")
	if !ok {
		return
	}
	var req models.UpdateGlossaryElementsRequest
	if !bindJSON(c, &req) {
		return
	}
	result, err := h.svc.UpdateElements(id, getTenantID(c), getUserID(c), &req)
	if err != nil {
		respondError(c, http.StatusBadRequest, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *GlossaryHandler) revisionAction(c *gin.Context, action func(int64, int64, int64, int64, int64) (*models.GlossaryAggregate, error)) {
	id, revisionID, ok := glossaryRevisionPath(c)
	if !ok {
		return
	}
	var req models.RevisionActionRequest
	if !bindJSON(c, &req) {
		return
	}
	result, err := action(id, revisionID, getTenantID(c), getUserID(c), req.Version)
	if err != nil {
		respondError(c, http.StatusConflict, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

func glossaryRevisionPath(c *gin.Context) (int64, int64, bool) {
	id, ok := elementPathID(c, "id")
	if !ok {
		return 0, 0, false
	}
	revisionID, ok := elementPathID(c, "revision_id")
	return id, revisionID, ok
}
