package iam

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	commonapi "github.com/addp/common/api"
	passwordutils "github.com/addp/system/pkg/utils"
	"github.com/lib/pq"
	"github.com/pquerna/otp/totp"
)

const (
	mfaEnrollmentTokenPrefix         = "addp_mfe_"
	mfaStepUpChallengeTokenPrefix    = "addp_mfc_"
	mfaEnrollmentRevocationReason    = "mfa_enrollment_completed"
	mfaStepUpRevocationReason        = "mfa_step_up_completed"
	browserSessionIssueModeMFAStepUp = BrowserSessionIssueMode("mfa_step_up")
	browserSessionIssueModeMFAEnroll = BrowserSessionIssueMode("mfa_enrollment")
)

var (
	ErrTOTPAlreadyEnrolled    = fmt.Errorf("%w: TOTP is already enrolled", commonapi.ErrConflict)
	ErrTOTPEnrollmentRequired = fmt.Errorf("%w: TOTP enrollment is required", commonapi.ErrConflict)
)

type MFAStatus struct {
	TOTPEnrolled bool
}

type BeginMFAEnrollmentInput struct {
	AccessToken     string
	RefreshToken    string
	CurrentPassword string
	Audit           AuditMetadata
}

type IssuedMFAEnrollment struct {
	EnrollmentToken string
	Secret          string
	OTPAuthURI      string
	ExpiresAt       time.Time
}

type CompleteMFAEnrollmentInput struct {
	AccessToken     string
	RefreshToken    string
	EnrollmentToken string
	Code            string
	Audit           AuditMetadata
}

type BeginMFAStepUpInput struct {
	AccessToken  string
	RefreshToken string
	Audit        AuditMetadata
}

type CompleteMFAStepUpInput struct {
	AccessToken    string
	RefreshToken   string
	ChallengeToken string
	Code           string
	Audit          AuditMetadata
}

type MFASessionService struct {
	repository   *Repository
	cipher       *MFACredentialCipher
	tokenService *TokenFamilyService
	config       MFAServiceConfig
	generate     OpaqueTokenGenerator
	now          func() time.Time
}

func NewMFASessionService(
	repository *Repository,
	cipher *MFACredentialCipher,
	tokenService *TokenFamilyService,
	config MFAServiceConfig,
	generate OpaqueTokenGenerator,
	now func() time.Time,
) (*MFASessionService, error) {
	if repository == nil || cipher == nil || tokenService == nil {
		return nil, fmt.Errorf("%w: MFA session dependencies are required", commonapi.ErrBadRequest)
	}
	if config.ChallengeTTL <= 0 {
		config.ChallengeTTL = defaultMFAChallengeTTL
	}
	if config.ChallengeTTL > 5*time.Minute {
		return nil, fmt.Errorf("%w: MFA enrollment and challenge TTL cannot exceed 5 minutes", commonapi.ErrBadRequest)
	}
	if config.StepUpTTL <= 0 {
		config.StepUpTTL = defaultMFAStepUpTTL
	}
	if generate == nil {
		generate = generateOpaqueToken
	}
	if now == nil {
		now = time.Now
	}
	return &MFASessionService{
		repository: repository, cipher: cipher, tokenService: tokenService,
		config: config, generate: generate, now: now,
	}, nil
}

func (s *MFASessionService) Status(ctx context.Context, principalID int64) (*MFAStatus, error) {
	if s == nil || principalID <= 0 {
		return nil, fmt.Errorf("%w: valid principal is required", commonapi.ErrBadRequest)
	}
	enrolled, err := s.repository.HasActiveMFACredential(ctx, principalID)
	if err != nil {
		return nil, err
	}
	return &MFAStatus{TOTPEnrolled: enrolled}, nil
}

