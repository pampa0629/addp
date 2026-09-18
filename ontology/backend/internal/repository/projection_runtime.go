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

type ProjectionWork struct {
	Execution  execution.TaskExecution
	Revision   models.Revision
	Projection models.Projection
}

func (w *ProjectionWork) Boundary() execution.InternalTaskScope {
	return execution.InternalTaskScope{TaskType: models.ProjectionTaskType, ResourceID: w.Revision.OntologyID,
		Revision: strconv.FormatUint(w.Revision.Revision, 10), Digest: w.Revision.Digest, Generation: w.Projection.Generation}
}

func databaseNow(tx *gorm.DB) (time.Time, error) {
	var now time.Time
	err := tx.Raw("SELECT clock_timestamp()").Scan(&now).Error
	return now, err
}

func (r *RevisionRepository) ClaimProjection(ctx context.Context, owner string, duration time.Duration) (*execution.Lease, error) {
	var lease *execution.Lease
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now, err := databaseNow(tx)
		if err != nil {
			return err
		}
		_, lease, err = execution.ClaimNext(ctx, tx.Where("EXISTS (SELECT 1 FROM ontology.projections p WHERE p.execution_id::text=common.task_executions.execution_id AND p.status='pending')"), execution.ClaimOptions{Module: models.Module, Source: models.Module,
			TaskType: models.ProjectionTaskType, WorkerID: owner, LeaseDuration: duration, Now: now, RequireAuthorization: true})
		return err
	})
	return lease, err
}

// Always lock head -> revision -> projection -> execution. The first execution
// read only discovers the owner; every fact is checked again after row locks.
func lockProjectionWork(tx *gorm.DB, lease execution.Lease, expired bool) (*ProjectionWork, *models.Ontology, error) {
	var item execution.TaskExecution
	queue := tx.Where("execution_id=? AND tenant_id=? AND module=? AND source=? AND task_type=? AND execution_boundary=?",
		lease.ExecutionID, lease.TenantID, models.Module, models.Module, models.ProjectionTaskType, execution.ExecutionBoundaryBounded)
	if err := queue.First(&item).Error; err != nil {
		return nil, nil, normalizeError(err)
	}
	values := make(map[string]string, 4)
	for k, v := range item.ExecutionConfig {
		s, ok := v.(string)
		if !ok {
			return nil, nil, ErrIntegrity
		}
		values[k] = s
	}
	boundary := execution.InternalTaskScope{TaskType: models.ProjectionTaskType, ResourceID: values["ontology_id"], Revision: values["revision"], Digest: values["digest"], Generation: values["generation"]}
	if len(values) != 4 || boundary.Validate(execution.AudienceOntology) != nil || item.ActorPrincipalID == nil || item.ActorTenantMembershipID == nil || item.IssuedAuthorizationVersion == nil {
		return nil, nil, ErrIntegrity
	}
	revision, _ := strconv.ParseUint(boundary.Revision, 10, 64)
	scope := semantic.Scope{TenantID: uint64(item.TenantID), OntologyID: boundary.ResourceID, Revision: revision}
	head, err := lockOntology(tx, scope)
	if err != nil {
		return nil, nil, err
	}
	w := &ProjectionWork{}
	if err := scoped(tx.Clauses(clause.Locking{Strength: "UPDATE"}), scope).First(&w.Revision).Error; err != nil {
		return nil, nil, normalizeError(err)
	}
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("generation=? AND tenant_id=? AND ontology_id=? AND revision=? AND execution_id=? AND digest=?",
		boundary.Generation, scope.TenantID, scope.OntologyID, scope.Revision, lease.ExecutionID, boundary.Digest).First(&w.Projection).Error; err != nil {
		return nil, nil, normalizeError(err)
	}
	owned := queue.Clauses(clause.Locking{Strength: "UPDATE"}).Where("status='running' AND attempt=? AND lease_token=? AND lease_owner=?", lease.Attempt, lease.Token, lease.Owner)
	if expired {
		owned = owned.Where("lease_expires_at < clock_timestamp()")
	} else {
		owned = owned.Where("lease_expires_at > clock_timestamp()")
	}
	if err := owned.First(&w.Execution).Error; err != nil {
		return nil, nil, normalizeError(err)
	}
	if w.Revision.Digest != boundary.Digest ||
		w.Execution.ActorPrincipalID == nil || w.Execution.ActorTenantMembershipID == nil || w.Execution.IssuedAuthorizationVersion == nil ||
		!sameExecutionIdentity(item, w.Execution) {
		return nil, nil, ErrIntegrity
	}
	return w, head, nil
}

