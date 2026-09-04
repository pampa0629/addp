package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	commonapi "github.com/addp/common/api"
	"github.com/addp/system/internal/iam"
	"github.com/addp/system/internal/middleware"
	"github.com/gin-gonic/gin"
)

type IAMCatalogReferenceRequest struct {
	SubjectType string `json:"subject_type" enums:"department,user,project_group"`
	ID          string `json:"id"`
}

type IAMCatalogReferenceBatchRequest struct {
	References []IAMCatalogReferenceRequest `json:"references"`
}

type IAMCatalogReferenceResolution struct {
	SubjectType      string `json:"subject_type"`
	ID               string `json:"id"`
	Found            bool   `json:"found"`
	Referenceable    bool   `json:"referenceable"`
	Name             string `json:"name,omitempty"`
	Code             string `json:"code,omitempty"`
	Status           string `json:"status,omitempty"`
	PrincipalStatus  string `json:"principal_status,omitempty"`
	MembershipStatus string `json:"membership_status,omitempty"`
}

type IAMCatalogReferenceBatchResponse struct {
	Results []IAMCatalogReferenceResolution `json:"results"`
}

type catalogReferenceService interface {
	Resolve(context.Context, int64, string, []iam.CatalogReference) ([]iam.CatalogReferenceResolution, error)
	ListCandidates(context.Context, int64, string, iam.CatalogSubjectType, string, int, int) ([]iam.CatalogReferenceCandidate, int64, error)
}

type IAMCatalogReferenceHandler struct {
	service catalogReferenceService
}

func NewIAMCatalogReferenceHandler(service catalogReferenceService) (*IAMCatalogReferenceHandler, error) {
	if service == nil {
		return nil, fmt.Errorf("%w: catalog reference service is required", commonapi.ErrBadRequest)
	}
	return &IAMCatalogReferenceHandler{service: service}, nil
}

// Resolve godoc
// @Summary      精确批量解析 Catalog 组织引用 | Resolve Catalog organization references in batch
// @Description  仅 addp-catalog Tenant Service Principal 可按当前 Tenant 解析 Department、User 与 Project Group；User ID 是全局稳定身份但必须有当前 Tenant Membership；Project Group 只供协作集合显示；跨 Tenant 与不存在统一返回 found=false | Only the addp-catalog tenant service principal may resolve departments, users, and project groups in the current tenant; a user ID is globally stable but requires a current tenant membership; project groups are only used to display collaboration collections; cross-tenant and missing subjects both return found=false
// @Tags         Runtime Catalog References
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        request body IAMCatalogReferenceBatchRequest true "主体引用集合，最多 200 个 | Subject references, up to 200"
// @Success      200 {object} IAMCatalogReferenceBatchResponse
// @Failure      400 {object} IAMErrorResponse
// @Failure      401 {object} IAMErrorResponse
// @Failure      403 {object} IAMErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["iam.department.read","iam.project_group.read","iam.tenant_membership.read"]
// @Router       /runtime/catalog-references/resolve [post]
func (h *IAMCatalogReferenceHandler) Resolve(c *gin.Context) {
	if err := iamServiceOwnsModule(c, "catalog"); err != nil {
		respondIAMError(c, err)
		return
	}
	var request IAMCatalogReferenceBatchRequest
	if err := commonapi.BindOptionalJSONStrict(c, &request); err != nil || len(request.References) == 0 || len(request.References) > iam.MaxCatalogReferenceBatchSize {
		respondIAMError(c, fmt.Errorf("%w: invalid catalog reference request", commonapi.ErrBadRequest))
		return
	}
	references := make([]iam.CatalogReference, 0, len(request.References))
	for _, reference := range request.References {
		id, err := parseCanonicalIAMInt64(reference.ID)
		if err != nil || (reference.SubjectType != string(iam.CatalogSubjectTypeDepartment) && reference.SubjectType != string(iam.CatalogSubjectTypeUser) && reference.SubjectType != string(iam.CatalogSubjectTypeProjectGroup)) {
			respondIAMError(c, fmt.Errorf("%w: invalid catalog reference", commonapi.ErrBadRequest))
			return
		}
		references = append(references, iam.CatalogReference{SubjectType: iam.CatalogSubjectType(reference.SubjectType), ID: id})
	}
	_, tenantID, _, err := iamTenantActor(c)
	if err != nil {
		respondIAMError(c, err)
		return
	}
	authContext, exists := middleware.IAMAuthContextFromGin(c)
	if !exists || authContext.Client.ClientID == nil {
		respondIAMError(c, commonapi.ErrUnauthorized)
		return
	}
	results, err := h.service.Resolve(c.Request.Context(), int64(tenantID), *authContext.Client.ClientID, references)
	if err != nil {
		if errors.Is(err, iam.ErrInvalidCatalogReferenceRequest) {
			err = fmt.Errorf("%w: invalid catalog reference request", commonapi.ErrBadRequest)
		}
		respondIAMError(c, err)
		return
	}
	response := IAMCatalogReferenceBatchResponse{Results: make([]IAMCatalogReferenceResolution, 0, len(results))}
	for _, result := range results {
		response.Results = append(response.Results, IAMCatalogReferenceResolution{
			SubjectType: string(result.SubjectType), ID: strconv.FormatInt(result.ID, 10),
			Found: result.Found, Referenceable: result.Referenceable,
			Name: result.Name, Code: result.Code, Status: result.Status,
			PrincipalStatus: result.PrincipalStatus, MembershipStatus: result.MembershipStatus,
		})
	}
	c.JSON(http.StatusOK, response)
}

