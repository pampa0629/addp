package api

import (
	"reflect"
	"testing"

	"github.com/addp/system/internal/iam"
	"github.com/lib/pq"
)

func TestMapIAMTenantAssignablePermissionPreservesPresentationMetadata(t *testing.T) {
	permission := iam.TenantAssignablePermission{
		PermissionKey:      "manager.data_item.read",
		OwnerModule:        "manager",
		Action:             "read",
		RiskLevel:          "low",
		AllowedScopeTypes:  pq.StringArray{"tenant", "department", "project_group"},
		NameI18nKey:        "permissions.manager.data_item.read.name",
		DescriptionI18nKey: "permissions.manager.data_item.read.description",
	}

	response := mapIAMTenantAssignablePermission(permission)
	if response.PermissionKey != permission.PermissionKey || response.OwnerModule != permission.OwnerModule || response.Action != permission.Action {
		t.Fatalf("permission identity = %#v", response)
	}
	if response.NameI18nKey != permission.NameI18nKey || response.DescriptionI18nKey != permission.DescriptionI18nKey {
		t.Fatalf("permission i18n metadata = %#v", response)
	}
	if !reflect.DeepEqual(response.AllowedScopeTypes, []string{"tenant", "department", "project_group"}) {
		t.Fatalf("allowed_scope_types = %#v", response.AllowedScopeTypes)
	}
}
