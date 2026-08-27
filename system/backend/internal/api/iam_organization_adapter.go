package api

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/system/internal/iam"
	"github.com/gin-gonic/gin"
)

type IAMDepartmentResponse struct {
	ID        string               `json:"id"`
	ParentID  *string              `json:"parent_id"`
	Code      string               `json:"code"`
	Name      string               `json:"name"`
	Status    iam.DepartmentStatus `json:"status"`
	Version   int64                `json:"version"`
	CreatedAt time.Time            `json:"created_at"`
	UpdatedAt time.Time            `json:"updated_at"`
}

type IAMCreateDepartmentRequest struct {
	ParentID *string `json:"parent_id"`
	Code     string  `json:"code"`
	Name     string  `json:"name"`
}

type IAMUpdateDepartmentRequest struct {
	ParentID *string `json:"parent_id"`
	Name     string  `json:"name"`
	Version  int64   `json:"version"`
}

type IAMVersionedLifecycleRequest struct {
	Version int64  `json:"version"`
	Reason  string `json:"reason"`
}

type IAMOrganizationMembershipResponse struct {
	ID                 string                           `json:"id"`
	TenantMembershipID string                           `json:"tenant_membership_id"`
	PrincipalID        string                           `json:"principal_id"`
	DisplayName        string                           `json:"display_name"`
	Username           *string                          `json:"username"`
	MembershipType     *iam.DepartmentMembershipType    `json:"membership_type,omitempty"`
	DepartmentRole     *iam.DepartmentRelationRole      `json:"department_role,omitempty"`
	ProjectGroupRole   *iam.ProjectGroupRelationRole    `json:"project_group_role,omitempty"`
	Status             iam.OrganizationMembershipStatus `json:"status"`
	EndedAt            *time.Time                       `json:"ended_at"`
	Version            int64                            `json:"version"`
	CreatedAt          time.Time                        `json:"created_at"`
	UpdatedAt          time.Time                        `json:"updated_at"`
}

type IAMCreateDepartmentMembershipRequest struct {
	TenantMembershipID string                       `json:"tenant_membership_id"`
	MembershipType     iam.DepartmentMembershipType `json:"membership_type"`
	RelationRole       iam.DepartmentRelationRole   `json:"relation_role"`
}

type IAMUpdateDepartmentMembershipRequest struct {
	MembershipType iam.DepartmentMembershipType `json:"membership_type"`
	RelationRole   iam.DepartmentRelationRole   `json:"relation_role"`
	Version        int64                        `json:"version"`
}

type IAMProjectGroupResponse struct {
	ID          string                 `json:"id"`
	Code        string                 `json:"code"`
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Status      iam.ProjectGroupStatus `json:"status"`
	StartsAt    *time.Time             `json:"starts_at"`
	EndsAt      *time.Time             `json:"ends_at"`
	Version     int64                  `json:"version"`
	CreatedAt   time.Time              `json:"created_at"`
	UpdatedAt   time.Time              `json:"updated_at"`
}

type IAMCreateProjectGroupRequest struct {
	Code        string                 `json:"code"`
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Status      iam.ProjectGroupStatus `json:"status"`
	StartsAt    *time.Time             `json:"starts_at"`
	EndsAt      *time.Time             `json:"ends_at"`
}

type IAMUpdateProjectGroupRequest struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Status      iam.ProjectGroupStatus `json:"status"`
	StartsAt    *time.Time             `json:"starts_at"`
	EndsAt      *time.Time             `json:"ends_at"`
	Version     int64                  `json:"version"`
}

type IAMCreateProjectGroupMembershipRequest struct {
	TenantMembershipID string                       `json:"tenant_membership_id"`
	RelationRole       iam.ProjectGroupRelationRole `json:"relation_role"`
}

type IAMUpdateProjectGroupMembershipRequest struct {
	RelationRole iam.ProjectGroupRelationRole `json:"relation_role"`
	Version      int64                        `json:"version"`
}

type IAMOrganizationHandler struct {
	service *iam.OrganizationService
}

func NewIAMOrganizationHandler(service *iam.OrganizationService) (*IAMOrganizationHandler, error) {
	if service == nil {
		return nil, commonapi.ErrBadRequest
	}
	return &IAMOrganizationHandler{service: service}, nil
}

