package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"sort"

	"github.com/addp/common/cleanup"
	"github.com/addp/common/events"
	"github.com/addp/common/execution"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/repository"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type managerCleanupSnapshot struct {
	Plan            *managerCleanupPlan
	Artifacts       []CleanupArtifactTarget
	PreviewTokens   []repository.CleanupTargetToken
	EmbeddingTokens []repository.CleanupTargetToken
}

func (s *CleanupService) freezeCleanupPlan(ctx context.Context, tenant uint, scope map[string]interface{}) (*managerCleanupSnapshot, error) {
	p, err := s.buildCleanupPlan(ctx, tenant, scope)
	if err != nil {
		return nil, err
	}
	sort.Slice(p.PreviewStates, func(i, j int) bool { return p.PreviewStates[i].ID < p.PreviewStates[j].ID })
	sort.Slice(p.Embeddings, func(i, j int) bool { return p.Embeddings[i].ID < p.Embeddings[j].ID })
	sort.Slice(p.Tiles, func(i, j int) bool { return p.Tiles[i].ID < p.Tiles[j].ID })
	sort.Slice(p.Vectors, func(i, j int) bool { return p.Vectors[i].ID < p.Vectors[j].ID })
	sort.Slice(p.Tasks, func(i, j int) bool {
		if p.Tasks[i].TaskType == p.Tasks[j].TaskType {
			return p.Tasks[i].ID < p.Tasks[j].ID
		}
		return p.Tasks[i].TaskType < p.Tasks[j].TaskType
	})
	sort.Slice(p.Artifacts, func(i, j int) bool {
		if p.Artifacts[i].TaskType == p.Artifacts[j].TaskType {
			return p.Artifacts[i].ID < p.Artifacts[j].ID
		}
		return p.Artifacts[i].TaskType < p.Artifacts[j].TaskType
	})
	snap := &managerCleanupSnapshot{Plan: p}
	if len(p.Tiles)+len(p.Vectors)+len(p.Artifacts) > 0 {
		if s.artifacts == nil {
			return nil, errors.New("Manager artifact cleanup unavailable")
		}
		snap.Artifacts, err = s.artifacts.Freeze(ctx, tenant, p.Tiles, p.Vectors, p.Artifacts)
		if err != nil {
			return nil, err
		}
	}
	observe := func(kind, identity string) (repository.CleanupTargetToken, error) {
		return repository.ObserveCleanupTarget(ctx, s.db, repository.CleanupTargetKey{TenantID: tenant, Kind: kind, Identity: identity})
	}
	for _, v := range p.PreviewStates {
		token, e := observe(repository.CleanupTargetPreviewState, v.ItemFingerprint)
		if e != nil {
			return nil, e
		}
		snap.PreviewTokens = append(snap.PreviewTokens, token)
	}
	for _, v := range p.Embeddings {
		token, e := observe(repository.CleanupTargetEmbedding, v.ItemFingerprint)
		if e != nil {
			return nil, e
		}
		snap.EmbeddingTokens = append(snap.EmbeddingTokens, token)
	}
	return snap, nil
}
func (snap *managerCleanupSnapshot) digest() (string, error) {
	b, e := json.Marshal(snap)
	if e != nil {
		return "", e
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:]), nil
}