type IAMCatalogReferenceCandidateResponse struct {
	SubjectType string `json:"subject_type"`
	ID          string `json:"id"`
	Name        string `json:"name"`
	Code        string `json:"code,omitempty"`
	Status      string `json:"status"`
}

// ListCandidates godoc
// @Summary      查询 Catalog 责任主体候选 | List Catalog responsibility candidates
// @Description  仅 addp-catalog Tenant Service Principal 可按名称或编码分页查询当前可引用的 Department 或 User；只返回最小显示摘要 | Only the addp-catalog tenant service principal may search currently referenceable departments or users by name or code; only minimal display summaries are returned
// @Tags         Runtime Catalog References
// @Produce      json
// @Security     BearerAuth
// @Param        subject_type query string true "主体类型 | Subject type" Enums(department,user)
// @Param        search query string false "名称或编码，最多 100 字符 | Name or code, maximum 100 characters"
// @Param        page query int false "页码，默认 1 | Page number, default 1"
// @Param        page_size query int false "每页数量，默认 20，最大 50 | Page size, default 20 and maximum 50"
// @Success      200 {object} object{data=[]IAMCatalogReferenceCandidateResponse,total=int64,page=int,page_size=int,total_pages=int}
// @Failure      400 {object} IAMErrorResponse
// @Failure      401 {object} IAMErrorResponse
// @Failure      403 {object} IAMErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["iam.department.read","iam.tenant_membership.read"]
// @Router       /runtime/catalog-references/candidates [get]
func (h *IAMCatalogReferenceHandler) ListCandidates(c *gin.Context) {
	if err := iamServiceOwnsModule(c, "catalog"); err != nil {
		respondIAMError(c, err)
		return
	}
	page, pageErr := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, pageSizeErr := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	_, tenantID, _, err := iamTenantActor(c)
	if err != nil {
		respondIAMError(c, err)
		return
	}
	authContext, exists := middleware.IAMAuthContextFromGin(c)
	if !exists || authContext.Client.ClientID == nil {
		respondIAMError(c, commonapi.ErrUnauthorized)
		return
	}
	if pageErr != nil || pageSizeErr != nil {
		respondIAMError(c, fmt.Errorf("%w: invalid catalog reference candidate request", commonapi.ErrBadRequest))
		return
	}
	items, total, err := h.service.ListCandidates(
		c.Request.Context(), int64(tenantID), *authContext.Client.ClientID,
		iam.CatalogSubjectType(c.Query("subject_type")), c.Query("search"), page, pageSize,
	)
	if err != nil {
		if errors.Is(err, iam.ErrInvalidCatalogReferenceRequest) {
			err = fmt.Errorf("%w: invalid catalog reference candidate request", commonapi.ErrBadRequest)
		}
		respondIAMError(c, err)
		return
	}
	data := make([]IAMCatalogReferenceCandidateResponse, 0, len(items))
	for _, item := range items {
		data = append(data, IAMCatalogReferenceCandidateResponse{
			SubjectType: string(item.SubjectType), ID: strconv.FormatInt(item.ID, 10),
			Name: item.Name, Code: item.Code, Status: item.Status,
		})
	}
	commonapi.RespondPaginated(c, data, total, page, pageSize)
}