// ListDepartments godoc
// @Summary 查询部门 | List departments
// @Description 分页查询当前 Tenant 的稳定组织单元 | List stable organization units in the current tenant
// @Tags 租户组织 | Tenant Organization
// @Produce json
// @Security BearerAuth
// @Param page query int false "页码 | Page number"
// @Param page_size query int false "每页数量 | Page size"
// @Param search query string false "编码或名称 | Code or name"
// @Param status query string false "状态：active/disabled | Status: active/disabled"
// @Success 200 {object} object{data=[]IAMDepartmentResponse,total=int64,page=int,page_size=int,total_pages=int}
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["iam.department.read"]
// @Router /tenant/departments [get]
func (h *IAMOrganizationHandler) ListDepartments(c *gin.Context) {
	_, tenantID, ok := organizationActor(c)
	if !ok || rejectTenantIDQuery(c) {
		return
	}
	status, err := parseDepartmentStatus(c.Query("status"))
	if err != nil {
		respondIAMError(c, err)
		return
	}
	page, pageSize := commonapi.ParsePagination(c)
	departments, total, err := h.service.ListDepartments(c.Request.Context(), int64(tenantID), page, pageSize, c.Query("search"), status)
	if err != nil {
		respondIAMError(c, err)
		return
	}
	data := make([]IAMDepartmentResponse, 0, len(departments))
	for _, department := range departments {
		data = append(data, mapIAMDepartment(department))
	}
	commonapi.RespondPaginated(c, data, total, page, pageSize)
}

// CreateDepartment godoc
// @Summary 创建部门 | Create department
// @Tags 租户组织 | Tenant Organization
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body IAMCreateDepartmentRequest true "部门定义 | Department definition"
// @Success 201 {object} IAMDepartmentResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["iam.department.create"]
// @Router /tenant/departments [post]
func (h *IAMOrganizationHandler) CreateDepartment(c *gin.Context) {
	actorID, tenantID, ok := organizationActor(c)
	if !ok {
		return
	}
	var request IAMCreateDepartmentRequest
	if err := commonapi.BindOptionalJSONStrict(c, &request); err != nil {
		respondIAMError(c, commonapi.ErrBadRequest)
		return
	}
	parentID, err := parseOptionalIAMID(request.ParentID)
	if err != nil {
		respondIAMError(c, err)
		return
	}
	department, err := h.service.CreateDepartment(c.Request.Context(), iam.CreateDepartmentInput{
		TenantID: int64(tenantID), ActorPrincipalID: int64(actorID), ParentID: parentID,
		Code: request.Code, Name: request.Name, Audit: iamAuditMetadataWithStatus(c, http.StatusCreated),
	})
	if err != nil {
		respondIAMError(c, err)
		return
	}
	c.JSON(http.StatusCreated, mapIAMDepartment(*department))
}

// GetDepartment godoc
// @Summary 查询部门详情 | Get department
// @Tags 租户组织 | Tenant Organization
// @Produce json
// @Security BearerAuth
// @Param id path string true "部门 ID | Department ID"
// @Success 200 {object} IAMDepartmentResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["iam.department.read"]
// @Router /tenant/departments/{id} [get]
func (h *IAMOrganizationHandler) GetDepartment(c *gin.Context) {
	_, tenantID, ok := organizationActor(c)
	if !ok {
		return
	}
	departmentID, err := parseIAMDecimalID(c.Param("id"))
	if err != nil {
		respondIAMError(c, err)
		return
	}
	department, err := h.service.GetDepartment(c.Request.Context(), int64(tenantID), departmentID)
	if err != nil {
		respondIAMError(c, err)
		return
	}
	c.JSON(http.StatusOK, mapIAMDepartment(*department))
}

// UpdateDepartment godoc
// @Summary 更新部门 | Update department
// @Tags 租户组织 | Tenant Organization
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "部门 ID | Department ID"
// @Param request body IAMUpdateDepartmentRequest true "完整可编辑聚合与版本 | Full editable aggregate and version"
// @Success 200 {object} IAMDepartmentResponse
// @Failure 409 {object} IAMErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["iam.department.update"]
// @Router /tenant/departments/{id} [put]
func (h *IAMOrganizationHandler) UpdateDepartment(c *gin.Context) {
	actorID, tenantID, ok := organizationActor(c)
	if !ok {
		return
	}
	departmentID, err := parseIAMDecimalID(c.Param("id"))
	if err != nil {
		respondIAMError(c, err)
		return
	}
	var request IAMUpdateDepartmentRequest
	if err := commonapi.BindOptionalJSONStrict(c, &request); err != nil {
		respondIAMError(c, commonapi.ErrBadRequest)
		return
	}
	parentID, err := parseOptionalIAMID(request.ParentID)
	if err != nil {
		respondIAMError(c, err)
		return
	}
	department, err := h.service.UpdateDepartment(c.Request.Context(), iam.UpdateDepartmentInput{
		TenantID: int64(tenantID), DepartmentID: departmentID, Version: request.Version,
		ActorPrincipalID: int64(actorID), ParentID: parentID, Name: request.Name,
		Audit: iamAuditMetadataWithStatus(c, http.StatusOK),
	})
	if err != nil {
		respondIAMError(c, err)
		return
	}
	c.JSON(http.StatusOK, mapIAMDepartment(*department))
}

