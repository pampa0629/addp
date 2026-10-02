package iam

import (
	"errors"
	"reflect"
	"testing"

	commonapi "github.com/addp/common/api"
)

func TestMutationPrincipalIDsAreOrderedWithoutChangingInput(t *testing.T) {
	ids := []int64{9, 2, 9, 4, 2}
	if got := normalizedPrincipalIDs(ids); !reflect.DeepEqual(got, []int64{2, 4, 9}) {
		t.Fatalf("lock order=%v", got)
	}
	if !reflect.DeepEqual(ids, []int64{9, 2, 9, 4, 2}) {
		t.Fatalf("discovery snapshot mutated=%v", ids)
	}
}

func TestMutationPrincipalSetRequiresExactCurrentAccounts(t *testing.T) {
	if err := verifyMutationPrincipalSet([]int64{3, 1}, []int64{1, 3, 3}, nil); err != nil {
		t.Fatal(err)
	}
	for _, current := range [][]int64{{1}, {1, 2, 3}, {1, 4}} {
		if err := verifyMutationPrincipalSet([]int64{1, 3}, current, nil); !errors.Is(err, commonapi.ErrConflict) {
			t.Fatalf("changed current set=%v error=%v", current, err)
		}
	}
	readErr := errors.New("discovery failed")
	if err := verifyMutationPrincipalSet(nil, nil, readErr); !errors.Is(err, readErr) {
		t.Fatalf("discovery error hidden: %v", err)
	}
}
