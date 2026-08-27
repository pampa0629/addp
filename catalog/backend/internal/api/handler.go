package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	catalogi18n "github.com/addp/catalog/i18n"
	catalogauthorization "github.com/addp/catalog/internal/authorization"
	"github.com/addp/catalog/internal/service"
	commonapi "github.com/addp/common/api"
	commonAuth "github.com/addp/common/middleware/auth"
	commoni18n "github.com/addp/common/middleware/i18n"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type Handler struct {
	entries         *service.EntryService
	governanceTasks *service.GovernanceTaskService
	personal        *service.PersonalCatalogService
	collections     *service.CollectionService
	sync            *service.SourceSyncRunner
}

type updateDomainLinkRequest struct {
	ID   string `json:"id"`
	Role string `json:"role" enums:"primary,secondary"`
}

type updateResponsibilityRequest struct {
	Role        string `json:"role" enums:"accountable_department,business_owner,data_steward,technical_owner"`
	SubjectType string `json:"subject_type" enums:"department,user"`
	SubjectID   string `json:"subject_id"`
}

type updateComponentElementRequest struct {
	ComponentID string `json:"component_id"`
	ElementID   string `json:"element_id"`
}

type updateEntryRequest struct {
	Version                     int64                           `json:"version" minimum:"1"`
	BusinessName                *string                         `json:"business_name"`
	BusinessDescription         *string                         `json:"business_description"`
	GovernanceStatus            string                          `json:"governance_status" enums:"discovered,curated,certified,deprecated"`
	Visibility                  string                          `json:"visibility" enums:"inventory,department,tenant"`
	Domains                     []updateDomainLinkRequest       `json:"domains"`
	GlossaryIDs                 []string                        `json:"glossary_ids"`
	Responsibilities            []updateResponsibilityRequest   `json:"responsibilities"`
	ComponentElements           []updateComponentElementRequest `json:"component_elements"`
	DeprecationReason           *string                         `json:"deprecation_reason"`
	RecommendedSuccessorEntryID *string                         `json:"recommended_successor_entry_id" format:"uuid"`
}

type rebindSourceRequest struct {
	TargetVersion         int64  `json:"target_version" minimum:"1"`
	TemporaryEntryID      string `json:"temporary_entry_id"`
	TemporaryEntryVersion int64  `json:"temporary_entry_version" minimum:"1"`
	NewSourceIdentity     string `json:"new_source_identity"`
	Reason                string `json:"reason"`
	Evidence              string `json:"evidence"`
}

type resolveReferencesRequest struct {
	IDs []string `json:"ids"`
}

type resolveReferencesResponse struct {
	Results []service.CatalogReferenceResolution `json:"results"`
}

type replaceEntryMarksRequest struct {
	Favorite  bool `json:"favorite"`
	Following bool `json:"following"`
}

type createCollectionRequest struct {
	ProjectGroupID string   `json:"project_group_id"`
	Name           string   `json:"name"`
	Description    string   `json:"description"`
	EntryIDs       []string `json:"entry_ids"`
}

