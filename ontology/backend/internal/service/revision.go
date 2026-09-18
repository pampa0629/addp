// Package service owns ontology commands. It is not an HTTP/authentication
// boundary: callers must authorize the operation before supplying Actor facts.
package service

import (
	"context"
	"fmt"

	"github.com/addp/ontology/internal/models"
	"github.com/addp/ontology/internal/repository"
	"github.com/addp/ontology/internal/semantic"
)

type RevisionService struct {
	repo *repository.RevisionRepository
}

func NewRevisionService(repo *repository.RevisionRepository) *RevisionService {
	return &RevisionService{repo: repo}
}

func (s *RevisionService) RebuildProjection(ctx context.Context, actor models.Actor, scope semantic.Scope, version uint64, failedGeneration string, expectedActivationVersion uint64) (*models.Projection, error) {
	return s.repo.RebuildProjection(ctx, actor, scope, version, failedGeneration, expectedActivationVersion)
}

func (s *RevisionService) CreateDraft(ctx context.Context, actor models.Actor, definition semantic.Definition) (*models.Revision, error) {
	if err := repository.ValidateActor(actor, definition.Scope); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	snapshot, err := semantic.Freeze(definition)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", repository.ErrInvalid, err)
	}
	return s.repo.Create(ctx, actor, snapshot)
}

func (s *RevisionService) SaveDraft(ctx context.Context, actor models.Actor, version uint64, definition semantic.Definition) (*models.Revision, error) {
	if err := repository.ValidateActor(actor, definition.Scope); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	snapshot, err := semantic.Freeze(definition)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", repository.ErrInvalid, err)
	}
	return s.repo.Change(ctx, actor, definition.Scope, version, "save", snapshot)
}

func (s *RevisionService) Get(ctx context.Context, actor models.Actor, scope semantic.Scope) (*models.Revision, error) {
	record, err := s.repo.Get(ctx, actor, scope)
	if err != nil {
		return nil, err
	}
	snapshot, err := semantic.Restore([]byte(record.Payload), record.Digest)
	if err != nil || snapshot.Scope() != scope {
		return nil, repository.ErrIntegrity
	}
	return record, nil
}

func (s *RevisionService) Transition(ctx context.Context, actor models.Actor, scope semantic.Scope, version uint64, action string) (*models.Revision, error) {
	// Editing is a separate typed command; a transition never accepts content.
	if action != "submit" && action != "return" && action != "publish" && action != "withdraw" {
		return nil, repository.ErrInvalid
	}
	record, err := s.repo.Get(ctx, actor, scope)
	if err != nil {
		return nil, err
	}
	if record.Version != version {
		return nil, repository.ErrConflict
	}
	snapshot, err := semantic.Restore([]byte(record.Payload), record.Digest)
	if err != nil || snapshot.Scope() != scope {
		return nil, repository.ErrIntegrity
	}
	return s.repo.Change(ctx, actor, scope, version, action, snapshot)
}
