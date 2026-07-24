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
