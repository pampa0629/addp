package service

import (
	"errors"
	"testing"
	"time"

	"github.com/addp/system/internal/models"
)

type authUserRepoStub struct {
	user *models.User
	err  error
}

func (r authUserRepoStub) GetByID(uint) (*models.User, error) { return r.user, r.err }

type authTenantRepoStub struct {
	tenant *models.Tenant
	err    error
}

func (r authTenantRepoStub) GetByID(uint) (*models.Tenant, error) { return r.tenant, r.err }

func authAccessToken(userID, tenantID uint) *models.AccessToken {
	now := time.Now().UTC()
	return &models.AccessToken{
		UserID: userID, TenantID: &tenantID,
		AuthType:  models.AuthTypeFirstPartyAccessToken,
		Audiences: []string{}, Scopes: []string{},
		CreatedAt: now, ExpiresAt: now.Add(15 * time.Minute),
	}
}

func TestAuthContextUsesCurrentUserAndTenantFacts(t *testing.T) {
	tenantID := uint(3)
	service := NewAuthContextService(
		authUserRepoStub{user: &models.User{
			ID: 12, Username: "alice", UserType: models.UserTypeTenantAdmin,
			TenantID: &tenantID, IsActive: true,
		}},
		authTenantRepoStub{tenant: &models.Tenant{ID: tenantID, IsActive: true}},
	)

	context, err := service.ResolveAccessToken(authAccessToken(12, tenantID))
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if context.Username != "alice" || context.UserType != models.UserTypeTenantAdmin {
		t.Fatalf("context user facts = %#v", context)
	}
	if context.TenantID == nil || *context.TenantID != tenantID {
		t.Fatalf("context tenant_id = %#v", context.TenantID)
	}
	if context.AuthType != models.AuthTypeFirstPartyAccessToken {
		t.Fatalf("context auth_type = %q", context.AuthType)
	}
	if context.Scopes == nil || context.Audiences == nil {
		t.Fatal("scopes and audiences must be explicit empty arrays")
	}
}

func TestAuthContextRejectsTenantMismatch(t *testing.T) {
	tenantID := uint(3)
	service := NewAuthContextService(
		authUserRepoStub{user: &models.User{
			ID: 12, Username: "alice", UserType: models.UserTypeUser,
			TenantID: &tenantID, IsActive: true,
		}},
		authTenantRepoStub{tenant: &models.Tenant{ID: tenantID, IsActive: true}},
	)

	_, err := service.ResolveAccessToken(authAccessToken(12, 9))
	if !errors.Is(err, ErrInvalidAuthorizationIdentity) {
		t.Fatalf("Resolve() error = %v, want invalid identity", err)
	}
}

func TestAuthContextRejectsInactiveUserOrTenant(t *testing.T) {
	tenantID := uint(3)
	tests := []struct {
		name   string
		user   *models.User
		tenant *models.Tenant
		want   error
	}{
		{
			name:   "inactive user",
			user:   &models.User{ID: 12, UserType: models.UserTypeUser, TenantID: &tenantID},
			tenant: &models.Tenant{ID: tenantID, IsActive: true},
			want:   ErrInactiveAuthorizationUser,
		},
		{
			name:   "inactive tenant",
			user:   &models.User{ID: 12, UserType: models.UserTypeUser, TenantID: &tenantID, IsActive: true},
			tenant: &models.Tenant{ID: tenantID},
			want:   ErrInactiveAuthorizationTenant,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := NewAuthContextService(
				authUserRepoStub{user: tt.user},
				authTenantRepoStub{tenant: tt.tenant},
			)
			_, err := service.ResolveAccessToken(authAccessToken(12, tenantID))
			if !errors.Is(err, tt.want) {
				t.Fatalf("Resolve() error = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestAuthContextKeepsSuperAdminTenantless(t *testing.T) {
	service := NewAuthContextService(
		authUserRepoStub{user: &models.User{
			ID: 1, Username: "SuperAdmin", UserType: models.UserTypeSuperAdmin, IsActive: true,
		}},
		authTenantRepoStub{},
	)

	token := authAccessToken(1, 0)
	token.TenantID = nil
	context, err := service.ResolveAccessToken(token)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if context.TenantID != nil {
		t.Fatalf("super admin tenant_id = %#v, want nil", context.TenantID)
	}
}