type updateCollectionRequest struct {
	Version     int64    `json:"version" minimum:"1"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	EntryIDs    []string `json:"entry_ids"`
}

type deleteCollectionRequest struct {
	Version int64 `json:"version" minimum:"1"`
}

func NewHandler(entries *service.EntryService, governanceTasks *service.GovernanceTaskService, personal *service.PersonalCatalogService, collections *service.CollectionService, syncRunner *service.SourceSyncRunner) *Handler {
	return &Handler{entries: entries, governanceTasks: governanceTasks, personal: personal, collections: collections, sync: syncRunner}
}

// ListCollections 列出当前项目组目录集合。
// @Summary 查询项目组目录集合 | List project group catalog collections
// @Tags Catalog Collections
// @Produce json
// @Param project_group_id query string false "Project Group 稳定 ID | Project Group stable ID"
// @Param page query int false "页码，默认 1 | Page number, default 1"
// @Param page_size query int false "每页数量，默认 20，最大 200 | Page size, default 20 and maximum 200"
// @Success 200 {object} service.CollectionListResult "目录集合分页结果 | Paginated catalog collections"
// @Failure 400 {object} map[string]interface{} "请求参数无效 | Invalid request"
// @Failure 401 {object} map[string]interface{} "未认证 | Unauthorized"
// @Failure 403 {object} map[string]interface{} "权限不足 | Forbidden"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["catalog.entry.read","catalog.collection.read"]
// @Router /collections [get]
// @Security BearerAuth
func (h *Handler) ListCollections(c *gin.Context) {
	tenantID, userID, ok := currentUserContext(c)
	page, pageErr := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, pageSizeErr := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	projectGroupID, groupErr := parseOptionalCanonicalPositiveInt64(c.Query("project_group_id"))
	if !ok || h.collections == nil {
		respondError(c, http.StatusForbidden, service.ErrUserPrincipalRequired)
		return
	}
	if pageErr != nil || pageSizeErr != nil || groupErr != nil {
		respondError(c, http.StatusBadRequest, service.ErrInvalidCollection)
		return
	}
	access := collectionAccess(c, userID)
	result, err := h.collections.List(c.Request.Context(), tenantID, access, service.CollectionListFilter{
		ProjectGroupID: projectGroupID, Page: page, PageSize: pageSize,
	})
	if err != nil {
		respondError(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// CreateCollection 创建项目组目录集合。
// @Summary 创建项目组目录集合 | Create a project group catalog collection
// @Tags Catalog Collections
// @Accept json
// @Produce json
// @Param request body createCollectionRequest true "集合完整内容 | Complete collection content"
// @Success 201 {object} service.CollectionDetail "目录集合 | Catalog collection"
// @Failure 400 {object} map[string]interface{} "请求参数无效 | Invalid request"
// @Failure 401 {object} map[string]interface{} "未认证 | Unauthorized"
// @Failure 403 {object} map[string]interface{} "权限不足 | Forbidden"
// @Failure 409 {object} map[string]interface{} "集合名称冲突 | Collection name conflict"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["catalog.entry.read","catalog.collection.read","catalog.collection.update"]
// @Router /collections [post]
// @Security BearerAuth
func (h *Handler) CreateCollection(c *gin.Context) {
	tenantID, userID, ok := currentUserContext(c)
	var request createCollectionRequest
	if !ok || h.collections == nil {
		respondError(c, http.StatusForbidden, service.ErrUserPrincipalRequired)
		return
	}
	if c.ShouldBindJSON(&request) != nil {
		respondError(c, http.StatusBadRequest, service.ErrInvalidCollection)
		return
	}
	projectGroupID, err := parseCanonicalPositiveInt64(request.ProjectGroupID)
	entryIDs, idsErr := parseCanonicalUUIDs(request.EntryIDs)
	if err != nil || idsErr != nil {
		respondError(c, http.StatusBadRequest, service.ErrInvalidCollection)
		return
	}
	result, err := h.collections.Create(c.Request.Context(), tenantID, collectionAccess(c, userID), service.CollectionInput{
		ProjectGroupID: projectGroupID, Name: request.Name, Description: request.Description, EntryIDs: entryIDs,
	})
	if err != nil {
		respondError(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusCreated, result)
}

// GetCollection 读取项目组目录集合。
// @Summary 读取项目组目录集合 | Get a project group catalog collection
// @Tags Catalog Collections
// @Produce json
// @Param id path string true "Collection UUID"
// @Success 200 {object} service.CollectionDetail "目录集合与当前可见条目 | Collection and currently visible entries"
// @Failure 401 {object} map[string]interface{} "未认证 | Unauthorized"
// @Failure 403 {object} map[string]interface{} "权限不足 | Forbidden"
// @Failure 404 {object} map[string]interface{} "集合不存在或不可访问 | Collection not found or inaccessible"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["catalog.entry.read","catalog.collection.read"]
// @Router /collections/{id} [get]
// @Security BearerAuth
func (h *Handler) GetCollection(c *gin.Context) {
	tenantID, userID, ok := currentUserContext(c)
	id, err := parseCanonicalUUID(c.Param("id"))
	if !ok || h.collections == nil {
		respondError(c, http.StatusForbidden, service.ErrUserPrincipalRequired)
		return
	}
	if err != nil {
		respondError(c, http.StatusBadRequest, service.ErrInvalidCollection)
		return
	}
	result, err := h.collections.Get(c.Request.Context(), tenantID, collectionAccess(c, userID), id)
	if err != nil {
		respondError(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// UpdateCollection 原子更新项目组目录集合。
// @Summary 更新项目组目录集合 | Replace a project group catalog collection
// @Tags Catalog Collections
// @Accept json
// @Produce json
// @Param id path string true "Collection UUID"
// @Param request body updateCollectionRequest true "集合版本与完整内容 | Collection version and complete content"
// @Success 200 {object} service.CollectionDetail "更新后的目录集合 | Updated catalog collection"
// @Failure 400 {object} map[string]interface{} "请求参数无效 | Invalid request"
// @Failure 401 {object} map[string]interface{} "未认证 | Unauthorized"
// @Failure 403 {object} map[string]interface{} "权限不足 | Forbidden"
// @Failure 404 {object} map[string]interface{} "集合不存在或不可访问 | Collection not found or inaccessible"
// @Failure 409 {object} map[string]interface{} "版本或名称冲突 | Version or name conflict"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["catalog.entry.read","catalog.collection.read","catalog.collection.update"]
// @Router /collections/{id} [put]
// @Security BearerAuth
func (h *Handler) UpdateCollection(c *gin.Context) {
	tenantID, userID, ok := currentUserContext(c)
	id, idErr := parseCanonicalUUID(c.Param("id"))
	var request updateCollectionRequest
	if !ok || h.collections == nil {
		respondError(c, http.StatusForbidden, service.ErrUserPrincipalRequired)
		return
	}
	if idErr != nil || c.ShouldBindJSON(&request) != nil {
		respondError(c, http.StatusBadRequest, service.ErrInvalidCollection)
		return
	}
	entryIDs, err := parseCanonicalUUIDs(request.EntryIDs)
	if err != nil {
		respondError(c, http.StatusBadRequest, service.ErrInvalidCollection)
		return
	}
	result, err := h.collections.Update(c.Request.Context(), tenantID, collectionAccess(c, userID), id, service.CollectionInput{
		Version: request.Version, Name: request.Name, Description: request.Description, EntryIDs: entryIDs,
	})
	if err != nil {
		respondError(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// DeleteCollection 删除项目组目录集合。
// @Summary 删除项目组目录集合 | Delete a project group catalog collection
// @Tags Catalog Collections
// @Accept json
// @Param id path string true "Collection UUID"
// @Param request body deleteCollectionRequest true "当前集合版本 | Current collection version"
// @Success 204 "删除成功 | Deleted"
// @Failure 400 {object} map[string]interface{} "请求参数无效 | Invalid request"
// @Failure 401 {object} map[string]interface{} "未认证 | Unauthorized"
// @Failure 403 {object} map[string]interface{} "权限不足 | Forbidden"
// @Failure 404 {object} map[string]interface{} "集合不存在或不可访问 | Collection not found or inaccessible"
// @Failure 409 {object} map[string]interface{} "版本冲突 | Version conflict"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["catalog.entry.read","catalog.collection.read","catalog.collection.update"]
// @Router /collections/{id} [delete]
// @Security BearerAuth
func (h *Handler) DeleteCollection(c *gin.Context) {
	tenantID, userID, ok := currentUserContext(c)
	id, idErr := parseCanonicalUUID(c.Param("id"))
	var request deleteCollectionRequest
	if !ok || h.collections == nil {
		respondError(c, http.StatusForbidden, service.ErrUserPrincipalRequired)
		return
	}
	if idErr != nil || c.ShouldBindJSON(&request) != nil {
		respondError(c, http.StatusBadRequest, service.ErrInvalidCollection)
		return
	}
	if err := h.collections.Delete(c.Request.Context(), tenantID, collectionAccess(c, userID), id, request.Version); err != nil {
		respondError(c, http.StatusInternalServerError, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// ListMyEntries 列出当前 User 的个人目录关系视图。
// @Summary 查询我的目录 | List my catalog entries
// @Description 按当前 User 的责任、收藏或关注关系返回仍然可见的目录条目 | Return still-visible entries related to the current User by responsibility, favorite, or following
// @Tags Catalog Personal
// @Produce json
// @Param relation query string true "个人关系 | Personal relation" Enums(responsible,favorite,following)
// @Param page query int false "页码，默认 1 | Page number, default 1"
// @Param page_size query int false "每页数量，默认 20，最大 200 | Page size, default 20 and maximum 200"
// @Success 200 {object} service.EntryListResult "我的目录分页结果 | Paginated personal catalog entries"
// @Failure 400 {object} map[string]interface{} "请求参数无效 | Invalid request"
// @Failure 401 {object} map[string]interface{} "未认证 | Unauthorized"
// @Failure 403 {object} map[string]interface{} "当前主体不是 User 或权限不足 | User principal or permission required"
// @Failure 500 {object} map[string]interface{} "服务器内部错误 | Internal server error"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["catalog.entry.read"]
// @Router /me/entries [get]
// @Security BearerAuth
func (h *Handler) ListMyEntries(c *gin.Context) {
	tenantID, userID, ok := currentUserContext(c)
	if !ok || h.personal == nil {
		respondError(c, http.StatusForbidden, service.ErrUserPrincipalRequired)
		return
	}
	page, pageErr := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, pageSizeErr := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if pageErr != nil || pageSizeErr != nil {
		respondError(c, http.StatusBadRequest, service.ErrInvalidPersonalRelation)
		return
	}
	result, err := h.personal.List(c.Request.Context(), tenantID, userID, entryAccess(c), strings.TrimSpace(c.Query("relation")), page, pageSize)
	if err != nil {
		respondError(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// GetMyEntryMarks 读取当前 User 的目录条目标记。
// @Summary 读取我的条目标记 | Get my entry marks
// @Tags Catalog Personal
// @Produce json
// @Param id path string true "CatalogEntry UUID"
// @Success 200 {object} service.EntryMarks "收藏与关注状态 | Favorite and following state"
// @Failure 401 {object} map[string]interface{} "未认证 | Unauthorized"
// @Failure 403 {object} map[string]interface{} "当前主体不是 User 或权限不足 | User principal or permission required"
// @Failure 404 {object} map[string]interface{} "目录条目不存在或不可见 | Catalog entry not found or not visible"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["catalog.entry.read"]
// @Router /me/entries/{id}/marks [get]
// @Security BearerAuth
func (h *Handler) GetMyEntryMarks(c *gin.Context) {
	tenantID, userID, ok := currentUserContext(c)
	entryID, err := parseCanonicalUUID(c.Param("id"))
	if !ok || h.personal == nil {
		respondError(c, http.StatusForbidden, service.ErrUserPrincipalRequired)
		return
	}
	if err != nil {
		respondError(c, http.StatusBadRequest, service.ErrInvalidEntryUpdate)
		return
	}
	result, err := h.personal.GetMarks(c.Request.Context(), tenantID, userID, entryAccess(c), entryID)
	if err != nil {
		respondError(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// ReplaceMyEntryMarks 原子替换当前 User 的目录条目标记。
// @Summary 更新我的条目标记 | Replace my entry marks
// @Tags Catalog Personal
// @Accept json
// @Produce json
// @Param id path string true "CatalogEntry UUID"
// @Param request body replaceEntryMarksRequest true "完整收藏与关注状态 | Complete favorite and following state"
// @Success 200 {object} service.EntryMarks "更新后的标记 | Updated marks"
// @Failure 400 {object} map[string]interface{} "请求参数无效 | Invalid request"
// @Failure 401 {object} map[string]interface{} "未认证 | Unauthorized"
// @Failure 403 {object} map[string]interface{} "当前主体不是 User 或权限不足 | User principal or permission required"
// @Failure 404 {object} map[string]interface{} "目录条目不存在或不可见 | Catalog entry not found or not visible"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["catalog.entry.read"]
// @Router /me/entries/{id}/marks [put]
// @Security BearerAuth
func (h *Handler) ReplaceMyEntryMarks(c *gin.Context) {
	tenantID, userID, ok := currentUserContext(c)
	entryID, err := parseCanonicalUUID(c.Param("id"))
	if !ok || h.personal == nil {
		respondError(c, http.StatusForbidden, service.ErrUserPrincipalRequired)
		return
	}
	var request replaceEntryMarksRequest
	if err != nil || c.ShouldBindJSON(&request) != nil {
		respondError(c, http.StatusBadRequest, service.ErrInvalidEntryUpdate)
		return
	}
	result, err := h.personal.ReplaceMarks(c.Request.Context(), tenantID, userID, entryAccess(c), entryID, service.EntryMarks{
		Favorite: request.Favorite, Following: request.Following,
	})
	if err != nil {
		respondError(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// ListGovernanceTasks 列出责任失效治理队列。
// @Summary 查询责任治理队列 | List responsibility governance tasks
// @Description 返回由 System 责任引用失效对账产生的任务；责任修复仍通过 CatalogEntry 完整聚合更新完成 | Return tasks derived from invalid System responsibility references; repair still uses the full CatalogEntry aggregate update
// @Tags Catalog Governance
// @Produce json
// @Param status query string false "任务状态，默认 open | Task status, open by default" Enums(open,resolved)
// @Param entry_id query string false "CatalogEntry UUID"
// @Param page query int false "页码，默认 1 | Page number, default 1"
// @Param page_size query int false "每页数量，默认 20，最大 200 | Page size, default 20 and maximum 200"
// @Success 200 {object} service.GovernanceTaskListResult "治理任务分页结果 | Paginated governance tasks"
// @Failure 400 {object} map[string]interface{} "请求参数无效 | Invalid request"
// @Failure 401 {object} map[string]interface{} "未认证 | Unauthorized"
// @Failure 403 {object} map[string]interface{} "权限不足 | Forbidden"
// @Failure 500 {object} map[string]interface{} "服务器内部错误 | Internal server error"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["catalog.entry.update"]
// @Router /governance/tasks [get]
// @Security BearerAuth
func (h *Handler) ListGovernanceTasks(c *gin.Context) {
	tenantID, ok := commonAuth.TenantIDFromGin(c)
	if !ok || h.governanceTasks == nil {
		respondError(c, http.StatusBadRequest, service.ErrInvalidPage)
		return
	}
	h.sync.ObserveTenant(tenantID)
	page, pageErr := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, pageSizeErr := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	status := strings.TrimSpace(c.DefaultQuery("status", "open"))
	entryID := uuid.Nil
	if rawEntryID := strings.TrimSpace(c.Query("entry_id")); rawEntryID != "" {
		parsed, err := uuid.Parse(rawEntryID)
		if err != nil || parsed.String() != rawEntryID {
			respondError(c, http.StatusBadRequest, service.ErrInvalidPage)
			return
		}
		entryID = parsed
	}
	if pageErr != nil || pageSizeErr != nil {
		respondError(c, http.StatusBadRequest, service.ErrInvalidPage)
		return
	}
	result, err := h.governanceTasks.List(c.Request.Context(), tenantID, service.GovernanceTaskListFilter{
		Status: status, EntryID: entryID, Page: page, PageSize: pageSize,
	})
	if err != nil {
		if errors.Is(err, service.ErrInvalidPage) {
			respondError(c, http.StatusBadRequest, err)
			return
		}
		respondError(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// ListEntries 列出当前调用方可发现的企业目录条目。
// @Summary 列出企业数据目录 | List enterprise catalog entries
// @Description 按目录可见性返回当前租户的企业目录条目；目录可见不代表底层数据内容授权 | Return enterprise catalog entries visible to the caller; catalog visibility does not grant data content access
// @Tags Catalog
// @Produce json
// @Param view query string false "目录视图，默认 governance | Catalog view, governance by default" Enums(governance,inventory)
// @Param search query string false "名称搜索 | Name search"
// @Param entry_type query string false "目录对象类型 | Catalog entry type" Enums(data_item,business_entity,logical_model,metric,data_service,development_artifact)
// @Param source_status query string false "来源状态 | Source status" Enums(active,missing)
// @Param source_identity query string false "来源 owner 的精确稳定身份；第一阶段为 Meta fingerprint | Exact stable owner identity; Meta fingerprint in the first release"
// @Param governance_status query string false "治理状态 | Governance status" Enums(discovered,curated,certified,deprecated)
// @Param visibility query string false "目录可见性 | Catalog visibility" Enums(inventory,department,tenant)
// @Param primary_domain_id query string false "主业务域稳定 ID | Primary Domain stable ID"
// @Param accountable_department_id query string false "责任部门稳定 ID | Accountable Department stable ID"
// @Param source_engine_id query string false "来源引擎稳定 ID | Source engine stable ID"
// @Param page query int false "页码，默认 1 | Page number, default 1"
// @Param page_size query int false "每页数量，默认 20，最大 200 | Page size, default 20 and maximum 200"
// @Success 200 {object} service.EntryListResult "企业目录分页结果 | Paginated catalog entries"
// @Failure 400 {object} map[string]interface{} "请求参数无效 | Invalid request"
// @Failure 401 {object} map[string]interface{} "未认证 | Unauthorized"
// @Failure 403 {object} map[string]interface{} "权限不足 | Forbidden"
// @Failure 500 {object} map[string]interface{} "服务器内部错误 | Internal server error"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["catalog.entry.read"]
// @x-addp-conditional-permissions ["catalog.inventory.read"]
// @Router /entries [get]
// @Security BearerAuth
func (h *Handler) ListEntries(c *gin.Context) {
	tenantID, ok := commonAuth.TenantIDFromGin(c)
	if !ok {
		respondError(c, http.StatusBadRequest, service.ErrInvalidPage)
		return
	}
	h.sync.ObserveTenant(tenantID)
	page, pageErr := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, pageSizeErr := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if pageErr != nil || pageSizeErr != nil {
		respondError(c, http.StatusBadRequest, service.ErrInvalidPage)
		return
	}
	primaryDomainID, domainErr := parseOptionalCanonicalPositiveInt64(c.Query("primary_domain_id"))
	departmentID, departmentErr := parseOptionalCanonicalPositiveInt64(c.Query("accountable_department_id"))
	sourceEngineID, engineErr := parseOptionalCanonicalPositiveInt64(c.Query("source_engine_id"))
	if domainErr != nil || departmentErr != nil || engineErr != nil {
		respondError(c, http.StatusBadRequest, service.ErrInvalidPage)
		return
	}
	result, err := h.entries.List(c.Request.Context(), tenantID, entryAccess(c), service.EntryListFilter{
		View:   c.Query("view"),
		Search: c.Query("search"), EntryType: c.Query("entry_type"), SourceStatus: c.Query("source_status"), SourceIdentity: strings.TrimSpace(c.Query("source_identity")),
		GovernanceStatus: c.Query("governance_status"), Visibility: c.Query("visibility"),
		PrimaryDomainID: primaryDomainID, DepartmentID: departmentID, SourceEngineID: sourceEngineID,
		Page: page, PageSize: pageSize,
	})
	if err != nil {
		respondError(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// ListEntryFacets 列出当前目录视图中实际出现的人类可读引用分面。
// @Summary 列出企业目录引用分面 | List enterprise catalog reference facets
// @Description Catalog 计算当前调用方可见条目中的引用集，Standard / System 动态解析显示信息；分面解析不影响目录列表和 Ready | Catalog computes references from entries visible to the caller while Standard and System dynamically resolve display facts; facet resolution does not affect entry listing or readiness
// @Tags Catalog
// @Produce json
// @Param view query string false "目录视图，默认 governance | Catalog view, governance by default" Enums(governance,inventory)
// @Success 200 {object} service.EntryFacets "权限感知引用分面 | Permission-aware reference facets"
// @Failure 400 {object} map[string]interface{} "请求参数无效 | Invalid request"
// @Failure 401 {object} map[string]interface{} "未认证 | Unauthorized"
// @Failure 403 {object} map[string]interface{} "缺少盘点权限 | Inventory permission required"
// @Failure 500 {object} map[string]interface{} "服务器内部错误 | Internal server error"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["catalog.entry.read"]
// @x-addp-conditional-permissions ["catalog.inventory.read"]
// @Router /entries/facets [get]
// @Security BearerAuth
func (h *Handler) ListEntryFacets(c *gin.Context) {
	tenantID, ok := commonAuth.TenantIDFromGin(c)
	if !ok {
		respondError(c, http.StatusBadRequest, service.ErrInvalidPage)
		return
	}
	h.sync.ObserveTenant(tenantID)
	result, err := h.entries.ListFacets(c.Request.Context(), tenantID, entryAccess(c), c.Query("view"))
	if err != nil {
		respondError(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// GetEntry 获取企业目录聚合详情。
// @Summary 获取企业目录详情 | Get enterprise catalog entry
// @Description 返回目录条目、当前来源绑定和组件；目录可见不代表底层数据内容授权 | Return the catalog entry, current source binding, and components; catalog visibility does not grant data content access
// @Tags Catalog
// @Produce json
// @Param id path string true "CatalogEntry UUID"
// @Success 200 {object} service.EntryDetail "企业目录详情 | Catalog entry detail"
// @Failure 400 {object} map[string]interface{} "无效 ID | Invalid ID"
// @Failure 401 {object} map[string]interface{} "未认证 | Unauthorized"
// @Failure 403 {object} map[string]interface{} "权限不足 | Forbidden"
// @Failure 404 {object} map[string]interface{} "条目不存在或不可见 | Entry not found or not visible"
// @Failure 500 {object} map[string]interface{} "服务器内部错误 | Internal server error"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["catalog.entry.read"]
// @x-addp-conditional-permissions ["catalog.inventory.read"]
// @Router /entries/{id} [get]
// @Security BearerAuth
func (h *Handler) GetEntry(c *gin.Context) {
	tenantID, ok := commonAuth.TenantIDFromGin(c)
	if !ok {
		respondError(c, http.StatusBadRequest, service.ErrInvalidPage)
		return
	}
	id, err := uuid.Parse(strings.TrimSpace(c.Param("id")))
	if err != nil {
		respondError(c, http.StatusBadRequest, err)
		return
	}
	h.sync.ObserveTenant(tenantID)
	entry, err := h.entries.Get(c.Request.Context(), tenantID, entryAccess(c), id)
	if err != nil {
		respondError(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, entry)
}

// UpdateEntry 原子更新企业目录的完整可编辑聚合。
// @Summary 编目企业目录条目 | Curate an enterprise catalog entry
// @Description 使用聚合根 version 原子替换业务信息、语义关联、责任、组件数据元关联、可见性、治理状态和可选推荐继任项；推荐继任只允许指向同租户、来源有效且已编目或已认证的 active 条目；Standard 或 System 校验不可达时明确失败但不影响模块 Ready | Atomically replace business metadata, semantic links, responsibilities, component-element links, visibility, governance status, and the optional recommended successor using the aggregate version; a successor must be an active-source curated or certified entry in the same tenant; unavailable Standard or System validation fails explicitly without affecting module readiness
// @Tags Catalog
// @Accept json
// @Produce json
// @Param id path string true "CatalogEntry UUID"
// @Param request body updateEntryRequest true "完整可编辑聚合；所有跨模块 BIGINT ID 使用规范十进制字符串 | Complete editable aggregate; all cross-module BIGINT IDs use canonical decimal strings"
// @Success 200 {object} service.EntryDetail "更新后的完整条目 | Updated complete entry"
// @Failure 400 {object} map[string]interface{} "请求或编目要求无效 | Invalid request or curation requirements"
// @Failure 401 {object} map[string]interface{} "未认证 | Unauthorized"
// @Failure 403 {object} map[string]interface{} "缺少更新或条件状态权限 | Missing update or conditional status permission"
// @Failure 404 {object} map[string]interface{} "条目不存在 | Entry not found"
// @Failure 409 {object} map[string]interface{} "版本、状态或引用冲突 | Version, state, or reference conflict"
// @Failure 503 {object} map[string]interface{} "Standard 或 System 引用校验不可达 | Standard or System reference validation unavailable"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["catalog.entry.update"]
// @x-addp-conditional-permissions ["catalog.entry.certify","catalog.entry.deprecate"]
// @Router /entries/{id} [put]
// @Security BearerAuth
func (h *Handler) UpdateEntry(c *gin.Context) {
	tenantID, ok := commonAuth.TenantIDFromGin(c)
	if !ok {
		respondError(c, http.StatusBadRequest, service.ErrInvalidEntryUpdate)
		return
	}
	id, err := uuid.Parse(strings.TrimSpace(c.Param("id")))
	if err != nil || id == uuid.Nil {
		respondError(c, http.StatusBadRequest, service.ErrInvalidEntryUpdate)
		return
	}
	var request updateEntryRequest
	if err := commonapi.BindOptionalJSONStrict(c, &request); err != nil {
		respondError(c, http.StatusBadRequest, service.ErrInvalidEntryUpdate)
		return
	}
	input, err := mapUpdateEntryRequest(request)
	if err != nil {
		respondError(c, http.StatusBadRequest, service.ErrInvalidEntryUpdate)
		return
	}
	authContext, ok := commonAuth.AuthContextFromGin(c)
	if !ok {
		respondError(c, http.StatusUnauthorized, service.ErrInvalidEntryUpdate)
		return
	}
	h.sync.ObserveTenant(tenantID)
	entry, err := h.entries.Update(
		c.Request.Context(), tenantID, id, input,
		service.UpdateEntryAuthorization{
			CanCertify:   commonAuth.HasRolePermission(c, catalogauthorization.PermissionCatalogEntryCertify),
			CanDeprecate: commonAuth.HasRolePermission(c, catalogauthorization.PermissionCatalogEntryDeprecate),
		},
		service.UpdateEntryActor{Type: authContext.Principal.Type, ID: authContext.Principal.ID},
	)
	if err != nil {
		respondError(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, entry)
}

// RebindSource 将新发现的 DataItem 来源显式重绑到既有企业目录身份。
// @Summary 重绑目录来源 | Rebind catalog source
// @Description 仅允许把 active + discovered + inventory 且无人工作业的临时条目合并到当前来源为 missing 的既有条目；请求必须携带两个聚合版本、原因和人工证据 | Only merge an active discovered inventory temporary entry without human work into an existing entry whose current source is missing; both aggregate versions, a reason, and human evidence are required
// @Tags Catalog
// @Accept json
// @Produce json
// @Param id path string true "目标 CatalogEntry UUID | Target CatalogEntry UUID"
// @Param request body rebindSourceRequest true "重绑请求 | Rebind request"
// @Success 200 {object} service.EntryDetail "重绑后的规范目录条目 | Canonical catalog entry after rebind"
// @Failure 400 {object} map[string]interface{} "请求无效 | Invalid request"
// @Failure 401 {object} map[string]interface{} "未认证 | Unauthorized"
// @Failure 403 {object} map[string]interface{} "权限不足 | Forbidden"
// @Failure 404 {object} map[string]interface{} "目录条目不存在 | Catalog entry not found"
// @Failure 409 {object} map[string]interface{} "版本或重绑前置条件冲突 | Version or rebind precondition conflict"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["catalog.source.rebind"]
// @Router /entries/{id}/rebind-source [post]
// @Security BearerAuth
func (h *Handler) RebindSource(c *gin.Context) {
	tenantID, ok := commonAuth.TenantIDFromGin(c)
	if !ok {
		respondError(c, http.StatusBadRequest, service.ErrInvalidSourceRebind)
		return
	}
	targetID, err := parseCanonicalUUID(c.Param("id"))
	if err != nil {
		respondError(c, http.StatusBadRequest, service.ErrInvalidSourceRebind)
		return
	}
	var request rebindSourceRequest
	if err := commonapi.BindOptionalJSONStrict(c, &request); err != nil {
		respondError(c, http.StatusBadRequest, service.ErrInvalidSourceRebind)
		return
	}
	temporaryID, err := parseCanonicalUUID(request.TemporaryEntryID)
	if err != nil {
		respondError(c, http.StatusBadRequest, service.ErrInvalidSourceRebind)
		return
	}
	authContext, ok := commonAuth.AuthContextFromGin(c)
	if !ok {
		respondError(c, http.StatusUnauthorized, service.ErrInvalidSourceRebind)
		return
	}
	h.sync.ObserveTenant(tenantID)
	entry, err := h.entries.RebindSource(c.Request.Context(), tenantID, targetID, service.RebindSourceInput{
		TargetVersion: request.TargetVersion, TemporaryEntryID: temporaryID,
		TemporaryEntryVersion: request.TemporaryEntryVersion, NewSourceIdentity: request.NewSourceIdentity,
		Reason: request.Reason, Evidence: request.Evidence,
	}, service.UpdateEntryActor{Type: authContext.Principal.Type, ID: authContext.Principal.ID})
	if err != nil {
		respondError(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, entry)
}

// GetEntryHistory 读取企业目录聚合的来源与治理审计历史。
// @Summary 获取目录历史 | Get catalog entry history
// @Description 返回当前调用方可发现条目的来源绑定历史与最近 200 条不可变领域审计 | Return source binding history and the latest 200 immutable domain audit events for an entry visible to the caller
// @Tags Catalog
// @Produce json
// @Param id path string true "CatalogEntry UUID"
// @Success 200 {object} service.EntryHistory "目录历史 | Catalog entry history"
// @Failure 400 {object} map[string]interface{} "无效 ID | Invalid ID"
// @Failure 401 {object} map[string]interface{} "未认证 | Unauthorized"
// @Failure 403 {object} map[string]interface{} "权限不足 | Forbidden"
// @Failure 404 {object} map[string]interface{} "条目不存在或不可见 | Entry not found or not visible"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["catalog.entry.read","catalog.audit.read"]
// @x-addp-conditional-permissions ["catalog.inventory.read"]
// @Router /entries/{id}/history [get]
// @Security BearerAuth
func (h *Handler) GetEntryHistory(c *gin.Context) {
	tenantID, ok := commonAuth.TenantIDFromGin(c)
	if !ok {
		respondError(c, http.StatusBadRequest, service.ErrInvalidPage)
		return
	}
	entryID, err := parseCanonicalUUID(c.Param("id"))
	if err != nil {
		respondError(c, http.StatusBadRequest, service.ErrInvalidPage)
		return
	}
	h.sync.ObserveTenant(tenantID)
	history, err := h.entries.History(c.Request.Context(), tenantID, entryAccess(c), entryID)
	if err != nil {
		respondError(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, history)
}

// ResolveReferences resolves exact CatalogEntry references for the Asset runtime.
// @Summary 解析资产目录引用 | Resolve asset catalog references
// @Description 按请求顺序返回当前租户 CatalogEntry 的可组合与可发布状态；只允许 addp-asset Service Client 调用 | Return selectable and publishable states for current-tenant CatalogEntry references in request order; restricted to the addp-asset service client
// @Tags Catalog Runtime
// @Accept json
// @Produce json
// @Param request body resolveReferencesRequest true "CatalogEntry UUID 列表，1 到 200 个 | CatalogEntry UUID list, 1 to 200 items"
// @Success 200 {object} resolveReferencesResponse
// @Failure 400 {object} map[string]interface{} "请求无效 | Invalid request"
// @Failure 401 {object} map[string]interface{} "未认证 | Unauthorized"
// @Failure 403 {object} map[string]interface{} "非 addp-asset 客户端或权限不足 | Wrong service client or insufficient permission"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["catalog.reference.read"]
// @Router /runtime/references/resolve [post]
// @Security BearerAuth
func (h *Handler) ResolveReferences(c *gin.Context) {
	tenantID, ok := commonAuth.TenantIDFromGin(c)
	if !ok {
		respondError(c, http.StatusBadRequest, service.ErrInvalidEntryUpdate)
		return
	}
	var request resolveReferencesRequest
	if err := commonapi.BindOptionalJSONStrict(c, &request); err != nil || len(request.IDs) == 0 || len(request.IDs) > 200 {
		respondError(c, http.StatusBadRequest, service.ErrInvalidEntryUpdate)
		return
	}
	ids := make([]uuid.UUID, 0, len(request.IDs))
	for _, value := range request.IDs {
		id, err := uuid.Parse(value)
		if err != nil || id == uuid.Nil || id.String() != value {
			respondError(c, http.StatusBadRequest, service.ErrInvalidEntryUpdate)
			return
		}
		ids = append(ids, id)
	}
	results, err := h.entries.ResolveReferences(c.Request.Context(), tenantID, ids)
	if err != nil {
		respondError(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, resolveReferencesResponse{Results: results})
}

func mapUpdateEntryRequest(request updateEntryRequest) (service.UpdateEntryInput, error) {
	input := service.UpdateEntryInput{
		Version: request.Version, BusinessName: request.BusinessName,
		BusinessDescription: request.BusinessDescription, GovernanceStatus: request.GovernanceStatus,
		Visibility: request.Visibility, DeprecationReason: request.DeprecationReason,
		Domains:           make([]service.DomainLinkInput, 0, len(request.Domains)),
		GlossaryIDs:       make([]int64, 0, len(request.GlossaryIDs)),
		Responsibilities:  make([]service.ResponsibilityInput, 0, len(request.Responsibilities)),
		ComponentElements: make([]service.ComponentElementInput, 0, len(request.ComponentElements)),
	}
	if request.RecommendedSuccessorEntryID != nil {
		successorID, err := uuid.Parse(*request.RecommendedSuccessorEntryID)
		if err != nil || successorID == uuid.Nil || successorID.String() != *request.RecommendedSuccessorEntryID {
			return input, service.ErrInvalidEntryUpdate
		}
		input.RecommendedSuccessorEntryID = &successorID
	}
	for _, domain := range request.Domains {
		id, err := parseCanonicalPositiveInt64(domain.ID)
		if err != nil {
			return input, err
		}
		input.Domains = append(input.Domains, service.DomainLinkInput{ID: id, Role: domain.Role})
	}
	for _, glossaryID := range request.GlossaryIDs {
		id, err := parseCanonicalPositiveInt64(glossaryID)
		if err != nil {
			return input, err
		}
		input.GlossaryIDs = append(input.GlossaryIDs, id)
	}
	for _, responsibility := range request.Responsibilities {
		id, err := parseCanonicalPositiveInt64(responsibility.SubjectID)
		if err != nil {
			return input, err
		}
		input.Responsibilities = append(input.Responsibilities, service.ResponsibilityInput{
			Role: responsibility.Role, SubjectType: responsibility.SubjectType, SubjectID: id,
		})
	}
	for _, component := range request.ComponentElements {
		componentID, err := uuid.Parse(component.ComponentID)
		if err != nil || componentID == uuid.Nil || componentID.String() != component.ComponentID {
			return input, service.ErrInvalidEntryUpdate
		}
		elementID, err := parseCanonicalPositiveInt64(component.ElementID)
		if err != nil {
			return input, err
		}
		input.ComponentElements = append(input.ComponentElements, service.ComponentElementInput{
			ComponentID: componentID, ElementID: elementID,
		})
	}
	return input, nil
}

func parseCanonicalPositiveInt64(value string) (int64, error) {
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed <= 0 || strconv.FormatInt(parsed, 10) != value {
		return 0, service.ErrInvalidEntryUpdate
	}
	return parsed, nil
}

func parseOptionalCanonicalPositiveInt64(value string) (int64, error) {
	if value == "" {
		return 0, nil
	}
	return parseCanonicalPositiveInt64(value)
}

func parseCanonicalUUID(value string) (uuid.UUID, error) {
	parsed, err := uuid.Parse(strings.TrimSpace(value))
	if err != nil || parsed == uuid.Nil || parsed.String() != value {
		return uuid.Nil, service.ErrInvalidSourceRebind
	}
	return parsed, nil
}

func parseCanonicalUUIDs(values []string) ([]uuid.UUID, error) {
	result := make([]uuid.UUID, 0, len(values))
	seen := make(map[uuid.UUID]struct{}, len(values))
	for _, value := range values {
		id, err := parseCanonicalUUID(value)
		if err != nil {
			return nil, err
		}
		if _, exists := seen[id]; exists {
			return nil, service.ErrInvalidCollection
		}
		seen[id] = struct{}{}
		result = append(result, id)
	}
	return result, nil
}

func entryAccess(c *gin.Context) service.EntryAccess {
	access := service.EntryAccess{Inventory: commonAuth.HasRolePermission(c, catalogauthorization.PermissionCatalogInventoryRead)}
	authContext, ok := commonAuth.AuthContextFromGin(c)
	if !ok {
		return access
	}
	seen := make(map[int64]struct{}, len(authContext.Organization.Departments))
	for _, membership := range authContext.Organization.Departments {
		departmentID, err := strconv.ParseInt(membership.DepartmentID, 10, 64)
		if err != nil || departmentID <= 0 {
			continue
		}
		if _, exists := seen[departmentID]; !exists {
			seen[departmentID] = struct{}{}
			access.DepartmentIDs = append(access.DepartmentIDs, departmentID)
		}
	}
	return access
}

func currentUserContext(c *gin.Context) (int64, int64, bool) {
	tenantID, tenantOK := commonAuth.TenantIDFromGin(c)
	principal, principalOK := commonAuth.PrincipalFromGin(c)
	if !tenantOK || !principalOK || principal.Type != "user" {
		return 0, 0, false
	}
	userID, err := strconv.ParseInt(principal.ID, 10, 64)
	return tenantID, userID, err == nil && userID > 0
}

func collectionAccess(c *gin.Context, userID int64) service.CollectionAccess {
	return service.CollectionAccess{
		UserID: userID, ReadGroupIDs: permissionProjectGroupIDs(c, catalogauthorization.PermissionCatalogCollectionRead),
		UpdateGroupIDs: permissionProjectGroupIDs(c, catalogauthorization.PermissionCatalogCollectionUpdate), EntryAccess: entryAccess(c),
	}
}

func permissionProjectGroupIDs(c *gin.Context, permission string) []int64 {
	authContext, ok := commonAuth.AuthContextFromGin(c)
	if !ok {
		return nil
	}
	tenantScope := false
	projectGroupScopes := make(map[string]struct{})
	for _, scope := range commonAuth.RolePermissionScopes(c, permission) {
		switch scope.Type {
		case "tenant":
			tenantScope = true
		case "project_group":
			if scope.ProjectGroupID != nil {
				projectGroupScopes[*scope.ProjectGroupID] = struct{}{}
			}
		}
	}
	result := make([]int64, 0, len(authContext.Organization.ProjectGroups))
	for _, membership := range authContext.Organization.ProjectGroups {
		if !tenantScope {
			if _, exists := projectGroupScopes[membership.ProjectGroupID]; !exists {
				continue
			}
		}
		id, err := strconv.ParseInt(membership.ProjectGroupID, 10, 64)
		if err == nil && id > 0 {
			result = append(result, id)
		}
	}
	return result
}

func respondError(c *gin.Context, status int, err error) {
	message := commoni18n.T(c, catalogi18n.MsgOperationFailed)
	errorCode := "catalog_operation_failed"
	switch {
	case errors.Is(err, service.ErrEntryNotFound):
		status = http.StatusNotFound
		message = commoni18n.T(c, catalogi18n.MsgEntryNotFound)
		errorCode = "catalog_entry_not_found"
	case errors.Is(err, service.ErrInvalidPage):
		status = http.StatusBadRequest
		message = commoni18n.T(c, catalogi18n.MsgInvalidParams)
		errorCode = "invalid_request"
	case errors.Is(err, service.ErrEntryVersionConflict):
		status = http.StatusConflict
		message = commoni18n.T(c, catalogi18n.MsgVersionConflict)
		errorCode = "catalog_entry_version_conflict"
	case errors.Is(err, service.ErrEntryNotEditable):
		status = http.StatusConflict
		message = commoni18n.T(c, catalogi18n.MsgEntryNotEditable)
		errorCode = "catalog_entry_not_editable"
	case errors.Is(err, service.ErrInvalidGovernanceTransition):
		status = http.StatusConflict
		message = commoni18n.T(c, catalogi18n.MsgInvalidTransition)
		errorCode = "catalog_governance_transition_invalid"
	case errors.Is(err, service.ErrReferenceNotReferenceable):
		status = http.StatusConflict
		message = commoni18n.T(c, catalogi18n.MsgReferenceNotReferenceable)
		errorCode = "catalog_reference_not_referenceable"
	case errors.Is(err, service.ErrCertificationPermissionRequired):
		status = http.StatusForbidden
		message = commoni18n.T(c, catalogi18n.MsgCertificationPermissionRequired)
		errorCode = "catalog_certification_permission_required"
	case errors.Is(err, service.ErrDeprecationPermissionRequired):
		status = http.StatusForbidden
		message = commoni18n.T(c, catalogi18n.MsgDeprecationPermissionRequired)
		errorCode = "catalog_deprecation_permission_required"
	case errors.Is(err, service.ErrReferenceValidationUnavailable):
		status = http.StatusServiceUnavailable
		message = commoni18n.T(c, catalogi18n.MsgReferenceValidationUnavailable)
		errorCode = "catalog_reference_validation_unavailable"
	case errors.Is(err, service.ErrCurationRequirementsNotMet):
		status = http.StatusBadRequest
		message = commoni18n.T(c, catalogi18n.MsgCurationRequirementsNotMet)
		errorCode = "catalog_curation_requirements_not_met"
	case errors.Is(err, service.ErrDeprecationReasonRequired):
		status = http.StatusBadRequest
		message = commoni18n.T(c, catalogi18n.MsgDeprecationReasonRequired)
		errorCode = "catalog_deprecation_reason_required"
	case errors.Is(err, service.ErrInvalidRecommendedSuccessor):
		status = http.StatusConflict
		message = commoni18n.T(c, catalogi18n.MsgInvalidRecommendedSuccessor)
		errorCode = "catalog_recommended_successor_invalid"
	case errors.Is(err, service.ErrInvalidEntryUpdate):
		status = http.StatusBadRequest
		message = commoni18n.T(c, catalogi18n.MsgInvalidParams)
		errorCode = "invalid_request"
	case errors.Is(err, service.ErrSourceRebindConflict):
		status = http.StatusConflict
		message = commoni18n.T(c, catalogi18n.MsgSourceRebindConflict)
		errorCode = "catalog_source_rebind_conflict"
	case errors.Is(err, service.ErrInvalidSourceRebind):
		status = http.StatusBadRequest
		message = commoni18n.T(c, catalogi18n.MsgInvalidParams)
		errorCode = "invalid_request"
	case errors.Is(err, service.ErrSearchUnavailable):
		status = http.StatusServiceUnavailable
		message = commoni18n.T(c, catalogi18n.MsgSearchUnavailable)
		errorCode = "catalog_search_unavailable"
	case errors.Is(err, service.ErrInventoryPermissionRequired):
		status = http.StatusForbidden
		message = commoni18n.T(c, catalogi18n.MsgInventoryPermissionRequired)
		errorCode = "catalog_inventory_permission_required"
	case errors.Is(err, service.ErrUserPrincipalRequired):
		status = http.StatusForbidden
		message = commoni18n.T(c, catalogi18n.MsgUserPrincipalRequired)
		errorCode = "catalog_user_principal_required"
	case errors.Is(err, service.ErrInvalidPersonalRelation):
		status = http.StatusBadRequest
		message = commoni18n.T(c, catalogi18n.MsgInvalidParams)
		errorCode = "invalid_request"
	case errors.Is(err, service.ErrCollectionNotFound):
		status = http.StatusNotFound
		message = commoni18n.T(c, catalogi18n.MsgCollectionNotFound)
		errorCode = "catalog_collection_not_found"
	case errors.Is(err, service.ErrCollectionVersionConflict):
		status = http.StatusConflict
		message = commoni18n.T(c, catalogi18n.MsgCollectionVersionConflict)
		errorCode = "catalog_collection_version_conflict"
	case errors.Is(err, service.ErrCollectionNameConflict):
		status = http.StatusConflict
		message = commoni18n.T(c, catalogi18n.MsgCollectionNameConflict)
		errorCode = "catalog_collection_name_conflict"
	case errors.Is(err, service.ErrInvalidCollection):
		status = http.StatusBadRequest
		message = commoni18n.T(c, catalogi18n.MsgInvalidParams)
		errorCode = "invalid_request"
	case status == http.StatusBadRequest:
		message = commoni18n.T(c, catalogi18n.MsgInvalidParams)
		errorCode = "invalid_request"
	}
	c.JSON(status, gin.H{"error": message, "error_code": errorCode})
}
