package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/engine/plugin"
	commonauth "github.com/addp/common/middleware/auth"
	commoni18n "github.com/addp/common/middleware/i18n"
	"github.com/addp/meta/internal/metatest"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/service"
	"github.com/gin-gonic/gin"
)

func TestNodeIdentityRuntimeGuardAndStrictReadOnlyContract(t *testing.T) {
	db := metatest.OpenMetadataDB(t)
	root := models.MetaNode{TenantID: 7, EngineID: 9, NodeType: "server", Name: "PG"}
	if err := db.Create(&root).Error; err != nil {
		t.Fatal(err)
	}
	h := &Handler{metadataQueryService: service.NewMetadataQueryService(db)}
	query := authorizationfacts.NodeIdentityQuery{Target: plugin.EngineCatalogRootPath(plugin.TabularCatalogModel("schema"), 9)}
	for _, mode := range []string{"resolve", "query"} {
		var input any = query
		if mode == "query" {
			input = authorizationfacts.NodeIdentityPageQuery{Target: query.Target, Page: 1, PageSize: 100}
		}
		encoded, _ := json.Marshal(input)
		const permission = "meta.node_identity.read"
		for _, tc := range []struct {
			name, client, token, tenant, permission, body, suffix string
			status                                                int
		}{
			{"system", "addp-system", "service_access_token", "7", permission, string(encoded), "", 200},
			{"foreign tenant", "addp-system", "service_access_token", "8", permission, string(encoded), "", 200},
			{"catalog", "addp-catalog", "service_access_token", "7", permission, string(encoded), "", 403},
			{"manager", "addp-manager", "service_access_token", "7", permission, string(encoded), "", 403},
			{"missing permission", "addp-system", "service_access_token", "7", "", string(encoded), "", 403},
			{"user", "addp-web", "first_party_access_token", "7", permission, string(encoded), "", 403},
			{"query injection", "addp-system", "service_access_token", "7", permission, string(encoded), "?tenant_id=8", 400},
			{"duplicate key", "addp-system", "service_access_token", "7", permission, strings.Replace(string(encoded), `"engine_id":9`, `"engine_id":9,"engine_id":9`, 1), "", 400},
			{"unknown key", "addp-system", "service_access_token", "7", permission, strings.TrimSuffix(string(encoded), "}") + `,"tenant_id":8}`, "", 400},
			{"empty", "addp-system", "service_access_token", "7", permission, `{}`, "", 400},
			{"oversize", "addp-system", "service_access_token", "7", permission, strings.Repeat(" ", authorizationfacts.MaxNodeIdentityBytes+1), "", 413},
		} {
			t.Run(mode+"/"+tc.name, func(t *testing.T) {
				router := gin.New()
				router.Use(commoni18n.I18nMiddleware(), func(c *gin.Context) {
					if err := commonauth.SetAuthContextForGin(c, authorizationFactsProjection(tc.client, tc.tenant, tc.token, tc.permission)); err != nil {
						t.Fatal(err)
					}
					c.Next()
				})
				api := router.Group("/api/v1/meta", commonauth.MustNewContextGuard("tenant"))
				registerDataItemAuthorizationFactsRoute(api, h)
				request := httptest.NewRequest(http.MethodPost, "/api/v1/meta/runtime/node-identities/"+mode+tc.suffix, bytes.NewBufferString(tc.body))
				request.Header.Set("Content-Type", "application/json")
				response := httptest.NewRecorder()
				router.ServeHTTP(response, request)
				if response.Code != tc.status {
					t.Fatalf("status=%d want=%d body=%s", response.Code, tc.status, response.Body)
				}
				if tc.status == 200 {
					if mode == "resolve" {
						var fact authorizationfacts.NodeIdentity
						if json.Unmarshal(response.Body.Bytes(), &fact) != nil || fact.Exists != (tc.tenant == "7") {
							t.Fatal("unbound exact node fact")
						}
					} else {
						var page authorizationfacts.NodeIdentityPage
						if json.Unmarshal(response.Body.Bytes(), &page) != nil || page.Parent.Exists != (tc.tenant == "7") || page.Total != 0 || len(page.Nodes) != 0 {
							t.Fatal("unbound child node page")
						}
					}
					if response.Header().Get("Cache-Control") != "no-store" {
						t.Fatal("cacheable identity result")
					}

				}
			})
		}
	}
	var count int64
	if err := db.Model(&models.MetaNode{}).Count(&count).Error; err != nil || count != 1 {
		t.Fatal("identity query changed owner records")
	}
}
