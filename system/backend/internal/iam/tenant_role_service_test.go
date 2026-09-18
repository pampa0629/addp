package iam

import (
	"errors"
	"reflect"
	"testing"

	commonapi "github.com/addp/common/api"
)

func TestTenantRoleKeyValidation(t *testing.T) {
	for _, key := range []string{"", "ontology_manager", "Custom.manager", "custom.Manager", "1custom.manager", "custom.1manager", "custom.role-name", "custom..manager", "custom.role.extra", "custom.本体", "custom.role\nextra", "custom .manager"} {
		t.Run(key, func(t *testing.T) {
			_, _, _, _, err := validateTenantRoleDefinition(key, "Role", []string{"tenant"}, []string{"ontology.revision.read"})
			if !errors.Is(err, ErrTenantRoleKeyInvalid) || !errors.Is(err, commonapi.ErrBadRequest) {
				t.Fatalf("error = %v, want role key invalid / bad request", err)
			}
		})
	}
	for _, key := range []string{"custom.ontology_manager", "tenant.reader", "a.b", "custom_2.role_3"} {
		got, _, _, _, err := validateTenantRoleDefinition(" \t"+key+"\n", "Role", []string{"tenant"}, []string{"ontology.revision.read"})
		if err != nil || got != key {
			t.Fatalf("key = %q, err = %v, want %q", got, err, key)
		}
	}
	_, _, _, _, err := validateTenantRoleDefinition("custom.reader", " ", []string{"tenant"}, []string{"ontology.revision.read"})
	if !errors.Is(err, commonapi.ErrBadRequest) || errors.Is(err, ErrTenantRoleKeyInvalid) {
		t.Fatalf("missing name misclassified as role key error: %v", err)
	}
}

func TestNormalizeTenantRoleAssignmentIDs(t *testing.T) {
	t.Run("sorts explicit role IDs", func(t *testing.T) {
		got, err := normalizeTenantRoleAssignmentIDs([]int64{9, 3, 7})
		if err != nil {
			t.Fatal(err)
		}
		if want := []int64{3, 7, 9}; !reflect.DeepEqual(got, want) {
			t.Fatalf("role IDs = %v, want %v", got, want)
		}
	})

	for name, roleIDs := range map[string][]int64{
		"empty":     nil,
		"duplicate": {3, 3},
		"invalid":   {0},
		"too many":  make([]int64, maxTenantRoleAssignmentBatchSize+1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := normalizeTenantRoleAssignmentIDs(roleIDs); !errors.Is(err, commonapi.ErrBadRequest) {
				t.Fatalf("error = %v, want bad request", err)
			}
		})
	}
}