// All authority HTTP and inventory reads finish before a PG effect transaction.
// The assessment binds the exact Owner domain snapshot to the accepted scan.
func (s *CleanupService) runCleanupExecution(ctx context.Context, run *cleanup.Run) error {
	if run.Input.Context.TenantID != 0 {
		return errors.New("historical Tenant cleanup unsupported")
	}
	snap, err := s.freezeCleanupPlan(ctx, run.Input.TenantID, run.Input.Context.Map())
	if err != nil {
		return err
	}
	digest, err := snap.digest()
	if err != nil {
		return err
	}
	var index *models.CleanupIndexAssessment
	var inventory *ContentIndexInventory
	if run.Input.Action == events.CleanupActionScan {
		index, inventory, err = s.assessCleanupIndex(ctx, run.Input)
	} else {
		index, inventory, err = s.prepareCleanupIndex(ctx, run)
		if err == nil {
			var saved models.CleanupIndexAssessment
			err = s.db.WithContext(ctx).Where("request_id=? AND tenant_id=? AND requested_by=?", run.Input.BasedOnScan, run.Input.TenantID, run.Input.RequestedBy).First(&saved).Error
			if err == nil && (saved.DomainDigest == "" || saved.DomainDigest != digest) {
				err = errors.New("Manager cleanup domain changed since scan")
			}
		}
	}
	if err != nil {
		return err
	}
	index.DomainDigest = digest
	items := append(snap.Plan.impactItems(), cleanupIndexImpact(index)...)
	impact, err := events.BuildCleanupImpactData(items, "/manager")
	if err != nil {
		return err
	}
	finish := func(tx *gorm.DB, stats *ManagerCleanupStats) error {
		summary := managerScanSummary(stats)
		if run.Input.Action == events.CleanupActionExecute {
			summary = managerExecuteSummary(stats)
		}
		result := events.CleanupResultData{TaskID: run.Input.RequestID, TenantID: run.Input.TenantID, Module: events.ModuleManager, Action: run.Input.Action, CleanupMode: run.Input.CleanupMode, Status: events.CleanupResultSuccess, Summary: summary, Impact: &impact}
		receipt, e := execution.NewCleanupReceipt(run.Execution, result, "")
		if e != nil {
			return e
		}
		counters := managerCleanupCounters(stats)
		if run.Input.Action == events.CleanupActionScan {
			counters["estimated_reclaimable_bytes"] = counters["freed_bytes"]
			delete(counters, "freed_bytes")
		}
		return cleanup.Complete(ctx, tx, run.Lease, receipt, counters)
	}
	if run.Input.Action == events.CleanupActionScan {
		return run.Effect(ctx, func(tx *gorm.DB) error {
			if err := s.verifyCleanupPlain(ctx, tx, snap); err != nil {
				return err
			}
			if err := s.search.deliveries.VerifyInventoryGuard(ctx, tx, inventory.Guard); err != nil {
				return err
			}
			if err := tx.Create(index).Error; err != nil {
				return err
			}
			stats := snap.Plan.statistics()
			stats.IndexDocuments = index.CandidateCount
			return finish(tx, stats)
		})
	}
	if impact.Summary.Running > 0 {
		return errors.New("Manager cleanup targets still running")
	}
	// Register every artifact before its effect; a failed/unknown operation retains
	// its durable target hold. There is no automatic retry or compensating release.
	operations := make([]string, 0, len(snap.Artifacts))
	for _, target := range snap.Artifacts {
		var id string
		callCtx, cancel, e := run.PrepareExternal(ctx, func(tx *gorm.DB) error {
			var x error
			id, x = s.artifacts.Prepare(ctx, tx, run.Lease, target, run.Input.CleanupMode, run.Input.Context.ExternalArtifactPolicy)
			return x
		})
		if e != nil {
			return e
		}
		e = s.artifacts.Effect(callCtx, id)
		cancel()
		if e != nil {
			return e
		}
		operations = append(operations, id)
	}
	indexOperation, err := s.executeCleanupIndex(ctx, run, index, inventory)
	if err != nil {
		return err
	}
	return run.Effect(ctx, func(tx *gorm.DB) error {
		stats, err := s.finalizeCleanupPlain(ctx, tx, run.Lease, snap, run.Input.CleanupMode, run.Input.Context.EngineID != 0)
		if err != nil {
			return err
		}
		for _, id := range operations {
			part, e := s.artifacts.Finalize(ctx, tx, run.Lease, id)
			if e != nil {
				return e
			}
			addManagerCleanupStats(stats, part)
		}
		if indexOperation != nil {
			if err := s.search.deliveries.CompleteCleanupIndex(ctx, tx, run.Lease, indexOperation.ID); err != nil {
				return err
			}
			stats.IndexDocuments = index.CandidateCount
		}
		return finish(tx, stats)
	})
}

