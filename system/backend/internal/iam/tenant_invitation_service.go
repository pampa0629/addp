package iam

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	commonapi "github.com/addp/common/api"
	passwordutils "github.com/addp/system/pkg/utils"
)

type TenantInvitationServiceConfig struct {
	InvitationTTL       time.Duration
	EnrollmentTicketTTL time.Duration
	Generate            OpaqueTokenGenerator
	Now                 func() time.Time
}

type CreateTenantInvitationInput struct {
	TenantID             int64
	Email                string
	CreatedByPrincipalID int64
	Audit                AuditMetadata
}

type CreatedTenantInvitation struct {
	Invitation TenantInvitation
	Secret     string
}

type IssueEnrollmentTicketInput struct {
	InvitationSecret string
	Username         string
	Password         string
	Audit            AuditMetadata
}

type IssuedEnrollmentTicket struct {
	EnrollmentTicket string
	ExpiresAt        time.Time
}

type RegisterTenantInvitationInput struct {
	InvitationSecret string
	Username         string
	Password         string
	DisplayName      string
	Locale           *string
	Audit            AuditMetadata
}

type AcceptTenantInvitationInput struct {
	InvitationSecret string
	PrincipalID      int64
	EnrollmentTicket string
	Authentication   SessionAuthentication
	Audit            AuditMetadata
}

type AcceptedTenantInvitation struct {
	Invitation TenantInvitation
	Membership TenantMembership
	Session    IssuedBrowserSession
}

type TenantInvitationService struct {
	repository   *Repository
	identity     *IdentityService
	tokenService *TokenFamilyService
	config       TenantInvitationServiceConfig
}

