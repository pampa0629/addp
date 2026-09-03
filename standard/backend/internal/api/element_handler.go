package api

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/addp/common/dataquality"
	commoni18n "github.com/addp/common/middleware/i18n"
	sysi18n "github.com/addp/standard/i18n"
	"github.com/addp/standard/internal/models"
	"github.com/addp/standard/internal/repository"
	"github.com/addp/standard/internal/service"
	"github.com/gin-gonic/gin"
)

type ElementHandler struct{ svc *service.ElementService }

func NewElementHandler(svc *service.ElementService) *ElementHandler { return &ElementHandler{svc: svc} }

// ListElements godoc
// @Summary 获取数据元列表 | List data elements
// @Tags Standard
// @Produce json
// @Param ids query string false "数据元 ID 集合，逗号分隔，最多 100 个 | Data element IDs, comma-separated, maximum 100"
// @Param scope_type query string false "适用范围 | Scope" Enums(platform,tenant_common,domain)
// @Param owner_domain_id query int false "归属业务域 ID，仅 domain 范围适用 | Owning domain ID, only for domain scope"
// @Param status query string false "修订状态 | Revision status" Enums(draft,in_review,published,withdrawn)
// @Param keyword query string false "关键字 | Keyword"
// @Param as_of query string false "生效时点（RFC3339，默认服务端当前时间） | Effective point in time (RFC3339, defaults to server time)"
// @Success 200 {object} models.PaginatedElementResponse
// @Failure 400 {object} map[string]string "请求无效 | Invalid request"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.element.read"]
// @Router /elements [get]
// @Security BearerAuth
func (h *ElementHandler) ListElements(c *gin.Context) {
	if len(c.Request.URL.Query()["ids"]) > 1 {
		respondError(c, http.StatusBadRequest, fmt.Errorf("duplicate element ids filter"))
		return
	}
	ids, err := parseElementIDs(c.Query("ids"))
	if err != nil {
		respondError(c, http.StatusBadRequest, err)
		return
	}
	status, err := parseOptionalRevisionStatus(c)
	if err != nil {
		respondError(c, http.StatusBadRequest, err)
		return
	}
	opts := repository.ListElementOptions{IDs: ids, Status: status, Keyword: c.Query("keyword")}
	if opts.ScopeType, err = parseOptionalStandardScope(c); err != nil {
		respondError(c, http.StatusBadRequest, err)
		return
	}
	if opts.AsOf, err = parseOptionalAsOf(c); err != nil {
		respondError(c, http.StatusBadRequest, err)
		return
	}
	ownerDomainValues := c.Request.URL.Query()["owner_domain_id"]
	if len(ownerDomainValues) > 1 {
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
	items, total, err := h.svc.ListElements(getTenantID(c), opts)
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

func parseElementIDs(value string) ([]int64, error) {
	if value == "" {
		return nil, nil
	}
	parts := strings.Split(value, ",")
	if len(parts) > 100 {
		return nil, fmt.Errorf("too many element ids")
	}
	ids := make([]int64, 0, len(parts))
	seen := map[int64]struct{}{}
	for _, part := range parts {
		if part == "" || strings.TrimSpace(part) != part {
			return nil, fmt.Errorf("invalid element ids")
		}
		id, err := strconv.ParseInt(part, 10, 64)
		if err != nil || id <= 0 {
			return nil, fmt.Errorf("invalid element ids")
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	return ids, nil
}

// CreateElement godoc
// @Summary 创建数据元及首个草稿修订 | Create data element with initial draft revision
// @Tags Standard
// @Accept json
// @Produce json
// @Param request body models.CreateElementRequest true "数据元和首个草稿 | Data element and initial draft"
// @Success 201 {object} models.ElementAggregate
// @Failure 400 {object} map[string]string "请求无效 | Invalid request"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.element.create"]
// @Router /elements [post]
// @Security BearerAuth
func (h *ElementHandler) CreateElement(c *gin.Context) {
	var req models.CreateElementRequest
	if !bindJSON(c, &req) {
		return
	}
	result, err := h.svc.CreateElement(&req, getTenantID(c), getUserID(c))
	if err != nil {
		respondError(c, http.StatusBadRequest, err)
		return
	}
	c.JSON(http.StatusCreated, result)
}

// GetElement godoc
// @Summary 获取数据元聚合 | Get data element aggregate
// @Tags Standard
// @Produce json
// @Param as_of query string false "生效时点（RFC3339，默认服务端当前时间） | Effective point in time (RFC3339, defaults to server time)"
// @Success 200 {object} models.ElementAggregate
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.element.read"]
// @Router /elements/{id} [get]
// @Security BearerAuth
func (h *ElementHandler) GetElement(c *gin.Context) {
	id, ok := elementPathID(c, "id")
	if !ok {
		return
	}
	asOf, err := parseOptionalAsOf(c)
	if err != nil {
		respondError(c, http.StatusBadRequest, err)
		return
	}
	result, err := h.svc.GetElementAt(id, getTenantID(c), asOf)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": commoni18n.T(c, sysi18n.MsgElementNotFound)})
		return
	}
	c.JSON(http.StatusOK, result)
}

// UpdateElement godoc
// @Summary 更新数据元归属信息 | Update data element ownership
// @Tags Standard
// @Accept json
// @Produce json
// @Param request body models.UpdateElementRequest true "归属信息及并发版本 | Ownership and concurrency version"
// @Success 200 {object} models.ElementAggregate
// @Failure 409 {object} map[string]string "资源版本冲突 | Resource version conflict"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.element.update"]
// @Router /elements/{id} [put]
// @Security BearerAuth
func (h *ElementHandler) UpdateElement(c *gin.Context) {
	id, ok := elementPathID(c, "id")
	if !ok {
		return
	}
	var req models.UpdateElementRequest
	if !bindJSON(c, &req) {
		return
	}
	result, err := h.svc.UpdateElement(id, getTenantID(c), getUserID(c), &req)
	if err != nil {
		respondError(c, http.StatusBadRequest, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// DeleteElement godoc
// @Summary 删除数据元稳定身份 | Delete data element identity
// @Tags Standard
// @Produce json
// @Success 200 {object} map[string]string
// @Failure 409 {object} map[string]string "仍被引用 | Still referenced"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.element.delete"]
// @Router /elements/{id} [delete]
// @Security BearerAuth
func (h *ElementHandler) DeleteElement(c *gin.Context) {
	id, ok := elementPathID(c, "id")
	if !ok {
		return
	}
	if err := h.svc.DeleteElement(c.Request.Context(), id, getTenantID(c)); err != nil {
		respondError(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": commoni18n.T(c, sysi18n.MsgDeleteSuccess)})
}

// ListElementRevisions godoc
// @Summary 获取数据元修订历史 | List data element revisions
// @Tags Standard
// @Produce json
// @Success 200 {array} models.ElementRevision
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.element.read"]
// @Router /elements/{id}/revisions [get]
// @Security BearerAuth
func (h *ElementHandler) ListElementRevisions(c *gin.Context) {
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

// CreateElementRevision godoc
// @Summary 从最近修订创建新草稿 | Create a new draft from latest revision
// @Tags Standard
// @Accept json
// @Produce json
// @Param request body models.CreateElementRevisionRequest true "并发版本和变更说明 | Version and change summary"
// @Success 201 {object} models.ElementAggregate
// @Failure 409 {object} map[string]string "草稿已存在或版本冲突 | Draft exists or version conflict"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.element.update"]
// @Router /elements/{id}/revisions [post]
// @Security BearerAuth
func (h *ElementHandler) CreateElementRevision(c *gin.Context) {
	id, ok := elementPathID(c, "id")
	if !ok {
		return
	}
	var req models.CreateElementRevisionRequest
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

// GetElementRevision godoc
// @Summary 获取数据元修订 | Get data element revision
// @Tags Standard
// @Produce json
// @Success 200 {object} models.ElementRevision
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.element.read"]
// @Router /elements/{id}/revisions/{revision_id} [get]
// @Security BearerAuth
func (h *ElementHandler) GetElementRevision(c *gin.Context) {
	id, revisionID, ok := elementRevisionPath(c)
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

// UpdateElementRevision godoc
// @Summary 更新数据元草稿修订 | Update data element draft revision
// @Tags Standard
// @Accept json
// @Produce json
// @Param request body models.UpdateElementRevisionRequest true "完整草稿及并发版本 | Full draft and concurrency version"
// @Success 200 {object} models.ElementAggregate
// @Failure 409 {object} map[string]string "状态或版本冲突 | State or version conflict"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.element.update"]
// @Router /elements/{id}/revisions/{revision_id} [put]
// @Security BearerAuth
func (h *ElementHandler) UpdateElementRevision(c *gin.Context) {
	id, revisionID, ok := elementRevisionPath(c)
	if !ok {
		return
	}
	var req models.UpdateElementRevisionRequest
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

// SubmitElementRevision godoc
// @Summary 提交数据元修订审核 | Submit data element revision for review
// @Tags Standard
// @Accept json
// @Produce json
// @Param request body models.RevisionActionRequest true "当前资源版本 | Current resource version"
// @Success 200 {object} models.ElementAggregate
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.element.update"]
// @Router /elements/{id}/revisions/{revision_id}/submit [post]
// @Security BearerAuth
func (h *ElementHandler) SubmitElementRevision(c *gin.Context) {
	h.revisionAction(c, h.svc.SubmitRevision)
}

// ReturnElementRevision godoc
// @Summary 退回数据元修订 | Return data element revision to draft
// @Tags Standard
// @Accept json
// @Produce json
// @Param request body models.RevisionActionRequest true "当前资源版本 | Current resource version"
// @Success 200 {object} models.ElementAggregate
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.element.publish"]
// @Router /elements/{id}/revisions/{revision_id}/return [post]
// @Security BearerAuth
func (h *ElementHandler) ReturnElementRevision(c *gin.Context) {
	h.revisionAction(c, h.svc.ReturnRevision)
}

// PublishElementRevision godoc
// @Summary 发布数据元修订 | Publish data element revision
// @Tags Standard
// @Accept json
// @Produce json
// @Param request body models.RevisionActionRequest true "当前资源版本 | Current resource version"
// @Success 200 {object} models.ElementAggregate
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.element.publish"]
// @Router /elements/{id}/revisions/{revision_id}/publish [post]
// @Security BearerAuth
func (h *ElementHandler) PublishElementRevision(c *gin.Context) {
	h.revisionAction(c, h.svc.PublishRevision)
}

// WithdrawElementRevision godoc
// @Summary 撤回数据元发布修订 | Withdraw published data element revision
// @Tags Standard
// @Accept json
// @Produce json
// @Param request body models.RevisionActionRequest true "当前资源版本 | Current resource version"
// @Success 200 {object} models.ElementAggregate
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.element.publish"]
// @Router /elements/{id}/revisions/{revision_id}/withdraw [post]
// @Security BearerAuth
func (h *ElementHandler) WithdrawElementRevision(c *gin.Context) {
	h.revisionAction(c, h.svc.WithdrawRevision)
}

// GetElementQualityRules godoc
// @Summary 获取指定时点生效的数据元质量规则快照 | Get the effective element quality rule snapshot at a point in time
// @Tags Standard
// @Produce json
// @Param as_of query string false "生效时点（RFC3339，默认服务端当前时间） | Effective point in time (RFC3339, defaults to server time)"
// @Success 200 {object} models.PublishedElementQualityRulesResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.element.read"]
// @Router /elements/{id}/quality-rules [get]
// @Security BearerAuth
func (h *ElementHandler) GetElementQualityRules(c *gin.Context) {
	id, ok := elementPathID(c, "id")
	if !ok {
		return
	}
	asOf, err := parseOptionalAsOf(c)
	if err != nil {
		respondError(c, http.StatusBadRequest, err)
		return
	}
	revision, rules, err := h.svc.GetPublishedQualityRulesAt(id, getTenantID(c), asOf)
	if err != nil {
		respondError(c, http.StatusNotFound, err)
		return
	}
	value, err := dataquality.ToMap(*rules)
	if err != nil {
		respondError(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, models.PublishedElementQualityRulesResponse{ElementID: id, ElementRevisionID: revision.ID, RevisionNo: revision.RevisionNo, QualityRules: models.JSONB(value)})
}

func (h *ElementHandler) revisionAction(c *gin.Context, action func(int64, int64, int64, int64, int64) (*models.ElementAggregate, error)) {
	id, revisionID, ok := elementRevisionPath(c)
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
func elementPathID(c *gin.Context, name string) (int64, bool) {
	id, err := strconv.ParseInt(c.Param(name), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, sysi18n.MsgInvalidID)})
		return 0, false
	}
	return id, true
}
func elementRevisionPath(c *gin.Context) (int64, int64, bool) {
	id, ok := elementPathID(c, "id")
	if !ok {
		return 0, 0, false
	}
	revisionID, ok := elementPathID(c, "revision_id")
	return id, revisionID, ok
}
func bindJSON(c *gin.Context, value interface{}) bool {
	if err := c.ShouldBindJSON(value); err != nil {
		respondError(c, http.StatusBadRequest, err)
		return false
	}
	return true
}
