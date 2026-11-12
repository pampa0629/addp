package repository

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/execution"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/manager/internal/cog"
	"github.com/addp/manager/internal/models"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// Real PG control/row-lock proof. Provider effects remain controlled responses;
// this test does not claim Hosted MinIO or Runtime acceptance.
func TestIntegrationPostgresManagerArtifactTargetTransactions(t *testing.T) {
	if os.Getenv("ADDP_POSTGRES_INTEGRATION") != "1" {
		t.Skip("requires standard Manager PostgreSQL gate")
	}
	db, err := gorm.Open(postgres.Open(managerTileCacheRepositoryIntegrationDSN()), &gorm.Config{})
	if err != nil {
		t.Fatal("open PG failed")
	}
	pool, _ := db.DB()
	defer pool.Close()
	pool.SetMaxOpenConns(4)
	if err := db.Exec("CREATE SCHEMA IF NOT EXISTS manager").Error; err != nil {
		t.Fatal(err)
	}
	if err := MigrateCleanupArtifactSchema(db); err != nil {
		t.Fatal(err)
	}
	if err := execution.EnsureStore(db); err != nil {
		t.Fatal(err)
	}
	t.Run("unstarted_source_authorization_failure", func(t *testing.T) {
		if err := db.AutoMigrate(&models.RasterCOGTask{}, &models.RasterCOG{}); err != nil {
			t.Fatal(err)
		}
		tenant := uint(time.Now().UnixNano()%100000000 + 970000000)
		t.Cleanup(func() {
			for _, model := range []interface{}{&models.CleanupTarget{}, &execution.TaskExecution{}, &models.RasterCOGTask{}} {
				if err := db.Unscoped().Where("tenant_id = ?", tenant).Delete(model).Error; err != nil {
					t.Error(err)
				}
			}
		})
		fp := uuid.NewString()
		task := models.RasterCOGTask{TenantID: tenant, Name: "authorization-preparation-release", Enabled: true, Config: commonmodels.JSONMap{
			"target": commonmodels.JSONMap{"item_fingerprint": fp},
			"result": commonmodels.JSONMap{"storage_ref": cog.ObjectStorageRef("manager", "tenant_"+fmt.Sprint(tenant)+"/"+fp+".tif")},
		}}
		if err := createTaskDefinition(t.Context(), db, execution.TaskTypeRasterCOGGeneration, &task); err != nil {
			t.Fatal(err)
		}
		repo := NewRasterCOGRepository(db)
		claim := func() *execution.TaskExecution {
			t.Helper()
			item := newManagerRepositoryTestExecution(uuid.NewString(), int(tenant), execution.TaskTypeRasterCOGGeneration, time.Now().UTC())
			actor, principal, membership, version := 31, int64(31), int64(41), int64(1)
			item.TriggeredBy, item.ActorPrincipalID, item.ActorTenantMembershipID, item.IssuedAuthorizationVersion = &actor, &principal, &membership, &version
			snapshot, err := repo.TaskConfigSnapshot(t.Context(), task.ID, tenant)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := repo.ClaimExecution(t.Context(), task.ID, tenant, item, false, snapshot); err != nil {
				t.Fatal(err)
			}
			return item
		}
		assertHolds := func(item *execution.TaskExecution, want int64) {
			t.Helper()
			var count int64
			if err := db.Model(&models.CleanupTarget{}).Where("tenant_id = ? AND operation_id = ?", tenant, item.ExecutionID).Count(&count).Error; err != nil || count != want {
				t.Fatalf("generation holds=%d want=%d err=%v", count, want, err)
			}
		}
		old := claim()
		assertHolds(old, 3)
		// Persist the old finalizer's failure state, leaving its exact holds.
		if err := db.Model(old).Updates(map[string]interface{}{"status": "failed", "completed_at": time.Now(), "error_details": commonmodels.JSONMap{"code": "source_authorization_required"}}).Error; err != nil {
			t.Fatal(err)
		}
		next := claim()
		assertHolds(old, 0)
		assertHolds(next, 3)
		injected := errors.New("preparation failure rollback")
		if err := db.Transaction(func(tx *gorm.DB) error {
			if err := failPendingFileExecution(tx, next, time.Now()); err != nil {
				return err
			}
			return injected
		}); !errors.Is(err, injected) {
			t.Fatal(err)
		}
		stored, err := repo.GetExecution(t.Context(), int(tenant), next.ExecutionID)
		if err != nil || stored.Status != execution.ExecutionStatusPending {
			t.Fatalf("failure rollback changed execution: %+v %v", stored, err)
		}
		assertHolds(next, 3)
		if err := repo.FailUnboundExecution(t.Context(), next); err != nil {
			t.Fatal(err)
		}
		assertHolds(next, 0)
		for _, item := range []*execution.TaskExecution{old, next} {
			stored, err := repo.GetExecution(t.Context(), int(tenant), item.ExecutionID)
			if err != nil || stored.Status != execution.ExecutionStatusFailed || stored.Attempt != 0 || stored.ErrorDetails["code"] != "source_authorization_required" {
				t.Fatalf("failure evidence lost: %+v %v", stored, err)
			}
		}
	})
	for _, outcome := range []string{"unknown", "succeeded"} {
		t.Run(outcome, func(t *testing.T) {
			id := uuid.NewString()
			fp := uuid.NewString()
			tokenValue := uuid.NewString()
			owner := "artifact-pg-" + id
			tokens := []CleanupTargetToken{{Key: CleanupTargetKey{TenantID: 7, Kind: CleanupTargetGeneration, Identity: execution.TaskTypeRasterCOGGeneration + ":" + fp}}, {Key: CleanupTargetKey{TenantID: 7, Kind: CleanupTargetGeneration, Identity: execution.TaskTypeRasterCOGGeneration + ":task:" + id}}}
			text, _ := json.Marshal(tokens)
			expires := time.Now().UTC().Add(time.Minute)
			item := execution.TaskExecution{ExecutionID: id, TenantID: 7, Module: execution.ModuleManager, TaskType: execution.TaskTypeRasterCOGGeneration, Source: execution.ModuleManager, TriggerType: execution.TriggerTypeManual, Status: execution.ExecutionStatusRunning, ExecutionBoundary: execution.ExecutionBoundaryBounded, Attempt: 1, LeaseToken: &tokenValue, LeaseOwner: &owner, LeaseExpiresAt: &expires, ExecutionConfig: commonmodels.JSONMap{artifactGenerationTokensKey: string(text)}}
			if err := db.Transaction(func(tx *gorm.DB) error {
				if err := HoldCleanupTargets(t.Context(), tx, tokens, id); err != nil {
					return err
				}
				return tx.Create(&item).Error
			}); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				for _, token := range tokens {
					key, _ := token.Key.digest()
					if err := db.Where("key = ?", key).Delete(&models.CleanupTarget{}).Error; err != nil {
						t.Error(err)
					}
				}
				if err := db.Where("id = ?", id).Delete(&models.CleanupArtifactOperation{}).Error; err != nil {
					t.Error(err)
				}
				if err := db.Where("execution_id = ?", id).Delete(&execution.TaskExecution{}).Error; err != nil {
					t.Error(err)
				}
			})
			lease := execution.Lease{ExecutionID: id, TenantID: 7, Attempt: 1, Token: tokenValue, Owner: owner}
			ctx := ArtifactWriterContext(execution.ContextWithLease(t.Context(), lease))
			if err := BeginArtifactWrite(ctx, db); err != nil {
				t.Fatal(err)
			}
			// Another instance holding the same lease cannot dispatch the original again.
			restarted := ArtifactWriterContext(execution.ContextWithLease(t.Context(), lease))
			if err := BeginArtifactWrite(restarted, db); !errors.Is(err, commonapi.ErrConflict) {
				t.Fatal("writer replay accepted", err)
			}
			if err := RecordArtifactWrite(ctx, db, outcome == "succeeded"); err != nil {
				t.Fatal(err)
			}
			if err := BeginArtifactWrite(ctx, db); !errors.Is(err, commonapi.ErrConflict) {
				t.Fatal("settled original ticket dispatched again", err)
			}
			if outcome == "unknown" {
				if err := db.Model(&execution.TaskExecution{}).Where("execution_id = ?", id).Updates(map[string]interface{}{"status": "failed", "lease_expires_at": time.Now().Add(-time.Minute)}).Error; err != nil {
					t.Fatal(err)
				}
				if _, err := ObserveCleanupTarget(t.Context(), db.Session(&gorm.Session{NewDB: true}), tokens[0].Key); !errors.Is(err, commonapi.ErrConflict) {
					t.Fatal("restart/terminal reopened unknown target", err)
				}
				if err := BeginArtifactWrite(ctx, db); !errors.Is(err, commonapi.ErrConflict) {
					t.Fatal("expired original ticket dispatched again", err)
				}
				return
			}
			injected := errors.New("private-result rollback")
			if err := db.Transaction(func(tx *gorm.DB) error {
				if err := finishArtifactGeneration(t.Context(), tx, item, "success"); err != nil {
					return err
				}
				return injected
			}); !errors.Is(err, injected) {
				t.Fatal(err)
			}
			if _, err := ObserveCleanupTarget(t.Context(), db, tokens[0].Key); !errors.Is(err, commonapi.ErrConflict) {
				t.Fatal("rollback leaked target release", err)
			}
			if err := db.Transaction(func(tx *gorm.DB) error { return finishArtifactGeneration(t.Context(), tx, item, "success") }); err != nil {
				t.Fatal(err)
			}
			if _, err := ObserveCleanupTarget(t.Context(), db, tokens[0].Key); err != nil {
				t.Fatal("known returned writer not released", err)
			}
		})
	}
	t.Run("prefix_registration_race", func(t *testing.T) {
		prefix := "tenant_7/pg-prefix/" + uuid.NewString()
		identities := []string{prefix, prefix + "/child.glb"}
		ids := []string{uuid.NewString(), uuid.NewString()}
		tokens := make([]CleanupTargetToken, 2)
		for i, object := range identities {
			value, _ := json.Marshal([]string{"manager", object})
			tokens[i] = CleanupTargetToken{Key: CleanupTargetKey{TenantID: 7, Kind: CleanupTargetObject, Identity: string(value)}}
		}
		t.Cleanup(func() {
			for _, token := range tokens {
				key, _ := token.Key.digest()
				if err := db.Where("key = ?", key).Delete(&models.CleanupTarget{}).Error; err != nil {
					t.Error(err)
				}
			}
		})
		start := make(chan struct{})
		results := make(chan error, 2)
		for i := range tokens {
			go func(i int) {
				<-start
				results <- db.WithContext(t.Context()).Transaction(func(tx *gorm.DB) error {
					return HoldCleanupTargets(t.Context(), tx, []CleanupTargetToken{tokens[i]}, ids[i])
				})
			}(i)
		}
		close(start)
		accepted, rejected := 0, 0
		for i := 0; i < 2; i++ {
			select {
			case err := <-results:
				if err == nil {
					accepted++
				} else if errors.Is(err, commonapi.ErrConflict) {
					rejected++
				} else {
					t.Fatal(err)
				}
			case <-time.After(10 * time.Second):
				t.Fatal("physical target lock order stalled")
			}
		}
		if accepted != 1 || rejected != 1 {
			t.Fatal("prefix/object overlap admitted", accepted, rejected)
		}
	})
}
