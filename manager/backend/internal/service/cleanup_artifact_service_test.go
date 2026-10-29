package service

import (
	"context"
	"encoding/json"
	"errors"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/manager/internal/testfixture"
	"strings"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/events"
	"github.com/addp/common/execution"
	"github.com/addp/manager/internal/cog"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/repository"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func artifactTestSchema(t *testing.T, db *gorm.DB) {
	t.Helper()
	for _, sql := range []string{
		`CREATE TABLE IF NOT EXISTS manager.cleanup_targets(key TEXT PRIMARY KEY,tenant_id INTEGER NOT NULL,kind TEXT NOT NULL,identity TEXT NOT NULL,generation INTEGER NOT NULL DEFAULT 0,operation_id TEXT NOT NULL DEFAULT '',updated_at DATETIME)`,
		`CREATE TABLE IF NOT EXISTS manager.cleanup_artifact_operations(id TEXT PRIMARY KEY,tenant_id INTEGER NOT NULL,origin TEXT NOT NULL,execution_id TEXT NOT NULL,attempt INTEGER NOT NULL,lease_token TEXT NOT NULL,lease_owner TEXT NOT NULL,action TEXT NOT NULL,snapshot TEXT NOT NULL,target_keys TEXT NOT NULL,binding_digest TEXT NOT NULL,state TEXT NOT NULL,created_at DATETIME,effect_at DATETIME,finalized_at DATETIME)`,
	} {
		if err := db.Exec(sql).Error; err != nil {
			t.Fatal(err)
		}
	}
}

func artifactTestLease(t *testing.T, db *gorm.DB, policies ...string) (context.Context, execution.Lease) {
	t.Helper()
	lease := execution.Lease{ExecutionID: uuid.NewString(), TenantID: 7, Attempt: 1, Token: uuid.NewString(), Owner: "artifact-cleanup-test"}
	expires := time.Now().Add(time.Minute)
	item := execution.TaskExecution{ExecutionID: lease.ExecutionID, TenantID: 7, Module: execution.ModuleManager, TaskType: execution.TaskTypeCleanupExecutor, Source: "system", Status: execution.ExecutionStatusRunning, ExecutionBoundary: execution.ExecutionBoundaryBounded, Attempt: 1, LeaseToken: &lease.Token, LeaseOwner: &lease.Owner, LeaseExpiresAt: &expires}
	policy := "delete"
	if len(policies) > 0 {
		policy = policies[0]
	}
	parentID := uuid.NewString()
	actor := 7
	principal := int64(7)
	membership := int64(8)
	version := int64(1)
	input := events.CleanupRequest{RequestID: uuid.NewString(), ParentExecutionID: parentID, TenantID: 7, RequestedBy: 7, Action: events.CleanupActionExecute, CleanupMode: events.CleanupModePhysical, BasedOnScan: uuid.NewString(), Confirmation: events.CleanupConfirmation{Confirmed: true, TextProvided: true, RiskLevel: "high", ImpactDigest: strings.Repeat("a", 64)}, Context: events.CleanupContext{EngineID: 8, ExternalArtifactPolicy: policy}, TriggerType: "manual", ExpectedModules: []string{"manager"}, RequestedAt: time.Now().UTC(), Deadline: expires}
	if err := input.Seal(); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(input)
	item.ParentExecutionID = &parentID
	item.TriggerType = "manual"
	item.TriggeredBy = &actor
	item.ActorPrincipalID = &principal
	item.ActorTenantMembershipID = &membership
	item.IssuedAuthorizationVersion = &version
	item.ExecutionConfig = commonmodels.JSONMap{"cleanup_request": string(b), execution.DisplayLanguageKey: "en"}
	if err := db.Create(&item).Error; err != nil {
		t.Fatal(err)
	}
	return execution.ContextWithLease(t.Context(), lease), lease
}

type artifactCleanerFunc func(context.Context, string) error

