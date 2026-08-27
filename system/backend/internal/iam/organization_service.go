package iam

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	commonapi "github.com/addp/common/api"
)

var organizationCodePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}[a-z0-9]$|^[a-z]$`)

type CreateDepartmentInput struct {
	TenantID, ActorPrincipalID int64
	ParentID                   *int64
	Code, Name                 string
	Audit                      AuditMetadata
}

type UpdateDepartmentInput struct {
	TenantID, DepartmentID, Version, ActorPrincipalID int64
	ParentID                                          *int64
	Name                                              string
	Audit                                             AuditMetadata
}

type ChangeDepartmentStatusInput struct {
	TenantID, DepartmentID, Version, ActorPrincipalID int64
	Reason                                            string
	Audit                                             AuditMetadata
}

type CreateDepartmentMembershipInput struct {
	TenantID, DepartmentID, TenantMembershipID, ActorPrincipalID int64
	MembershipType                                               DepartmentMembershipType
	RelationRole                                                 DepartmentRelationRole
	Audit                                                        AuditMetadata
}

type UpdateDepartmentMembershipInput struct {
	TenantID, DepartmentID, MembershipID, Version, ActorPrincipalID int64
	MembershipType                                                  DepartmentMembershipType
	RelationRole                                                    DepartmentRelationRole
	Audit                                                           AuditMetadata
}

type CloseOrganizationMembershipInput struct {
	TenantID, OrganizationID, MembershipID, Version, ActorPrincipalID int64
	Reason                                                            string
	Audit                                                             AuditMetadata
}

type CreateProjectGroupInput struct {
	TenantID, ActorPrincipalID int64
	Code, Name, Description    string
	Status                     ProjectGroupStatus
	StartsAt, EndsAt           *time.Time
	Audit                      AuditMetadata
}

type UpdateProjectGroupInput struct {
	TenantID, ProjectGroupID, Version, ActorPrincipalID int64
	Name, Description                                   string
	Status                                              ProjectGroupStatus
	StartsAt, EndsAt                                    *time.Time
	Audit                                               AuditMetadata
}

type CloseProjectGroupInput struct {
	TenantID, ProjectGroupID, Version, ActorPrincipalID int64
	Reason                                              string
	Audit                                               AuditMetadata
}

type CreateProjectGroupMembershipInput struct {
	TenantID, ProjectGroupID, TenantMembershipID, ActorPrincipalID int64
	RelationRole                                                   ProjectGroupRelationRole
	Audit                                                          AuditMetadata
}

type UpdateProjectGroupMembershipInput struct {
	TenantID, ProjectGroupID, MembershipID, Version, ActorPrincipalID int64
	RelationRole                                                      ProjectGroupRelationRole
	Audit                                                             AuditMetadata
}

type OrganizationService struct {
	repository *Repository
	now        func() time.Time
}

func NewOrganizationService(repository *Repository, now func() time.Time) *OrganizationService {
	if now == nil {
		now = time.Now
	}
	return &OrganizationService{repository: repository, now: now}
}

func (s *OrganizationService) ListDepartments(ctx context.Context, tenantID int64, page, pageSize int, search string, status *DepartmentStatus) ([]Department, int64, error) {
	if tenantID <= 0 || validateManagementPagination(page, pageSize) != nil {
		return nil, 0, commonapi.ErrBadRequest
	}
	return s.repository.ListDepartments(ctx, tenantID, page, pageSize, search, status)
}

func (s *OrganizationService) GetDepartment(ctx context.Context, tenantID, departmentID int64) (*Department, error) {
	if tenantID <= 0 || departmentID <= 0 {
		return nil, commonapi.ErrBadRequest
	}
	return s.repository.GetDepartment(ctx, tenantID, departmentID)
}

func (s *OrganizationService) CreateDepartment(ctx context.Context, input CreateDepartmentInput) (*Department, error) {
	code, name, err := validateOrganizationIdentity(input.Code, input.Name)
	if err != nil || input.TenantID <= 0 || input.ActorPrincipalID <= 0 {
		if err != nil {
			return nil, err
		}
		return nil, commonapi.ErrBadRequest
	}
	department := &Department{TenantID: input.TenantID, ParentID: input.ParentID, Code: code, Name: name, Status: DepartmentStatusActive, Version: 1}
	err = s.repository.Transaction(ctx, func(tx *Repository) error {
		if err := tx.LockDepartmentStructure(ctx, input.TenantID); err != nil {
			return err
		}
		if input.ParentID != nil {
			parent, err := tx.LockDepartment(ctx, input.TenantID, *input.ParentID)
			if err != nil {
				return err
			}
			if parent.Status != DepartmentStatusActive {
				return fmt.Errorf("%w: parent department is disabled", commonapi.ErrConflict)
			}
		}
		if err := tx.CreateDepartment(ctx, department); err != nil {
			return err
		}
		return writeOrganizationAudit(ctx, tx, input.Audit, "iam.department.created", AuditRiskMedium, "department", department.ID, map[string]any{"tenant_id": input.TenantID, "code": code, "parent_id": input.ParentID})
	})
	if err != nil {
		return nil, err
	}
	return s.GetDepartment(ctx, input.TenantID, department.ID)
}

func (s *OrganizationService) UpdateDepartment(ctx context.Context, input UpdateDepartmentInput) (*Department, error) {
	name := strings.TrimSpace(input.Name)
	if input.TenantID <= 0 || input.DepartmentID <= 0 || input.Version <= 0 || input.ActorPrincipalID <= 0 || name == "" || (input.ParentID != nil && *input.ParentID == input.DepartmentID) {
		return nil, commonapi.ErrBadRequest
	}
	err := s.repository.Transaction(ctx, func(tx *Repository) error {
		if err := tx.LockDepartmentStructure(ctx, input.TenantID); err != nil {
			return err
		}
		current, err := tx.LockDepartment(ctx, input.TenantID, input.DepartmentID)
		if err != nil {
			return err
		}
		if current.Status != DepartmentStatusActive {
			return fmt.Errorf("%w: disabled department cannot be edited", commonapi.ErrConflict)
		}
		if input.ParentID != nil {
			parent, err := tx.GetDepartment(ctx, input.TenantID, *input.ParentID)
			if err != nil {
				return err
			}
			if parent.Status != DepartmentStatusActive {
				return fmt.Errorf("%w: parent department is disabled", commonapi.ErrConflict)
			}
			descendant, err := tx.IsDepartmentDescendant(ctx, input.TenantID, input.DepartmentID, *input.ParentID)
			if err != nil {
				return err
			}
			if descendant {
				return fmt.Errorf("%w: parent department cannot be a descendant", commonapi.ErrConflict)
			}
		}
		if err := tx.UpdateDepartment(ctx, input.TenantID, input.DepartmentID, input.Version, input.ParentID, name); err != nil {
			return err
		}
		return writeOrganizationAudit(ctx, tx, input.Audit, "iam.department.updated", AuditRiskMedium, "department", input.DepartmentID, map[string]any{"tenant_id": input.TenantID, "parent_id": input.ParentID})
	})
	if err != nil {
		return nil, err
	}
	return s.GetDepartment(ctx, input.TenantID, input.DepartmentID)
}

func (s *OrganizationService) DisableDepartment(ctx context.Context, input ChangeDepartmentStatusInput) (*Department, error) {
	return s.changeDepartmentStatus(ctx, input, DepartmentStatusActive, DepartmentStatusDisabled, "iam.department.disabled", AuditRiskHigh)
}

func (s *OrganizationService) RestoreDepartment(ctx context.Context, input ChangeDepartmentStatusInput) (*Department, error) {
	return s.changeDepartmentStatus(ctx, input, DepartmentStatusDisabled, DepartmentStatusActive, "iam.department.restored", AuditRiskHigh)
}

func (s *OrganizationService) changeDepartmentStatus(ctx context.Context, input ChangeDepartmentStatusInput, from, to DepartmentStatus, event string, risk AuditRiskLevel) (*Department, error) {
	reason := strings.TrimSpace(input.Reason)
	if input.TenantID <= 0 || input.DepartmentID <= 0 || input.Version <= 0 || input.ActorPrincipalID <= 0 || reason == "" {
		return nil, commonapi.ErrBadRequest
	}
	err := s.repository.Transaction(ctx, func(tx *Repository) error {
		if err := tx.LockDepartmentStructure(ctx, input.TenantID); err != nil {
			return err
		}
		current, err := tx.LockDepartment(ctx, input.TenantID, input.DepartmentID)
		if err != nil {
			return err
		}
		if current.Status != from {
			return fmt.Errorf("%w: invalid department lifecycle transition", commonapi.ErrConflict)
		}
		if to == DepartmentStatusDisabled {
			children, err := tx.CountActiveDepartmentChildren(ctx, input.TenantID, input.DepartmentID)
			if err != nil {
				return err
			}
			if children > 0 {
				return fmt.Errorf("%w: department has active child departments", commonapi.ErrConflict)
			}
		} else if current.ParentID != nil {
			parent, err := tx.GetDepartment(ctx, input.TenantID, *current.ParentID)
			if err != nil {
				return err
			}
			if parent.Status != DepartmentStatusActive {
				return fmt.Errorf("%w: parent department is disabled", commonapi.ErrConflict)
			}
		}
		if err := tx.UpdateDepartmentStatus(ctx, input.TenantID, input.DepartmentID, input.Version, to); err != nil {
			return err
		}
		return writeOrganizationAudit(ctx, tx, input.Audit, event, risk, "department", input.DepartmentID, map[string]any{"tenant_id": input.TenantID, "reason": reason})
	})
	if err != nil {
		return nil, err
	}
	return s.GetDepartment(ctx, input.TenantID, input.DepartmentID)
}

func (s *OrganizationService) ListDepartmentMemberships(ctx context.Context, tenantID, departmentID int64, page, pageSize int, status *OrganizationMembershipStatus) ([]ManagedOrganizationMembership, int64, error) {
	if tenantID <= 0 || departmentID <= 0 || validateManagementPagination(page, pageSize) != nil {
		return nil, 0, commonapi.ErrBadRequest
	}
	if _, err := s.repository.GetDepartment(ctx, tenantID, departmentID); err != nil {
		return nil, 0, err
	}
	return s.repository.ListDepartmentMemberships(ctx, tenantID, departmentID, page, pageSize, status)
}

func (s *OrganizationService) CreateDepartmentMembership(ctx context.Context, input CreateDepartmentMembershipInput) (*ManagedOrganizationMembership, error) {
	if input.TenantID <= 0 || input.DepartmentID <= 0 || input.TenantMembershipID <= 0 || input.ActorPrincipalID <= 0 || !validDepartmentMembershipType(input.MembershipType) || !validDepartmentRole(input.RelationRole) {
		return nil, commonapi.ErrBadRequest
	}
	membership := &DepartmentMembership{TenantID: input.TenantID, DepartmentID: input.DepartmentID, TenantMembershipID: input.TenantMembershipID, MembershipType: input.MembershipType, RelationRole: input.RelationRole, Status: OrganizationMembershipStatusActive, Version: 1}
	err := s.repository.Transaction(ctx, func(tx *Repository) error {
		department, err := tx.LockDepartment(ctx, input.TenantID, input.DepartmentID)
		if err != nil {
			return err
		}
		if department.Status != DepartmentStatusActive {
			return fmt.Errorf("%w: department is disabled", commonapi.ErrConflict)
		}
		tenantMembership, err := tx.GetManagedTenantMembership(ctx, input.TenantID, input.TenantMembershipID)
		if err != nil {
			return err
		}
		if tenantMembership.Status != TenantMembershipStatusActive || tenantMembership.PrincipalStatus != PrincipalStatusActive {
			return fmt.Errorf("%w: tenant membership is not active", commonapi.ErrConflict)
		}
		if _, err := tx.LockPrincipal(ctx, tenantMembership.PrincipalID); err != nil {
			return err
		}
		if err := tx.CreateDepartmentMembership(ctx, membership); err != nil {
			return err
		}
		return writeOrganizationAudit(ctx, tx, input.Audit, "iam.department_membership.created", AuditRiskMedium, "department_membership", membership.ID, map[string]any{"tenant_id": input.TenantID, "department_id": input.DepartmentID, "tenant_membership_id": input.TenantMembershipID})
	})
	if err != nil {
		return nil, err
	}
	return s.repository.GetDepartmentMembership(ctx, input.TenantID, input.DepartmentID, membership.ID)
}

func (s *OrganizationService) UpdateDepartmentMembership(ctx context.Context, input UpdateDepartmentMembershipInput) (*ManagedOrganizationMembership, error) {
	if input.TenantID <= 0 || input.DepartmentID <= 0 || input.MembershipID <= 0 || input.Version <= 0 || input.ActorPrincipalID <= 0 || !validDepartmentMembershipType(input.MembershipType) || !validDepartmentRole(input.RelationRole) {
		return nil, commonapi.ErrBadRequest
	}
	err := s.repository.Transaction(ctx, func(tx *Repository) error {
		department, err := tx.LockDepartment(ctx, input.TenantID, input.DepartmentID)
		if err != nil {
			return err
		}
		if department.Status != DepartmentStatusActive {
			return fmt.Errorf("%w: department is disabled", commonapi.ErrConflict)
		}
		current, err := tx.GetDepartmentMembership(ctx, input.TenantID, input.DepartmentID, input.MembershipID)
		if err != nil {
			return err
		}
		if _, err := tx.LockPrincipal(ctx, current.PrincipalID); err != nil {
			return err
		}
		if err := tx.UpdateDepartmentMembership(ctx, input.TenantID, input.DepartmentID, input.MembershipID, input.Version, input.MembershipType, input.RelationRole); err != nil {
			return err
		}
		return writeOrganizationAudit(ctx, tx, input.Audit, "iam.department_membership.updated", AuditRiskMedium, "department_membership", input.MembershipID, map[string]any{"tenant_id": input.TenantID, "department_id": input.DepartmentID})
	})
	if err != nil {
		return nil, err
	}
	return s.repository.GetDepartmentMembership(ctx, input.TenantID, input.DepartmentID, input.MembershipID)
}

func (s *OrganizationService) CloseDepartmentMembership(ctx context.Context, input CloseOrganizationMembershipInput) (*ManagedOrganizationMembership, error) {
	return s.closeOrganizationMembership(ctx, input, true)
}

func (s *OrganizationService) ListProjectGroups(ctx context.Context, tenantID int64, page, pageSize int, search string, status *ProjectGroupStatus) ([]ProjectGroup, int64, error) {
	if tenantID <= 0 || validateManagementPagination(page, pageSize) != nil {
		return nil, 0, commonapi.ErrBadRequest
	}
	return s.repository.ListProjectGroups(ctx, tenantID, page, pageSize, search, status)
}

func (s *OrganizationService) GetProjectGroup(ctx context.Context, tenantID, groupID int64) (*ProjectGroup, error) {
	if tenantID <= 0 || groupID <= 0 {
		return nil, commonapi.ErrBadRequest
	}
	return s.repository.GetProjectGroup(ctx, tenantID, groupID)
}

func (s *OrganizationService) CreateProjectGroup(ctx context.Context, input CreateProjectGroupInput) (*ProjectGroup, error) {
	code, name, err := validateOrganizationIdentity(input.Code, input.Name)
	if err != nil || input.TenantID <= 0 || input.ActorPrincipalID <= 0 || !validProjectGroupWritableStatus(input.Status) || !validProjectDates(input.StartsAt, input.EndsAt) {
		if err != nil {
			return nil, err
		}
		return nil, commonapi.ErrBadRequest
	}
	group := &ProjectGroup{TenantID: input.TenantID, Code: code, Name: name, Description: strings.TrimSpace(input.Description), Status: input.Status, StartsAt: utcTimePointer(input.StartsAt), EndsAt: utcTimePointer(input.EndsAt), Version: 1}
	err = s.repository.Transaction(ctx, func(tx *Repository) error {
		if err := tx.CreateProjectGroup(ctx, group); err != nil {
			return err
		}
		return writeOrganizationAudit(ctx, tx, input.Audit, "iam.project_group.created", AuditRiskMedium, "project_group", group.ID, map[string]any{"tenant_id": input.TenantID, "code": code, "status": input.Status})
	})
	if err != nil {
		return nil, err
	}
	return s.GetProjectGroup(ctx, input.TenantID, group.ID)
}

func (s *OrganizationService) UpdateProjectGroup(ctx context.Context, input UpdateProjectGroupInput) (*ProjectGroup, error) {
	name := strings.TrimSpace(input.Name)
	if input.TenantID <= 0 || input.ProjectGroupID <= 0 || input.Version <= 0 || input.ActorPrincipalID <= 0 || name == "" || !validProjectGroupWritableStatus(input.Status) || !validProjectDates(input.StartsAt, input.EndsAt) {
		return nil, commonapi.ErrBadRequest
	}
	err := s.repository.Transaction(ctx, func(tx *Repository) error {
		current, err := tx.LockProjectGroup(ctx, input.TenantID, input.ProjectGroupID)
		if err != nil {
			return err
		}
		if current.Status == ProjectGroupStatusClosed || (current.Status == ProjectGroupStatusActive && input.Status == ProjectGroupStatusPlanned) {
			return fmt.Errorf("%w: invalid project group lifecycle transition", commonapi.ErrConflict)
		}
		if err := tx.UpdateProjectGroup(ctx, input.TenantID, input.ProjectGroupID, input.Version, name, strings.TrimSpace(input.Description), input.Status, utcTimePointer(input.StartsAt), utcTimePointer(input.EndsAt)); err != nil {
			return err
		}
		return writeOrganizationAudit(ctx, tx, input.Audit, "iam.project_group.updated", AuditRiskMedium, "project_group", input.ProjectGroupID, map[string]any{"tenant_id": input.TenantID, "status": input.Status})
	})
	if err != nil {
		return nil, err
	}
	return s.GetProjectGroup(ctx, input.TenantID, input.ProjectGroupID)
}

func (s *OrganizationService) CloseProjectGroup(ctx context.Context, input CloseProjectGroupInput) (*ProjectGroup, error) {
	reason := strings.TrimSpace(input.Reason)
	if input.TenantID <= 0 || input.ProjectGroupID <= 0 || input.Version <= 0 || input.ActorPrincipalID <= 0 || reason == "" {
		return nil, commonapi.ErrBadRequest
	}
	err := s.repository.Transaction(ctx, func(tx *Repository) error {
		current, err := tx.LockProjectGroup(ctx, input.TenantID, input.ProjectGroupID)
		if err != nil {
			return err
		}
		if current.Status == ProjectGroupStatusClosed {
			return fmt.Errorf("%w: project group is already closed", commonapi.ErrConflict)
		}
		if err := tx.CloseProjectGroup(ctx, input.TenantID, input.ProjectGroupID, input.Version); err != nil {
			return err
		}
		return writeOrganizationAudit(ctx, tx, input.Audit, "iam.project_group.closed", AuditRiskCritical, "project_group", input.ProjectGroupID, map[string]any{"tenant_id": input.TenantID, "reason": reason})
	})
	if err != nil {
		return nil, err
	}
	return s.GetProjectGroup(ctx, input.TenantID, input.ProjectGroupID)
}

func (s *OrganizationService) ListProjectGroupMemberships(ctx context.Context, tenantID, groupID int64, page, pageSize int, status *OrganizationMembershipStatus) ([]ManagedOrganizationMembership, int64, error) {
	if tenantID <= 0 || groupID <= 0 || validateManagementPagination(page, pageSize) != nil {
		return nil, 0, commonapi.ErrBadRequest
	}
	if _, err := s.repository.GetProjectGroup(ctx, tenantID, groupID); err != nil {
		return nil, 0, err
	}
	return s.repository.ListProjectGroupMemberships(ctx, tenantID, groupID, page, pageSize, status)
}

func (s *OrganizationService) CreateProjectGroupMembership(ctx context.Context, input CreateProjectGroupMembershipInput) (*ManagedOrganizationMembership, error) {
	if input.TenantID <= 0 || input.ProjectGroupID <= 0 || input.TenantMembershipID <= 0 || input.ActorPrincipalID <= 0 || !validProjectGroupRole(input.RelationRole) {
		return nil, commonapi.ErrBadRequest
	}
	membership := &ProjectGroupMembership{TenantID: input.TenantID, ProjectGroupID: input.ProjectGroupID, TenantMembershipID: input.TenantMembershipID, RelationRole: input.RelationRole, Status: OrganizationMembershipStatusActive, Version: 1}
	err := s.repository.Transaction(ctx, func(tx *Repository) error {
		group, err := tx.LockProjectGroup(ctx, input.TenantID, input.ProjectGroupID)
		if err != nil {
			return err
		}
		if group.Status == ProjectGroupStatusClosed {
			return fmt.Errorf("%w: project group is closed", commonapi.ErrConflict)
		}
		tenantMembership, err := tx.GetManagedTenantMembership(ctx, input.TenantID, input.TenantMembershipID)
		if err != nil {
			return err
		}
		if tenantMembership.Status != TenantMembershipStatusActive || tenantMembership.PrincipalStatus != PrincipalStatusActive {
			return fmt.Errorf("%w: tenant membership is not active", commonapi.ErrConflict)
		}
		if _, err := tx.LockPrincipal(ctx, tenantMembership.PrincipalID); err != nil {
			return err
		}
		if err := tx.CreateProjectGroupMembership(ctx, membership); err != nil {
			return err
		}
		return writeOrganizationAudit(ctx, tx, input.Audit, "iam.project_group_membership.created", AuditRiskMedium, "project_group_membership", membership.ID, map[string]any{"tenant_id": input.TenantID, "project_group_id": input.ProjectGroupID, "tenant_membership_id": input.TenantMembershipID})
	})
	if err != nil {
		return nil, err
	}
	return s.repository.GetProjectGroupMembership(ctx, input.TenantID, input.ProjectGroupID, membership.ID)
}

func (s *OrganizationService) UpdateProjectGroupMembership(ctx context.Context, input UpdateProjectGroupMembershipInput) (*ManagedOrganizationMembership, error) {
	if input.TenantID <= 0 || input.ProjectGroupID <= 0 || input.MembershipID <= 0 || input.Version <= 0 || input.ActorPrincipalID <= 0 || !validProjectGroupRole(input.RelationRole) {
		return nil, commonapi.ErrBadRequest
	}
	err := s.repository.Transaction(ctx, func(tx *Repository) error {
		group, err := tx.LockProjectGroup(ctx, input.TenantID, input.ProjectGroupID)
		if err != nil {
			return err
		}
		if group.Status == ProjectGroupStatusClosed {
			return fmt.Errorf("%w: project group is closed", commonapi.ErrConflict)
		}
		current, err := tx.GetProjectGroupMembership(ctx, input.TenantID, input.ProjectGroupID, input.MembershipID)
		if err != nil {
			return err
		}
		if _, err := tx.LockPrincipal(ctx, current.PrincipalID); err != nil {
			return err
		}
		if err := tx.UpdateProjectGroupMembership(ctx, input.TenantID, input.ProjectGroupID, input.MembershipID, input.Version, input.RelationRole); err != nil {
			return err
		}
		return writeOrganizationAudit(ctx, tx, input.Audit, "iam.project_group_membership.updated", AuditRiskMedium, "project_group_membership", input.MembershipID, map[string]any{"tenant_id": input.TenantID, "project_group_id": input.ProjectGroupID})
	})
	if err != nil {
		return nil, err
	}
	return s.repository.GetProjectGroupMembership(ctx, input.TenantID, input.ProjectGroupID, input.MembershipID)
}

func (s *OrganizationService) CloseProjectGroupMembership(ctx context.Context, input CloseOrganizationMembershipInput) (*ManagedOrganizationMembership, error) {
	return s.closeOrganizationMembership(ctx, input, false)
}

func (s *OrganizationService) closeOrganizationMembership(ctx context.Context, input CloseOrganizationMembershipInput, department bool) (*ManagedOrganizationMembership, error) {
	reason := strings.TrimSpace(input.Reason)
	if input.TenantID <= 0 || input.OrganizationID <= 0 || input.MembershipID <= 0 || input.Version <= 0 || input.ActorPrincipalID <= 0 || reason == "" {
		return nil, commonapi.ErrBadRequest
	}
	now := s.now().UTC()
	err := s.repository.Transaction(ctx, func(tx *Repository) error {
		var current *ManagedOrganizationMembership
		var err error
		if department {
			if _, err = tx.LockDepartment(ctx, input.TenantID, input.OrganizationID); err != nil {
				return err
			}
			current, err = tx.GetDepartmentMembership(ctx, input.TenantID, input.OrganizationID, input.MembershipID)
		} else {
			if _, err = tx.LockProjectGroup(ctx, input.TenantID, input.OrganizationID); err != nil {
				return err
			}
			current, err = tx.GetProjectGroupMembership(ctx, input.TenantID, input.OrganizationID, input.MembershipID)
		}
		if err != nil {
			return err
		}
		if _, err := tx.LockPrincipal(ctx, current.PrincipalID); err != nil {
			return err
		}
		entityType, event := "project_group_membership", "iam.project_group_membership.closed"
		if department {
			entityType, event = "department_membership", "iam.department_membership.closed"
			err = tx.CloseDepartmentMembership(ctx, input.TenantID, input.OrganizationID, input.MembershipID, input.Version, now)
		} else {
			err = tx.CloseProjectGroupMembership(ctx, input.TenantID, input.OrganizationID, input.MembershipID, input.Version, now)
		}
		if err != nil {
			return err
		}
		return writeOrganizationAudit(ctx, tx, input.Audit, event, AuditRiskCritical, entityType, input.MembershipID, map[string]any{"tenant_id": input.TenantID, "organization_id": input.OrganizationID, "reason": reason})
	})
	if err != nil {
		return nil, err
	}
	if department {
		return s.repository.GetDepartmentMembership(ctx, input.TenantID, input.OrganizationID, input.MembershipID)
	}
	return s.repository.GetProjectGroupMembership(ctx, input.TenantID, input.OrganizationID, input.MembershipID)
}

func validateOrganizationIdentity(code, name string) (string, string, error) {
	code = strings.ToLower(strings.TrimSpace(code))
	name = strings.TrimSpace(name)
	if !organizationCodePattern.MatchString(code) || name == "" {
		return "", "", commonapi.ErrBadRequest
	}
	return code, name, nil
}

func validDepartmentMembershipType(value DepartmentMembershipType) bool {
	return value == DepartmentMembershipTypePrimary || value == DepartmentMembershipTypeAdditional
}

func validDepartmentRole(value DepartmentRelationRole) bool {
	return value == DepartmentRelationRoleMember || value == DepartmentRelationRoleLeader
}

func validProjectGroupWritableStatus(value ProjectGroupStatus) bool {
	return value == ProjectGroupStatusPlanned || value == ProjectGroupStatusActive
}

func validProjectGroupRole(value ProjectGroupRelationRole) bool {
	return value == ProjectGroupRelationRoleMember || value == ProjectGroupRelationRoleLeader || value == ProjectGroupRelationRoleCoordinator
}

func validProjectDates(startsAt, endsAt *time.Time) bool {
	return startsAt == nil || endsAt == nil || endsAt.After(*startsAt)
}

func writeOrganizationAudit(ctx context.Context, tx *Repository, metadata AuditMetadata, event string, risk AuditRiskLevel, entityType string, entityID int64, details map[string]any) error {
	return NewAuditWriter(tx).Write(ctx, AuditEvent{Metadata: metadata, EventName: event, Result: AuditResultSucceeded, RiskLevel: risk, ModuleName: "system", EntityType: entityType, EntityID: strconv.FormatInt(entityID, 10), Details: details})
}
