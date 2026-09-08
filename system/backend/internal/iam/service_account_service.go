package iam

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"

	commonapi "github.com/addp/common/api"
	"golang.org/x/crypto/bcrypt"
)

const (
	tenantServiceClientIDPrefix       = "addp_svc_"
	maxTenantServiceAccountNameLength = 120
	maxTenantServiceDescriptionLength = 500
)

type CreateTenantServiceAccountInput struct {
	TenantID, ActorPrincipalID int64
	Name, Description          string
	Audit                      AuditMetadata
}

type UpdateTenantServiceAccountInput struct {
	TenantID, AccountID, Version, ActorPrincipalID int64
	Name, Description                              string
	Audit                                          AuditMetadata
}

type ChangeTenantServiceAccountStatusInput struct {
	TenantID, AccountID, Version, ActorPrincipalID int64
	Reason                                         string
	Audit                                          AuditMetadata
}

type RotateTenantServiceAccountSecretInput struct {
	TenantID, AccountID, Version, ActorPrincipalID int64
	Reason                                         string
	Audit                                          AuditMetadata
}

type TenantServiceAccountCredential struct {
	Account      TenantServiceAccount
	ClientSecret string
}

type TenantServiceAccountService struct {
	repository *Repository
}

func NewTenantServiceAccountService(repository *Repository) *TenantServiceAccountService {
	return &TenantServiceAccountService{repository: repository}
}

func (s *TenantServiceAccountService) List(ctx context.Context, tenantID int64, page, pageSize int, search string, status *PrincipalStatus) ([]TenantServiceAccount, int64, error) {
	if tenantID <= 0 || validateManagementPagination(page, pageSize) != nil || (status != nil && !validServiceAccountStatus(*status)) {
		return nil, 0, commonapi.ErrBadRequest
	}
	return s.repository.ListTenantServiceAccounts(ctx, tenantID, page, pageSize, search, status)
}

func (s *TenantServiceAccountService) Get(ctx context.Context, tenantID, accountID int64) (*TenantServiceAccount, error) {
	if tenantID <= 0 || accountID <= 0 {
		return nil, commonapi.ErrBadRequest
	}
	return s.repository.GetTenantServiceAccount(ctx, tenantID, accountID)
}

func (s *TenantServiceAccountService) Create(ctx context.Context, input CreateTenantServiceAccountInput) (*TenantServiceAccountCredential, error) {
	name, description, err := validateTenantServiceAccountDefinition(input.Name, input.Description)
	if err != nil || input.TenantID <= 0 || input.ActorPrincipalID <= 0 {
		if err != nil {
			return nil, err
		}
		return nil, commonapi.ErrBadRequest
	}
	clientID, err := generateTenantServiceCredential(tenantServiceClientIDPrefix, 18)
	if err != nil {
		return nil, err
	}
	clientSecret, err := generateTenantServiceCredential("", 32)
	if err != nil {
		return nil, err
	}
	secretHash, err := bcrypt.GenerateFromPassword([]byte(clientSecret), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("hash tenant service account secret: %w", err)
	}
	var accountID int64
	err = s.repository.Transaction(ctx, func(tx *Repository) error {
		now, err := tx.CurrentDatabaseTime(ctx)
		if err != nil {
			return err
		}
		accountID, err = tx.CreateTenantServiceAccount(ctx, input.TenantID, input.ActorPrincipalID, name, description, clientID, string(secretHash), now)
		if err != nil {
			return err
		}
		return writeTenantServiceAccountAudit(ctx, tx, input.Audit, "iam.service_account.created", AuditRiskHigh, accountID, map[string]any{
			"tenant_id": input.TenantID, "client_id": clientID,
		})
	})
	if err != nil {
		return nil, err
	}
	account, err := s.Get(ctx, input.TenantID, accountID)
	if err != nil {
		return nil, err
	}
	return &TenantServiceAccountCredential{Account: *account, ClientSecret: clientSecret}, nil
}

func (s *TenantServiceAccountService) Update(ctx context.Context, input UpdateTenantServiceAccountInput) (*TenantServiceAccount, error) {
	name, description, err := validateTenantServiceAccountDefinition(input.Name, input.Description)
	if err != nil || !validTenantServiceAccountMutation(input.TenantID, input.AccountID, input.Version, input.ActorPrincipalID) {
		if err != nil {
			return nil, err
		}
		return nil, commonapi.ErrBadRequest
	}
	err = s.repository.Transaction(ctx, func(tx *Repository) error {
		if _, err := tx.LockTenantServiceAccount(ctx, input.TenantID, input.AccountID); err != nil {
			return err
		}
		if err := tx.UpdateTenantServiceAccountDefinition(ctx, input.TenantID, input.AccountID, input.Version, name, description); err != nil {
			return err
		}
		return writeTenantServiceAccountAudit(ctx, tx, input.Audit, "iam.service_account.updated", AuditRiskMedium, input.AccountID, map[string]any{"tenant_id": input.TenantID})
	})
	if err != nil {
		return nil, err
	}
	return s.Get(ctx, input.TenantID, input.AccountID)
}

func (s *TenantServiceAccountService) Suspend(ctx context.Context, input ChangeTenantServiceAccountStatusInput) (*TenantServiceAccount, error) {
	return s.changeStatus(ctx, input, PrincipalStatusActive, PrincipalStatusSuspended, TenantMembershipStatusActive, TenantMembershipStatusSuspended, OAuthClientStatusActive, OAuthClientStatusDisabled, "iam.service_account.suspended")
}

