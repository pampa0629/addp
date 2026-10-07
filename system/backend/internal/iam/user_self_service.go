package iam

import (
	"context"
	"errors"
	"fmt"
	"time"

	commonapi "github.com/addp/common/api"
)

type CurrentLocalAccountProfile struct {
	Username string
}

type CurrentUserProfile struct {
	ID           int64
	DisplayName  string
	PrimaryEmail *string
	Locale       *string
	CreatedAt    time.Time
	UpdatedAt    time.Time
	LocalAccount *CurrentLocalAccountProfile
}

type CurrentOrganizationReference struct {
	ID   int64
	Name string
	Code string
}

type CurrentDepartmentMembership struct {
	CurrentOrganizationReference
	Path           []CurrentOrganizationReference
	MembershipType string
	RelationRole   string
}

type CurrentProjectGroupMembership struct {
	CurrentOrganizationReference
	RelationRole string
}

type CurrentUserOrganization struct {
	Tenant        *CurrentOrganizationReference
	Departments   []CurrentDepartmentMembership
	ProjectGroups []CurrentProjectGroupMembership
}

// ResolveCurrentUserOrganization uses the same effective membership facts as
// AuthContext; ancestor names are display facts, never additional memberships.
func (s *UserSelfService) ResolveCurrentUserOrganization(ctx context.Context, accessToken string) (*CurrentUserOrganization, error) {
	if s == nil || s.repository == nil {
		return nil, fmt.Errorf("%w: user self service is required", commonapi.ErrBadRequest)
	}
	result := &CurrentUserOrganization{
		Departments:   make([]CurrentDepartmentMembership, 0),
		ProjectGroups: make([]CurrentProjectGroupMembership, 0),
	}
	err := s.repository.ReadOnlyRepeatableReadTransaction(ctx, func(tx *Repository) error {
		snapshot, err := resolveFirstPartyAccessTokenSnapshot(ctx, tx, accessToken)
		if err != nil {
			return err
		}
		if snapshot.FamilyContextType == ContextTypePlatform {
			return nil
		}
		tenantID, membershipID := *snapshot.TenantID, *snapshot.TenantMembershipID
		tenant, err := tx.GetTenant(ctx, tenantID)
		if err != nil {
			return err
		}
		result.Tenant = &CurrentOrganizationReference{ID: tenant.ID, Name: tenant.Name, Code: tenant.Code}
		departments, err := tx.ListEffectiveDepartmentMemberships(ctx, membershipID, tenantID)
		if err != nil {
			return err
		}
		departmentNames := make(map[int64]CurrentOrganizationReference)
		resolveDepartment := func(id int64) (CurrentOrganizationReference, error) {
			if ref, ok := departmentNames[id]; ok {
				return ref, nil
			}
			department, err := tx.GetDepartment(ctx, tenantID, id)
			if err != nil {
				return CurrentOrganizationReference{}, err
			}
			ref := CurrentOrganizationReference{ID: department.ID, Name: department.Name, Code: department.Code}
			departmentNames[id] = ref
			return ref, nil
		}
		for _, membership := range departments {
			ref, err := resolveDepartment(membership.DepartmentID)
			if err != nil {
				return err
			}
			path := make([]CurrentOrganizationReference, 0, len(membership.AncestorIDs)+1)
			for _, id := range membership.AncestorIDs {
				ancestor, err := resolveDepartment(id)
				if err != nil {
					return err
				}
				path = append(path, ancestor)
			}
			path = append(path, ref)
			result.Departments = append(result.Departments, CurrentDepartmentMembership{
				CurrentOrganizationReference: ref, Path: path,
				MembershipType: membership.MembershipType, RelationRole: membership.RelationRole,
			})
		}
		groups, err := tx.ListEffectiveProjectGroupMemberships(ctx, membershipID, tenantID)
		if err != nil {
			return err
		}
		for _, membership := range groups {
			group, err := tx.GetProjectGroup(ctx, tenantID, membership.ProjectGroupID)
			if err != nil {
				return err
			}
			result.ProjectGroups = append(result.ProjectGroups, CurrentProjectGroupMembership{
				CurrentOrganizationReference: CurrentOrganizationReference{ID: group.ID, Name: group.Name, Code: group.Code},
				RelationRole:                 membership.RelationRole,
			})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

type UserSelfService struct {
	repository      *Repository
	identityService *IdentityService
}

func NewUserSelfService(repository *Repository, identityService *IdentityService) (*UserSelfService, error) {
	if repository == nil || identityService == nil {
		return nil, fmt.Errorf("%w: user self dependencies are required", commonapi.ErrBadRequest)
	}
	return &UserSelfService{repository: repository, identityService: identityService}, nil
}

func (s *UserSelfService) ResolveCurrentUserProfile(
	ctx context.Context,
	accessToken string,
) (*CurrentUserProfile, error) {
	if s == nil || s.repository == nil {
		return nil, fmt.Errorf("%w: user self service is required", commonapi.ErrBadRequest)
	}

	var profile *CurrentUserProfile
	err := s.repository.ReadOnlyRepeatableReadTransaction(ctx, func(tx *Repository) error {
		snapshot, err := resolveFirstPartyAccessTokenSnapshot(ctx, tx, accessToken)
		if err != nil {
			return err
		}
		user, err := tx.GetUser(ctx, snapshot.FamilyPrincipalID)
		if err != nil {
			return fmt.Errorf("current user profile is inconsistent: %v", err)
		}
		profile = &CurrentUserProfile{
			ID:           user.ID,
			DisplayName:  user.DisplayName,
			PrimaryEmail: user.PrimaryEmail,
			Locale:       user.Locale,
			CreatedAt:    user.CreatedAt.UTC(),
			UpdatedAt:    user.UpdatedAt.UTC(),
		}
		account, err := tx.GetLocalAccountByUserID(ctx, user.ID)
		if err != nil {
			if errors.Is(err, commonapi.ErrNotFound) {
				return nil
			}
			return err
		}
		profile.LocalAccount = &CurrentLocalAccountProfile{Username: account.Username}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return profile, nil
}

func (s *UserSelfService) RotateCurrentPassword(
	ctx context.Context,
	accessToken string,
	currentPassword string,
	newPassword string,
	audit AuditMetadata,
) (*PasswordRotationResult, error) {
	if s == nil || s.identityService == nil {
		return nil, fmt.Errorf("%w: user self service is required", commonapi.ErrBadRequest)
	}
	return s.identityService.RotateCurrentUserPassword(
		ctx,
		accessToken,
		currentPassword,
		newPassword,
		audit,
	)
}
