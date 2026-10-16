package api

import (
	commonauth "github.com/addp/common/authorization"
	middleware "github.com/addp/common/middleware/auth"
	"github.com/gin-gonic/gin"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestUnitContextRequiresCanonicalRealm(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, realm := range []string{"tenant", "platform"} {
		t.Run(realm, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			router := gin.New()
			router.Use(withElementHandlerAuth(7))
			router.GET("/", func(c *gin.Context) {
				auth, ok := middleware.AuthContextFromGin(c)
				if !ok {
					t.Fatal("missing canonical context")
				}
				if realm == "platform" {
					auth.Context = commonauth.AuthSessionContext{Type: "platform"}
					auth.Authentication.AssuranceLevel = "aal2"
					auth.Authentication.Methods = []string{"password", "totp"}
					if err := middleware.SetAuthContextForGin(c, auth); err != nil {
						t.Fatal(err)
					}
				}
				owner, allowed := unitContext(c)
				if !allowed || owner.Type != realm {
					t.Fatalf("owner=%+v allowed=%v", owner, allowed)
				}
				if realm == "tenant" && owner.TenantID != 7 {
					t.Fatalf("lost tenant: %+v", owner)
				}
			})
			router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
		})
	}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set("tenant_id", 0)
	if _, ok := unitContext(c); ok {
		t.Fatal("inferred platform authority from missing tenant")
	}
}
