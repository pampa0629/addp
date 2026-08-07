package service

import (
	"context"
	"fmt"
	"strings"

	commonScheduler "github.com/addp/common/scheduler"
	"github.com/addp/service/internal/config"
	"github.com/addp/service/internal/models"
	"github.com/addp/service/internal/repository"
)

type RuntimePolicyResponse struct {
	Version             uint64 `json:"version"`
	HealthCheckCron     string `json:"health_check_cron"`
	MetadataRefreshCron string `json:"metadata_refresh_cron"`
	PendingRestart      bool   `json:"pending_restart"`
}

type UpdateRuntimePolicyInput struct {
	Version             uint64 `json:"version"`
	HealthCheckCron     string `json:"health_check_cron" binding:"required"`
	MetadataRefreshCron string `json:"metadata_refresh_cron" binding:"required"`
}

type RuntimePolicyService struct {
	repo *repository.RuntimePolicyRepository
}

func NewRuntimePolicyService(repo *repository.RuntimePolicyRepository) *RuntimePolicyService {
	return &RuntimePolicyService{repo: repo}
}

func (s *RuntimePolicyService) Get(ctx context.Context) (RuntimePolicyResponse, error) {
	value, err := s.repo.Get(ctx)
	if err != nil {
		return RuntimePolicyResponse{}, err
	}
	if value == nil {
		value = defaultRuntimePolicy()
	}
	return response(value, value.Version > 0), nil
}

func (s *RuntimePolicyService) Update(ctx context.Context, input UpdateRuntimePolicyInput, updatedBy uint) (RuntimePolicyResponse, error) {
	value := &models.RuntimePolicy{HealthCheckCron: strings.TrimSpace(input.HealthCheckCron), MetadataRefreshCron: strings.TrimSpace(input.MetadataRefreshCron), UpdatedBy: updatedBy}
	if err := validateRuntimePolicy(value); err != nil {
		return RuntimePolicyResponse{}, err
	}
	if err := s.repo.Save(ctx, value, input.Version); err != nil {
		return RuntimePolicyResponse{}, err
	}
	return response(value, true), nil
}

func (s *RuntimePolicyService) Apply(ctx context.Context, cfg *config.Config) error {
	value, err := s.repo.Get(ctx)
	if err != nil || value == nil {
		return err
	}
	if err := validateRuntimePolicy(value); err != nil {
		return fmt.Errorf("stored service runtime policy is invalid: %w", err)
	}
	cfg.HealthCheckCron, cfg.MetadataRefreshCron = value.HealthCheckCron, value.MetadataRefreshCron
	return nil
}

func defaultRuntimePolicy() *models.RuntimePolicy {
	return &models.RuntimePolicy{HealthCheckCron: "0 * * * *", MetadataRefreshCron: "0 2 * * *"}
}

func validateRuntimePolicy(value *models.RuntimePolicy) error {
	if strings.TrimSpace(value.HealthCheckCron) == "" || strings.TrimSpace(value.MetadataRefreshCron) == "" {
		return fmt.Errorf("scheduler cron expressions must not be empty")
	}
	builder := commonScheduler.NewExpressionBuilder()
	if err := builder.Validate(value.HealthCheckCron); err != nil {
		return fmt.Errorf("invalid health check cron: %w", err)
	}
	if err := builder.Validate(value.MetadataRefreshCron); err != nil {
		return fmt.Errorf("invalid metadata refresh cron: %w", err)
	}
	return nil
}

func response(value *models.RuntimePolicy, pending bool) RuntimePolicyResponse {
	return RuntimePolicyResponse{Version: value.Version, HealthCheckCron: value.HealthCheckCron, MetadataRefreshCron: value.MetadataRefreshCron, PendingRestart: pending}
}
