package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/addp/system/internal/config"
	"github.com/addp/system/internal/models"
	"github.com/addp/system/internal/service"
	"github.com/gin-gonic/gin"
)

func TestSetWebSessionCookiesUsesOwnerScopedHttpOnlyTickets(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	handler := &AuthHandler{cfg: &config.Config{ResourceAccessTicketExpireMinutes: 10}}

	handler.setWebSessionCookies(context, &service.IssuedTokenPair{
		RefreshToken:                  "addp_rt_test",
		AccessExpiresIn:               900,
		ResourceAccessTicketExpiresIn: 600,
		ResourceAccessTickets: map[string]string{
			"manager":  "addp_rat_manager",
			"standard": "addp_rat_standard",
		},
	})

	cookies := recorder.Result().Cookies()
	if len(cookies) != 3 {
		t.Fatalf("cookies = %#v, want refresh plus two resource tickets", cookies)
	}
	paths := map[string]string{}
	for _, cookie := range cookies {
		if !cookie.HttpOnly || cookie.SameSite != http.SameSiteLaxMode {
			t.Fatalf("cookie flags = %#v", cookie)
		}
		paths[cookie.Path] = cookie.Name
		if cookie.Name == models.BrowserResourceAccessTicketCookieName && cookie.MaxAge != 600 {
			t.Fatalf("resource ticket cookie max age = %d", cookie.MaxAge)
		}
	}
	if paths["/api/v1/system"] != refreshCookieName ||
		paths["/api/v1/manager"] != models.BrowserResourceAccessTicketCookieName ||
		paths["/api/v1/standard"] != models.BrowserResourceAccessTicketCookieName {
		t.Fatalf("cookie paths = %#v", paths)
	}
}
