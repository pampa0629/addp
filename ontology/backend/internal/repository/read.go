package repository

import (
	"context"

	"github.com/addp/ontology/internal/models"
	"github.com/addp/ontology/internal/semantic"
)

func (r *RevisionRepository) Head(ctx context.Context, actor models.Actor, ontologyID string) (*models.Ontology, error) {
	if err := ValidateActor(actor, semantic.Scope{TenantID: actor.TenantID, OntologyID: ontologyID, Revision: 1}); err != nil {
		return nil, err
	}
	var result models.Ontology
	err := r.db.WithContext(ctx).Where("tenant_id = ? AND ontology_id = ?", actor.TenantID, ontologyID).First(&result).Error
	return &result, normalizeError(err)
}

func (r *RevisionRepository) Projection(ctx context.Context, actor models.Actor, ontologyID, generation string) (*models.Projection, error) {
	if err := ValidateActor(actor, semantic.Scope{TenantID: actor.TenantID, OntologyID: ontologyID, Revision: 1}); err != nil {
		return nil, err
	}
	if !canonicalGeneration(generation) {
		return nil, ErrInvalid
	}
	var result models.Projection
	err := r.db.WithContext(ctx).Where("tenant_id = ? AND ontology_id = ? AND generation = ?", actor.TenantID, ontologyID, generation).First(&result).Error
	return &result, normalizeError(err)
}

// LatestProjection follows the immutable predecessor chain, not wall-clock
// ordering. A lost command response can be recovered without replaying writes.
func (r *RevisionRepository) LatestProjection(ctx context.Context, actor models.Actor, scope semantic.Scope) (*models.Projection, error) {
	if err := ValidateActor(actor, scope); err != nil {
		return nil, err
	}
	var rows []models.Projection
	err := r.db.WithContext(ctx).Table("ontology.projections AS p").Select("p.*").
		Where("p.tenant_id = ? AND p.ontology_id = ? AND p.revision = ?", scope.TenantID, scope.OntologyID, scope.Revision).
		Where("NOT EXISTS (SELECT 1 FROM ontology.projections successor WHERE successor.predecessor_generation = p.generation)").
		Limit(2).Find(&rows).Error
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, ErrNotFound
	}
	if len(rows) != 1 {
		return nil, ErrIntegrity
	}
	return &rows[0], nil
}
