package repository

import (
	"context"
	"testing"
	"time"

	plugin "github.com/addp/common/engine/plugin"
	execution "github.com/addp/common/execution"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/transfer/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

const productionTestLocator = "addp://engine/2/path/outdoor/persons?type=table"

func productionExecutionFixture(t *testing.T, db *gorm.DB) (execution.TaskExecution, execution.Lease) {
	t.Helper()
	owner, token := "production-worker", uuid.NewString()
	expires := time.Now().UTC().Add(time.Hour)
	item := execution.TaskExecution{TenantID: 999999, ExecutionID: uuid.NewString(), Module: execution.ModuleTransfer, TaskType: execution.TaskTypeSync, Source: execution.ModuleTransfer, Status: execution.ExecutionStatusRunning, ExecutionBoundary: execution.ExecutionBoundaryBounded, Attempt: 1, LeaseOwner: &owner, LeaseToken: &token, LeaseExpiresAt: &expires, Metadata: commonmodels.JSONMap{"unrelated": "keep"}}
	item.TriggerType = execution.TriggerTypeManual
	if err := db.Create(&item).Error; err != nil {
		t.Fatal(err)
	}
	lease, err := execution.LeaseFromExecution(item)
	if err != nil {
		t.Fatal(err)
	}
	return item, lease
}

func readProductionRecord(t *testing.T, db *gorm.DB, id int64) models.TargetProduction {
	t.Helper()
	var item execution.TaskExecution
	if err := db.First(&item, id).Error; err != nil {
		t.Fatal(err)
	}
	if item.Metadata["unrelated"] != "keep" {
		t.Fatal("unrelated metadata lost")
	}
	record, err := models.DecodeTargetProduction(item.Metadata[models.TargetProductionMetadataKey])
	if err != nil {
		t.Fatal(err)
	}
	return record
}

func TestTargetProductionDurableLifecycle(t *testing.T) {
	db := newTaskRepositoryTestDB(t)
	item, lease := productionExecutionFixture(t, db)
	repo := NewTaskRepository(db)
	ctx := context.Background()
	if err := repo.RecordTargetCommit(ctx, lease, productionTestLocator, nil); err == nil {
		t.Fatal("commit without intent accepted")
	}
	if err := repo.BeginTargetProduction(ctx, lease, productionTestLocator); err != nil {
		t.Fatal(err)
	}
	first := readProductionRecord(t, db, item.ID)
	if first.Phase != models.TargetProductionPending || first.Preparation != "" {
		t.Fatal("pending inferred creation")
	}
	if err := repo.BeginTargetProduction(ctx, lease, productionTestLocator); err == nil {
		t.Fatal("pending receipt reset to allow replay")
	}
	if err := repo.RecordTargetCommit(ctx, lease, productionTestLocator, nil); err == nil {
		t.Fatal("pending receipt promoted directly to commit")
	}
	if err := repo.RecordTargetPreparation(ctx, lease, productionTestLocator, plugin.TableTargetPreparation{}); err == nil {
		t.Fatal("unknown preparation accepted")
	}
	prepared := plugin.TableTargetPreparation{Disposition: plugin.TableTargetRecreated}
	if err := repo.RecordTargetPreparation(ctx, lease, productionTestLocator, prepared); err != nil {
		t.Fatal(err)
	}
	before := readProductionRecord(t, db, item.ID)
	if before.Phase != models.TargetProductionPrepared || !before.StartedAt.Equal(first.StartedAt) || before.CommittedAt != nil {
		t.Fatal("preparation changed intent or claimed commit")
	}
	if err := repo.RecordTargetPreparation(ctx, lease, productionTestLocator, plugin.TableTargetPreparation{Disposition: plugin.TableTargetCreated}); err == nil {
		t.Fatal("rebuild rewritten as creation")
	}
	if err := repo.RecordTargetCommit(ctx, lease, "addp://engine/2/path/outdoor/other?type=table", nil); err == nil {
		t.Fatal("target rebound")
	}
	if err := repo.RecordTargetCommit(ctx, lease, productionTestLocator, nil); err != nil {
		t.Fatal(err)
	}
	committed := readProductionRecord(t, db, item.ID)
	if err := repo.RecordTargetCommit(ctx, lease, productionTestLocator, nil); err != nil {
		t.Fatal(err)
	}
	if err := repo.RecordTargetPreparation(ctx, lease, productionTestLocator, prepared); err != nil {
		t.Fatal(err)
	}
	after := readProductionRecord(t, db, item.ID)
	if after.Phase != models.TargetProductionCommitted || after.Preparation != plugin.TableTargetRecreated || !after.CommittedAt.Equal(*committed.CommittedAt) || !after.PreparedAt.Equal(*before.PreparedAt) {
		t.Fatal("duplicate receipt changed original facts/times")
	}
	sources, _ := plugin.NewQueryReadSet(plugin.TabularItemPath(3, "schema", "outdoor", "source"))
	if err := repo.RecordTargetCommit(ctx, lease, productionTestLocator, sources); err == nil {
		t.Fatal("unavailable historical source evidence was backfilled")
	}
}

func TestTargetProductionFreezesActualReadSetWithCommit(t *testing.T) {
	db := newTaskRepositoryTestDB(t)
	item, lease := productionExecutionFixture(t, db)
	repo := NewTaskRepository(db)
	ctx := t.Context()
	if err := repo.BeginTargetProduction(ctx, lease, productionTestLocator); err != nil {
		t.Fatal(err)
	}
	if err := repo.RecordTargetPreparation(ctx, lease, productionTestLocator, plugin.TableTargetPreparation{Disposition: plugin.TableTargetCreated}); err != nil {
		t.Fatal(err)
	}
	const largeEngine = uint(9007199254740993)
	sources, _ := plugin.NewQueryReadSet(plugin.TabularItemPath(largeEngine, "schema", "outdoor", "source"))
	if err := repo.RecordTargetCommit(ctx, lease, productionTestLocator, sources); err != nil {
		t.Fatal(err)
	}
	frozen := readProductionRecord(t, db, item.ID)
	if frozen.SourceReadSet == nil || frozen.SourceReadSet.Paths[0].Segments[2].Name != "source" || frozen.SourceReadSet.Paths[0].EngineID != largeEngine {
		t.Fatal("actual sources not persisted with commit")
	}
	if err := repo.RecordTargetCommit(ctx, lease, productionTestLocator, sources); err != nil {
		t.Fatal("same source evidence retry failed", err)
	}
	sources.Paths[0].Segments[2].Name = "other"
	for _, changed := range []*plugin.QueryReadSet{nil, sources, {Paths: []plugin.EngineCatalogPath{sources.Paths[0], sources.Paths[0]}}} {
		if err := repo.RecordTargetCommit(ctx, lease, productionTestLocator, changed); err == nil {
			t.Fatal("committed sources changed, cleared or normalized on retry")
		}
	}
	if again := readProductionRecord(t, db, item.ID); again.SourceReadSet.Paths[0].Segments[2].Name != "source" || !again.CommittedAt.Equal(*frozen.CommittedAt) {
		t.Fatal("source proof or first commit time was rewritten")
	}
}

func TestTargetProductionRejectsStaleIdentity(t *testing.T) {
	for _, change := range []string{"tenant", "owner", "token", "attempt", "expired", "terminal", "malformed"} {
		t.Run(change, func(t *testing.T) {
			db := newTaskRepositoryTestDB(t)
			item, lease := productionExecutionFixture(t, db)
			repo := NewTaskRepository(db)
			if err := repo.BeginTargetProduction(t.Context(), lease, productionTestLocator); err != nil {
				t.Fatal(err)
			}
			switch change {
			case "tenant":
				lease.TenantID++
			case "owner":
				lease.Owner = "other-worker"
			case "token":
				lease.Token = uuid.NewString()
			case "attempt":
				lease.Attempt++
			case "expired":
				if err := db.Model(&item).Update("lease_expires_at", time.Now().Add(-time.Hour)).Error; err != nil {
					t.Fatal(err)
				}
			case "terminal":
				if err := db.Model(&item).Update("status", execution.ExecutionStatusFailed).Error; err != nil {
					t.Fatal(err)
				}
			case "malformed":
				if err := db.Model(&item).Update("metadata", commonmodels.JSONMap{models.TargetProductionMetadataKey: map[string]interface{}{"phase": "committed"}, "unrelated": "keep"}).Error; err != nil {
					t.Fatal(err)
				}
			}
			if err := repo.RecordTargetPreparation(t.Context(), lease, productionTestLocator, plugin.TableTargetPreparation{Disposition: plugin.TableTargetCreated}); err == nil {
				t.Fatal("stale/malformed receipt accepted")
			}
			if change != "malformed" && readProductionRecord(t, db, item.ID).Phase != models.TargetProductionPending {
				t.Fatal("rejected receipt changed phase")
			}
		})
	}
}
