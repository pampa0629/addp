package migration

import (
	"database/sql"
	"encoding/json"
	commonauth "github.com/addp/common/authorization"
	"reflect"
	"slices"
	"testing"

	"github.com/addp/system/internal/iam"
)

// Both engines must expose the same initial OAuth clients and exact platform grants.
func assertInstalledMachineCatalog(t *testing.T, db *sql.DB) {
	t.Helper()
	var catalog machineCatalog
	if err := json.Unmarshal(mysqlIAMMachineCatalogV14, &catalog); err != nil {
		t.Fatal(err)
	}
	for _, data := range [][]byte{mysqlIAMTenantCatalogV15, mysqlIAMAdministratorCatalogV16, mysqlIAMFunctionalRolesV17} {
		var tenantCatalog machineCatalog
		if err := json.Unmarshal(data, &tenantCatalog); err != nil {
			t.Fatal(err)
		}
		catalog.Roles = append(catalog.Roles, tenantCatalog.Roles...)
		catalog.Permissions = append(catalog.Permissions, tenantCatalog.Permissions...)
	}
	assertInstalledPermissionDescriptors(t, db, catalog.Permissions)
	expected := make(map[string]platformMachine)
	for _, s := range catalog.Services {
		expected[s.ID] = s
	}
	rows, err := db.Query(`SELECT c.client_id,c.display_name,c.client_type,c.status,c.token_endpoint_auth_method,
		c.client_secret_hash IS NULL,c.grant_types,c.redirect_uris,c.response_types,c.allowed_scopes,c.allowed_audiences,
		c.owner_scope,c.owner_tenant_id IS NULL,COALESCE(s.name,''),COALESCE(s.description,''),
		COALESCE(s.owner_scope,''),COALESCE(p.status,''),COALESCE(s.created_by_principal_id=s.id,false),
		COALESCE(r.role_key,''),COALESCE(a.grant_reason,''),COALESCE(a.source_type,''),COALESCE(a.scope_type,''),
		a.tenant_id IS NULL AND a.valid_until IS NULL
		FROM system.oauth_clients c LEFT JOIN system.service_principals s ON s.id=c.service_principal_id
		LEFT JOIN system.principals p ON p.id=s.id LEFT JOIN system.role_assignments a ON a.principal_id=s.id AND a.status='active'
		LEFT JOIN system.roles r ON r.id=a.role_id ORDER BY c.client_id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	cli := false
	for rows.Next() {
		var service platformMachine
		var kind, status, method, owner, name, principalOwner, principalStatus, source, scope string
		var noSecret, noTenant, selfCreated, unbounded bool
		var grants, redirects, responses, scopes, audiences iam.DBStringList
		if err := rows.Scan(&service.ID, &service.DisplayName, &kind, &status, &method, &noSecret, &grants, &redirects, &responses, &scopes, &audiences, &owner, &noTenant,
			&name, &service.Description, &principalOwner, &principalStatus, &selfCreated, &service.Role, &service.Reason, &source, &scope, &unbounded); err != nil {
			t.Fatal(err)
		}
		if !noSecret || !noTenant || owner != "platform" || !slices.Equal(scopes, []string{"addp.api"}) || !slices.Equal(audiences, []string{"addp.api"}) {
			t.Errorf("%s has unexpected owner, credential, scopes or audiences", service.ID)
		}
		if service.ID == "addp-cli" {
			cli = true
			if kind != "public" || service.DisplayName != "ADDP CLI" || method != "none" || status != "active" || name != "" ||
				!slices.Equal(redirects, []string{"http://127.0.0.1/callback"}) || !slices.Equal(responses, []string{"code"}) ||
				!slices.Equal(grants, []string{"authorization_code", "refresh_token", "urn:ietf:params:oauth:grant-type:device_code"}) {
				t.Error("unexpected public CLI client")
			}
			continue
		}
		if service != expected[service.ID] || name != service.ID || !selfCreated || principalOwner != "platform" || principalStatus != "active" ||
			kind != "confidential" || method != "client_secret_basic" || status != "disabled" || len(redirects) != 0 || len(responses) != 0 ||
			!slices.Equal(grants, []string{"client_credentials"}) || (service.Role != "" && (source != "bootstrap" || scope != "platform" || !unbounded)) {
			t.Errorf("unexpected platform service identity or assignment for %s", service.ID)
		}
		delete(expected, service.ID)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if !cli || len(expected) != 0 {
		t.Fatalf("incomplete machine catalog: cli=%t missing=%v", cli, expected)
	}
	bindings, err := db.Query(`SELECT r.role_key,p.permission_key,b.source_type FROM system.roles r JOIN system.role_permissions b ON b.role_id=r.id
		JOIN system.permissions p ON p.id=b.permission_id WHERE r.status='active' AND p.status='active' ORDER BY r.role_key,p.permission_key`)
	if err != nil {
		t.Fatal(err)
	}
	defer bindings.Close()
	actual := make(map[string][]string)
	for bindings.Next() {
		var role, key, source string
		if err := bindings.Scan(&role, &key, &source); err != nil {
			t.Fatal(err)
		}
		if source == "product" {
			actual[role] = append(actual[role], key)
		}
	}
	if err := bindings.Err(); err != nil {
		t.Fatal(err)
	}
	for _, r := range catalog.Roles {
		if r.Key == "tenant.transfer_runtime" {
			r.Permissions = append(r.Permissions, "system.transfer_field_connection.read")
			slices.Sort(r.Permissions)
		}
		if r.Key == "tenant.spark_runtime" {
			r.Permissions = []string{"system.engine_catalog.read", "system.execution_authorization.execute"}
		}
		if r.Key == "tenant.quality_runtime" {
			// The immutable v15 installation snapshot precedes the descriptor-only release.
			r.Permissions = []string{"meta.catalog.read", "standard.domain.read", "standard.element.read", "system.cleanup_authorization.execute", "system.engine_catalog.read", "system.engine_descriptor.read", "system.execution_authorization.execute"}
		}
		slices.Sort(actual[r.Key])
		if !slices.Equal(actual[r.Key], r.Permissions) {
			t.Errorf("%s permissions=%v, want %v", r.Key, actual[r.Key], r.Permissions)
		}
	}
}

func assertInstalledPermissionDescriptors(t *testing.T, db *sql.DB, descriptors []commonauth.PermissionDescriptor) {
	t.Helper()
	expected := make(map[string]commonauth.PermissionDescriptor, len(descriptors))
	for _, p := range descriptors {
		expected[p.Key] = p
	}
	rows, err := db.Query(`SELECT permission_key,owner_module,action,risk_level,delegable,allowed_scope_types,tenant_customizable,name_i18n_key,description_i18n_key,status FROM system.permissions`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var p commonauth.PermissionDescriptor
		var scopes iam.DBStringList
		if err := rows.Scan(&p.Key, &p.OwnerModule, &p.Action, &p.RiskLevel, &p.Delegable, &scopes, &p.TenantCustomizable, &p.NameI18nKey, &p.DescriptionI18nKey, &p.Status); err != nil {
			t.Fatal(err)
		}
		p.AllowedScopeTypes = []string(scopes)
		if want, ok := expected[p.Key]; ok {
			if !reflect.DeepEqual(p, want) {
				t.Errorf("different permission %s: database=%+v release=%+v", p.Key, p, want)
			}
			delete(expected, p.Key)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	for key := range expected {
		t.Errorf("missing permission %s", key)
	}
}
