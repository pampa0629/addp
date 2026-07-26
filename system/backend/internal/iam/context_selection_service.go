package iam

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/lib/pq"
)

var ErrStepUpRequired = errors.New("需要增强认证")

type ContextSelectionNextAction string

const (
	ContextSelectionNextActionSessionIssued ContextSelectionNextAction = "session_issued"
	ContextSelectionNextActionSelectContext ContextSelectionNextAction = "select_context"
	ContextSelectionNextActionVerifyMFA     ContextSelectionNextAction = "verify_mfa"
)

type AvailableContext struct {
	Type               ContextType
	TenantID           *int64
	TenantMembershipID *int64
	TenantCode         string
	TenantName         string
}

type ContextSelectionChallenge struct {
	SelectionTicket string
	ExpiresAt       time.Time
	Contexts        []AvailableContext
}

type ContextSelectionResult struct {
	NextAction ContextSelectionNextAction
	Session    *IssuedBrowserSession
	Challenge  *ContextSelectionChallenge
	MFA        *IssuedMFAChallenge
}

type BeginContextSelectionInput struct {
	PrincipalID    int64
	Authentication SessionAuthentication
	Audit          AuditMetadata
}

type ContextSelectionChoice struct {
	Type               ContextType
	TenantMembershipID *int64
}

type ConsumeContextSelectionInput struct {
	SelectionTicket string
	Choice          ContextSelectionChoice
	Audit           AuditMetadata
}

type ContextSelectionService struct {
	repository   *Repository
	tokenService *TokenFamilyService
}

func NewContextSelectionService(
	repository *Repository,
	tokenService *TokenFamilyService,
) (*ContextSelectionService, error) {
	if repository == nil || tokenService == nil {
		return nil, fmt.Errorf("%w: context selection dependencies are required", commonapi.ErrBadRequest)
	}
	return &ContextSelectionService{repository: repository, tokenService: tokenService}, nil
}

