package repository

import (
	"context"
	"math"
	"strconv"
	"time"

	"github.com/addp/common/execution"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/ontology/internal/models"
	"github.com/addp/ontology/internal/semantic"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func canonicalGeneration(generation string) bool {
	id, err := uuid.Parse(generation)
	return err == nil && id != uuid.Nil && id.String() == generation
}

// insertProjection is the single creation path for publication and rebuild.
// The caller holds the ontology/revision locks and owns the transaction.
func insertProjection(ctx context.Context, tx *gorm.DB, actor models.Actor, p *models.Projection) error {
	item := &execution.TaskExecution{ExecutionID: p.ExecutionID, TenantID: int(p.TenantID), Module: models.Module,
		TaskType: models.ProjectionTaskType, Source: models.Module, Status: execution.ExecutionStatusPending,
		TriggerType: execution.TriggerTypeManual, ExecutionBoundary: execution.ExecutionBoundaryBounded, MaxAttempts: 1,
		ActorPrincipalID: &actor.PrincipalID, ActorTenantMembershipID: &actor.MembershipID, IssuedAuthorizationVersion: &actor.AuthorizationVersion,
		ExecutionConfig: commonmodels.JSONMap{"ontology_id": p.OntologyID, "revision": strconv.FormatUint(p.Revision, 10), "digest": p.Digest, "generation": p.Generation},
		Metadata:        commonmodels.JSONMap{}, ErrorDetails: commonmodels.JSONMap{}, CreatedAt: p.CreatedAt, UpdatedAt: p.CreatedAt}
	if err := execution.NewTaskExecutionRepository(tx).Create(ctx, item); err != nil {
		return err
	}
	return tx.Create(p).Error
}

// RebuildProjection never mutates the frozen revision or reuses an execution.
func (r *RevisionRepository) RebuildProjection(ctx context.Context, actor models.Actor, scope semantic.Scope, version uint64, failedGeneration string, baseline uint64) (*models.Projection, error) {
	if err := ValidateActor(actor, scope); err != nil {
		return nil, err
	}
	if version == 0 || version >= math.MaxInt64 || baseline == 0 || baseline >= math.MaxInt64 || !canonicalGeneration(failedGeneration) {
		return nil, ErrInvalid
	}
	var result *models.Projection
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		head, err := lockOntology(tx, scope)
		if err != nil {
			return err
		}
		if head.ActivationVersion != baseline {
			return ErrConflict
		}
		var revision models.Revision
		if err := scoped(tx.Clauses(clause.Locking{Strength: "UPDATE"}), scope).First(&revision).Error; err != nil {
			return err
		}
		if revision.Status != models.Published || revision.Version != version {
			return ErrConflict
		}
		snapshot, err := semantic.Restore([]byte(revision.Payload), revision.Digest)
		if err != nil || snapshot.Scope() != scope {
			return ErrIntegrity
		}
		var previous models.Projection
		if err := scoped(tx.Clauses(clause.Locking{Strength: "UPDATE"}), scope).Where("generation=?", failedGeneration).First(&previous).Error; err != nil {
			return err
		}
		if previous.Status != "failed" || previous.Digest != revision.Digest {
			return ErrConflict
		}
		var successors int64
		if err := tx.Model(&models.Projection{}).Where("predecessor_generation=?", failedGeneration).Count(&successors).Error; err != nil {
			return err
		}
		if successors != 0 {
			return ErrConflict
		}
		var old execution.TaskExecution
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("execution_id=? AND tenant_id=? AND module=? AND source=? AND task_type=?",
			previous.ExecutionID, actor.TenantID, models.Module, models.Module, models.ProjectionTaskType).First(&old).Error; err != nil {
			return err
		}
		if (old.Status != execution.ExecutionStatusFailed && old.Status != execution.ExecutionStatusCancelled && old.Status != execution.ExecutionStatusTimeout) ||
			old.LeaseToken != nil || old.LeaseOwner != nil || old.LeaseExpiresAt != nil {
			return ErrConflict
		}
		now := time.Now().UTC()
		result = &models.Projection{Generation: uuid.NewString(), ExecutionID: uuid.NewString(), PredecessorGeneration: &failedGeneration,
			TenantID: scope.TenantID, OntologyID: scope.OntologyID, Revision: scope.Revision, Digest: revision.Digest,
			BaselineVersion: baseline, Status: "pending", CreatedAt: now, UpdatedAt: now}
		if err := insertProjection(ctx, tx, actor, result); err != nil {
			return err
		}
		return tx.Create(&models.ProjectionEvent{Generation: result.Generation, Action: "rebuild_requested", Attempt: 0,
			ActorPrincipalID: actor.PrincipalID, ActorMembershipID: actor.MembershipID, AuthorizationVersion: actor.AuthorizationVersion, CreatedAt: now}).Error
	})
	if err != nil {
		return nil, normalizeError(err)
	}
	return result, nil
}

func cancelPendingProjections(tx *gorm.DB, actor models.Actor, record *models.Revision) error {
	var projections []models.Projection
	scope := semantic.Scope{TenantID: record.TenantID, OntologyID: record.OntologyID, Revision: record.Revision}
	if err := scoped(tx.Clauses(clause.Locking{Strength: "UPDATE"}), scope).Where("status='pending'").Order("generation").Find(&projections).Error; err != nil {
		return err
	}
	for _, p := range projections {
		if err := failPendingProjection(tx, actor, &p); err != nil {
			return err
		}
		if err := tx.Model(&execution.TaskExecution{}).Where("execution_id=? AND tenant_id=? AND module=? AND status=?",
			p.ExecutionID, actor.TenantID, models.Module, execution.ExecutionStatusPending).
			Updates(map[string]any{"status": execution.ExecutionStatusCancelled, "completed_at": gorm.Expr("clock_timestamp()"), "updated_at": gorm.Expr("clock_timestamp()")}).Error; err != nil {
			return err
		}
	}
	return nil
}
