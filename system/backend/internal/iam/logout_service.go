package iam

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	commonapi "github.com/addp/common/api"
)

const browserLogoutRevocationReason = "browser_logout"

type LogoutBrowserSessionInput struct {
	AccessToken  string
	RefreshToken string
	Audit        AuditMetadata
}

type LogoutService struct {
	repository   *Repository
	tokenService *TokenFamilyService
}

func NewLogoutService(repository *Repository, tokenService *TokenFamilyService) (*LogoutService, error) {
	if repository == nil || tokenService == nil {
		return nil, fmt.Errorf("%w: logout dependencies are required", commonapi.ErrBadRequest)
	}
	return &LogoutService{repository: repository, tokenService: tokenService}, nil
}

func (s *LogoutService) LogoutBrowserSession(ctx context.Context, input LogoutBrowserSessionInput) error {
	if s == nil || s.repository == nil || s.tokenService == nil {
		return fmt.Errorf("%w: logout service is required", commonapi.ErrBadRequest)
	}
	if !strings.HasPrefix(input.AccessToken, "addp_at_") || len(input.AccessToken) == len("addp_at_") ||
		!strings.HasPrefix(input.RefreshToken, "addp_rt_") || len(input.RefreshToken) == len("addp_rt_") {
		return commonapi.ErrUnauthorized
	}

	accessHash := hashOpaqueToken(input.AccessToken)
	refreshHash := hashOpaqueToken(input.RefreshToken)
	accessSnapshot, err := s.repository.GetAccessTokenByHash(ctx, accessHash)
	if err != nil {
		return hideTokenLookupError(err)
	}
	refreshSnapshot, err := s.repository.GetRefreshTokenByHash(ctx, refreshHash)
	if err != nil {
		return hideTokenLookupError(err)
	}
	if accessSnapshot.FamilyID != refreshSnapshot.FamilyID ||
		refreshSnapshot.IssuedAccessTokenID != accessSnapshot.ID {
		return commonapi.ErrUnauthorized
	}
	familySnapshot, err := s.repository.GetRefreshTokenFamily(ctx, accessSnapshot.FamilyID)
	if err != nil {
		return hideTokenLookupError(err)
	}
	resolvedContext, err := s.tokenService.resolveFamilyContext(ctx, familySnapshot)
	if err != nil {
		return hideTokenLookupError(err)
	}

	return s.repository.Transaction(ctx, func(tx *Repository) error {
		principal, err := tx.LockPrincipal(ctx, familySnapshot.PrincipalID)
		if err != nil {
			return hideTokenLookupError(err)
		}
		family, err := tx.LockRefreshTokenFamily(ctx, familySnapshot.ID)
		if err != nil {
			return hideTokenLookupError(err)
		}
		refreshToken, err := tx.LockRefreshTokenByHash(ctx, refreshHash)
		if err != nil {
			return hideTokenLookupError(err)
		}
		accessToken, err := tx.LockAccessToken(ctx, accessSnapshot.ID)
		if err != nil {
			return hideTokenLookupError(err)
		}
		if _, err := tx.LockActiveResourceAccessTickets(ctx, family.ID); err != nil {
			return err
		}

		now := s.tokenService.now().UTC()
		if err := validateBrowserLogout(
			principal,
			family,
			familySnapshot,
			accessToken,
			accessSnapshot,
			refreshToken,
			refreshSnapshot,
			now,
		); err != nil {
			return err
		}
		if err := tx.RevokeTokenFamily(ctx, family.ID, now, browserLogoutRevocationReason); err != nil {
			return err
		}
		return writeTokenFamilyAudit(ctx, tx, input.Audit, principal, family, resolvedContext, AuditEvent{
			EventName:  "iam.browser_session.logged_out",
			Result:     AuditResultSucceeded,
			RiskLevel:  AuditRiskMedium,
			ModuleName: "system",
			EntityType: "token_family",
			EntityID:   strconv.FormatInt(family.ID, 10),
			Details: map[string]any{
				"client_id":             family.ClientID,
				"context_type":          family.ContextType,
				"authorization_version": family.IssuedAuthorizationVersion,
				"revocation_reason":     browserLogoutRevocationReason,
			},
		})
	})
}

func validateBrowserLogout(
	principal *Principal,
	family *RefreshTokenFamily,
	familySnapshot *RefreshTokenFamily,
	accessToken *AccessToken,
	accessSnapshot *AccessToken,
	refreshToken *RefreshToken,
	refreshSnapshot *RefreshToken,
	now time.Time,
) error {
	if principal == nil || family == nil || familySnapshot == nil || accessToken == nil || accessSnapshot == nil ||
		refreshToken == nil || refreshSnapshot == nil {
		return commonapi.ErrUnauthorized
	}
	if family.ID != familySnapshot.ID || family.PrincipalID != principal.ID ||
		accessToken.ID != accessSnapshot.ID || accessToken.FamilyID != family.ID ||
		refreshToken.ID != refreshSnapshot.ID || refreshToken.FamilyID != family.ID ||
		refreshToken.IssuedAccessTokenID != accessToken.ID {
		return commonapi.ErrUnauthorized
	}
	if principal.PrincipalType != PrincipalTypeUser || principal.Status != PrincipalStatusActive ||
		principal.AuthorizationVersion != family.IssuedAuthorizationVersion ||
		family.AuthType != "first_party" || family.ClientID != "addp-web" ||
		family.RevokedAt != nil || !family.ExpiresAt.After(now) ||
		accessToken.RevokedAt != nil || !accessToken.ExpiresAt.After(now) ||
		refreshToken.UsedAt != nil || refreshToken.RevokedAt != nil || !refreshToken.ExpiresAt.After(now) {
		return commonapi.ErrUnauthorized
	}
	return nil
}
