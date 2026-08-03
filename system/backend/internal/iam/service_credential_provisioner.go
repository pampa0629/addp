package iam

import (
	"context"
	"fmt"
	"strings"
	"time"

	commonapi "github.com/addp/common/api"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm/clause"
)

var builtinServiceClientIDs = []string{
	"addp-asset",
	"addp-develop",
	"addp-duckdb",
	"addp-manager",
	"addp-meta",
	"addp-monitor",
	"addp-orchestrator",
	"addp-portal",
	"addp-quality",
	"addp-service",
	"addp-transfer",
}

type serviceOAuthClientCredentialRow struct {
	ClientID           string  `gorm:"column:client_id"`
	ClientSecretHash   *string `gorm:"column:client_secret_hash"`
	ServicePrincipalID int64   `gorm:"column:service_principal_id"`
	Status             string  `gorm:"column:status"`
}

func (serviceOAuthClientCredentialRow) TableName() string { return "system.oauth_clients" }

type ServiceCredentialProvisioner struct {
	repository *Repository
	now        func() time.Time
}

func NewServiceCredentialProvisioner(repository *Repository, now func() time.Time) (*ServiceCredentialProvisioner, error) {
	if repository == nil {
		return nil, fmt.Errorf("%w: IAM repository is required", commonapi.ErrBadRequest)
	}
	if now == nil {
		now = time.Now
	}
	return &ServiceCredentialProvisioner{repository: repository, now: now}, nil
}

func (s *ServiceCredentialProvisioner) Apply(ctx context.Context, secrets map[string]string) error {
	if err := validateBuiltinServiceSecrets(secrets); err != nil {
		return err
	}
	return s.repository.Transaction(ctx, func(tx *Repository) error {
		for _, clientID := range builtinServiceClientIDs {
			if err := s.applyClient(ctx, tx, clientID, secrets[clientID]); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *ServiceCredentialProvisioner) applyClient(ctx context.Context, repository *Repository, clientID, secret string) error {
	var client serviceOAuthClientCredentialRow
	if err := repository.db.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("client_id = ?", clientID).Take(&client).Error; err != nil {
		return wrapRepositoryError(err)
	}
	if client.ServicePrincipalID <= 0 {
		return fmt.Errorf("%w: OAuth client %s is not bound to a service principal", commonapi.ErrConflict, clientID)
	}
	if client.ClientSecretHash != nil && bcrypt.CompareHashAndPassword([]byte(*client.ClientSecretHash), []byte(secret)) == nil {
		if client.Status == "active" {
			return nil
		}
		return wrapRepositoryError(repository.db.WithContext(ctx).Model(&serviceOAuthClientCredentialRow{}).
			Where("client_id = ?", clientID).Update("status", "active").Error)
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(secret), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash OAuth client secret %s: %w", clientID, err)
	}
	now := s.now().UTC()
	result := repository.db.WithContext(ctx).Model(&serviceOAuthClientCredentialRow{}).
		Where("client_id = ? AND service_principal_id = ?", clientID, client.ServicePrincipalID).
		Updates(map[string]any{"client_secret_hash": string(hash), "status": "active", "updated_at": now})
	if result.Error != nil {
		return wrapRepositoryError(result.Error)
	}
	if result.RowsAffected != 1 {
		return commonapi.ErrConflict
	}
	if client.ClientSecretHash != nil {
		if _, err := repository.IncrementPrincipalAuthorizationVersion(ctx, client.ServicePrincipalID); err != nil {
			return err
		}
		if _, err := repository.RevokeActiveTokenFamilies(ctx, client.ServicePrincipalID, now, "service_credential_rotated"); err != nil {
			return err
		}
	}
	eventName := "iam.service_principal.credential.provisioned"
	if client.ClientSecretHash != nil {
		eventName = "iam.service_principal.credential.rotated"
	}
	return NewAuditWriter(repository).Write(ctx, AuditEvent{
		EventName: eventName, Result: AuditResultSucceeded, RiskLevel: AuditRiskHigh,
		ModuleName: "system", EntityType: "service_principal",
		EntityID: fmt.Sprintf("%d", client.ServicePrincipalID),
		Details:  map[string]any{"client_id": clientID, "credential_method": "client_secret_basic"},
	})
}

func validateBuiltinServiceSecrets(secrets map[string]string) error {
	if len(secrets) != len(builtinServiceClientIDs) {
		return fmt.Errorf("%w: all built-in service client secrets are required", commonapi.ErrBadRequest)
	}
	seen := make(map[string]string, len(secrets))
	for _, clientID := range builtinServiceClientIDs {
		secret := secrets[clientID]
		if secret != strings.TrimSpace(secret) || len(secret) < 32 || len(secret) > 72 {
			return fmt.Errorf("%w: %s secret must contain 32-72 non-whitespace bytes", commonapi.ErrBadRequest, clientID)
		}
		if previous, exists := seen[secret]; exists {
			return fmt.Errorf("%w: %s and %s must not share a client secret", commonapi.ErrBadRequest, previous, clientID)
		}
		seen[secret] = clientID
	}
	return nil
}
