package iam

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/lib/pq"
)

const (
	defaultContextSelectionTicketTTL = 5 * time.Minute
	defaultAccessTokenTTL            = 15 * time.Minute
	defaultRefreshTokenFamilyTTL     = 30 * 24 * time.Hour
	defaultResourceAccessTicketTTL   = 15 * time.Minute
)

var resourceTicketOwnerPattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

var allowedAuthenticationMethods = map[string]struct{}{
	"password": {}, "totp": {}, "webauthn": {}, "external_idp": {},
	"recovery_code": {}, "service_secret": {}, "private_key_jwt": {}, "mtls": {},
}

var ErrRefreshTokenRotationConflict = fmt.Errorf(
	"%w: refresh token was rotated by another request",
	commonapi.ErrConflict,
)

const refreshTokenReuseRevocationReason = "refresh_token_reuse_detected"

type BrowserSessionConfig struct {
	ContextSelectionTicketTTL time.Duration
	AccessTokenTTL            time.Duration
	RefreshTokenFamilyTTL     time.Duration
	ResourceAccessTicketTTL   time.Duration
	ResourceTicketOwners      []string
}

type OpaqueTokenGenerator func(prefix string) (string, error)

type SessionAuthentication struct {
	Methods         []string
	AssuranceLevel  AssuranceLevel
	AuthenticatedAt time.Time
	StepUpExpiresAt *time.Time
}

type ResolvedSessionContext struct {
	Type               ContextType
	TenantID           *int64
	TenantMembershipID *int64
}

type BrowserSessionIssueMode string

const (
	BrowserSessionIssueModeDirect           BrowserSessionIssueMode = "direct"
	BrowserSessionIssueModeContextSelection BrowserSessionIssueMode = "context_selection"
	BrowserSessionIssueModeContextSwitch    BrowserSessionIssueMode = "context_switch"
)

type IssuedBrowserSession struct {
	FamilyID                    int64
	Context                     ResolvedSessionContext
	AccessToken                 string
	RefreshToken                string
	ResourceAccessTickets       map[string]string
	AccessTokenExpiresAt        time.Time
	RefreshTokenFamilyExpiresAt time.Time
	ResourceTicketExpiresAt     time.Time
}

type RotateBrowserRefreshTokenInput struct {
	RefreshToken string
	Audit        AuditMetadata
}

type browserSessionSecrets struct {
	accessToken          string
	refreshToken         string
	resourceAccessTicket map[string]string
}

type browserSessionIssueInput struct {
	Principal       *Principal
	Context         ResolvedSessionContext
	Authentication  SessionAuthentication
	FamilyExpiresAt *time.Time
	Mode            BrowserSessionIssueMode
	Audit           AuditMetadata
}

type TokenFamilyService struct {
	repository *Repository
	config     BrowserSessionConfig
	generate   OpaqueTokenGenerator
	now        func() time.Time
}

func NewTokenFamilyService(
	repository *Repository,
	config BrowserSessionConfig,
	generate OpaqueTokenGenerator,
	now func() time.Time,
) (*TokenFamilyService, error) {
	if repository == nil {
		return nil, fmt.Errorf("%w: IAM repository is required", commonapi.ErrBadRequest)
	}
	normalizedConfig, err := normalizeBrowserSessionConfig(config)
	if err != nil {
		return nil, err
	}
	if generate == nil {
		generate = generateOpaqueToken
	}
	if now == nil {
		now = time.Now
	}
	return &TokenFamilyService{
		repository: repository,
		config:     normalizedConfig,
		generate:   generate,
		now:        now,
	}, nil
}