func verifyCleanupRow(ctx context.Context, tx *gorm.DB, expected interface{}, current interface{}, id, tenant uint) error {
	if err := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=? AND tenant_id=?", id, tenant).First(current).Error; err != nil {
		return err
	}
	a, e := json.Marshal(expected)
	if e != nil {
		return e
	}
	b, e := json.Marshal(current)
	if e != nil {
		return e
	}
	if string(a) != string(b) {
		return errors.New("Manager cleanup target changed")
	}
	return nil
}
func (s *CleanupService) verifyCleanupPlain(ctx context.Context, tx *gorm.DB, snap *managerCleanupSnapshot) error {
	for _, v := range snap.Plan.PreviewStates {
		if err := verifyCleanupRow(ctx, tx, v, &models.PreviewState{}, v.ID, v.TenantID); err != nil {
			return err
		}
	}
	for _, v := range snap.Plan.Embeddings {
		if err := verifyCleanupRow(ctx, tx, v, &models.Embedding{}, v.ID, v.TenantID); err != nil {
			return err
		}
	}
	for _, v := range snap.Plan.Tasks {
		if err := s.taskDefinitionRepo.VerifyFrozen(ctx, tx, v); err != nil {
			return err
		}
	}
	return nil
}
func (s *CleanupService) finalizeCleanupPlain(ctx context.Context, tx *gorm.DB, lease execution.Lease, snap *managerCleanupSnapshot, mode string, engineDeletion bool) (*ManagerCleanupStats, error) {
	stats := &ManagerCleanupStats{}
	// Same target-before-domain order as ordinary writes prevents cross-owner
	// lock inversion and serializes a Cleanup with the original observed token.
	for _, token := range append(append([]repository.CleanupTargetToken{}, snap.PreviewTokens...), snap.EmbeddingTokens...) {
		if err := repository.FenceCleanupTarget(ctx, tx, lease, token); err != nil {
			return nil, err
		}
	}
	if err := s.verifyCleanupPlain(ctx, tx, snap); err != nil {
		return nil, err
	}
	for _, v := range snap.Plan.PreviewStates {
		if err := tx.WithContext(ctx).Delete(v).Error; err != nil {
			return nil, err
		}
		stats.PreviewStates++
		stats.MarkedMissingSource++
	}
	for _, v := range snap.Plan.Embeddings {
		if mode == events.CleanupModePhysical {
			if err := tx.WithContext(ctx).Delete(v).Error; err != nil {
				return nil, err
			}
			stats.DeletedPhysicalArtifacts++
		} else {
			if err := tx.WithContext(ctx).Model(v).Updates(map[string]interface{}{"status": models.EmbeddingStatusMissingSource, "status_reason": models.EmbeddingReasonSourceMissing, "error_message": "", "embedding": nil}).Error; err != nil {
				return nil, err
			}
		}
		stats.Embeddings++
		stats.MarkedMissingSource++
	}
	for _, v := range snap.Plan.Tasks {
		physical := mode == events.CleanupModePhysical && !engineDeletion
		if err := s.taskDefinitionRepo.ApplyFrozen(ctx, tx, v, physical); err != nil {
			return nil, err
		}
		stats.TaskDefinitions++
		if physical {
			stats.DeletedTaskDefinitions++
		} else if v.Enabled {
			stats.DisabledTaskDefinitions++
		}
	}
	return stats, nil
}
func addManagerCleanupStats(to *ManagerCleanupStats, from ManagerCleanupStats) {
	a, b := reflect.ValueOf(to).Elem(), reflect.ValueOf(from)
	for i := 0; i < a.NumField(); i++ {
		if a.Field(i).Kind() == reflect.Int || a.Field(i).Kind() == reflect.Int64 {
			a.Field(i).SetInt(a.Field(i).Int() + b.Field(i).Int())
		}
	}
}
func managerCleanupCounters(s *ManagerCleanupStats) map[string]int64 {
	return map[string]int64{"preview_states": int64(s.PreviewStates), "vector_tile_caches": int64(s.TileCaches), "embeddings": int64(s.Embeddings), "vector_materialized_view_generations": int64(s.VectorMaterializedViews), "managed_quick_view_artifacts": int64(s.ManagedArtifacts), "index_documents": int64(s.IndexDocuments), "task_definitions": int64(s.TaskDefinitions), "disabled_task_definitions": int64(s.DisabledTaskDefinitions), "deleted_task_definitions": int64(s.DeletedTaskDefinitions), "deleted_physical_artifacts": int64(s.DeletedPhysicalArtifacts), "freed_bytes": s.FreedBytes, "marked_missing_source": int64(s.MarkedMissingSource), "skipped_external_targets": int64(s.SkippedExternalTargets), "abandoned_external": int64(s.AbandonedExternal)}
}
