package repository

import (
	"context"
	"math"
	"time"

	"github.com/addp/ontology/internal/models"
	"github.com/addp/ontology/internal/platform"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func validatePlatformPublisher(actor models.PlatformActor) error {
	if ValidatePlatformActor(actor) != nil || actor.PrincipalType != "service_principal" {
		return ErrInvalid
	}
	return nil
}

// BeginPlatformProjection rotates the publication fence, never the active
// pointer. Interrupted and uncertain writes are not replayed in-place.
// Every writer locks capability -> immutable revision -> projection -> audit.
func (r *PlatformRevisionRepository) BeginPlatformProjection(ctx context.Context, actor models.PlatformActor, snapshot *platform.Snapshot) (*models.PlatformProjection, error) {
	if validatePlatformPublisher(actor) != nil {
		return nil, ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	d, err := snapshot.Context()
	if err != nil {
		return nil, ErrInvalid
	}
	p := models.PlatformProjection{Generation: uuid.NewString(), Capability: d.Capability,
		Revision: d.Revision, Digest: snapshot.Digest(), Status: "building", CreatedAt: time.Now().UTC().Truncate(time.Microsecond)}
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var head models.PlatformCapability
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("capability = ?", p.Capability).First(&head).Error; err != nil {
			return err
		}
		if head.LastRevision != p.Revision || head.ActivationVersion == math.MaxInt64 {
			return ErrConflict
		}
		if err := validateStoredPlatformSnapshot(tx, snapshot, p); err != nil {
			return err
		}
		p.BaselineActivationVersion = head.ActivationVersion
		if head.PublishGeneration != nil {
			var previous models.PlatformProjection
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("generation = ? AND capability = ?", *head.PublishGeneration, p.Capability).First(&previous).Error; err != nil {
				return err
			}
			if previous.Status == "building" {
				if err := tx.Model(&previous).Update("status", "superseded").Error; err != nil {
					return err
				}
				if err := platformProjectionAudit(tx, actor, previous.Generation, "superseded"); err != nil {
					return err
				}
			}
		}
		if err := tx.Create(&p).Error; err != nil {
			return err
		}
		if err := tx.Model(&head).Update("publish_generation", p.Generation).Error; err != nil {
			return err
		}
		return platformProjectionAudit(tx, actor, p.Generation, "begin")
	})
	if err != nil {
		return nil, normalizeError(err)
	}
	return &p, nil
}

// FinishPlatformProjection requires the exact current generation and activation
// baseline. A ready result is admitted only by the publisher after fresh IAM and
// full graph verification; supplied actor facts alone are not an authorization.
func (r *PlatformRevisionRepository) FinishPlatformProjection(ctx context.Context, actor models.PlatformActor, snapshot *platform.Snapshot, attempt *models.PlatformProjection, status string) error {
	if validatePlatformPublisher(actor) != nil || attempt == nil || (status != "ready" && status != "failed") {
		return ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	d, err := snapshot.Context()
	id, idErr := uuid.Parse(attempt.Generation)
	if err != nil || idErr != nil || id == uuid.Nil || id.String() != attempt.Generation ||
		attempt.Capability != d.Capability || attempt.Revision != d.Revision || attempt.Digest != snapshot.Digest() ||
		attempt.Status != "building" || attempt.BaselineActivationVersion > math.MaxInt64 {
		return ErrInvalid
	}
	return normalizeError(r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var head models.PlatformCapability
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("capability = ?", attempt.Capability).First(&head).Error; err != nil {
			return err
		}
		if head.PublishGeneration == nil || *head.PublishGeneration != attempt.Generation || head.ActivationVersion != attempt.BaselineActivationVersion {
			return ErrConflict
		}
		if status == "ready" && (head.LastRevision != attempt.Revision || head.ActivationVersion == math.MaxInt64) {
			return ErrConflict
		}
		if err := validateStoredPlatformSnapshot(tx, snapshot, *attempt); err != nil {
			return err
		}
		var stored models.PlatformProjection
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("generation = ? AND capability = ?", attempt.Generation, attempt.Capability).First(&stored).Error; err != nil {
			return err
		}
		if stored.Status != "building" || stored.Revision != attempt.Revision || stored.Digest != attempt.Digest || stored.BaselineActivationVersion != attempt.BaselineActivationVersion {
			return ErrConflict
		}
		if err := tx.Model(&stored).Update("status", status).Error; err != nil {
			return err
		}
		if status == "ready" {
			if err := tx.Model(&head).Updates(map[string]any{"active_revision": attempt.Revision,
				"active_generation": attempt.Generation, "activation_version": head.ActivationVersion + 1}).Error; err != nil {
				return err
			}
		}
		return platformProjectionAudit(tx, actor, attempt.Generation, status)
	}))
}

func validateStoredPlatformSnapshot(tx *gorm.DB, snapshot *platform.Snapshot, p models.PlatformProjection) error {
	var record models.PlatformRevision
	if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Where("capability = ? AND revision = ?", p.Capability, p.Revision).First(&record).Error; err != nil {
		return err
	}
	if record.Digest != p.Digest || record.Payload != string(snapshot.CanonicalJSON()) {
		return ErrIntegrity
	}
	return nil
}

func platformProjectionAudit(tx *gorm.DB, actor models.PlatformActor, generation, action string) error {
	return tx.Create(&models.PlatformProjectionEvent{Generation: generation, Action: action,
		ActorPrincipalID: actor.PrincipalID, AuthorizationVersion: actor.AuthorizationVersion}).Error
}
