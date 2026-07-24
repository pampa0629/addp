package iam

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	commonapi "github.com/addp/common/api"
)

var ErrBrowserContextSwitchConflict = fmt.Errorf(
	"%w: browser session state changed during context switch",
	commonapi.ErrConflict,
)

const browserContextSwitchRevocationReason = "browser_context_switched"

type SwitchBrowserContextInput struct {
	AccessToken  string
	RefreshToken string
	Target       ContextSelectionChoice
	Audit        AuditMetadata
}

type ContextSwitchService struct {
	repository   *Repository
	tokenService *TokenFamilyService
}

func NewContextSwitchService(
	repository *Repository,
	tokenService *TokenFamilyService,
) (*ContextSwitchService, error) {
	if repository == nil || tokenService == nil {
		return nil, fmt.Errorf("%w: context switch dependencies are required", commonapi.ErrBadRequest)
	}
	return &ContextSwitchService{repository: repository, tokenService: tokenService}, nil
}

func (s *ContextSwitchService) SwitchBrowserContext(
	ctx context.Context,
	input SwitchBrowserContextInput,
) (*IssuedBrowserSession, error) {
	if s == nil || s.repository == nil || s.tokenService == nil {
		return nil, fmt.Errorf("%w: context switch service is required", commonapi.ErrBadRequest)
	}
	if err := validateContextSwitchChoice(input.Target); err != nil {
		return nil, err
	}
	if !strings.HasPrefix(input.AccessToken, "addp_at_") || len(input.AccessToken) == len("addp_at_") ||
		!strings.HasPrefix(input.RefreshToken, "addp_rt_") || len(input.RefreshToken) == len("addp_rt_") {
		return nil, commonapi.ErrUnauthorized
	}

	accessHash := hashOpaqueToken(input.AccessToken)
	refreshHash := hashOpaqueToken(input.RefreshToken)
	accessSnapshot, err := s.repository.GetAccessTokenByHash(ctx, accessHash)
	if err != nil {
		return nil, hideTokenLookupError(err)
	}
	refreshSnapshot, err := s.repository.GetRefreshTokenByHash(ctx, refreshHash)
	if err != nil {
		return nil, hideTokenLookupError(err)
	}
	if accessSnapshot.FamilyID != refreshSnapshot.FamilyID {
		return nil, commonapi.ErrUnauthorized
	}
	if refreshSnapshot.IssuedAccessTokenID != accessSnapshot.ID {
		return nil, commonapi.ErrUnauthorized
	}
	familySnapshot, err := s.repository.GetRefreshTokenFamily(ctx, accessSnapshot.FamilyID)
	if err != nil {
		return nil, hideTokenLookupError(err)
	}
	sourceContext, err := s.tokenService.resolveFamilyContext(ctx, familySnapshot)
	if err != nil {
		return nil, hideTokenLookupError(err)
	}
	now := s.tokenService.now().UTC()
	if accessSnapshot.RevokedAt != nil || !accessSnapshot.ExpiresAt.After(now) ||
		refreshSnapshot.UsedAt != nil || refreshSnapshot.RevokedAt != nil || !refreshSnapshot.ExpiresAt.After(now) ||
		familySnapshot.RevokedAt != nil || !familySnapshot.ExpiresAt.After(now) ||
		familySnapshot.AuthType != "first_party" || familySnapshot.ClientID != "addp-web" {
		return nil, commonapi.ErrUnauthorized
	}
	if sameSessionContext(sourceContext, input.Target) {
		return nil, fmt.Errorf("%w: target context is already active", commonapi.ErrConflict)
	}

	var session *IssuedBrowserSession
	err = s.repository.Transaction(ctx, func(tx *Repository) error {
		principal, err := tx.LockPrincipal(ctx, familySnapshot.PrincipalID)
		if err != nil {
			return hideTokenLookupError(err)
		}
		now := s.tokenService.now().UTC()
		targetContext, err := s.resolveTargetContext(ctx, tx, principal, familySnapshot, input.Target, now)
		if err != nil {
			return err
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

		if err := validateContextSwitchSource(
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
		if sameResolvedSessionContext(sourceContext, targetContext) {
			return fmt.Errorf("%w: target context is already active", commonapi.ErrConflict)
		}

		if err := tx.RevokeTokenFamily(ctx, family.ID, now, browserContextSwitchRevocationReason); err != nil {
			return err
		}
		familyExpiresAt := family.ExpiresAt
		session, err = s.tokenService.createBrowserSessionTx(ctx, tx, browserSessionIssueInput{
			Principal: principal,
			Context:   targetContext,
			Authentication: SessionAuthentication{
				Methods:         []string(family.AuthenticationMethods),
				AssuranceLevel:  family.AssuranceLevel,
				AuthenticatedAt: family.AuthenticatedAt,
				StepUpExpiresAt: utcTimePointer(family.StepUpExpiresAt),
			},
			FamilyExpiresAt: &familyExpiresAt,
			Mode:            BrowserSessionIssueModeContextSwitch,
		})
		if err != nil {
			return err
		}

		return writeTokenFamilyAudit(ctx, tx, input.Audit, principal, family, sourceContext, AuditEvent{
			EventName:  "iam.browser_context.switched",
			Result:     AuditResultSucceeded,
			RiskLevel:  AuditRiskMedium,
			ModuleName: "system",
			EntityType: "token_family",
			EntityID:   strconv.FormatInt(family.ID, 10),
			Details: map[string]any{
				"source_family_id":      family.ID,
				"replacement_family_id": session.FamilyID,
				"old_context_type":      sourceContext.Type,
				"old_tenant_id":         sourceContext.TenantID,
				"new_context_type":      targetContext.Type,
				"new_tenant_id":         targetContext.TenantID,
				"assurance_level":       family.AssuranceLevel,
				"authorization_version": family.IssuedAuthorizationVersion,
				"family_expires_at":     family.ExpiresAt,
			},
		})
	})
	if err != nil {
		return nil, err
	}
	return session, nil
}

func (s *ContextSwitchService) resolveTargetContext(
	ctx context.Context,
	tx *Repository,
	principal *Principal,
	family *RefreshTokenFamily,
	choice ContextSelectionChoice,
	now time.Time,
) (ResolvedSessionContext, error) {
	switch choice.Type {
	case ContextTypePlatform:
		if family.AssuranceLevel != AssuranceLevelAAL2 && family.AssuranceLevel != AssuranceLevelAAL3 {
			return ResolvedSessionContext{}, ErrStepUpRequired
		}
		hasPlatformRole, err := tx.HasEffectivePlatformRole(ctx, principal.ID, now)
		if err != nil {
			return ResolvedSessionContext{}, err
		}
		if !hasPlatformRole {
			return ResolvedSessionContext{}, commonapi.ErrForbidden
		}
		return ResolvedSessionContext{Type: ContextTypePlatform}, nil
	case ContextTypeTenant:
		return resolveTenantSessionContext(ctx, tx, principal.ID, *choice.TenantMembershipID, now)
	default:
		return ResolvedSessionContext{}, fmt.Errorf("%w: unsupported context choice", commonapi.ErrBadRequest)
	}
}

func validateContextSwitchSource(
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
		!family.ExpiresAt.After(now) || !accessToken.ExpiresAt.After(now) || !refreshToken.ExpiresAt.After(now) {
		return commonapi.ErrUnauthorized
	}
	if family.RevokedAt != nil || accessToken.RevokedAt != nil || refreshToken.UsedAt != nil || refreshToken.RevokedAt != nil {
		if family.RevokedReason != nil && *family.RevokedReason == browserLogoutRevocationReason {
			return commonapi.ErrUnauthorized
		}
		if familySnapshot.RevokedAt == nil && accessSnapshot.RevokedAt == nil &&
			refreshSnapshot.UsedAt == nil && refreshSnapshot.RevokedAt == nil {
			return ErrBrowserContextSwitchConflict
		}
		return commonapi.ErrUnauthorized
	}
	return nil
}

func validateContextSwitchChoice(choice ContextSelectionChoice) error {
	switch choice.Type {
	case ContextTypePlatform:
		if choice.TenantMembershipID != nil {
			return fmt.Errorf("%w: platform choice cannot include membership", commonapi.ErrBadRequest)
		}
	case ContextTypeTenant:
		if choice.TenantMembershipID == nil || *choice.TenantMembershipID <= 0 {
			return fmt.Errorf("%w: tenant choice requires membership", commonapi.ErrBadRequest)
		}
	default:
		return fmt.Errorf("%w: unsupported context choice", commonapi.ErrBadRequest)
	}
	return nil
}

func sameSessionContext(current ResolvedSessionContext, target ContextSelectionChoice) bool {
	if current.Type != target.Type {
		return false
	}
	if current.Type == ContextTypePlatform {
		return true
	}
	return current.TenantMembershipID != nil && target.TenantMembershipID != nil &&
		*current.TenantMembershipID == *target.TenantMembershipID
}

func sameResolvedSessionContext(left ResolvedSessionContext, right ResolvedSessionContext) bool {
	if left.Type != right.Type {
		return false
	}
	if left.Type == ContextTypePlatform {
		return true
	}
	return left.TenantMembershipID != nil && right.TenantMembershipID != nil &&
		*left.TenantMembershipID == *right.TenantMembershipID
}
