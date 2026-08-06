package api

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/system/internal/iam"
	"github.com/addp/system/internal/middleware"
	"github.com/gin-gonic/gin"
)

type IAMTenantRoleResponse struct {
	ID                    string   `json:"id"`
	RoleKey               string   `json:"role_key"`
	Name                  *string  `json:"name"`
	Description           *string  `json:"description"`
	NameI18nKey           *string  `json:"name_i18n_key"`
	DescriptionI18nKey    *string  `json:"description_i18n_key"`
	RoleType              string   `json:"role_type"`
	AllowedScopeTypes     []string `json:"allowed_scope_types"`
	AllowedPrincipalTypes []string `json:"allowed_principal_types"`
	Immutable             bool     `json:"immutable"`
	PermissionKeys        []string `json:"permission_keys"`
}

type IAMTenantAssignablePermissionResponse struct {
	PermissionKey     string   `json:"permission_key"`
	RiskLevel         string   `json:"risk_level"`
	AllowedScopeTypes []string `json:"allowed_scope_types"`
}

type IAMTenantRoleRequest struct {
	RoleKey        string   `json:"role_key"`
	Name           string   `json:"name"`
	Description    string   `json:"description"`
	ScopeTypes     []string `json:"scope_types"`
	PermissionKeys []string `json:"permission_keys"`
}

type IAMDeleteTenantRoleRequest struct {
	Reason string `json:"reason"`
}

type IAMTenantRoleAssignmentResponse struct {
	ID                   string     `json:"id"`
	MembershipID         string     `json:"membership_id"`
	PrincipalID          string     `json:"principal_id"`
	DisplayName          string     `json:"display_name"`
	Username             *string    `json:"username"`
	RoleID               string     `json:"role_id"`
	RoleKey              string     `json:"role_key"`
	RoleName             *string    `json:"role_name"`
	RoleNameI18nKey      *string    `json:"role_name_i18n_key"`
	ScopeType            string     `json:"scope_type"`
	DepartmentID         *string    `json:"department_id"`
	ProjectGroupID       *string    `json:"project_group_id"`
	Status               string     `json:"status"`
	ValidFrom            time.Time  `json:"valid_from"`
	ValidUntil           *time.Time `json:"valid_until"`
	Reason               string     `json:"reason"`
	CreatedByPrincipalID *string    `json:"created_by_principal_id"`
	RevokedByPrincipalID *string    `json:"revoked_by_principal_id"`
	RevokedAt            *time.Time `json:"revoked_at"`
}

type IAMCreateTenantRoleAssignmentRequest struct {
	MembershipID   string     `json:"membership_id"`
	RoleID         string     `json:"role_id"`
	ScopeType      string     `json:"scope_type"`
	DepartmentID   *string    `json:"department_id"`
	ProjectGroupID *string    `json:"project_group_id"`
	ValidUntil     *time.Time `json:"valid_until"`
	Reason         string     `json:"reason"`
}

type IAMRevokeTenantRoleAssignmentRequest struct {
	Reason string `json:"reason"`
}

type iamTenantRoleService interface {
	ListRoles(context.Context, int64) ([]iam.TenantRole, error)
	ListAssignablePermissions(context.Context, int64) ([]iam.TenantAssignablePermission, error)
	CreateRole(context.Context, iam.CreateTenantRoleInput) (*iam.TenantRole, error)
	UpdateRole(context.Context, iam.UpdateTenantRoleInput) (*iam.TenantRole, error)
	DeleteRole(context.Context, iam.DeleteTenantRoleInput) error
	ListAssignments(context.Context, int64, iam.TenantRoleAssignmentFilter, int, int) ([]iam.ManagedTenantRoleAssignment, int64, error)
	CreateAssignment(context.Context, iam.CreateTenantRoleAssignmentInput) (*iam.ManagedTenantRoleAssignment, error)
	RevokeAssignment(context.Context, iam.RevokeTenantRoleAssignmentInput) (*iam.ManagedTenantRoleAssignment, error)
}

type IAMTenantRoleHandler struct{ service iamTenantRoleService }

func NewIAMTenantRoleHandler(service iamTenantRoleService) (*IAMTenantRoleHandler, error) {
	if service == nil {
		return nil, fmt.Errorf("%w: tenant role service is required", commonapi.ErrBadRequest)
	}
	return &IAMTenantRoleHandler{service: service}, nil
}

