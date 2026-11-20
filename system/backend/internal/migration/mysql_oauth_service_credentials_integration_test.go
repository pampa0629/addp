package migration

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	iamoauth "github.com/addp/system/internal/iam/oauth"
	"github.com/ory/fosite"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

func testMySQLOAuthServiceCredential(t *testing.T, db *gorm.DB) {
	t.Helper()
	exec := func(query string, args ...any) {
		t.Helper()
		if err := db.Exec(query, args...).Error; err != nil {
			t.Fatal(err)
		}
	}
	exec("INSERT INTO principals (id, principal_type) VALUES (104, 'service_principal'), (105, 'service_principal')")
	exec(`INSERT INTO service_principals (id, name, owner_scope, created_by_principal_id)
		VALUES (104, 'fixture-platform-service', 'platform', 100)`)
	for _, invalid := range []string{
		`INSERT INTO service_principals (id, name, owner_scope, created_by_principal_id)
		 VALUES (100, 'invalid-subtype', 'platform', 100)`,
		`INSERT INTO service_principals (id, name, owner_scope, owner_tenant_id, created_by_principal_id)
		 VALUES (105, 'tenant-service', 'tenant', 90, 100)`,
	} {
		if err := db.Exec(invalid).Error; err == nil {
			t.Fatal("schema accepted an invalid service principal")
		}
	}
	exec("INSERT INTO tenant_memberships (tenant_id, principal_id, source_type, joined_at) VALUES (90, 105, 'manual', NOW())")
	exec(`INSERT INTO service_principals (id, name, owner_scope, owner_tenant_id, created_by_principal_id)
		VALUES (105, 'tenant-service', 'tenant', 90, 100)`)
	if err := db.Exec("INSERT INTO tenant_memberships (tenant_id, principal_id, source_type, joined_at) VALUES (10, 105, 'manual', NOW())").Error; err == nil {
		t.Fatal("accepted a membership outside the service principal's owner tenant")
	}

	const secret = "fixture-only-mysql-service-client-secret"
	hash, err := bcrypt.GenerateFromPassword([]byte(secret), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	const insert = `INSERT INTO oauth_clients (
		client_id, display_name, client_type, client_secret_hash, redirect_uris, grant_types, response_types,
		allowed_scopes, allowed_audiences, token_endpoint_auth_method, service_principal_id,
		owner_scope, owner_tenant_id, created_by_principal_id
	) VALUES (?, 'Service', 'confidential', ?, JSON_ARRAY(), JSON_ARRAY('client_credentials'), JSON_ARRAY(),
	          JSON_ARRAY('addp.api'), JSON_ARRAY('addp.api'), 'client_secret_basic', ?, ?, ?, 100)`
	if err := db.Exec(insert, "addp_svc_wrongtenant00001", string(hash), 104, "tenant", 90).Error; err == nil {
		t.Fatal("accepted a client owned by a different tenant than its principal")
	}
	exec(insert, "fixture-platform-service", string(hash), 104, "platform", nil)
	exec(insert, "addp_svc_correcttenant001", string(hash), 105, "tenant", 90)
	for _, invalid := range []string{
		"UPDATE oauth_clients SET allowed_scopes=JSON_ARRAY('addp.api', 'other') WHERE client_id='fixture-platform-service'",
		"UPDATE oauth_clients SET grant_types=JSON_ARRAY('authorization_code') WHERE client_id='fixture-platform-service'",
		"UPDATE oauth_clients SET service_principal_id=NULL WHERE client_id='fixture-platform-service'",
		"DELETE FROM oauth_clients WHERE client_id='fixture-platform-service'",
	} {
		if err := db.Exec(invalid).Error; err == nil {
			t.Fatalf("accepted invalid service client change: %s", invalid)
		}
	}

	storage, err := iamoauth.NewStorage(db, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	client, err := storage.GetClient(ctx, "fixture-platform-service")
	if err != nil {
		t.Fatal(err)
	}
	if client.IsPublic() || len(client.GetRedirectURIs()) != 0 ||
		!slices.Equal(client.GetGrantTypes(), []string{"client_credentials"}) ||
		!slices.Equal(client.GetScopes(), []string{"addp.api"}) ||
		!slices.Equal(client.GetAudience(), []string{"addp.api"}) ||
		bcrypt.CompareHashAndPassword(client.GetHashedSecret(), []byte(secret)) != nil {
		t.Fatal("OAuth storage did not return the service client and its exact credential contract")
	}
	exec("UPDATE oauth_clients SET status='disabled' WHERE client_id='fixture-platform-service'")
	if _, err := storage.GetClient(ctx, "fixture-platform-service"); !errors.Is(err, fosite.ErrNotFound) {
		t.Fatalf("disabled client lookup returned %v", err)
	}
}
