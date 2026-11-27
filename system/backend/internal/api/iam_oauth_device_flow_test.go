package api

import (
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"testing"
	"time"

	sharedauth "github.com/addp/common/authorization"
	"github.com/addp/common/infra/bootstrap"
	"github.com/addp/common/infra/database"
	"github.com/addp/system/internal/iam"
	iamoauth "github.com/addp/system/internal/iam/oauth"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestOAuthDeviceFlowAgainstMySQL(t *testing.T) {
	if os.Getenv("ADDP_TEST_MYSQL_DEVICE_AUTHORIZATION") != "1" {
		t.Skip("requires isolated System MySQL installation")
	}
	db, err := bootstrap.OpenPlatformRuntime(database.Config{Schema: "system"}, "system", &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pool.Close() })
	var principal, tenant, membership, version int64
	if err := db.Raw(`SELECT p.id,t.id,m.id,p.authorization_version FROM system.local_accounts a
        JOIN system.principals p ON p.id=a.user_id JOIN system.tenant_memberships m ON m.principal_id=p.id
        JOIN system.tenants t ON t.id=m.tenant_id WHERE a.username='scan-tenant-admin' AND t.code='mysql-scan'`).Row().Scan(&principal, &tenant, &membership, &version); err != nil {
		t.Fatal(err)
	}
	exerciseDeviceGrant(t, db, iamOAuthHandlerAuthContext(principal, tenant, membership, version))
}

func exerciseDeviceGrant(t *testing.T, db *gorm.DB, actor sharedauth.AuthContext) {
	t.Helper()
	// A short provider lifetime exercises actual expiry without changing timestamps or the service policy.
	cfg := iamoauth.ProviderConfig{AccessTokenLifespan: 15 * time.Minute, RefreshTokenLifespan: 24 * time.Hour,
		AuthorizeCodeLifespan: 5 * time.Minute, DeviceCodeLifespan: 16 * time.Second, DevicePollingInterval: 5 * time.Second,
		DeviceVerificationURL: "http://localhost:5170/oauth/device", TokenEndpointURL: "http://localhost:8000/api/v1/system/oauth/token"}
	provider, err := iamoauth.NewProvider(db, cfg, iamoauth.StrategyConfig{AccessTokenLifespan: cfg.AccessTokenLifespan, RefreshTokenLifespan: cfg.RefreshTokenLifespan,
		AuthorizeCodeLifespan: cfg.AuthorizeCodeLifespan, DeviceCodeLifespan: cfg.DeviceCodeLifespan, UserCodePepper: []byte("0123456789abcdef0123456789abcdef")})
	if err != nil {
		t.Fatal(err)
	}
	bridge, err := iamoauth.NewConsentBridge(provider, iam.NewRepository(db), 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	handler, err := NewIAMOAuthHandler(provider, bridge)
	if err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	router.POST("/device/code", handler.DeviceCode)
	router.POST("/token", handler.Token)
	type codes struct {
		Device string `json:"device_code"`
		User   string `json:"user_code"`
	}
	issue := func() codes {
		t.Helper()
		response := performIAMOAuthFormRequest(t, router, "/device/code", url.Values{"client_id": {"addp-cli"}, "audience": {"addp.api"}, "scope": {"addp.api"}})
		var result codes
		decodeIAMResponse(t, response, &result)
		if response.Code != 200 || result.User == "" || result.Device == "" {
			t.Fatalf("device code issuance: HTTP %d", response.Code)
		}
		return result
	}
	decide := func(code codes, approve bool) {
		t.Helper()
		if err := bridge.DecideDeviceAuthorization(t.Context(), iamoauth.DeviceAuthorizationDecisionInput{UserCode: code.User, Approve: approve, AuthContext: actor}); err != nil {
			t.Fatalf("device consent: %v", err)
		}
	}
	poll := func(code codes, want string) {
		t.Helper()
		response := performIAMOAuthFormRequest(t, router, "/token", url.Values{"grant_type": {"urn:ietf:params:oauth:grant-type:device_code"}, "client_id": {"addp-cli"}, "device_code": {code.Device}})
		var result struct {
			Error   string `json:"error"`
			Access  string `json:"access_token"`
			Refresh string `json:"refresh_token"`
		}
		decodeIAMResponse(t, response, &result)
		if want != "" {
			if response.Code < 400 || response.Code >= 500 || result.Error != want || result.Access != "" || result.Refresh != "" {
				t.Fatalf("device poll: HTTP %d error=%q want %q without tokens", response.Code, result.Error, want)
			}
		} else {
			if response.Code != http.StatusOK || result.Access == "" || result.Refresh == "" {
				t.Fatalf("device exchange: HTTP %d error=%q", response.Code, result.Error)
			}
			svc, err := iam.NewAuthContextService(iam.NewRepository(db))
			if err != nil {
				t.Fatal(err)
			}
			auth, err := svc.ResolveUserAccessToken(t.Context(), result.Access)
			if err != nil || auth.Principal.ID != actor.Principal.ID || auth.Client.ClientID == nil || *auth.Client.ClientID != "addp-cli" || auth.Context.TenantID == nil || *auth.Context.TenantID != *actor.Context.TenantID {
				t.Fatalf("issued device grant identity: %v", err)
			}
		}
		var families int64
		if err := db.Raw(`SELECT COUNT(*) FROM system.refresh_token_families f JOIN system.oauth_device_authorizations d ON d.id=f.protocol_request_id WHERE d.device_code_hash=?`, fmt.Sprintf("%x", sha256.Sum256([]byte(code.Device)))).Scan(&families).Error; err != nil {
			t.Fatal(err)
		}
		if (want != "" && families != 0) || (want == "" && families != 1) {
			t.Fatalf("device poll %q created %d token families", want, families)
		}
	}
	approved, rejected, pending, expired := issue(), issue(), issue(), issue()
	poll(pending, "slow_down")
	decide(rejected, false)
	decide(expired, true)
	time.Sleep(cfg.DevicePollingInterval + 100*time.Millisecond)
	poll(approved, "authorization_pending")
	decide(approved, true)
	poll(rejected, "access_denied")
	time.Sleep(cfg.DevicePollingInterval + 100*time.Millisecond)
	poll(approved, "")
	var expiresAt time.Time
	if err := db.Raw("SELECT expires_at FROM system.oauth_device_authorizations WHERE device_code_hash=?", fmt.Sprintf("%x", sha256.Sum256([]byte(expired.Device)))).Scan(&expiresAt).Error; err != nil {
		t.Fatal(err)
	}
	time.Sleep(time.Until(expiresAt) + 100*time.Millisecond)
	poll(expired, "expired_token")
	poll(pending, "expired_token")
}
