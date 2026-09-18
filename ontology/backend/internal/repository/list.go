package repository

import (
	"context"
	"database/sql"

	"github.com/addp/ontology/internal/models"
	"github.com/addp/ontology/internal/semantic"
	"gorm.io/gorm"
)

func (r *RevisionRepository) ListOntologies(ctx context.Context, actor models.Actor, page models.ListPage) ([]models.Ontology, int64, error) {
	if validateActorIdentity(actor) != nil || !page.Valid() {
		return nil, 0, ErrInvalid
	}
	rows := make([]models.Ontology, 0)
	var total int64
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		query := tx.Model(&models.Ontology{}).Where("tenant_id = ?", actor.TenantID)
		if err := query.Count(&total).Error; err != nil {
			return err
		}
		return query.Order("ontology_id ASC").Limit(page.PageSize).Offset(page.Offset()).Find(&rows).Error
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return rows, total, normalizeError(err)
}

func (r *RevisionRepository) ListRevisions(ctx context.Context, actor models.Actor, ontologyID string, page models.ListPage) ([]models.RevisionSummary, int64, error) {
	if ValidateActor(actor, semantic.Scope{TenantID: actor.TenantID, OntologyID: ontologyID, Revision: 1}) != nil || !page.Valid() {
		return nil, 0, ErrInvalid
	}
	rows := make([]models.RevisionSummary, 0)
	var total int64
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Parent existence and summaries use the same tenant and read snapshot.
		var head models.Ontology
		if err := tx.Select("ontology_id").Where("tenant_id = ? AND ontology_id = ?", actor.TenantID, ontologyID).First(&head).Error; err != nil {
			return err
		}
		query := tx.Model(&models.Revision{}).Where("tenant_id = ? AND ontology_id = ?", actor.TenantID, ontologyID)
		if err := query.Count(&total).Error; err != nil {
			return err
		}
		return query.Select("ontology_id, revision, version, status, digest, generation, build_execution_id, created_at, updated_at, published_at").
			Order("revision DESC").Limit(page.PageSize).Offset(page.Offset()).Find(&rows).Error
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return rows, total, normalizeError(err)
}
