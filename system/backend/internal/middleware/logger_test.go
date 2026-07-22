package middleware

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/addp/system/internal/models"
	"github.com/addp/system/internal/repository"
	"github.com/addp/system/internal/service"
	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestLoggerMiddlewarePersistsOnlyStructuredOAuthSecurityAudit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.AuditLog{}); err != nil {
		t.Fatalf("migrate audit log: %v", err)
	}
	logService := service.NewLogService(repository.NewLogRepository(db), nil)
	router := gin.New()
	router.Use(LoggerMiddleware(logService))
	router.POST("/api/v1/system/oauth/token", func(c *gin.Context) {
		SetOAuthSecurityAudit(c, "oauth.token.failed", "failed", "addp-cli", "refresh_token", "", "addp.api", "invalid_grant")
		c.Set("error", "must-not-be-persisted")
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_grant"})
	})

	form := url.Values{
		"grant_type":    {"refresh_token"},
		"client_id":     {"addp-cli"},
		"refresh_token": {"addp_rt_secret"},
		"code_verifier": {"pkce-secret"},
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/system/oauth/token?code=addp_ac_secret", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	var stored models.AuditLog
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if err := db.First(&stored).Error; err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if stored.ID == 0 {
		t.Fatal("OAuth audit log was not persisted")
	}
	if stored.EntityType != "oauth_security_event" || stored.EntityID != "oauth.token.failed" {
		t.Fatalf("audit identity = %q/%q", stored.EntityType, stored.EntityID)
	}
	if stored.QueryParams != "" || stored.ErrorMessage != "" {
		t.Fatalf("OAuth query/error were persisted: query=%q error=%q", stored.QueryParams, stored.ErrorMessage)
	}
	for _, secret := range []string{"addp_rt_secret", "addp_ac_secret", "pkce-secret"} {
		if strings.Contains(stored.RequestBody, secret) {
			t.Fatalf("OAuth secret %q persisted in %s", secret, stored.RequestBody)
		}
	}
	for _, safeField := range []string{`"event":"oauth.token.failed"`, `"client_id":"addp-cli"`, `"grant_type":"refresh_token"`, `"error":"invalid_grant"`} {
		if !strings.Contains(stored.RequestBody, safeField) {
			t.Fatalf("structured audit missing %s: %s", safeField, stored.RequestBody)
		}
	}
}

func TestLoggerMiddlewareUsesStableOAuthFailureEventBeforeHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.AuditLog{}); err != nil {
		t.Fatalf("migrate audit log: %v", err)
	}
	logService := service.NewLogService(repository.NewLogRepository(db), nil)
	router := gin.New()
	router.Use(LoggerMiddleware(logService))
	router.POST("/api/v1/system/oauth/authorizations", func(c *gin.Context) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "authentication_required"})
	})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v1/system/oauth/authorizations", strings.NewReader(`{"request_id":"secret"}`)))

	var stored models.AuditLog
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if err := db.First(&stored).Error; err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if stored.EntityID != "oauth.authorization.failed" || !strings.Contains(stored.RequestBody, `"error":"authentication_required"`) {
		t.Fatalf("audit = entity_id=%q body=%s", stored.EntityID, stored.RequestBody)
	}
	if strings.Contains(stored.RequestBody, "secret") {
		t.Fatalf("raw OAuth body persisted: %s", stored.RequestBody)
	}
}
