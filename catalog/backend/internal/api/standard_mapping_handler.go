package api

import (
	"net/http"
	"strconv"

	"github.com/addp/catalog/internal/models"
	"github.com/addp/catalog/internal/service"
	commonapi "github.com/addp/common/api"
	commonAuth "github.com/addp/common/middleware/auth"
	commonModels "github.com/addp/common/models"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type standardMappingResponse models.StandardMapping

type standardMappingRequest struct {
	CatalogEntryID    string               `json:"catalog_entry_id" format:"uuid"`
	ComponentID       string               `json:"component_id" format:"uuid"`
	ElementID         string               `json:"element_id"`
	ElementRevisionID string               `json:"element_revision_id"`
	Confidence        *float64             `json:"confidence,omitempty"`
	Evidence          commonModels.JSONMap `json:"evidence"`
	Version           int64                `json:"version,omitempty" minimum:"1"`
}

type standardMappingDecisionRequest struct {
	Version int64  `json:"version" minimum:"1"`
	Opinion string `json:"opinion"`
}

type deleteStandardMappingRequest struct {
	Version int64 `json:"version" minimum:"1"`
}

func parseMappingID(value string) (uuid.UUID, error) {
	id, err := uuid.Parse(value)
	if err != nil || id == uuid.Nil || id.String() != value {
		return uuid.Nil, service.ErrInvalidStandardMapping
	}
	return id, nil
}

func parseMappingInput(request standardMappingRequest) (service.StandardMappingInput, error) {
	entryID, err := parseMappingID(request.CatalogEntryID)
	if err != nil {
		return service.StandardMappingInput{}, err
	}
	componentID, err := parseMappingID(request.ComponentID)
	if err != nil {
		return service.StandardMappingInput{}, err
	}
	elementID, err := parseCanonicalPositiveInt64(request.ElementID)
	if err != nil {
		return service.StandardMappingInput{}, service.ErrInvalidStandardMapping
	}
	revisionID, err := parseCanonicalPositiveInt64(request.ElementRevisionID)
	if err != nil {
		return service.StandardMappingInput{}, service.ErrInvalidStandardMapping
	}
	return service.StandardMappingInput{
		CatalogEntryID: entryID, ComponentID: componentID, ElementID: elementID,
		ElementRevisionID: revisionID, Confidence: request.Confidence,
		Evidence: request.Evidence, Version: request.Version,
	}, nil
}

func mappingContext(c *gin.Context) (int64, service.UpdateEntryActor, bool) {
	tenantID, ok := commonAuth.TenantIDFromGin(c)
	auth, authOK := commonAuth.AuthContextFromGin(c)
	if !ok || !authOK {
		return 0, service.UpdateEntryActor{}, false
	}
	return tenantID, service.UpdateEntryActor{Type: auth.Principal.Type, ID: auth.Principal.ID}, true
}

// ListStandardMappings lists candidate and reviewed relations for one visible entry.
// @Summary 查询字段标准映射 | List field standard mappings
// @Tags Catalog Standard Mappings
// @Produce json
// @Param catalog_entry_id query string true "CatalogEntry UUID"
// @Param page query int false "页码 | Page"
// @Param page_size query int false "每页数量，最多 200 | Page size, maximum 200"
// @Success 200 {object} service.StandardMappingList
// @Failure 400 {object} map[string]interface{}
// @Failure 401 {object} map[string]interface{}
// @Failure 403 {object} map[string]interface{}
// @Failure 404 {object} map[string]interface{}
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["catalog.entry.read"]
// @Router /standard-mappings [get]
// @Security BearerAuth
func (h *Handler) ListStandardMappings(c *gin.Context) {
	tenantID, ok := commonAuth.TenantIDFromGin(c)
	entryID, idErr := parseMappingID(c.Query("catalog_entry_id"))
	page, pageErr := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, sizeErr := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if !ok || idErr != nil || pageErr != nil || sizeErr != nil {
		respondError(c, http.StatusBadRequest, service.ErrInvalidPage)
		return
	}
	result, err := h.entries.ListStandardMappings(c.Request.Context(), tenantID, entryAccess(c), entryID, page, pageSize)
	if err != nil {
		respondError(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// ListStandardMappingRevisionOptions queries Standard-owned published revisions for one selected element.
// @Summary 查询可选数据元修订 | List published element revision choices
// @Tags Catalog Standard Mappings
// @Produce json
// @Param element_id query string true "数据元稳定 ID | Stable element ID"
// @Success 200 {array} service.StandardMappingRevisionOption
// @Failure 400 {object} map[string]interface{}
// @Failure 401 {object} map[string]interface{}
// @Failure 403 {object} map[string]interface{}
// @Failure 503 {object} map[string]interface{}
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["catalog.entry.update"]
// @Router /standard-mappings/revision-options [get]
// @Security BearerAuth
func (h *Handler) ListStandardMappingRevisionOptions(c *gin.Context) {
	tenantID, ok := commonAuth.TenantIDFromGin(c)
	elementID, err := parseCanonicalPositiveInt64(c.Query("element_id"))
	if !ok || err != nil {
		respondError(c, http.StatusBadRequest, service.ErrInvalidStandardMapping)
		return
	}
	options, err := h.entries.ListStandardMappingRevisionOptions(c.Request.Context(), tenantID, elementID)
	if err != nil {
		respondError(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, options)
}

// CreateStandardMapping creates a manual candidate, never an approved mapping.
// @Summary 提交字段标准映射候选 | Propose a field standard mapping
// @Tags Catalog Standard Mappings
// @Accept json
// @Produce json
// @Param request body standardMappingRequest true "确定的数据元修订及证据 | Exact element revision and evidence"
// @Success 201 {object} standardMappingResponse
// @Failure 400 {object} map[string]interface{}
// @Failure 401 {object} map[string]interface{}
// @Failure 403 {object} map[string]interface{}
// @Failure 409 {object} map[string]interface{}
// @Failure 503 {object} map[string]interface{}
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["catalog.entry.update"]
// @Router /standard-mappings [post]
// @Security BearerAuth
func (h *Handler) CreateStandardMapping(c *gin.Context) {
	tenantID, actor, ok := mappingContext(c)
	var request standardMappingRequest
	if !ok || commonapi.BindOptionalJSONStrict(c, &request) != nil {
		respondError(c, http.StatusBadRequest, service.ErrInvalidStandardMapping)
		return
	}
	input, err := parseMappingInput(request)
	if err != nil {
		respondError(c, http.StatusBadRequest, err)
		return
	}
	row, err := h.entries.CreateStandardMapping(c.Request.Context(), tenantID, input, actor)
	if err != nil {
		respondError(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusCreated, row)
}

// GetStandardMapping reads one mapping only when its entry is visible.
// @Summary 查询字段标准映射 | Get a field standard mapping
// @Tags Catalog Standard Mappings
// @Produce json
// @Param id path string true "StandardMapping UUID"
// @Success 200 {object} standardMappingResponse
// @Failure 400 {object} map[string]interface{}
// @Failure 401 {object} map[string]interface{}
// @Failure 403 {object} map[string]interface{}
// @Failure 404 {object} map[string]interface{}
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["catalog.entry.read"]
// @Router /standard-mappings/{id} [get]
// @Security BearerAuth
func (h *Handler) GetStandardMapping(c *gin.Context) {
	tenantID, ok := commonAuth.TenantIDFromGin(c)
	id, err := parseMappingID(c.Param("id"))
	if !ok || err != nil {
		respondError(c, http.StatusBadRequest, service.ErrInvalidStandardMapping)
		return
	}
	row, err := h.entries.GetStandardMapping(c.Request.Context(), tenantID, entryAccess(c), id)
	if err != nil {
		respondError(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, row)
}

// UpdateStandardMapping edits only a proposed candidate at its own version.
// @Summary 修改字段标准映射候选 | Update a proposed field standard mapping
// @Tags Catalog Standard Mappings
// @Accept json
// @Produce json
// @Param id path string true "StandardMapping UUID"
// @Param request body standardMappingRequest true "完整候选及映射版本 | Complete candidate and mapping version"
// @Success 200 {object} standardMappingResponse
// @Failure 400 {object} map[string]interface{}
// @Failure 401 {object} map[string]interface{}
// @Failure 403 {object} map[string]interface{}
// @Failure 404 {object} map[string]interface{}
// @Failure 409 {object} map[string]interface{}
// @Failure 503 {object} map[string]interface{}
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["catalog.entry.update"]
// @Router /standard-mappings/{id} [put]
// @Security BearerAuth
func (h *Handler) UpdateStandardMapping(c *gin.Context) {
	tenantID, actor, ok := mappingContext(c)
	id, idErr := parseMappingID(c.Param("id"))
	var request standardMappingRequest
	if !ok || idErr != nil || commonapi.BindOptionalJSONStrict(c, &request) != nil {
		respondError(c, http.StatusBadRequest, service.ErrInvalidStandardMapping)
		return
	}
	input, err := parseMappingInput(request)
	if err != nil {
		respondError(c, http.StatusBadRequest, err)
		return
	}
	row, err := h.entries.UpdateStandardMapping(c.Request.Context(), tenantID, id, input, actor)
	if err != nil {
		respondError(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, row)
}

// DeleteStandardMapping deletes only an unreviewed candidate at its version.
// @Summary 删除字段标准映射候选 | Delete a proposed field standard mapping
// @Tags Catalog Standard Mappings
// @Accept json
// @Produce json
// @Param id path string true "StandardMapping UUID"
// @Param request body deleteStandardMappingRequest true "映射版本 | Mapping version"
// @Success 204 "已删除 | Deleted"
// @Failure 400 {object} map[string]interface{}
// @Failure 401 {object} map[string]interface{}
// @Failure 403 {object} map[string]interface{}
// @Failure 404 {object} map[string]interface{}
// @Failure 409 {object} map[string]interface{}
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["catalog.entry.update"]
// @Router /standard-mappings/{id} [delete]
// @Security BearerAuth
func (h *Handler) DeleteStandardMapping(c *gin.Context) {
	tenantID, actor, ok := mappingContext(c)
	id, idErr := parseMappingID(c.Param("id"))
	var request deleteStandardMappingRequest
	if !ok || idErr != nil || commonapi.BindOptionalJSONStrict(c, &request) != nil {
		respondError(c, http.StatusBadRequest, service.ErrInvalidStandardMapping)
		return
	}
	if err := h.entries.DeleteStandardMapping(c.Request.Context(), tenantID, id, request.Version, actor); err != nil {
		respondError(c, http.StatusInternalServerError, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handler) reviewStandardMapping(c *gin.Context, action string) {
	tenantID, actor, ok := mappingContext(c)
	id, idErr := parseMappingID(c.Param("id"))
	var request standardMappingDecisionRequest
	if !ok || idErr != nil || commonapi.BindOptionalJSONStrict(c, &request) != nil {
		respondError(c, http.StatusBadRequest, service.ErrInvalidStandardMapping)
		return
	}
	row, err := h.entries.ReviewStandardMapping(c.Request.Context(), tenantID, id, action, service.StandardMappingDecision{Version: request.Version, Opinion: request.Opinion}, actor)
	if err != nil {
		respondError(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, row)
}

// ApproveStandardMapping approves a pinned revision and withdraws the prior approved mapping atomically.
// @Summary 审核通过字段标准映射 | Approve a field standard mapping
// @Tags Catalog Standard Mappings
// @Accept json
// @Produce json
// @Param id path string true "StandardMapping UUID"
// @Param request body standardMappingDecisionRequest true "版本与审核意见 | Version and review opinion"
// @Success 200 {object} standardMappingResponse
// @Failure 400 {object} map[string]interface{}
// @Failure 401 {object} map[string]interface{}
// @Failure 403 {object} map[string]interface{}
// @Failure 404 {object} map[string]interface{}
// @Failure 409 {object} map[string]interface{}
// @Failure 503 {object} map[string]interface{}
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["catalog.standard_mapping.review"]
// @Router /standard-mappings/{id}/approve [post]
// @Security BearerAuth
func (h *Handler) ApproveStandardMapping(c *gin.Context) { h.reviewStandardMapping(c, "approve") }

// RejectStandardMapping rejects a candidate with an opinion.
// @Summary 驳回字段标准映射 | Reject a field standard mapping
// @Tags Catalog Standard Mappings
// @Accept json
// @Produce json
// @Param id path string true "StandardMapping UUID"
// @Param request body standardMappingDecisionRequest true "版本与审核意见 | Version and review opinion"
// @Success 200 {object} standardMappingResponse
// @Failure 400 {object} map[string]interface{}
// @Failure 401 {object} map[string]interface{}
// @Failure 403 {object} map[string]interface{}
// @Failure 404 {object} map[string]interface{}
// @Failure 409 {object} map[string]interface{}
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["catalog.standard_mapping.review"]
// @Router /standard-mappings/{id}/reject [post]
// @Security BearerAuth
func (h *Handler) RejectStandardMapping(c *gin.Context) { h.reviewStandardMapping(c, "reject") }

// WithdrawStandardMapping withdraws an approved mapping with a reason.
// @Summary 撤回字段标准映射 | Withdraw a field standard mapping
// @Tags Catalog Standard Mappings
// @Accept json
// @Produce json
// @Param id path string true "StandardMapping UUID"
// @Param request body standardMappingDecisionRequest true "版本与撤回原因 | Version and withdrawal reason"
// @Success 200 {object} standardMappingResponse
// @Failure 400 {object} map[string]interface{}
// @Failure 401 {object} map[string]interface{}
// @Failure 403 {object} map[string]interface{}
// @Failure 404 {object} map[string]interface{}
// @Failure 409 {object} map[string]interface{}
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["catalog.standard_mapping.review"]
// @Router /standard-mappings/{id}/withdraw [post]
// @Security BearerAuth
func (h *Handler) WithdrawStandardMapping(c *gin.Context) { h.reviewStandardMapping(c, "withdraw") }