// ResolveStandardGovernanceUsers godoc
// @Summary      精确批量解析 Standard 治理用户 | Resolve Standard governance users in batch
// @Description  仅 addp-standard Tenant Service Principal 可按当前 Tenant 解析 User Principal；跨 Tenant、非用户和无有效成员关系均不可引用 | Only the addp-standard tenant service principal may resolve User Principals in the current tenant; cross-tenant, non-user, and inactive memberships are not referenceable
// @Tags         Runtime Standard Governance Users
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        request body IAMCatalogReferenceBatchRequest true "User 引用集合，最多 200 个 | User references, up to 200"
// @Success      200 {object} IAMCatalogReferenceBatchResponse
// @Failure      400 {object} IAMErrorResponse
// @Failure      401 {object} IAMErrorResponse
// @Failure      403 {object} IAMErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["iam.tenant_membership.read"]
// @Router       /runtime/standard-governance-users/resolve [post]
func (h *IAMCatalogReferenceHandler) ResolveStandardGovernanceUsers(c *gin.Context) {
	if err := iamServiceOwnsModule(c, "standard"); err != nil {
		respondIAMError(c, err)
		return
	}
	var request IAMCatalogReferenceBatchRequest
	if err := commonapi.BindOptionalJSONStrict(c, &request); err != nil || len(request.References) == 0 || len(request.References) > iam.MaxCatalogReferenceBatchSize {
		respondIAMError(c, fmt.Errorf("%w: invalid Standard governance user request", commonapi.ErrBadRequest))
		return
	}
	references := make([]iam.CatalogReference, 0, len(request.References))
	for _, reference := range request.References {
		id, err := parseCanonicalIAMInt64(reference.ID)
		if err != nil || reference.SubjectType != string(iam.CatalogSubjectTypeUser) {
			respondIAMError(c, fmt.Errorf("%w: invalid Standard governance user reference", commonapi.ErrBadRequest))
			return
		}
		references = append(references, iam.CatalogReference{SubjectType: iam.CatalogSubjectTypeUser, ID: id})
	}
	_, tenantID, _, err := iamTenantActor(c)
	if err != nil {
		respondIAMError(c, err)
		return
	}
	authContext, exists := middleware.IAMAuthContextFromGin(c)
	if !exists || authContext.Client.ClientID == nil {
		respondIAMError(c, commonapi.ErrUnauthorized)
		return
	}
	results, err := h.service.Resolve(c.Request.Context(), int64(tenantID), *authContext.Client.ClientID, references)
	if err != nil {
		respondIAMError(c, err)
		return
	}
	response := IAMCatalogReferenceBatchResponse{Results: make([]IAMCatalogReferenceResolution, 0, len(results))}
	for _, result := range results {
		response.Results = append(response.Results, IAMCatalogReferenceResolution{
			SubjectType: string(result.SubjectType), ID: strconv.FormatInt(result.ID, 10), Found: result.Found,
			Referenceable: result.Referenceable, Name: result.Name, Code: result.Code, Status: result.Status,
			PrincipalStatus: result.PrincipalStatus, MembershipStatus: result.MembershipStatus,
		})
	}
	c.JSON(http.StatusOK, response)
}

// ListStandardGovernanceUsers godoc
// @Summary      查询 Standard 治理用户候选 | List Standard governance user candidates
// @Description  仅 addp-standard Tenant Service Principal 可按名称或用户名分页查询当前租户内可引用的活动用户 | Only the addp-standard tenant service principal may search referenceable active users in the current tenant by display name or username
// @Tags         Runtime Standard Governance Users
// @Produce      json
// @Security     BearerAuth
// @Param        search query string false "名称或用户名，最多 100 字符 | Name or username, maximum 100 characters"
// @Param        page query int false "页码，默认 1 | Page number, default 1"
// @Param        page_size query int false "每页数量，默认 20，最大 50 | Page size, default 20 and maximum 50"
// @Success      200 {object} object{data=[]IAMCatalogReferenceCandidateResponse,total=int64,page=int,page_size=int,total_pages=int}
// @Failure      400 {object} IAMErrorResponse
// @Failure      401 {object} IAMErrorResponse
// @Failure      403 {object} IAMErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["iam.tenant_membership.read"]
// @Router       /runtime/standard-governance-users/candidates [get]
func (h *IAMCatalogReferenceHandler) ListStandardGovernanceUsers(c *gin.Context) {
	if err := iamServiceOwnsModule(c, "standard"); err != nil {
		respondIAMError(c, err)
		return
	}
	page, pageErr := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, pageSizeErr := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	_, tenantID, _, err := iamTenantActor(c)
	if err != nil {
		respondIAMError(c, err)
		return
	}
	authContext, exists := middleware.IAMAuthContextFromGin(c)
	if !exists || authContext.Client.ClientID == nil || pageErr != nil || pageSizeErr != nil {
		respondIAMError(c, fmt.Errorf("%w: invalid Standard governance user candidate request", commonapi.ErrBadRequest))
		return
	}
	items, total, err := h.service.ListCandidates(c.Request.Context(), int64(tenantID), *authContext.Client.ClientID, iam.CatalogSubjectTypeUser, c.Query("search"), page, pageSize)
	if err != nil {
		respondIAMError(c, err)
		return
	}
	data := make([]IAMCatalogReferenceCandidateResponse, 0, len(items))
	for _, item := range items {
		data = append(data, IAMCatalogReferenceCandidateResponse{SubjectType: string(item.SubjectType), ID: strconv.FormatInt(item.ID, 10), Name: item.Name, Code: item.Code, Status: item.Status})
	}
	commonapi.RespondPaginated(c, data, total, page, pageSize)
}
