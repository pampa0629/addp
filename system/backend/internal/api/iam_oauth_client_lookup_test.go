package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	commonclient "github.com/addp/common/client"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Exercise the actual ADDP Storage -> controlled Fosite -> HTTP -> service client
// path. The fault is injected only at the OAuth client query boundary.
func TestIAMOAuthClientLookupFailureClassification(t *testing.T) {
	for _, endpoint := range []string{"token", "revoke"} {
		t.Run(endpoint, func(t *testing.T) {
			for _, tc := range []struct {
				name      string
				cause     error
				status    int
				code      string
				retryable bool
			}{
				{"missing-or-disabled", gorm.ErrRecordNotFound, 401, "invalid_client", false},
				{"canceled", context.Canceled, 503, "temporarily_unavailable", true},
				{"deadline", context.DeadlineExceeded, 503, "temporarily_unavailable", true},
				{"connection-closed", sql.ErrConnDone, 503, "temporarily_unavailable", true},
				{"connection-lost", io.EOF, 503, "temporarily_unavailable", true},
				{"postgres-recovery", &pgconn.PgError{Code: "57P03", Message: "private database recovery diagnostic"}, 503, "temporarily_unavailable", true},
				{"postgres-recovery-wrapped", fmt.Errorf("connection failed: %w", &pgconn.PgError{Code: "57P03", Message: "private database recovery diagnostic"}), 503, "temporarily_unavailable", true},
				{"internal", errors.New("private storage diagnostic"), 503, "temporarily_unavailable", true},
			} {
				t.Run(tc.name, func(t *testing.T) {
					db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
					if err != nil {
						t.Fatal(err)
					}
					sqlDB, err := db.DB()
					if err != nil {
						t.Fatal(err)
					}
					t.Cleanup(func() { _ = sqlDB.Close() })
					queries := 0
					if err := db.Callback().Query().Before("gorm:query").Register("test:oauth_client_lookup", func(tx *gorm.DB) {
						if tx.Statement.Schema != nil && tx.Statement.Schema.Table == "system.oauth_clients" {
							queries++
							tx.AddError(tc.cause)
						}
					}); err != nil {
						t.Fatal(err)
					}
					runtime, err := NewIAMRuntime(db, testIAMRuntimeConfig(), testIAMSecurityPolicy())
					if err != nil {
						t.Fatal(err)
					}
					router := gin.New()
					router.POST("/api/v1/system/oauth/token", runtime.OAuthHandler.Token)
					router.POST("/api/v1/system/oauth/revoke", runtime.OAuthHandler.Revoke)
					form := url.Values{"grant_type": {"client_credentials"}, "scope": {"addp.api"}, "audience": {"addp.api"}, "tenant_id": {"1"}}
					const secret = "private-secret-0123456789abcdef0123456789abcdef"
					if endpoint == "revoke" {
						form.Set("token", "synthetic-revocation-token")
					}
					request := httptest.NewRequest(http.MethodPost, "/api/v1/system/oauth/"+endpoint, strings.NewReader(form.Encode()))
					request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
					request.SetBasicAuth("addp-manager", secret)
					response := httptest.NewRecorder()
					router.ServeHTTP(response, request)
					if queries != 1 {
						t.Fatalf("OAuth client fault boundary calls = %d", queries)
					}
					if response.Code != tc.status {
						t.Fatalf("status = %d, want %d; body=%s", response.Code, tc.status, response.Body.String())
					}
					var payload map[string]any
					if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
						t.Fatal(err)
					}
					if payload["error"] != tc.code || payload["access_token"] != nil || payload["refresh_token"] != nil {
						t.Fatalf("failure response = %#v", payload)
					}
					for _, sensitive := range []string{secret, "private storage diagnostic", "private database recovery diagnostic", "OAuth Storage"} {
						if strings.Contains(response.Body.String(), sensitive) {
							t.Fatal("response exposed storage details or credentials")
						}
					}
					if response.Header().Get("Cache-Control") != "no-store" {
						t.Fatal("failure must not be cached")
					}
					if endpoint == "revoke" {
						return
					}
					server := httptest.NewServer(router)
					t.Cleanup(server.Close)
					source, err := commonclient.NewOAuthServiceTokenSource(server.URL, "addp-manager", secret, server.Client())
					if err != nil {
						t.Fatal(err)
					}
					token, err := source.Token(context.Background(), 1)
					var failure *commonclient.ServiceTokenError
					if token != "" || !errors.As(err, &failure) || failure.StatusCode != tc.status || failure.Code != tc.code || failure.Retryable != tc.retryable {
						t.Fatalf("service token=%q error=%#v", token, err)
					}
				})
			}
		})
	}

}

func TestIAMOAuthRevocationTransactionFailureIsNotSuccess(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := NewIAMRuntime(db, testIAMRuntimeConfig(), testIAMSecurityPolicy())
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	router.POST("/api/v1/system/oauth/revoke", runtime.OAuthHandler.Revoke)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/system/oauth/revoke", strings.NewReader("client_id=addp-cli&token=synthetic-token"))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("closed database revocation status=%d body=%s", response.Code, response.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["error"] != "server_error" || strings.Contains(response.Body.String(), "database is closed") {
		t.Fatalf("unsafe revocation failure: %s", response.Body.String())
	}
}