// DisableDepartment godoc
// @Summary 停用部门 | Disable department
// @Tags 租户组织 | Tenant Organization
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "部门 ID | Department ID"
// @Param request body IAMVersionedLifecycleRequest true "版本与原因 | Version and reason"
// @Success 200 {object} IAMDepartmentResponse
// @Failure 409 {object} IAMErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["iam.department.update"]
// @Router /tenant/departments/{id}/disable [post]
func (h *IAMOrganizationHandler) DisableDepartment(c *gin.Context) {
	h.changeDepartmentStatus(c, h.service.DisableDepartment)
}

// RestoreDepartment godoc
// @Summary 恢复部门 | Restore department
// @Tags 租户组织 | Tenant Organization
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "部门 ID | Department ID"
// @Param request body IAMVersionedLifecycleRequest true "版本与原因 | Version and reason"
// @Success 200 {object} IAMDepartmentResponse
// @Failure 409 {object} IAMErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["iam.department.restore"]
// @Router /tenant/departments/{id}/restore [post]
func (h *IAMOrganizationHandler) RestoreDepartment(c *gin.Context) {
	h.changeDepartmentStatus(c, h.service.RestoreDepartment)
}

func (h *IAMOrganizationHandler) changeDepartmentStatus(c *gin.Context, change func(context.Context, iam.ChangeDepartmentStatusInput) (*iam.Department, error)) {
	actorID, tenantID, ok := organizationActor(c)
	if !ok {
		return
	}
	departmentID, err := parseIAMDecimalID(c.Param("id"))
	if err != nil {
		respondIAMError(c, err)
		return
	}
	request, ok := bindVersionedLifecycle(c)
	if !ok {
		return
	}
	department, err := change(c.Request.Context(), iam.ChangeDepartmentStatusInput{TenantID: int64(tenantID), DepartmentID: departmentID, Version: request.Version, ActorPrincipalID: int64(actorID), Reason: request.Reason, Audit: iamAuditMetadataWithStatus(c, http.StatusOK)})
	if err != nil {
		respondIAMError(c, err)
		return
	}
	c.JSON(http.StatusOK, mapIAMDepartment(*department))
}

// ListDepartmentMemberships godoc
// @Summary 查询部门成员 | List department memberships
// @Tags 租户组织 | Tenant Organization
// @Produce json
// @Security BearerAuth
// @Param id path string true "部门 ID | Department ID"
// @Param page query int false "页码 | Page number"
// @Param page_size query int false "每页数量 | Page size"
// @Param status query string false "状态：active/ended | Status: active/ended"
// @Success 200 {object} object{data=[]IAMOrganizationMembershipResponse,total=int64,page=int,page_size=int,total_pages=int}
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["iam.department_membership.read"]
// @Router /tenant/departments/{id}/memberships [get]
func (h *IAMOrganizationHandler) ListDepartmentMemberships(c *gin.Context) {
	_, tenantID, ok := organizationActor(c)
	if !ok {
		return
	}
	departmentID, status, ok := organizationMembershipListParams(c)
	if !ok {
		return
	}
	page, pageSize := commonapi.ParsePagination(c)
	memberships, total, err := h.service.ListDepartmentMemberships(c.Request.Context(), int64(tenantID), departmentID, page, pageSize, status)
	if err != nil {
		respondIAMError(c, err)
		return
	}
	commonapi.RespondPaginated(c, mapIAMOrganizationMemberships(memberships), total, page, pageSize)
}

