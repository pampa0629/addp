package api

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	i18nmiddleware "github.com/addp/common/middleware/i18n"
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
)

type browserLoginFixture struct {
	Origin     string `json:"origin"`
	Entry      string `json:"entry"`
	Username   string `json:"username"`
	Password   string `json:"password"`
	TOTPSecret string `json:"totpSecret"`
	Principal  string `json:"principal"`
	Tenant     string `json:"tenant"`
	Membership string `json:"membership"`
	TenantName string `json:"tenantName"`
}

// The IAM gate owns the database lock, resets the schemas and builds the
// frontends before calling this test. Ordinary Go tests need no browser.
func TestBrowserLoginE2E(t *testing.T) {
	buildDir := os.Getenv("ADDP_SYSTEM_LOGIN_BROWSER_BUILD_DIR")
	if buildDir == "" {
		t.Skip("run the System IAM PostgreSQL gate with --test browser-login-e2e")
	}
	dsn := os.Getenv("ADDP_SYSTEM_POSTGRES_TEST_DSN")
	testsupport.RequireDisposablePostgresDSN(t, dsn)
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal("open browser test database")
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal("get browser test database pool")
	}
	t.Cleanup(func() { _ = pool.Close() })
	assertBrowserTestSchemasAbsent(t, db)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		cleanup := db.WithContext(ctx)
		if err := cleanup.Exec("DROP SCHEMA IF EXISTS system CASCADE; DROP SCHEMA IF EXISTS common CASCADE;").Error; err != nil {
			t.Fatal("clean browser test schemas")
		}
		assertBrowserTestSchemasAbsent(t, cleanup)
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	if err := migration.NewRunner(dsn).Run(ctx); err != nil {
		t.Fatalf("apply browser test IAM migrations: %v", err)
	}
	cfg := testIAMRuntimeConfig()
	runtime, err := NewIAMRuntime(db, cfg, testIAMSecurityPolicy())
	if err != nil {
		t.Fatal("assemble browser test IAM runtime")
	}
	runtime.ExecutionAuthorizationHandler = &IAMExecutionAuthorizationHandler{}
	runtime.NotebookSessionAuthorizationHandler = &IAMNotebookSessionAuthorizationHandler{}
	runtime.TaskAuthorizationSubjectHandler = &IAMTaskAuthorizationSubjectHandler{}
	cipher, err := iam.NewMFACredentialCipher(cfg.IAMMFAEncryptionKey)
	if err != nil {
		t.Fatal("create browser test credential cipher")
	}

	redis := miniredis.RunT(t)
	redisClient := redisv9.NewClient(&redisv9.Options{Addr: redis.Addr()})
	defer redisClient.Close()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(i18nmiddleware.I18nMiddleware())
	if err := RegisterIAMRoutes(router.Group("/api/v1/system"), runtime, redisClient); err != nil {
		t.Fatalf("register browser test IAM routes: %v", err)
	}
	server := httptest.NewServer(browserLoginHandler(router, buildDir))
	defer server.Close()

	for chosen, name := range []string{"Workbench", "Portal"} {
		t.Run(name, func(t *testing.T) {
			fixture := createBrowserLoginFixture(t, ctx, db, runtime, cipher, chosen)
			fixture.Origin, fixture.Entry = server.URL, name
			runBrowserLogin(t, ctx, buildDir, fixture)
		})
	}
}

func assertBrowserTestSchemasAbsent(t *testing.T, db *gorm.DB) {
	t.Helper()
	var count int
	if err := db.Raw("SELECT count(*) FROM pg_namespace WHERE nspname IN ('system', 'common')").Scan(&count).Error; err != nil || count != 0 {
		t.Fatal("browser test database must have no remaining System or Common schema")
	}
}