func (f artifactCleanerFunc) DeleteByStorageRef(ctx context.Context, ref string) error {
	return f(ctx, ref)
}

func artifactFixture(t *testing.T) (*gorm.DB, *CleanupArtifactService, CleanupArtifactTarget, context.Context, execution.Lease, *int) {
	t.Helper()
	db := newTileCacheTaskServiceTestDB(t)
	artifactTestSchema(t, db)
	ctx, lease := artifactTestLease(t, db)
	ref := cog.ObjectStorageRef("manager", "tenant_7/cog/exact.tif")
	item := models.RasterCOG{TenantID: 7, ItemFingerprint: "cleanup-exact", SourceEngineID: 8, TargetKind: models.RasterCOGTargetKindMinIO, StorageRef: ref, Status: models.RasterCOGStatusReady, SizeBytes: 31}
	if err := db.Create(&item).Error; err != nil {
		t.Fatal(err)
	}
	testfixture.ArtifactReferenceSQLite(t, db)
	calls := new(int)
	svc := NewCleanupArtifactService(db, CleanupArtifactEffects{Bucket: "manager", Objects: artifactCleanerFunc(func(c context.Context, r string) error {
		*calls++
		// MaxOpenConns(1): this read would deadlock if Effect retained a PG tx.
		var count int64
		if err := db.WithContext(c).Model(&models.CleanupArtifactOperation{}).Where("state = ?", artifactSubmitting).Count(&count).Error; err != nil {
			return err
		}
		if count != 1 || r != ref {
			return errors.New("wrong operation or physical identity")
		}
		return nil
	})})
	targets, err := svc.Freeze(ctx, 7, nil, nil, []repository.CleanupManagedArtifact{{ID: item.ID, TenantID: 7, TaskType: execution.TaskTypeRasterCOGGeneration}})
	if err != nil || len(targets) != 1 {
		t.Fatal(targets, err)
	}
	return db, svc, targets[0], ctx, lease, calls
}

