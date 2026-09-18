package repository

import (
	"context"
	"database/sql"
	"errors"

	"github.com/addp/ontology/internal/models"
	"github.com/addp/ontology/internal/semantic"
	"gorm.io/gorm"
)

var ErrNotActive = errors.New("ontology_not_active")

// ActiveDefinition reads the authority pointer and its exact published package
// in one snapshot. No remote calls or business-data reads run in this transaction.
func (r *RevisionRepository) ActiveDefinition(ctx context.Context, actor models.Actor, id string) (*models.Ontology, *models.Revision, error) {
	if ValidateActor(actor, semantic.Scope{TenantID: actor.TenantID, OntologyID: id, Revision: 1}) != nil {
		return nil, nil, ErrInvalid
	}
	var head models.Ontology
	var revision models.Revision
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("tenant_id = ? AND ontology_id = ?", actor.TenantID, id).First(&head).Error; err != nil {
			return err
		}
		if head.ActiveRevision == nil || head.ActiveGeneration == nil {
			return ErrNotActive
		}
		scope := semantic.Scope{TenantID: actor.TenantID, OntologyID: id, Revision: *head.ActiveRevision}
		if err := scoped(tx, scope).First(&revision).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrIntegrity
			}
			return err
		}
		var projection models.Projection
		if err := tx.Where("tenant_id = ? AND ontology_id = ? AND generation = ?", actor.TenantID, id, *head.ActiveGeneration).First(&projection).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrIntegrity
			}
			return err
		}
		if revision.Status != models.Published || projection.Status != "ready" || projection.Revision != revision.Revision || projection.Digest != revision.Digest {
			return ErrIntegrity
		}
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return &head, &revision, normalizeError(err)
}
