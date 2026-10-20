package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	commonauth "github.com/addp/common/authorization"
	commonAuth "github.com/addp/common/middleware/auth"
	commoni18n "github.com/addp/common/middleware/i18n"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/meta/internal/metatest"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/service"
	"github.com/gin-gonic/gin"
)

func authorizationFactsProjection(clientID, tenantID, tokenType, permission string) commonauth.AuthContext {
	now, membership := time.Now().UTC(), "1"
	projection := commonauth.AuthContext{SchemaVersion: commonauth.AuthContextSchemaVersion,
		Principal:      commonauth.AuthPrincipal{Type: "service_principal", ID: "3"},
		Context:        commonauth.AuthSessionContext{Type: "tenant", TenantID: &tenantID, TenantMembershipID: &membership},
		Authentication: commonauth.AuthenticationFacts{Methods: []string{"service_secret"}, AssuranceLevel: "not_applicable", AuthenticatedAt: now},
		Client:         commonauth.ClientConstraints{ClientID: &clientID, Audiences: []string{"addp.api"}, ScopeMode: "restricted", Scopes: []string{"addp.api"}},
		Organization:   commonauth.OrganizationContext{Departments: []commonauth.DepartmentMembership{}, ProjectGroups: []commonauth.ProjectGroupMembership{}},
		Authorization:  commonauth.AuthorizationFacts{AuthorizationVersion: "1", RoleAssignments: []commonauth.RoleAssignment{}},
		Token:          commonauth.TokenFacts{Type: tokenType, IssuedAt: now, ExpiresAt: now.Add(time.Hour)}}
	if permission != "" {
		projection.Authorization.RoleAssignments = []commonauth.RoleAssignment{{AssignmentID: "1", RoleKey: "tenant.system_runtime", SourceType: "bootstrap", ValidFrom: now.Add(-time.Minute), Scope: commonauth.AssignmentScope{Type: "tenant", TenantID: &tenantID}, Permissions: []string{permission}}}
	}
	if tokenType == "first_party_access_token" {
		projection.Principal.Type = "user"
		projection.Authentication.Methods, projection.Authentication.AssuranceLevel = []string{"password"}, "aal1"
		projection.Client.ScopeMode, projection.Client.Scopes = "unrestricted", []string{}
	}
	return projection
}

func TestMetaAuthorizationFactsRuntimeGuardAndReadOnlyQuery(t *testing.T) {
	db := metatest.OpenMetadataDB(t)
	root := models.MetaNode{TenantID: 7, EngineID: 2, NodeType: "server", Name: "PG"}
	if err := db.Create(&root).Error; err != nil {
		t.Fatal(err)
	}
	parent := models.MetaNode{TenantID: 7, EngineID: 2, ParentNodeID: &root.ID, NodeType: "schema", Name: "outdoor", FullName: "outdoor", Depth: 1}
	if err := db.Create(&parent).Error; err != nil {
		t.Fatal(err)
	}
	item := models.MetaItem{TenantID: 7, EngineID: 2, NodeID: parent.ID, ItemType: "table", Name: "persons", FullName: "outdoor.persons", Attributes: models.JSONMap{"item": map[string]any{"layout": "single"}, "storage": map[string]any{"schema_name": "outdoor"}}}
	item.Fingerprint = commonmodels.GenerateItemFingerprint(2, item.FullName)
	if err := db.Create(&item).Error; err != nil {
		t.Fatal(err)
	}
	handler := &Handler{metadataQueryService: service.NewMetadataQueryService(db)}
	for _, test := range []struct {
		client, tenant, token, permission, suffix string
		want                                      int
	}{
		{"addp-system", "7", "service_access_token", "meta.authorization_facts.read", "", 200},
		{"addp-catalog", "7", "service_access_token", "meta.authorization_facts.read", "", 200},
		{"addp-catalog", "7", "service_access_token", "", "", 403},
		{"addp-catalog", "8", "service_access_token", "meta.authorization_facts.read", "", 404},
		{"addp-catalog", "7", "service_access_token", "meta.authorization_facts.read", "?tenant_id=8", 400},
		{"addp-security", "7", "service_access_token", "meta.authorization_facts.read", "", 403},
		{"addp-manager", "7", "service_access_token", "meta.authorization_facts.read", "", 403},
		{"addp-system", "7", "service_access_token", "", "", 403},
		{"addp-web", "7", "first_party_access_token", "meta.authorization_facts.read", "", 403},
		{"addp-system", "8", "service_access_token", "meta.authorization_facts.read", "", 404},
		{"addp-system", "7", "service_access_token", "meta.authorization_facts.read", "?tenant_id=8", 400},
	} {
		t.Run(test.client+test.tenant+test.suffix+test.permission, func(t *testing.T) {
			projection := authorizationFactsProjection(test.client, test.tenant, test.token, test.permission)
			router := gin.New()
			router.Use(commoni18n.I18nMiddleware(), func(c *gin.Context) {
				if err := commonAuth.SetAuthContextForGin(c, projection); err != nil {
					t.Fatal(err)
				}
				c.Next()
			})
			api := router.Group("/api/v1/meta")
			api.Use(commonAuth.MustNewContextGuard("tenant"))
			registerDataItemAuthorizationFactsRoute(api, handler)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/meta/runtime/data-items/"+item.Fingerprint+"/authorization-facts"+test.suffix, nil))
			if response.Code != test.want {
				t.Fatalf("status=%d want=%d body=%s", response.Code, test.want, response.Body.String())
			}
			if test.want == 200 && response.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("cacheable facts")
			}
		})
	}
	var count int64
	if err := db.Model(&models.MetaItem{}).Count(&count).Error; err != nil || count != 1 {
		t.Fatal("query changed metadata", err)
	}
}
