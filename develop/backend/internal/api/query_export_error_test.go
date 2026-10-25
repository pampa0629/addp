package api

import (
	"encoding/json"
	commonClient "github.com/addp/common/client"
	commoni18n "github.com/addp/common/middleware/i18n"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	commonAPI "github.com/addp/common/api"
	"github.com/addp/common/dataprotection"
	"github.com/addp/develop/backend/internal/service"
	"github.com/gin-gonic/gin"
)

func TestQueryExportErrorsSeparateCapabilityPermissionProtectionAndOutage(t *testing.T) {
	for _, item := range []struct {
		err    error
		status int
		code   string
	}{
		{service.ErrQueryExportUnsupported, 422, "query_export_unsupported"},
		{service.ErrQueryExportUnavailable, 503, "query_export_unavailable"},
		{commonAPI.ErrForbidden, 403, ""},
		{commonAPI.ErrUnauthorized, 401, ""},
		{dataprotection.ErrRoleOptionUnavailable, 409, "query_protection_choice_invalid"},
		{dataprotection.ErrRoleOptionIneligible, 409, "query_protection_roles_unavailable"},
	} {
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		queryExportCreateError(ctx, item.err)
		var body map[string]any
		if json.Unmarshal(recorder.Body.Bytes(), &body) != nil || recorder.Code != item.status {
			t.Fatal("export refusal collapsed", item.err, recorder.Code)
		}
		if item.code != "" && body["error_code"] != item.code {
			t.Fatal("wrong reason", body)
		}
	}
}

func TestQueryExportRefusalsPreserveTransferReasonsAndLocalizedAdvice(t *testing.T) {
	for _, language := range []string{"zh-cn", "en"} {
		for _, item := range []struct {
			name, upstream, code, zh, en string
			status                       int
		}{
			{"role", `{"error":"PRIVATE_RESPONSE","error_code":"transfer_protection_roles_unavailable"}`, "query_protection_roles_unavailable", "当前账号的角色资格已失效或无法确认", "Your role eligibility has expired or could not be verified", 409},
			{"choice", `{"error":"PRIVATE_RESPONSE","error_code":"transfer_protection_choice_invalid"}`, "query_protection_choice_invalid", "所选导出脱敏方式已变更或失效", "The selected export masking method has changed or is no longer valid", 409},
			{"unknown", `{"error":"角色已失效 PRIVATE_RESPONSE","error_code":"unknown"}`, "query_export_unavailable", "", "", 503},
			{"malformed", `PRIVATE_RESPONSE`, "query_export_unavailable", "", "", 503},
		} {
			for _, boundary := range []string{"create", "status", "download"} {
				t.Run(language+"/"+item.name+"/"+boundary, func(t *testing.T) {
					calls := 0
					upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						calls++
						if r.Header.Get("Authorization") != "Bearer addp_at_current" {
							t.Error("borrowed Service identity")
						}
						w.WriteHeader(http.StatusConflict)
						w.Write([]byte(item.upstream))
					}))
					defer upstream.Close()
					router := gin.New()
					router.Use(commoni18n.I18nMiddleware())
					router.GET("/refusal", func(c *gin.Context) {
						err := commonClient.NewTransferClient(upstream.URL, nil).ValidateSourceProtection(c.Request.Context(), "addp_at_current", &commonClient.TransferSourceProtectionSelection{
							SourceLocator: "source", Choices: []dataprotection.FieldRoleChoice{{Target: dataprotection.ResourceReference{OwnerModule: "meta", ResourceType: "data_item", ResourceIdentity: "item"}, ComponentKey: "phone", Selection: dataprotection.RoleOptionSelection{PolicyID: "policy", PolicyRevision: 1, AssessmentRevision: 1, OptionID: "option"}}}})
						if boundary == "create" {
							queryExportCreateError(c, err)
						} else {
							queryExportError(c, err)
						}
					})
					request := httptest.NewRequest(http.MethodGet, "/refusal", nil)
					request.Header.Set("Accept-Language", language)
					recorder := httptest.NewRecorder()
					router.ServeHTTP(recorder, request)
					var body map[string]any
					if json.Unmarshal(recorder.Body.Bytes(), &body) != nil || recorder.Code != item.status || body["error_code"] != item.code || calls != 1 {
						t.Fatal("lost exact export boundary reason", recorder.Code, recorder.Body.String(), calls)
					}
					text, _ := body["error"].(string)
					if item.status == 409 {
						prefix := item.zh
						advice := "执行查询后再次导出"
						if language == "en" {
							prefix = item.en
							advice = "rerun the query before exporting again"
						}
						if len(body) != 2 || !strings.HasPrefix(text, prefix) || !strings.Contains(text, advice) {
							t.Fatal("missing precise localized export advice", body)
						}
						if strings.Contains(text, "实时结果已过期") || strings.Contains(text, "live result has expired") {
							t.Fatal("export confused with live query cache")
						}
					}
					if strings.Contains(recorder.Body.String(), "PRIVATE_RESPONSE") {
						t.Fatal("upstream body leaked")
					}
				})
			}
		}
	}
}