func prepareArtifactTest(t *testing.T, db *gorm.DB, s *CleanupArtifactService, ctx context.Context, lease execution.Lease, target CleanupArtifactTarget, mode, policy string) string {
	t.Helper()
	var id string
	if err := db.Transaction(func(tx *gorm.DB) error {
		var err error
		id, err = s.Prepare(ctx, tx, lease, target, mode, policy)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestCleanupArtifactEffectRollbackKeepsReceiptAndNeverRepeats(t *testing.T) {
	db, s, target, ctx, lease, calls := artifactFixture(t)
	id := prepareArtifactTest(t, db, s, ctx, lease, target, events.CleanupModePhysical, "delete")
	if err := s.Effect(ctx, id); err != nil {
		t.Fatal(err)
	}
	injected := errors.New("private result failed")
	err := db.Transaction(func(tx *gorm.DB) error {
		stats, err := s.Finalize(ctx, tx, lease, id)
		if err != nil {
			return err
		}
		if stats.DeletedPhysicalArtifacts != 1 || stats.FreedBytes != 31 {
			t.Fatal(stats)
		}
		return injected
	})
	if !errors.Is(err, injected) {
		t.Fatal(err)
	}
	if _, err := repository.ObserveCleanupTarget(ctx, db, target.Tokens[0].Key); !errors.Is(err, commonapi.ErrConflict) {
		t.Fatal("rollback lost hold", err)
	}
	restarted := NewCleanupArtifactService(db, s.effects)
	if err := restarted.Effect(ctx, id); err != nil {
		t.Fatal(err)
	}
	if *calls != 1 {
		t.Fatal("effect replayed", *calls)
	}
	if err := db.Transaction(func(tx *gorm.DB) error { _, err := restarted.Finalize(ctx, tx, lease, id); return err }); err != nil {
		t.Fatal(err)
	}
	var row models.RasterCOG
	if err := db.Unscoped().First(&row, target.ID).Error; err != nil || !row.DeletedAt.Valid {
		t.Fatal(row, err)
	}
	for _, old := range target.Tokens {
		if err := db.Transaction(func(tx *gorm.DB) error { return repository.GuardCleanupTargetWrite(ctx, tx, old) }); !errors.Is(err, commonapi.ErrConflict) {
			t.Fatal("late writer resurrected", err)
		}
	}
}

func TestCleanupArtifactUnknownEffectBlocksRestartAndOriginalWriters(t *testing.T) {
	db, s, target, ctx, lease, calls := artifactFixture(t)
	s.effects.Objects = artifactCleanerFunc(func(context.Context, string) error { *calls++; return context.DeadlineExceeded })
	id := prepareArtifactTest(t, db, s, ctx, lease, target, events.CleanupModePhysical, "delete")
	if !errors.Is(s.Effect(ctx, id), context.DeadlineExceeded) {
		t.Fatal("missing original timeout")
	}
	restarted := NewCleanupArtifactService(db, s.effects)
	if err := restarted.Effect(ctx, id); !errors.Is(err, commonapi.ErrConflict) {
		t.Fatal("unknown replay accepted", err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error { _, err := restarted.Finalize(ctx, tx, lease, id); return err }); !errors.Is(err, commonapi.ErrConflict) {
		t.Fatal("unknown finalized", err)
	}
	if *calls != 1 {
		t.Fatal(*calls)
	}
	for _, token := range target.Tokens {
		if _, err := repository.ObserveCleanupTarget(ctx, db, token.Key); !errors.Is(err, commonapi.ErrConflict) {
			t.Fatal("unresolved hold reopened", err)
		}
	}
}

func TestCleanupArtifactRejectsChangedRowAndStaleLeaseBeforeEffect(t *testing.T) {
	db, s, target, ctx, lease, calls := artifactFixture(t)
	if err := db.Model(&models.RasterCOG{}).Where("id = ?", target.ID).Update("storage_ref", cog.ObjectStorageRef("manager", "tenant_7/cog/replaced.tif")).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		_, err := s.Prepare(ctx, tx, lease, target, events.CleanupModePhysical, "delete")
		return err
	}); !errors.Is(err, commonapi.ErrConflict) {
		t.Fatal("changed row accepted", err)
	}
	if *calls != 0 {
		t.Fatal(*calls)
	}
}

func TestCleanupArtifactAbandonPreservesExactExternalIdentity(t *testing.T) {
	db := newTileCacheTaskServiceTestDB(t)
	artifactTestSchema(t, db)
	ctx, lease := artifactTestLease(t, db, "abandon")
	repo := repository.NewVectorMaterializedViewRepository(db)
	item := cleanupTestVectorMaterializedView()
	item.ErrorMessage = "original remote failure"
	if err := repo.CreateResult(ctx, item); err != nil {
		t.Fatal(err)
	}
	s := NewCleanupArtifactService(db, CleanupArtifactEffects{})
	targets, err := s.Freeze(ctx, 7, nil, []*models.VectorMaterializedView{item}, nil)
	if err != nil {
		t.Fatal(err)
	}
	id := prepareArtifactTest(t, db, s, ctx, lease, targets[0], events.CleanupModePhysical, "abandon")
	if err := s.Effect(ctx, id); err != nil {
		t.Fatal(err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		stats, err := s.Finalize(ctx, tx, lease, id)
		if stats.AbandonedExternal != 1 || stats.DeletedPhysicalArtifacts != 0 {
			t.Fatal(stats)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	stored, err := repo.GetResult(ctx, item.ID, 7)
	if err != nil || stored.TargetSchema != item.TargetSchema || stored.TargetTable != item.TargetTable || stored.Status != models.VectorMaterializedViewStatusAbandonedExternal || stored.Metadata["last_cleanup_error"] != item.ErrorMessage {
		t.Fatal(stored, err)
	}
}
