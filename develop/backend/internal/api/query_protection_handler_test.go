package api

import (
	commonAPI "github.com/addp/common/api"
	"github.com/addp/common/dataprotection"
	"github.com/gin-gonic/gin"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestQueryMaskingCandidatesRejectSubjectSelectorsAndDuplicateSources(t *testing.T) {
	for _, query := range []string{"?engine_id=11&target_locator=one&role_keys=tenant.admin", "?engine_id=11&target_locator=one&tenant_id=9", "?engine_id=11&engine_id=12&target_locator=one", "?engine_id=11&target_locator=one&target_locator=two"} {
		router := gin.New()
		router.GET("/query-protection-options", (&QueryHandler{}).QueryProtectionOptions)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/query-protection-options"+query, nil))
		if response.Code != 400 || !strings.Contains(response.Body.String(), "query_protection_choice_invalid") || response.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("candidate selectors reached a service", response.Code)
		}
	}
}

func TestProtectionResponseDistinguishesIAMDenialFromMaskingConflict(t *testing.T) {
	for _, item := range []struct {
		err    error
		status int
	}{{commonAPI.ErrForbidden, 403}, {commonAPI.ErrUnauthorized, 401}, {dataprotection.ErrRoleOptionUnavailable, 409}, {dataprotection.ErrRoleOptionIneligible, 409}} {
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		if !writeQueryProtectionError(ctx, item.err) || recorder.Code != item.status {
			t.Fatal("IAM denial was confused with protection conflict", item.err, recorder.Code)
		}
	}
}
