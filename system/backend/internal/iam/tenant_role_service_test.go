package iam

import (
	"errors"
	"reflect"
	"testing"

	commonapi "github.com/addp/common/api"
)

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