func sameExecutionIdentity(a, b execution.TaskExecution) bool {
	return a.ExecutionConfig["ontology_id"] == b.ExecutionConfig["ontology_id"] && a.ExecutionConfig["revision"] == b.ExecutionConfig["revision"] &&
		a.ExecutionConfig["digest"] == b.ExecutionConfig["digest"] && a.ExecutionConfig["generation"] == b.ExecutionConfig["generation"] &&
		*a.ActorPrincipalID == *b.ActorPrincipalID && *a.ActorTenantMembershipID == *b.ActorTenantMembershipID && *a.IssuedAuthorizationVersion == *b.IssuedAuthorizationVersion
}

func projectionEvent(tx *gorm.DB, w *ProjectionWork, action string) error {
	return tx.Create(&models.ProjectionEvent{Generation: w.Projection.Generation, Action: action, Attempt: w.Execution.Attempt,
		ActorPrincipalID: *w.Execution.ActorPrincipalID, ActorMembershipID: *w.Execution.ActorTenantMembershipID, AuthorizationVersion: *w.Execution.IssuedAuthorizationVersion, CreatedAt: time.Now().UTC()}).Error
}

func (r *RevisionRepository) ProjectionWork(ctx context.Context, lease execution.Lease) (*ProjectionWork, error) {
	var work *ProjectionWork
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		work, _, err = lockProjectionWork(tx, lease, false)
		return err
	})
	return work, err
}

func validateReceipt(tx *gorm.DB, w *ProjectionWork, receipt *client.InternalTaskAccess) error {
	if receipt == nil || w.Execution.ExecutionAuthorizationID == nil || w.Execution.AuthorizationExpiresAt == nil ||
		receipt.AuthorizationID != strconv.FormatInt(*w.Execution.ExecutionAuthorizationID, 10) || receipt.ExecutionID != w.Execution.ExecutionID ||
		receipt.TenantID != strconv.Itoa(w.Execution.TenantID) || receipt.Audience != execution.AudienceOntology ||
		receipt.Attempt != w.Execution.Attempt || receipt.InternalTask != w.Boundary() || receipt.ExpiresAt.After(*w.Execution.AuthorizationExpiresAt) {
		return ErrInvalid
	}
	now, err := databaseNow(tx)
	if err != nil {
		return err
	}
	if !receipt.ExpiresAt.After(now) || w.Execution.LeaseExpiresAt == nil || !w.Execution.LeaseExpiresAt.After(now) {
		return ErrConflict
	}
	if w.Revision.Status != models.Published {
		return ErrConflict
	}
	return nil
}

func (r *RevisionRepository) BeginProjection(ctx context.Context, lease execution.Lease, receipt *client.InternalTaskAccess) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		w, head, err := lockProjectionWork(tx, lease, false)
		if err != nil {
			return err
		}
		if err := validateReceipt(tx, w, receipt); err != nil {
			return err
		}
		if w.Projection.Status != "pending" || head.ActivationVersion != w.Projection.BaselineVersion {
			return ErrConflict
		}
		if err := tx.Model(&w.Projection).Updates(map[string]any{"status": "building", "updated_at": gorm.Expr("clock_timestamp()")}).Error; err != nil {
			return err
		}
		return projectionEvent(tx, w, "building")
	})
}