// CreateDepartmentMembership godoc
// @Summary 新增部门成员 | Create department membership
// @Tags 租户组织 | Tenant Organization
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "部门 ID | Department ID"
// @Param request body IAMCreateDepartmentMembershipRequest true "成员关系 | Membership"
// @Success 201 {object} IAMOrganizationMembershipResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["iam.department_membership.create"]
// @Router /tenant/departments/{id}/memberships [post]
func (h *IAMOrganizationHandler) CreateDepartmentMembership(c *gin.Context) {
	actorID, tenantID, ok := organizationActor(c)
	if !ok {
		return
	}
	departmentID, err := parseIAMDecimalID(c.Param("id"))
	if err != nil {
		respondIAMError(c, err)
		return
	}
	var request IAMCreateDepartmentMembershipRequest
	if err := commonapi.BindOptionalJSONStrict(c, &request); err != nil {
		respondIAMError(c, commonapi.ErrBadRequest)
		return
	}
	tenantMembershipID, err := parseIAMDecimalID(request.TenantMembershipID)
	if err != nil {
		respondIAMError(c, err)
		return
	}
	membership, err := h.service.CreateDepartmentMembership(c.Request.Context(), iam.CreateDepartmentMembershipInput{
		TenantID: int64(tenantID), DepartmentID: departmentID, TenantMembershipID: tenantMembershipID,
		ActorPrincipalID: int64(actorID), MembershipType: request.MembershipType, RelationRole: request.RelationRole,
		Audit: iamAuditMetadataWithStatus(c, http.StatusCreated),
	})
	if err != nil {
		respondIAMError(c, err)
		return
	}
	c.JSON(http.StatusCreated, mapIAMOrganizationMembership(*membership))
}

// UpdateDepartmentMembership godoc
// @Summary 更新部门成员关系 | Update department membership
// @Tags 租户组织 | Tenant Organization
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "部门 ID | Department ID"
// @Param membership_id path string true "成员关系 ID | Membership ID"
// @Param request body IAMUpdateDepartmentMembershipRequest true "关系与版本 | Relationship and version"
// @Success 200 {object} IAMOrganizationMembershipResponse
// @Failure 409 {object} IAMErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["iam.department_membership.update"]
// @Router /tenant/departments/{id}/memberships/{membership_id} [put]
func (h *IAMOrganizationHandler) UpdateDepartmentMembership(c *gin.Context) {
	actorID, tenantID, organizationID, membershipID, ok := organizationMembershipActorAndIDs(c)
	if !ok {
		return
	}
	var request IAMUpdateDepartmentMembershipRequest
	if err := commonapi.BindOptionalJSONStrict(c, &request); err != nil {
		respondIAMError(c, commonapi.ErrBadRequest)
		return
	}
	membership, err := h.service.UpdateDepartmentMembership(c.Request.Context(), iam.UpdateDepartmentMembershipInput{
		TenantID: int64(tenantID), DepartmentID: organizationID, MembershipID: membershipID,
		Version: request.Version, ActorPrincipalID: int64(actorID), MembershipType: request.MembershipType,
		RelationRole: request.RelationRole, Audit: iamAuditMetadataWithStatus(c, http.StatusOK),
	})
	if err != nil {
		respondIAMError(c, err)
		return
	}
	c.JSON(http.StatusOK, mapIAMOrganizationMembership(*membership))
}

// CloseDepartmentMembership godoc
// @Summary 结束部门成员关系 | Close department membership
// @Tags 租户组织 | Tenant Organization
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "部门 ID | Department ID"
// @Param membership_id path string true "成员关系 ID | Membership ID"
// @Param request body IAMVersionedLifecycleRequest true "版本与原因 | Version and reason"
// @Success 200 {object} IAMOrganizationMembershipResponse
// @Failure 409 {object} IAMErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["iam.department_membership.close"]
// @Router /tenant/departments/{id}/memberships/{membership_id}/close [post]
func (h *IAMOrganizationHandler) CloseDepartmentMembership(c *gin.Context) {
	actorID, tenantID, organizationID, membershipID, ok := organizationMembershipActorAndIDs(c)
	if !ok {
		return
	}
	request, ok := bindVersionedLifecycle(c)
	if !ok {
		return
	}
	membership, err := h.service.CloseDepartmentMembership(c.Request.Context(), iam.CloseOrganizationMembershipInput{
		TenantID: int64(tenantID), OrganizationID: organizationID, MembershipID: membershipID,
		Version: request.Version, ActorPrincipalID: int64(actorID), Reason: request.Reason,
		Audit: iamAuditMetadataWithStatus(c, http.StatusOK),
	})
	if err != nil {
		respondIAMError(c, err)
		return
	}
	c.JSON(http.StatusOK, mapIAMOrganizationMembership(*membership))
}

