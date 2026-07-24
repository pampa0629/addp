package iam

import (
	"context"
	"fmt"

	commonapi "github.com/addp/common/api"
)

type BrowserContextOption struct {
	AvailableContext
	Current        bool
	RequiresStepUp bool
}

type ContextOptionsService struct {
	repository *Repository
}

func NewContextOptionsService(repository *Repository) (*ContextOptionsService, error) {
	if repository == nil {
		return nil, fmt.Errorf("%w: context options repository is required", commonapi.ErrBadRequest)
	}
	return &ContextOptionsService{repository: repository}, nil
}

func (s *ContextOptionsService) ListBrowserContextOptions(
	ctx context.Context,
	accessToken string,
) ([]BrowserContextOption, error) {
	if s == nil || s.repository == nil {
		return nil, fmt.Errorf("%w: context options service is required", commonapi.ErrBadRequest)
	}

	options := make([]BrowserContextOption, 0)
	err := s.repository.ReadOnlyRepeatableReadTransaction(ctx, func(tx *Repository) error {
		snapshot, err := resolveFirstPartyAccessTokenSnapshot(ctx, tx, accessToken)
		if err != nil {
			return err
		}
		hasPlatformRole, err := tx.HasEffectivePlatformRole(ctx, snapshot.FamilyPrincipalID, snapshot.DatabaseTime)
		if err != nil {
			return err
		}
		if hasPlatformRole {
			options = append(options, BrowserContextOption{
				AvailableContext: AvailableContext{Type: ContextTypePlatform},
				Current:          snapshot.FamilyContextType == ContextTypePlatform,
				RequiresStepUp: snapshot.FamilyAssuranceLevel != AssuranceLevelAAL2 &&
					snapshot.FamilyAssuranceLevel != AssuranceLevelAAL3,
			})
		}

		memberships, err := tx.ListEffectiveTenantMemberships(ctx, snapshot.FamilyPrincipalID, snapshot.DatabaseTime)
		if err != nil {
			return err
		}
		for _, membership := range memberships {
			tenantID := membership.TenantID
			membershipID := membership.MembershipID
			options = append(options, BrowserContextOption{
				AvailableContext: AvailableContext{
					Type:               ContextTypeTenant,
					TenantID:           &tenantID,
					TenantMembershipID: &membershipID,
					TenantCode:         membership.TenantCode,
					TenantName:         membership.TenantName,
				},
				Current: snapshot.FamilyContextType == ContextTypeTenant &&
					snapshot.FamilyTenantMembershipID != nil &&
					*snapshot.FamilyTenantMembershipID == membershipID,
			})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return options, nil
}
