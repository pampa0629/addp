package iam

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"strconv"
	"time"

	commonapi "github.com/addp/common/api"
	passwordutils "github.com/addp/system/pkg/utils"
)

const defaultRecoverySecretTTL = time.Hour

type RecoveryAdministratorInput struct {
	RoleKey    string
	Password   string
	TOTPSecret string
	TOTPProofs []BootstrapTOTPProof
}

type RecoveryApplyInput struct {
	RecoverySecret string
	Administrators []RecoveryAdministratorInput
}

type RecoveryResult struct {
	AttemptID   int64
	CompletedAt time.Time
	Principals  map[string]int64
}

type RecoveryService struct {
	repository *Repository
	cipher     *MFACredentialCipher
	secretTTL  time.Duration
	generate   OpaqueTokenGenerator
	now        func() time.Time
}

func NewRecoveryService(
	repository *Repository,
	cipher *MFACredentialCipher,
	secretTTL time.Duration,
	generate OpaqueTokenGenerator,
	now func() time.Time,
) (*RecoveryService, error) {
	if repository == nil || cipher == nil {
		return nil, fmt.Errorf("%w: recovery dependencies are required", commonapi.ErrBadRequest)
	}
	if secretTTL <= 0 {
		secretTTL = defaultRecoverySecretTTL
	}
	if secretTTL > time.Hour {
		return nil, fmt.Errorf("%w: recovery secret TTL cannot exceed one hour", commonapi.ErrBadRequest)
	}
	if generate == nil {
		generate = generateOpaqueToken
	}
	if now == nil {
		now = time.Now
	}
	return &RecoveryService{
		repository: repository, cipher: cipher, secretTTL: secretTTL, generate: generate, now: now,
	}, nil
}

func (s *RecoveryService) Prepare(ctx context.Context) (string, time.Time, error) {
	if s == nil || s.repository == nil {
		return "", time.Time{}, fmt.Errorf("%w: recovery service is required", commonapi.ErrBadRequest)
	}
	secret, err := s.generate("addp_ir_")
	if err != nil {
		return "", time.Time{}, fmt.Errorf("generate recovery secret: %w", err)
	}
	now := s.now().UTC()
	expiresAt := now.Add(s.secretTTL)
	secretHash := hashOpaqueToken(secret)
	err = s.repository.Transaction(ctx, func(tx *Repository) error {
		if err := tx.LockIAMRecoveryTable(ctx); err != nil {
			return err
		}
		if err := requireCompletedBootstrap(ctx, tx); err != nil {
			return err
		}
		expired, err := tx.ExpirePreparedIAMRecoveryAttempts(ctx, now)
		if err != nil {
			return err
		}
		for _, attempt := range expired {
			if err := writeRecoveryAudit(ctx, tx, "iam.recovery.expired", attempt.ID, nil, map[string]any{
				"expired_at": now,
			}); err != nil {
				return err
			}
		}
		prepared, err := tx.HasPreparedIAMRecoveryAttempt(ctx)
		if err != nil {
			return err
		}
		if prepared {
			return fmt.Errorf("%w: an IAM recovery attempt is already prepared", commonapi.ErrConflict)
		}
		targets, err := tx.LockRecoveryAdministratorTargets(ctx, now)
		if err != nil {
			return err
		}
		if err := validateRecoveryTargets(targets); err != nil {
			return err
		}
		attempt := &IAMRecoveryAttempt{
			SecretHash: &secretHash, Status: IAMRecoveryStatusPrepared,
			PreparedAt: now, ExpiresAt: expiresAt,
		}
		if err := tx.CreateIAMRecoveryAttempt(ctx, attempt); err != nil {
			return err
		}
		return writeRecoveryAudit(ctx, tx, "iam.recovery.prepared", attempt.ID, nil, map[string]any{
			"expires_at": expiresAt, "administrator_count": len(targets),
		})
	})
	if err != nil {
		return "", time.Time{}, err
	}
	return secret, expiresAt, nil
}

