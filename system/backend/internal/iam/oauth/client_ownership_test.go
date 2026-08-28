package oauth

import (
	"testing"

	commonapi "github.com/addp/common/api"
	commonauth "github.com/addp/common/authorization"
	"github.com/addp/common/authorization/authtest"
)

func TestValidateOAuthClientAuthorizationContext(t *testing.T) {
	tenantContext := authtest.NewTenantUserAuthContext("42", "9", []string{"service.data_read.execute"})
	otherTenantContext := authtest.NewTenantUserAuthContext("43", "9", []string{"service.data_read.execute"})
	tenantID := int64(42)

	if err := validateOAuthClientAuthorizationContext(oauthClientRow{OwnerScope: "tenant", OwnerTenantID: &tenantID}, tenantContext); err != nil {
		t.Fatalf("matching tenant context rejected: %v", err)
	}
	if err := validateOAuthClientAuthorizationContext(oauthClientRow{OwnerScope: "tenant", OwnerTenantID: &tenantID}, otherTenantContext); err != commonapi.ErrForbidden {
		t.Fatalf("other tenant context error = %v, want forbidden", err)
	}
	if err := validateOAuthClientAuthorizationContext(oauthClientRow{OwnerScope: "platform"}, tenantContext); err != nil {
		t.Fatalf("platform-owned client rejected: %v", err)
	}

	invalid := tenantContext
	invalid.Principal.Type = "service_principal"
	if err := validateOAuthClientAuthorizationContext(oauthClientRow{OwnerScope: "tenant", OwnerTenantID: &tenantID}, invalid); err != commonapi.ErrUnauthorized {
		t.Fatalf("service principal context error = %v, want unauthorized", err)
	}

	platform := tenantContext
	platform.Context = commonauth.AuthSessionContext{Type: "platform"}
	if err := validateOAuthClientAuthorizationContext(oauthClientRow{OwnerScope: "tenant", OwnerTenantID: &tenantID}, platform); err == nil {
		t.Fatal("platform context was accepted for tenant-owned client")
	}
}
