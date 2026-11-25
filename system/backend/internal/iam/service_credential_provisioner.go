package iam

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/logpipeline"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var builtinServiceClientIDs = []string{
	"addp-system",
	"addp-log-observer",
	"addp-prometheus",
	"addp-ontology",
	"addp-agent",
	"addp-asset",
	"addp-catalog",
	"addp-workbench",
	"addp-copilot",
	"addp-develop",
	"addp-jupyter",
	"addp-supermap",
	"addp-document",
	"addp-duckdb",
	"addp-gateway",
	"addp-graph",
	"addp-geopython",
	"addp-manager",
	"addp-inference",
	"addp-meta",
	"addp-model",
	"addp-model3d",
	"addp-monitor",
	"addp-orchestrator",
	"addp-portal",
	"addp-quality",
	"addp-security",
	"addp-pointcloud",
	"addp-service",
	"addp-standard",
	"addp-spark",
	"addp-transfer",
}

var builtinTenantRuntimeServiceClientIDs = []string{
	"addp-system",
	"addp-ontology",
	"addp-agent",
	"addp-asset",
	"addp-catalog",
	"addp-copilot",
	"addp-develop",
	"addp-document",
	"addp-duckdb",
	"addp-graph",
	"addp-geopython",
	"addp-manager",
	"addp-meta",
	"addp-model",
	"addp-monitor",
	"addp-model3d",
	"addp-orchestrator",
	"addp-quality",
	"addp-security",
	"addp-pointcloud",
	"addp-service",
	"addp-spark",
	"addp-standard",
	"addp-transfer",
}

func optionalServiceCredential(clientID string) bool {
	switch clientID {
	case "addp-system", "addp-log-observer", "addp-prometheus", "addp-supermap", "addp-jupyter":
		return true
	}
	return false
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

// ProvisionPublisher runs only in the deployment credential preparation command.
func (s *ServiceCredentialProvisioner) ProvisionPublisher(ctx context.Context, clientID, secret string) error {
	if !strings.HasPrefix(clientID, "addp-") || !strings.HasSuffix(clientID, "-publisher") || strings.TrimSpace(secret) != secret || len(secret) < 32 || len(secret) > 72 {
		return commonapi.ErrBadRequest
	}
	return s.repository.Transaction(ctx, func(tx *Repository) error { return s.applyClient(ctx, tx, clientID, secret) })
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
	// Removing an optional capability credential disables its client and tokens.
	if secret == "" {
		if client.Status == "disabled" {
			return nil
		}
		now := s.now().UTC()
		if err := repository.db.WithContext(ctx).Model(&serviceOAuthClientCredentialRow{}).Where("client_id = ?", clientID).Updates(map[string]any{"status": "disabled", "updated_at": now}).Error; err != nil {
			return wrapRepositoryError(err)
		}
		if _, err := repository.IncrementPrincipalAuthorizationVersion(ctx, client.ServicePrincipalID); err != nil {
			return err
		}
		if _, err := repository.RevokeActiveTokenFamilies(ctx, client.ServicePrincipalID, now, "service_credential_disabled"); err != nil {
			return err
		}
		return NewAuditWriter(repository).Write(ctx, AuditEvent{EventName: "iam.service_principal.credential.disabled", Result: AuditResultSucceeded, RiskLevel: AuditRiskHigh, ModuleName: "system", EntityType: "service_principal", EntityID: fmt.Sprintf("%d", client.ServicePrincipalID), Details: map[string]any{"client_id": clientID}})
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
		if optionalServiceCredential(clientID) && secret == "" {
			continue
		}
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

// ProvisionLogObserver installs a deployment-owned platform identity for one node.
// The role lock serializes provisioning across concurrent installers.
func (s *ServiceCredentialProvisioner) ProvisionLogObserver(ctx context.Context, node, secret string) error {
	if !logpipeline.Identity.MatchString(node) || secret != "" && (len(secret) < 32 || len(secret) > 72 || strings.TrimSpace(secret) != secret) {
		return commonapi.ErrBadRequest
	}
	clientID := logpipeline.ObserverClientID(node)
	return s.repository.Transaction(ctx, func(tx *Repository) error {
		db := tx.db.WithContext(ctx)
		var role Role
		if err := db.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id IS NULL AND role_key=? AND status='active'", "platform.log_observer_runtime").Take(&role).Error; err != nil {
			return err
		}
		var client serviceOAuthClientCredentialRow
		err := db.Where("client_id=?", clientID).Take(&client).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if err == nil {
			var count int64
			if err := db.Table("system.service_principals s").Joins("JOIN system.role_assignments a ON a.principal_id=s.id").Where("s.id=? AND s.name=? AND s.owner_scope='platform' AND a.role_id=? AND a.scope_type='platform' AND a.status='active'", client.ServicePrincipalID, clientID, role.ID).Count(&count).Error; err != nil {
				return err
			}
			if count != 1 {
				return commonapi.ErrConflict
			}
		} else {
			if secret == "" {
				return nil
			}
			principal := Principal{PrincipalType: PrincipalTypeServicePrincipal, Status: PrincipalStatusActive, AuthorizationVersion: 1}
			if err := tx.CreatePrincipal(ctx, &principal); err != nil {
				return err
			}
			if err := db.Table("system.service_principals").Create(map[string]any{"id": principal.ID, "name": clientID, "description": "Node runtime log observer", "owner_scope": "platform", "created_by_principal_id": principal.ID, "version": 1}).Error; err != nil {
				return err
			}
			if err := db.Table("system.oauth_clients").Create(map[string]any{
				"client_id": clientID, "display_name": clientID, "client_type": "confidential", "service_principal_id": principal.ID,
				"redirect_uris": DBStringList{}, "grant_types": DBStringList{"client_credentials"}, "response_types": DBStringList{},
				"allowed_scopes": DBStringList{"addp.api"}, "allowed_audiences": DBStringList{"addp.api"}, "token_endpoint_auth_method": "client_secret_basic",
				"status": "disabled", "owner_scope": "platform", "request_uris": DBStringList{}, "created_by_principal_id": principal.ID, "version": 1,
			}).Error; err != nil {
				return err
			}
			reason := "Node runtime log observation"
			if err := db.Create(&RoleAssignment{PrincipalID: principal.ID, RoleID: role.ID, ScopeType: "platform", Status: "active", ValidFrom: s.now().UTC(), SourceType: "bootstrap", GrantReason: &reason}).Error; err != nil {
				return err
			}
		}
		var others []serviceOAuthClientCredentialRow
		if err := db.Table("system.oauth_clients c").Select("c.client_secret_hash").Joins("JOIN system.role_assignments a ON a.principal_id=c.service_principal_id").Where("a.role_id=? AND c.client_id<>? AND c.status='active'", role.ID, clientID).Scan(&others).Error; err != nil {
			return err
		}
		for _, other := range others {
			if other.ClientSecretHash != nil && bcrypt.CompareHashAndPassword([]byte(*other.ClientSecretHash), []byte(secret)) == nil {
				return fmt.Errorf("%w: observer credentials must be distinct", commonapi.ErrBadRequest)
			}
		}
		return s.applyClient(ctx, tx, clientID, secret)
	})
}