func createBrowserLoginFixture(t *testing.T, ctx context.Context, db *gorm.DB, runtime *IAMRuntime, cipher *iam.MFACredentialCipher, chosen int) browserLoginFixture {
	t.Helper()
	username := "browser-login-" + strconv.Itoa(chosen)
	password := "Browser-login-" + rand.Text() + "!1"
	user, err := runtime.IdentityService.CreateLocalUser(ctx, iam.CreateLocalUserInput{
		Username: username, Password: password, DisplayName: username,
	})
	if err != nil {
		t.Fatal("create temporary browser test user")
	}
	key, err := totp.Generate(totp.GenerateOpts{Issuer: "ADDP browser test", AccountName: username})
	if err != nil {
		t.Fatal("generate temporary browser test TOTP")
	}
	ciphertext, nonce, version, err := cipher.EncryptTOTPSecret(user.PrincipalID, key.Secret())
	if err != nil {
		t.Fatal("encrypt temporary browser test TOTP")
	}
	if err := runtime.Repository.CreateMFACredential(ctx, &iam.MFACredential{
		UserID: user.PrincipalID, Method: "totp", Status: iam.MFACredentialStatusActive,
		SecretCiphertext: ciphertext, SecretNonce: nonce, KeyVersion: version,
	}); err != nil {
		t.Fatal("register temporary browser test TOTP")
	}
	fixture := browserLoginFixture{
		Username: username, Password: password, TOTPSecret: key.Secret(),
		Principal: strconv.FormatInt(user.PrincipalID, 10),
	}
	for index, suffix := range []string{"a", "b"} {
		tenant := &iam.Tenant{Code: username + "-" + suffix, Name: "Login tenant " + suffix, Status: iam.TenantStatusActive}
		if err := db.WithContext(ctx).Create(tenant).Error; err != nil {
			t.Fatal("create temporary browser test tenant")
		}
		membership, err := runtime.TenantMembershipService.EstablishMembership(ctx, iam.EstablishTenantMembershipInput{
			TenantID: tenant.ID, PrincipalID: user.PrincipalID, SourceType: iam.TenantMembershipSourceManual,
		})
		if err != nil {
			t.Fatal("create temporary browser test membership")
		}
		role, err := runtime.TenantRoleService.CreateRole(ctx, iam.CreateTenantRoleInput{
			TenantID: tenant.ID, RoleKey: "test.auth_browser", Name: "Browser acceptance",
			PermissionKeys:   []string{"workbench.data_application.execute", "asset.entry.read"},
			ActorPrincipalID: user.PrincipalID,
		})
		if err != nil {
			t.Fatal("create minimum browser test role")
		}
		_, err = runtime.TenantRoleService.CreateAssignments(ctx, iam.CreateTenantRoleAssignmentsInput{
			TenantID: tenant.ID, MembershipID: membership.Membership.ID, RoleIDs: []int64{role.ID},
			ActorPrincipalID: user.PrincipalID, Reason: "Disposable browser acceptance",
		})
		if err != nil {
			t.Fatal("assign minimum browser test role")
		}
		if index == chosen {
			fixture.Tenant, fixture.TenantName = strconv.FormatInt(tenant.ID, 10), tenant.Name
			fixture.Membership = strconv.FormatInt(membership.Membership.ID, 10)
		}
	}
	return fixture
}

// Serve the product builds at their same-origin public paths. Every System
// request goes directly to the production IAM router.
func browserLoginHandler(api http.Handler, buildDir string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		switch {
		case strings.HasPrefix(path, "/api/v1/system/"):
			api.ServeHTTP(w, r)
		case strings.HasPrefix(path, "/module-ui/workbench/assets/"):
			http.ServeFile(w, r, filepath.Join(buildDir, "workbench", "assets", filepath.Base(path)))
		case strings.HasPrefix(path, "/portal/assets/"):
			http.ServeFile(w, r, filepath.Join(buildDir, "portal", "assets", filepath.Base(path)))
		case strings.HasPrefix(path, "/data-apps/"), strings.HasPrefix(path, "/module-ui/workbench/"):
			http.ServeFile(w, r, filepath.Join(buildDir, "workbench", "index.html"))
		case strings.HasPrefix(path, "/portal/"):
			http.ServeFile(w, r, filepath.Join(buildDir, "portal", "index.html"))
		default:
			http.NotFound(w, r)
		}
	})
}

func runBrowserLogin(t *testing.T, parent context.Context, buildDir string, fixture browserLoginFixture) {
	t.Helper()
	input, err := json.Marshal(fixture)
	if err != nil {
		t.Fatal("encode temporary browser test credentials")
	}
	ctx, cancel := context.WithTimeout(parent, 110*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "node", "../../../../scripts/test/system-iam-browser-login.mjs")
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + os.Getenv("HOME"), "TMPDIR=" + buildDir}
	if path, ok := os.LookupEnv("PLAYWRIGHT_BROWSERS_PATH"); ok {
		cmd.Env = append(cmd.Env, "PLAYWRIGHT_BROWSERS_PATH="+path)
	}
	cmd.Stdin = bytes.NewReader(input)
	// The driver logs only names of checks and HTTP statuses. No trace,
	// screenshots or Playwright assertions containing credentials are emitted.
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("browser login failed: %v\n%s", err, output)
	}
	t.Log(strings.TrimSpace(string(output)))
}