// ListProjectGroups godoc
// @Summary 查询项目组 | List project groups
// @Description 分页查询当前 Tenant 的跨部门协作集合 | List cross-department collaboration groups in the current tenant
// @Tags 租户组织 | Tenant Organization
// @Produce json
// @Security BearerAuth
// @Param page query int false "页码 | Page number"
// @Param page_size query int false "每页数量 | Page size"
// @Param search query string false "编码或名称 | Code or name"
// @Param status query string false "状态：planned/active/closed | Status: planned/active/closed"
// @Success 200 {object} object{data=[]IAMProjectGroupResponse,total=int64,page=int,page_size=int,total_pages=int}
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["iam.project_group.read"]
// @Router /tenant/project_groups [get]
func (h *IAMOrganizationHandler) ListProjectGroups(c *gin.Context) {
	_, tenantID, ok := organizationActor(c)
	if !ok || rejectTenantIDQuery(c) {
		return
	}
	status, err := parseProjectGroupStatus(c.Query("status"))
	if err != nil {
		respondIAMError(c, err)
		return
	}
	page, pageSize := commonapi.ParsePagination(c)
	groups, total, err := h.service.ListProjectGroups(c.Request.Context(), int64(tenantID), page, pageSize, c.Query("search"), status)
	if err != nil {
		respondIAMError(c, err)
		return
	}
	data := make([]IAMProjectGroupResponse, 0, len(groups))
	for _, group := range groups {
		data = append(data, mapIAMProjectGroup(group))
	}
	commonapi.RespondPaginated(c, data, total, page, pageSize)
}

// CreateProjectGroup godoc
// @Summary 创建项目组 | Create project group
// @Tags 租户组织 | Tenant Organization
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body IAMCreateProjectGroupRequest true "项目组定义 | Project group definition"
// @Success 201 {object} IAMProjectGroupResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["iam.project_group.create"]
// @Router /tenant/project_groups [post]
func (h *IAMOrganizationHandler) CreateProjectGroup(c *gin.Context) {
	actorID, tenantID, ok := organizationActor(c)
	if !ok {
		return
	}
	var request IAMCreateProjectGroupRequest
	if err := commonapi.BindOptionalJSONStrict(c, &request); err != nil {
		respondIAMError(c, commonapi.ErrBadRequest)
		return
	}
	group, err := h.service.CreateProjectGroup(c.Request.Context(), iam.CreateProjectGroupInput{
		TenantID: int64(tenantID), ActorPrincipalID: int64(actorID), Code: request.Code, Name: request.Name,
		Description: request.Description, Status: request.Status, StartsAt: request.StartsAt, EndsAt: request.EndsAt,
		Audit: iamAuditMetadataWithStatus(c, http.StatusCreated),
	})
	if err != nil {
		respondIAMError(c, err)
		return
	}
	c.JSON(http.StatusCreated, mapIAMProjectGroup(*group))
}

// GetProjectGroup godoc
// @Summary 查询项目组详情 | Get project group
// @Tags 租户组织 | Tenant Organization
// @Produce json
// @Security BearerAuth
// @Param id path string true "项目组 ID | Project group ID"
// @Success 200 {object} IAMProjectGroupResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["iam.project_group.read"]
// @Router /tenant/project_groups/{id} [get]
func (h *IAMOrganizationHandler) GetProjectGroup(c *gin.Context) {
	_, tenantID, ok := organizationActor(c)
	if !ok {
		return
	}
	groupID, err := parseIAMDecimalID(c.Param("id"))
	if err != nil {
		respondIAMError(c, err)
		return
	}
	group, err := h.service.GetProjectGroup(c.Request.Context(), int64(tenantID), groupID)
	if err != nil {
		respondIAMError(c, err)
		return
	}
	c.JSON(http.StatusOK, mapIAMProjectGroup(*group))
}

// UpdateProjectGroup godoc
// @Summary 更新项目组 | Update project group
// @Tags 租户组织 | Tenant Organization
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "项目组 ID | Project group ID"
// @Param request body IAMUpdateProjectGroupRequest true "完整可编辑聚合与版本 | Full editable aggregate and version"
// @Success 200 {object} IAMProjectGroupResponse
// @Failure 409 {object} IAMErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["iam.project_group.update"]
// @Router /tenant/project_groups/{id} [put]
func (h *IAMOrganizationHandler) UpdateProjectGroup(c *gin.Context) {
	actorID, tenantID, ok := organizationActor(c)
	if !ok {
		return
	}
	groupID, err := parseIAMDecimalID(c.Param("id"))
	if err != nil {
		respondIAMError(c, err)
		return
	}
	var request IAMUpdateProjectGroupRequest
	if err := commonapi.BindOptionalJSONStrict(c, &request); err != nil {
		respondIAMError(c, commonapi.ErrBadRequest)
		return
	}
	group, err := h.service.UpdateProjectGroup(c.Request.Context(), iam.UpdateProjectGroupInput{
		TenantID: int64(tenantID), ProjectGroupID: groupID, Version: request.Version,
		ActorPrincipalID: int64(actorID), Name: request.Name, Description: request.Description,
		Status: request.Status, StartsAt: request.StartsAt, EndsAt: request.EndsAt,
		Audit: iamAuditMetadataWithStatus(c, http.StatusOK),
	})
	if err != nil {
		respondIAMError(c, err)
		return
	}
	c.JSON(http.StatusOK, mapIAMProjectGroup(*group))
}

