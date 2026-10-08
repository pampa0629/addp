package repository

import (
	"context"
	"math"
	"time"

	"github.com/addp/ontology/internal/models"
	"github.com/addp/ontology/internal/platform"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type PlatformRevisionRepository struct{ db *gorm.DB }

func NewPlatformRevisionRepository(db *gorm.DB) *PlatformRevisionRepository {
	return &PlatformRevisionRepository{db: db}
}

func ValidatePlatformActor(actor models.PlatformActor) error {
	if actor.ContextType != "platform" || actor.PrincipalID <= 0 || actor.AuthorizationVersion <= 0 ||
		(actor.PrincipalType != "user" && actor.PrincipalType != "service_principal") {
		return ErrInvalid
	}
	return nil
}

// Store records a code release once. Authorization belongs to the caller's
// platform boundary, not to these supplied audit facts. No graph/execution work
// is admitted here. Lock order: capability head -> revision -> audit.
func (r *PlatformRevisionRepository) Store(ctx context.Context, actor models.PlatformActor, snapshot *platform.Snapshot, expectedLastRevision uint64) (*models.PlatformRevision, error) {
	if err := ValidatePlatformActor(actor); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	definition, err := snapshot.Context()
	if err != nil || expectedLastRevision > math.MaxInt64 {
		return nil, ErrInvalid
	}
	if definition.Revision <= expectedLastRevision {
		return nil, ErrConflict
	}
	record := models.PlatformRevision{Capability: definition.Capability, Revision: definition.Revision,
		Payload: string(snapshot.CanonicalJSON()), Digest: snapshot.Digest(), CreatedAt: time.Now().UTC().Truncate(time.Microsecond)}
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		head := models.PlatformCapability{Capability: record.Capability}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&head).Error; err != nil {
			return err
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("capability = ?", record.Capability).First(&head).Error; err != nil {
			return err
		}
		if head.LastRevision != expectedLastRevision {
			return ErrConflict
		}
		if err := tx.Create(&record).Error; err != nil {
			return err
		}
		if err := tx.Model(&head).Update("last_revision", record.Revision).Error; err != nil {
			return err
		}
		return tx.Create(&models.PlatformRevisionEvent{Capability: record.Capability, Revision: record.Revision,
			Action: "record", Digest: record.Digest, ActorPrincipalID: actor.PrincipalID,
			ActorPrincipalType: actor.PrincipalType, AuthorizationVersion: actor.AuthorizationVersion,
			CreatedAt: record.CreatedAt}).Error
	})
	if err != nil {
		return nil, normalizeError(err)
	}
	return &record, nil
}

func (r *PlatformRevisionRepository) Get(ctx context.Context, actor models.PlatformActor, capability string, revision uint64) (*models.PlatformRevision, error) {
	if err := ValidatePlatformActor(actor); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if platform.ValidateIdentity(capability, revision) != nil {
		return nil, ErrInvalid
	}
	var record models.PlatformRevision
	if err := r.db.WithContext(ctx).Where("capability = ? AND revision = ?", capability, revision).First(&record).Error; err != nil {
		return nil, normalizeError(err)
	}
	return &record, nil
}

func (r *PlatformRevisionRepository) Head(ctx context.Context, actor models.PlatformActor, capability string) (*models.PlatformCapability, error) {
	if err := ValidatePlatformActor(actor); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if platform.ValidateIdentity(capability, 1) != nil {
		return nil, ErrInvalid
	}
	var head models.PlatformCapability
	if err := r.db.WithContext(ctx).Where("capability = ?", capability).First(&head).Error; err != nil {
		return nil, normalizeError(err)
	}
	return &head, nil
}
