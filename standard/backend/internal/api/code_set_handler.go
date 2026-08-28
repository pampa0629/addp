package api

import (
	"fmt"
	"net/http"
	"strconv"

	commoni18n "github.com/addp/common/middleware/i18n"
	sysi18n "github.com/addp/standard/i18n"
	"github.com/addp/standard/internal/models"
	"github.com/addp/standard/internal/service"
	"github.com/gin-gonic/gin"
)

type CodeSetHandler struct{ svc *service.CodeSetService }

func NewCodeSetHandler(svc *service.CodeSetService) *CodeSetHandler { return &CodeSetHandler{svc: svc} }

// ListCodeSets godoc
// @Summary 获取码值集列表 | List code sets
// @Tags Standard
// @Produce json
// @Param domain_id query int false "归属业务域 ID | Owning domain ID"
// @Param status query string false "修订状态 | Revision status"
// @Param keyword query string false "关键字 | Keyword"
// @Success 200 {object} models.PaginatedCodeSetResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.code_set.read"]
// @Router /code-sets [get]
// @Security BearerAuth
func (h *CodeSetHandler) ListCodeSets(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	var domainID *int64
	if raw := c.Query("domain_id"); raw != "" {
		value, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || value <= 0 {
			respondError(c, http.StatusBadRequest, fmt.Errorf("invalid domain_id"))
			return
		}
		domainID = &value
	}
	items, total, err := h.svc.ListCodeSets(getTenantID(c), domainID, c.Query("keyword"), c.Query("status"), page, pageSize)
	if err != nil {
		respondError(c, http.StatusInternalServerError, err)
		return
	}
	totalPages := int((total + int64(pageSize) - 1) / int64(pageSize))
	if totalPages < 1 {
		totalPages = 1
	}
	c.JSON(http.StatusOK, gin.H{"data": items, "total": total, "page": page, "page_size": pageSize, "total_pages": totalPages})
}

// CreateCodeSet godoc
// @Summary 创建码值集及首个草稿修订 | Create code set with initial draft revision
// @Tags Standard
// @Accept json
// @Produce json
// @Param request body models.CreateCodeSetRequest true "码值集和首个草稿 | Code set and initial draft"
// @Success 201 {object} models.CodeSetAggregate
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.code_set.create"]
// @Router /code-sets [post]
// @Security BearerAuth
func (h *CodeSetHandler) CreateCodeSet(c *gin.Context) {
	var req models.CreateCodeSetRequest
	if !bindJSON(c, &req) {
		return
	}
	result, err := h.svc.CreateCodeSet(getTenantID(c), getUserID(c), &req)
	if err != nil {
		respondError(c, http.StatusBadRequest, err)
		return
	}
	c.JSON(http.StatusCreated, result)
}

