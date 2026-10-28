package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/addp/common/dataitem/authorizationfacts"
	commonAuth "github.com/addp/common/middleware/auth"
	commoni18n "github.com/addp/common/middleware/i18n"
	"github.com/addp/meta/internal/metatest"
	"github.com/addp/meta/internal/service"
	"github.com/gin-gonic/gin"
)

func TestMetaIdentityQueryRuntimeScope(t *testing.T) {
	const body = `{"engine_id":2,"page":1,"page_size":20}`
	const permission = "meta.data_item_identity.read"
	h := &Handler{metadataQueryService: service.NewMetadataQueryService(metatest.OpenMetadataDB(t))}
	for _, tc := range []struct {
		client, token, permission, body, suffix string
		want                                    int
	}{
		{"addp-system", "service_access_token", permission, body, "", 200},
		{"addp-system", "service_access_token", "meta.authorization_facts.read", body, "", 403},
		{"addp-catalog", "service_access_token", permission, body, "", 403},
		{"addp-web", "first_party_access_token", permission, body, "", 403},
		{"addp-system", "service_access_token", permission, body, "?tenant_id=8", 400},
		{"addp-system", "service_access_token", permission, `{"engine_id":2,"tenant_id":8,"page":1,"page_size":20}`, "", 400},
		{"addp-system", "service_access_token", permission, `{"engine_id":2,"engine_id":3,"page":1,"page_size":20}`, "", 400},
		{"addp-system", "service_access_token", permission, `null`, "", 400},
		{"addp-system", "service_access_token", permission, body + `{}`, "", 400},
		{"addp-system", "service_access_token", permission, strings.Repeat(" ", authorizationfacts.MaxIdentityQueryBytes) + body, "", 413},
	} {
		t.Run(tc.client+tc.token+tc.permission+tc.suffix, func(t *testing.T) {
			projection := authorizationFactsProjection(tc.client, "7", tc.token, tc.permission)
			router := gin.New()
			router.Use(commoni18n.I18nMiddleware(), func(c *gin.Context) {
				if err := commonAuth.SetAuthContextForGin(c, projection); err != nil {
					t.Fatal(err)
				}
				c.Next()
			})
			group := router.Group("/api/v1/meta", commonAuth.MustNewContextGuard("tenant"))
			registerDataItemAuthorizationFactsRoute(group, h)
			recorder := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/api/v1/meta/runtime/data-item-identities/query"+tc.suffix, strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			router.ServeHTTP(recorder, req)
			if recorder.Code != tc.want {
				t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
			}
			if tc.want == 200 && recorder.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("cacheable inventory")
			}
		})
	}
}