func (s *ContextSelectionService) BeginContextSelection(
	ctx context.Context,
	input BeginContextSelectionInput,
) (*ContextSelectionResult, error) {
	if input.PrincipalID <= 0 {
		return nil, fmt.Errorf("%w: principal is required", commonapi.ErrBadRequest)
	}
	methods, err := normalizeAuthenticationMethods(input.Authentication.Methods)
	if err != nil {
		return nil, err
	}
	if err := validateAssuranceLevel(input.Authentication.AssuranceLevel); err != nil {
		return nil, err
	}
	now := s.tokenService.now().UTC()
	if input.Authentication.AuthenticatedAt.IsZero() || input.Authentication.AuthenticatedAt.After(now) {
		return nil, fmt.Errorf("%w: authenticated time must not be in the future", commonapi.ErrBadRequest)
	}
	if input.Authentication.StepUpExpiresAt != nil && input.Authentication.StepUpExpiresAt.Before(input.Authentication.AuthenticatedAt) {
		return nil, fmt.Errorf("%w: step-up expiry cannot precede authentication", commonapi.ErrBadRequest)
	}
	authentication := input.Authentication
	authentication.Methods = methods
	authentication.AuthenticatedAt = authentication.AuthenticatedAt.UTC()

	var result *ContextSelectionResult
	err = s.repository.Transaction(ctx, func(tx *Repository) error {
		principal, err := tx.LockPrincipal(ctx, input.PrincipalID)
		if err != nil {
			return err
		}
		if principal.PrincipalType != PrincipalTypeUser || principal.Status != PrincipalStatusActive {
			return fmt.Errorf("%w: context selection requires an active user", commonapi.ErrForbidden)
		}
		contexts, platformRoleExists, err := availableContexts(ctx, tx, principal.ID, authentication.AssuranceLevel, now)
		if err != nil {
			return err
		}
		if len(contexts) == 0 {
			if platformRoleExists {
				return ErrStepUpRequired
			}
			return fmt.Errorf("%w: principal has no available context", commonapi.ErrForbidden)
		}
		if len(contexts) == 1 {
			resolved, err := resolveAvailableContext(ctx, tx, principal, contexts[0], now)
			if err != nil {
				return err
			}
			session, err := s.tokenService.issueBrowserSessionTx(ctx, tx, browserSessionIssueInput{
				Principal:      principal,
				Context:        resolved,
				Authentication: authentication,
				Mode:           BrowserSessionIssueModeDirect,
				Audit:          input.Audit,
			})
			if err != nil {
				return err
			}
			result = &ContextSelectionResult{
				NextAction: ContextSelectionNextActionSessionIssued,
				Session:    session,
			}
			return nil
		}

		plainTicket, err := s.tokenService.generate("addp_cst_")
		if err != nil {
			return fmt.Errorf("generate context selection ticket: %w", err)
		}
		expiresAt := now.Add(s.tokenService.config.ContextSelectionTicketTTL)
		ticket := &ContextSelectionTicket{
			TokenHash:                  hashOpaqueToken(plainTicket),
			PrincipalID:                principal.ID,
			IssuedAuthorizationVersion: principal.AuthorizationVersion,
			ClientID:                   "addp-web",
			AuthenticationMethods:      pq.StringArray(methods),
			AssuranceLevel:             authentication.AssuranceLevel,
			AuthenticatedAt:            authentication.AuthenticatedAt,
			StepUpExpiresAt:            utcTimePointer(authentication.StepUpExpiresAt),
			ExpiresAt:                  expiresAt,
			CreatedAt:                  now,
		}
		if err := tx.CreateContextSelectionTicket(ctx, ticket); err != nil {
			return err
		}
		for _, available := range contexts {
			if err := tx.CreateContextSelectionOption(ctx, &ContextSelectionOption{
				TicketID:           ticket.ID,
				ContextType:        available.Type,
				TenantMembershipID: available.TenantMembershipID,
				CreatedAt:          now,
			}); err != nil {
				return err
			}
		}

		auditMetadata := authenticationAuditMetadata(input.Audit, principal.ID)
		if err := NewAuditWriter(tx).Write(ctx, AuditEvent{
			Metadata:   auditMetadata,
			EventName:  "iam.context_selection.ticket_issued",
			Result:     AuditResultSucceeded,
			RiskLevel:  AuditRiskLow,
			ModuleName: "system",
			EntityType: "context_selection_ticket",
			EntityID:   strconv.FormatInt(ticket.ID, 10),
			Details: map[string]any{
				"context_count":   len(contexts),
				"assurance_level": authentication.AssuranceLevel,
				"expires_at":      expiresAt,
			},
		}); err != nil {
			return err
		}
		result = &ContextSelectionResult{
			NextAction: ContextSelectionNextActionSelectContext,
			Challenge: &ContextSelectionChallenge{
				SelectionTicket: plainTicket,
				ExpiresAt:       expiresAt,
				Contexts:        contexts,
			},
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (s *ContextSelectionService) ConsumeContextSelection(
	ctx context.Context,
	input ConsumeContextSelectionInput,
) (*IssuedBrowserSession, error) {
	if !strings.HasPrefix(input.SelectionTicket, "addp_cst_") || len(input.SelectionTicket) == len("addp_cst_") {
		return nil, commonapi.ErrUnauthorized
	}
	tokenHash := hashOpaqueToken(input.SelectionTicket)
	ticketSnapshot, err := s.repository.GetContextSelectionTicketByHash(ctx, tokenHash)
	if err != nil {
		if errors.Is(err, commonapi.ErrNotFound) {
			return nil, commonapi.ErrUnauthorized
		}
		return nil, err
	}

	var session *IssuedBrowserSession
	err = s.repository.Transaction(ctx, func(tx *Repository) error {
		principal, err := tx.LockPrincipal(ctx, ticketSnapshot.PrincipalID)
		if err != nil {
			return err
		}
		ticket, err := tx.LockContextSelectionTicketByHash(ctx, tokenHash)
		if err != nil {
			return err
		}
		now := s.tokenService.now().UTC()
		if ticket.ID != ticketSnapshot.ID || ticket.PrincipalID != principal.ID {
			return commonapi.ErrUnauthorized
		}
		if ticket.ConsumedAt != nil {
			return fmt.Errorf("%w: context selection ticket is already consumed", commonapi.ErrConflict)
		}
		if !ticket.ExpiresAt.After(now) {
			return commonapi.ErrUnauthorized
		}
		if principal.PrincipalType != PrincipalTypeUser || principal.Status != PrincipalStatusActive ||
			ticket.IssuedAuthorizationVersion != principal.AuthorizationVersion {
			return commonapi.ErrUnauthorized
		}

		resolved, err := s.resolveSelectedContext(ctx, tx, principal, ticket, input.Choice, now)
		if err != nil {
			return err
		}
		if err := tx.ConsumeContextSelectionTicket(ctx, ticket.ID, now); err != nil {
			return err
		}
		session, err = s.tokenService.issueBrowserSessionTx(ctx, tx, browserSessionIssueInput{
			Principal: principal,
			Context:   resolved,
			Authentication: SessionAuthentication{
				Methods:         []string(ticket.AuthenticationMethods),
				AssuranceLevel:  ticket.AssuranceLevel,
				AuthenticatedAt: ticket.AuthenticatedAt,
				StepUpExpiresAt: utcTimePointer(ticket.StepUpExpiresAt),
			},
			Mode:  BrowserSessionIssueModeContextSelection,
			Audit: input.Audit,
		})
		return err
	})
	if err != nil {
		return nil, err
	}
	return session, nil
}

func (s *ContextSelectionService) resolveSelectedContext(
	ctx context.Context,
	tx *Repository,
	principal *Principal,
	ticket *ContextSelectionTicket,
	choice ContextSelectionChoice,
	now time.Time,
) (ResolvedSessionContext, error) {
	switch choice.Type {
	case ContextTypePlatform:
		if choice.TenantMembershipID != nil {
			return ResolvedSessionContext{}, fmt.Errorf("%w: platform choice cannot include membership", commonapi.ErrBadRequest)
		}
		if _, err := tx.GetContextSelectionOption(ctx, ticket.ID, ContextTypePlatform, nil); err != nil {
			return ResolvedSessionContext{}, fmt.Errorf("%w: platform context was not offered", commonapi.ErrBadRequest)
		}
		if ticket.AssuranceLevel != AssuranceLevelAAL2 && ticket.AssuranceLevel != AssuranceLevelAAL3 {
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
		if choice.TenantMembershipID == nil || *choice.TenantMembershipID <= 0 {
			return ResolvedSessionContext{}, fmt.Errorf("%w: tenant choice requires membership", commonapi.ErrBadRequest)
		}
		if _, err := tx.GetContextSelectionOption(
			ctx, ticket.ID, ContextTypeTenant, choice.TenantMembershipID,
		); err != nil {
			return ResolvedSessionContext{}, fmt.Errorf("%w: tenant context was not offered", commonapi.ErrBadRequest)
		}
		return resolveTenantSessionContext(ctx, tx, principal.ID, *choice.TenantMembershipID, now)
	default:
		return ResolvedSessionContext{}, fmt.Errorf("%w: unsupported context choice", commonapi.ErrBadRequest)
	}
}

func availableContexts(
	ctx context.Context,
	tx *Repository,
	principalID int64,
	assuranceLevel AssuranceLevel,
	at time.Time,
) ([]AvailableContext, bool, error) {
	hasPlatformRole, err := tx.HasEffectivePlatformRole(ctx, principalID, at)
	if err != nil {
		return nil, false, err
	}
	memberships, err := tx.ListEffectiveTenantMemberships(ctx, principalID, at)
	if err != nil {
		return nil, false, err
	}
	contexts := make([]AvailableContext, 0, len(memberships)+1)
	if hasPlatformRole && (assuranceLevel == AssuranceLevelAAL2 || assuranceLevel == AssuranceLevelAAL3) {
		contexts = append(contexts, AvailableContext{Type: ContextTypePlatform})
	}
	for _, membership := range memberships {
		tenantID := membership.TenantID
		membershipID := membership.MembershipID
		contexts = append(contexts, AvailableContext{
			Type:               ContextTypeTenant,
			TenantID:           &tenantID,
			TenantMembershipID: &membershipID,
			TenantCode:         membership.TenantCode,
			TenantName:         membership.TenantName,
		})
	}
	return contexts, hasPlatformRole, nil
}

func resolveAvailableContext(
	ctx context.Context,
	tx *Repository,
	principal *Principal,
	available AvailableContext,
	now time.Time,
) (ResolvedSessionContext, error) {
	if available.Type == ContextTypePlatform {
		return ResolvedSessionContext{Type: ContextTypePlatform}, nil
	}
	if available.Type != ContextTypeTenant || available.TenantMembershipID == nil {
		return ResolvedSessionContext{}, fmt.Errorf("%w: invalid available context", commonapi.ErrBadRequest)
	}
	return resolveTenantSessionContext(ctx, tx, principal.ID, *available.TenantMembershipID, now)
}

func resolveTenantSessionContext(
	ctx context.Context,
	tx *Repository,
	principalID int64,
	membershipID int64,
	now time.Time,
) (ResolvedSessionContext, error) {
	membership, err := tx.LockTenantMembershipByID(ctx, membershipID)
	if err != nil {
		return ResolvedSessionContext{}, err
	}
	if membership.PrincipalID != principalID || membership.Status != TenantMembershipStatusActive ||
		membership.JoinedAt.After(now) || (membership.ExpiresAt != nil && !membership.ExpiresAt.After(now)) {
		return ResolvedSessionContext{}, commonapi.ErrForbidden
	}
	tenant, err := tx.LockTenant(ctx, membership.TenantID)
	if err != nil {
		return ResolvedSessionContext{}, err
	}
	if tenant.Status != TenantStatusActive {
		return ResolvedSessionContext{}, commonapi.ErrForbidden
	}
	tenantID := tenant.ID
	resolvedMembershipID := membership.ID
	return ResolvedSessionContext{
		Type:               ContextTypeTenant,
		TenantID:           &tenantID,
		TenantMembershipID: &resolvedMembershipID,
	}, nil
}