func (s *TenantServiceAccountService) Restore(ctx context.Context, input ChangeTenantServiceAccountStatusInput) (*TenantServiceAccount, error) {
	return s.changeStatus(ctx, input, PrincipalStatusSuspended, PrincipalStatusActive, TenantMembershipStatusSuspended, TenantMembershipStatusActive, OAuthClientStatusDisabled, OAuthClientStatusActive, "iam.service_account.restored")
}

func (s *TenantServiceAccountService) changeStatus(
	ctx context.Context,
	input ChangeTenantServiceAccountStatusInput,
	from, to PrincipalStatus,
	fromMembership, toMembership TenantMembershipStatus,
	fromCredential, toCredential OAuthClientStatus,
	event string,
) (*TenantServiceAccount, error) {
	reason := strings.TrimSpace(input.Reason)
	if !validTenantServiceAccountMutation(input.TenantID, input.AccountID, input.Version, input.ActorPrincipalID) || reason == "" {
		return nil, commonapi.ErrBadRequest
	}
	err := s.repository.Transaction(ctx, func(tx *Repository) error {
		now, err := tx.CurrentDatabaseTime(ctx)
		if err != nil {
			return err
		}
		account, err := tx.LockTenantServiceAccount(ctx, input.TenantID, input.AccountID)
		if err != nil {
			return err
		}
		if account.Status != from || account.MembershipStatus != fromMembership || account.CredentialStatus != fromCredential {
			return commonapi.ErrConflict
		}
		if err := tx.UpdateTenantServiceAccountLifecycle(ctx, input.TenantID, input.AccountID, input.Version, from, to, fromMembership, toMembership, fromCredential, toCredential); err != nil {
			return err
		}
		revoked, err := tx.RevokeActiveTokenFamilies(ctx, input.AccountID, now, "service_account_"+string(to))
		if err != nil {
			return err
		}
		return writeTenantServiceAccountAudit(ctx, tx, input.Audit, event, AuditRiskHigh, input.AccountID, map[string]any{
			"tenant_id": input.TenantID, "reason": reason, "revoked_token_families": revoked,
		})
	})
	if err != nil {
		return nil, err
	}
	return s.Get(ctx, input.TenantID, input.AccountID)
}

func (s *TenantServiceAccountService) RotateSecret(ctx context.Context, input RotateTenantServiceAccountSecretInput) (*TenantServiceAccountCredential, error) {
	reason := strings.TrimSpace(input.Reason)
	if !validTenantServiceAccountMutation(input.TenantID, input.AccountID, input.Version, input.ActorPrincipalID) || reason == "" {
		return nil, commonapi.ErrBadRequest
	}
	clientSecret, err := generateTenantServiceCredential("", 32)
	if err != nil {
		return nil, err
	}
	secretHash, err := bcrypt.GenerateFromPassword([]byte(clientSecret), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("hash tenant service account secret: %w", err)
	}
	err = s.repository.Transaction(ctx, func(tx *Repository) error {
		now, err := tx.CurrentDatabaseTime(ctx)
		if err != nil {
			return err
		}
		account, err := tx.LockTenantServiceAccount(ctx, input.TenantID, input.AccountID)
		if err != nil {
			return err
		}
		if err := tx.RotateTenantServiceAccountSecret(ctx, input.TenantID, input.AccountID, input.Version, string(secretHash)); err != nil {
			return err
		}
		if _, err := tx.IncrementPrincipalAuthorizationVersion(ctx, input.AccountID); err != nil {
			return err
		}
		revoked, err := tx.RevokeActiveTokenFamilies(ctx, input.AccountID, now, "service_account_secret_rotated")
		if err != nil {
			return err
		}
		return writeTenantServiceAccountAudit(ctx, tx, input.Audit, "iam.service_account.secret_rotated", AuditRiskHigh, input.AccountID, map[string]any{
			"tenant_id": input.TenantID, "client_id": account.ClientID, "reason": reason, "revoked_token_families": revoked,
		})
	})
	if err != nil {
		return nil, err
	}
	account, err := s.Get(ctx, input.TenantID, input.AccountID)
	if err != nil {
		return nil, err
	}
	return &TenantServiceAccountCredential{Account: *account, ClientSecret: clientSecret}, nil
}

func validateTenantServiceAccountDefinition(name, description string) (string, string, error) {
	name = strings.TrimSpace(name)
	description = strings.TrimSpace(description)
	if name == "" || len([]rune(name)) > maxTenantServiceAccountNameLength || len([]rune(description)) > maxTenantServiceDescriptionLength {
		return "", "", commonapi.ErrBadRequest
	}
	return name, description, nil
}

func validServiceAccountStatus(status PrincipalStatus) bool {
	return status == PrincipalStatusActive || status == PrincipalStatusSuspended
}

func validTenantServiceAccountMutation(tenantID, accountID, version, actorPrincipalID int64) bool {
	return tenantID > 0 && accountID > 0 && version > 0 && actorPrincipalID > 0
}

func generateTenantServiceCredential(prefix string, byteLength int) (string, error) {
	randomBytes := make([]byte, byteLength)
	if _, err := rand.Read(randomBytes); err != nil {
		return "", fmt.Errorf("generate tenant service account credential: %w", err)
	}
	return prefix + base64.RawURLEncoding.EncodeToString(randomBytes), nil
}

func writeTenantServiceAccountAudit(ctx context.Context, tx *Repository, metadata AuditMetadata, event string, risk AuditRiskLevel, accountID int64, details map[string]any) error {
	return NewAuditWriter(tx).Write(ctx, AuditEvent{
		Metadata: metadata, EventName: event, Result: AuditResultSucceeded, RiskLevel: risk,
		ModuleName: "system", EntityType: "service_account", EntityID: strconv.FormatInt(accountID, 10), Details: details,
	})
}
