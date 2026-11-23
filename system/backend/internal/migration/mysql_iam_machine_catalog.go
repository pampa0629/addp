package migration

import (
	_ "embed"
	"encoding/json"
	"fmt"

	commonauth "github.com/addp/common/authorization"
	"gorm.io/gorm"
)

// Versioned installation data for the platform clients, independent of tenant setup.
//
//go:embed mysql_iam_v14.json
var mysqlIAMMachineCatalogV14 []byte

//go:embed mysql_iam_v15.json
var mysqlIAMTenantCatalogV15 []byte

//go:embed mysql_iam_v16.json
var mysqlIAMAdministratorCatalogV16 []byte

//go:embed mysql_iam_v17.json
var mysqlIAMFunctionalRolesV17 []byte

type platformMachine struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
	Description string `json:"description"`
	Role        string `json:"role"`
	Reason      string `json:"reason"`
}

type machineCatalog struct {
	Permissions []commonauth.PermissionDescriptor
	Roles       []commonauth.BuiltinRoleDescriptor
	Services    []platformMachine
}

var platformAdministratorRoles = []string{"platform.system_administrator", "platform.security_administrator", "platform.audit_administrator"}

func installMySQLIAMCatalog(tx *gorm.DB, from int64) error {
	if from < 14 {
		if err := installMySQLCatalogRevision(tx, mysqlIAMMachineCatalogV14); err != nil {
			return err
		}
		if err := tx.Exec(`INSERT INTO oauth_clients (client_id,display_name,client_type,redirect_uris,grant_types,response_types,
		allowed_scopes,allowed_audiences,token_endpoint_auth_method)
		VALUES ('addp-cli','ADDP CLI','public',JSON_ARRAY('http://127.0.0.1/callback'),
		JSON_ARRAY('authorization_code','refresh_token','urn:ietf:params:oauth:grant-type:device_code'),
		JSON_ARRAY('code'),JSON_ARRAY('addp.api'),JSON_ARRAY('addp.api'),'none')`).Error; err != nil {
			return err
		}
	}
	if from < 15 {
		if err := installMySQLCatalogRevision(tx, mysqlIAMTenantCatalogV15); err != nil {
			return err
		}
	}
	if from < 16 {
		if err := installMySQLCatalogRevision(tx, mysqlIAMAdministratorCatalogV16); err != nil {
			return err
		}
		if err := tx.Exec(`INSERT INTO role_conflicts (role_id_low,role_id_high,reason) SELECT a.id,b.id,'platform_three_administrators_separation_of_duties' FROM roles a JOIN roles b ON a.id<b.id WHERE a.role_key IN ? AND b.role_key IN ?`, platformAdministratorRoles, platformAdministratorRoles).Error; err != nil {
			return err
		}
	}
	if from < 17 {
		if err := installMySQLCatalogRevision(tx, mysqlIAMFunctionalRolesV17); err != nil {
			return err
		}
	}
	if from < 18 {
		if err := tx.Exec(`INSERT INTO iam_security_policy (
			id,version,applied_version,access_token_ttl_minutes,delegated_access_token_ttl_minutes,resource_access_ticket_ttl_minutes,
			refresh_token_ttl_days,oauth_authorization_code_ttl_minutes,oauth_device_code_ttl_minutes,oauth_device_poll_interval_seconds,
			tenant_invitation_ttl_hours,oauth_public_rate_limit_per_minute,oauth_user_rate_limit_per_minute)
			VALUES (1,1,0,15,2,15,30,5,10,5,168,60,30)`).Error; err != nil {
			return err
		}
	}
	if from < 19 {
		if err := tx.Exec(`DELETE FROM role_permissions
			WHERE role_id = (SELECT id FROM roles WHERE tenant_id IS NULL AND role_key = 'tenant.quality_runtime')
			AND permission_id = (SELECT id FROM permissions WHERE permission_key = 'system.engine.read')
			AND source_type = 'product'`).Error; err != nil {
			return err
		}
		if err := tx.Exec(`INSERT INTO role_permissions (role_id,permission_id,source_type)
			SELECT r.id,p.id,'product' FROM roles r JOIN permissions p ON p.permission_key='system.engine_descriptor.read'
			WHERE r.tenant_id IS NULL AND r.role_key='tenant.quality_runtime'`).Error; err != nil {
			return err
		}
	}
	if from < 20 {
		return tx.Exec(`DELETE FROM role_permissions
			WHERE role_id = (SELECT id FROM roles WHERE tenant_id IS NULL AND role_key='tenant.spark_runtime')
			  AND permission_id = (SELECT id FROM permissions WHERE permission_key='system.engine.read')
			  AND source_type='product'`).Error
	}
	return nil
}

