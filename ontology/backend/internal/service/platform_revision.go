package service

import (
	"context"

	"github.com/addp/ontology/internal/models"
	"github.com/addp/ontology/internal/platform"
	"github.com/addp/ontology/internal/repository"
)

// PlatformRevisionService is an internal immutable recording service, not an
// authorization boundary or a publication/activation interface.
type PlatformRevisionService struct {
	repo *repository.PlatformRevisionRepository
}

func NewPlatformRevisionService(repo *repository.PlatformRevisionRepository) *PlatformRevisionService {
	return &PlatformRevisionService{repo: repo}
}

func (s *PlatformRevisionService) Store(ctx context.Context, actor models.PlatformActor, snapshot *platform.Snapshot, expectedLastRevision uint64) (*models.PlatformRevision, error) {
	return s.repo.Store(ctx, actor, snapshot, expectedLastRevision)
}

func (s *PlatformRevisionService) Head(ctx context.Context, actor models.PlatformActor, capability string) (*models.PlatformCapability, error) {
	return s.repo.Head(ctx, actor, capability)
}

func (s *PlatformRevisionService) Get(ctx context.Context, actor models.PlatformActor, capability string, revision uint64) (*models.PlatformRevision, error) {
	record, err := s.repo.Get(ctx, actor, capability, revision)
	if err != nil {
		return nil, err
	}
	snapshot, err := platform.Restore([]byte(record.Payload), record.Digest)
	if err != nil {
		return nil, repository.ErrIntegrity
	}
	definition, err := snapshot.Context()
	if err != nil || definition.Capability != capability || definition.Revision != revision {
		return nil, repository.ErrIntegrity
	}
	return record, nil
}
