package repository

import (
	"context"
	"errors"
	"math"
	"strconv"
	"time"

	"github.com/addp/common/execution"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/ontology/internal/models"
	"github.com/addp/ontology/internal/semantic"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrInvalid   = errors.New("invalid_ontology_command")
	ErrConflict  = errors.New("ontology_revision_conflict")
	ErrNotFound  = errors.New("ontology_revision_not_found")
	ErrIntegrity = errors.New("ontology_snapshot_integrity_error")
)

type RevisionRepository struct{ db *gorm.DB }

func NewRevisionRepository(db *gorm.DB) *RevisionRepository { return &RevisionRepository{db: db} }

func ValidateActor(actor models.Actor, scope semantic.Scope) error {
	if actor.TenantID == 0 || actor.TenantID > math.MaxInt64 || actor.TenantID != scope.TenantID ||
		actor.PrincipalID <= 0 || actor.MembershipID <= 0 || actor.AuthorizationVersion <= 0 ||
		scope.Revision == 0 || scope.Revision >= math.MaxInt64 || scope.OntologyID == "" || len(scope.OntologyID) > 64 {
		return ErrInvalid
	}
	return nil
}

func scoped(db *gorm.DB, s semantic.Scope) *gorm.DB {
	return db.Where("tenant_id = ? AND ontology_id = ? AND revision = ?", s.TenantID, s.OntologyID, s.Revision)
}

func normalizeError(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrNotFound
	}
	var pg *pgconn.PgError
	if errors.Is(err, gorm.ErrDuplicatedKey) || (errors.As(err, &pg) && pg.Code == "23505") {
		return ErrConflict
	}
	return err
}

func (r *RevisionRepository) Get(ctx context.Context, actor models.Actor, scope semantic.Scope) (*models.Revision, error) {
	if err := ValidateActor(actor, scope); err != nil {
		return nil, err
	}
	var record models.Revision
	err := scoped(r.db.WithContext(ctx), scope).First(&record).Error
	return &record, normalizeError(err)
}

func lockOntology(tx *gorm.DB, scope semantic.Scope) (*models.Ontology, error) {
	var head models.Ontology
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id = ? AND ontology_id = ?", scope.TenantID, scope.OntologyID).First(&head).Error
	return &head, normalizeError(err)
}

func addEvent(tx *gorm.DB, actor models.Actor, record *models.Revision, action, from string) error {
	return tx.Create(&models.RevisionEvent{TenantID: record.TenantID, OntologyID: record.OntologyID,
		Revision: record.Revision, Version: record.Version, Action: action, FromState: from, ToState: record.Status,
		Digest: record.Digest, ActorPrincipalID: actor.PrincipalID, ActorMembershipID: actor.MembershipID,
		AuthorizationVersion: actor.AuthorizationVersion, CreatedAt: time.Now().UTC()}).Error
}

func (r *RevisionRepository) Create(ctx context.Context, actor models.Actor, snapshot *semantic.Snapshot) (*models.Revision, error) {
	if snapshot == nil {
		return nil, ErrInvalid
	}
	scope := snapshot.Scope()
	if err := ValidateActor(actor, scope); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	record := &models.Revision{TenantID: scope.TenantID, OntologyID: scope.OntologyID, Revision: scope.Revision,
		Version: 1, Status: models.Draft, Payload: string(snapshot.CanonicalJSON()), Digest: snapshot.Digest(), CreatedAt: now, UpdatedAt: now}
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&models.Ontology{TenantID: scope.TenantID, OntologyID: scope.OntologyID}).Error; err != nil {
			return err
		}
		head, err := lockOntology(tx, scope)
		if err != nil {
			return err
		}
		if head.LastRevision+1 != scope.Revision {
			return ErrConflict
		}
		if head.LastRevision > 0 {
			var last models.Revision
			previous := scope
			previous.Revision = head.LastRevision
			if err := scoped(tx, previous).First(&last).Error; err != nil {
				return err
			}
			if last.Status != models.Published && last.Status != models.Withdrawn {
				return ErrConflict
			}
		}
		if err := tx.Create(record).Error; err != nil {
			return err
		}
		if err := tx.Model(head).Update("last_revision", scope.Revision).Error; err != nil {
			return err
		}
		return addEvent(tx, actor, record, "create", "")
	})
	if err != nil {
		return nil, normalizeError(err)
	}
	return record, nil
}

