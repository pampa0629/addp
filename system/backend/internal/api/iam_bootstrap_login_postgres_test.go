package api

import (
	"context"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	commonauth "github.com/addp/common/authorization"
	i18nmiddleware "github.com/addp/common/middleware/i18n"
	"github.com/addp/system/internal/iam"
	iamoauth "github.com/addp/system/internal/iam/oauth"
	"github.com/addp/system/internal/migration"
	"github.com/addp/system/internal/testsupport"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/pquerna/otp/totp"
	redisv9 "github.com/redis/go-redis/v9"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestBootstrapBrowserLoginAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("ADDP_SYSTEM_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("set ADDP_SYSTEM_POSTGRES_TEST_DSN to a disposable PostgreSQL 15+ database")
	}
	testsupport.RequireDisposablePostgresDSN(t, dsn)
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`DROP SCHEMA IF EXISTS system CASCADE`).Error; err != nil {
		t.Fatalf("reset bootstrap login schema: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := migration.NewRunner(dsn).Run(ctx); err != nil {
		t.Fatalf("apply IAM migrations: %v", err)
	}

	cfg := testIAMRuntimeConfig()
	runtime, err := NewIAMRuntime(db, cfg, testIAMSecurityPolicy())
	if err != nil {
		t.Fatalf("create IAM runtime: %v", err)
	}
	runtime.ExecutionAuthorizationHandler = &IAMExecutionAuthorizationHandler{}
	runtime.NotebookSessionAuthorizationHandler = &IAMNotebookSessionAuthorizationHandler{}
	runtime.TaskAuthorizationSubjectHandler = &IAMTaskAuthorizationSubjectHandler{}
	cipher, err := iam.NewMFACredentialCipher(cfg.IAMMFAEncryptionKey)
	if err != nil {
		t.Fatal(err)
	}
	bootstrapTime := time.Now().UTC().Add(-2 * time.Minute).Truncate(30 * time.Second)
	bootstrapService, err := iam.NewBootstrapService(
		runtime.Repository,
		runtime.IdentityService,
		cipher,
		time.Hour,
		func(prefix string) (string, error) { return prefix + "api-bootstrap-test", nil },
		func() time.Time { return bootstrapTime },
	)
	if err != nil {
		t.Fatal(err)
	}
	bootstrapSecret, _, err := bootstrapService.Prepare(ctx)
	if err != nil {
		t.Fatalf("prepare bootstrap: %v", err)
	}
	const systemSecret = "JBSWY3DPEHPK3PXP"
	administrators := []iam.BootstrapAdministratorInput{
		apiBootstrapAdministrator(t, "platform.system_administrator", "system-admin", systemSecret, bootstrapTime),
		apiBootstrapAdministrator(t, "platform.security_administrator", "security-admin", "KRSXG5DSNFXGOIDB", bootstrapTime),
		apiBootstrapAdministrator(t, "platform.audit_administrator", "audit-admin", "MFRGGZDFMZTWQ2LK", bootstrapTime),
	}
	if _, err := bootstrapService.Apply(ctx, iam.BootstrapApplyInput{
		BootstrapSecret: bootstrapSecret, Administrators: administrators,
	}); err != nil {
		t.Fatalf("apply bootstrap: %v", err)
	}

	gin.SetMode(gin.TestMode)
	redisServer, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}
	defer redisServer.Close()
	redisClient := redisv9.NewClient(&redisv9.Options{Addr: redisServer.Addr()})
	defer redisClient.Close()
	router := gin.New()
	router.Use(i18nmiddleware.I18nMiddleware())
	apiGroup := router.Group("/api/v1/system")
	if err := RegisterIAMRoutes(apiGroup, runtime, redisClient); err != nil {
		t.Fatalf("register IAM routes: %v", err)
	}
	authorizationRequest, err := runtime.ConsentBridge.CreateAuthorizationRequest(
		ctx,
		iamoauth.AuthorizationRequestInput{
			ClientID: "addp-cli", RedirectURI: "http://127.0.0.1:49152/callback",
			Scope: "addp.api", CodeChallenge: strings.Repeat("A", 43), CodeChallengeMethod: "S256",
		},
	)
	if err != nil {
		t.Fatalf("create bootstrap OAuth authorization request: %v", err)
	}
	login := performIAMJSONRequest(t, router, http.MethodPost, "/api/v1/system/login", map[string]any{
		"username": "system-admin", "password": "Bootstrap-password-system-admin!",
	}, nil)
	var loginResponse IAMBrowserLoginResponse
	decodeIAMResponse(t, login, &loginResponse)
	if login.Code != http.StatusOK || loginResponse.NextAction != "verify_mfa" || loginResponse.MFA == nil ||
		loginResponse.Session != nil || len(login.Result().Cookies()) != 0 {
		t.Fatalf("bootstrap login status=%d response=%#v cookies=%#v", login.Code, loginResponse, login.Result().Cookies())
	}
	currentCode, err := totp.GenerateCode(systemSecret, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	verification := performIAMJSONRequest(
		t,
		router,
		http.MethodPost,
		"/api/v1/system/auth/mfa-verifications",
		map[string]any{"challenge_token": loginResponse.MFA.ChallengeToken, "code": currentCode},
		nil,
	)
	var verificationResponse IAMBrowserLoginResponse
	decodeIAMResponse(t, verification, &verificationResponse)
	if verification.Code != http.StatusOK || verificationResponse.NextAction != "session_issued" ||
		verificationResponse.Session == nil {
		t.Fatalf("bootstrap MFA status=%d response=%#v body=%s",
			verification.Code, verificationResponse, verification.Body.String())
	}
	contextResponse := performIAMJSONRequest(
		t,
		router,
		http.MethodGet,
		"/api/v1/system/auth/context",
		nil,
		map[string]string{"Authorization": "Bearer " + verificationResponse.Session.AccessToken},
	)
	var authContext commonauth.AuthContext
	decodeIAMResponse(t, contextResponse, &authContext)
	if contextResponse.Code != http.StatusOK || authContext.Context.Type != "platform" ||
		authContext.Authentication.AssuranceLevel != "aal2" || len(authContext.Authentication.Methods) != 2 {
		t.Fatalf("bootstrap auth context status=%d context=%#v", contextResponse.Code, authContext)
	}
	decisionResponse := performIAMJSONRequest(
		t,
		router,
		http.MethodPost,
		"/api/v1/system/oauth/authorizations",
		map[string]any{
			"request_id": authorizationRequest.RequestID,
			"decision":   string(iamoauth.AuthorizationDecisionApprove),
		},
		map[string]string{"Authorization": "Bearer " + verificationResponse.Session.AccessToken},
	)
	var decision IAMOAuthAuthorizationDecisionResponse
	decodeIAMResponse(t, decisionResponse, &decision)
	if decisionResponse.Code != http.StatusOK ||
		!strings.HasPrefix(decision.RedirectURL, "http://127.0.0.1:49152/callback?") {
		t.Fatalf("bootstrap OAuth decision status=%d response=%#v body=%s",
			decisionResponse.Code, decision, decisionResponse.Body.String())
	}

	replayLogin := performIAMJSONRequest(t, router, http.MethodPost, "/api/v1/system/login", map[string]any{
		"username": "system-admin", "password": "Bootstrap-password-system-admin!",
	}, nil)
	var replayLoginResponse IAMBrowserLoginResponse
	decodeIAMResponse(t, replayLogin, &replayLoginResponse)
	replay := performIAMJSONRequest(
		t,
		router,
		http.MethodPost,
		"/api/v1/system/auth/mfa-verifications",
		map[string]any{"challenge_token": replayLoginResponse.MFA.ChallengeToken, "code": currentCode},
		nil,
	)
	if replay.Code != http.StatusUnauthorized {
		t.Fatalf("replayed TOTP status=%d body=%s", replay.Code, replay.Body.String())
	}

	// Exercise the production HTTP composition with both tenant choices. Each
	// case owns its credential, so real TOTP anti-replay never requires sleeps.
	for chosen := 0; chosen < 2; chosen++ {
		t.Run("MFA_then_explicit_tenant_"+strconv.Itoa(chosen), func(t *testing.T) {
			username := "browser-mfa-multi-" + strconv.Itoa(chosen)
			user, err := runtime.IdentityService.CreateLocalUser(ctx, iam.CreateLocalUserInput{
				Username: username, Password: "Browser-multi-password-2026!", DisplayName: username,
			})
			if err != nil {
				t.Fatal(err)
			}
			var memberships []int64
			var tenants []int64
			for _, suffix := range []string{"a", "b"} {
				tenant := &iam.Tenant{Code: username + "-" + suffix, Name: "Login tenant " + suffix, Status: iam.TenantStatusActive}
				if err := db.Create(tenant).Error; err != nil {
					t.Fatal(err)
				}
				membership, err := runtime.TenantMembershipService.EstablishMembership(ctx, iam.EstablishTenantMembershipInput{
					TenantID: tenant.ID, PrincipalID: user.PrincipalID, SourceType: iam.TenantMembershipSourceManual,
				})
				if err != nil {
					t.Fatal(err)
				}
				memberships = append(memberships, membership.Membership.ID)
				tenants = append(tenants, tenant.ID)
			}
			ciphertext, nonce, version, err := cipher.EncryptTOTPSecret(user.PrincipalID, systemSecret)
			if err != nil {
				t.Fatal(err)
			}
			if err := runtime.Repository.CreateMFACredential(ctx, &iam.MFACredential{
				UserID: user.PrincipalID, Method: "totp", Status: iam.MFACredentialStatusActive,
				SecretCiphertext: ciphertext, SecretNonce: nonce, KeyVersion: version,
			}); err != nil {
				t.Fatal(err)
			}
			assertNoFamily := func() {
				t.Helper()
				var count int64
				if err := db.Table("system.refresh_token_families").Where("principal_id = ?", user.PrincipalID).Count(&count).Error; err != nil {
					t.Fatal(err)
				}
				if count != 0 {
					t.Fatal("session family issued before MFA and tenant choice completed")
				}
			}
			password := performIAMJSONRequest(t, router, http.MethodPost, "/api/v1/system/login", map[string]any{
				"username": username, "password": "Browser-multi-password-2026!",
			}, nil)
			var challenge IAMBrowserLoginResponse
			decodeIAMResponse(t, password, &challenge)
			if password.Code != http.StatusOK || challenge.NextAction != "verify_mfa" || challenge.MFA == nil ||
				challenge.Session != nil || challenge.Selection != nil || len(password.Result().Cookies()) != 0 {
				t.Fatalf("password did not stop at MFA: status %d", password.Code)
			}
			assertNoFamily()
			code, err := totp.GenerateCode(systemSecret, time.Now().UTC())
			if err != nil {
				t.Fatal(err)
			}
			mfa := performIAMJSONRequest(t, router, http.MethodPost, "/api/v1/system/auth/mfa-verifications", map[string]any{
				"challenge_token": challenge.MFA.ChallengeToken, "code": code,
			}, nil)
			var selection IAMBrowserLoginResponse
			decodeIAMResponse(t, mfa, &selection)
			if mfa.Code != http.StatusOK || selection.NextAction != "select_context" || selection.Selection == nil ||
				selection.Session != nil || len(selection.Selection.Contexts) != 2 || len(mfa.Result().Cookies()) != 0 {
				t.Fatalf("MFA did not stop at tenant choice: status %d", mfa.Code)
			}
			assertNoFamily()
			choice := map[string]any{
				"selection_ticket": selection.Selection.SelectionTicket, "context_type": "tenant",
				"tenant_membership_id": strconv.FormatInt(memberships[chosen], 10),
			}
			selected := performIAMJSONRequest(t, router, http.MethodPost, "/api/v1/system/auth/context-selections", choice, nil)
			var session IAMAccessTokenResponse
			decodeIAMResponse(t, selected, &session)
			var refreshCookie *http.Cookie
			for _, cookie := range selected.Result().Cookies() {
				if cookie.Name == iamRefreshCookieName {
					refreshCookie = cookie
				}
			}
			if selected.Code != http.StatusOK || session.AccessToken == "" || refreshCookie == nil ||
				!refreshCookie.HttpOnly || refreshCookie.Path != "/api/v1/system" {
				t.Fatalf("tenant choice did not issue a browser session: status %d", selected.Code)
			}
			refreshed := performIAMJSONRequest(t, router, http.MethodPost, "/api/v1/system/refresh", nil,
				map[string]string{"Cookie": refreshCookie.Name + "=" + refreshCookie.Value})
			var renewed IAMAccessTokenResponse
			decodeIAMResponse(t, refreshed, &renewed)
			if refreshed.Code != http.StatusOK || renewed.AccessToken == "" || renewed.AccessToken == session.AccessToken {
				t.Fatalf("selected session could not be refreshed: status %d", refreshed.Code)
			}
			resolved := performIAMJSONRequest(t, router, http.MethodGet, "/api/v1/system/auth/context", nil,
				map[string]string{"Authorization": "Bearer " + renewed.AccessToken})
			var actual commonauth.AuthContext
			decodeIAMResponse(t, resolved, &actual)
			if resolved.Code != http.StatusOK || actual.Context.Type != "tenant" || actual.Context.TenantID == nil ||
				*actual.Context.TenantID != strconv.FormatInt(tenants[chosen], 10) || actual.Context.TenantMembershipID == nil ||
				*actual.Context.TenantMembershipID != strconv.FormatInt(memberships[chosen], 10) || actual.Authentication.AssuranceLevel != "aal2" ||
				strings.Join(actual.Authentication.Methods, ",") != "password,totp" {
				t.Fatalf("selected session lost MFA or tenant isolation: status %d", resolved.Code)
			}
			reused := performIAMJSONRequest(t, router, http.MethodPost, "/api/v1/system/auth/context-selections", choice, nil)
			if reused.Code != http.StatusConflict || len(reused.Result().Cookies()) != 0 {
				t.Fatalf("selection ticket replay was not rejected: status %d", reused.Code)
			}
		})
	}
}

func apiBootstrapAdministrator(
	t *testing.T,
	roleKey string,
	username string,
	secret string,
	bootstrapTime time.Time,
) iam.BootstrapAdministratorInput {
	t.Helper()
	firstAt := bootstrapTime.Add(-30 * time.Second)
	firstCode, err := totp.GenerateCode(secret, firstAt)
	if err != nil {
		t.Fatal(err)
	}
	secondCode, err := totp.GenerateCode(secret, bootstrapTime)
	if err != nil {
		t.Fatal(err)
	}
	email := username + "@example.test"
	return iam.BootstrapAdministratorInput{
		RoleKey: roleKey, Username: username, DisplayName: username,
		Password: "Bootstrap-password-" + username + "!", PrimaryEmail: &email,
		TOTPSecret: secret,
		TOTPProofs: []iam.BootstrapTOTPProof{
			{Code: firstCode, VerifiedAt: firstAt},
			{Code: secondCode, VerifiedAt: bootstrapTime},
		},
	}
}
