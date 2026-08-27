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
	SubjectType string `json:"subject_type" enums:"department,user"`
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
// @Summary      精确批量解析 Catalog 责任主体 | Resolve Catalog responsibility subjects in batch
// @Description  仅 addp-catalog Tenant Service Principal 可按当前 Tenant 解析 Department 与 User；User ID 是全局稳定身份，但必须有当前 Tenant Membership；跨 Tenant 与不存在统一返回 found=false | Only the addp-catalog tenant service principal may resolve departments and users in the current tenant; a user ID is globally stable but requires a current tenant membership; cross-tenant and missing subjects both return found=false
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
// @x-addp-required-permissions ["iam.department.read","iam.tenant_membership.read"]
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
		if err != nil || (reference.SubjectType != string(iam.CatalogSubjectTypeDepartment) && reference.SubjectType != string(iam.CatalogSubjectTypeUser)) {
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
