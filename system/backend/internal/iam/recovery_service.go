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
	PrincipalID int64
	RoleKey     string
	Password    string
	TOTPSecret  string
	TOTPProofs  []BootstrapTOTPProof
}

type RecoveryApplyInput struct {
	RecoverySecret string
	Administrators []RecoveryAdministratorInput
}

type RecoveryResult struct {
	AttemptID   int64
	CompletedAt time.Time
	Principals  map[int64]string
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
	result := &RecoveryResult{CompletedAt: now, Principals: make(map[int64]string, len(prepared))}
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
		if len(prepared) != len(targets) {
			return commonapi.ErrConflict
		}
		targetByPrincipal := make(map[int64]RecoveryAdministratorTarget, len(targets))
		for _, target := range targets {
			targetByPrincipal[target.PrincipalID] = target
		}
		for _, administrator := range prepared {
			target, exists := targetByPrincipal[administrator.PrincipalID]
			if !exists || target.RoleKey != administrator.RoleKey {
				return commonapi.ErrConflict
			}
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
			result.Principals[target.PrincipalID] = target.RoleKey
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

func (s *RecoveryService) prepareAdministratorInputs(administrators []RecoveryAdministratorInput) ([]preparedRecoveryAdministrator, error) {
	if len(administrators) < len(bootstrapRoleOrder) {
		return nil, fmt.Errorf("%w: all current administrator identities are required", commonapi.ErrBadRequest)
	}
	now := s.now().UTC()
	seen := map[int64]bool{}
	passwords := map[string]bool{}
	roles := map[string]bool{}
	prepared := make([]preparedRecoveryAdministrator, 0, len(administrators))
	for _, a := range administrators {
		if a.PrincipalID <= 0 || seen[a.PrincipalID] || !isBootstrapRole(a.RoleKey) || len([]rune(a.Password)) < minimumBootstrapPasswordLength || passwords[a.Password] {
			return nil, commonapi.ErrBadRequest
		}
		counter, err := validateBootstrapTOTPProofs(a.TOTPSecret, a.TOTPProofs, now)
		if err != nil {
			return nil, err
		}
		hash, err := passwordutils.HashPassword(a.Password)
		if err != nil {
			return nil, err
		}
		seen[a.PrincipalID] = true
		passwords[a.Password] = true
		roles[a.RoleKey] = true
		prepared = append(prepared, preparedRecoveryAdministrator{RecoveryAdministratorInput: a, passwordHash: hash, lastAcceptedCounter: counter})
	}
	for _, role := range bootstrapRoleOrder {
		if !roles[role] {
			return nil, commonapi.ErrBadRequest
		}
	}
	return prepared, nil
}
func validateRecoveryTargets(targets []RecoveryAdministratorTarget) error {
	roles := map[string]bool{}
	principals := map[int64]bool{}
	for _, target := range targets {
		if !isBootstrapRole(target.RoleKey) || principals[target.PrincipalID] || (target.PrincipalStatus != PrincipalStatusActive && target.PrincipalStatus != PrincipalStatusSuspended) || (target.AccountStatus != LocalAccountStatusActive && target.AccountStatus != LocalAccountStatusLocked) {
			return fmt.Errorf("%w: administrator identity is not recoverable", commonapi.ErrConflict)
		}
		roles[target.RoleKey] = true
		principals[target.PrincipalID] = true
	}
	for _, role := range bootstrapRoleOrder {
		if !roles[role] {
			return fmt.Errorf("%w: administrator role holder is missing", commonapi.ErrConflict)
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
