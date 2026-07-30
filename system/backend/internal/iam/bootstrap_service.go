package iam

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	commonapi "github.com/addp/common/api"
	passwordutils "github.com/addp/system/pkg/utils"
)

const (
	defaultBootstrapSecretTTL      = time.Hour
	minimumBootstrapPasswordLength = 14
)

var bootstrapRoleOrder = []string{
	"platform.system_administrator",
	"platform.security_administrator",
	"platform.audit_administrator",
}

var bootstrapRoles = map[string]struct{}{
	"platform.system_administrator":   {},
	"platform.security_administrator": {},
	"platform.audit_administrator":    {},
}

type BootstrapTOTPProof struct {
	Code       string
	VerifiedAt time.Time
}

type BootstrapAdministratorInput struct {
	RoleKey      string
	Username     string
	Password     string
	DisplayName  string
	PrimaryEmail *string
	Locale       *string
	TOTPSecret   string
	TOTPProofs   []BootstrapTOTPProof
}

type BootstrapApplyInput struct {
	BootstrapSecret string
	Administrators  []BootstrapAdministratorInput
}

type BootstrapResult struct {
	CompletedAt  time.Time
	PrincipalIDs map[string]int64
}

type BootstrapService struct {
	repository      *Repository
	identityService *IdentityService
	cipher          *MFACredentialCipher
	secretTTL       time.Duration
	generate        OpaqueTokenGenerator
	now             func() time.Time
}

func NewBootstrapService(
	repository *Repository,
	identityService *IdentityService,
	cipher *MFACredentialCipher,
	secretTTL time.Duration,
	generate OpaqueTokenGenerator,
	now func() time.Time,
) (*BootstrapService, error) {
	if repository == nil || identityService == nil || cipher == nil {
		return nil, fmt.Errorf("%w: bootstrap dependencies are required", commonapi.ErrBadRequest)
	}
	if secretTTL <= 0 {
		secretTTL = defaultBootstrapSecretTTL
	}
	if secretTTL > time.Hour {
		return nil, fmt.Errorf("%w: bootstrap secret TTL cannot exceed one hour", commonapi.ErrBadRequest)
	}
	if generate == nil {
		generate = generateOpaqueToken
	}
	if now == nil {
		now = time.Now
	}
	return &BootstrapService{
		repository: repository, identityService: identityService, cipher: cipher,
		secretTTL: secretTTL, generate: generate, now: now,
	}, nil
}

func (s *BootstrapService) Prepare(ctx context.Context) (string, time.Time, error) {
	if s == nil || s.repository == nil {
		return "", time.Time{}, fmt.Errorf("%w: bootstrap service is required", commonapi.ErrBadRequest)
	}
	secret, err := s.generate("addp_bs_")
	if err != nil {
		return "", time.Time{}, fmt.Errorf("generate bootstrap secret: %w", err)
	}
	now := s.now().UTC()
	expiresAt := now.Add(s.secretTTL)
	secretHash := hashOpaqueToken(secret)
	err = s.repository.Transaction(ctx, func(tx *Repository) error {
		if err := tx.LockIAMBootstrapTable(ctx); err != nil {
			return err
		}
		if _, err := tx.LockIAMBootstrapState(ctx); err == nil {
			return fmt.Errorf("%w: IAM bootstrap was already prepared", commonapi.ErrConflict)
		} else if !errors.Is(err, commonapi.ErrNotFound) {
			return err
		}
		factCount, err := tx.CountIAMBootstrapBlockingUserFacts(ctx)
		if err != nil {
			return err
		}
		if factCount != 0 {
			return fmt.Errorf("%w: IAM bootstrap requires no existing user principals", commonapi.ErrConflict)
		}
		if err := tx.CreateIAMBootstrapState(ctx, &IAMBootstrapState{
			Singleton: true, Status: IAMBootstrapStatusPrepared, SecretHash: &secretHash,
			PreparedAt: now, ExpiresAt: expiresAt,
		}); err != nil {
			return err
		}
		return NewAuditWriter(tx).Write(ctx, AuditEvent{
			EventName: "iam.bootstrap.prepared", Result: AuditResultSucceeded,
			RiskLevel: AuditRiskCritical, ModuleName: "system",
			EntityType: "iam_bootstrap", EntityID: "singleton",
			Details: map[string]any{"expires_at": expiresAt},
		})
	})
	if err != nil {
		return "", time.Time{}, err
	}
	return secret, expiresAt, nil
}

