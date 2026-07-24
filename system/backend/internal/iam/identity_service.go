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

const dummyLocalAccountPasswordHash = "$2a$10$UJvKh/XXObz7YPQpQvkDTuBYD8J4R3zoDWrV1v9RRf1f2.FEOaer2"

type CreateLocalUserInput struct {
	Username     string
	Password     string
	DisplayName  string
	PrimaryEmail *string
	Locale       *string
	Audit        AuditMetadata
}

type CreatedLocalUser struct {
	PrincipalID          int64
	AccountID            int64
	AuthorizationVersion int64
}

type AuthenticatedLocalUser struct {
	PrincipalID          int64
	AccountID            int64
	AuthorizationVersion int64
	Username             string
	DisplayName          string
	PrimaryEmail         *string
	Locale               *string
	AuthenticatedAt      time.Time
}

type RotatePasswordInput struct {
	UserID          int64
	CurrentPassword string
	NewPassword     string
	Audit           AuditMetadata
}

type PasswordRotationResult struct {
	AuthorizationVersion int64
	RevokedFamilyCount   int64
	ChangedAt            time.Time
}

var (
	ErrInvalidCurrentPassword = fmt.Errorf("%w: invalid current password", commonapi.ErrBadRequest)
	ErrPasswordUnchanged      = fmt.Errorf("%w: new password must differ from current password", commonapi.ErrBadRequest)
)

type IdentityService struct {
	repository *Repository
	now        func() time.Time
}

func NewIdentityService(repository *Repository, now func() time.Time) *IdentityService {
	if now == nil {
		now = time.Now
	}
	return &IdentityService{repository: repository, now: now}
}

func (s *IdentityService) CreateLocalUser(
	ctx context.Context,
	input CreateLocalUserInput,
) (*CreatedLocalUser, error) {
	if s == nil || s.repository == nil {
		return nil, fmt.Errorf("%w: IAM repository is required", commonapi.ErrBadRequest)
	}
	if strings.TrimSpace(input.DisplayName) == "" {
		return nil, fmt.Errorf("%w: display name is required", commonapi.ErrBadRequest)
	}
	if input.Password == "" {
		return nil, fmt.Errorf("%w: password is required", commonapi.ErrBadRequest)
	}
	if _, err := NormalizeUsername(input.Username); err != nil {
		return nil, err
	}
	passwordHash, err := passwordutils.HashPassword(input.Password)
	if err != nil {
		return nil, fmt.Errorf("%w: hash password: %v", commonapi.ErrBadRequest, err)
	}

	now := s.now().UTC()
	created := &CreatedLocalUser{}
	err = s.repository.Transaction(ctx, func(tx *Repository) error {
		principal := &Principal{
			PrincipalType: PrincipalTypeUser,
			Status:        PrincipalStatusActive,
		}
		if err := tx.CreatePrincipal(ctx, principal); err != nil {
			return err
		}
		if err := tx.CreateUser(ctx, &User{
			ID:           principal.ID,
			DisplayName:  strings.TrimSpace(input.DisplayName),
			PrimaryEmail: input.PrimaryEmail,
			Locale:       input.Locale,
		}); err != nil {
			return err
		}
		account := &LocalAccount{
			UserID:            principal.ID,
			Username:          strings.TrimSpace(input.Username),
			PasswordHash:      passwordHash,
			Status:            LocalAccountStatusActive,
			PasswordChangedAt: now,
		}
		if err := tx.CreateLocalAccount(ctx, account); err != nil {
			return err
		}
		if err := NewAuditWriter(tx).Write(ctx, AuditEvent{
			Metadata:   input.Audit,
			EventName:  "iam.identity.created",
			Result:     AuditResultSucceeded,
			RiskLevel:  AuditRiskMedium,
			ModuleName: "system",
			EntityType: "principal",
			EntityID:   strconv.FormatInt(principal.ID, 10),
			Details: map[string]any{
				"account_id":   account.ID,
				"account_type": "local",
			},
		}); err != nil {
			return err
		}
		created.PrincipalID = principal.ID
		created.AccountID = account.ID
		created.AuthorizationVersion = principal.AuthorizationVersion
		return nil
	})
	if err != nil {
		return nil, err
	}
	return created, nil
}