// ListRoles godoc
// @Summary      查询当前租户可分配角色 | List assignable roles in the current tenant
// @Tags         租户角色 | Tenant Roles
// @Produce      json
// @Security     BearerAuth
// @Success      200 {array} IAMTenantRoleResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["iam.tenant_role.read"]
// @Router       /tenant/roles [get]
func (h *IAMTenantRoleHandler) ListRoles(c *gin.Context) {
	_, tenantID, err := iamTenantUserActor(c)
	if err != nil {
		respondIAMError(c, err)
		return
	}
	roles, err := h.service.ListRoles(c.Request.Context(), int64(tenantID))
	if err != nil {
		respondIAMError(c, err)
		return
	}
	responses := make([]IAMTenantRoleResponse, 0, len(roles))
	for _, role := range roles {
		responses = append(responses, mapIAMTenantRole(role))
	}
	c.JSON(http.StatusOK, responses)
}

// ListAssignablePermissions godoc
// @Summary      查询租户自定义角色可选权限 | List permissions available to tenant custom roles
// @Tags         租户角色 | Tenant Roles
// @Produce      json
// @Security     BearerAuth
// @Success      200 {array} IAMTenantAssignablePermissionResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["iam.tenant_role.read"]
// @Router       /tenant/role_permissions [get]
func (h *IAMTenantRoleHandler) ListAssignablePermissions(c *gin.Context) {
	_, tenantID, err := iamTenantUserActor(c)
	if err != nil {
		respondIAMError(c, err)
		return
	}
	permissions, err := h.service.ListAssignablePermissions(c.Request.Context(), int64(tenantID))
	if err != nil {
		respondIAMError(c, err)
		return
	}
	responses := make([]IAMTenantAssignablePermissionResponse, 0, len(permissions))
	for _, permission := range permissions {
		responses = append(responses, IAMTenantAssignablePermissionResponse{PermissionKey: permission.PermissionKey, RiskLevel: permission.RiskLevel, AllowedScopeTypes: []string(permission.AllowedScopeTypes)})
	}
	c.JSON(http.StatusOK, responses)
}

// CreateRole godoc
// @Summary      创建租户自定义角色 | Create tenant custom role
// @Tags         租户角色 | Tenant Roles
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        request body IAMTenantRoleRequest true "角色定义 | Role definition"
// @Success      201 {object} IAMTenantRoleResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["iam.tenant_role.create"]
// @Router       /tenant/roles [post]
func (h *IAMTenantRoleHandler) CreateRole(c *gin.Context) {
	actorID, tenantID, err := iamTenantUserActor(c)
	if err != nil {
		respondIAMError(c, err)
		return
	}
	var request IAMTenantRoleRequest
	if err := commonapi.BindOptionalJSONStrict(c, &request); err != nil {
		respondIAMError(c, fmt.Errorf("%w: invalid tenant role request", commonapi.ErrBadRequest))
		return
	}
	role, err := h.service.CreateRole(c.Request.Context(), iam.CreateTenantRoleInput{TenantID: int64(tenantID), RoleKey: request.RoleKey, Name: request.Name, Description: request.Description, ScopeTypes: request.ScopeTypes, PermissionKeys: request.PermissionKeys, ActorPrincipalID: int64(actorID), Audit: iamAuditMetadataWithStatus(c, http.StatusCreated)})
	if err != nil {
		respondIAMError(c, err)
		return
	}
	c.JSON(http.StatusCreated, mapIAMTenantRole(*role))
}

