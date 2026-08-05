package service

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/repository"
)

type EffectiveEmbeddingConfiguration struct {
	Version          uint64
	BaseURL          string
	Model            string
	Timeout          time.Duration
	Dimension        int
	MaxDistance      float64
	MaxFileSizeMB    int
	BatchConcurrency int
	APIKey           string
}

type EmbeddingConfigurationResponse struct {
	Version          uint64     `json:"version"`
	BaseURL          string     `json:"base_url"`
	Model            string     `json:"model"`
	TimeoutSeconds   int        `json:"timeout_seconds"`
	Dimension        int        `json:"dimension"`
	MaxDistance      float64    `json:"max_distance"`
	MaxFileSizeMB    int        `json:"max_file_size_mb"`
	BatchConcurrency int        `json:"batch_concurrency"`
	APIKeyConfigured bool       `json:"api_key_configured"`
	Persisted        bool       `json:"persisted"`
	UpdatedBy        *uint      `json:"updated_by,omitempty"`
	UpdatedAt        *time.Time `json:"updated_at,omitempty"`
}

type UpdateEmbeddingConfigurationInput struct {
	Version          uint64  `json:"version"`
	BaseURL          string  `json:"base_url" binding:"required"`
	Model            string  `json:"model" binding:"required"`
	TimeoutSeconds   int     `json:"timeout_seconds" binding:"required"`
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
	apiKey   string
	provider *EmbeddingConfigurationProvider
}

func NewEmbeddingConfigurationService(repo *repository.EmbeddingConfigurationRepository, apiKey string) *EmbeddingConfigurationService {
	service := &EmbeddingConfigurationService{
		repo:     repo,
		apiKey:   strings.TrimSpace(apiKey),
		provider: &EmbeddingConfigurationProvider{},
	}
	service.provider.replace(service.effective(nil))
	return service
}

func (s *EmbeddingConfigurationService) Initialize(ctx context.Context) error {
	value, err := s.repo.Get(ctx)
	if err != nil {
		return err
	}
	if value != nil {
		if err := validateEmbeddingConfiguration(value.BaseURL, value.Model, value.TimeoutSeconds, value.MaxDistance, value.MaxFileSizeMB, value.BatchConcurrency); err != nil {
			return fmt.Errorf("stored embedding configuration is invalid: %w", err)
		}
	}
	s.provider.replace(s.effective(value))
	return nil
}

func (s *EmbeddingConfigurationService) Provider() *EmbeddingConfigurationProvider {
	return s.provider
}

func (s *EmbeddingConfigurationService) Get(ctx context.Context) (EmbeddingConfigurationResponse, error) {
	value, err := s.repo.Get(ctx)
	if err != nil {
		return EmbeddingConfigurationResponse{}, err
	}
	return s.response(value), nil
}

func (s *EmbeddingConfigurationService) Update(ctx context.Context, input UpdateEmbeddingConfigurationInput, updatedBy uint) (EmbeddingConfigurationResponse, error) {
	input.BaseURL = strings.TrimSpace(input.BaseURL)
	input.Model = strings.TrimSpace(input.Model)
	if err := validateEmbeddingConfiguration(input.BaseURL, input.Model, input.TimeoutSeconds, input.MaxDistance, input.MaxFileSizeMB, input.BatchConcurrency); err != nil {
		return EmbeddingConfigurationResponse{}, err
	}
	value := &models.EmbeddingConfiguration{
		BaseURL: input.BaseURL, Model: input.Model, TimeoutSeconds: input.TimeoutSeconds,
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
		Model: "qwen3-vl-embedding", Timeout: 15 * time.Second,
		Dimension: models.EmbeddingVectorDimension, MaxDistance: 0.78,
		MaxFileSizeMB: 10, BatchConcurrency: 5, APIKey: s.apiKey,
	}
	if value != nil {
		effective.Version = value.Version
		effective.BaseURL = value.BaseURL
		effective.Model = value.Model
		effective.Timeout = time.Duration(value.TimeoutSeconds) * time.Second
		effective.MaxDistance = value.MaxDistance
		effective.MaxFileSizeMB = value.MaxFileSizeMB
		effective.BatchConcurrency = value.BatchConcurrency
	}
	return effective
}

func (s *EmbeddingConfigurationService) response(value *models.EmbeddingConfiguration) EmbeddingConfigurationResponse {
	effective := s.effective(value)
	response := EmbeddingConfigurationResponse{
		Version: effective.Version, BaseURL: effective.BaseURL, Model: effective.Model,
		TimeoutSeconds: int(effective.Timeout / time.Second), Dimension: effective.Dimension,
		MaxDistance: effective.MaxDistance, MaxFileSizeMB: effective.MaxFileSizeMB,
		BatchConcurrency: effective.BatchConcurrency, APIKeyConfigured: effective.APIKey != "",
		Persisted: value != nil,
	}
	if value != nil {
		response.UpdatedBy = &value.UpdatedBy
		response.UpdatedAt = &value.UpdatedAt
	}
	return response
}

func validateEmbeddingConfiguration(baseURL, model string, timeoutSeconds int, maxDistance float64, maxFileSizeMB, batchConcurrency int) error {
	parsed, err := url.ParseRequestURI(baseURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("base_url must be an HTTP(S) service root without credentials, query, or fragment")
	}
	if strings.TrimSpace(model) == "" || len(model) > 255 {
		return fmt.Errorf("model is required and must not exceed 255 characters")
	}
	if timeoutSeconds < 1 || timeoutSeconds > 300 {
		return fmt.Errorf("timeout_seconds must be between 1 and 300")
	}
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
