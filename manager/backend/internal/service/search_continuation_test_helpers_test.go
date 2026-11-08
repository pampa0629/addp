package service

import (
	"context"
	"fmt"
	"strconv"
	"testing"

	shared "github.com/addp/common/authorization"
)

func searchTestActor(tenant uint) shared.AuthContext {
	value := strconv.FormatUint(uint64(tenant), 10)
	membership := "1"
	client := "addp-web"
	return shared.AuthContext{Principal: shared.AuthPrincipal{Type: "user", ID: "1"}, Context: shared.AuthSessionContext{Type: "tenant", TenantID: &value, TenantMembershipID: &membership}, Client: shared.ClientConstraints{ClientID: &client, ScopeMode: "unrestricted"}}
}
func testSearchDocuments(t *testing.T, s *HybridSearchService, ctx context.Context, tenant uint, engine *uint, checker SearchVisibilityChecker, query string, size int, cursor string) (*SearchContinuationResult, error) {
	t.Helper()
	input, err := ParseSearchContinuation(query, fmt.Sprint(size), cursor)
	if err != nil {
		return nil, err
	}
	return s.SearchDocuments(ctx, searchTestActor(tenant), engine, "addp_at_test", checker, input)
}
