package api

import (
	"errors"
	"testing"

	commonapi "github.com/addp/common/api"
	"github.com/addp/system/internal/iam"
)

func TestParseTenantMembershipPrincipalTypeFilter(t *testing.T) {
	for _, value := range []iam.PrincipalType{iam.PrincipalTypeUser, iam.PrincipalTypeServicePrincipal} {
		parsed, err := parseTenantMembershipPrincipalTypeFilter(string(value))
		if err != nil || parsed == nil || *parsed != value {
			t.Fatalf("parse principal type %q = %#v, err=%v", value, parsed, err)
		}
	}
	if parsed, err := parseTenantMembershipPrincipalTypeFilter(""); err != nil || parsed != nil {
		t.Fatalf("empty principal type = %#v, err=%v", parsed, err)
	}
	if _, err := parseTenantMembershipPrincipalTypeFilter("application"); !errors.Is(err, commonapi.ErrBadRequest) {
		t.Fatalf("invalid principal type error = %v, want bad request", err)
	}
}