// UpdateRole godoc
// @Summary      更新租户自定义角色 | Update tenant custom role
// @Tags         租户角色 | Tenant Roles
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id path string true "角色 ID | Role ID"
// @Param        request body IAMTenantRoleRequest true "角色定义 | Role definition"
// @Success      200 {object} IAMTenantRoleResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["iam.tenant_role.update"]
// @Router       /tenant/roles/{id} [put]
func (h *IAMTenantRoleHandler) UpdateRole(c *gin.Context) {
	actorID, tenantID, err := iamTenantUserActor(c)
	if err != nil {
		respondIAMError(c, err)
		return
	}
	roleID, err := parseIAMDecimalID(c.Param("id"))
	if err != nil {
		respondIAMError(c, err)
		return
	}
	var request IAMTenantRoleRequest
	if err := commonapi.BindOptionalJSONStrict(c, &request); err != nil {
		respondIAMError(c, fmt.Errorf("%w: invalid tenant role request", commonapi.ErrBadRequest))
		return
	}
	role, err := h.service.UpdateRole(c.Request.Context(), iam.UpdateTenantRoleInput{TenantID: int64(tenantID), RoleID: roleID, Name: request.Name, Description: request.Description, ScopeTypes: request.ScopeTypes, PermissionKeys: request.PermissionKeys, ActorPrincipalID: int64(actorID), Audit: iamAuditMetadataWithStatus(c, http.StatusOK)})
	if err != nil {
		respondIAMError(c, err)
		return
	}
	c.JSON(http.StatusOK, mapIAMTenantRole(*role))
}

