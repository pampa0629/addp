package service

import (
	"context"
	"github.com/addp/ontology/internal/models"
	"github.com/addp/ontology/internal/semantic"
)

func (s *RevisionService) Head(ctx context.Context, actor models.Actor, ontologyID string) (*models.Ontology, error) {
	return s.repo.Head(ctx, actor, ontologyID)
}

func (s *RevisionService) LatestProjection(ctx context.Context, actor models.Actor, scope semantic.Scope) (*models.Projection, error) {
	return s.repo.LatestProjection(ctx, actor, scope)
}

func (s *RevisionService) Projection(ctx context.Context, actor models.Actor, ontologyID, generation string) (*models.Projection, error) {
	return s.repo.Projection(ctx, actor, ontologyID, generation)
}