// CloseProjectGroup godoc
// @Summary 关闭项目组 | Close project group
// @Tags 租户组织 | Tenant Organization
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "项目组 ID | Project group ID"
// @Param request body IAMVersionedLifecycleRequest true "版本与原因 | Version and reason"
// @Success 200 {object} IAMProjectGroupResponse
// @Failure 409 {object} IAMErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["iam.project_group.close"]
// @Router /tenant/project_groups/{id}/close [post]
func (h *IAMOrganizationHandler) CloseProjectGroup(c *gin.Context) {
	actorID, tenantID, ok := organizationActor(c)
	if !ok {
		return
	}
	groupID, err := parseIAMDecimalID(c.Param("id"))
	if err != nil {
		respondIAMError(c, err)
		return
	}
	request, ok := bindVersionedLifecycle(c)
	if !ok {
		return
	}
	group, err := h.service.CloseProjectGroup(c.Request.Context(), iam.CloseProjectGroupInput{
		TenantID: int64(tenantID), ProjectGroupID: groupID, Version: request.Version,
		ActorPrincipalID: int64(actorID), Reason: request.Reason, Audit: iamAuditMetadataWithStatus(c, http.StatusOK),
	})
	if err != nil {
		respondIAMError(c, err)
		return
	}
	c.JSON(http.StatusOK, mapIAMProjectGroup(*group))
}

// ListProjectGroupMemberships godoc
// @Summary 查询项目组成员 | List project group memberships
// @Tags 租户组织 | Tenant Organization
// @Produce json
// @Security BearerAuth
// @Param id path string true "项目组 ID | Project group ID"
// @Param page query int false "页码 | Page number"
// @Param page_size query int false "每页数量 | Page size"
// @Param status query string false "状态：active/ended | Status: active/ended"
// @Success 200 {object} object{data=[]IAMOrganizationMembershipResponse,total=int64,page=int,page_size=int,total_pages=int}
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["iam.project_group_membership.read"]
// @Router /tenant/project_groups/{id}/memberships [get]
func (h *IAMOrganizationHandler) ListProjectGroupMemberships(c *gin.Context) {
	_, tenantID, ok := organizationActor(c)
	if !ok {
		return
	}
	groupID, status, ok := organizationMembershipListParams(c)
	if !ok {
		return
	}
	page, pageSize := commonapi.ParsePagination(c)
	memberships, total, err := h.service.ListProjectGroupMemberships(c.Request.Context(), int64(tenantID), groupID, page, pageSize, status)
	if err != nil {
		respondIAMError(c, err)
		return
	}
	commonapi.RespondPaginated(c, mapIAMOrganizationMemberships(memberships), total, page, pageSize)
}

// CreateProjectGroupMembership godoc
// @Summary 新增项目组成员 | Create project group membership
// @Tags 租户组织 | Tenant Organization
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "项目组 ID | Project group ID"
// @Param request body IAMCreateProjectGroupMembershipRequest true "成员关系 | Membership"
// @Success 201 {object} IAMOrganizationMembershipResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["iam.project_group_membership.create"]
// @Router /tenant/project_groups/{id}/memberships [post]
func (h *IAMOrganizationHandler) CreateProjectGroupMembership(c *gin.Context) {
	actorID, tenantID, ok := organizationActor(c)
	if !ok {
		return
	}
	groupID, err := parseIAMDecimalID(c.Param("id"))
	if err != nil {
		respondIAMError(c, err)
		return
	}
	var request IAMCreateProjectGroupMembershipRequest
	if err := commonapi.BindOptionalJSONStrict(c, &request); err != nil {
		respondIAMError(c, commonapi.ErrBadRequest)
		return
	}
	tenantMembershipID, err := parseIAMDecimalID(request.TenantMembershipID)
	if err != nil {
		respondIAMError(c, err)
		return
	}
	membership, err := h.service.CreateProjectGroupMembership(c.Request.Context(), iam.CreateProjectGroupMembershipInput{
		TenantID: int64(tenantID), ProjectGroupID: groupID, TenantMembershipID: tenantMembershipID,
		ActorPrincipalID: int64(actorID), RelationRole: request.RelationRole,
		Audit: iamAuditMetadataWithStatus(c, http.StatusCreated),
	})
	if err != nil {
		respondIAMError(c, err)
		return
	}
	c.JSON(http.StatusCreated, mapIAMOrganizationMembership(*membership))
}