// Change serializes mutations by ontology, then revision, then execution.
// Compilation and all external authorization happen outside this transaction.
func (r *RevisionRepository) Change(ctx context.Context, actor models.Actor, scope semantic.Scope, version uint64, action string, snapshot *semantic.Snapshot) (*models.Revision, error) {
	if err := ValidateActor(actor, scope); err != nil {
		return nil, err
	}
	if version == 0 || version >= math.MaxInt64 {
		return nil, ErrInvalid
	}
	from, to := "", ""
	switch action {
	case "save":
		from, to = models.Draft, models.Draft
	case "submit":
		from, to = models.Draft, models.InReview
	case "return":
		from, to = models.InReview, models.Draft
	case "publish":
		from, to = models.InReview, models.Published
	case "withdraw":
		from, to = models.Published, models.Withdrawn
	default:
		return nil, ErrInvalid
	}
	if snapshot == nil || snapshot.Scope() != scope {
		return nil, ErrInvalid
	}
	var record models.Revision
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := lockOntology(tx, scope); err != nil {
			return err
		}
		if err := scoped(tx.Clauses(clause.Locking{Strength: "UPDATE"}), scope).First(&record).Error; err != nil {
			return err
		}
		if record.Version != version || record.Status != from {
			return ErrConflict
		}
		if action != "save" && (record.Digest != snapshot.Digest() || record.Payload != string(snapshot.CanonicalJSON())) {
			return ErrIntegrity
		}
		now := time.Now().UTC()
		fields := map[string]any{"version": version + 1, "status": to, "updated_at": now}
		if action == "save" {
			fields["payload"], fields["digest"] = string(snapshot.CanonicalJSON()), snapshot.Digest()
			record.Payload, record.Digest = fields["payload"].(string), snapshot.Digest()
		}
		if action == "publish" {
			executionID, generation := uuid.NewString(), uuid.NewString()
			item := &execution.TaskExecution{ExecutionID: executionID, TenantID: int(scope.TenantID), Module: models.Module,
				TaskType: models.ProjectionTaskType, Source: models.Module, Status: execution.ExecutionStatusPending,
				TriggerType: execution.TriggerTypeManual, ExecutionBoundary: execution.ExecutionBoundaryBounded, MaxAttempts: 3,
				ActorPrincipalID: &actor.PrincipalID, ActorTenantMembershipID: &actor.MembershipID, IssuedAuthorizationVersion: &actor.AuthorizationVersion,
				ExecutionConfig: commonmodels.JSONMap{"ontology_id": scope.OntologyID, "revision": strconv.FormatUint(scope.Revision, 10), "digest": record.Digest, "generation": generation},
				Metadata:        commonmodels.JSONMap{}, ErrorDetails: commonmodels.JSONMap{}, CreatedAt: now, UpdatedAt: now}
			if err := execution.NewTaskExecutionRepository(tx).Create(ctx, item); err != nil {
				return err
			}
			fields["build_execution_id"], fields["generation"], fields["published_at"] = executionID, generation, now
			record.BuildExecutionID, record.Generation, record.PublishedAt = &executionID, &generation, &now
		}
		if action == "withdraw" {
			// Cancellation of a pending intent is atomic with withdrawal. A
			// running worker must itself observe withdrawal before activation.
			if err := tx.Model(&execution.TaskExecution{}).
				Where("execution_id = ? AND tenant_id = ? AND module = ? AND task_type = ? AND status = ?", record.BuildExecutionID, scope.TenantID, models.Module, models.ProjectionTaskType, execution.ExecutionStatusPending).
				Updates(map[string]any{"status": execution.ExecutionStatusCancelled, "completed_at": now, "updated_at": now}).Error; err != nil {
				return err
			}
		}
		result := scoped(tx.Model(&models.Revision{}), scope).Where("version = ? AND status = ?", version, from).Updates(fields)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrConflict
		}
		record.Version, record.Status, record.UpdatedAt = version+1, to, now
		return addEvent(tx, actor, &record, action, from)
	})
	if err != nil {
		return nil, normalizeError(err)
	}
	return &record, nil
}
