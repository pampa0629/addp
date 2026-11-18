package main

import (
	"context"
	"encoding/json"
	"github.com/addp/common/authorization"
	"github.com/addp/system/internal/config"
	"github.com/addp/system/internal/iam"
	"github.com/addp/system/internal/migration"
	"github.com/addp/system/internal/testsupport"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"
	"time"
)

func TestRedisCorePermissionBoundary(t *testing.T) {
	if err := run([]string{"--suite", redisCoreSuite, "--output", "/tmp/redis-forbidden"}, []string{}); err == nil {
		t.Fatal("non-Hosted fixture was accepted")
	}
	got, err := suitePermissions(redisCoreSuite)
	want := []string{"service.definition.create", "service.definition.read", "service.definition.update", "service.definition.delete", "service.data_read.execute", "meta.catalog.read", "security.enrollment.create", "security.enrollment.read"}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatal("Redis permissions changed")
	}
	catalog, err := authorization.LoadRepositoryAuthorizationCatalog(filepath.Clean("../../../.."))
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range got {
		found := false
		for _, p := range catalog.Permissions {
			if p.Key == key {
				found = p.TenantCustomizable && p.Status == "active"
			}
		}
		if !found {
			t.Fatal("fixture needs unpublished permission", key)
		}
	}
	if !needsEngineProvisioner(redisCoreSuite) {
		t.Fatal("missing formal engine provisioner")
	}
}

func TestRedisCoreIdentityAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("ADDP_SYSTEM_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Fatal("real PG required")
	}
	testsupport.RequireDisposablePostgresDSN(t, dsn)
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	defer sqlDB.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	if err := migration.NewRunner(dsn).Run(ctx); err != nil {
		t.Fatal(err)
	}
	repository := iam.NewRepository(db)
	clock, err := testsupport.FixtureDatabaseClock(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	identity := iam.NewIdentityService(repository, nil)
	tenants := iam.NewPlatformTenantService(repository, clock)
	reserve, err := createUser(ctx, identity, "redis-reserve")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tenants.Create(ctx, iam.CreateTenantInput{Code: "redis-reserve", Name: "Redis reserve", InitialAdministratorPrincipalID: reserve.PrincipalID, ActorPrincipalID: reserve.PrincipalID, Audit: audit("redis-reserve")}); err != nil {
		t.Fatal(err)
	}
	actor, err := createUser(ctx, identity, "redis-fixture-owner")
	if err != nil {
		t.Fatal(err)
	}
	tenant, err := tenants.Create(ctx, iam.CreateTenantInput{Code: "redis-fixture", Name: "Redis fixture", InitialAdministratorPrincipalID: actor.PrincipalID, ActorPrincipalID: actor.PrincipalID, Audit: audit("redis-fixture")})
	if err != nil {
		t.Fatal(err)
	}
	values := map[string]string{"ADDP_ONLINE_TEST_TENANT_ID": fmtID(tenant.ID), "ADDP_ONLINE_TEST_RUN_ID": "protocol-precheck"}
	if err := prepareRedisCoreIdentity(ctx, db, tenant.ID, actor.PrincipalID, values); err != nil {
		t.Fatal(err)
	}
	path := os.Getenv("ADDP_REDIS_WIRING_IDENTITY")
	if path == "" {
		t.Fatal("private identity output required")
	}
	if err := writeRedisPrivateJSON(path, values); err != nil {
		t.Fatal(err)
	}
	if err := redisCoreConsumerCommand(ctx, db, "prepare", path, 731); err != nil {
		t.Fatal(err)
	}
	var roles int64
	if err := db.Raw(`SELECT count(*) FROM system.role_assignments r JOIN system.oauth_clients c ON c.service_principal_id=r.principal_id WHERE c.client_id=?`, values["ADDP_REDIS_HTTP_CLIENT_ID"]).Scan(&roles).Error; err != nil || roles != 0 {
		t.Fatal("OAuth probe gained Role", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var owned map[string]string
	if err := json.Unmarshal(raw, &owned); err != nil {
		t.Fatal(err)
	}
	owned["ADDP_REDIS_HTTP_QUERY_ID"] = "732"
	if err := writeRedisPrivateJSON(path, owned); err != nil {
		t.Fatal(err)
	}
	if err := redisCoreConsumerCommand(ctx, db, "cleanup", path, 0); err == nil {
		t.Fatal("changed Query binding was accepted")
	}
	owned["ADDP_REDIS_HTTP_QUERY_ID"] = "731"
	if err := writeRedisPrivateJSON(path, owned); err != nil {
		t.Fatal(err)
	}
	if err := redisCoreConsumerCommand(ctx, db, "cleanup", path, 0); err != nil {
		t.Fatal(err)
	}
	// The Consumer is a System-owned fixture. Its fake protocol-only Query ID
	// never goes through a product data request and is not T4 query evidence.
	t.Log("fixed PG owner fixture: zero-role OAuth account; exact Consumer prepare/cleanup; no admin Token")
}
func fmtID(value int64) string { return strconv.FormatInt(value, 10) }

func TestRedisCoreRetireAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("ADDP_SYSTEM_POSTGRES_TEST_DSN")
	testsupport.RequireDisposablePostgresDSN(t, dsn)
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	defer sqlDB.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	path := os.Getenv("ADDP_REDIS_WIRING_IDENTITY")
	if err := redisCoreConsumerCommand(ctx, db, "retire", path, 0); err != nil {
		t.Fatal(err)
	}
	var consumers int64
	if err := db.Table("system.api_consumers").Count(&consumers).Error; err != nil || consumers != 0 {
		t.Fatal("owned Consumer remains", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var values map[string]string
	if err := json.Unmarshal(raw, &values); err != nil {
		t.Fatal(err)
	}
	var status string
	if err := db.Raw("SELECT status FROM system.principals WHERE id=?", values["ADDP_REDIS_HTTP_ACCOUNT_ID"]).Scan(&status).Error; err != nil || status != "suspended" {
		t.Fatal("OAuth account was not retired", err)
	}
}

// Run only under the real gate's process supervisor, which terminates this writer
// after the first committed action of the normal initial identity preparation.
func TestRedisCoreInterruptedWriter(t *testing.T) {
	dsn := os.Getenv("ADDP_SYSTEM_POSTGRES_TEST_DSN")
	testsupport.RequireDisposablePostgresDSN(t, dsn)
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	_, err = createUser(t.Context(), iam.NewIdentityService(iam.NewRepository(db), nil), "external-online-reserve")
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(time.Minute)
	t.Fatal("identity supervisor did not terminate writer")
}

// The CI job owns a separate, fresh PG service for the Hosted-only CLI.
func TestRedisCoreMigrateHostedIdentity(t *testing.T) {
	cfg := config.Load()
	if os.Getenv("RUNNER_ENVIRONMENT") != "github-hosted" || os.Getenv("GITHUB_ACTIONS") != "true" ||
		cfg.PostgresDB != "addp_online" || cfg.PostgresHost != "127.0.0.1" ||
		cfg.PostgresPort != os.Getenv("ADDP_REDIS_IDENTITY_PG_PORT") {
		t.Fatal("dedicated Hosted identity PG required")
	}
	if err := migration.NewRunner(cfg.PostgreSQLDSN()).Run(t.Context()); err != nil {
		t.Fatal(err)
	}
}