// DeleteRole godoc
// @Summary      停用租户自定义角色 | Disable tenant custom role
// @Tags         租户角色 | Tenant Roles
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id path string true "角色 ID | Role ID"
// @Param        request body IAMDeleteTenantRoleRequest true "停用原因 | Disable reason"
// @Success      204
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["iam.tenant_role.delete"]
// @Router       /tenant/roles/{id} [delete]
func (h *IAMTenantRoleHandler) DeleteRole(c *gin.Context) {
	actorID, tenantID, err := iamTenantUserActor(c)
	if err != nil {
		respondIAMError(c, err)
		return
	}
	roleID, err := parseIAMDecimalID(c.Param("id"))
	if err != nil {
		respondIAMError(c, err)
		return
	}
	var request IAMDeleteTenantRoleRequest
	if err := commonapi.BindOptionalJSONStrict(c, &request); err != nil {
		respondIAMError(c, fmt.Errorf("%w: invalid tenant role deletion request", commonapi.ErrBadRequest))
		return
	}
	if err := h.service.DeleteRole(c.Request.Context(), iam.DeleteTenantRoleInput{TenantID: int64(tenantID), RoleID: roleID, Reason: request.Reason, ActorPrincipalID: int64(actorID), Audit: iamAuditMetadataWithStatus(c, http.StatusNoContent)}); err != nil {
		respondIAMError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// ListAssignments godoc
// @Summary      查询当前租户角色分配 | List role assignments in the current tenant
// @Tags         租户角色分配 | Tenant Role Assignments
// @Produce      json
// @Security     BearerAuth
// @Param        page query int false "页码 | Page number"
// @Param        page_size query int false "每页数量 | Page size"
// @Param        membership_id query string false "成员关系 ID | Membership ID"
// @Param        status query string false "状态：active 或 revoked | Status: active or revoked"
// @Param        scope_type query string false "授权范围类型 | Assignment scope type"
// @Param        department_id query string false "部门 ID，仅 department 范围 | Department ID for department scope"
// @Param        project_group_id query string false "项目组 ID，仅 project_group 范围 | Project group ID for project_group scope"
// @Success      200 {object} object{data=[]IAMTenantRoleAssignmentResponse,total=int64,page=int,page_size=int,total_pages=int}
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["iam.tenant_role_assignment.read"]
// @Router       /tenant/role_assignments [get]
func (h *IAMTenantRoleHandler) ListAssignments(c *gin.Context) {
	_, tenantID, err := iamTenantUserActor(c)
	if err != nil {
		respondIAMError(c, err)
		return
	}
	page, pageSize := commonapi.ParsePagination(c)
	filter, err := parseIAMTenantRoleAssignmentFilter(c)
	if err != nil {
		respondIAMError(c, err)
		return
	}
	assignments, total, err := h.service.ListAssignments(c.Request.Context(), int64(tenantID), filter, page, pageSize)
	if err != nil {
		respondIAMError(c, err)
		return
	}
	responses := make([]IAMTenantRoleAssignmentResponse, 0, len(assignments))
	for _, assignment := range assignments {
		responses = append(responses, mapIAMManagedTenantRoleAssignment(assignment))
	}
	commonapi.RespondPaginated(c, responses, total, page, pageSize)
}

func parseIAMTenantRoleAssignmentFilter(c *gin.Context) (iam.TenantRoleAssignmentFilter, error) {
	var filter iam.TenantRoleAssignmentFilter
	var err error
	if value := strings.TrimSpace(c.Query("membership_id")); value != "" {
		parsed, parseErr := parseIAMDecimalID(value)
		if parseErr != nil {
			return filter, fmt.Errorf("%w: invalid membership_id", commonapi.ErrBadRequest)
		}
		filter.MembershipID = &parsed
	}
	if value := strings.TrimSpace(c.Query("status")); value != "" {
		filter.Status = &value
	}
	if value := strings.TrimSpace(c.Query("scope_type")); value != "" {
		filter.ScopeType = &value
	}
	filter.DepartmentID, err = parseOptionalIAMQueryID(c, "department_id")
	if err != nil {
		return filter, err
	}
	filter.ProjectGroupID, err = parseOptionalIAMQueryID(c, "project_group_id")
	if err != nil {
		return filter, err
	}
	return filter, nil
}

func parseOptionalIAMQueryID(c *gin.Context, name string) (*int64, error) {
	value := strings.TrimSpace(c.Query(name))
	if value == "" {
		return nil, nil
	}
	parsed, err := parseIAMDecimalID(value)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid %s", commonapi.ErrBadRequest, name)
	}
	return &parsed, nil
}

// CreateAssignment godoc
// @Summary      创建当前租户角色分配 | Create role assignment in the current tenant
// @Tags         租户角色分配 | Tenant Role Assignments
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        request body IAMCreateTenantRoleAssignmentRequest true "角色分配 | Role assignment"
// @Success      201 {object} IAMTenantRoleAssignmentResponse
// @Failure      409 {object} IAMErrorResponse "角色分配冲突：重复分配或主体类型不兼容 | Role assignment conflict: duplicate assignment or incompatible principal type"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["iam.tenant_role_assignment.create"]
// @Router       /tenant/role_assignments [post]
func (h *IAMTenantRoleHandler) CreateAssignment(c *gin.Context) {
	actorID, tenantID, err := iamTenantUserActor(c)
	if err != nil {
		respondIAMError(c, err)
		return
	}
	var request IAMCreateTenantRoleAssignmentRequest
	if err := commonapi.BindOptionalJSONStrict(c, &request); err != nil {
		respondIAMError(c, fmt.Errorf("%w: invalid role assignment request", commonapi.ErrBadRequest))
		return
	}
	membershipID, err := parseIAMDecimalID(request.MembershipID)
	if err != nil {
		respondIAMError(c, fmt.Errorf("%w: membership is required", commonapi.ErrBadRequest))
		return
	}
	roleID, err := parseIAMDecimalID(request.RoleID)
	if err != nil {
		respondIAMError(c, fmt.Errorf("%w: role is required", commonapi.ErrBadRequest))
		return
	}
	departmentID, err := parseOptionalIAMDecimalID(request.DepartmentID)
	if err != nil {
		respondIAMError(c, err)
		return
	}
	projectGroupID, err := parseOptionalIAMDecimalID(request.ProjectGroupID)
	if err != nil {
		respondIAMError(c, err)
		return
	}
	authContext, exists := middleware.IAMAuthContextFromGin(c)
	if !exists {
		respondIAMError(c, commonapi.ErrUnauthorized)
		return
	}
	assignment, err := h.service.CreateAssignment(c.Request.Context(), iam.CreateTenantRoleAssignmentInput{TenantID: int64(tenantID), MembershipID: membershipID, RoleID: roleID, ScopeType: request.ScopeType, DepartmentID: departmentID, ProjectGroupID: projectGroupID, ValidUntil: request.ValidUntil, Reason: request.Reason, ActorPrincipalID: int64(actorID), AssuranceLevel: iam.AssuranceLevel(authContext.Authentication.AssuranceLevel), StepUpExpiresAt: authContext.Authentication.StepUpExpiresAt, Audit: iamAuditMetadataWithStatus(c, http.StatusCreated)})
	if err != nil {
		respondIAMError(c, err)
		return
	}
	c.JSON(http.StatusCreated, mapIAMManagedTenantRoleAssignment(*assignment))
}

// RevokeAssignment godoc
// @Summary      撤销当前租户角色分配 | Revoke role assignment in the current tenant
// @Tags         租户角色分配 | Tenant Role Assignments
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id path string true "角色分配 ID | Role assignment ID"
// @Param        request body IAMRevokeTenantRoleAssignmentRequest true "撤销原因 | Revocation reason"
// @Success      200 {object} IAMTenantRoleAssignmentResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["iam.tenant_role_assignment.revoke"]
// @Router       /tenant/role_assignments/{id}/revoke [post]
func (h *IAMTenantRoleHandler) RevokeAssignment(c *gin.Context) {
	actorID, tenantID, err := iamTenantUserActor(c)
	if err != nil {
		respondIAMError(c, err)
		return
	}
	assignmentID, err := parseIAMDecimalID(c.Param("id"))
	if err != nil {
		respondIAMError(c, err)
		return
	}
	var request IAMRevokeTenantRoleAssignmentRequest
	if err := commonapi.BindOptionalJSONStrict(c, &request); err != nil || strings.TrimSpace(request.Reason) == "" {
		respondIAMError(c, fmt.Errorf("%w: revocation reason is required", commonapi.ErrBadRequest))
		return
	}
	assignment, err := h.service.RevokeAssignment(c.Request.Context(), iam.RevokeTenantRoleAssignmentInput{TenantID: int64(tenantID), AssignmentID: assignmentID, Reason: request.Reason, ActorPrincipalID: int64(actorID), Audit: iamAuditMetadataWithStatus(c, http.StatusOK)})
	if err != nil {
		respondIAMError(c, err)
		return
	}
	c.JSON(http.StatusOK, mapIAMManagedTenantRoleAssignment(*assignment))
}

func mapIAMTenantRole(role iam.TenantRole) IAMTenantRoleResponse {
	return IAMTenantRoleResponse{ID: strconv.FormatInt(role.ID, 10), RoleKey: role.RoleKey, Name: role.Name, Description: role.Description, NameI18nKey: role.NameI18nKey, DescriptionI18nKey: role.DescriptionI18nKey, RoleType: role.RoleType, AllowedScopeTypes: []string(role.AllowedScopeTypes), AllowedPrincipalTypes: []string(role.AllowedPrincipalTypes), Immutable: role.Immutable, PermissionKeys: []string(role.PermissionKeys)}
}

func mapIAMManagedTenantRoleAssignment(assignment iam.ManagedTenantRoleAssignment) IAMTenantRoleAssignmentResponse {
	response := mapIAMRoleAssignment(assignment.RoleAssignment)
	response.MembershipID = strconv.FormatInt(assignment.MembershipID, 10)
	response.DisplayName = assignment.DisplayName
	response.Username = assignment.Username
	response.RoleKey = assignment.RoleKey
	response.RoleName = assignment.RoleName
	response.RoleNameI18nKey = assignment.RoleNameI18nKey
	return response
}

func mapIAMRoleAssignment(assignment iam.RoleAssignment) IAMTenantRoleAssignmentResponse {
	response := IAMTenantRoleAssignmentResponse{ID: strconv.FormatInt(assignment.ID, 10), PrincipalID: strconv.FormatInt(assignment.PrincipalID, 10), RoleID: strconv.FormatInt(assignment.RoleID, 10), ScopeType: assignment.ScopeType, Status: assignment.Status, ValidFrom: assignment.ValidFrom.UTC(), ValidUntil: utcTimePointer(assignment.ValidUntil), Reason: assignment.Reason, RevokedAt: utcTimePointer(assignment.RevokedAt)}
	response.DepartmentID = formatOptionalIAMID(assignment.DepartmentID)
	response.ProjectGroupID = formatOptionalIAMID(assignment.ProjectGroupID)
	response.CreatedByPrincipalID = formatOptionalIAMID(assignment.CreatedByPrincipalID)
	response.RevokedByPrincipalID = formatOptionalIAMID(assignment.RevokedByPrincipalID)
	return response
}

func parseOptionalIAMDecimalID(value *string) (*int64, error) {
	if value == nil {
		return nil, nil
	}
	parsed, err := parseIAMDecimalID(*value)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}
func formatOptionalIAMID(value *int64) *string {
	if value == nil {
		return nil
	}
	formatted := strconv.FormatInt(*value, 10)
	return &formatted
}