func (s *BootstrapService) Apply(ctx context.Context, input BootstrapApplyInput) (*BootstrapResult, error) {
	prepared, err := s.prepareAdministratorInputs(input.Administrators)
	if err != nil {
		return nil, err
	}
	if !strings.HasPrefix(input.BootstrapSecret, "addp_bs_") || len(input.BootstrapSecret) == len("addp_bs_") {
		return nil, commonapi.ErrUnauthorized
	}
	providedHash := hashOpaqueToken(input.BootstrapSecret)
	now := s.now().UTC()
	result := &BootstrapResult{CompletedAt: now, PrincipalIDs: make(map[string]int64, len(prepared))}
	err = s.repository.Transaction(ctx, func(tx *Repository) error {
		state, err := tx.LockIAMBootstrapState(ctx)
		if err != nil {
			return hideBootstrapStateError(err)
		}
		if state.Status != IAMBootstrapStatusPrepared || state.SecretHash == nil || !state.ExpiresAt.After(now) ||
			subtle.ConstantTimeCompare([]byte(*state.SecretHash), []byte(providedHash)) != 1 {
			return commonapi.ErrUnauthorized
		}
		if err := tx.LockIAMBootstrapPrincipalWrites(ctx); err != nil {
			return err
		}
		factCount, err := tx.CountIAMBootstrapBlockingUserFacts(ctx)
		if err != nil {
			return err
		}
		if factCount != 0 {
			return fmt.Errorf("%w: IAM bootstrap user principals already exist", commonapi.ErrConflict)
		}
		for _, administrator := range prepared {
			created, err := s.identityService.createLocalUserTx(
				ctx,
				tx,
				CreateLocalUserInput{
					Username: administrator.Username, DisplayName: administrator.DisplayName,
					PrimaryEmail: administrator.PrimaryEmail, Locale: administrator.Locale,
				},
				administrator.passwordHash,
				now,
			)
			if err != nil {
				return err
			}
			ciphertext, nonce, keyVersion, err := s.cipher.EncryptTOTPSecret(created.PrincipalID, administrator.TOTPSecret)
			if err != nil {
				return err
			}
			if err := tx.CreateMFACredential(ctx, &MFACredential{
				UserID: created.PrincipalID, Method: "totp", Status: MFACredentialStatusActive,
				SecretCiphertext: ciphertext, SecretNonce: nonce, KeyVersion: keyVersion,
				LastAcceptedCounter: &administrator.lastAcceptedCounter,
			}); err != nil {
				return err
			}
			assignmentID, err := tx.CreateBootstrapRoleAssignment(
				ctx, created.PrincipalID, administrator.RoleKey,
				"initial offline IAM bootstrap", now,
			)
			if err != nil {
				return err
			}
			result.PrincipalIDs[administrator.RoleKey] = created.PrincipalID
			if err := NewAuditWriter(tx).Write(ctx, AuditEvent{
				EventName: "iam.bootstrap.administrator_created", Result: AuditResultSucceeded,
				RiskLevel: AuditRiskCritical, ModuleName: "system",
				EntityType: "principal", EntityID: strconv.FormatInt(created.PrincipalID, 10),
				Details: map[string]any{
					"role_key": administrator.RoleKey, "role_assignment_id": assignmentID,
					"authentication_method": "totp",
				},
			}); err != nil {
				return err
			}
		}
		if err := tx.CompleteIAMBootstrap(ctx, now); err != nil {
			return err
		}
		return NewAuditWriter(tx).Write(ctx, AuditEvent{
			EventName: "iam.bootstrap.completed", Result: AuditResultSucceeded,
			RiskLevel: AuditRiskCritical, ModuleName: "system",
			EntityType: "iam_bootstrap", EntityID: "singleton",
			Details: map[string]any{"administrator_count": len(prepared)},
		})
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

type preparedBootstrapAdministrator struct {
	BootstrapAdministratorInput
	passwordHash        string
	lastAcceptedCounter int64
}

func (s *BootstrapService) prepareAdministratorInputs(
	administrators []BootstrapAdministratorInput,
) ([]preparedBootstrapAdministrator, error) {
	if len(administrators) != len(bootstrapRoleOrder) {
		return nil, fmt.Errorf("%w: bootstrap requires exactly three administrators", commonapi.ErrBadRequest)
	}
	now := s.now().UTC()
	byRole := make(map[string]BootstrapAdministratorInput, len(administrators))
	counterByRole := make(map[string]int64, len(administrators))
	usernames := make(map[string]struct{}, len(administrators))
	passwords := make(map[string]struct{}, len(administrators))
	for _, administrator := range administrators {
		if _, exists := byRole[administrator.RoleKey]; exists || !isBootstrapRole(administrator.RoleKey) {
			return nil, fmt.Errorf("%w: invalid or duplicate bootstrap role", commonapi.ErrBadRequest)
		}
		normalizedUsername, err := NormalizeUsername(administrator.Username)
		if err != nil {
			return nil, err
		}
		if _, exists := usernames[normalizedUsername]; exists {
			return nil, fmt.Errorf("%w: bootstrap usernames must be distinct", commonapi.ErrBadRequest)
		}
		if strings.TrimSpace(administrator.DisplayName) == "" {
			return nil, fmt.Errorf("%w: bootstrap display name is required", commonapi.ErrBadRequest)
		}
		if len([]rune(administrator.Password)) < minimumBootstrapPasswordLength {
			return nil, fmt.Errorf("%w: bootstrap password must contain at least 14 characters", commonapi.ErrBadRequest)
		}
		if _, exists := passwords[administrator.Password]; exists {
			return nil, fmt.Errorf("%w: bootstrap passwords must be distinct", commonapi.ErrBadRequest)
		}
		lastAcceptedCounter, err := validateBootstrapTOTPProofs(administrator.TOTPSecret, administrator.TOTPProofs, now)
		if err != nil {
			return nil, err
		}
		administrator.Username = strings.TrimSpace(administrator.Username)
		administrator.DisplayName = strings.TrimSpace(administrator.DisplayName)
		administrator.PrimaryEmail = trimmedOptionalString(administrator.PrimaryEmail)
		administrator.Locale = trimmedOptionalString(administrator.Locale)
		byRole[administrator.RoleKey] = administrator
		counterByRole[administrator.RoleKey] = lastAcceptedCounter
		usernames[normalizedUsername] = struct{}{}
		passwords[administrator.Password] = struct{}{}
	}
	result := make([]preparedBootstrapAdministrator, 0, len(bootstrapRoleOrder))
	for _, roleKey := range bootstrapRoleOrder {
		administrator, exists := byRole[roleKey]
		if !exists {
			return nil, fmt.Errorf("%w: missing bootstrap role %s", commonapi.ErrBadRequest, roleKey)
		}
		passwordHash, err := passwordutils.HashPassword(administrator.Password)
		if err != nil {
			return nil, fmt.Errorf("hash bootstrap password: %w", err)
		}
		administrator.Password = ""
		result = append(result, preparedBootstrapAdministrator{
			BootstrapAdministratorInput: administrator,
			passwordHash:                passwordHash, lastAcceptedCounter: counterByRole[roleKey],
		})
	}
	return result, nil
}

func validateBootstrapTOTPProofs(secret string, proofs []BootstrapTOTPProof, now time.Time) (int64, error) {
	if secret == "" || len(proofs) != 2 || proofs[0].VerifiedAt.IsZero() || proofs[1].VerifiedAt.IsZero() ||
		!proofs[1].VerifiedAt.After(proofs[0].VerifiedAt) || proofs[0].VerifiedAt.Before(now.Add(-5*time.Minute)) ||
		proofs[1].VerifiedAt.After(now.Add(30*time.Second)) {
		return 0, fmt.Errorf("%w: two recent consecutive TOTP proofs are required", commonapi.ErrBadRequest)
	}
	firstCounter, valid := matchTOTPCode(secret, proofs[0].Code, proofs[0].VerifiedAt.UTC(), nil)
	if !valid {
		return 0, fmt.Errorf("%w: first TOTP proof is invalid", commonapi.ErrBadRequest)
	}
	secondCounter, valid := matchTOTPCode(secret, proofs[1].Code, proofs[1].VerifiedAt.UTC(), &firstCounter)
	if !valid || secondCounter != firstCounter+1 {
		return 0, fmt.Errorf("%w: TOTP proofs must use consecutive counters", commonapi.ErrBadRequest)
	}
	return secondCounter, nil
}

func isBootstrapRole(roleKey string) bool {
	_, exists := bootstrapRoles[roleKey]
	return exists
}

func trimmedOptionalString(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func hideBootstrapStateError(err error) error {
	if errors.Is(err, commonapi.ErrNotFound) {
		return commonapi.ErrUnauthorized
	}
	return err
}