// UpdateProjectGroupMembership godoc
// @Summary 更新项目组成员关系 | Update project group membership
// @Tags 租户组织 | Tenant Organization
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "项目组 ID | Project group ID"
// @Param membership_id path string true "成员关系 ID | Membership ID"
// @Param request body IAMUpdateProjectGroupMembershipRequest true "组内角色与版本 | Group role and version"
// @Success 200 {object} IAMOrganizationMembershipResponse
// @Failure 409 {object} IAMErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["iam.project_group_membership.update"]
// @Router /tenant/project_groups/{id}/memberships/{membership_id} [put]
func (h *IAMOrganizationHandler) UpdateProjectGroupMembership(c *gin.Context) {
	actorID, tenantID, organizationID, membershipID, ok := organizationMembershipActorAndIDs(c)
	if !ok {
		return
	}
	var request IAMUpdateProjectGroupMembershipRequest
	if err := commonapi.BindOptionalJSONStrict(c, &request); err != nil {
		respondIAMError(c, commonapi.ErrBadRequest)
		return
	}
	membership, err := h.service.UpdateProjectGroupMembership(c.Request.Context(), iam.UpdateProjectGroupMembershipInput{
		TenantID: int64(tenantID), ProjectGroupID: organizationID, MembershipID: membershipID,
		Version: request.Version, ActorPrincipalID: int64(actorID), RelationRole: request.RelationRole,
		Audit: iamAuditMetadataWithStatus(c, http.StatusOK),
	})
	if err != nil {
		respondIAMError(c, err)
		return
	}
	c.JSON(http.StatusOK, mapIAMOrganizationMembership(*membership))
}

// CloseProjectGroupMembership godoc
// @Summary 结束项目组成员关系 | Close project group membership
// @Tags 租户组织 | Tenant Organization
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "项目组 ID | Project group ID"
// @Param membership_id path string true "成员关系 ID | Membership ID"
// @Param request body IAMVersionedLifecycleRequest true "版本与原因 | Version and reason"
// @Success 200 {object} IAMOrganizationMembershipResponse
// @Failure 409 {object} IAMErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["iam.project_group_membership.close"]
// @Router /tenant/project_groups/{id}/memberships/{membership_id}/close [post]
func (h *IAMOrganizationHandler) CloseProjectGroupMembership(c *gin.Context) {
	actorID, tenantID, organizationID, membershipID, ok := organizationMembershipActorAndIDs(c)
	if !ok {
		return
	}
	request, ok := bindVersionedLifecycle(c)
	if !ok {
		return
	}
	membership, err := h.service.CloseProjectGroupMembership(c.Request.Context(), iam.CloseOrganizationMembershipInput{
		TenantID: int64(tenantID), OrganizationID: organizationID, MembershipID: membershipID,
		Version: request.Version, ActorPrincipalID: int64(actorID), Reason: request.Reason,
		Audit: iamAuditMetadataWithStatus(c, http.StatusOK),
	})
	if err != nil {
		respondIAMError(c, err)
		return
	}
	c.JSON(http.StatusOK, mapIAMOrganizationMembership(*membership))
}

func organizationActor(c *gin.Context) (uint, uint, bool) {
	actorID, tenantID, err := iamTenantUserActor(c)
	if err != nil {
		respondIAMError(c, err)
		return 0, 0, false
	}
	return actorID, tenantID, true
}

func organizationMembershipActorAndIDs(c *gin.Context) (uint, uint, int64, int64, bool) {
	actorID, tenantID, ok := organizationActor(c)
	if !ok {
		return 0, 0, 0, 0, false
	}
	organizationID, err := parseIAMDecimalID(c.Param("id"))
	if err != nil {
		respondIAMError(c, err)
		return 0, 0, 0, 0, false
	}
	membershipID, err := parseIAMDecimalID(c.Param("membership_id"))
	if err != nil {
		respondIAMError(c, err)
		return 0, 0, 0, 0, false
	}
	return actorID, tenantID, organizationID, membershipID, true
}

