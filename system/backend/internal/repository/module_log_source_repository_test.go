package repository_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/addp/common/runtimelog"
	"github.com/addp/system/internal/models"
	"github.com/addp/system/internal/repository"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestLogSourceCatalogRetentionIdentityAndRegistration(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err = db.AutoMigrate(&models.ModuleLogSource{}, &models.ModuleLogSourceNode{}, &models.ModuleLogSourceBoot{}, &models.ModuleDefinition{}, &models.ModuleRuntimeInstance{}); err != nil {
		t.Fatal(err)
	}
	exerciseLogSourceCatalog(t, db)
}

func exerciseLogSourceCatalog(t *testing.T, db *gorm.DB) {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)
	r := repository.NewModuleRegistryRepository(db)
	report := runtimelog.SourceReport{Schema: runtimelog.SourceSchema, Node: "host", BootID: "boot-1", Sequence: 1, SampledAt: now, Complete: true, Issues: []runtimelog.SourceIssue{}, Sources: []runtimelog.Source{{Module: "copilot", InstanceID: "start-1", Role: "backend", Node: "host", CaptureStartedAt: now.Add(-time.Hour), ObservedAt: now.Add(-30 * time.Minute)}}}
	if err := r.SaveLogSources(ctx, report, 7*24*time.Hour, now); err != nil {
		t.Fatal(err)
	}
	if err := r.SaveLogSources(ctx, report, 7*24*time.Hour, now); err != nil {
		t.Fatal("idempotence", err)
	}
	report.Complete = false
	report.Issues = []runtimelog.SourceIssue{{Code: "metadata_missing", Count: 4}}
	if !errors.Is(r.SaveLogSources(ctx, report, 7*24*time.Hour, now), repository.ErrLogSourceConflict) {
		t.Fatal("changed duplicate accepted")
	}
	report.Sequence++
	report.SampledAt = now.Add(time.Second)
	if err := r.SaveLogSources(ctx, report, 7*24*time.Hour, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	page, err := r.ListLogSources(ctx, models.ModuleLogSourceFilter{Page: 1, PageSize: 20}, "host", now.Add(time.Second))
	if err != nil || page.Total != 1 || page.DiscoveryState != "unknown" {
		t.Fatalf("list %+v %v", page, err)
	}
	if len(page.DiscoveryIssues) != 1 || page.DiscoveryIssues[0].Code != "metadata_missing" || page.DiscoveryIssues[0].Count != 4 {
		t.Fatalf("scan evidence lost: %+v", page)
	}
	stale, err := r.ListLogSources(ctx, models.ModuleLogSourceFilter{Page: 1, PageSize: 20}, "host", now.Add(122*time.Second))
	if err != nil || stale.DiscoveryState != "unknown" || len(stale.DiscoveryIssues) != 1 || stale.DiscoveryIssues[0].Code != "observer_stale" {
		t.Fatalf("stale scan shown as current: %+v %v", stale, err)
	}
	unobserved, err := r.ListLogSources(ctx, models.ModuleLogSourceFilter{Page: 1, PageSize: 20}, "new-host", now)
	if err != nil || len(unobserved.DiscoveryIssues) != 1 || unobserved.DiscoveryIssues[0].Code != "observer_unobserved" {
		t.Fatalf("missing observation misclassified: %+v %v", unobserved, err)
	}
	expires := page.Data[0].ExpiresAt
	if !expires.Equal(report.Sources[0].ObservedAt.Add(7 * 24 * time.Hour)) {
		t.Fatal("scan extended retention")
	}
	if node, err := r.ResolveLogIdentity(ctx, "copilot", "start-1", now); err != nil || node != "host" {
		t.Fatal(node, err)
	}
	if _, err := r.ResolveLogIdentity(ctx, "manager", "start-1", now); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatal("foreign module accepted", err)
	}
	report.Sequence++
	report.SampledAt = now.Add(2 * time.Second)
	report.Sources[0].Role = "worker"
	if !errors.Is(r.SaveLogSources(ctx, report, 7*24*time.Hour, now), repository.ErrLogSourceConflict) {
		t.Fatal("immutable role changed")
	}
	if err := db.Model(&models.ModuleLogSourceNode{}).Where("node=?", "host").Update("scan_issues", nil).Error; err != nil {
		t.Fatal(err)
	}
	page, err = r.ListLogSources(ctx, models.ModuleLogSourceFilter{Page: 1, PageSize: 20}, "host", now.Add(time.Second))
	if err != nil || page.DiscoveryIssues[0].Code != "diagnostics_unavailable" {
		t.Fatalf("missing scan evidence fabricated: %+v %v", page, err)
	}
	report.Sequence++
	report.SampledAt = now.Add(3 * time.Second)
	report.Complete = true
	report.Issues = []runtimelog.SourceIssue{}
	report.Sources[0].Role = "backend"
	if err := r.SaveLogSources(ctx, report, 7*24*time.Hour, report.SampledAt); err != nil {
		t.Fatal(err)
	}
	page, err = r.ListLogSources(ctx, models.ModuleLogSourceFilter{Page: 1, PageSize: 20}, "host", report.SampledAt)
	if err != nil || page.DiscoveryState != "observed" || len(page.DiscoveryIssues) != 0 {
		t.Fatalf("recovery kept obsolete diagnostics: %+v %v", page, err)
	}
	definition := models.ModuleDefinition{ModuleName: "copilot", RoutePrefix: "/copilot", Enabled: true, Version: 1}
	if err = db.Create(&definition).Error; err != nil {
		t.Fatal(err)
	}
	i := models.ModuleRuntimeInstance{ModuleDefinitionID: definition.ID, InstanceID: "start-1", HostNodeName: "host", Role: "backend", ModuleURL: "http://copilot:8087", Status: "up", LastHeartbeat: now, LeaseExpiresAt: now.Add(time.Minute), RegisteredAt: now}
	if err = db.Create(&i).Error; err != nil {
		t.Fatal(err)
	}
	page, err = r.ListLogSources(ctx, models.ModuleLogSourceFilter{Page: 1, PageSize: 20}, "host", now)
	if err != nil || page.Total != 0 {
		t.Fatal("registered source remained unregistered", page, err)
	}
	if err = db.Delete(&i).Error; err != nil {
		t.Fatal(err)
	}
	if _, err = r.ResolveLogIdentity(ctx, "copilot", "start-1", expires.Add(time.Second)); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatal("expired source authorized", err)
	}
}