func installMySQLCatalogRevision(tx *gorm.DB, release []byte) error {
	var catalog machineCatalog
	if err := json.Unmarshal(release, &catalog); err != nil {
		return err
	}
	insertID := func(query string, args ...any) (int64, error) {
		result, err := tx.Statement.ConnPool.ExecContext(tx.Statement.Context, query, args...)
		if err != nil {
			return 0, err
		}
		return result.LastInsertId()
	}

	for _, p := range catalog.Permissions {
		scopes, err := json.Marshal(p.AllowedScopeTypes)
		if err != nil {
			return err
		}
		err = tx.Exec(`INSERT INTO permissions (permission_key, owner_module, action, risk_level, allowed_scope_types,
			tenant_customizable, delegable, status, name_i18n_key, description_i18n_key) VALUES (?,?,?,?,?,?,?,?,?,?)`,
			p.Key, p.OwnerModule, p.Action, p.RiskLevel, string(scopes), p.TenantCustomizable, p.Delegable, p.Status, p.NameI18nKey, p.DescriptionI18nKey).Error
		if err != nil {
			return fmt.Errorf("install permission %s: %w", p.Key, err)
		}
	}
	var installed []struct {
		ID            int64
		PermissionKey string
	}
	if err := tx.Raw("SELECT id,permission_key FROM permissions").Scan(&installed).Error; err != nil {
		return err
	}
	permissions := make(map[string]int64, len(installed))
	for _, p := range installed {
		permissions[p.PermissionKey] = p.ID
	}
	roles := make(map[string]int64, len(catalog.Roles))
	for _, r := range catalog.Roles {
		scopeTypes, err := json.Marshal(r.AllowedScopeTypes)
		if err != nil {
			return err
		}
		principalTypes, err := json.Marshal(r.AllowedPrincipalTypes)
		if err != nil {
			return err
		}
		id, err := insertID(`INSERT INTO roles (role_key, role_type, allowed_scope_types, allowed_principal_types,
			immutable, name_i18n_key, description_i18n_key) VALUES (?, ?, ?, ?, true, ?, ?)`, r.Key, r.RoleType, string(scopeTypes), string(principalTypes), r.NameI18nKey, r.DescriptionI18nKey)
		if err != nil {
			return fmt.Errorf("install role %s: %w", r.Key, err)
		}
		roles[r.Key] = id
		for _, key := range r.Permissions {
			if err := tx.Exec("INSERT INTO role_permissions (role_id,permission_id,source_type) VALUES (?,?,'product')", id, permissions[key]).Error; err != nil {
				return fmt.Errorf("bind %s to %s: %w", r.Key, key, err)
			}
		}
	}
	for _, service := range catalog.Services {
		id, err := insertID("INSERT INTO principals (principal_type) VALUES ('service_principal')")
		if err != nil {
			return err
		}
		if err := tx.Exec(`INSERT INTO service_principals (id,name,description,owner_scope,created_by_principal_id)
			VALUES (?,?,?,'platform',?)`, id, service.ID, service.Description, id).Error; err != nil {
			return fmt.Errorf("install service principal %s: %w", service.ID, err)
		}
		if err := tx.Exec(`INSERT INTO oauth_clients (client_id,display_name,client_type,redirect_uris,grant_types,response_types,
			allowed_scopes,allowed_audiences,token_endpoint_auth_method,status,service_principal_id)
			VALUES (?,?,'confidential',JSON_ARRAY(),JSON_ARRAY('client_credentials'),JSON_ARRAY(),JSON_ARRAY('addp.api'),
			JSON_ARRAY('addp.api'),'client_secret_basic','disabled',?)`, service.ID, service.DisplayName, id).Error; err != nil {
			return fmt.Errorf("install service client %s: %w", service.ID, err)
		}
		if service.Role != "" {
			if err := tx.Exec(`INSERT INTO role_assignments (principal_id,role_id,scope_type,source_type,grant_reason)
				VALUES (?,?,'platform','bootstrap',?)`, id, roles[service.Role], service.Reason).Error; err != nil {
				return fmt.Errorf("assign %s to %s: %w", service.Role, service.ID, err)
			}
		}
	}
	return nil
}
