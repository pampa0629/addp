package service

import (
	"errors"

	"github.com/addp/system/internal/models"
)

var (
	ErrInvalidAuthorizationIdentity = errors.New("invalid authorization identity")
	ErrInactiveAuthorizationUser    = errors.New("authorization user is inactive")
	ErrInactiveAuthorizationTenant  = errors.New("authorization tenant is inactive")
)

type authContextUserRepository interface {
	GetByID(id uint) (*models.User, error)
}

type authContextTenantRepository interface {
	GetByID(id uint) (*models.Tenant, error)
}

type AuthContextService struct {
	userRepo   authContextUserRepository
	tenantRepo authContextTenantRepository
}

func NewAuthContextService(
	userRepo authContextUserRepository,
	tenantRepo authContextTenantRepository,
) *AuthContextService {
	return &AuthContextService{userRepo: userRepo, tenantRepo: tenantRepo}
}

func (s *AuthContextService) ValidateIdentity(userID, tokenTenantID uint) (*models.User, error) {
	if userID == 0 {
		return nil, ErrInvalidAuthorizationIdentity
	}
	user, err := s.userRepo.GetByID(userID)
	if err != nil {
		return nil, ErrInvalidAuthorizationIdentity
	}
	if !user.IsActive {
		return nil, ErrInactiveAuthorizationUser
	}

	if user.UserType == models.UserTypeSuperAdmin {
		if user.TenantID != nil || tokenTenantID != 0 {
			return nil, ErrInvalidAuthorizationIdentity
		}
		return user, nil
	}

	if user.TenantID == nil || *user.TenantID == 0 || tokenTenantID != *user.TenantID {
		return nil, ErrInvalidAuthorizationIdentity
	}
	tenant, err := s.tenantRepo.GetByID(*user.TenantID)
	if err != nil {
		return nil, ErrInvalidAuthorizationIdentity
	}
	if !tenant.IsActive {
		return nil, ErrInactiveAuthorizationTenant
	}
	return user, nil
}

func (s *AuthContextService) ResolveAccessToken(token *models.AccessToken) (*models.AuthorizationContext, error) {
	if token == nil || token.ExpiresAt.IsZero() || token.CreatedAt.IsZero() {
		return nil, ErrInvalidAuthorizationIdentity
	}
	var tenantID uint
	if token.TenantID != nil {
		tenantID = *token.TenantID
	}
	user, err := s.ValidateIdentity(token.UserID, tenantID)
	if err != nil {
		return nil, err
	}

	return &models.AuthorizationContext{
		SubjectType: models.SubjectTypeUser,
		UserID:      user.ID,
		Username:    user.Username,
		UserType:    user.UserType,
		TenantID:    user.TenantID,
		AuthType:    token.AuthType,
		ClientID:    token.ClientID,
		Audiences:   []string(token.Audiences),
		Scopes:      []string(token.Scopes),
		IssuedAt:    token.CreatedAt,
		ExpiresAt:   token.ExpiresAt,
	}, nil
}

func (s *AuthContextService) ResolveDelegatedAccessToken(token *models.DelegatedAccessToken) (*models.AuthorizationContext, error) {
	if token == nil || token.ExpiresAt.IsZero() || token.CreatedAt.IsZero() {
		return nil, ErrInvalidAuthorizationIdentity
	}
	var tenantID uint
	if token.TenantID != nil {
		tenantID = *token.TenantID
	}
	user, err := s.ValidateIdentity(token.UserID, tenantID)
	if err != nil {
		return nil, err
	}
	delegatedBy := token.DelegatedBy
	agentRunID := token.AgentRunID
	toolCallID := token.ToolCallID
	return &models.AuthorizationContext{
		SubjectType: models.SubjectTypeUser,
		UserID:      user.ID,
		Username:    user.Username,
		UserType:    user.UserType,
		TenantID:    user.TenantID,
		AuthType:    models.AuthTypeDelegatedAccessToken,
		ClientID:    token.ClientID,
		Audiences:   []string{token.Audience},
		Scopes:      []string(token.Scopes),
		DelegatedBy: &delegatedBy,
		AgentRunID:  &agentRunID,
		ToolCallID:  &toolCallID,
		IssuedAt:    token.CreatedAt,
		ExpiresAt:   token.ExpiresAt,
	}, nil
}