func organizationMembershipListParams(c *gin.Context) (int64, *iam.OrganizationMembershipStatus, bool) {
	organizationID, err := parseIAMDecimalID(c.Param("id"))
	if err != nil {
		respondIAMError(c, err)
		return 0, nil, false
	}
	status, err := parseOrganizationMembershipStatus(c.Query("status"))
	if err != nil {
		respondIAMError(c, err)
		return 0, nil, false
	}
	return organizationID, status, true
}

func bindVersionedLifecycle(c *gin.Context) (IAMVersionedLifecycleRequest, bool) {
	var request IAMVersionedLifecycleRequest
	if err := commonapi.BindOptionalJSONStrict(c, &request); err != nil || request.Version <= 0 || strings.TrimSpace(request.Reason) == "" {
		respondIAMError(c, commonapi.ErrBadRequest)
		return IAMVersionedLifecycleRequest{}, false
	}
	return request, true
}

func parseOptionalIAMID(value *string) (*int64, error) {
	if value == nil {
		return nil, nil
	}
	id, err := parseIAMDecimalID(*value)
	if err != nil {
		return nil, err
	}
	return &id, nil
}

func parseDepartmentStatus(value string) (*iam.DepartmentStatus, error) {
	normalized := iam.DepartmentStatus(strings.TrimSpace(value))
	if normalized == "" {
		return nil, nil
	}
	if normalized != iam.DepartmentStatusActive && normalized != iam.DepartmentStatusDisabled {
		return nil, commonapi.ErrBadRequest
	}
	return &normalized, nil
}

func parseProjectGroupStatus(value string) (*iam.ProjectGroupStatus, error) {
	normalized := iam.ProjectGroupStatus(strings.TrimSpace(value))
	if normalized == "" {
		return nil, nil
	}
	if normalized != iam.ProjectGroupStatusPlanned && normalized != iam.ProjectGroupStatusActive && normalized != iam.ProjectGroupStatusClosed {
		return nil, commonapi.ErrBadRequest
	}
	return &normalized, nil
}

func parseOrganizationMembershipStatus(value string) (*iam.OrganizationMembershipStatus, error) {
	normalized := iam.OrganizationMembershipStatus(strings.TrimSpace(value))
	if normalized == "" {
		return nil, nil
	}
	if normalized != iam.OrganizationMembershipStatusActive && normalized != iam.OrganizationMembershipStatusEnded {
		return nil, commonapi.ErrBadRequest
	}
	return &normalized, nil
}

func mapIAMDepartment(department iam.Department) IAMDepartmentResponse {
	return IAMDepartmentResponse{
		ID: strconv.FormatInt(department.ID, 10), ParentID: formatOptionalIAMID(department.ParentID), Code: department.Code,
		Name: department.Name, Status: department.Status, Version: department.Version,
		CreatedAt: department.CreatedAt, UpdatedAt: department.UpdatedAt,
	}
}

func mapIAMProjectGroup(group iam.ProjectGroup) IAMProjectGroupResponse {
	return IAMProjectGroupResponse{
		ID: strconv.FormatInt(group.ID, 10), Code: group.Code, Name: group.Name, Description: group.Description,
		Status: group.Status, StartsAt: group.StartsAt, EndsAt: group.EndsAt, Version: group.Version,
		CreatedAt: group.CreatedAt, UpdatedAt: group.UpdatedAt,
	}
}

func mapIAMOrganizationMemberships(memberships []iam.ManagedOrganizationMembership) []IAMOrganizationMembershipResponse {
	responses := make([]IAMOrganizationMembershipResponse, 0, len(memberships))
	for _, membership := range memberships {
		responses = append(responses, mapIAMOrganizationMembership(membership))
	}
	return responses
}

func mapIAMOrganizationMembership(membership iam.ManagedOrganizationMembership) IAMOrganizationMembershipResponse {
	return IAMOrganizationMembershipResponse{
		ID: strconv.FormatInt(membership.ID, 10), TenantMembershipID: strconv.FormatInt(membership.TenantMembershipID, 10),
		PrincipalID: strconv.FormatInt(membership.PrincipalID, 10), DisplayName: membership.DisplayName, Username: membership.Username,
		MembershipType: membership.MembershipType, DepartmentRole: membership.DepartmentRole, ProjectGroupRole: membership.ProjectGroupRole,
		Status: membership.Status, EndedAt: membership.EndedAt, Version: membership.Version,
		CreatedAt: membership.CreatedAt, UpdatedAt: membership.UpdatedAt,
	}
}
