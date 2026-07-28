package iam

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/lib/pq"
	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
)

const (
	defaultMFAChallengeTTL = 5 * time.Minute
	defaultMFAStepUpTTL    = 12 * time.Hour
	maxMFAFailedAttempts   = 5
	totpPeriodSeconds      = 30
)

var totpCodePattern = regexp.MustCompile(`^[0-9]{6}$`)

type MFAServiceConfig struct {
	ChallengeTTL time.Duration
	StepUpTTL    time.Duration
}

type IssuedMFAChallenge struct {
	ChallengeToken string
	ExpiresAt      time.Time
}

type VerifyMFAChallengeInput struct {
	ChallengeToken string
	Code           string
	Audit          AuditMetadata
}

type MFAService struct {
	repository *Repository
	cipher     *MFACredentialCipher
	config     MFAServiceConfig
	generate   OpaqueTokenGenerator
	now        func() time.Time
}

func NewMFAService(
	repository *Repository,
	cipher *MFACredentialCipher,
	config MFAServiceConfig,
	generate OpaqueTokenGenerator,
	now func() time.Time,
) (*MFAService, error) {
	if repository == nil || cipher == nil {
		return nil, fmt.Errorf("%w: MFA dependencies are required", commonapi.ErrBadRequest)
	}
	if config.ChallengeTTL <= 0 {
		config.ChallengeTTL = defaultMFAChallengeTTL
	}
	if config.ChallengeTTL > 5*time.Minute {
		return nil, fmt.Errorf("%w: MFA challenge TTL cannot exceed 5 minutes", commonapi.ErrBadRequest)
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
	return &MFAService{repository: repository, cipher: cipher, config: config, generate: generate, now: now}, nil
}

func (s *MFAService) HasActiveTOTP(ctx context.Context, userID int64) (bool, error) {
	if s == nil || s.repository == nil || userID <= 0 {
		return false, fmt.Errorf("%w: valid user is required", commonapi.ErrBadRequest)
	}
	return s.repository.HasActiveMFACredential(ctx, userID)
}

func (s *MFAService) BeginChallenge(
	ctx context.Context,
	authenticated *AuthenticatedLocalUser,
	audit AuditMetadata,
) (*IssuedMFAChallenge, error) {
	if s == nil || authenticated == nil || authenticated.PrincipalID <= 0 || authenticated.AuthenticatedAt.IsZero() {
		return nil, fmt.Errorf("%w: authenticated local user is required", commonapi.ErrBadRequest)
	}
	plainToken, err := s.generate("addp_mfc_")
	if err != nil {
		return nil, fmt.Errorf("generate MFA challenge: %w", err)
	}
	now := s.now().UTC()
	expiresAt := now.Add(s.config.ChallengeTTL)
	err = s.repository.Transaction(ctx, func(tx *Repository) error {
		principal, err := tx.LockPrincipal(ctx, authenticated.PrincipalID)
		if err != nil {
			return err
		}
		if principal.PrincipalType != PrincipalTypeUser || principal.Status != PrincipalStatusActive ||
			principal.AuthorizationVersion != authenticated.AuthorizationVersion {
			return commonapi.ErrUnauthorized
		}
		if _, err := tx.LockActiveMFACredential(ctx, principal.ID); err != nil {
			return hideMFAStorageError(err)
		}
		challenge := &MFAChallenge{
			TokenHash:                  hashOpaqueToken(plainToken),
			PrincipalID:                principal.ID,
			Purpose:                    "login",
			IssuedAuthorizationVersion: principal.AuthorizationVersion,
			AuthenticationMethods:      pq.StringArray{"password"},
			AuthenticatedAt:            authenticated.AuthenticatedAt.UTC(),
			ExpiresAt:                  expiresAt,
			FailedAttempts:             0,
			CreatedAt:                  now,
		}
		if err := tx.CreateMFAChallenge(ctx, challenge); err != nil {
			return err
		}
		return NewAuditWriter(tx).Write(ctx, AuditEvent{
			Metadata:  authenticationAuditMetadata(audit, principal.ID),
			EventName: "iam.mfa.challenge_issued", Result: AuditResultSucceeded,
			RiskLevel: AuditRiskMedium, ModuleName: "system",
			EntityType: "mfa_challenge", EntityID: strconv.FormatInt(challenge.ID, 10),
			Details: map[string]any{"method": "totp", "expires_at": expiresAt},
		})
	})
	if err != nil {
		return nil, err
	}
	return &IssuedMFAChallenge{ChallengeToken: plainToken, ExpiresAt: expiresAt}, nil
}

func (s *MFAService) VerifyChallenge(
	ctx context.Context,
	input VerifyMFAChallengeInput,
) (*BeginContextSelectionInput, error) {
	if s == nil || s.repository == nil || s.cipher == nil ||
		len(input.ChallengeToken) <= len("addp_mfc_") || input.ChallengeToken[:len("addp_mfc_")] != "addp_mfc_" ||
		!totpCodePattern.MatchString(input.Code) {
		return nil, commonapi.ErrUnauthorized
	}
	tokenHash := hashOpaqueToken(input.ChallengeToken)
	snapshot, err := s.repository.GetMFAChallengeByHash(ctx, tokenHash)
	if err != nil {
		return nil, hideMFAStorageError(err)
	}
	var result *BeginContextSelectionInput
	var outcomeErr error
	err = s.repository.Transaction(ctx, func(tx *Repository) error {
		principal, err := tx.LockPrincipal(ctx, snapshot.PrincipalID)
		if err != nil {
			return hideMFAStorageError(err)
		}
		challenge, err := tx.LockMFAChallengeByHash(ctx, tokenHash)
		if err != nil {
			return hideMFAStorageError(err)
		}
		now := s.now().UTC()
		if challenge.ID != snapshot.ID || challenge.PrincipalID != principal.ID || challenge.Purpose != "login" ||
			challenge.SourceFamilyID != nil || challenge.ConsumedAt != nil ||
			!challenge.ExpiresAt.After(now) || challenge.FailedAttempts >= maxMFAFailedAttempts ||
			principal.PrincipalType != PrincipalTypeUser || principal.Status != PrincipalStatusActive ||
			principal.AuthorizationVersion != challenge.IssuedAuthorizationVersion {
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
			if err := NewAuditWriter(tx).Write(ctx, AuditEvent{
				Metadata:  authenticationAuditMetadata(input.Audit, principal.ID),
				EventName: "iam.mfa.verification_failed", Result: AuditResultDenied,
				RiskLevel: AuditRiskHigh, ModuleName: "system",
				EntityType: "mfa_challenge", EntityID: strconv.FormatInt(challenge.ID, 10),
				Details: map[string]any{"method": "totp", "failed_attempts": nextAttempts},
			}); err != nil {
				return err
			}
			outcomeErr = commonapi.ErrUnauthorized
			return nil
		}
		if err := tx.UpdateMFALastAcceptedCounter(ctx, credential.ID, counter); err != nil {
			return err
		}
		if err := tx.ConsumeMFAChallenge(ctx, challenge.ID, now); err != nil {
			return err
		}
		if err := NewAuditWriter(tx).Write(ctx, AuditEvent{
			Metadata:  authenticationAuditMetadata(input.Audit, principal.ID),
			EventName: "iam.mfa.verified", Result: AuditResultSucceeded,
			RiskLevel: AuditRiskHigh, ModuleName: "system",
			EntityType: "mfa_credential", EntityID: strconv.FormatInt(credential.ID, 10),
			Details: map[string]any{"method": "totp"},
		}); err != nil {
			return err
		}
		stepUpExpiresAt := now.Add(s.config.StepUpTTL)
		result = &BeginContextSelectionInput{
			PrincipalID: principal.ID,
			Authentication: SessionAuthentication{
				Methods: []string{"password", "totp"}, AssuranceLevel: AssuranceLevelAAL2,
				AuthenticatedAt: now, StepUpExpiresAt: &stepUpExpiresAt,
			},
			Audit: input.Audit,
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if outcomeErr != nil {
		return nil, outcomeErr
	}
	return result, nil
}

func matchTOTPCode(secret, code string, now time.Time, lastAcceptedCounter *int64) (int64, bool) {
	for _, offset := range []int{-1, 0, 1} {
		candidateTime := now.Add(time.Duration(offset*totpPeriodSeconds) * time.Second)
		candidate, err := totp.GenerateCodeCustom(secret, candidateTime, totp.ValidateOpts{
			Period: totpPeriodSeconds, Skew: 0, Digits: otp.DigitsSix, Algorithm: otp.AlgorithmSHA1,
		})
		if err != nil || subtle.ConstantTimeCompare([]byte(candidate), []byte(code)) != 1 {
			continue
		}
		counter := candidateTime.Unix() / totpPeriodSeconds
		if lastAcceptedCounter != nil && counter <= *lastAcceptedCounter {
			return 0, false
		}
		return counter, true
	}
	return 0, false
}

func hideMFAStorageError(err error) error {
	if errors.Is(err, commonapi.ErrNotFound) {
		return commonapi.ErrUnauthorized
	}
	return err
}
