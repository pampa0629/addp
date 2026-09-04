package api

import (
	"context"
	"net/http"
	"strconv"

	commonapi "github.com/addp/common/api"
	commoni18n "github.com/addp/common/middleware/i18n"
	sysi18n "github.com/addp/standard/i18n"
	"github.com/addp/standard/internal/models"
	"github.com/addp/standard/internal/service"
	"github.com/gin-gonic/gin"
)

type StandardCollectionHandler struct {
	svc *service.StandardCollectionService
}

func NewStandardCollectionHandler(svc *service.StandardCollectionService) *StandardCollectionHandler {
	return &StandardCollectionHandler{svc: svc}
}

// List godoc
// @Summary 查询标准集 | List standard collections
// @Tags Standard Collections
// @Produce json
// @Param keyword query string false "名称、编码或说明 | Name, code, or description"
// @Param status query string false "当前治理状态 | Current governance status" Enums(draft,in_review,published)
// @Param page query int false "页码 | Page"
// @Param page_size query int false "每页数量 | Page size"
// @Success 200 {object} object{data=[]models.StandardCollectionAggregate,total=int64,page=int,page_size=int,total_pages=int}
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Failure 403 {object} map[string]string
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.collection.read"]
// @Router /collections [get]
// @Security BearerAuth
func (h *StandardCollectionHandler) List(c *gin.Context) {
	page, err1 := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, err2 := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if err1 != nil || err2 != nil || page < 1 || pageSize < 1 || pageSize > 100 {
		respondError(c, http.StatusBadRequest, service.ErrInvalidStandardCollection)
		return
	}
	items, total, err := h.svc.List(c.Request.Context(), getTenantID(c), getUserID(c), c.Query("keyword"), c.Query("status"), page, pageSize)
	if err != nil {
		respondError(c, http.StatusInternalServerError, err)
		return
	}
	commonapi.RespondPaginated(c, items, total, page, pageSize)
}

