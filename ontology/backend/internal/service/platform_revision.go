package service

import (
	"context"
	"encoding/json"
	"errors"

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

// PlatformCapabilityContext consumes only the PG active publication. Revision
// and digest identify this read; they do not grant permission to execute Tools.
func (s *PlatformRevisionService) PlatformCapabilityContext(ctx context.Context, capability string) (platform.Context, error) {
	record, err := s.repo.ActivePlatformDefinition(ctx, capability)
	if err != nil {
		return platform.Context{}, err
	}
	return restorePlatformContext(ctx, *record)
}

// PlatformDefinition is a read-only view of one authoritative frozen release.
type PlatformDefinition struct {
	Context platform.Context `json:"context"`
	Review  platform.Review  `json:"review"`
}

func (s *PlatformRevisionService) PlatformDefinition(ctx context.Context, capability string) (PlatformDefinition, error) {
	record, err := s.repo.ActivePlatformDefinition(ctx, capability)
	if err != nil {
		return PlatformDefinition{}, err
	}
	return restorePlatformDefinition(ctx, *record)
}

func restorePlatformDefinition(ctx context.Context, record models.PlatformRevision) (PlatformDefinition, error) {
	snapshot, err := platform.Restore([]byte(record.Payload), record.Digest)
	if err != nil {
		return PlatformDefinition{}, repository.ErrIntegrity
	}
	definition, err := snapshot.Context()
	if err != nil || definition.Capability != record.Capability || definition.Revision != record.Revision {
		return PlatformDefinition{}, repository.ErrIntegrity
	}
	review, err := snapshot.Review()
	if err != nil {
		return PlatformDefinition{}, repository.ErrIntegrity
	}
	if err := ctx.Err(); err != nil {
		return PlatformDefinition{}, err
	}
	return PlatformDefinition{Context: definition, Review: review}, nil
}

func (s *PlatformRevisionService) PlatformCapabilities(ctx context.Context) (platform.Catalog, error) {
	records, err := s.repo.ActivePlatformDefinitions(ctx)
	if errors.Is(err, repository.ErrPlatformCatalogTooLarge) {
		return platform.Catalog{}, ErrResultTooLarge
	}
	if err != nil {
		return platform.Catalog{}, err
	}
	return platformCatalog(ctx, records)
}

func platformCatalog(ctx context.Context, records []models.PlatformRevision) (platform.Catalog, error) {
	if len(records) > platform.CatalogMaxItems {
		return platform.Catalog{}, ErrResultTooLarge
	}
	catalog := platform.Catalog{SchemaVersion: "addp.platform-capability-catalog/v1", Capabilities: make([]platform.Context, 0, len(records))}
	for _, record := range records {
		definition, err := restorePlatformContext(ctx, record)
		if err != nil {
			return platform.Catalog{}, err
		}
		catalog.Capabilities = append(catalog.Capabilities, definition)
	}
	encoded, err := json.Marshal(catalog)
	if err != nil {
		return platform.Catalog{}, err
	}
	if len(encoded) > platform.CatalogMaxBytes {
		return platform.Catalog{}, ErrResultTooLarge
	}
	return catalog, ctx.Err()
}

func restorePlatformContext(ctx context.Context, record models.PlatformRevision) (platform.Context, error) {
	snapshot, err := platform.Restore([]byte(record.Payload), record.Digest)
	if err != nil {
		return platform.Context{}, repository.ErrIntegrity
	}
	result, err := snapshot.Context()
	if err != nil || result.Capability != record.Capability || result.Revision != record.Revision {
		return platform.Context{}, repository.ErrIntegrity
	}
	if err := ctx.Err(); err != nil {
		return platform.Context{}, err
	}
	return result, nil
}
