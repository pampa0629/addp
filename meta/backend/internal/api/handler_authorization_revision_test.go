package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/addp/common/dataitem/authorizationfacts"
	commonAuth "github.com/addp/common/middleware/auth"
	commoni18n "github.com/addp/common/middleware/i18n"
	"github.com/addp/meta/internal/metatest"
	"github.com/addp/meta/internal/service"
	"github.com/gin-gonic/gin"
)

func TestAuthorizationRevisionRestrictsOwnerWithoutGrantChanges(t *testing.T) {
	db := metatest.OpenMetadataDB(t)
	if err := db.Exec(`CREATE TABLE meta.authorization_basis_generation(singleton boolean PRIMARY KEY,generation text NOT NULL);
 CREATE TABLE meta.authorization_basis_revisions(tenant_id bigint PRIMARY KEY,version bigint NOT NULL);
 INSERT INTO meta.authorization_basis_generation VALUES(true,'03c40e08-fcbf-468c-932e-d54da85ca8e7');
 INSERT INTO meta.authorization_basis_revisions VALUES(7,4)`).Error; err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		client, tenant, token, permission, suffix string
		want                                      int
	}{
		{"addp-system", "7", "service_access_token", "meta.authorization_facts.read", "", 200},
		{"addp-system", "8", "service_access_token", "meta.authorization_facts.read", "", 200},
		{"addp-catalog", "7", "service_access_token", "meta.authorization_facts.read", "", 403},
		{"addp-manager", "7", "service_access_token", "meta.authorization_facts.read", "", 403},
		{"addp-system", "7", "service_access_token", "", "", 403},
		{"addp-web", "7", "first_party_access_token", "meta.authorization_facts.read", "", 403},
		{"addp-system", "7", "service_access_token", "meta.authorization_facts.read", "?tenant_id=8", 400},
	} {
		t.Run(tc.client+tc.tenant+tc.suffix+tc.permission, func(t *testing.T) {
			projection := authorizationFactsProjection(tc.client, tc.tenant, tc.token, tc.permission)
			router := gin.New()
			router.Use(commoni18n.I18nMiddleware(), func(c *gin.Context) {
				if e := commonAuth.SetAuthContextForGin(c, projection); e != nil {
					t.Fatal(e)
				}
				c.Next()
			})
			api := router.Group("/api/v1/meta")
			api.Use(commonAuth.MustNewContextGuard("tenant"))
			registerDataItemAuthorizationFactsRoute(api, &Handler{metadataQueryService: service.NewMetadataQueryService(db)})
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/meta/runtime/data-items/authorization-facts/revision"+tc.suffix, nil))
			if response.Code != tc.want {
				t.Fatalf("status=%d want=%d body=%s", response.Code, tc.want, response.Body.String())
			}
			if tc.want == 200 {
				var r authorizationfacts.Revision
				if json.Unmarshal(response.Body.Bytes(), &r) != nil || r.Validate() != nil || response.Header().Get("Cache-Control") != "no-store" {
					t.Fatal("invalid revision")
				}
				wantVersion := "4"
				if tc.tenant == "8" {
					wantVersion = "0"
				}
				if r.Version != wantVersion {
					t.Fatal("Tenant revision escaped scope")
				}
			}
		})
	}
}
