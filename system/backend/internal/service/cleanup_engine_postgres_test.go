package service

import (
	"context"
	"encoding/json"
	"errors"
	commonapi "github.com/addp/common/api"
	"github.com/addp/common/authorization/authtest"
	"github.com/addp/common/events"
	"github.com/addp/common/execution"
	commoni18n "github.com/addp/common/middleware/i18n"
	"github.com/addp/system/internal/iam"
	"github.com/addp/system/internal/models"
	"github.com/addp/system/internal/repository"
	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"net/url"
	"os"
	"strconv"
	"testing"
	"time"
)

// Called by the registered acceptance atomicity gate. All domain tables reside
// in this fixture's random schema, and Common/request rows use exact owned IDs.
func testCleanupEngineWorkflowPostgres(t *testing.T) {
	f := newCleanupPGFixture(t, time.Minute)
	parsed, err := url.Parse(os.Getenv("ADDP_SYSTEM_CLEANUP_POSTGRES_TEST_DSN"))
	if err != nil {
		t.Fatal(err)
	}
	query := parsed.Query()
	query.Set("search_path", f.probe+",system,common")
	parsed.RawQuery = query.Encode()
	db, err := gorm.Open(postgres.Open(parsed.String()), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	t.Cleanup(func() { _ = pool.Close() })
	if err := db.AutoMigrate(&models.Engine{}, &models.ModuleDefinition{}, &models.ModuleRuntimeInstance{}); err != nil {
		t.Fatal(err)
	}
	for _, owner := range f.input.ExpectedModules {
		definition := models.ModuleDefinition{ModuleName: owner, RoutePrefix: "/" + owner, Enabled: true, Version: 1}
		if err := db.Create(&definition).Error; err != nil {
			t.Fatal(err)
		}
		instance := models.ModuleRuntimeInstance{ModuleDefinitionID: definition.ID, InstanceID: "engine-test-" + owner, Role: models.ModuleRuntimeRoleBackend, Status: models.ModuleRuntimeStatusUp, ModuleURL: "http://" + owner + ":8080", LeaseExpiresAt: time.Now().Add(time.Minute), LastHeartbeat: time.Now(), Metadata: datatypes.JSON(`{"capabilities":{"cleanup_executor":{"enabled":true,"causes":["engine.deleting"]}}}`)}
		if err := db.Create(&instance).Error; err != nil {
			t.Fatal(err)
		}
	}
	repo := repository.NewEngineRepository(db)
	engine := createDeletionTestEngine(t, repo, f.input.TenantID)
	f.coordinator.db = db
	svc := &CleanupOrchestratorService{coordinator: f.coordinator, registry: NewModuleRegistryService(repository.NewModuleRegistryRepository(db))}
	engineService := NewEngineService(repo, nil, nil).WithCleanupOrchestrator(svc)
	f.input.CauseEvent = events.CleanupCauseEngineDeleting
	f.input.Context = events.CleanupContext{EngineID: engine.ID}
	if err := f.input.Seal(); err != nil {
		t.Fatal(err)
	}
	private, _ := json.Marshal(models.CleanupEngineConfirmation{Phase: "preflight", ExternalArtifactPolicy: models.ExternalArtifactPolicyAbandon})
	if _, err := f.coordinator.accept(f.ctx, f.input, f.parent, string(private), f.input.ExpectedModules); err != nil {
		t.Fatal(err)
	}
	complete := func(id string) {
		t.Helper()
		var record models.CleanupRequestRecord
		if err := db.Where("request_id=?", id).First(&record).Error; err != nil {
			t.Fatal(err)
		}
		if err := f.coordinator.advance(f.ctx, record.ParentExecutionID, "engine-parent"); err != nil {
			t.Fatal(err)
		}
		snapshot, err := f.coordinator.snapshot(f.ctx, id, f.input.TenantID)
		if err != nil {
			t.Fatal(err)
		}
		for _, child := range snapshot.Children {
			claimed, lease, err := execution.ClaimCleanupChild(f.ctx, db, cleanupPending(child), cleanupClaimOptions(child), nil)
			if err != nil || lease == nil {
				t.Fatal("claim", err)
			}
			impact, _ := events.BuildCleanupImpactData(nil, "")
			result := events.CleanupResultData{TaskID: id, TenantID: f.input.TenantID, Module: child.Module, Action: snapshot.Input.Action, CleanupMode: snapshot.Input.CleanupMode, Status: events.CleanupResultSuccess, Summary: events.CleanupResultSummary{RiskLevel: "low"}, Impact: &impact}
			receipt, err := execution.NewCleanupReceipt(*claimed, result, "")
			if err != nil {
				t.Fatal(err)
			}
			if err := execution.CompleteCleanupWithReceipt(f.ctx, db, *lease, receipt); err != nil {
				t.Fatal(err)
			}
		}
		if err := f.coordinator.advance(f.ctx, record.ParentExecutionID, "engine-parent"); err != nil {
			t.Fatal(err)
		}
	}
	complete(f.input.RequestID)
	ctx := execution.WithUserOrigin(f.ctx, authtest.NewTenantUserAuthContext(strconv.Itoa(int(f.input.TenantID)), "7", []string{"system.cleanup.execute", "system.engine.delete"}))
	ctx, err = commoni18n.WithLanguage(ctx, "en")
	if err != nil {
		t.Fatal(err)
	}
	bypassID := uuid.NewString()
	if _, err := svc.CreateExecuteTask(ctx, bypassID, f.input.RequestID, events.CleanupModePhysical, f.input.TenantID, 7, CleanupExecuteConfirmation{Confirmed: true, ConfirmationToken: "CONFIRM", ExternalArtifactPolicy: models.ExternalArtifactPolicyAbandon}); !errors.Is(err, commonapi.ErrConflict) {
		t.Fatalf("Engine preflight entered generic execute: %v", err)
	}
	var bypassCount int64
	if err := db.Model(&models.CleanupRequestRecord{}).Where("request_id=?", bypassID).Count(&bypassCount).Error; err != nil || bypassCount != 0 {
		t.Fatal("rejected preflight persisted request", err)
	}
	req := &models.EngineDeleteRequest{Version: engine.Version, AssessmentID: f.input.RequestID, ConfirmationToken: engine.Name, ExternalArtifactPolicy: models.ExternalArtifactPolicyAbandon}
	audit := f.coordinator.audit
	f.coordinator.audit = func(context.Context, *gorm.DB, iam.AuditEvent) error { return errors.New("injected audit failure") }
	if _, err := engineService.BeginDeletion(ctx, engine.ID, f.input.TenantID, 7, req); err == nil {
		t.Fatal("engine accepted without atomic scan audit")
	}
	stored, err := repo.GetByID(engine.ID)
	if err != nil || stored.LifecycleState != models.EngineLifecycleActive || stored.Version != engine.Version || stored.DeletionScanTaskID != nil {
		t.Fatalf("partial Engine transition: %+v %v", stored, err)
	}
	f.coordinator.audit = audit
	accepted, err := engineService.BeginDeletion(ctx, engine.ID, f.input.TenantID, 7, req)
	if err != nil {
		t.Fatal(err)
	}
	if accepted.LifecycleState != models.EngineLifecycleDeleting || accepted.DeletionScanTaskID == nil {
		t.Fatal("no atomic scan identity")
	}
	scanID := *accepted.DeletionScanTaskID
	owned := []string{scanID}
	t.Cleanup(func() {
		var parents []string
		if err := db.Model(&models.CleanupRequestRecord{}).Where("request_id IN ?", owned).Pluck("parent_execution_id", &parents).Error; err != nil {
			t.Error(err)
			return
		}
		var ids []string
		if err := db.Model(&execution.TaskExecution{}).Where("execution_id IN ? OR parent_execution_id IN ?", parents, parents).Pluck("execution_id", &ids).Error; err != nil {
			t.Error(err)
			return
		}
		for _, table := range []any{&execution.Event{}, &execution.TaskExecution{}} {
			if len(ids) > 0 {
				if err := db.Where("execution_id IN ?", ids).Delete(table).Error; err != nil {
					t.Error(err)
				}
			}
		}
		if err := db.Where("request_id IN ?", owned).Delete(&models.CleanupRequestRecord{}).Error; err != nil {
			t.Error(err)
		}
		var remaining int64
		db.Model(&execution.TaskExecution{}).Where("execution_id IN ?", ids).Count(&remaining)
		if remaining != 0 {
			t.Error("Engine fixture executions remain", remaining)
		}
	})
	engineService.continueDeletion(ctx, engine.ID, scanID)
	stored, _ = repo.GetByID(engine.ID)
	if stored.DeletionExecuteTaskID != nil || stored.LifecycleState != "deleting" {
		t.Fatal("pending scan caused execute")
	}
	complete(scanID)
	// Authorization/audit failure during derived execute cannot persist an ID or
	// partial P/C. It also cannot substitute a fabricated current User context.
	f.coordinator.audit = func(context.Context, *gorm.DB, iam.AuditEvent) error {
		return errors.New("injected derived audit failure")
	}
	if _, err := svc.createEngineExecute(ctx, engine.ID, scanID); err == nil {
		t.Fatal("derived acceptance ignored audit")
	}
	stored, _ = repo.GetByID(engine.ID)
	if stored.DeletionExecuteTaskID != nil {
		t.Fatal("partial execute identity")
	}
	f.coordinator.audit = audit
	executeID, err := svc.createEngineExecute(ctx, engine.ID, scanID)
	if err != nil {
		t.Fatal(err)
	}
	owned = append(owned, executeID)
	again, err := svc.createEngineExecute(ctx, engine.ID, scanID)
	if err != nil || again != executeID {
		t.Fatal("restart duplicated execute", err)
	}
	scan, err := svc.engineStatus(ctx, scanID, f.input.TenantID, 7, engine.ID)
	if err != nil {
		t.Fatal(err)
	}
	derived, err := svc.engineStatus(ctx, executeID, f.input.TenantID, 7, engine.ID)
	if err != nil {
		t.Fatal(err)
	}
	if *derived.Parent.ActorPrincipalID != *scan.Parent.ActorPrincipalID || *derived.Parent.ActorTenantMembershipID != *scan.Parent.ActorTenantMembershipID || *derived.Parent.IssuedAuthorizationVersion != *scan.Parent.IssuedAuthorizationVersion {
		t.Fatal("derived execution replaced original actor")
	}
	engineService.continueDeletion(ctx, engine.ID, scanID)
	stored, _ = repo.GetByID(engine.ID)
	if stored.LifecycleState != "deleting" {
		t.Fatal("pending execute tombstoned Engine")
	}
	complete(executeID)
	engineService.continueDeletion(ctx, engine.ID, scanID)
	stored, _ = repo.GetByID(engine.ID)
	if stored.LifecycleState != "deleted" {
		t.Fatalf("successful confirmed workflow not finalized: %+v", stored)
	}
	version := stored.Version
	engineService.continueDeletion(ctx, engine.ID, scanID)
	stored, _ = repo.GetByID(engine.ID)
	if stored.Version != version {
		t.Fatal("terminal workflow repeated")
	}
}