func (s *RecoveryService) Validate(
	ctx context.Context,
	recoverySecret string,
) ([]RecoveryAdministratorTarget, error) {
	if s == nil || s.repository == nil {
		return nil, fmt.Errorf("%w: recovery service is required", commonapi.ErrBadRequest)
	}
	providedHash, valid := recoverySecretHash(recoverySecret)
	if !valid {
		return nil, commonapi.ErrUnauthorized
	}
	now := s.now().UTC()
	var targets []RecoveryAdministratorTarget
	err := s.repository.Transaction(ctx, func(tx *Repository) error {
		if err := tx.LockIAMRecoveryTable(ctx); err != nil {
			return err
		}
		if _, err := lockValidRecoveryAttempt(ctx, tx, providedHash, now); err != nil {
			return err
		}
		if err := requireCompletedBootstrap(ctx, tx); err != nil {
			return err
		}
		var err error
		targets, err = tx.LockRecoveryAdministratorTargets(ctx, now)
		if err != nil {
			return err
		}
		return validateRecoveryTargets(targets)
	})
	return targets, err
}

func (s *RecoveryService) Apply(ctx context.Context, input RecoveryApplyInput) (*RecoveryResult, error) {
	if s == nil || s.repository == nil {
		return nil, fmt.Errorf("%w: recovery service is required", commonapi.ErrBadRequest)
	}
	prepared, err := s.prepareAdministratorInputs(input.Administrators)
	if err != nil {
		return nil, err
	}
	providedHash, valid := recoverySecretHash(input.RecoverySecret)
	if !valid {
		return nil, commonapi.ErrUnauthorized
	}
	now := s.now().UTC()
	result := &RecoveryResult{CompletedAt: now, Principals: make(map[string]int64, len(prepared))}
	err = s.repository.Transaction(ctx, func(tx *Repository) error {
		if err := tx.LockIAMRecoveryTable(ctx); err != nil {
			return err
		}
		attempt, err := lockValidRecoveryAttempt(ctx, tx, providedHash, now)
		if err != nil {
			return err
		}
		if err := requireCompletedBootstrap(ctx, tx); err != nil {
			return err
		}
		targets, err := tx.LockRecoveryAdministratorTargets(ctx, now)
		if err != nil {
			return err
		}
		if err := validateRecoveryTargets(targets); err != nil {
			return err
		}
		targetByRole := make(map[string]RecoveryAdministratorTarget, len(targets))
		for _, target := range targets {
			targetByRole[target.RoleKey] = target
		}
		for _, administrator := range prepared {
			target := targetByRole[administrator.RoleKey]
			account, err := tx.LockLocalAccountByUserID(ctx, target.PrincipalID)
			if err != nil {
				return err
			}
			if passwordutils.CheckPassword(administrator.Password, account.PasswordHash) {
				return fmt.Errorf("%w: recovery password must differ from the current password", commonapi.ErrBadRequest)
			}
			credential, err := tx.LockActiveMFACredential(ctx, target.PrincipalID)
			if err != nil {
				return fmt.Errorf("%w: active TOTP credential is required for %s", commonapi.ErrConflict, target.RoleKey)
			}
			ciphertext, nonce, keyVersion, err := s.cipher.EncryptTOTPSecret(
				target.PrincipalID, administrator.TOTPSecret,
			)
			if err != nil {
				return err
			}
			if err := tx.DisableMFACredential(ctx, credential.ID); err != nil {
				return err
			}
			if err := tx.CreateMFACredential(ctx, &MFACredential{
				UserID: target.PrincipalID, Method: "totp", Status: MFACredentialStatusActive,
				SecretCiphertext: ciphertext, SecretNonce: nonce, KeyVersion: keyVersion,
				LastAcceptedCounter: &administrator.lastAcceptedCounter,
			}); err != nil {
				return err
			}
			if err := tx.ResetLocalAccountPassword(ctx, target.AccountID, administrator.passwordHash, now); err != nil {
				return err
			}
			authorizationVersion, err := tx.IncrementPrincipalAuthorizationVersion(ctx, target.PrincipalID)
			if err != nil {
				return err
			}
			revokedFamilyCount, err := tx.RevokeActiveTokenFamilies(
				ctx, target.PrincipalID, now, "administrator_credentials_recovered",
			)
			if err != nil {
				return err
			}
			consumedMFAChallengeCount, err := tx.ConsumePendingMFAChallenges(ctx, target.PrincipalID, now)
			if err != nil {
				return err
			}
			consumedContextTicketCount, err := tx.ConsumeActiveContextSelectionTickets(ctx, target.PrincipalID, now)
			if err != nil {
				return err
			}
			principalID := target.PrincipalID
			principalType := PrincipalTypeUser
			if err := writeRecoveryAudit(ctx, tx, "iam.recovery.administrator_credentials_replaced", attempt.ID,
				&AuditMetadata{PrincipalID: &principalID, PrincipalType: &principalType}, map[string]any{
					"role_key": target.RoleKey, "authorization_version": authorizationVersion,
					"revoked_family_count":          revokedFamilyCount,
					"consumed_mfa_challenge_count":  consumedMFAChallengeCount,
					"consumed_context_ticket_count": consumedContextTicketCount,
				}); err != nil {
				return err
			}
			result.Principals[target.RoleKey] = target.PrincipalID
		}
		if err := tx.CompleteIAMRecoveryAttempt(ctx, attempt.ID, now); err != nil {
			return err
		}
		result.AttemptID = attempt.ID
		return writeRecoveryAudit(ctx, tx, "iam.recovery.completed", attempt.ID, nil, map[string]any{
			"administrator_count": len(prepared),
		})
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

type preparedRecoveryAdministrator struct {
	RecoveryAdministratorInput
	passwordHash        string
	lastAcceptedCounter int64
}

func (s *RecoveryService) prepareAdministratorInputs(
	administrators []RecoveryAdministratorInput,
) ([]preparedRecoveryAdministrator, error) {
	if len(administrators) != len(bootstrapRoleOrder) {
		return nil, fmt.Errorf("%w: recovery requires exactly three administrators", commonapi.ErrBadRequest)
	}
	now := s.now().UTC()
	byRole := make(map[string]RecoveryAdministratorInput, len(administrators))
	passwords := make(map[string]struct{}, len(administrators))
	counters := make(map[string]int64, len(administrators))
	for _, administrator := range administrators {
		if _, exists := byRole[administrator.RoleKey]; exists || !isBootstrapRole(administrator.RoleKey) {
			return nil, fmt.Errorf("%w: invalid or duplicate recovery role", commonapi.ErrBadRequest)
		}
		if len([]rune(administrator.Password)) < minimumBootstrapPasswordLength {
			return nil, fmt.Errorf("%w: recovery password must contain at least 14 characters", commonapi.ErrBadRequest)
		}
		if _, exists := passwords[administrator.Password]; exists {
			return nil, fmt.Errorf("%w: recovery passwords must be distinct", commonapi.ErrBadRequest)
		}
		counter, err := validateBootstrapTOTPProofs(administrator.TOTPSecret, administrator.TOTPProofs, now)
		if err != nil {
			return nil, err
		}
		byRole[administrator.RoleKey] = administrator
		passwords[administrator.Password] = struct{}{}
		counters[administrator.RoleKey] = counter
	}
	prepared := make([]preparedRecoveryAdministrator, 0, len(bootstrapRoleOrder))
	for _, roleKey := range bootstrapRoleOrder {
		administrator, exists := byRole[roleKey]
		if !exists {
			return nil, fmt.Errorf("%w: missing recovery role %s", commonapi.ErrBadRequest, roleKey)
		}
		passwordHash, err := passwordutils.HashPassword(administrator.Password)
		if err != nil {
			return nil, fmt.Errorf("hash recovery password: %w", err)
		}
		prepared = append(prepared, preparedRecoveryAdministrator{
			RecoveryAdministratorInput: administrator,
			passwordHash:               passwordHash, lastAcceptedCounter: counters[roleKey],
		})
	}
	return prepared, nil
}

func validateRecoveryTargets(targets []RecoveryAdministratorTarget) error {
	if len(targets) != len(bootstrapRoleOrder) {
		return fmt.Errorf("%w: recovery requires exactly one active holder for each administrator role", commonapi.ErrConflict)
	}
	byRole := make(map[string]RecoveryAdministratorTarget, len(targets))
	principals := make(map[int64]struct{}, len(targets))
	for _, target := range targets {
		if _, exists := byRole[target.RoleKey]; exists || !isBootstrapRole(target.RoleKey) {
			return fmt.Errorf("%w: administrator role holders are ambiguous", commonapi.ErrConflict)
		}
		if target.PrincipalStatus != PrincipalStatusActive ||
			(target.AccountStatus != LocalAccountStatusActive && target.AccountStatus != LocalAccountStatusLocked) {
			return fmt.Errorf("%w: administrator identity is not recoverable", commonapi.ErrConflict)
		}
		if _, exists := principals[target.PrincipalID]; exists {
			return fmt.Errorf("%w: administrator roles must be held by three distinct users", commonapi.ErrConflict)
		}
		byRole[target.RoleKey] = target
		principals[target.PrincipalID] = struct{}{}
	}
	for _, roleKey := range bootstrapRoleOrder {
		if _, exists := byRole[roleKey]; !exists {
			return fmt.Errorf("%w: missing administrator role %s", commonapi.ErrConflict, roleKey)
		}
	}
	return nil
}

func requireCompletedBootstrap(ctx context.Context, repository *Repository) error {
	state, err := repository.LockIAMBootstrapState(ctx)
	if err != nil {
		return fmt.Errorf("%w: completed IAM bootstrap is required", commonapi.ErrConflict)
	}
	if state.Status != IAMBootstrapStatusCompleted || state.SecretHash != nil || state.CompletedAt == nil {
		return fmt.Errorf("%w: completed IAM bootstrap is required", commonapi.ErrConflict)
	}
	return nil
}

func writeRecoveryAudit(
	ctx context.Context,
	repository *Repository,
	eventName string,
	attemptID int64,
	metadata *AuditMetadata,
	details map[string]any,
) error {
	auditMetadata := AuditMetadata{}
	if metadata != nil {
		auditMetadata = *metadata
	}
	return NewAuditWriter(repository).Write(ctx, AuditEvent{
		Metadata: auditMetadata, EventName: eventName, Result: AuditResultSucceeded,
		RiskLevel: AuditRiskCritical, ModuleName: "system",
		EntityType: "iam_recovery_attempt", EntityID: strconv.FormatInt(attemptID, 10),
		Details: details,
	})
}

func hideRecoveryStateError(err error) error {
	if errors.Is(err, commonapi.ErrNotFound) {
		return commonapi.ErrUnauthorized
	}
	return err
}

func recoverySecretHash(secret string) (string, bool) {
	if len(secret) <= len("addp_ir_") || secret[:len("addp_ir_")] != "addp_ir_" {
		return "", false
	}
	return hashOpaqueToken(secret), true
}

func lockValidRecoveryAttempt(
	ctx context.Context,
	repository *Repository,
	providedHash string,
	now time.Time,
) (*IAMRecoveryAttempt, error) {
	attempt, err := repository.LockIAMRecoveryAttemptByHash(ctx, providedHash)
	if err != nil {
		return nil, hideRecoveryStateError(err)
	}
	if attempt.Status != IAMRecoveryStatusPrepared || attempt.SecretHash == nil || !attempt.ExpiresAt.After(now) ||
		subtle.ConstantTimeCompare([]byte(*attempt.SecretHash), []byte(providedHash)) != 1 {
		return nil, commonapi.ErrUnauthorized
	}
	return attempt, nil
}