// Create godoc
// @Summary 创建标准集 | Create a standard collection
// @Tags Standard Collections
// @Accept json
// @Produce json
// @Param request body models.CreateStandardCollectionRequest true "标准集首个草稿 | Initial collection draft"
// @Success 201 {object} models.StandardCollectionAggregate
// @Failure 400 {object} map[string]string
// @Failure 409 {object} map[string]string
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.collection.create"]
// @Router /collections [post]
// @Security BearerAuth
func (h *StandardCollectionHandler) Create(c *gin.Context) {
	var req models.CreateStandardCollectionRequest
	if err := commonapi.BindOptionalJSONStrict(c, &req); err != nil {
		respondError(c, http.StatusBadRequest, err)
		return
	}
	result, err := h.svc.Create(c.Request.Context(), getTenantID(c), getUserID(c), &req)
	if err != nil {
		respondError(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusCreated, result)
}

// Get godoc
// @Summary 获取标准集详情 | Get standard collection detail
// @Tags Standard Collections
// @Produce json
// @Param id path int true "标准集 ID | Collection ID"
// @Success 200 {object} models.StandardCollectionAggregate
// @Failure 404 {object} map[string]string
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.collection.read"]
// @Router /collections/{id} [get]
// @Security BearerAuth
func (h *StandardCollectionHandler) Get(c *gin.Context) {
	id, ok := parseCollectionID(c, "id")
	if !ok {
		return
	}
	result, err := h.svc.Get(c.Request.Context(), id, getTenantID(c), getUserID(c))
	if err != nil {
		respondError(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// Delete godoc
// @Summary 删除未发布标准集 | Delete an unpublished standard collection
// @Tags Standard Collections
// @Accept json
// @Produce json
// @Param id path int true "标准集 ID | Collection ID"
// @Param request body models.VersionRequest true "当前资源版本 | Current resource version"
// @Success 200 {object} map[string]string
// @Failure 403 {object} map[string]string
// @Failure 409 {object} map[string]string
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.collection.delete"]
// @Router /collections/{id} [delete]
// @Security BearerAuth
func (h *StandardCollectionHandler) Delete(c *gin.Context) {
	id, ok := parseCollectionID(c, "id")
	if !ok {
		return
	}
	var req models.VersionRequest
	if err := commonapi.BindOptionalJSONStrict(c, &req); err != nil {
		respondError(c, http.StatusBadRequest, err)
		return
	}
	if req.Version <= 0 {
		respondError(c, http.StatusBadRequest, service.ErrInvalidStandardCollection)
		return
	}
	if err := h.svc.Delete(id, getTenantID(c), getUserID(c), req.Version); err != nil {
		respondError(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": commoni18n.T(c, sysi18n.MsgDeleteSuccess)})
}

// ListRevisions godoc
// @Summary 查询标准集修订 | List standard collection revisions
// @Tags Standard Collections
// @Produce json
// @Param id path int true "标准集 ID | Collection ID"
// @Success 200 {array} models.StandardCollectionRevision
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.collection.read"]
// @Router /collections/{id}/revisions [get]
// @Security BearerAuth
func (h *StandardCollectionHandler) ListRevisions(c *gin.Context) {
	id, ok := parseCollectionID(c, "id")
	if !ok {
		return
	}
	result, err := h.svc.ListRevisions(c.Request.Context(), id, getTenantID(c))
	if err != nil {
		respondError(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// ListEvents godoc
// @Summary 查询标准集审核事件 | List standard collection governance events
// @Tags Standard Collections
// @Produce json
// @Param id path int true "标准集 ID | Collection ID"
// @Param page query int false "页码 | Page"
// @Param page_size query int false "每页数量 | Page size"
// @Success 200 {object} object{data=[]models.StandardCollectionEvent,total=int64,page=int,page_size=int,total_pages=int}
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.collection.read"]
// @Router /collections/{id}/events [get]
// @Security BearerAuth
func (h *StandardCollectionHandler) ListEvents(c *gin.Context) {
	id, ok := parseCollectionID(c, "id")
	if !ok {
		return
	}
	page, err1 := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, err2 := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if err1 != nil || err2 != nil || page < 1 || pageSize < 1 || pageSize > 100 {
		respondError(c, http.StatusBadRequest, service.ErrInvalidStandardCollection)
		return
	}
	result, total, err := h.svc.ListEvents(c.Request.Context(), id, getTenantID(c), page, pageSize)
	if err != nil {
		respondError(c, http.StatusInternalServerError, err)
		return
	}
	commonapi.RespondPaginated(c, result, total, page, pageSize)
}

// CreateRevision godoc
// @Summary 创建标准集草稿修订 | Create a standard collection draft revision
// @Tags Standard Collections
// @Accept json
// @Produce json
// @Param id path int true "标准集 ID | Collection ID"
// @Param request body models.CreateStandardCollectionRevisionRequest true "新修订 | New revision"
// @Success 201 {object} models.StandardCollectionAggregate
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.collection.update"]
// @Router /collections/{id}/revisions [post]
// @Security BearerAuth
func (h *StandardCollectionHandler) CreateRevision(c *gin.Context) {
	id, ok := parseCollectionID(c, "id")
	if !ok {
		return
	}
	var req models.CreateStandardCollectionRevisionRequest
	if err := commonapi.BindOptionalJSONStrict(c, &req); err != nil {
		respondError(c, http.StatusBadRequest, err)
		return
	}
	if req.Version <= 0 {
		respondError(c, http.StatusBadRequest, service.ErrInvalidStandardCollection)
		return
	}
	result, err := h.svc.CreateRevision(c.Request.Context(), id, getTenantID(c), getUserID(c), &req)
	if err != nil {
		respondError(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusCreated, result)
}

// UpdateRevision godoc
// @Summary 更新标准集草稿 | Update a standard collection draft
// @Tags Standard Collections
// @Accept json
// @Produce json
// @Param id path int true "标准集 ID | Collection ID"
// @Param revision_id path int true "修订 ID | Revision ID"
// @Param request body models.UpdateStandardCollectionRevisionRequest true "完整草稿 | Complete draft"
// @Success 200 {object} models.StandardCollectionAggregate
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.collection.update"]
// @Router /collections/{id}/revisions/{revision_id} [put]
// @Security BearerAuth
func (h *StandardCollectionHandler) UpdateRevision(c *gin.Context) {
	id, ok := parseCollectionID(c, "id")
	if !ok {
		return
	}
	revisionID, ok := parseCollectionID(c, "revision_id")
	if !ok {
		return
	}
	var req models.UpdateStandardCollectionRevisionRequest
	if err := commonapi.BindOptionalJSONStrict(c, &req); err != nil {
		respondError(c, http.StatusBadRequest, err)
		return
	}
	if req.Version <= 0 {
		respondError(c, http.StatusBadRequest, service.ErrInvalidStandardCollection)
		return
	}
	result, err := h.svc.UpdateRevision(c.Request.Context(), id, revisionID, getTenantID(c), getUserID(c), &req)
	if err != nil {
		respondError(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// Submit godoc
// @Summary 提交标准集修订审核 | Submit a standard collection revision
// @Tags Standard Collections
// @Accept json
// @Produce json
// @Param id path int true "标准集 ID | Collection ID"
// @Param revision_id path int true "修订 ID | Revision ID"
// @Param request body models.RevisionActionRequest true "当前资源版本 | Current resource version"
// @Success 200 {object} models.StandardCollectionAggregate
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.collection.update"]
// @Router /collections/{id}/revisions/{revision_id}/submit [post]
// @Security BearerAuth
func (h *StandardCollectionHandler) Submit(c *gin.Context) { h.action(c, h.svc.Submit) }

// Return godoc
// @Summary 退回标准集修订 | Return a standard collection revision
// @Tags Standard Collections
// @Accept json
// @Produce json
// @Param id path int true "标准集 ID | Collection ID"
// @Param revision_id path int true "修订 ID | Revision ID"
// @Param request body models.RevisionActionRequest true "当前资源版本 | Current resource version"
// @Success 200 {object} models.StandardCollectionAggregate
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.collection.publish"]
// @Router /collections/{id}/revisions/{revision_id}/return [post]
// @Security BearerAuth
func (h *StandardCollectionHandler) Return(c *gin.Context) { h.action(c, h.svc.Return) }

// Publish godoc
// @Summary 发布标准集修订 | Publish a standard collection revision
// @Tags Standard Collections
// @Accept json
// @Produce json
// @Param id path int true "标准集 ID | Collection ID"
// @Param revision_id path int true "修订 ID | Revision ID"
// @Param request body models.RevisionActionRequest true "当前资源版本 | Current resource version"
// @Success 200 {object} models.StandardCollectionAggregate
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.collection.publish"]
// @Router /collections/{id}/revisions/{revision_id}/publish [post]
// @Security BearerAuth
func (h *StandardCollectionHandler) Publish(c *gin.Context) { h.action(c, h.svc.Publish) }

type collectionAction func(context.Context, int64, int64, int64, int64, int64) (*models.StandardCollectionAggregate, error)

func (h *StandardCollectionHandler) action(c *gin.Context, action collectionAction) {
	id, ok := parseCollectionID(c, "id")
	if !ok {
		return
	}
	revisionID, ok := parseCollectionID(c, "revision_id")
	if !ok {
		return
	}
	var req models.RevisionActionRequest
	if err := commonapi.BindOptionalJSONStrict(c, &req); err != nil {
		respondError(c, http.StatusBadRequest, err)
		return
	}
	if req.Version <= 0 {
		respondError(c, http.StatusBadRequest, service.ErrInvalidStandardCollection)
		return
	}
	result, err := action(c.Request.Context(), id, revisionID, getTenantID(c), getUserID(c), req.Version)
	if err != nil {
		respondError(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// ReplaceAssignments godoc
// @Summary 替换标准集职责分配 | Replace standard collection assignments
// @Tags Standard Collections
// @Accept json
// @Produce json
// @Param id path int true "标准集 ID | Collection ID"
// @Param request body models.ReplaceStandardCollectionAssignmentsRequest true "完整职责分配 | Complete assignments"
// @Success 200 {object} models.StandardCollectionAggregate
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.collection_assignment.update"]
// @Router /collections/{id}/assignments [put]
// @Security BearerAuth
func (h *StandardCollectionHandler) ReplaceAssignments(c *gin.Context) {
	id, ok := parseCollectionID(c, "id")
	if !ok {
		return
	}
	var req models.ReplaceStandardCollectionAssignmentsRequest
	if err := commonapi.BindOptionalJSONStrict(c, &req); err != nil {
		respondError(c, http.StatusBadRequest, err)
		return
	}
	if req.Version <= 0 {
		respondError(c, http.StatusBadRequest, service.ErrInvalidStandardCollection)
		return
	}
	result, err := h.svc.ReplaceAssignments(c.Request.Context(), id, getTenantID(c), getUserID(c), &req)
	if err != nil {
		respondError(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// ListUserCandidates godoc
// @Summary 查询标准集职责用户候选 | List collection assignment user candidates
// @Tags Standard Collections
// @Produce json
// @Param search query string false "名称或用户名 | Name or username"
// @Param page query int false "页码 | Page"
// @Param page_size query int false "每页数量 | Page size"
// @Success 200 {object} object{data=[]object,total=int64,page=int,page_size=int,total_pages=int}
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.collection_assignment.update"]
// @Router /collection-user-candidates [get]
// @Security BearerAuth
func (h *StandardCollectionHandler) ListUserCandidates(c *gin.Context) {
	page, err1 := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, err2 := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if err1 != nil || err2 != nil || page < 1 || pageSize < 1 || pageSize > 50 {
		respondError(c, http.StatusBadRequest, service.ErrInvalidStandardCollection)
		return
	}
	result, err := h.svc.ListUserCandidates(c.Request.Context(), getTenantID(c), c.Query("search"), page, pageSize)
	if err != nil {
		respondError(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

func parseCollectionID(c *gin.Context, name string) (int64, bool) {
	id, err := strconv.ParseInt(c.Param(name), 10, 64)
	if err != nil || id <= 0 {
		respondError(c, http.StatusBadRequest, service.ErrInvalidStandardCollection)
		return 0, false
	}
	return id, true
}