func (s *TokenFamilyService) RotateBrowserRefreshToken(
	ctx context.Context,
	input RotateBrowserRefreshTokenInput,
) (*IssuedBrowserSession, error) {
	if s == nil || s.repository == nil {
		return nil, fmt.Errorf("%w: token family service is required", commonapi.ErrBadRequest)
	}
	if !strings.HasPrefix(input.RefreshToken, "addp_rt_") || len(input.RefreshToken) == len("addp_rt_") {
		return nil, commonapi.ErrUnauthorized
	}

	tokenHash := hashOpaqueToken(input.RefreshToken)
	tokenSnapshot, err := s.repository.GetRefreshTokenByHash(ctx, tokenHash)
	if err != nil {
		return nil, hideTokenLookupError(err)
	}
	familySnapshot, err := s.repository.GetRefreshTokenFamily(ctx, tokenSnapshot.FamilyID)
	if err != nil {
		return nil, hideTokenLookupError(err)
	}
	contextSnapshot, err := s.resolveFamilyContext(ctx, familySnapshot)
	if err != nil {
		return nil, hideTokenLookupError(err)
	}

	if tokenSnapshot.UsedAt != nil {
		if tokenSnapshot.ReplacedByTokenID == nil {
			return nil, fmt.Errorf("refresh token %d is used without a replacement", tokenSnapshot.ID)
		}
		err := s.handleBrowserRefreshTokenReuse(
			ctx,
			tokenHash,
			tokenSnapshot,
			familySnapshot,
			contextSnapshot,
			input.Audit,
		)
		if err != nil {
			return nil, err
		}
		return nil, commonapi.ErrUnauthorized
	}

	secrets, err := s.generateBrowserSessionSecrets()
	if err != nil {
		return nil, err
	}

	var session *IssuedBrowserSession
	err = s.repository.Transaction(ctx, func(tx *Repository) error {
		principal, err := tx.LockPrincipal(ctx, familySnapshot.PrincipalID)
		if err != nil {
			return hideTokenLookupError(err)
		}
		family, err := tx.LockRefreshTokenFamily(ctx, familySnapshot.ID)
		if err != nil {
			return hideTokenLookupError(err)
		}
		token, err := tx.LockRefreshTokenByHash(ctx, tokenHash)
		if err != nil {
			return hideTokenLookupError(err)
		}
		now := s.now().UTC()
		if err := validateRefreshTokenForRotation(principal, family, token, tokenSnapshot, now); err != nil {
			return err
		}
		if token.UsedAt != nil {
			if token.ReplacedByTokenID != nil {
				return ErrRefreshTokenRotationConflict
			}
			return fmt.Errorf("refresh token %d is used without a replacement", token.ID)
		}

		accessToken, err := tx.LockAccessToken(ctx, token.IssuedAccessTokenID)
		if err != nil {
			return hideTokenLookupError(err)
		}
		if accessToken.FamilyID != family.ID {
			return commonapi.ErrUnauthorized
		}
		if _, err := tx.LockActiveResourceAccessTickets(ctx, family.ID); err != nil {
			return err
		}

		if err := tx.MarkRefreshTokenUsed(ctx, token.ID, now); err != nil {
			return err
		}
		if err := tx.RevokeAccessToken(ctx, accessToken.ID, now); err != nil {
			return err
		}
		if err := tx.RevokeActiveResourceAccessTickets(ctx, family.ID, now); err != nil {
			return err
		}

		accessExpiresAt := earlierTime(now.Add(s.config.AccessTokenTTL), family.ExpiresAt)
		resourceExpiresAt := earlierTime(now.Add(s.config.ResourceAccessTicketTTL), accessExpiresAt)
		replacementAccessToken := &AccessToken{
			TokenHash: hashOpaqueToken(secrets.accessToken),
			FamilyID:  family.ID,
			ExpiresAt: accessExpiresAt,
			CreatedAt: now,
		}
		if err := tx.CreateAccessToken(ctx, replacementAccessToken); err != nil {
			return err
		}
		replacementRefreshToken := &RefreshToken{
			TokenHash:           hashOpaqueToken(secrets.refreshToken),
			FamilyID:            family.ID,
			IssuedAccessTokenID: replacementAccessToken.ID,
			ParentTokenID:       &token.ID,
			ExpiresAt:           family.ExpiresAt,
			CreatedAt:           now,
		}
		if err := tx.CreateRefreshToken(ctx, replacementRefreshToken); err != nil {
			return err
		}
		if err := tx.LinkRefreshTokenReplacement(ctx, token.ID, replacementRefreshToken.ID); err != nil {
			return err
		}
		for _, owner := range s.config.ResourceTicketOwners {
			if err := tx.CreateResourceAccessTicket(ctx, &ResourceAccessTicket{
				TokenHash: hashOpaqueToken(secrets.resourceAccessTicket[owner]),
				FamilyID:  family.ID,
				Owner:     owner,
				ExpiresAt: resourceExpiresAt,
				CreatedAt: now,
			}); err != nil {
				return err
			}
		}

		if err := writeTokenFamilyAudit(ctx, tx, input.Audit, principal, family, contextSnapshot, AuditEvent{
			EventName:  "iam.refresh_token.rotated",
			Result:     AuditResultSucceeded,
			RiskLevel:  AuditRiskMedium,
			ModuleName: "system",
			EntityType: "token_family",
			EntityID:   strconv.FormatInt(family.ID, 10),
			Details: map[string]any{
				"previous_refresh_token_id":    token.ID,
				"replacement_refresh_token_id": replacementRefreshToken.ID,
				"resource_ticket_count":        len(secrets.resourceAccessTicket),
				"authorization_version":        family.IssuedAuthorizationVersion,
				"family_expires_at":            family.ExpiresAt,
			},
		}); err != nil {
			return err
		}

		session = &IssuedBrowserSession{
			FamilyID:                    family.ID,
			Context:                     contextSnapshot,
			AccessToken:                 secrets.accessToken,
			RefreshToken:                secrets.refreshToken,
			ResourceAccessTickets:       secrets.resourceAccessTicket,
			AccessTokenExpiresAt:        accessExpiresAt,
			RefreshTokenFamilyExpiresAt: family.ExpiresAt,
			ResourceTicketExpiresAt:     resourceExpiresAt,
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return session, nil
}

func (s *TokenFamilyService) handleBrowserRefreshTokenReuse(
	ctx context.Context,
	tokenHash string,
	tokenSnapshot *RefreshToken,
	familySnapshot *RefreshTokenFamily,
	contextSnapshot ResolvedSessionContext,
	audit AuditMetadata,
) error {
	return s.repository.Transaction(ctx, func(tx *Repository) error {
		principal, err := tx.LockPrincipal(ctx, familySnapshot.PrincipalID)
		if err != nil {
			return hideTokenLookupError(err)
		}
		family, err := tx.LockRefreshTokenFamily(ctx, familySnapshot.ID)
		if err != nil {
			return hideTokenLookupError(err)
		}
		token, err := tx.LockRefreshTokenByHash(ctx, tokenHash)
		if err != nil {
			return hideTokenLookupError(err)
		}
		now := s.now().UTC()
		if err := validateRefreshTokenForRotation(principal, family, token, tokenSnapshot, now); err != nil {
			return err
		}
		if token.UsedAt == nil || token.ReplacedByTokenID == nil {
			return fmt.Errorf("refresh token %d does not have a completed replacement", token.ID)
		}
		if token.ReuseDetectedAt != nil {
			return commonapi.ErrUnauthorized
		}
		if err := tx.MarkRefreshTokenReuseDetected(ctx, token.ID, now); err != nil {
			return err
		}
		if err := tx.RevokeTokenFamily(ctx, family.ID, now, refreshTokenReuseRevocationReason); err != nil {
			return err
		}
		return writeTokenFamilyAudit(ctx, tx, audit, principal, family, contextSnapshot, AuditEvent{
			EventName:  "iam.refresh_token.reuse_detected",
			Result:     AuditResultSucceeded,
			RiskLevel:  AuditRiskHigh,
			ModuleName: "system",
			EntityType: "token_family",
			EntityID:   strconv.FormatInt(family.ID, 10),
			Details: map[string]any{
				"refresh_token_id":      token.ID,
				"client_id":             family.ClientID,
				"context_type":          family.ContextType,
				"authorization_version": family.IssuedAuthorizationVersion,
				"revocation_reason":     refreshTokenReuseRevocationReason,
			},
		})
	})
}

func (s *TokenFamilyService) generateBrowserSessionSecrets() (*browserSessionSecrets, error) {
	accessToken, err := s.generate("addp_at_")
	if err != nil {
		return nil, fmt.Errorf("generate access token: %w", err)
	}
	refreshToken, err := s.generate("addp_rt_")
	if err != nil {
		return nil, fmt.Errorf("generate refresh token: %w", err)
	}
	resourceAccessTickets := make(map[string]string, len(s.config.ResourceTicketOwners))
	for _, owner := range s.config.ResourceTicketOwners {
		ticket, err := s.generate("addp_rat_")
		if err != nil {
			return nil, fmt.Errorf("generate %s resource access ticket: %w", owner, err)
		}
		resourceAccessTickets[owner] = ticket
	}
	return &browserSessionSecrets{
		accessToken:          accessToken,
		refreshToken:         refreshToken,
		resourceAccessTicket: resourceAccessTickets,
	}, nil
}

func (s *TokenFamilyService) resolveFamilyContext(
	ctx context.Context,
	family *RefreshTokenFamily,
) (ResolvedSessionContext, error) {
	if family == nil {
		return ResolvedSessionContext{}, commonapi.ErrUnauthorized
	}
	switch family.ContextType {
	case ContextTypePlatform:
		if family.TenantMembershipID != nil {
			return ResolvedSessionContext{}, commonapi.ErrUnauthorized
		}
		return ResolvedSessionContext{Type: ContextTypePlatform}, nil
	case ContextTypeTenant:
		if family.TenantMembershipID == nil {
			return ResolvedSessionContext{}, commonapi.ErrUnauthorized
		}
		membership, err := s.repository.GetTenantMembershipByID(ctx, *family.TenantMembershipID)
		if err != nil {
			return ResolvedSessionContext{}, err
		}
		if membership.PrincipalID != family.PrincipalID {
			return ResolvedSessionContext{}, commonapi.ErrUnauthorized
		}
		tenantID := membership.TenantID
		membershipID := membership.ID
		return ResolvedSessionContext{
			Type:               ContextTypeTenant,
			TenantID:           &tenantID,
			TenantMembershipID: &membershipID,
		}, nil
	default:
		return ResolvedSessionContext{}, commonapi.ErrUnauthorized
	}
}

func validateRefreshTokenForRotation(
	principal *Principal,
	family *RefreshTokenFamily,
	token *RefreshToken,
	tokenSnapshot *RefreshToken,
	now time.Time,
) error {
	if principal == nil || family == nil || token == nil || tokenSnapshot == nil {
		return commonapi.ErrUnauthorized
	}
	if principal.ID != family.PrincipalID || principal.Status != PrincipalStatusActive ||
		principal.AuthorizationVersion != family.IssuedAuthorizationVersion ||
		family.AuthType != "first_party" || family.ClientID != "addp-web" ||
		family.RevokedAt != nil || !family.ExpiresAt.After(now) ||
		token.ID != tokenSnapshot.ID || token.FamilyID != family.ID ||
		token.RevokedAt != nil || token.ReuseDetectedAt != nil || !token.ExpiresAt.After(now) {
		return commonapi.ErrUnauthorized
	}
	return nil
}

func writeTokenFamilyAudit(
	ctx context.Context,
	tx *Repository,
	metadata AuditMetadata,
	principal *Principal,
	family *RefreshTokenFamily,
	resolvedContext ResolvedSessionContext,
	event AuditEvent,
) error {
	principalID := principal.ID
	principalType := principal.PrincipalType
	contextType := family.ContextType
	metadata.PrincipalID = &principalID
	metadata.PrincipalType = &principalType
	metadata.ContextType = &contextType
	metadata.TenantID = resolvedContext.TenantID
	event.Metadata = metadata
	return NewAuditWriter(tx).Write(ctx, event)
}

func hideTokenLookupError(err error) error {
	if errors.Is(err, commonapi.ErrNotFound) {
		return commonapi.ErrUnauthorized
	}
	return err
}

func (s *TokenFamilyService) issueBrowserSessionTx(
	ctx context.Context,
	tx *Repository,
	input browserSessionIssueInput,
) (*IssuedBrowserSession, error) {
	session, err := s.createBrowserSessionTx(ctx, tx, input)
	if err != nil {
		return nil, err
	}

	auditMetadata := input.Audit
	principalID := input.Principal.ID
	principalType := input.Principal.PrincipalType
	contextType := input.Context.Type
	auditMetadata.PrincipalID = &principalID
	auditMetadata.PrincipalType = &principalType
	auditMetadata.ContextType = &contextType
	auditMetadata.TenantID = input.Context.TenantID
	if err := NewAuditWriter(tx).Write(ctx, AuditEvent{
		Metadata:   auditMetadata,
		EventName:  "iam.browser_session.issued",
		Result:     AuditResultSucceeded,
		RiskLevel:  AuditRiskMedium,
		ModuleName: "system",
		EntityType: "token_family",
		EntityID:   strconv.FormatInt(session.FamilyID, 10),
		Details: map[string]any{
			"issue_mode":            input.Mode,
			"context_type":          input.Context.Type,
			"assurance_level":       input.Authentication.AssuranceLevel,
			"resource_ticket_count": len(session.ResourceAccessTickets),
			"authorization_version": input.Principal.AuthorizationVersion,
		},
	}); err != nil {
		return nil, err
	}
	return session, nil
}

func (s *TokenFamilyService) createBrowserSessionTx(
	ctx context.Context,
	tx *Repository,
	input browserSessionIssueInput,
) (*IssuedBrowserSession, error) {
	if s == nil || tx == nil || input.Principal == nil {
		return nil, fmt.Errorf("%w: browser session issue context is incomplete", commonapi.ErrBadRequest)
	}
	methods, err := normalizeAuthenticationMethods(input.Authentication.Methods)
	if err != nil {
		return nil, err
	}
	if err := validateAssuranceLevel(input.Authentication.AssuranceLevel); err != nil {
		return nil, err
	}
	now := s.now().UTC()
	if input.Authentication.AuthenticatedAt.IsZero() || input.Authentication.AuthenticatedAt.After(now) {
		return nil, fmt.Errorf("%w: authenticated time must not be in the future", commonapi.ErrBadRequest)
	}
	familyExpiresAt := now.Add(s.config.RefreshTokenFamilyTTL)
	if input.FamilyExpiresAt != nil {
		familyExpiresAt = input.FamilyExpiresAt.UTC()
	}
	if !familyExpiresAt.After(now) {
		return nil, commonapi.ErrUnauthorized
	}
	if input.Authentication.StepUpExpiresAt != nil &&
		(input.Authentication.StepUpExpiresAt.Before(input.Authentication.AuthenticatedAt) ||
			input.Authentication.StepUpExpiresAt.After(familyExpiresAt)) {
		return nil, fmt.Errorf("%w: step-up expiry is outside the session lifetime", commonapi.ErrBadRequest)
	}
	if input.Principal.Status != PrincipalStatusActive {
		return nil, fmt.Errorf("%w: principal must be active", commonapi.ErrConflict)
	}
	if input.Context.Type == ContextTypePlatform {
		if input.Context.TenantID != nil || input.Context.TenantMembershipID != nil {
			return nil, fmt.Errorf("%w: platform context cannot include a tenant", commonapi.ErrBadRequest)
		}
		if input.Principal.PrincipalType == PrincipalTypeUser &&
			input.Authentication.AssuranceLevel != AssuranceLevelAAL2 &&
			input.Authentication.AssuranceLevel != AssuranceLevelAAL3 {
			return nil, ErrStepUpRequired
		}
	} else if input.Context.Type == ContextTypeTenant {
		if input.Context.TenantID == nil || input.Context.TenantMembershipID == nil {
			return nil, fmt.Errorf("%w: tenant context requires tenant and membership", commonapi.ErrBadRequest)
		}
	} else {
		return nil, fmt.Errorf("%w: unsupported session context", commonapi.ErrBadRequest)
	}

	accessPlain, err := s.generate("addp_at_")
	if err != nil {
		return nil, fmt.Errorf("generate access token: %w", err)
	}
	refreshPlain, err := s.generate("addp_rt_")
	if err != nil {
		return nil, fmt.Errorf("generate refresh token: %w", err)
	}
	resourcePlain := make(map[string]string, len(s.config.ResourceTicketOwners))
	for _, owner := range s.config.ResourceTicketOwners {
		plain, err := s.generate("addp_rat_")
		if err != nil {
			return nil, fmt.Errorf("generate %s resource access ticket: %w", owner, err)
		}
		resourcePlain[owner] = plain
	}

	accessExpiresAt := earlierTime(now.Add(s.config.AccessTokenTTL), familyExpiresAt)
	resourceExpiresAt := earlierTime(now.Add(s.config.ResourceAccessTicketTTL), accessExpiresAt)
	family := &RefreshTokenFamily{
		PrincipalID:                input.Principal.ID,
		ContextType:                input.Context.Type,
		TenantMembershipID:         input.Context.TenantMembershipID,
		IssuedAuthorizationVersion: input.Principal.AuthorizationVersion,
		ClientID:                   "addp-web",
		AuthType:                   "first_party",
		Audiences:                  pq.StringArray{"addp.api"},
		Scopes:                     pq.StringArray{},
		AuthenticationMethods:      pq.StringArray(methods),
		AssuranceLevel:             input.Authentication.AssuranceLevel,
		AuthenticatedAt:            input.Authentication.AuthenticatedAt.UTC(),
		StepUpExpiresAt:            utcTimePointer(input.Authentication.StepUpExpiresAt),
		ExpiresAt:                  familyExpiresAt,
		CreatedAt:                  now,
		UpdatedAt:                  now,
	}
	if err := tx.CreateRefreshTokenFamily(ctx, family); err != nil {
		return nil, err
	}
	accessToken := &AccessToken{
		TokenHash: hashOpaqueToken(accessPlain),
		FamilyID:  family.ID,
		ExpiresAt: accessExpiresAt,
		CreatedAt: now,
	}
	if err := tx.CreateAccessToken(ctx, accessToken); err != nil {
		return nil, err
	}
	refreshToken := &RefreshToken{
		TokenHash:           hashOpaqueToken(refreshPlain),
		FamilyID:            family.ID,
		IssuedAccessTokenID: accessToken.ID,
		ExpiresAt:           familyExpiresAt,
		CreatedAt:           now,
	}
	if err := tx.CreateRefreshToken(ctx, refreshToken); err != nil {
		return nil, err
	}
	for _, owner := range s.config.ResourceTicketOwners {
		if err := tx.CreateResourceAccessTicket(ctx, &ResourceAccessTicket{
			TokenHash: hashOpaqueToken(resourcePlain[owner]),
			FamilyID:  family.ID,
			Owner:     owner,
			ExpiresAt: resourceExpiresAt,
			CreatedAt: now,
		}); err != nil {
			return nil, err
		}
	}

	return &IssuedBrowserSession{
		FamilyID:                    family.ID,
		Context:                     input.Context,
		AccessToken:                 accessPlain,
		RefreshToken:                refreshPlain,
		ResourceAccessTickets:       resourcePlain,
		AccessTokenExpiresAt:        accessExpiresAt,
		RefreshTokenFamilyExpiresAt: familyExpiresAt,
		ResourceTicketExpiresAt:     resourceExpiresAt,
	}, nil
}

func normalizeBrowserSessionConfig(config BrowserSessionConfig) (BrowserSessionConfig, error) {
	if config.ContextSelectionTicketTTL <= 0 {
		config.ContextSelectionTicketTTL = defaultContextSelectionTicketTTL
	}
	if config.AccessTokenTTL <= 0 {
		config.AccessTokenTTL = defaultAccessTokenTTL
	}
	if config.RefreshTokenFamilyTTL <= 0 {
		config.RefreshTokenFamilyTTL = defaultRefreshTokenFamilyTTL
	}
	if config.ResourceAccessTicketTTL <= 0 {
		config.ResourceAccessTicketTTL = defaultResourceAccessTicketTTL
	}
	if len(config.ResourceTicketOwners) == 0 {
		return BrowserSessionConfig{}, fmt.Errorf("%w: resource ticket owners are required", commonapi.ErrBadRequest)
	}
	seen := make(map[string]struct{}, len(config.ResourceTicketOwners))
	owners := make([]string, 0, len(config.ResourceTicketOwners))
	for _, rawOwner := range config.ResourceTicketOwners {
		owner := strings.TrimSpace(rawOwner)
		if !resourceTicketOwnerPattern.MatchString(owner) {
			return BrowserSessionConfig{}, fmt.Errorf("%w: invalid resource ticket owner %q", commonapi.ErrBadRequest, rawOwner)
		}
		if _, exists := seen[owner]; exists {
			return BrowserSessionConfig{}, fmt.Errorf("%w: duplicate resource ticket owner %q", commonapi.ErrBadRequest, owner)
		}
		seen[owner] = struct{}{}
		owners = append(owners, owner)
	}
	sort.Strings(owners)
	config.ResourceTicketOwners = owners
	return config, nil
}

func normalizeAuthenticationMethods(methods []string) ([]string, error) {
	if len(methods) == 0 {
		return nil, fmt.Errorf("%w: authentication methods are required", commonapi.ErrBadRequest)
	}
	seen := make(map[string]struct{}, len(methods))
	normalized := make([]string, 0, len(methods))
	for _, rawMethod := range methods {
		method := strings.TrimSpace(rawMethod)
		if method == "" {
			return nil, fmt.Errorf("%w: authentication method must not be empty", commonapi.ErrBadRequest)
		}
		if _, allowed := allowedAuthenticationMethods[method]; !allowed {
			return nil, fmt.Errorf("%w: unsupported authentication method %q", commonapi.ErrBadRequest, method)
		}
		if _, exists := seen[method]; exists {
			return nil, fmt.Errorf("%w: duplicate authentication method %q", commonapi.ErrBadRequest, method)
		}
		seen[method] = struct{}{}
		normalized = append(normalized, method)
	}
	sort.Strings(normalized)
	return normalized, nil
}

func validateAssuranceLevel(level AssuranceLevel) error {
	switch level {
	case AssuranceLevelAAL1, AssuranceLevelAAL2, AssuranceLevelAAL3:
		return nil
	default:
		return fmt.Errorf("%w: unsupported assurance level", commonapi.ErrBadRequest)
	}
}

func generateOpaqueToken(prefix string) (string, error) {
	randomValue := make([]byte, 32)
	if _, err := rand.Read(randomValue); err != nil {
		return "", err
	}
	return prefix + base64.RawURLEncoding.EncodeToString(randomValue), nil
}

func hashOpaqueToken(token string) string {
	digest := sha256.Sum256([]byte(token))
	return hex.EncodeToString(digest[:])
}

func earlierTime(left time.Time, right time.Time) time.Time {
	if left.Before(right) {
		return left
	}
	return right
}
