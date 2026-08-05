package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/repository"
	"github.com/google/uuid"
)

type ResolvedInferenceScenarioBinding struct {
	ModelProfileID string
	BindingVersion uint64
	SourceScope    string
}

type InferenceScenarioBindingResponse struct {
	ScenarioCode   string     `json:"scenario_code"`
	ScopeType      string     `json:"scope_type"`
	TenantID       *uint      `json:"tenant_id,omitempty"`
	ModelProfileID string     `json:"model_profile_id"`
	Version        uint64     `json:"version"`
	UpdatedBy      *uint      `json:"updated_by,omitempty"`
	UpdatedAt      *time.Time `json:"updated_at,omitempty"`
	Effective      bool       `json:"effective"`
}

type UpdateInferenceScenarioBindingInput struct {
	Version        uint64 `json:"version"`
	ModelProfileID string `json:"model_profile_id" binding:"required"`
}

type InferenceScenarioBindingService struct {
	repo *repository.InferenceScenarioBindingRepository
}

func NewInferenceScenarioBindingService(repo *repository.InferenceScenarioBindingRepository) *InferenceScenarioBindingService {
	return &InferenceScenarioBindingService{repo: repo}
}

func (s *InferenceScenarioBindingService) Resolve(ctx context.Context, tenantID uint) (ResolvedInferenceScenarioBinding, error) {
	if s == nil || s.repo == nil {
		return ResolvedInferenceScenarioBinding{}, errors.New("inference scenario binding service is required")
	}
	value, err := s.repo.Resolve(ctx, tenantID, models.InferenceScenarioSemanticSearchEmbedding)
	if err != nil {
		return ResolvedInferenceScenarioBinding{}, err
	}
	if value == nil {
		return ResolvedInferenceScenarioBinding{}, errors.New("inference_scenario_not_configured")
	}
	return ResolvedInferenceScenarioBinding{ModelProfileID: value.ModelProfileID, BindingVersion: value.Version, SourceScope: value.ScopeType}, nil
}

func (s *InferenceScenarioBindingService) Get(ctx context.Context, scopeType string, tenantID *uint) (InferenceScenarioBindingResponse, error) {
	value, err := s.repo.Get(ctx, scopeType, tenantID, models.InferenceScenarioSemanticSearchEmbedding)
	if err != nil {
		return InferenceScenarioBindingResponse{}, err
	}
	if value == nil && scopeType == "tenant" && tenantID != nil {
		resolved, resolveErr := s.repo.Resolve(ctx, *tenantID, models.InferenceScenarioSemanticSearchEmbedding)
		if resolveErr != nil {
			return InferenceScenarioBindingResponse{}, resolveErr
		}
		if resolved != nil {
			return bindingResponse(resolved, true), nil
		}
	}
	if value == nil {
		return InferenceScenarioBindingResponse{ScenarioCode: models.InferenceScenarioSemanticSearchEmbedding, ScopeType: scopeType, TenantID: tenantID}, nil
	}
	return bindingResponse(value, true), nil
}

func (s *InferenceScenarioBindingService) Update(ctx context.Context, scopeType string, tenantID *uint, input UpdateInferenceScenarioBindingInput, updatedBy uint) (InferenceScenarioBindingResponse, error) {
	input.ModelProfileID = strings.TrimSpace(input.ModelProfileID)
	if _, err := uuid.Parse(input.ModelProfileID); err != nil {
		return InferenceScenarioBindingResponse{}, fmt.Errorf("model_profile_id must be a UUID")
	}
	if scopeType == "platform" {
		tenantID = nil
	} else if scopeType != "tenant" || tenantID == nil || *tenantID == 0 {
		return InferenceScenarioBindingResponse{}, fmt.Errorf("invalid inference scenario binding scope")
	}
	value := &models.InferenceScenarioBinding{
		ScenarioCode: models.InferenceScenarioSemanticSearchEmbedding, ScopeType: scopeType,
		TenantID: tenantID, ModelProfileID: input.ModelProfileID, UpdatedBy: updatedBy,
	}
	if err := s.repo.Save(ctx, value, input.Version); err != nil {
		return InferenceScenarioBindingResponse{}, err
	}
	return bindingResponse(value, true), nil
}

func bindingResponse(value *models.InferenceScenarioBinding, effective bool) InferenceScenarioBindingResponse {
	return InferenceScenarioBindingResponse{
		ScenarioCode: value.ScenarioCode, ScopeType: value.ScopeType, TenantID: value.TenantID,
		ModelProfileID: value.ModelProfileID, Version: value.Version, UpdatedBy: &value.UpdatedBy,
		UpdatedAt: &value.UpdatedAt, Effective: effective,
	}
}