func (s *IdentityService) AuthenticateLocalAccount(
	ctx context.Context,
	username string,
	password string,
	audit AuditMetadata,
) (*AuthenticatedLocalUser, error) {
	if s == nil || s.repository == nil {
		return nil, fmt.Errorf("%w: IAM repository is required", commonapi.ErrBadRequest)
	}
	normalizedUsername, normalizeErr := NormalizeUsername(username)
	if normalizeErr != nil {
		if err := s.writeAuthenticationFailure(ctx, audit, "unknown", "invalid_request", AuditResultFailed); err != nil {
			return nil, err
		}
		return nil, normalizeErr
	}

	account, err := s.repository.GetLocalAccountByNormalizedUsername(ctx, normalizedUsername)
	if err != nil {
		if !errors.Is(err, commonapi.ErrNotFound) {
			return nil, err
		}
		passwordutils.CheckPassword(password, dummyLocalAccountPasswordHash)
		if auditErr := s.writeAuthenticationFailure(ctx, audit, "unknown", "invalid_credentials", AuditResultDenied); auditErr != nil {
			return nil, auditErr
		}
		return nil, commonapi.ErrUnauthorized
	}

	var authenticated *AuthenticatedLocalUser
	var outcomeErr error
	err = s.repository.Transaction(ctx, func(tx *Repository) error {
		principal, err := tx.LockPrincipal(ctx, account.UserID)
		if err != nil {
			return err
		}
		lockedAccount, err := tx.LockLocalAccountByUserID(ctx, account.UserID)
		if err != nil {
			return err
		}
		user, err := tx.GetUser(ctx, account.UserID)
		if err != nil {
			return err
		}

		failureCode := ""
		if lockedAccount.NormalizedUsername != normalizedUsername ||
			!passwordutils.CheckPassword(password, lockedAccount.PasswordHash) {
			failureCode = "invalid_credentials"
		} else if principal.Status != PrincipalStatusActive || lockedAccount.Status != LocalAccountStatusActive {
			failureCode = "account_unavailable"
		}
		if failureCode != "" {
			outcomeErr = commonapi.ErrUnauthorized
			return NewAuditWriter(tx).Write(ctx, authenticationAuditEvent(
				audit,
				principal.ID,
				lockedAccount.ID,
				"iam.authentication.failed",
				AuditResultDenied,
				failureCode,
			))
		}

		authenticatedAt := s.now().UTC()
		if err := tx.UpdateLocalAccountLastAuthenticated(ctx, lockedAccount.ID, authenticatedAt); err != nil {
			return err
		}
		if err := NewAuditWriter(tx).Write(ctx, authenticationAuditEvent(
			audit,
			principal.ID,
			lockedAccount.ID,
			"iam.authentication.succeeded",
			AuditResultSucceeded,
			"",
		)); err != nil {
			return err
		}
		authenticated = &AuthenticatedLocalUser{
			PrincipalID:          principal.ID,
			AccountID:            lockedAccount.ID,
			AuthorizationVersion: principal.AuthorizationVersion,
			Username:             lockedAccount.Username,
			DisplayName:          user.DisplayName,
			PrimaryEmail:         user.PrimaryEmail,
			Locale:               user.Locale,
			AuthenticatedAt:      authenticatedAt,
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if outcomeErr != nil {
		return nil, outcomeErr
	}
	return authenticated, nil
}

func (s *IdentityService) RotatePassword(
	ctx context.Context,
	input RotatePasswordInput,
) (*PasswordRotationResult, error) {
	if s == nil || s.repository == nil {
		return nil, fmt.Errorf("%w: IAM repository is required", commonapi.ErrBadRequest)
	}
	if input.UserID <= 0 || input.CurrentPassword == "" || input.NewPassword == "" {
		return nil, fmt.Errorf("%w: user and both passwords are required", commonapi.ErrBadRequest)
	}
	newPasswordHash, err := passwordutils.HashPassword(input.NewPassword)
	if err != nil {
		return nil, fmt.Errorf("%w: hash password: %v", commonapi.ErrBadRequest, err)
	}

	var rotated *PasswordRotationResult
	var outcomeErr error
	err = s.repository.Transaction(ctx, func(tx *Repository) error {
		principal, err := tx.LockPrincipal(ctx, input.UserID)
		if err != nil {
			return err
		}
		rotated, outcomeErr, err = s.rotatePasswordTx(ctx, tx, principal, input, newPasswordHash)
		return err
	})
	if err != nil {
		return nil, err
	}
	if outcomeErr != nil {
		return nil, outcomeErr
	}
	return rotated, nil
}

func (s *IdentityService) RotateCurrentUserPassword(
	ctx context.Context,
	accessToken string,
	currentPassword string,
	newPassword string,
	audit AuditMetadata,
) (*PasswordRotationResult, error) {
	if s == nil || s.repository == nil {
		return nil, fmt.Errorf("%w: IAM repository is required", commonapi.ErrBadRequest)
	}
	if currentPassword == "" || newPassword == "" {
		return nil, fmt.Errorf("%w: both passwords are required", commonapi.ErrBadRequest)
	}
	snapshot, err := resolveFirstPartyAccessTokenSnapshot(ctx, s.repository, accessToken)
	if err != nil {
		return nil, err
	}
	newPasswordHash, err := passwordutils.HashPassword(newPassword)
	if err != nil {
		return nil, fmt.Errorf("%w: hash password: %v", commonapi.ErrBadRequest, err)
	}
	input := RotatePasswordInput{
		UserID:          snapshot.FamilyPrincipalID,
		CurrentPassword: currentPassword,
		NewPassword:     newPassword,
		Audit:           audit,
	}

	var rotated *PasswordRotationResult
	var outcomeErr error
	err = s.repository.Transaction(ctx, func(tx *Repository) error {
		principal, err := tx.LockPrincipal(ctx, snapshot.FamilyPrincipalID)
		if err != nil {
			return hideTokenLookupError(err)
		}
		lockedSnapshot, err := resolveFirstPartyAccessTokenSnapshot(ctx, tx, accessToken)
		if err != nil {
			return err
		}
		if lockedSnapshot.TokenID != snapshot.TokenID || lockedSnapshot.FamilyPrincipalID != principal.ID {
			return commonapi.ErrUnauthorized
		}
		rotated, outcomeErr, err = s.rotatePasswordTx(ctx, tx, principal, input, newPasswordHash)
		return err
	})
	if err != nil {
		return nil, err
	}
	if errors.Is(outcomeErr, commonapi.ErrUnauthorized) {
		return nil, ErrInvalidCurrentPassword
	}
	if outcomeErr != nil {
		return nil, outcomeErr
	}
	return rotated, nil
}

func (s *IdentityService) rotatePasswordTx(
	ctx context.Context,
	tx *Repository,
	principal *Principal,
	input RotatePasswordInput,
	newPasswordHash string,
) (*PasswordRotationResult, error, error) {
	account, err := tx.LockLocalAccountByUserID(ctx, principal.ID)
	if err != nil {
		return nil, nil, err
	}
	metadata := auditMetadataWithPrincipalFallback(input.Audit, principal.ID)
	if principal.Status != PrincipalStatusActive || account.Status != LocalAccountStatusActive {
		outcomeErr := commonapi.ErrForbidden
		auditErr := NewAuditWriter(tx).Write(ctx, passwordRotationAuditEvent(
			metadata, account.ID, AuditResultDenied, "account_unavailable", nil,
		))
		return nil, outcomeErr, auditErr
	}
	if !passwordutils.CheckPassword(input.CurrentPassword, account.PasswordHash) {
		outcomeErr := commonapi.ErrUnauthorized
		auditErr := NewAuditWriter(tx).Write(ctx, passwordRotationAuditEvent(
			metadata, account.ID, AuditResultDenied, "invalid_credentials", nil,
		))
		return nil, outcomeErr, auditErr
	}
	if input.CurrentPassword == input.NewPassword {
		auditErr := NewAuditWriter(tx).Write(ctx, passwordRotationAuditEvent(
			metadata, account.ID, AuditResultDenied, "password_unchanged", nil,
		))
		return nil, ErrPasswordUnchanged, auditErr
	}

	changedAt := s.now().UTC()
	if err := tx.UpdateLocalAccountPassword(ctx, account.ID, newPasswordHash, changedAt); err != nil {
		return nil, nil, err
	}
	authorizationVersion, err := tx.IncrementPrincipalAuthorizationVersion(ctx, principal.ID)
	if err != nil {
		return nil, nil, err
	}
	revokedFamilyCount, err := tx.RevokeActiveTokenFamilies(
		ctx, principal.ID, changedAt, "password_rotated",
	)
	if err != nil {
		return nil, nil, err
	}
	rotated := &PasswordRotationResult{
		AuthorizationVersion: authorizationVersion,
		RevokedFamilyCount:   revokedFamilyCount,
		ChangedAt:            changedAt,
	}
	if err := NewAuditWriter(tx).Write(ctx, passwordRotationAuditEvent(
		metadata, account.ID, AuditResultSucceeded, "", map[string]any{
			"authorization_version": authorizationVersion,
			"revoked_family_count":  revokedFamilyCount,
		},
	)); err != nil {
		return nil, nil, err
	}
	return rotated, nil, nil
}

func (s *IdentityService) writeAuthenticationFailure(
	ctx context.Context,
	metadata AuditMetadata,
	entityID string,
	errorCode string,
	result AuditResult,
) error {
	return s.repository.Transaction(ctx, func(tx *Repository) error {
		return NewAuditWriter(tx).Write(ctx, AuditEvent{
			Metadata:   authenticationAuditMetadata(metadata, 0),
			EventName:  "iam.authentication.failed",
			Result:     result,
			RiskLevel:  AuditRiskMedium,
			ModuleName: "system",
			EntityType: "local_account",
			EntityID:   entityID,
			Details:    map[string]any{"error_code": errorCode},
		})
	})
}

func authenticationAuditEvent(
	metadata AuditMetadata,
	principalID int64,
	accountID int64,
	eventName string,
	result AuditResult,
	errorCode string,
) AuditEvent {
	details := map[string]any{}
	if errorCode != "" {
		details["error_code"] = errorCode
	}
	return AuditEvent{
		Metadata:   authenticationAuditMetadata(metadata, principalID),
		EventName:  eventName,
		Result:     result,
		RiskLevel:  AuditRiskMedium,
		ModuleName: "system",
		EntityType: "local_account",
		EntityID:   strconv.FormatInt(accountID, 10),
		Details:    details,
	}
}

func authenticationAuditMetadata(metadata AuditMetadata, principalID int64) AuditMetadata {
	metadata.ContextType = nil
	metadata.TenantID = nil
	if principalID == 0 {
		metadata.PrincipalID = nil
		metadata.PrincipalType = nil
		return metadata
	}
	principalType := PrincipalTypeUser
	metadata.PrincipalID = &principalID
	metadata.PrincipalType = &principalType
	return metadata
}

func auditMetadataWithPrincipalFallback(metadata AuditMetadata, principalID int64) AuditMetadata {
	if metadata.PrincipalID != nil {
		return metadata
	}
	principalType := PrincipalTypeUser
	metadata.PrincipalID = &principalID
	metadata.PrincipalType = &principalType
	return metadata
}

func passwordRotationAuditEvent(
	metadata AuditMetadata,
	accountID int64,
	result AuditResult,
	errorCode string,
	details map[string]any,
) AuditEvent {
	if details == nil {
		details = map[string]any{}
	}
	if errorCode != "" {
		details["error_code"] = errorCode
	}
	return AuditEvent{
		Metadata:   metadata,
		EventName:  "iam.password.rotated",
		Result:     result,
		RiskLevel:  AuditRiskHigh,
		ModuleName: "system",
		EntityType: "local_account",
		EntityID:   strconv.FormatInt(accountID, 10),
		Details:    details,
	}
}
