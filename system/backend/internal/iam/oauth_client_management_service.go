package iam

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strings"

	commonapi "github.com/addp/common/api"
)

const (
	managedOAuthClientIDPrefix = "addp_ext_"
	maxOAuthClientNameLength   = 120
	maxOAuthRedirectURIs       = 10
	maxOAuthRedirectURILength  = 2048
)

var (
	managedOAuthClientIDPattern  = regexp.MustCompile(`^addp_ext_[A-Za-z0-9_-]{16,64}$`)
	managedOAuthGrantTypes       = []string{"authorization_code", "refresh_token"}
	managedOAuthResponseTypes    = []string{"code"}
	managedOAuthAllowedScopes    = []string{"addp.api"}
	managedOAuthAllowedAudiences = []string{"addp.api"}
)

type CreateManagedOAuthClientInput struct {
	TenantID, ActorPrincipalID int64
	DisplayName                string
	RedirectURIs               []string
	Audit                      AuditMetadata
}

type UpdateManagedOAuthClientInput struct {
	TenantID, Version, ActorPrincipalID int64
	ClientID                            string
	DisplayName                         string
	RedirectURIs                        []string
	Audit                               AuditMetadata
}

type ChangeManagedOAuthClientStatusInput struct {
	TenantID, Version, ActorPrincipalID int64
	ClientID                            string
	Reason                              string
	Audit                               AuditMetadata
}

type OAuthClientManagementService struct {
	repository *Repository
}

func NewOAuthClientManagementService(repository *Repository) *OAuthClientManagementService {
	return &OAuthClientManagementService{repository: repository}
}

func (s *OAuthClientManagementService) List(ctx context.Context, tenantID int64, page, pageSize int, search string, status *OAuthClientStatus) ([]ManagedOAuthClient, int64, error) {
	if tenantID <= 0 || validateManagementPagination(page, pageSize) != nil || (status != nil && !validOAuthClientStatus(*status)) {
		return nil, 0, commonapi.ErrBadRequest
	}
	return s.repository.ListManagedOAuthClients(ctx, tenantID, page, pageSize, search, status)
}

func (s *OAuthClientManagementService) Get(ctx context.Context, tenantID int64, clientID string) (*ManagedOAuthClient, error) {
	if tenantID <= 0 || !validManagedOAuthClientID(clientID) {
		return nil, commonapi.ErrBadRequest
	}
	return s.repository.GetManagedOAuthClient(ctx, tenantID, clientID)
}

func (s *OAuthClientManagementService) Create(ctx context.Context, input CreateManagedOAuthClientInput) (*ManagedOAuthClient, error) {
	displayName, redirectURIs, err := validateManagedOAuthClientDefinition(input.DisplayName, input.RedirectURIs)
	if err != nil || input.TenantID <= 0 || input.ActorPrincipalID <= 0 {
		if err != nil {
			return nil, err
		}
		return nil, commonapi.ErrBadRequest
	}
	clientID, err := generateManagedOAuthClientID()
	if err != nil {
		return nil, err
	}
	client := &ManagedOAuthClient{
		ClientID: clientID, DisplayName: displayName, RedirectURIs: redirectURIs,
		GrantTypes:       append([]string(nil), managedOAuthGrantTypes...),
		ResponseTypes:    append([]string(nil), managedOAuthResponseTypes...),
		AllowedScopes:    append([]string(nil), managedOAuthAllowedScopes...),
		AllowedAudiences: append([]string(nil), managedOAuthAllowedAudiences...),
		TokenAuthMethod:  "none", Status: OAuthClientStatusActive, Version: 1,
		CreatedByPrincipal: input.ActorPrincipalID,
	}
	err = s.repository.Transaction(ctx, func(tx *Repository) error {
		if err := tx.CreateManagedOAuthClient(ctx, input.TenantID, client); err != nil {
			return err
		}
		return writeManagedOAuthClientAudit(ctx, tx, input.Audit, "iam.oauth_client.created", AuditRiskMedium, clientID, map[string]any{
			"tenant_id": input.TenantID, "redirect_uri_count": len(redirectURIs),
		})
	})
	if err != nil {
		return nil, err
	}
	return s.Get(ctx, input.TenantID, clientID)
}

func (s *OAuthClientManagementService) Update(ctx context.Context, input UpdateManagedOAuthClientInput) (*ManagedOAuthClient, error) {
	displayName, redirectURIs, err := validateManagedOAuthClientDefinition(input.DisplayName, input.RedirectURIs)
	if err != nil || input.TenantID <= 0 || input.ActorPrincipalID <= 0 || input.Version <= 0 || !validManagedOAuthClientID(input.ClientID) {
		if err != nil {
			return nil, err
		}
		return nil, commonapi.ErrBadRequest
	}
	err = s.repository.Transaction(ctx, func(tx *Repository) error {
		if _, err := tx.LockManagedOAuthClient(ctx, input.TenantID, input.ClientID); err != nil {
			return err
		}
		if err := tx.UpdateManagedOAuthClient(ctx, input.TenantID, input.ClientID, input.Version, displayName, redirectURIs); err != nil {
			return err
		}
		return writeManagedOAuthClientAudit(ctx, tx, input.Audit, "iam.oauth_client.updated", AuditRiskMedium, input.ClientID, map[string]any{
			"tenant_id": input.TenantID, "redirect_uri_count": len(redirectURIs),
		})
	})
	if err != nil {
		return nil, err
	}
	return s.Get(ctx, input.TenantID, input.ClientID)
}

