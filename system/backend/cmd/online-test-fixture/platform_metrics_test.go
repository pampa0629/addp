package main

import (
	"encoding/base32"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/addp/system/internal/config"
	"github.com/addp/system/internal/iam"
	"github.com/addp/system/internal/migration"
	"github.com/addp/system/internal/testsupport"
	"github.com/pquerna/otp/totp"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestMetricsBootstrapCredentialsKeepThreeOfficersSeparate(t *testing.T) {
	now := time.Now().UTC()
	inputs, err := platformMetricsBootstrapInputs(now)
	if err != nil || len(inputs) != 3 {
		t.Fatal("bootstrap inputs", err)
	}
	secrets, passwords := map[string]bool{}, map[string]bool{}
	for i, role := range []string{"system", "security", "audit"} {
		input := inputs[i]
		key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(input.TOTPSecret)
		if err != nil || len(key) != 20 || secrets[input.TOTPSecret] || passwords[input.Password] || len(input.Password) < 32 || input.RoleKey != "platform."+role+"_administrator" {
			t.Fatal("officer credentials are not independent")
		}
		secrets[input.TOTPSecret], passwords[input.Password] = true, true
		if len(input.TOTPProofs) != 2 || input.TOTPProofs[1].VerifiedAt.Sub(input.TOTPProofs[0].VerifiedAt) != 30*time.Second || !input.TOTPProofs[1].VerifiedAt.Before(now.Truncate(30*time.Second)) {
			t.Fatal("bootstrap consumed the login MFA counter")
		}
		for _, proof := range input.TOTPProofs {
			code, err := totp.GenerateCode(input.TOTPSecret, proof.VerifiedAt)
			if err != nil || code != proof.Code {
				t.Fatal("invalid bootstrap proof")
			}
		}
	}
	permissions, err := suitePermissions("platform-node-metrics")
	if err != nil || len(permissions) != 0 || needsEngineProvisioner("platform-node-metrics") {
		t.Fatal("metrics fixture gained business provisioning")
	}
}

func TestMetricsIdentitiesAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("ADDP_SYSTEM_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("requires standard System PostgreSQL gate")
	}
	testsupport.RequireDisposablePostgresDSN(t, dsn)
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	reset := func() {
		if err := db.Exec("DROP SCHEMA IF EXISTS system CASCADE; DROP SCHEMA IF EXISTS common CASCADE").Error; err != nil {
			t.Fatal(err)
		}
	}
	reset()
	defer reset()
	if err := migration.NewRunner(dsn).Run(t.Context()); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{IAMMFAEncryptionKey: []byte("0123456789abcdef0123456789abcdef")}
	output := filepath.Join(t.TempDir(), "identity.env")
	if err := preparePlatformMetricsIdentities(db, cfg, output); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(output)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("credential file permissions")
	}
	values := map[string]string{}
	for _, line := range strings.Split(string(data), "\n") {
		key, value, ok := strings.Cut(strings.TrimPrefix(line, "export "), "=")
		if ok {
			values[key] = strings.Trim(value, "'")
		}
	}
	if len(values) != 10 || values["ADDP_ONLINE_TEST_TENANT_ID"] != "2" || values["ADDP_ONLINE_METRICS_FOREIGN_TENANT_ID"] != "3" {
		t.Fatal("identity export scope")
	}
	repo := iam.NewRepository(db)
	tokens, err := iam.NewTokenFamilyService(repo, iam.BrowserSessionConfig{ResourceTicketOwners: []string{"monitor"}}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	selection, err := iam.NewContextSelectionService(repo, tokens)
	if err != nil {
		t.Fatal(err)
	}
	cipher, err := iam.NewMFACredentialCipher(cfg.IAMMFAEncryptionKey)
	if err != nil {
		t.Fatal(err)
	}
	mfa, err := iam.NewMFAService(repo, cipher, iam.MFAServiceConfig{}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	login, err := iam.NewBrowserLoginService(iam.NewIdentityService(repo, nil), mfa, selection)
	if err != nil {
		t.Fatal(err)
	}
	contexts, err := iam.NewAuthContextService(repo)
	if err != nil {
		t.Fatal(err)
	}
	for _, prefix := range []string{"ADDP_ONLINE_METRICS_ADMIN", "ADDP_ONLINE_METRICS_SECURITY"} {
		result, err := login.LoginLocalBrowser(t.Context(), iam.LoginLocalBrowserInput{Username: values[prefix+"_USERNAME"], Password: values[prefix+"_PASSWORD"], Audit: audit("metrics-login")})
		if err != nil || result.MFA == nil || result.Session != nil {
			t.Fatal("login must challenge MFA", err)
		}
		code, err := totp.GenerateCode(values[prefix+"_TOTP"], time.Now())
		if err != nil {
			t.Fatal(err)
		}
		result, err = login.VerifyLocalBrowserMFA(t.Context(), iam.VerifyMFAChallengeInput{ChallengeToken: result.MFA.ChallengeToken, Code: code, Audit: audit("metrics-mfa")})
		if err != nil || result.Session == nil {
			t.Fatal("real MFA did not issue session", err)
		}
		projection, err := contexts.ResolveAccessToken(t.Context(), result.Session.AccessToken)
		if err != nil || projection.Context.Type != "platform" || projection.Context.TenantID != nil || projection.Authentication.AssuranceLevel != "aal2" || len(projection.Authorization.RoleAssignments) != 1 {
			t.Fatal("invalid Platform identity", err)
		}
		read := false
		for _, permission := range projection.Authorization.RoleAssignments[0].Permissions {
			read = read || permission == "platform.host_node.read"
		}
		if read != (prefix == "ADDP_ONLINE_METRICS_ADMIN") {
			t.Fatal("officer separation lost")
		}
	}
	for _, key := range []string{"ADDP_ONLINE_TEST_USER_ACCESS_TOKEN", "ADDP_ONLINE_FOREIGN_USER_ACCESS_TOKEN"} {
		projection, err := contexts.ResolveAccessToken(t.Context(), values[key])
		if err != nil {
			t.Fatal(err)
		}
		if projection.Context.Type != "tenant" || projection.Context.TenantID == nil || (*projection.Context.TenantID != "2" && *projection.Context.TenantID != "3") || len(projection.Authorization.RoleAssignments) != 1 || len(projection.Authorization.RoleAssignments[0].Permissions) != 1 || projection.Authorization.RoleAssignments[0].Permissions[0] != "monitor.execution.read" {
			t.Fatal("Tenant identity is not minimally scoped")
		}
	}
	if err := preparePlatformMetricsIdentities(db, cfg, filepath.Join(t.TempDir(), "second.env")); err == nil {
		t.Fatal("bootstrap reused an initialized deployment")
	}
}