func (s *MFASessionService) BeginEnrollment(
	ctx context.Context,
	input BeginMFAEnrollmentInput,
) (*IssuedMFAEnrollment, error) {
	source, err := s.loadSource(ctx, input.AccessToken, input.RefreshToken)
	if err != nil {
		return nil, err
	}
	plainToken, err := s.generate(mfaEnrollmentTokenPrefix)
	if err != nil {
		return nil, fmt.Errorf("generate MFA enrollment token: %w", err)
	}
	var issued *IssuedMFAEnrollment
	var outcomeErr error
	err = s.repository.Transaction(ctx, func(tx *Repository) error {
		principal, family, _, _, err := s.lockSource(ctx, tx, source)
		if err != nil {
			return err
		}
		account, err := tx.LockLocalAccountByUserID(ctx, principal.ID)
		if err != nil {
			return err
		}
		if account.Status != LocalAccountStatusActive || !passwordutils.CheckPassword(input.CurrentPassword, account.PasswordHash) {
			outcomeErr = ErrInvalidCurrentPassword
			return NewAuditWriter(tx).Write(ctx, AuditEvent{
				Metadata: input.Audit, EventName: "iam.mfa.enrollment_denied", Result: AuditResultDenied,
				RiskLevel: AuditRiskHigh, ModuleName: "system", EntityType: "principal",
				EntityID: strconv.FormatInt(principal.ID, 10), Details: map[string]any{"reason": "invalid_current_password"},
			})
		}
		if _, err := tx.LockActiveMFACredential(ctx, principal.ID); err == nil {
			return ErrTOTPAlreadyEnrolled
		} else if !errors.Is(err, commonapi.ErrNotFound) {
			return err
		}
		key, err := totp.Generate(totp.GenerateOpts{Issuer: "ADDP", AccountName: account.Username})
		if err != nil {
			return fmt.Errorf("generate TOTP secret: %w", err)
		}
		ciphertext, nonce, keyVersion, err := s.cipher.EncryptTOTPSecret(principal.ID, key.Secret())
		if err != nil {
			return err
		}
		now := s.now().UTC()
		expiresAt := now.Add(s.config.ChallengeTTL)
		enrollment := &MFAEnrollment{
			TokenHash: hashOpaqueToken(plainToken), PrincipalID: principal.ID, SourceFamilyID: family.ID,
			IssuedAuthorizationVersion: principal.AuthorizationVersion, SecretCiphertext: ciphertext,
			SecretNonce: nonce, KeyVersion: keyVersion, ExpiresAt: expiresAt, CreatedAt: now,
		}
		if err := tx.CreateMFAEnrollment(ctx, enrollment); err != nil {
			return err
		}
		if err := NewAuditWriter(tx).Write(ctx, AuditEvent{
			Metadata: input.Audit, EventName: "iam.mfa.enrollment_started", Result: AuditResultSucceeded,
			RiskLevel: AuditRiskHigh, ModuleName: "system", EntityType: "mfa_enrollment",
			EntityID: strconv.FormatInt(enrollment.ID, 10), Details: map[string]any{"method": "totp", "expires_at": expiresAt},
		}); err != nil {
			return err
		}
		issued = &IssuedMFAEnrollment{
			EnrollmentToken: plainToken, Secret: key.Secret(), OTPAuthURI: key.URL(), ExpiresAt: expiresAt,
		}
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

func (s *MFASessionService) CompleteEnrollment(
	ctx context.Context,
	input CompleteMFAEnrollmentInput,
) (*IssuedBrowserSession, error) {
	if !strings.HasPrefix(input.EnrollmentToken, mfaEnrollmentTokenPrefix) || !totpCodePattern.MatchString(input.Code) {
		return nil, commonapi.ErrUnauthorized
	}
	source, err := s.loadSource(ctx, input.AccessToken, input.RefreshToken)
	if err != nil {
		return nil, err
	}
	tokenHash := hashOpaqueToken(input.EnrollmentToken)
	snapshot, err := s.repository.GetMFAEnrollmentByHash(ctx, tokenHash)
	if err != nil {
		return nil, hideMFAStorageError(err)
	}
	var session *IssuedBrowserSession
	var outcomeErr error
	err = s.repository.Transaction(ctx, func(tx *Repository) error {
		principal, family, _, _, err := s.lockSource(ctx, tx, source)
		if err != nil {
			return err
		}
		enrollment, err := tx.LockMFAEnrollmentByHash(ctx, tokenHash)
		if err != nil {
			return hideMFAStorageError(err)
		}
		now := s.now().UTC()
		if enrollment.ID != snapshot.ID || enrollment.PrincipalID != principal.ID ||
			enrollment.SourceFamilyID != family.ID || enrollment.IssuedAuthorizationVersion != principal.AuthorizationVersion ||
			enrollment.ConsumedAt != nil || !enrollment.ExpiresAt.After(now) || enrollment.FailedAttempts >= maxMFAFailedAttempts {
			return commonapi.ErrUnauthorized
		}
		if _, err := tx.LockActiveMFACredential(ctx, principal.ID); err == nil {
			return ErrTOTPAlreadyEnrolled
		} else if !errors.Is(err, commonapi.ErrNotFound) {
			return err
		}
		secret, err := s.cipher.DecryptTOTPSecret(&MFACredential{
			UserID: principal.ID, SecretCiphertext: enrollment.SecretCiphertext,
			SecretNonce: enrollment.SecretNonce, KeyVersion: enrollment.KeyVersion,
		})
		if err != nil {
			return err
		}
		counter, valid := matchTOTPCode(secret, input.Code, now, nil)
		if !valid {
			nextAttempts := enrollment.FailedAttempts + 1
			if err := tx.RecordMFAEnrollmentFailure(ctx, enrollment.ID, nextAttempts >= maxMFAFailedAttempts, now); err != nil {
				return err
			}
			outcomeErr = commonapi.ErrUnauthorized
			return NewAuditWriter(tx).Write(ctx, AuditEvent{
				Metadata: input.Audit, EventName: "iam.mfa.enrollment_verification_failed", Result: AuditResultDenied,
				RiskLevel: AuditRiskHigh, ModuleName: "system", EntityType: "mfa_enrollment",
				EntityID: strconv.FormatInt(enrollment.ID, 10), Details: map[string]any{"method": "totp", "failed_attempts": nextAttempts},
			})
		}
		credential := &MFACredential{
			UserID: principal.ID, Method: "totp", Status: MFACredentialStatusActive,
			SecretCiphertext: enrollment.SecretCiphertext, SecretNonce: enrollment.SecretNonce,
			KeyVersion: enrollment.KeyVersion, LastAcceptedCounter: &counter, CreatedAt: now, UpdatedAt: now,
		}
		if err := tx.CreateMFACredential(ctx, credential); err != nil {
			return err
		}
		if err := tx.ConsumeMFAEnrollment(ctx, enrollment.ID, now); err != nil {
			return err
		}
		session, err = s.replaceSourceWithAAL2(ctx, tx, principal, family, source.context, now,
			mfaEnrollmentRevocationReason, browserSessionIssueModeMFAEnroll)
		if err != nil {
			return err
		}
		return s.writeCompletionAudit(ctx, tx, input.Audit, principal, family, session,
			"iam.mfa.enrollment_completed", "mfa_credential", credential.ID)
	})
	if err != nil {
		return nil, err
	}
	if outcomeErr != nil {
		return nil, outcomeErr
	}
	return session, nil
}

func (s *MFASessionService) BeginStepUp(ctx context.Context, input BeginMFAStepUpInput) (*IssuedMFAChallenge, error) {
	source, err := s.loadSource(ctx, input.AccessToken, input.RefreshToken)
	if err != nil {
		return nil, err
	}
	plainToken, err := s.generate(mfaStepUpChallengeTokenPrefix)
	if err != nil {
		return nil, err
	}
	var issued *IssuedMFAChallenge
	err = s.repository.Transaction(ctx, func(tx *Repository) error {
		principal, family, _, _, err := s.lockSource(ctx, tx, source)
		if err != nil {
			return err
		}
		if _, err := tx.LockActiveMFACredential(ctx, principal.ID); err != nil {
			if errors.Is(err, commonapi.ErrNotFound) {
				return ErrTOTPEnrollmentRequired
			}
			return err
		}
		now := s.now().UTC()
		expiresAt := now.Add(s.config.ChallengeTTL)
		familyID := family.ID
		challenge := &MFAChallenge{
			TokenHash: hashOpaqueToken(plainToken), PrincipalID: principal.ID, Purpose: "step_up",
			SourceFamilyID: &familyID, IssuedAuthorizationVersion: principal.AuthorizationVersion,
			AuthenticationMethods: pq.StringArray{"password"}, AuthenticatedAt: family.AuthenticatedAt,
			ExpiresAt: expiresAt, CreatedAt: now,
		}
		if err := tx.CreateMFAChallenge(ctx, challenge); err != nil {
			return err
		}
		if err := NewAuditWriter(tx).Write(ctx, AuditEvent{
			Metadata: input.Audit, EventName: "iam.mfa.step_up_started", Result: AuditResultSucceeded,
			RiskLevel: AuditRiskHigh, ModuleName: "system", EntityType: "mfa_challenge",
			EntityID: strconv.FormatInt(challenge.ID, 10), Details: map[string]any{"method": "totp", "expires_at": expiresAt},
		}); err != nil {
			return err
		}
		issued = &IssuedMFAChallenge{ChallengeToken: plainToken, ExpiresAt: expiresAt}
		return nil
	})
	return issued, err
}

func (s *MFASessionService) CompleteStepUp(ctx context.Context, input CompleteMFAStepUpInput) (*IssuedBrowserSession, error) {
	if !strings.HasPrefix(input.ChallengeToken, mfaStepUpChallengeTokenPrefix) || !totpCodePattern.MatchString(input.Code) {
		return nil, commonapi.ErrUnauthorized
	}
	source, err := s.loadSource(ctx, input.AccessToken, input.RefreshToken)
	if err != nil {
		return nil, err
	}
	tokenHash := hashOpaqueToken(input.ChallengeToken)
	snapshot, err := s.repository.GetMFAChallengeByHash(ctx, tokenHash)
	if err != nil {
		return nil, hideMFAStorageError(err)
	}
	var session *IssuedBrowserSession
	var outcomeErr error
	err = s.repository.Transaction(ctx, func(tx *Repository) error {
		principal, family, _, _, err := s.lockSource(ctx, tx, source)
		if err != nil {
			return err
		}
		challenge, err := tx.LockMFAChallengeByHash(ctx, tokenHash)
		if err != nil {
			return hideMFAStorageError(err)
		}
		now := s.now().UTC()
		if challenge.ID != snapshot.ID || challenge.Purpose != "step_up" || challenge.SourceFamilyID == nil ||
			*challenge.SourceFamilyID != family.ID || challenge.PrincipalID != principal.ID ||
			challenge.IssuedAuthorizationVersion != principal.AuthorizationVersion || challenge.ConsumedAt != nil ||
			!challenge.ExpiresAt.After(now) || challenge.FailedAttempts >= maxMFAFailedAttempts {
			return commonapi.ErrUnauthorized
		}
		credential, err := tx.LockActiveMFACredential(ctx, principal.ID)
		if err != nil {
			return hideMFAStorageError(err)
		}
		secret, err := s.cipher.DecryptTOTPSecret(credential)
		if err != nil {
			return err
		}
		counter, valid := matchTOTPCode(secret, input.Code, now, credential.LastAcceptedCounter)
		if !valid {
			nextAttempts := challenge.FailedAttempts + 1
			if err := tx.RecordMFAChallengeFailure(ctx, challenge.ID, nextAttempts >= maxMFAFailedAttempts, now); err != nil {
				return err
			}
			if nextAttempts >= maxMFAFailedAttempts {
				if _, err := tx.LockActiveResourceAccessTickets(ctx, family.ID); err != nil {
					return err
				}
				if err := tx.RevokeTokenFamily(ctx, family.ID, now, "mfa_step_up_failed"); err != nil {
					return err
				}
			}
			outcomeErr = commonapi.ErrUnauthorized
			return NewAuditWriter(tx).Write(ctx, AuditEvent{
				Metadata: input.Audit, EventName: "iam.mfa.step_up_failed", Result: AuditResultDenied,
				RiskLevel: AuditRiskHigh, ModuleName: "system", EntityType: "mfa_challenge",
				EntityID: strconv.FormatInt(challenge.ID, 10), Details: map[string]any{"method": "totp", "failed_attempts": nextAttempts},
			})
		}
		if err := tx.UpdateMFALastAcceptedCounter(ctx, credential.ID, counter); err != nil {
			return err
		}
		if err := tx.ConsumeMFAChallenge(ctx, challenge.ID, now); err != nil {
			return err
		}
		session, err = s.replaceSourceWithAAL2(ctx, tx, principal, family, source.context, now,
			mfaStepUpRevocationReason, browserSessionIssueModeMFAStepUp)
		if err != nil {
			return err
		}
		return s.writeCompletionAudit(ctx, tx, input.Audit, principal, family, session,
			"iam.mfa.step_up_completed", "mfa_credential", credential.ID)
	})
	if err != nil {
		return nil, err
	}
	if outcomeErr != nil {
		return nil, outcomeErr
	}
	return session, nil
}

type mfaBrowserSource struct {
	access  *AccessToken
	refresh *RefreshToken
	family  *RefreshTokenFamily
	context ResolvedSessionContext
}

func (s *MFASessionService) loadSource(ctx context.Context, accessPlain, refreshPlain string) (*mfaBrowserSource, error) {
	if !strings.HasPrefix(accessPlain, "addp_at_") || !strings.HasPrefix(refreshPlain, "addp_rt_") {
		return nil, commonapi.ErrUnauthorized
	}
	access, err := s.repository.GetAccessTokenByHash(ctx, hashOpaqueToken(accessPlain))
	if err != nil {
		return nil, hideTokenLookupError(err)
	}
	refresh, err := s.repository.GetRefreshTokenByHash(ctx, hashOpaqueToken(refreshPlain))
	if err != nil {
		return nil, hideTokenLookupError(err)
	}
	if access.FamilyID != refresh.FamilyID || refresh.IssuedAccessTokenID != access.ID {
		return nil, commonapi.ErrUnauthorized
	}
	family, err := s.repository.GetRefreshTokenFamily(ctx, access.FamilyID)
	if err != nil {
		return nil, hideTokenLookupError(err)
	}
	resolvedContext, err := s.tokenService.resolveFamilyContext(ctx, family)
	if err != nil {
		return nil, hideTokenLookupError(err)
	}
	return &mfaBrowserSource{access: access, refresh: refresh, family: family, context: resolvedContext}, nil
}

func (s *MFASessionService) lockSource(
	ctx context.Context,
	tx *Repository,
	source *mfaBrowserSource,
) (*Principal, *RefreshTokenFamily, *RefreshToken, *AccessToken, error) {
	principal, err := tx.LockPrincipal(ctx, source.family.PrincipalID)
	if err != nil {
		return nil, nil, nil, nil, hideTokenLookupError(err)
	}
	family, err := tx.LockRefreshTokenFamily(ctx, source.family.ID)
	if err != nil {
		return nil, nil, nil, nil, hideTokenLookupError(err)
	}
	refresh, err := tx.LockRefreshTokenByHash(ctx, source.refresh.TokenHash)
	if err != nil {
		return nil, nil, nil, nil, hideTokenLookupError(err)
	}
	access, err := tx.LockAccessToken(ctx, source.access.ID)
	if err != nil {
		return nil, nil, nil, nil, hideTokenLookupError(err)
	}
	if err := validateContextSwitchSource(principal, family, source.family, access, source.access, refresh, source.refresh, s.now().UTC()); err != nil {
		return nil, nil, nil, nil, err
	}
	active, err := tx.RefreshTokenFamilyContextIsActive(ctx, principal, family, s.now().UTC())
	if err != nil {
		return nil, nil, nil, nil, err
	}
	if !active {
		return nil, nil, nil, nil, commonapi.ErrUnauthorized
	}
	return principal, family, refresh, access, nil
}

func (s *MFASessionService) replaceSourceWithAAL2(
	ctx context.Context,
	tx *Repository,
	principal *Principal,
	family *RefreshTokenFamily,
	resolvedContext ResolvedSessionContext,
	now time.Time,
	revocationReason string,
	mode BrowserSessionIssueMode,
) (*IssuedBrowserSession, error) {
	if _, err := tx.LockActiveResourceAccessTickets(ctx, family.ID); err != nil {
		return nil, err
	}
	if err := tx.RevokeTokenFamily(ctx, family.ID, now, revocationReason); err != nil {
		return nil, err
	}
	stepUpExpiresAt := earlierTime(now.Add(s.config.StepUpTTL), family.ExpiresAt)
	familyExpiresAt := family.ExpiresAt
	return s.tokenService.createBrowserSessionTx(ctx, tx, browserSessionIssueInput{
		Principal: principal, Context: resolvedContext,
		Authentication: SessionAuthentication{
			Methods: []string{"password", "totp"}, AssuranceLevel: AssuranceLevelAAL2,
			AuthenticatedAt: now, StepUpExpiresAt: &stepUpExpiresAt,
		},
		FamilyExpiresAt: &familyExpiresAt, Mode: mode,
	})
}

func (s *MFASessionService) writeCompletionAudit(
	ctx context.Context,
	tx *Repository,
	metadata AuditMetadata,
	principal *Principal,
	sourceFamily *RefreshTokenFamily,
	session *IssuedBrowserSession,
	eventName, entityType string,
	entityID int64,
) error {
	methods := []string{"password", "totp"}
	sort.Strings(methods)
	return NewAuditWriter(tx).Write(ctx, AuditEvent{
		Metadata: metadata, EventName: eventName, Result: AuditResultSucceeded,
		RiskLevel: AuditRiskHigh, ModuleName: "system", EntityType: entityType,
		EntityID: strconv.FormatInt(entityID, 10), Details: map[string]any{
			"source_family_id": sourceFamily.ID, "replacement_family_id": session.FamilyID,
			"assurance_level": AssuranceLevelAAL2, "authentication_methods": methods,
			"principal_id": principal.ID,
		},
	})
}
