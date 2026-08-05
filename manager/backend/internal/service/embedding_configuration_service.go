package service

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/repository"
)

type EffectiveEmbeddingConfiguration struct {
	Version          uint64
	Dimension        int
	MaxDistance      float64
	MaxFileSizeMB    int
	BatchConcurrency int
}

type EmbeddingConfigurationResponse struct {
	Version          uint64     `json:"version"`
	Dimension        int        `json:"dimension"`
	MaxDistance      float64    `json:"max_distance"`
	MaxFileSizeMB    int        `json:"max_file_size_mb"`
	BatchConcurrency int        `json:"batch_concurrency"`
	Persisted        bool       `json:"persisted"`
	UpdatedBy        *uint      `json:"updated_by,omitempty"`
	UpdatedAt        *time.Time `json:"updated_at,omitempty"`
}

type UpdateEmbeddingConfigurationInput struct {
	Version          uint64  `json:"version"`
	MaxDistance      float64 `json:"max_distance" binding:"required"`
	MaxFileSizeMB    int     `json:"max_file_size_mb" binding:"required"`
	BatchConcurrency int     `json:"batch_concurrency" binding:"required"`
}

type EmbeddingConfigurationProvider struct {
	mu      sync.RWMutex
	current EffectiveEmbeddingConfiguration
}

func NewEmbeddingConfigurationProvider(value EffectiveEmbeddingConfiguration) *EmbeddingConfigurationProvider {
	provider := &EmbeddingConfigurationProvider{}
	provider.replace(value)
	return provider
}

func (p *EmbeddingConfigurationProvider) Current() EffectiveEmbeddingConfiguration {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.current
}

func (p *EmbeddingConfigurationProvider) replace(value EffectiveEmbeddingConfiguration) {
	p.mu.Lock()
	p.current = value
	p.mu.Unlock()
}

type EmbeddingConfigurationService struct {
	repo     *repository.EmbeddingConfigurationRepository
	provider *EmbeddingConfigurationProvider
}

func NewEmbeddingConfigurationService(repo *repository.EmbeddingConfigurationRepository) *EmbeddingConfigurationService {
	service := &EmbeddingConfigurationService{repo: repo, provider: &EmbeddingConfigurationProvider{}}
	service.provider.replace(service.effective(nil))
	return service
}

func (s *EmbeddingConfigurationService) Initialize(ctx context.Context) error {
	value, err := s.repo.Get(ctx)
	if err != nil {
		return err
	}
	if value != nil {
		if err := validateEmbeddingConfiguration(value.MaxDistance, value.MaxFileSizeMB, value.BatchConcurrency); err != nil {
			return fmt.Errorf("stored embedding configuration is invalid: %w", err)
		}
	}
	s.provider.replace(s.effective(value))
	return nil
}

func (s *EmbeddingConfigurationService) Provider() *EmbeddingConfigurationProvider { return s.provider }

func (s *EmbeddingConfigurationService) Get(ctx context.Context) (EmbeddingConfigurationResponse, error) {
	value, err := s.repo.Get(ctx)
	if err != nil {
		return EmbeddingConfigurationResponse{}, err
	}
	return s.response(value), nil
}

func (s *EmbeddingConfigurationService) Update(ctx context.Context, input UpdateEmbeddingConfigurationInput, updatedBy uint) (EmbeddingConfigurationResponse, error) {
	if err := validateEmbeddingConfiguration(input.MaxDistance, input.MaxFileSizeMB, input.BatchConcurrency); err != nil {
		return EmbeddingConfigurationResponse{}, err
	}
	value := &models.EmbeddingConfiguration{
		MaxDistance: input.MaxDistance, MaxFileSizeMB: input.MaxFileSizeMB,
		BatchConcurrency: input.BatchConcurrency, UpdatedBy: updatedBy,
	}
	if err := s.repo.Save(ctx, value, input.Version); err != nil {
		return EmbeddingConfigurationResponse{}, err
	}
	s.provider.replace(s.effective(value))
	return s.response(value), nil
}

func (s *EmbeddingConfigurationService) effective(value *models.EmbeddingConfiguration) EffectiveEmbeddingConfiguration {
	effective := EffectiveEmbeddingConfiguration{
		Dimension: models.EmbeddingVectorDimension, MaxDistance: 0.78,
		MaxFileSizeMB: 10, BatchConcurrency: 5,
	}
	if value != nil {
		effective.Version = value.Version
		effective.MaxDistance = value.MaxDistance
		effective.MaxFileSizeMB = value.MaxFileSizeMB
		effective.BatchConcurrency = value.BatchConcurrency
	}
	return effective
}

func (s *EmbeddingConfigurationService) response(value *models.EmbeddingConfiguration) EmbeddingConfigurationResponse {
	effective := s.effective(value)
	response := EmbeddingConfigurationResponse{
		Version: effective.Version, Dimension: effective.Dimension,
		MaxDistance: effective.MaxDistance, MaxFileSizeMB: effective.MaxFileSizeMB,
		BatchConcurrency: effective.BatchConcurrency, Persisted: value != nil,
	}
	if value != nil {
		response.UpdatedBy = &value.UpdatedBy
		response.UpdatedAt = &value.UpdatedAt
	}
	return response
}

func validateEmbeddingConfiguration(maxDistance float64, maxFileSizeMB, batchConcurrency int) error {
	if maxDistance <= 0 || maxDistance > 2 {
		return fmt.Errorf("max_distance must be greater than 0 and at most 2")
	}
	if maxFileSizeMB < 1 || maxFileSizeMB > 1024 {
		return fmt.Errorf("max_file_size_mb must be between 1 and 1024")
	}
	if batchConcurrency < 1 || batchConcurrency > 64 {
		return fmt.Errorf("batch_concurrency must be between 1 and 64")
	}
	return nil
}
