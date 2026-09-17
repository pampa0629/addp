package api

import (
	"errors"
	"net/http"
	"strconv"

	commonapi "github.com/addp/common/api"
	commonAuth "github.com/addp/common/middleware/auth"
	standardauthorization "github.com/addp/standard/internal/authorization"
	"github.com/addp/standard/internal/service"
	"github.com/gin-gonic/gin"
)

type ReferenceResolutionHandler struct {
	service *service.ReferenceResolutionService
}

type referenceResolutionRequest struct {
	References []service.ReferenceResolutionRequest `json:"references" binding:"required,min=1,max=200"`
}

type referenceResolutionResponse struct {
	Results []service.ReferenceResolution `json:"results"`
}

func NewReferenceResolutionHandler(resolutionService *service.ReferenceResolutionService) *ReferenceResolutionHandler {
	return &ReferenceResolutionHandler{service: resolutionService}
}

// Resolve 精确解析内部消费者提交的 Standard 引用。
// @Summary 精确批量解析 Standard 引用 | Resolve exact Standard references in batch
// @Description 仅 addp-catalog 与 addp-model Tenant Service Principal 可按当前 Tenant 通过 ID 或稳定编码精确解析 Domain、Glossary 和 Element；每项必须且只能提供 id 或 code，跨 Tenant 与不存在统一返回 found=false；Glossary 仅允许 addp-catalog 解析 | Only addp-catalog and addp-model tenant service principals may resolve domains, glossaries, and elements in the current tenant by exact ID or stable code; each item must provide exactly one of id or code, cross-tenant and missing references both return found=false, and only addp-catalog may resolve glossaries
// @Tags CatalogReferences
// @Accept json
// @Produce json
// @Param request body referenceResolutionRequest true "精确引用集合，最多 200 个 | Exact references, up to 200"
// @Success 200 {object} referenceResolutionResponse
// @Failure 400 {object} map[string]string "请求无效 | Invalid request"
// @Failure 401 {object} map[string]string "需要认证 | Authentication required"
// @Failure 403 {object} map[string]string "仅允许指定运行时客户端且需对应读取权限 | Allowed runtime client and corresponding read permissions required"
// @Failure 500 {object} map[string]string "解析失败 | Resolution failed"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.domain.read","standard.element.read"]
// @x-addp-conditional-permissions ["standard.glossary.read"]
// @Router /references/resolve [post]
// @Security BearerAuth
func (h *ReferenceResolutionHandler) Resolve(c *gin.Context) {
	var request referenceResolutionRequest
	if h == nil || h.service == nil || c.ShouldBindJSON(&request) != nil {
		respondError(c, http.StatusBadRequest, service.ErrInvalidReferenceResolutionRequest)
		return
	}
	for _, reference := range request.References {
		if reference.ObjectType == service.ReferenceTypeGlossary && !commonAuth.HasRolePermission(c, standardauthorization.PermissionStandardGlossaryRead) {
			respondError(c, http.StatusForbidden, commonapi.ErrForbidden)
			return
		}
	}
	results, err := h.service.Resolve(c.Request.Context(), getTenantID(c), request.References)
	if err != nil {
		if errors.Is(err, service.ErrInvalidReferenceResolutionRequest) {
			respondError(c, http.StatusBadRequest, err)
			return
		}
		respondError(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, referenceResolutionResponse{Results: results})
}

// ListCandidates 查询 Catalog 可建立新关联的 Standard 候选。
// @Summary 查询 Catalog 的 Standard 引用候选 | List Standard reference candidates for Catalog
// @Description 仅 addp-catalog Tenant Service Principal 可按名称或编码分页查询当前可引用的 Domain、Glossary 或 Element；只返回最小显示摘要，业务域包含 domain_path 并支持路径搜索 | Only the addp-catalog tenant service principal may search currently referenceable domains, glossaries, or elements by name or code; only minimal display summaries are returned, including domain_path and path search for domains
// @Tags CatalogReferences
// @Produce json
// @Param object_type query string true "引用类型 | Reference type" Enums(domain,glossary,element)
// @Param search query string false "名称或编码，最多 100 字符 | Name or code, maximum 100 characters"
// @Param page query int false "页码，默认 1 | Page number, default 1"
// @Param page_size query int false "每页数量，默认 20，最大 50 | Page size, default 20 and maximum 50"
// @Success 200 {object} service.ReferenceCandidateList
// @Failure 400 {object} map[string]string "请求无效 | Invalid request"
// @Failure 401 {object} map[string]string "需要认证 | Authentication required"
// @Failure 403 {object} map[string]string "仅允许 addp-catalog 且需三个读取权限 | addp-catalog and all three read permissions required"
// @Failure 500 {object} map[string]string "查询失败 | Query failed"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.domain.read","standard.glossary.read","standard.element.read"]
// @Router /references/candidates [get]
// @Security BearerAuth
func (h *ReferenceResolutionHandler) ListCandidates(c *gin.Context) {
	page, pageErr := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, pageSizeErr := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if h == nil || h.service == nil || pageErr != nil || pageSizeErr != nil {
		respondError(c, http.StatusBadRequest, service.ErrInvalidReferenceResolutionRequest)
		return
	}
	result, err := h.service.ListCandidates(
		c.Request.Context(), getTenantID(c), service.ReferenceType(c.Query("object_type")), c.Query("search"), page, pageSize,
	)
	if err != nil {
		if errors.Is(err, service.ErrInvalidReferenceResolutionRequest) {
			respondError(c, http.StatusBadRequest, err)
			return
		}
		respondError(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, result)
}
