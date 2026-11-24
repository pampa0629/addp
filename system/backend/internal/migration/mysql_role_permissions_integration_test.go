package migration

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/addp/system/internal/iam"
	"gorm.io/gorm"
)

func testMySQLRolePermissionProjection(t *testing.T, db *gorm.DB) {
	ctx := context.Background()
	exec := func(sql string) {
		t.Helper()
		if err := db.Exec(sql).Error; err != nil {
			t.Fatal(err)
		}
	}
	exec("INSERT INTO tenants (id,code,name) VALUES (500,'role-projection','Role projection')")
	exec("INSERT INTO tenant_memberships (id,tenant_id,principal_id,source_type,joined_at) VALUES (500,500,100,'manual',CURRENT_TIMESTAMP(6))")
	exec(`INSERT INTO roles (id,role_key,role_type,name_i18n_key,description_i18n_key,immutable,allowed_scope_types,allowed_principal_types)
		  VALUES (500,'tenant.projection','tenant_builtin','name','description',1,JSON_ARRAY('tenant'),JSON_ARRAY('user'))`)
	exec(`INSERT INTO role_assignments (principal_id,role_id,scope_type,tenant_id,source_type) VALUES (100,500,'tenant',500,'bootstrap')`)
	exec(`INSERT INTO permissions (id,permission_key,owner_module,action,risk_level,allowed_scope_types,name_i18n_key,description_i18n_key)
		  VALUES (500,'sample.projection.read','sample','read','low',JSON_ARRAY('tenant'),'name','description')`)
	repo := iam.NewRepository(db)
	t.Run("tenant custom role permission scopes", func(t *testing.T) {
		tx := db.Begin()
		if tx.Error != nil {
			t.Fatal(tx.Error)
		}
		defer tx.Rollback()
		if err := tx.Exec("UPDATE permissions SET tenant_customizable=true WHERE id=500").Error; err != nil {
			t.Fatal(err)
		}
		repo := iam.NewRepository(tx)
		available, err := repo.ListTenantAssignablePermissions(ctx)
		if err != nil {
			t.Fatal(err)
		}
		found := slices.ContainsFunc(available, func(permission iam.TenantAssignablePermission) bool {
			return permission.PermissionKey == "sample.projection.read" && slices.Equal(permission.AllowedScopeTypes, []string{"tenant"})
		})
		if !found {
			t.Fatal("missing tenant-customizable permission or its tenant scope")
		}
		roleService := iam.NewTenantRoleService(repo, nil)
		keys := []string{"sample.projection.read"}
		created, err := roleService.CreateRole(ctx, iam.CreateTenantRoleInput{
			TenantID: 500, ActorPrincipalID: 100, RoleKey: "isolation.role", Name: "Before", PermissionKeys: keys,
		})
		if err != nil {
			t.Fatal(err)
		}
		edited, err := roleService.UpdateRole(ctx, iam.UpdateTenantRoleInput{
			TenantID: 500, RoleID: created.ID, ActorPrincipalID: 100, Name: "After", PermissionKeys: keys,
		})
		if err != nil || edited.Name == nil || *edited.Name != "After" || !slices.Equal(edited.AllowedScopeTypes, []string{"tenant"}) {
			t.Fatalf("custom role update: %v", err)
		}

	})

	version := func() int64 {
		t.Helper()
		principal, err := repo.GetPrincipal(ctx, 100)
		if err != nil {
			t.Fatal(err)
		}
		return principal.AuthorizationVersion
	}
	initial := version()
	exec("INSERT INTO role_permissions (role_id,permission_id,source_type) VALUES (500,500,'product')")
	if version() != initial+1 {
		t.Fatal("binding must invalidate the holder's authorization version")
	}
	at, err := repo.CurrentDatabaseTime(ctx)
	if err != nil {
		t.Fatal(err)
	}
	tenant, member := int64(500), int64(500)
	var otherMembership int64
	if err := db.Raw("SELECT id FROM tenant_memberships WHERE tenant_id=90 AND principal_id=100").Row().Scan(&otherMembership); err != nil {
		t.Fatal(err)
	}
	otherTenant := int64(90)
	other, err := repo.ListEffectiveRoleAssignmentPermissions(ctx, 100, iam.PrincipalTypeUser, iam.ContextTypeTenant, &otherTenant, &otherMembership, at)
	if err != nil || len(other) == 0 {
		t.Fatalf("other tenant administrator has no permissions: %v", err)
	}
	for _, r := range other {
		if r.RoleKey != "tenant.administrator" || r.PermissionKey == "sample.projection.read" {
			t.Fatalf("unexpected permission in other tenant: %+v", r)
		}
	}
	for _, tc := range []struct {
		name          string
		principal     int64
		principalType iam.PrincipalType
		tenant        int64
		at            time.Time
		want          int
	}{
		{"assigned user", 100, iam.PrincipalTypeUser, tenant, at, 1},
		{"other user", 103, iam.PrincipalTypeUser, tenant, at, 0},
		{"ineligible principal type", 100, iam.PrincipalTypeServicePrincipal, tenant, at, 0},
		{"before assignment", 100, iam.PrincipalTypeUser, tenant, at.Add(-time.Hour), 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := repo.ListEffectiveRoleAssignmentPermissions(ctx, tc.principal, tc.principalType, iam.ContextTypeTenant, &tc.tenant, &member, tc.at)
			if err != nil || len(got) != tc.want {
				t.Fatalf("effective permissions count=%d want=%d error=%v", len(got), tc.want, err)
			}
			if tc.want == 1 && (got[0].PermissionKey != "sample.projection.read" || got[0].RoleKey != "tenant.projection") {
				t.Fatalf("wrong permission or role: %+v", got)
			}
		})
	}
	exec(`INSERT INTO roles (id,tenant_id,role_key,role_type,name,immutable,allowed_scope_types,allowed_principal_types,created_by_principal_id)
		  VALUES (501,500,'tenant.custom_projection','tenant_custom','Custom projection',0,JSON_ARRAY('tenant'),JSON_ARRAY('user'),100)`)
	for _, invalid := range []string{
		"UPDATE roles SET allowed_principal_types=JSON_ARRAY('service_principal') WHERE id=500",
		"INSERT INTO role_permissions (role_id,permission_id,source_type) VALUES (501,500,'product')",
		"INSERT INTO role_permissions (role_id,permission_id,source_type,created_by_principal_id) VALUES (501,500,'tenant',100)",
		"UPDATE permissions SET allowed_scope_types=JSON_ARRAY('platform') WHERE id=500",
		"UPDATE role_permissions SET source_type='tenant', created_by_principal_id=100 WHERE role_id=500",
		"DELETE FROM permissions WHERE id=500",
	} {
		if db.Exec(invalid).Error == nil {
			t.Fatalf("accepted invalid catalog change: %s", invalid)
		}
	}
	exec("UPDATE permissions SET status='disabled' WHERE id=500")
	got, err := repo.ListEffectiveRoleAssignmentPermissions(ctx, 100, iam.PrincipalTypeUser, iam.ContextTypeTenant, &tenant, &member, at)
	if err != nil || len(got) != 0 || version() != initial+2 {
		t.Fatalf("disabled permission: count=%d error=%v", len(got), err)
	}
	exec("DELETE FROM role_permissions WHERE role_id=500 AND permission_id=500")
	if version() != initial+3 {
		t.Fatal("removing a binding must invalidate the holder's authorization version")
	}
	exec("UPDATE permissions SET status='active',tenant_customizable=1 WHERE id=500")
	if err := db.Exec("INSERT INTO role_permissions (role_id,permission_id,source_type,created_by_principal_id) VALUES (501,500,'tenant',103)").Error; err == nil {
		t.Fatal("accepted a nonmember as creator of a tenant role binding")
	}
	exec("INSERT INTO role_permissions (role_id,permission_id,source_type,created_by_principal_id) VALUES (501,500,'tenant',100)")

}
