package migration

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"testing"

	commonauth "github.com/addp/common/authorization"
	"github.com/addp/system/internal/iam"
	"gorm.io/gorm"
)

func prepareMySQLBrowserOrganization(t *testing.T, db *gorm.DB, member iam.TenantMembership) {
	t.Helper()
	exec := func(query string, args ...any) {
		t.Helper()
		if err := db.Exec(query, args...).Error; err != nil {
			t.Fatal(err)
		}
	}
	exec("INSERT INTO departments (id,tenant_id,code,name) VALUES (601,?,'root','Root')", member.TenantID)
	exec("INSERT INTO departments (id,tenant_id,parent_id,code,name) VALUES (602,?,601,'middle','Middle')", member.TenantID)
	exec("INSERT INTO departments (id,tenant_id,parent_id,code,name) VALUES (603,?,602,'leaf','Leaf')", member.TenantID)
	if err := db.Exec("UPDATE departments SET parent_id=603 WHERE id=601").Error; err == nil {
		t.Fatal("accepted a department cycle")
	}
	if err := db.Exec("INSERT INTO departments (id,tenant_id,parent_id,code,name) VALUES (604,?,604,'self','Self')", member.TenantID).Error; err == nil {
		t.Fatal("accepted a department as its own parent")
	}
	exec("INSERT INTO project_groups (id,tenant_id,code,name) VALUES (601,?,'team','Team')", member.TenantID)
	exec("INSERT INTO department_memberships (tenant_id,department_id,tenant_membership_id,membership_type) VALUES (?,603,?,'primary')", member.TenantID, member.ID)
	if err := db.Exec("INSERT INTO department_memberships (tenant_id,department_id,tenant_membership_id,membership_type) VALUES (?,601,?,'primary')", member.TenantID, member.ID).Error; err == nil {
		t.Fatal("accepted two active primary departments")
	}
	exec("INSERT INTO department_memberships (tenant_id,department_id,tenant_membership_id,membership_type) VALUES (?,601,?,'additional')", member.TenantID, member.ID)
	exec("INSERT INTO project_group_memberships (tenant_id,project_group_id,tenant_membership_id,relation_role) VALUES (?,601,?,'coordinator')", member.TenantID, member.ID)
	exec(`INSERT INTO roles (id,role_key,role_type,name_i18n_key,description_i18n_key,immutable,allowed_scope_types,allowed_principal_types)
		  VALUES (601,'tenant.browser_reader','tenant_builtin','name','description',1,JSON_ARRAY('tenant'),JSON_ARRAY('user'))`)
	exec(`INSERT INTO permissions (id,permission_key,owner_module,action,risk_level,allowed_scope_types,name_i18n_key,description_i18n_key)
		  VALUES (601,'sample.browser.read','sample','read','low',JSON_ARRAY('tenant'),'name','description')`)
	exec("INSERT INTO role_permissions (role_id,permission_id,source_type) VALUES (601,601,'product')")
	exec("INSERT INTO role_assignments (principal_id,role_id,scope_type,tenant_id,source_type) VALUES (?,601,'tenant',?,'bootstrap')", member.PrincipalID, member.TenantID)

}

func verifyMySQLBrowserOrganization(t *testing.T, db *gorm.DB, member iam.TenantMembership, token string) {
	t.Helper()
	ctx := context.Background()
	resolver, err := iam.NewAuthContextService(iam.NewRepository(db))
	if err != nil {
		t.Fatal(err)
	}
	identity, err := resolver.ResolveFirstPartyAccessToken(ctx, token)
	if err != nil {
		t.Fatal(err)
	}
	depts, groups := identity.Organization.Departments, identity.Organization.ProjectGroups
	if len(identity.Authorization.RoleAssignments) != 1 || !commonauth.HasContextPermissions(*identity, "sample.browser.read") ||
		identity.Principal.ID != strconv.FormatInt(member.PrincipalID, 10) || identity.Context.TenantID == nil ||
		*identity.Context.TenantID != strconv.FormatInt(member.TenantID, 10) || len(depts) != 2 ||
		depts[0].DepartmentID != "601" || len(depts[0].AncestorIDs) != 0 ||
		depts[1].DepartmentID != "603" || !slices.Equal(depts[1].AncestorIDs, []string{"601", "602"}) ||
		len(groups) != 1 || groups[0].ProjectGroupID != "601" || groups[0].RelationRole != "coordinator" {
		t.Fatal("browser identity, department hierarchy or project membership mismatch")
	}
	rollback := errors.New("roll back fixture change after checking invalidation")
	err = db.Transaction(func(tx *gorm.DB) error {
		before, err := iam.NewRepository(tx).GetPrincipal(ctx, member.PrincipalID)
		if err != nil {
			return err
		}
		if err = tx.Exec("UPDATE project_groups SET status='closed' WHERE id=601").Error; err != nil {
			return err
		}
		after, err := iam.NewRepository(tx).GetPrincipal(ctx, member.PrincipalID)
		if err != nil {
			return err
		}
		if after.AuthorizationVersion != before.AuthorizationVersion+1 {
			t.Fatal("group closure failed to invalidate the holder")
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatal(err)
	}
}