func (r *RevisionRepository) ActivateProjection(ctx context.Context, lease execution.Lease, receipt *client.InternalTaskAccess) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		w, head, err := lockProjectionWork(tx, lease, false)
		if err != nil {
			return err
		}
		if err := validateReceipt(tx, w, receipt); err != nil {
			return err
		}
		if w.Projection.Status != "building" || head.ActivationVersion != w.Projection.BaselineVersion {
			return ErrConflict
		}
		if err := tx.Model(&w.Projection).Updates(map[string]any{"status": "ready", "updated_at": gorm.Expr("clock_timestamp()")}).Error; err != nil {
			return err
		}
		if err := tx.Model(head).Updates(map[string]any{"active_revision": w.Revision.Revision, "active_generation": w.Projection.Generation, "activation_version": head.ActivationVersion + 1}).Error; err != nil {
			return err
		}
		if err := projectionEvent(tx, w, "activated"); err != nil {
			return err
		}
		now, err := databaseNow(tx)
		if err != nil {
			return err
		}
		// This last write fences even a deadline that elapsed while waiting on
		// audit/trigger work; failure rolls back ready, pointer and audit together.
		return execution.CompleteWithLease(ctx, tx.Where("lease_expires_at > clock_timestamp() AND ?::timestamptz > clock_timestamp()", receipt.ExpiresAt), lease, execution.ExecutionStatusSuccess, now, nil)
	})
}

func (r *RevisionRepository) FailProjection(ctx context.Context, lease execution.Lease, expired bool) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		w, _, err := lockProjectionWork(tx, lease, expired)
		if err != nil {
			return err
		}
		if w.Projection.Status != "failed" {
			if w.Projection.Status != "pending" && w.Projection.Status != "building" {
				return ErrConflict
			}
			if err := tx.Model(&w.Projection).Updates(map[string]any{"status": "failed", "updated_at": gorm.Expr("clock_timestamp()")}).Error; err != nil {
				return err
			}
			if err := projectionEvent(tx, w, "failed"); err != nil {
				return err
			}
		}
		now, err := databaseNow(tx)
		if err != nil {
			return err
		}
		fields := map[string]any{"error_details": commonmodels.JSONMap{"code": "ontology_projection_failed"}}
		if expired {
			return execution.FailExpired(ctx, tx, lease, now, fields)
		}
		return execution.CompleteWithLease(ctx, tx.Where("lease_expires_at > clock_timestamp()"), lease, execution.ExecutionStatusFailed, now, fields)
	})
}

func (r *RevisionRepository) RenewProjection(ctx context.Context, lease execution.Lease, duration time.Duration) error {
	now, err := databaseNow(r.db.WithContext(ctx))
	if err != nil {
		return err
	}
	return execution.RenewLease(ctx, r.db.Where("lease_expires_at > clock_timestamp()"), lease, now.Add(duration))
}

func (r *RevisionRepository) ProjectionTerminal(ctx context.Context, lease execution.Lease) (bool, error) {
	return execution.AttemptIsTerminal(ctx, r.db, lease)
}

func (r *RevisionRepository) RecoverProjections(ctx context.Context) error {
	// Discovery takes no row lock. Each candidate is revalidated with the
	// normal owner lock order; no side effects are retried on lease expiry.
	var items []execution.TaskExecution
	if err := r.db.WithContext(ctx).Where("module=? AND source=? AND task_type=? AND status='running' AND lease_expires_at < clock_timestamp()", models.Module, models.Module, models.ProjectionTaskType).
		Where("EXISTS (SELECT 1 FROM ontology.projections p WHERE p.execution_id::text=common.task_executions.execution_id)").
		Order("lease_expires_at, id").Limit(16).Find(&items).Error; err != nil {
		return err
	}
	for _, item := range items {
		lease, err := execution.LeaseFromExecution(item)
		if err != nil {
			return err
		}
		if err := r.FailProjection(ctx, lease, true); err != nil {
			terminal, checkErr := r.ProjectionTerminal(ctx, lease)
			if checkErr != nil {
				return checkErr
			}
			if !terminal {
				return err
			}
		}
	}
	return nil
}

func failPendingProjection(tx *gorm.DB, actor models.Actor, record *models.Projection) error {
	var p models.Projection
	result := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("generation=? AND status='pending'", record.Generation).Find(&p)
	if result.Error != nil || result.RowsAffected == 0 {
		return result.Error
	}
	if err := tx.Model(&p).Updates(map[string]any{"status": "failed", "updated_at": gorm.Expr("clock_timestamp()")}).Error; err != nil {
		return err
	}
	return tx.Create(&models.ProjectionEvent{Generation: p.Generation, Action: "failed", Attempt: 0, ActorPrincipalID: actor.PrincipalID,
		ActorMembershipID: actor.MembershipID, AuthorizationVersion: actor.AuthorizationVersion, CreatedAt: time.Now().UTC()}).Error
}