func NewTenantInvitationService(
	repository *Repository,
	identity *IdentityService,
	tokenService *TokenFamilyService,
	config TenantInvitationServiceConfig,
) (*TenantInvitationService, error) {
	if repository == nil || identity == nil || tokenService == nil {
		return nil, fmt.Errorf("%w: tenant invitation dependencies are required", commonapi.ErrBadRequest)
	}
	if config.InvitationTTL <= 0 || config.EnrollmentTicketTTL <= 0 {
		return nil, fmt.Errorf("%w: tenant invitation lifetimes must be positive", commonapi.ErrBadRequest)
	}
	if config.Generate == nil {
		config.Generate = generateOpaqueToken
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	return &TenantInvitationService{
		repository: repository, identity: identity, tokenService: tokenService, config: config,
	}, nil
}

func (s *TenantInvitationService) List(
	ctx context.Context,
	tenantID int64,
	page int,
	pageSize int,
	search string,
	status *TenantInvitationStatus,
	audit AuditMetadata,
) ([]TenantInvitation, int64, error) {
	if s == nil || s.repository == nil || tenantID <= 0 {
		return nil, 0, fmt.Errorf("%w: tenant invitation service and tenant are required", commonapi.ErrBadRequest)
	}
	if err := validateManagementPagination(page, pageSize); err != nil {
		return nil, 0, err
	}
	now := s.config.Now().UTC()
	if err := s.repository.Transaction(ctx, func(tx *Repository) error {
		return s.expireTenantInvitationsTx(ctx, tx, tenantID, now, audit)
	}); err != nil {
		return nil, 0, err
	}
	return s.repository.ListTenantInvitations(ctx, tenantID, page, pageSize, search, status)
}

func (s *TenantInvitationService) Get(
	ctx context.Context,
	tenantID int64,
	invitationID int64,
	audit AuditMetadata,
) (*TenantInvitation, error) {
	if s == nil || s.repository == nil || tenantID <= 0 || invitationID <= 0 {
		return nil, fmt.Errorf("%w: tenant and invitation are required", commonapi.ErrBadRequest)
	}
	now := s.config.Now().UTC()
	if err := s.repository.Transaction(ctx, func(tx *Repository) error {
		return s.expireTenantInvitationsTx(ctx, tx, tenantID, now, audit)
	}); err != nil {
		return nil, err
	}
	return s.repository.GetTenantInvitation(ctx, tenantID, invitationID)
}

func (s *TenantInvitationService) Create(
	ctx context.Context,
	input CreateTenantInvitationInput,
) (*CreatedTenantInvitation, error) {
	if s == nil || s.repository == nil || input.TenantID <= 0 || input.CreatedByPrincipalID <= 0 {
		return nil, fmt.Errorf("%w: tenant and invitation creator are required", commonapi.ErrBadRequest)
	}
	normalizedEmail, err := NormalizeEmail(input.Email)
	if err != nil {
		return nil, err
	}
	secret, err := s.config.Generate("addp_ti_")
	if err != nil || !validOpaqueValue(secret, "addp_ti_") {
		return nil, fmt.Errorf("generate tenant invitation secret")
	}
	now := s.config.Now().UTC()
	invitation := &TenantInvitation{
		TenantID: input.TenantID, Email: strings.TrimSpace(input.Email), NormalizedEmail: normalizedEmail,
		SecretHash: hashOpaqueToken(secret), Status: TenantInvitationStatusPending,
		ExpiresAt: now.Add(s.config.InvitationTTL), CreatedByPrincipalID: input.CreatedByPrincipalID,
		CreatedAt: now, UpdatedAt: now,
	}
	err = s.repository.Transaction(ctx, func(tx *Repository) error {
		creator, err := tx.LockPrincipal(ctx, input.CreatedByPrincipalID)
		if err != nil {
			return err
		}
		if creator.PrincipalType != PrincipalTypeUser || creator.Status != PrincipalStatusActive {
			return commonapi.ErrForbidden
		}
		tenant, err := tx.LockTenant(ctx, input.TenantID)
		if err != nil {
			return err
		}
		if tenant.Status != TenantStatusActive {
			return fmt.Errorf("%w: tenant must be active", commonapi.ErrConflict)
		}
		if err := s.expireTenantInvitationsTx(ctx, tx, tenant.ID, now, input.Audit); err != nil {
			return err
		}
		if err := tx.CreateTenantInvitation(ctx, invitation); err != nil {
			return err
		}
		return s.writeInvitationAudit(ctx, tx, invitation, input.Audit, "iam.tenant_invitation.created", AuditRiskMedium, nil)
	})
	if err != nil {
		return nil, err
	}
	return &CreatedTenantInvitation{Invitation: *invitation, Secret: secret}, nil
}

func (s *TenantInvitationService) Revoke(
	ctx context.Context,
	tenantID int64,
	invitationID int64,
	actorPrincipalID int64,
	audit AuditMetadata,
) (*TenantInvitation, error) {
	if s == nil || s.repository == nil || tenantID <= 0 || invitationID <= 0 || actorPrincipalID <= 0 {
		return nil, fmt.Errorf("%w: tenant, invitation and actor are required", commonapi.ErrBadRequest)
	}
	now := s.config.Now().UTC()
	var revoked *TenantInvitation
	var outcomeErr error
	err := s.repository.Transaction(ctx, func(tx *Repository) error {
		invitation, err := tx.LockTenantInvitationByID(ctx, tenantID, invitationID)
		if err != nil {
			return err
		}
		if invitation.Status == TenantInvitationStatusPending && !invitation.ExpiresAt.After(now) {
			if err := tx.TransitionTenantInvitation(ctx, invitation.ID, TenantInvitationStatusExpired, 0, now); err != nil {
				return err
			}
			invitation.Status, invitation.ExpiredAt = TenantInvitationStatusExpired, &now
			if err := s.writeInvitationAudit(ctx, tx, invitation, audit, "iam.tenant_invitation.expired", AuditRiskLow, nil); err != nil {
				return err
			}
			outcomeErr = fmt.Errorf("%w: tenant invitation has expired", commonapi.ErrConflict)
			return nil
		}
		if invitation.Status != TenantInvitationStatusPending {
			outcomeErr = fmt.Errorf("%w: tenant invitation is not pending", commonapi.ErrConflict)
			return nil
		}
		if err := tx.TransitionTenantInvitation(ctx, invitation.ID, TenantInvitationStatusRevoked, actorPrincipalID, now); err != nil {
			return err
		}
		invitation.Status, invitation.RevokedAt, invitation.RevokedByPrincipalID = TenantInvitationStatusRevoked, &now, &actorPrincipalID
		if err := s.writeInvitationAudit(ctx, tx, invitation, audit, "iam.tenant_invitation.revoked", AuditRiskHigh, nil); err != nil {
			return err
		}
		revoked = invitation
		return nil
	})
	if err != nil {
		return nil, err
	}
	if outcomeErr != nil {
		return nil, outcomeErr
	}
	return revoked, nil
}

func (s *TenantInvitationService) IssueEnrollmentTicket(
	ctx context.Context,
	input IssueEnrollmentTicketInput,
) (*IssuedEnrollmentTicket, error) {
	if !validOpaqueValue(input.InvitationSecret, "addp_ti_") {
		return nil, commonapi.ErrUnauthorized
	}
	authenticated, err := s.identity.AuthenticateLocalAccount(ctx, input.Username, input.Password, input.Audit)
	if err != nil {
		return nil, err
	}
	now := s.config.Now().UTC()
	plainTicket, err := s.config.Generate("addp_et_")
	if err != nil || !validOpaqueValue(plainTicket, "addp_et_") {
		return nil, fmt.Errorf("generate enrollment ticket")
	}
	var issued *IssuedEnrollmentTicket
	var outcomeErr error
	err = s.repository.Transaction(ctx, func(tx *Repository) error {
		principal, err := tx.LockPrincipal(ctx, authenticated.PrincipalID)
		if err != nil {
			return err
		}
		invitation, invitationOutcome, err := s.lockUsableInvitationTx(ctx, tx, input.InvitationSecret, now, input.Audit)
		if err != nil {
			return err
		}
		if invitationOutcome != nil {
			outcomeErr = invitationOutcome
			return nil
		}
		if err := s.validateInvitationPrincipal(ctx, tx, invitation, principal); err != nil {
			outcomeErr = err
			return nil
		}
		contexts, hasPlatformRole, err := availableContexts(ctx, tx, principal.ID, AssuranceLevelAAL1, now)
		if err != nil {
			return err
		}
		if len(contexts) != 0 || hasPlatformRole {
			outcomeErr = fmt.Errorf("%w: enrollment ticket is only available without an existing context", commonapi.ErrConflict)
			return nil
		}
		expiresAt := now.Add(s.config.EnrollmentTicketTTL)
		ticket := &EnrollmentTicket{
			TokenHash: hashOpaqueToken(plainTicket), PrincipalID: principal.ID, InvitationID: invitation.ID,
			IssuedAuthorizationVersion: principal.AuthorizationVersion,
			AuthenticatedAt:            authenticated.AuthenticatedAt.UTC(), ExpiresAt: expiresAt, CreatedAt: now,
		}
		if err := tx.CreateEnrollmentTicket(ctx, ticket); err != nil {
			return err
		}
		metadata := auditMetadataWithPrincipalFallback(input.Audit, principal.ID)
		if err := NewAuditWriter(tx).Write(ctx, AuditEvent{
			Metadata: metadata, EventName: "iam.enrollment_ticket.issued", Result: AuditResultSucceeded,
			RiskLevel: AuditRiskMedium, ModuleName: "system", EntityType: "enrollment_ticket",
			EntityID: strconv.FormatInt(ticket.ID, 10),
			Details:  map[string]any{"invitation_id": invitation.ID, "tenant_id": invitation.TenantID, "expires_at": expiresAt},
		}); err != nil {
			return err
		}
		issued = &IssuedEnrollmentTicket{EnrollmentTicket: plainTicket, ExpiresAt: expiresAt}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if outcomeErr != nil {
		return nil, outcomeErr
	}
	return issued, nil
}

func (s *TenantInvitationService) Register(
	ctx context.Context,
	input RegisterTenantInvitationInput,
) (*AcceptedTenantInvitation, error) {
	if !validOpaqueValue(input.InvitationSecret, "addp_ti_") || strings.TrimSpace(input.DisplayName) == "" || input.Password == "" {
		return nil, fmt.Errorf("%w: invalid invitation registration", commonapi.ErrBadRequest)
	}
	if _, err := NormalizeUsername(input.Username); err != nil {
		return nil, err
	}
	passwordHash, err := passwordutils.HashPassword(input.Password)
	if err != nil {
		return nil, fmt.Errorf("%w: hash password", commonapi.ErrBadRequest)
	}
	now := s.config.Now().UTC()
	var accepted *AcceptedTenantInvitation
	var outcomeErr error
	err = s.repository.Transaction(ctx, func(tx *Repository) error {
		invitation, invitationOutcome, err := s.lockUsableInvitationTx(ctx, tx, input.InvitationSecret, now, input.Audit)
		if err != nil {
			return err
		}
		if invitationOutcome != nil {
			outcomeErr = invitationOutcome
			return nil
		}
		tenant, err := tx.LockTenant(ctx, invitation.TenantID)
		if err != nil {
			return err
		}
		if tenant.Status != TenantStatusActive {
			outcomeErr = commonapi.ErrForbidden
			return nil
		}
		email := invitation.Email
		created, err := s.identity.createLocalUserTx(ctx, tx, CreateLocalUserInput{
			Username: input.Username, Password: input.Password, DisplayName: input.DisplayName,
			PrimaryEmail: &email, Locale: input.Locale, Audit: input.Audit,
		}, passwordHash, now)
		if err != nil {
			return err
		}
		authentication := SessionAuthentication{Methods: []string{"password"}, AssuranceLevel: AssuranceLevelAAL1, AuthenticatedAt: now}
		accepted, err = s.acceptInvitationTx(ctx, tx, invitation, created.PrincipalID, nil, authentication, now, input.Audit)
		return err
	})
	if err != nil {
		return nil, err
	}
	if outcomeErr != nil {
		return nil, outcomeErr
	}
	return accepted, nil
}

func (s *TenantInvitationService) Accept(
	ctx context.Context,
	input AcceptTenantInvitationInput,
) (*AcceptedTenantInvitation, error) {
	if !validOpaqueValue(input.InvitationSecret, "addp_ti_") {
		return nil, commonapi.ErrUnauthorized
	}
	usingBrowser := input.PrincipalID > 0 && input.EnrollmentTicket == ""
	usingEnrollment := input.PrincipalID == 0 && validOpaqueValue(input.EnrollmentTicket, "addp_et_")
	if usingBrowser == usingEnrollment {
		return nil, fmt.Errorf("%w: exactly one acceptance credential is required", commonapi.ErrBadRequest)
	}
	principalID := input.PrincipalID
	var ticketSnapshot *EnrollmentTicket
	var err error
	if usingEnrollment {
		ticketSnapshot, err = s.repository.GetEnrollmentTicketByHash(ctx, hashOpaqueToken(input.EnrollmentTicket))
		if err != nil {
			if errors.Is(err, commonapi.ErrNotFound) {
				return nil, commonapi.ErrUnauthorized
			}
			return nil, err
		}
		principalID = ticketSnapshot.PrincipalID
	}
	now := s.config.Now().UTC()
	var accepted *AcceptedTenantInvitation
	var outcomeErr error
	err = s.repository.Transaction(ctx, func(tx *Repository) error {
		principal, err := tx.LockPrincipal(ctx, principalID)
		if err != nil {
			return err
		}
		invitation, invitationOutcome, err := s.lockUsableInvitationTx(ctx, tx, input.InvitationSecret, now, input.Audit)
		if err != nil {
			return err
		}
		if invitationOutcome != nil {
			outcomeErr = invitationOutcome
			return nil
		}
		if err := s.validateInvitationPrincipal(ctx, tx, invitation, principal); err != nil {
			outcomeErr = err
			return nil
		}
		authentication := input.Authentication
		var enrollmentTicket *EnrollmentTicket
		if usingEnrollment {
			enrollmentTicket, err = tx.LockEnrollmentTicketByHash(ctx, hashOpaqueToken(input.EnrollmentTicket))
			if err != nil {
				return err
			}
			if enrollmentTicket.ID != ticketSnapshot.ID || enrollmentTicket.PrincipalID != principal.ID ||
				enrollmentTicket.InvitationID != invitation.ID || enrollmentTicket.ConsumedAt != nil ||
				enrollmentTicket.IssuedAuthorizationVersion != principal.AuthorizationVersion ||
				!enrollmentTicket.ExpiresAt.After(now) {
				outcomeErr = commonapi.ErrUnauthorized
				return nil
			}
			authentication = SessionAuthentication{
				Methods: []string{"password"}, AssuranceLevel: AssuranceLevelAAL1,
				AuthenticatedAt: enrollmentTicket.AuthenticatedAt,
			}
		}
		accepted, err = s.acceptInvitationTx(ctx, tx, invitation, principal.ID, enrollmentTicket, authentication, now, input.Audit)
		return err
	})
	if err != nil {
		return nil, err
	}
	if outcomeErr != nil {
		return nil, outcomeErr
	}
	return accepted, nil
}

func (s *TenantInvitationService) acceptInvitationTx(
	ctx context.Context,
	tx *Repository,
	invitation *TenantInvitation,
	principalID int64,
	enrollmentTicket *EnrollmentTicket,
	authentication SessionAuthentication,
	now time.Time,
	audit AuditMetadata,
) (*AcceptedTenantInvitation, error) {
	if _, err := normalizeAuthenticationMethods(authentication.Methods); err != nil {
		return nil, err
	}
	if err := validateAssuranceLevel(authentication.AssuranceLevel); err != nil || authentication.AuthenticatedAt.IsZero() || authentication.AuthenticatedAt.After(now) {
		return nil, fmt.Errorf("%w: invalid invitation authentication facts", commonapi.ErrBadRequest)
	}
	if _, err := tx.LockTenantMembership(ctx, invitation.TenantID, principalID); err == nil {
		return nil, fmt.Errorf("%w: tenant membership already exists and cannot be restored by invitation", commonapi.ErrConflict)
	} else if !errors.Is(err, commonapi.ErrNotFound) {
		return nil, err
	}
	sourceRef := strconv.FormatInt(invitation.ID, 10)
	membership := &TenantMembership{
		TenantID: invitation.TenantID, PrincipalID: principalID, Status: TenantMembershipStatusActive,
		SourceType: TenantMembershipSourceInvitation, SourceRef: &sourceRef,
		JoinedAt: now, CreatedByPrincipalID: &invitation.CreatedByPrincipalID,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := tx.CreateTenantMembership(ctx, membership); err != nil {
		return nil, err
	}
	if err := tx.TransitionTenantInvitation(ctx, invitation.ID, TenantInvitationStatusAccepted, principalID, now); err != nil {
		return nil, err
	}
	if enrollmentTicket != nil {
		if err := tx.ConsumeEnrollmentTicket(ctx, enrollmentTicket.ID, now); err != nil {
			return nil, err
		}
	}
	principal, err := tx.GetPrincipal(ctx, principalID)
	if err != nil {
		return nil, err
	}
	revokedFamilyCount, err := tx.RevokeActiveTokenFamilies(ctx, principalID, now, "tenant_invitation_accepted")
	if err != nil {
		return nil, err
	}
	invitation.Status, invitation.AcceptedAt, invitation.AcceptedByPrincipalID = TenantInvitationStatusAccepted, &now, &principalID
	metadata := auditMetadataWithPrincipalFallback(audit, principalID)
	if err := s.writeInvitationAudit(ctx, tx, invitation, metadata, "iam.tenant_invitation.accepted", AuditRiskMedium, map[string]any{
		"membership_id": membership.ID, "authorization_version": principal.AuthorizationVersion,
		"revoked_family_count": revokedFamilyCount,
	}); err != nil {
		return nil, err
	}
	if err := NewAuditWriter(tx).Write(ctx, AuditEvent{
		Metadata: metadata, EventName: "iam.tenant_membership.established", Result: AuditResultSucceeded,
		RiskLevel: AuditRiskMedium, ModuleName: "system", EntityType: "tenant_membership",
		EntityID: strconv.FormatInt(membership.ID, 10),
		Details: map[string]any{
			"tenant_id": invitation.TenantID, "principal_id": principalID,
			"source_type": TenantMembershipSourceInvitation, "source_ref": sourceRef,
			"authorization_version": principal.AuthorizationVersion, "revoked_family_count": revokedFamilyCount,
		},
	}); err != nil {
		return nil, err
	}
	resolved, err := resolveTenantSessionContext(ctx, tx, principal.ID, membership.ID, now)
	if err != nil {
		return nil, err
	}
	session, err := s.tokenService.issueBrowserSessionTx(ctx, tx, browserSessionIssueInput{
		Principal: principal, Context: resolved, Authentication: authentication,
		Mode: BrowserSessionIssueModeDirect, Audit: metadata,
	})
	if err != nil {
		return nil, err
	}
	return &AcceptedTenantInvitation{Invitation: *invitation, Membership: *membership, Session: *session}, nil
}

func (s *TenantInvitationService) lockUsableInvitationTx(
	ctx context.Context,
	tx *Repository,
	secret string,
	now time.Time,
	audit AuditMetadata,
) (*TenantInvitation, error, error) {
	invitation, err := tx.LockTenantInvitationBySecretHash(ctx, hashOpaqueToken(secret))
	if err != nil {
		if errors.Is(err, commonapi.ErrNotFound) {
			return nil, commonapi.ErrUnauthorized, nil
		}
		return nil, nil, err
	}
	if invitation.Status != TenantInvitationStatusPending {
		return nil, commonapi.ErrUnauthorized, nil
	}
	if !invitation.ExpiresAt.After(now) {
		if err := tx.TransitionTenantInvitation(ctx, invitation.ID, TenantInvitationStatusExpired, 0, now); err != nil {
			return nil, nil, err
		}
		invitation.Status, invitation.ExpiredAt = TenantInvitationStatusExpired, &now
		if err := s.writeInvitationAudit(ctx, tx, invitation, audit, "iam.tenant_invitation.expired", AuditRiskLow, nil); err != nil {
			return nil, nil, err
		}
		return nil, commonapi.ErrUnauthorized, nil
	}
	return invitation, nil, nil
}

func (s *TenantInvitationService) validateInvitationPrincipal(
	ctx context.Context,
	tx *Repository,
	invitation *TenantInvitation,
	principal *Principal,
) error {
	if principal.PrincipalType != PrincipalTypeUser || principal.Status != PrincipalStatusActive {
		return commonapi.ErrForbidden
	}
	user, err := tx.GetUser(ctx, principal.ID)
	if err != nil {
		return err
	}
	if user.PrimaryEmail == nil {
		return fmt.Errorf("%w: user email does not match invitation", commonapi.ErrForbidden)
	}
	normalizedEmail, err := NormalizeEmail(*user.PrimaryEmail)
	if err != nil || normalizedEmail != invitation.NormalizedEmail {
		return fmt.Errorf("%w: user email does not match invitation", commonapi.ErrForbidden)
	}
	return nil
}

func (s *TenantInvitationService) expireTenantInvitationsTx(
	ctx context.Context,
	tx *Repository,
	tenantID int64,
	now time.Time,
	audit AuditMetadata,
) error {
	expired, err := tx.ExpirePendingTenantInvitations(ctx, tenantID, now)
	if err != nil {
		return err
	}
	for index := range expired {
		if err := s.writeInvitationAudit(ctx, tx, &expired[index], audit, "iam.tenant_invitation.expired", AuditRiskLow, nil); err != nil {
			return err
		}
	}
	return nil
}

func (s *TenantInvitationService) writeInvitationAudit(
	ctx context.Context,
	tx *Repository,
	invitation *TenantInvitation,
	metadata AuditMetadata,
	eventName string,
	risk AuditRiskLevel,
	extra map[string]any,
) error {
	details := map[string]any{"tenant_id": invitation.TenantID, "status": invitation.Status}
	for key, value := range extra {
		details[key] = value
	}
	return NewAuditWriter(tx).Write(ctx, AuditEvent{
		Metadata: metadata, EventName: eventName, Result: AuditResultSucceeded, RiskLevel: risk,
		ModuleName: "system", EntityType: "tenant_invitation",
		EntityID: strconv.FormatInt(invitation.ID, 10), Details: details,
	})
}

func validOpaqueValue(value string, prefix string) bool {
	return strings.HasPrefix(value, prefix) && len(value) > len(prefix)
}
