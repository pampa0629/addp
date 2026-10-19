package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/addp/system/internal/config"
	"github.com/addp/system/internal/iam"
	"github.com/addp/system/internal/migration"
	"github.com/addp/system/internal/testsupport"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestAdministratorGovernanceFixtureRequiresDisposableProfile(t *testing.T) {
	permissions, err := suitePermissions("platform-administrator-governance")
	if err != nil || len(permissions) != 0 || needsEngineProvisioner("platform-administrator-governance") {
		t.Fatal("governance fixture gained business provisioning")
	}
	if err := validateExternalEnvironment([]string{"POSTGRES_DB=addp", "ADDP_ONLINE_LOCAL_SGM=1"}, filepath.Join(t.TempDir(), "identity.env"), "platform-administrator-governance"); err == nil {
		t.Fatal("personal/local profile accepted")
	}
	inputs, err := platformBootstrapInputs(time.Now(), "online-governance", "Online governance")
	if err != nil {
		t.Fatal(err)
	}
	for _, input := range inputs {
		if !strings.HasPrefix(input.Username, "online-governance-") {
			t.Fatal("fixture identity namespace escaped")
		}
	}
}

func TestAdministratorGovernanceIdentitiesAgainstPostgres(t *testing.T) {
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
	output := filepath.Join(t.TempDir(), "identity.env")
	cfg := &config.Config{IAMMFAEncryptionKey: []byte("0123456789abcdef0123456789abcdef")}
	if err := prepareAdministratorGovernanceIdentities(db, cfg, output); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(output)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("fixture credentials are not owner-only")
	}
	raw, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	values := map[string]string{}
	for _, line := range strings.Split(string(raw), "\n") {
		key, value, ok := strings.Cut(strings.TrimPrefix(line, "export "), "=")
		if ok {
			values[key] = strings.Trim(value, "'")
		}
	}
	if len(values) != 18 || values["ADDP_ONLINE_TEST_TENANT_ID"] != "2" {
		t.Fatal("fixture exported unexpected facts")
	}
	ids := map[int64]bool{}
	for _, role := range []string{"SYSTEM", "SECURITY", "AUDIT"} {
		prefix := "ADDP_ONLINE_GOV_" + role
		id, err := strconv.ParseInt(values[prefix+"_ID"], 10, 64)
		if err != nil || id <= 0 || ids[id] || values[prefix+"_PASSWORD"] == "" || values[prefix+"_TOTP"] == "" {
			t.Fatal("officer credentials are not distinct")
		}
		ids[id] = true
		var count int64
		if err := db.Table("system.refresh_token_families").Where("principal_id=?", id).Count(&count).Error; err != nil || count != 0 {
			t.Fatal("fixture directly issued an officer session")
		}
	}
	var credentialCount int64
	if err := db.Table("system.mfa_credentials").Where("user_id=? AND status='active'", values["ADDP_ONLINE_GOV_SUCCESSOR_ID"]).Count(&credentialCount).Error; err != nil || credentialCount != 1 {
		t.Fatal("successor MFA enrollment missing")
	}
	repo := iam.NewRepository(db)
	negative, err := iam.NewAuthContextService(repo)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := negative.ResolveAccessToken(t.Context(), values["ADDP_ONLINE_TEST_USER_ACCESS_TOKEN"])
	if err != nil || identity.Context.Type != "tenant" {
		t.Fatal("negative user is not tenant-scoped")
	}
	for _, a := range identity.Authorization.RoleAssignments {
		for _, p := range a.Permissions {
			if strings.HasPrefix(p, "iam.platform_") {
				t.Fatal("negative user gained platform permission")
			}
		}
	}
}

func TestAdministratorGovernanceFixtureExportsEveryRequiredCredential(t *testing.T) {
	values := map[string]string{"ADDP_ONLINE_TEST_TENANT_ID": "2", "ADDP_ONLINE_TEST_USER_ACCESS_TOKEN": "fixture-only-token"}
	for _, role := range []string{"SYSTEM", "SECURITY", "AUDIT", "SUCCESSOR"} {
		for _, field := range []string{"ID", "USERNAME", "PASSWORD", "TOTP"} {
			values["ADDP_ONLINE_GOV_"+role+"_"+field] = "fixture-only-value"
		}
	}
	output := filepath.Join(t.TempDir(), "identity.env")
	if err := writeEnvironmentFile(output, values); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	for key, value := range values {
		if !strings.Contains(string(data), "export "+key+"='"+value+"'\n") {
			t.Fatalf("required governance fixture input omitted: %s", key)
		}
	}
}
