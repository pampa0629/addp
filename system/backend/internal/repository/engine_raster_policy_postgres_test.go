package repository_test

import (
	"context"
	"errors"
	"net/url"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/system/internal/iam"
	"github.com/addp/system/internal/migration"
	"github.com/addp/system/internal/models"
	"github.com/addp/system/internal/repository"
	"github.com/addp/system/internal/service"
	"github.com/addp/system/internal/testsupport"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestRasterResourcePolicyAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("ADDP_SYSTEM_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("requires standard System PostgreSQL gate")
	}
	testsupport.RequireDisposablePostgresDSN(t, dsn)
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	query := u.Query()
	query.Set("search_path", "system")
	u.RawQuery = query.Encode()
	db, err := gorm.Open(postgres.Open(u.String()), &gorm.Config{})
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
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	runner := migration.NewRunner(dsn)
	if err = runner.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if err = runner.Run(ctx); err != nil {
		t.Fatal("migration idempotence", err)
	}
	var grants int64
	if err = db.Raw(`SELECT count(*) FROM role_permissions rp JOIN roles r ON r.id=rp.role_id JOIN permissions p ON p.id=rp.permission_id WHERE (r.role_key IN ('platform.system_administrator','tenant.administrator','tenant.infrastructure_administrator') AND p.permission_key IN ('system.engine_raster_policy.read','system.engine_raster_policy.update')) OR (r.role_key='platform.geopython_runtime' AND p.permission_key='system.engine_raster_policy_runtime.read')`).Scan(&grants).Error; err != nil || grants != 7 {
		t.Fatal("resource policy grants", grants, err)
	}
	tenant := iam.Tenant{Code: "raster-test", Name: "Raster test", Description: "Resource policy test"}
	if err = db.Create(&tenant).Error; err != nil {
		t.Fatal(err)
	}
	engine := models.Engine{Name: "Raster test", EngineType: "geopython_workflow", IsBuiltin: true, LifecycleState: models.EngineLifecycleActive, ConnectionInfo: models.ConnectionInfo{"protocol": "http", "host": "raster-runtime", "port": 8099}}
	if err = db.Create(&engine).Error; err != nil {
		t.Fatal(err)
	}
	svc := service.NewEngineRasterPolicyService(repository.NewEngineRasterPolicyRepository(db))
	initial, err := svc.Get(ctx, engine.ID, uint(tenant.ID))
	if err != nil {
		t.Fatal(err)
	}
	if initial.Policy != models.DefaultEngineRasterPolicy(engine.ID) || initial.Quota.Version != 1 || initial.Quota.Running != nil {
		t.Fatal("definition default", initial)
	}
	candidate := initial.Policy
	candidate.CacheMiB = 128
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := svc.SavePlatform(ctx, engine.ID, candidate, iam.AuditMetadata{})
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	successes, conflicts := 0, 0
	for err := range results {
		if err == nil {
			successes++
		} else if errors.Is(err, commonapi.ErrConflict) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatal("concurrent CAS", successes, conflicts)
	}
	running, waiting := 2, 2
	_, err = svc.SaveTenant(ctx, engine.ID, uint(tenant.ID), models.EngineRasterQuota{Version: 1, Running: &running, Waiting: &waiting}, iam.AuditMetadata{})
	if err != nil {
		t.Fatal(err)
	}
	candidate.Version = 2
	candidate.Running = 1
	candidate.Waiting = 0
	candidate.DefaultTenantRunning = 1
	candidate.DefaultTenantWaiting = 0
	if _, err = svc.SavePlatform(ctx, engine.ID, candidate, iam.AuditMetadata{}); err != nil {
		t.Fatal(err)
	}
	view, err := svc.Get(ctx, engine.ID, uint(tenant.ID))
	if err != nil {
		t.Fatal(err)
	}
	if *view.Quota.Running != 2 || *view.Quota.Waiting != 2 || view.EffectiveRunning != 1 || view.EffectiveWaiting != 0 {
		t.Fatal("shrink preserves saved quota and clamps effect", view)
	}
	resolved, err := svc.Resolve(ctx, engine.ConnectionInfo)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Policy.Version != 3 || len(resolved.Quotas) != 1 || resolved.Quotas[0].Version != 2 {
		t.Fatal("authoritative runtime snapshot", resolved)
	}
	if _, err = svc.Resolve(ctx, models.ConnectionInfo{"host": "different-runtime", "port": 8099}); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatal("foreign identity", err)
	}
	var count int64
	if err = db.Model(&iam.AuditLog{}).Where("entity_type = ? AND entity_id = ?", "engine_raster_policy", strconv.FormatUint(uint64(engine.ID), 10)).Count(&count).Error; err != nil || count != 3 {
		t.Fatal("transactional audit", count, err)
	}
}