func (s *OAuthClientManagementService) Disable(ctx context.Context, input ChangeManagedOAuthClientStatusInput) (*ManagedOAuthClient, error) {
	return s.changeStatus(ctx, input, OAuthClientStatusActive, OAuthClientStatusDisabled, "iam.oauth_client.disabled", AuditRiskHigh)
}

func (s *OAuthClientManagementService) Restore(ctx context.Context, input ChangeManagedOAuthClientStatusInput) (*ManagedOAuthClient, error) {
	return s.changeStatus(ctx, input, OAuthClientStatusDisabled, OAuthClientStatusActive, "iam.oauth_client.restored", AuditRiskHigh)
}

func (s *OAuthClientManagementService) changeStatus(ctx context.Context, input ChangeManagedOAuthClientStatusInput, from, to OAuthClientStatus, event string, risk AuditRiskLevel) (*ManagedOAuthClient, error) {
	reason := strings.TrimSpace(input.Reason)
	if input.TenantID <= 0 || input.ActorPrincipalID <= 0 || input.Version <= 0 || !validManagedOAuthClientID(input.ClientID) || reason == "" {
		return nil, commonapi.ErrBadRequest
	}
	err := s.repository.Transaction(ctx, func(tx *Repository) error {
		now, err := tx.CurrentDatabaseTime(ctx)
		if err != nil {
			return err
		}
		current, err := tx.LockManagedOAuthClient(ctx, input.TenantID, input.ClientID)
		if err != nil {
			return err
		}
		if current.Status != from {
			return fmt.Errorf("%w: invalid OAuth client lifecycle transition", commonapi.ErrConflict)
		}
		if err := tx.UpdateManagedOAuthClientStatus(ctx, input.TenantID, input.ClientID, input.Version, from, to); err != nil {
			return err
		}
		details := map[string]any{"tenant_id": input.TenantID, "reason": reason}
		if to == OAuthClientStatusDisabled {
			cancelled, err := tx.CancelPendingAuthorizationRequestsByClient(ctx, input.ClientID, now)
			if err != nil {
				return err
			}
			revoked, err := tx.RevokeActiveTokenFamiliesByClient(ctx, input.ClientID, now, "oauth_client_disabled")
			if err != nil {
				return err
			}
			details["cancelled_authorization_requests"] = cancelled
			details["revoked_token_families"] = revoked
		}
		return writeManagedOAuthClientAudit(ctx, tx, input.Audit, event, risk, input.ClientID, details)
	})
	if err != nil {
		return nil, err
	}
	return s.Get(ctx, input.TenantID, input.ClientID)
}

func validateManagedOAuthClientDefinition(displayName string, redirectURIs []string) (string, []string, error) {
	displayName = strings.TrimSpace(displayName)
	if displayName == "" || len([]rune(displayName)) > maxOAuthClientNameLength || len(redirectURIs) == 0 || len(redirectURIs) > maxOAuthRedirectURIs {
		return "", nil, commonapi.ErrBadRequest
	}
	validated := make([]string, 0, len(redirectURIs))
	seen := make(map[string]struct{}, len(redirectURIs))
	for _, raw := range redirectURIs {
		raw = strings.TrimSpace(raw)
		if err := validateManagedOAuthRedirectURI(raw); err != nil {
			return "", nil, err
		}
		if _, exists := seen[raw]; exists {
			return "", nil, commonapi.ErrBadRequest
		}
		seen[raw] = struct{}{}
		validated = append(validated, raw)
	}
	return displayName, validated, nil
}

func validateManagedOAuthRedirectURI(raw string) error {
	if raw == "" || len(raw) > maxOAuthRedirectURILength || strings.Contains(raw, "*") {
		return commonapi.ErrBadRequest
	}
	parsed, err := url.Parse(raw)
	if err != nil || !parsed.IsAbs() || parsed.Opaque != "" || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" {
		return commonapi.ErrBadRequest
	}
	host := parsed.Hostname()
	if host == "" || strings.EqualFold(host, "localhost") {
		return commonapi.ErrBadRequest
	}
	switch strings.ToLower(parsed.Scheme) {
	case "https":
		return nil
	case "http":
		ip := net.ParseIP(host)
		if ip != nil && ip.IsLoopback() {
			return nil
		}
	}
	return commonapi.ErrBadRequest
}

func validManagedOAuthClientID(clientID string) bool {
	return managedOAuthClientIDPattern.MatchString(clientID)
}

func validOAuthClientStatus(status OAuthClientStatus) bool {
	return status == OAuthClientStatusActive || status == OAuthClientStatusDisabled
}

func generateManagedOAuthClientID() (string, error) {
	randomBytes := make([]byte, 18)
	if _, err := rand.Read(randomBytes); err != nil {
		return "", fmt.Errorf("generate OAuth client ID: %w", err)
	}
	return managedOAuthClientIDPrefix + base64.RawURLEncoding.EncodeToString(randomBytes), nil
}

func writeManagedOAuthClientAudit(ctx context.Context, tx *Repository, metadata AuditMetadata, event string, risk AuditRiskLevel, clientID string, details map[string]any) error {
	return NewAuditWriter(tx).Write(ctx, AuditEvent{
		Metadata: metadata, EventName: event, Result: AuditResultSucceeded, RiskLevel: risk,
		ModuleName: "system", EntityType: "oauth_client", EntityID: clientID, Details: details,
	})
}
