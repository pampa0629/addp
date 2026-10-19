package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/addp/system/internal/iam"
	"github.com/addp/system/internal/migration"
	"github.com/addp/system/internal/testsupport"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/pquerna/otp/totp"
	redisv9 "github.com/redis/go-redis/v9"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"
	"time"
)

func TestAdministratorGovernanceAPIAuthenticationAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("ADDP_SYSTEM_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("set ADDP_SYSTEM_POSTGRES_TEST_DSN")
	}
	testsupport.RequireDisposablePostgresDSN(t, dsn)
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("DROP SCHEMA IF EXISTS system CASCADE").Error; err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if err := migration.NewRunner(dsn).Run(ctx); err != nil {
		t.Fatal(err)
	}
	cfg := testIAMRuntimeConfig()
	runtime, err := NewIAMRuntime(db, cfg, testIAMSecurityPolicy())
	if err != nil {
		t.Fatal(err)
	}
	cipher, err := iam.NewMFACredentialCipher(cfg.IAMMFAEncryptionKey)
	if err != nil {
		t.Fatal(err)
	}
	runtime.ExecutionAuthorizationHandler = &IAMExecutionAuthorizationHandler{}
	runtime.NotebookSessionAuthorizationHandler = &IAMNotebookSessionAuthorizationHandler{}
	runtime.TaskAuthorizationSubjectHandler = &IAMTaskAuthorizationSubjectHandler{}
	gin.SetMode(gin.TestMode)
	router := gin.New()
	api := router.Group("/api/v1/system")
	redisServer := miniredis.RunT(t)
	redisClient := redisv9.NewClient(&redisv9.Options{Addr: redisServer.Addr()})
	t.Cleanup(func() { _ = redisClient.Close() })
	if err := RegisterIAMRoutes(api, runtime, redisClient); err != nil {
		t.Fatal(err)
	}
	if err := RegisterIAMManagementRoutes(api, runtime, NewModuleRegistryHandler(nil, []byte("test-key"))); err != nil {
		t.Fatal(err)
	}
	request := func(method, path, token string, body any) *httptest.ResponseRecorder {
		t.Helper()
		var data []byte
		if body != nil {
			data, err = json.Marshal(body)
			if err != nil {
				t.Fatal(err)
			}
		}
		req := httptest.NewRequest(method, "/api/v1/system"+path, bytes.NewReader(data))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept-Language", "en")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, req)
		return recorder
	}
	requireStatus := func(response *httptest.ResponseRecorder, status int) {
		t.Helper()
		if response.Code != status {
			t.Fatalf("status=%d expected=%d", response.Code, status)
		}
	}
	ids := map[string]int64{}
	tokens := map[string]string{}
	for _, role := range []string{"system", "security", "audit"} {
		user, err := runtime.IdentityService.CreateLocalUser(ctx, iam.CreateLocalUserInput{Username: role, Password: "administrator-api-password", DisplayName: role})
		if err != nil {
			t.Fatal(err)
		}
		ids[role] = user.PrincipalID
		if err := db.Exec(`INSERT INTO system.role_assignments(principal_id,role_id,scope_type,source_type) SELECT ?,id,'platform','bootstrap' FROM system.roles WHERE role_key=?`, user.PrincipalID, "platform."+role+"_administrator").Error; err != nil {
			t.Fatal(err)
		}
		secret := "JBSWY3DPEHPK3PXP"
		ciphertext, nonce, version, err := cipher.EncryptTOTPSecret(user.PrincipalID, secret)
		if err != nil {
			t.Fatal(err)
		}
		if err := runtime.Repository.CreateMFACredential(ctx, &iam.MFACredential{UserID: user.PrincipalID, Method: "totp", Status: iam.MFACredentialStatusActive, SecretCiphertext: ciphertext, SecretNonce: nonce, KeyVersion: version}); err != nil {
			t.Fatal(err)
		}
		login := request("POST", "/login", "", map[string]any{"username": role, "password": "administrator-api-password"})
		requireStatus(login, 200)
		var challenge IAMBrowserLoginResponse
		if err := json.Unmarshal(login.Body.Bytes(), &challenge); err != nil {
			t.Fatal(err)
		}
		if challenge.MFA == nil {
			t.Fatal("administrator login did not require MFA")
		}
		code, err := totp.GenerateCode(secret, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		verification := request("POST", "/auth/mfa-verifications", "", map[string]any{"challenge_token": challenge.MFA.ChallengeToken, "code": code})
		requireStatus(verification, 200)
		var session IAMBrowserLoginResponse
		if err := json.Unmarshal(verification.Body.Bytes(), &session); err != nil {
			t.Fatal(err)
		}
		if session.Session == nil {
			t.Fatal("MFA did not issue the platform session")
		}
		tokens[role] = session.Session.AccessToken
	}
	requireStatus(request("GET", "/platform/identity_changes", tokens["system"], nil), 404)
	requireStatus(request("GET", "/platform/administrator_changes", "", nil), 401)
	requireStatus(request("GET", "/platform/users", tokens["audit"], nil), 403)
	requireStatus(request("GET", "/platform/administrator_changes/candidates", tokens["audit"], nil), 403)
	requireStatus(request("POST", "/platform/administrator_changes", tokens["audit"], map[string]any{"change_type": "platform_identity_suspend", "target_principal_id": strconv.FormatInt(ids["system"], 10), "reason": "audit cannot request"}), 403)
	body := map[string]any{"change_type": "platform_identity_suspend", "target_principal_id": strconv.FormatInt(ids["audit"], 10), "reason": "controlled suspension"}
	created := request("POST", "/platform/administrator_changes", tokens["security"], body)
	requireStatus(created, 201)
	var change IAMAdministratorChangeResponse
	if err := json.Unmarshal(created.Body.Bytes(), &change); err != nil {
		t.Fatal(err)
	}
	requireStatus(request("POST", "/platform/administrator_changes/"+change.ID+"/approve", tokens["security"], map[string]any{"version": change.Version, "reason": "self approval"}), 403)
	// An approval and its state transition are one request; no ordinary user write follows.
	result := request("POST", "/platform/administrator_changes/"+change.ID+"/approve", tokens["system"], map[string]any{"version": change.Version, "reason": "independent review"})
	requireStatus(result, 200)
	if err := json.Unmarshal(result.Body.Bytes(), &change); err != nil || change.Status != iam.PrivilegedChangeStatusApplied {
		t.Fatal("approval did not apply suspension")
	}
	if change.Review == nil || change.Review.PrincipalID != strconv.FormatInt(ids["system"], 10) || change.Review.Reason != "independent review" {
		t.Fatal("review identity and reason missing from read projection")
	}
	requireStatus(request("GET", "/platform/administrator_changes", tokens["audit"], nil), 401)
	// The security administrator has user lifecycle permission, but cannot bypass administrator governance.
	requireStatus(request("POST", fmt.Sprintf("/platform/users/%d/reactivate", ids["audit"]), tokens["security"], map[string]any{"reason": "ordinary bypass"}), 409)
	requireStatus(request("POST", fmt.Sprintf("/platform/users/%d/reactivate", ids["audit"]), tokens["security"], map[string]any{"reason": "old protocol", "change_request_id": change.ID}), 400)
	requireStatus(request(http.MethodPost, "/auth/administrator-recovery/enrollments", "", map[string]any{"recovery_token": "addp_pcr_invalid"}), 401)
}
