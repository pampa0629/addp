package repository

import (
	"context"
	"strconv"
	"time"

	"github.com/addp/common/client"
	"github.com/addp/common/execution"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/ontology/internal/models"
	"github.com/addp/ontology/internal/semantic"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ProjectionIntent binds one exact generation to its current request actor.
// It is not an authorization and never selects an implicit latest generation.
func (r *RevisionRepository) ProjectionIntent(ctx context.Context, actor models.Actor, scope semantic.Scope, version uint64, generation string) (*models.Projection, error) {
	if !canonicalGeneration(generation) {
		return nil, ErrInvalid
	}
	record, err := r.Get(ctx, actor, scope)
	if err != nil {
		return nil, err
	}
	if record.Status != models.Published || record.Version != version {
		return nil, ErrConflict
	}
	var projection models.Projection
	if err := scoped(r.db.WithContext(ctx), scope).Where("generation=?", generation).First(&projection).Error; err != nil {
		return nil, normalizeError(err)
	}
	if projection.Status != "pending" || projection.Digest != record.Digest {
		return nil, ErrConflict
	}
	var count int64
	if err := pendingProjection(r.db.WithContext(ctx), actor, &projection).Count(&count).Error; err != nil {
		return nil, err
	}
	if count != 1 {
		return nil, ErrConflict
	}
	return &projection, nil
}

func pendingProjection(db *gorm.DB, actor models.Actor, record *models.Projection) *gorm.DB {
	return db.Model(&execution.TaskExecution{}).
		Where("execution_id = ? AND tenant_id = ? AND module = ? AND source = ? AND task_type = ? AND execution_boundary = ?",
			record.ExecutionID, actor.TenantID, models.Module, models.Module, models.ProjectionTaskType, execution.ExecutionBoundaryBounded).
		Where("status = ? AND execution_authorization_id IS NULL", execution.ExecutionStatusPending).
		Where("actor_principal_id = ? AND actor_tenant_membership_id = ? AND issued_authorization_version = ?", actor.PrincipalID, actor.MembershipID, actor.AuthorizationVersion).
		Where("execution_config = jsonb_build_object('ontology_id',?::text,'revision',?::text,'digest',?::text,'generation',?::text)",
			record.OntologyID, strconv.FormatUint(record.Revision, 10), record.Digest, record.Generation)
}

func (r *RevisionRepository) AttachProjectionAuthorization(ctx context.Context, actor models.Actor, scope semantic.Scope, version uint64, generation string, issued *client.IssuedExecutionAuthorization) error {
	if err := ValidateActor(actor, scope); err != nil {
		return err
	}
	if !canonicalGeneration(generation) {
		return ErrInvalid
	}
	fields, err := client.TaskExecutionAuthorizationFields(issued)
	if err != nil {
		return ErrInvalid
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := lockOntology(tx, scope); err != nil {
			return err
		}
		var record models.Revision
		if err := scoped(tx.Clauses(clause.Locking{Strength: "UPDATE"}), scope).First(&record).Error; err != nil {
			return normalizeError(err)
		}
		if record.Status != models.Published || record.Version != version {
			return ErrConflict
		}
		var projection models.Projection
		if err := scoped(tx.Clauses(clause.Locking{Strength: "UPDATE"}), scope).Where("generation=?", generation).First(&projection).Error; err != nil {
			return normalizeError(err)
		}
		if projection.Status != "pending" || projection.Digest != record.Digest {
			return ErrConflict
		}
		expected := execution.InternalTaskScope{TaskType: models.ProjectionTaskType, ResourceID: scope.OntologyID, Revision: strconv.FormatUint(scope.Revision, 10), Digest: record.Digest, Generation: projection.Generation}
		if issued.ExecutionID != projection.ExecutionID || issued.Audience != execution.AudienceOntology || issued.InternalTask == nil || *issued.InternalTask != expected ||
			len(issued.Accesses) != 0 || issued.SourceType != "user" || issued.SourceDefinitionID != nil || issued.SourceDefinitionVersion != nil ||
			issued.TenantID != strconv.FormatUint(actor.TenantID, 10) || issued.ActorPrincipalID != strconv.FormatInt(actor.PrincipalID, 10) ||
			issued.TenantMembershipID != strconv.FormatInt(actor.MembershipID, 10) || issued.IssuedAuthorizationVersion != strconv.FormatInt(actor.AuthorizationVersion, 10) {
			return ErrInvalid
		}
		result := pendingProjection(tx, actor, &projection).Where("?::timestamptz > clock_timestamp()", issued.ExpiresAt).Updates(fields)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrConflict
		}
		return nil
	})
}

// FailProjectionAdmission closes only this still-unadmitted intent. A late
// failed request cannot cancel a concurrently authorized or running execution.
func (r *RevisionRepository) FailProjectionAdmission(ctx context.Context, actor models.Actor, record *models.Projection) error {
	if record == nil {
		return ErrInvalid
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := lockOntology(tx, semantic.Scope{TenantID: record.TenantID, OntologyID: record.OntologyID, Revision: record.Revision}); err != nil {
			return err
		}
		var count int64
		if err := pendingProjection(tx, actor, record).Count(&count).Error; err != nil {
			return err
		}
		if count == 0 {
			return nil
		}
		if err := failPendingProjection(tx, actor, record); err != nil {
			return err
		}
		return pendingProjection(tx, actor, record).Updates(map[string]any{
			"status": execution.ExecutionStatusFailed, "completed_at": time.Now().UTC(), "updated_at": time.Now().UTC(),
			"error_details": commonmodels.JSONMap{"code": "ontology_projection_admission_failed"},
		}).Error
	})
}
