package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	commonauth "github.com/addp/common/authorization"
	commoni18n "github.com/addp/common/middleware/i18n"
	"github.com/gin-gonic/gin"
)

func TestServiceClientGuardRequiresExactServicePrincipalClient(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, testCase := range []struct {
		name          string
		principalType string
		clientID      string
		wantStatus    int
	}{
		{name: "missing AuthContext", wantStatus: http.StatusUnauthorized},
		{name: "user cannot act as service client", principalType: "user", clientID: "addp-asset", wantStatus: http.StatusForbidden},
		{name: "different service client", principalType: "service_principal", clientID: "addp-meta", wantStatus: http.StatusForbidden},
		{name: "matching service client", principalType: "service_principal", clientID: "addp-asset", wantStatus: http.StatusNoContent},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			router := gin.New()
			router.Use(commoni18n.I18nMiddleware())
			if testCase.principalType != "" {
				router.Use(func(c *gin.Context) {
					clientID := testCase.clientID
					c.Set(canonicalAuthContextKey, commonauth.AuthContext{
						Principal: commonauth.AuthPrincipal{Type: testCase.principalType, ID: "9"},
						Client:    commonauth.ClientConstraints{ClientID: &clientID},
					})
					c.Next()
				})
			}
			router.GET("/resource", MustNewServiceClientGuard("addp-asset"), func(c *gin.Context) {
				c.Status(http.StatusNoContent)
			})
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/resource", nil))
			if response.Code != testCase.wantStatus {
				t.Fatalf("status=%d body=%s, want=%d", response.Code, response.Body.String(), testCase.wantStatus)
			}
		})
	}
}

func TestNewServiceClientGuardRejectsEmptyClientID(t *testing.T) {
	if _, err := NewServiceClientGuard("  "); err == nil {
		t.Fatal("NewServiceClientGuard() error = nil")
	}
}