// GetCodeSet godoc
// @Summary 获取码值集聚合 | Get code set aggregate
// @Tags Standard
// @Produce json
// @Success 200 {object} models.CodeSetAggregate
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.code_set.read"]
// @Router /code-sets/{id} [get]
// @Security BearerAuth
func (h *CodeSetHandler) GetCodeSet(c *gin.Context) {
	id, ok := elementPathID(c, "id")
	if !ok {
		return
	}
	result, err := h.svc.GetCodeSet(id, getTenantID(c))
	if err != nil {
		respondError(c, http.StatusNotFound, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// UpdateCodeSet godoc
// @Summary 更新码值集归属信息 | Update code set ownership
// @Tags Standard
// @Accept json
// @Produce json
// @Param request body models.UpdateCodeSetRequest true "归属信息及并发版本 | Ownership and concurrency version"
// @Success 200 {object} models.CodeSetAggregate
// @Failure 409 {object} map[string]string "资源版本冲突 | Resource version conflict"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.code_set.update"]
// @Router /code-sets/{id} [put]
// @Security BearerAuth
func (h *CodeSetHandler) UpdateCodeSet(c *gin.Context) {
	id, ok := elementPathID(c, "id")
	if !ok {
		return
	}
	var req models.UpdateCodeSetRequest
	if !bindJSON(c, &req) {
		return
	}
	result, err := h.svc.UpdateCodeSet(id, getTenantID(c), getUserID(c), &req)
	if err != nil {
		respondError(c, http.StatusBadRequest, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// DeleteCodeSet godoc
// @Summary 删除码值集稳定身份 | Delete code set identity
// @Tags Standard
// @Produce json
// @Success 200 {object} map[string]string
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.code_set.delete"]
// @Router /code-sets/{id} [delete]
// @Security BearerAuth
func (h *CodeSetHandler) DeleteCodeSet(c *gin.Context) {
	id, ok := elementPathID(c, "id")
	if !ok {
		return
	}
	if err := h.svc.DeleteCodeSet(id, getTenantID(c)); err != nil {
		respondError(c, http.StatusBadRequest, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": commoni18n.T(c, sysi18n.MsgDeleteSuccess)})
}

// ListCodeSetRevisions godoc
// @Summary 获取码值集修订历史 | List code set revisions
// @Tags Standard
// @Produce json
// @Success 200 {array} models.CodeSetRevision
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.code_set.read"]
// @Router /code-sets/{id}/revisions [get]
// @Security BearerAuth
func (h *CodeSetHandler) ListCodeSetRevisions(c *gin.Context) {
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

// CreateCodeSetRevision godoc
// @Summary 从最近修订创建新草稿 | Create code set draft from latest revision
// @Tags Standard
// @Accept json
// @Produce json
// @Param request body models.CreateCodeSetRevisionRequest true "并发版本和变更说明 | Version and change summary"
// @Success 201 {object} models.CodeSetAggregate
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.code_set.update"]
// @Router /code-sets/{id}/revisions [post]
// @Security BearerAuth
func (h *CodeSetHandler) CreateCodeSetRevision(c *gin.Context) {
	id, ok := elementPathID(c, "id")
	if !ok {
		return
	}
	var req models.CreateCodeSetRevisionRequest
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

// GetCodeSetRevision godoc
// @Summary 获取码值集修订 | Get code set revision
// @Tags Standard
// @Produce json
// @Success 200 {object} models.CodeSetRevision
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.code_set.read"]
// @Router /code-sets/{id}/revisions/{revision_id} [get]
// @Security BearerAuth
func (h *CodeSetHandler) GetCodeSetRevision(c *gin.Context) {
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

// UpdateCodeSetRevision godoc
// @Summary 更新码值集草稿修订 | Update code set draft revision
// @Tags Standard
// @Accept json
// @Produce json
// @Param request body models.UpdateCodeSetRevisionRequest true "完整草稿及并发版本 | Full draft and concurrency version"
// @Success 200 {object} models.CodeSetAggregate
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.code_set.update"]
// @Router /code-sets/{id}/revisions/{revision_id} [put]
// @Security BearerAuth
func (h *CodeSetHandler) UpdateCodeSetRevision(c *gin.Context) {
	id, revisionID, ok := elementRevisionPath(c)
	if !ok {
		return
	}
	var req models.UpdateCodeSetRevisionRequest
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

// SubmitCodeSetRevision godoc
// @Summary 提交码值集修订审核 | Submit code set revision for review
// @Tags Standard
// @Accept json
// @Produce json
// @Param request body models.RevisionActionRequest true "当前资源版本 | Current resource version"
// @Success 200 {object} models.CodeSetAggregate
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.code_set.update"]
// @Router /code-sets/{id}/revisions/{revision_id}/submit [post]
// @Security BearerAuth
func (h *CodeSetHandler) SubmitCodeSetRevision(c *gin.Context) {
	h.revisionAction(c, h.svc.SubmitRevision)
}

// ReturnCodeSetRevision godoc
// @Summary 退回码值集修订 | Return code set revision to draft
// @Tags Standard
// @Accept json
// @Produce json
// @Param request body models.RevisionActionRequest true "当前资源版本 | Current resource version"
// @Success 200 {object} models.CodeSetAggregate
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.code_set.publish"]
// @Router /code-sets/{id}/revisions/{revision_id}/return [post]
// @Security BearerAuth
func (h *CodeSetHandler) ReturnCodeSetRevision(c *gin.Context) {
	h.revisionAction(c, h.svc.ReturnRevision)
}

// PublishCodeSetRevision godoc
// @Summary 发布码值集修订 | Publish code set revision
// @Tags Standard
// @Accept json
// @Produce json
// @Param request body models.RevisionActionRequest true "当前资源版本 | Current resource version"
// @Success 200 {object} models.CodeSetAggregate
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.code_set.publish"]
// @Router /code-sets/{id}/revisions/{revision_id}/publish [post]
// @Security BearerAuth
func (h *CodeSetHandler) PublishCodeSetRevision(c *gin.Context) {
	h.revisionAction(c, h.svc.PublishRevision)
}

// WithdrawCodeSetRevision godoc
// @Summary 撤回码值集发布修订 | Withdraw published code set revision
// @Tags Standard
// @Accept json
// @Produce json
// @Param request body models.RevisionActionRequest true "当前资源版本 | Current resource version"
// @Success 200 {object} models.CodeSetAggregate
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.code_set.publish"]
// @Router /code-sets/{id}/revisions/{revision_id}/withdraw [post]
// @Security BearerAuth
func (h *CodeSetHandler) WithdrawCodeSetRevision(c *gin.Context) {
	h.revisionAction(c, h.svc.WithdrawRevision)
}

// CreateCodeItem godoc
// @Summary 创建草稿码值项 | Create draft code item
// @Tags Standard
// @Accept json
// @Produce json
// @Param request body models.CreateCodeItemRequest true "码值项及并发版本 | Code item and concurrency version"
// @Success 201 {object} models.CodeItemMutationResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.code_set.update"]
// @Router /code-sets/{id}/revisions/{revision_id}/items [post]
// @Security BearerAuth
func (h *CodeSetHandler) CreateCodeItem(c *gin.Context) {
	id, revisionID, ok := elementRevisionPath(c)
	if !ok {
		return
	}
	var req models.CreateCodeItemRequest
	if !bindJSON(c, &req) {
		return
	}
	result, err := h.svc.CreateCodeItem(id, revisionID, getTenantID(c), &req)
	if err != nil {
		respondError(c, http.StatusBadRequest, err)
		return
	}
	c.JSON(http.StatusCreated, result)
}

// UpdateCodeItem godoc
// @Summary 更新草稿码值项 | Update draft code item
// @Tags Standard
// @Accept json
// @Produce json
// @Param request body models.UpdateCodeItemRequest true "码值项及并发版本 | Code item and concurrency version"
// @Success 200 {object} models.CodeItemMutationResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.code_set.update"]
// @Router /code-sets/{id}/revisions/{revision_id}/items/{item_id} [put]
// @Security BearerAuth
func (h *CodeSetHandler) UpdateCodeItem(c *gin.Context) {
	id, revisionID, itemID, ok := codeItemPath(c)
	if !ok {
		return
	}
	var req models.UpdateCodeItemRequest
	if !bindJSON(c, &req) {
		return
	}
	result, err := h.svc.UpdateCodeItem(id, revisionID, itemID, getTenantID(c), &req)
	if err != nil {
		respondError(c, http.StatusBadRequest, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// DeleteCodeItem godoc
// @Summary 删除草稿码值项 | Delete draft code item
// @Tags Standard
// @Accept json
// @Produce json
// @Param request body models.RevisionActionRequest true "当前资源版本 | Current resource version"
// @Success 200 {object} models.ResourceVersionResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.code_set.update"]
// @Router /code-sets/{id}/revisions/{revision_id}/items/{item_id} [delete]
// @Security BearerAuth
func (h *CodeSetHandler) DeleteCodeItem(c *gin.Context) {
	id, revisionID, itemID, ok := codeItemPath(c)
	if !ok {
		return
	}
	var req models.RevisionActionRequest
	if !bindJSON(c, &req) {
		return
	}
	if err := h.svc.DeleteCodeItem(id, revisionID, itemID, getTenantID(c), req.Version); err != nil {
		respondError(c, http.StatusBadRequest, err)
		return
	}
	c.JSON(http.StatusOK, models.ResourceVersionResponse{Version: req.Version + 1})
}

func (h *CodeSetHandler) revisionAction(c *gin.Context, action func(int64, int64, int64, int64, int64) (*models.CodeSetAggregate, error)) {
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
func codeItemPath(c *gin.Context) (int64, int64, int64, bool) {
	id, revisionID, ok := elementRevisionPath(c)
	if !ok {
		return 0, 0, 0, false
	}
	itemID, ok := elementPathID(c, "item_id")
	return id, revisionID, itemID, ok
}
