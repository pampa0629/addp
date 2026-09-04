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

type MetricHandler struct{ svc *service.MetricService }

func NewMetricHandler(svc *service.MetricService) *MetricHandler { return &MetricHandler{svc: svc} }

// @Summary 获取指标分类列表 | List metric categories
// @Tags Standard
// @Produce json
// @Success 200 {array} models.MetricCategory
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.metric.read"]
// @Router /metric-categories [get]
// @Security BearerAuth
func (h *MetricHandler) ListCategories(c *gin.Context) {
	items, err := h.svc.ListCategories(getTenantID(c))
	if err != nil {
		respondError(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, items)
}

// @Summary 创建指标分类 | Create metric category
// @Tags Standard
// @Accept json
// @Produce json
// @Param request body models.CreateMetricCategoryRequest true "指标分类 | Metric category"
// @Success 201 {object} models.MetricCategory
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.metric.create"]
// @Router /metric-categories [post]
// @Security BearerAuth
func (h *MetricHandler) CreateCategory(c *gin.Context) {
	var req models.CreateMetricCategoryRequest
	if !bindJSON(c, &req) {
		return
	}
	item, err := h.svc.CreateCategory(&req, getTenantID(c), getUserID(c))
	if err != nil {
		respondError(c, http.StatusBadRequest, err)
		return
	}
	c.JSON(http.StatusCreated, item)
}

// @Summary 更新指标分类 | Update metric category
// @Tags Standard
// @Accept json
// @Produce json
// @Param request body models.UpdateMetricCategoryRequest true "完整分类及并发版本 | Full category and concurrency version"
// @Success 200 {object} models.MetricCategory
// @Failure 409 {object} map[string]string "版本冲突 | Version conflict"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.metric.update"]
// @Router /metric-categories/{id} [put]
// @Security BearerAuth
func (h *MetricHandler) UpdateCategory(c *gin.Context) {
	id, ok := elementPathID(c, "id")
	if !ok {
		return
	}
	var req models.UpdateMetricCategoryRequest
	if !bindJSON(c, &req) {
		return
	}
	item, err := h.svc.UpdateCategory(id, getTenantID(c), getUserID(c), &req)
	if err != nil {
		respondError(c, http.StatusBadRequest, err)
		return
	}
	c.JSON(http.StatusOK, item)
}

// @Summary 删除指标分类 | Delete metric category
// @Tags Standard
// @Produce json
// @Success 200 {object} map[string]string
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.metric.delete"]
// @Router /metric-categories/{id} [delete]
// @Security BearerAuth
func (h *MetricHandler) DeleteCategory(c *gin.Context) {
	id, ok := elementPathID(c, "id")
	if !ok {
		return
	}
	if err := h.svc.DeleteCategory(id, getTenantID(c)); err != nil {
		respondError(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": commoni18n.T(c, sysi18n.MsgDeleteSuccess)})
}

// @Summary 获取指标定义列表 | List metric definitions
// @Tags Standard
// @Produce json
// @Param category_id query int false "分类 ID | Category ID"
// @Param owner_domain_id query int false "归属业务域 ID | Owning domain ID"
// @Param scope_type query string false "适用范围 | Scope" Enums(platform,tenant_common,domain)
// @Param metric_type query string false "指标类型 | Metric type" Enums(atomic,derived,composite)
// @Param status query string false "修订状态 | Revision status" Enums(draft,in_review,published,withdrawn)
// @Param keyword query string false "关键字 | Keyword"
// @Param as_of query string false "生效时点 | Effective point in time"
// @Success 200 {object} models.PaginatedMetricResponse
// @Failure 400 {object} map[string]string "请求无效 | Invalid request"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.metric.read"]
// @Router /metrics [get]
// @Security BearerAuth
func (h *MetricHandler) ListMetrics(c *gin.Context) {
	opts := repository.ListMetricOptions{MetricType: c.Query("metric_type"), Keyword: c.Query("keyword")}
	var err error
	if opts.ScopeType, err = parseOptionalStandardScope(c); err != nil {
		respondError(c, http.StatusBadRequest, err)
		return
	}
	if opts.Status, err = parseOptionalRevisionStatus(c); err != nil {
		respondError(c, http.StatusBadRequest, err)
		return
	}
	if opts.AsOf, err = parseOptionalAsOf(c); err != nil {
		respondError(c, http.StatusBadRequest, err)
		return
	}
	if opts.MetricType != "" && opts.MetricType != models.MetricTypeAtomic && opts.MetricType != models.MetricTypeDerived && opts.MetricType != models.MetricTypeComposite {
		respondError(c, http.StatusBadRequest, fmt.Errorf("invalid metric_type"))
		return
	}
	for key, target := range map[string]**int64{"category_id": &opts.CategoryID, "owner_domain_id": &opts.OwnerDomainID} {
		if raw := c.Query(key); raw != "" {
			id, e := strconv.ParseInt(raw, 10, 64)
			if e != nil || id <= 0 {
				respondError(c, http.StatusBadRequest, fmt.Errorf("invalid %s", key))
				return
			}
			*target = &id
		}
	}
	if raw := c.Query("page"); raw != "" {
		opts.Page, _ = strconv.Atoi(raw)
	}
	if raw := c.Query("page_size"); raw != "" {
		opts.PageSize, _ = strconv.Atoi(raw)
	}
	items, total, err := h.svc.ListMetrics(getTenantID(c), opts)
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

// @Summary 创建指标定义及首个草稿修订 | Create metric definition with initial draft revision
// @Tags Standard
// @Accept json
// @Produce json
// @Param request body models.CreateMetricRequest true "指标定义与首个草稿 | Metric definition and initial draft"
// @Success 201 {object} models.MetricDefinitionAggregate
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.metric.create"]
// @Router /metrics [post]
// @Security BearerAuth
func (h *MetricHandler) CreateMetric(c *gin.Context) {
	var req models.CreateMetricRequest
	if !bindJSON(c, &req) {
		return
	}
	item, err := h.svc.CreateMetric(&req, getTenantID(c), getUserID(c))
	if err != nil {
		respondError(c, http.StatusBadRequest, err)
		return
	}
	c.JSON(http.StatusCreated, item)
}

// @Summary 获取指标定义聚合 | Get metric definition aggregate
// @Tags Standard
// @Produce json
// @Param as_of query string false "生效时点 | Effective point in time"
// @Success 200 {object} models.MetricDefinitionAggregate
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.metric.read"]
// @Router /metrics/{id} [get]
// @Security BearerAuth
func (h *MetricHandler) GetMetric(c *gin.Context) {
	id, ok := elementPathID(c, "id")
	if !ok {
		return
	}
	asOf, err := parseOptionalAsOf(c)
	if err != nil {
		respondError(c, http.StatusBadRequest, err)
		return
	}
	item, err := h.svc.GetMetricAt(id, getTenantID(c), asOf)
	if err != nil {
		respondError(c, http.StatusNotFound, err)
		return
	}
	c.JSON(http.StatusOK, item)
}

// @Summary 查询指标定义专业关系图 | Get metric definition professional relation graph
// @Tags Professional Relations
// @Produce json
// @Param limit query int false "最大边数量 | Maximum edges"
// @Success 200 {object} models.ProfessionalRelationsResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.metric.read"]
// @Router /metrics/{id}/relations [get]
// @Security BearerAuth
func (h *MetricHandler) GetProfessionalRelations(c *gin.Context) {
	id, ok := elementPathID(c, "id")
	if !ok {
		return
	}
	limit := 100
	if raw := c.Query("limit"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 || value > 200 {
			respondError(c, http.StatusBadRequest, fmt.Errorf("invalid limit"))
			return
		}
		limit = value
	}
	result, err := h.svc.GetProfessionalRelations(id, getTenantID(c), limit)
	if err != nil {
		respondError(c, http.StatusNotFound, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// @Summary 更新指标定义归属信息 | Update metric definition ownership
// @Tags Standard
// @Accept json
// @Produce json
// @Param request body models.UpdateMetricRequest true "归属信息及并发版本 | Ownership and concurrency version"
// @Success 200 {object} models.MetricDefinitionAggregate
// @Failure 409 {object} map[string]string "版本冲突 | Version conflict"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.metric.update"]
// @Router /metrics/{id} [put]
// @Security BearerAuth
func (h *MetricHandler) UpdateMetric(c *gin.Context) {
	id, ok := elementPathID(c, "id")
	if !ok {
		return
	}
	var req models.UpdateMetricRequest
	if !bindJSON(c, &req) {
		return
	}
	item, err := h.svc.UpdateMetric(id, getTenantID(c), getUserID(c), &req)
	if err != nil {
		respondError(c, http.StatusBadRequest, err)
		return
	}
	c.JSON(http.StatusOK, item)
}

// @Summary 删除指标定义稳定身份 | Delete metric definition identity
// @Tags Standard
// @Produce json
// @Success 200 {object} map[string]string
// @Failure 409 {object} map[string]string "仍被引用 | Still referenced"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.metric.delete"]
// @Router /metrics/{id} [delete]
// @Security BearerAuth
func (h *MetricHandler) DeleteMetric(c *gin.Context) {
	id, ok := elementPathID(c, "id")
	if !ok {
		return
	}
	if err := h.svc.DeleteMetric(c.Request.Context(), id, getTenantID(c)); err != nil {
		respondError(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": commoni18n.T(c, sysi18n.MsgDeleteSuccess)})
}

// @Summary 获取指标定义修订历史 | List metric definition revisions
// @Tags Standard
// @Produce json
// @Success 200 {array} models.MetricDefinitionRevision
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.metric.read"]
// @Router /metrics/{id}/revisions [get]
// @Security BearerAuth
func (h *MetricHandler) ListMetricRevisions(c *gin.Context) {
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

// @Summary 从最近修订创建指标定义草稿 | Create metric definition draft from latest revision
// @Tags Standard
// @Accept json
// @Produce json
// @Param request body models.CreateMetricRevisionRequest true "并发版本与变更说明 | Version and change summary"
// @Success 201 {object} models.MetricDefinitionAggregate
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.metric.update"]
// @Router /metrics/{id}/revisions [post]
// @Security BearerAuth
func (h *MetricHandler) CreateMetricRevision(c *gin.Context) {
	id, ok := elementPathID(c, "id")
	if !ok {
		return
	}
	var req models.CreateMetricRevisionRequest
	if !bindJSON(c, &req) {
		return
	}
	item, err := h.svc.CreateRevision(id, getTenantID(c), getUserID(c), &req)
	if err != nil {
		respondError(c, http.StatusConflict, err)
		return
	}
	c.JSON(http.StatusCreated, item)
}

// @Summary 获取指标定义修订 | Get metric definition revision
// @Tags Standard
// @Produce json
// @Success 200 {object} models.MetricDefinitionRevision
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.metric.read"]
// @Router /metrics/{id}/revisions/{revision_id} [get]
// @Security BearerAuth
func (h *MetricHandler) GetMetricRevision(c *gin.Context) {
	id, revisionID, ok := elementRevisionPath(c)
	if !ok {
		return
	}
	item, err := h.svc.GetRevision(id, revisionID, getTenantID(c))
	if err != nil {
		respondError(c, http.StatusNotFound, err)
		return
	}
	c.JSON(http.StatusOK, item)
}

// @Summary 更新指标定义草稿修订 | Update metric definition draft revision
// @Tags Standard
// @Accept json
// @Produce json
// @Param request body models.UpdateMetricRevisionRequest true "完整草稿及并发版本 | Full draft and concurrency version"
// @Success 200 {object} models.MetricDefinitionAggregate
// @Failure 409 {object} map[string]string "状态或版本冲突 | State or version conflict"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.metric.update"]
// @Router /metrics/{id}/revisions/{revision_id} [put]
// @Security BearerAuth
func (h *MetricHandler) UpdateMetricRevision(c *gin.Context) {
	id, revisionID, ok := elementRevisionPath(c)
	if !ok {
		return
	}
	var req models.UpdateMetricRevisionRequest
	if !bindJSON(c, &req) {
		return
	}
	item, err := h.svc.UpdateRevision(id, revisionID, getTenantID(c), getUserID(c), &req)
	if err != nil {
		respondError(c, http.StatusBadRequest, err)
		return
	}
	c.JSON(http.StatusOK, item)
}

// @Summary 提交指标定义修订审核 | Submit metric definition revision for review
// @Tags Standard
// @Accept json
// @Produce json
// @Param request body models.RevisionActionRequest true "当前资源版本 | Current resource version"
// @Success 200 {object} models.MetricDefinitionAggregate
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.metric.update"]
// @Router /metrics/{id}/revisions/{revision_id}/submit [post]
// @Security BearerAuth
func (h *MetricHandler) SubmitMetricRevision(c *gin.Context) {
	h.revisionAction(c, h.svc.SubmitRevision)
}

// @Summary 退回指标定义修订 | Return metric definition revision
// @Tags Standard
// @Accept json
// @Produce json
// @Param request body models.RevisionActionRequest true "当前资源版本 | Current resource version"
// @Success 200 {object} models.MetricDefinitionAggregate
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.metric.publish"]
// @Router /metrics/{id}/revisions/{revision_id}/return [post]
// @Security BearerAuth
func (h *MetricHandler) ReturnMetricRevision(c *gin.Context) {
	h.revisionAction(c, h.svc.ReturnRevision)
}

// @Summary 发布指标定义修订 | Publish metric definition revision
// @Tags Standard
// @Accept json
// @Produce json
// @Param request body models.RevisionActionRequest true "当前资源版本 | Current resource version"
// @Success 200 {object} models.MetricDefinitionAggregate
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.metric.publish"]
// @Router /metrics/{id}/revisions/{revision_id}/publish [post]
// @Security BearerAuth
func (h *MetricHandler) PublishMetricRevision(c *gin.Context) {
	h.revisionAction(c, h.svc.PublishRevision)
}

// @Summary 撤回指标定义发布修订 | Withdraw published metric definition revision
// @Tags Standard
// @Accept json
// @Produce json
// @Param request body models.RevisionActionRequest true "当前资源版本 | Current resource version"
// @Success 200 {object} models.MetricDefinitionAggregate
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.metric.publish"]
// @Router /metrics/{id}/revisions/{revision_id}/withdraw [post]
// @Security BearerAuth
func (h *MetricHandler) WithdrawMetricRevision(c *gin.Context) {
	h.revisionAction(c, h.svc.WithdrawRevision)
}

// @Summary 解析已发布指标定义修订 | Resolve published metric definition revision
// @Tags Standard
// @Produce json
// @Success 200 {object} models.PublishedMetricDefinitionReference
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.metric.read"]
// @Router /metrics/{id}/revisions/{revision_id}/published-reference [get]
// @Security BearerAuth
func (h *MetricHandler) GetPublishedReference(c *gin.Context) {
	id, revisionID, ok := elementRevisionPath(c)
	if !ok {
		return
	}
	item, err := h.svc.GetPublishedReference(id, revisionID, getTenantID(c))
	if err != nil {
		respondError(c, http.StatusNotFound, err)
		return
	}
	c.JSON(http.StatusOK, item)
}

func (h *MetricHandler) revisionAction(c *gin.Context, action func(int64, int64, int64, int64, int64) (*models.MetricDefinitionAggregate, error)) {
	id, revisionID, ok := elementRevisionPath(c)
	if !ok {
		return
	}
	var req models.RevisionActionRequest
	if !bindJSON(c, &req) {
		return
	}
	item, err := action(id, revisionID, getTenantID(c), getUserID(c), req.Version)
	if err != nil {
		respondError(c, http.StatusConflict, err)
		return
	}
	c.JSON(http.StatusOK, item)
}
