package service

import (
	"context"
	"fmt"
	"time"

	"github.com/addp/transfer/internal/config"
	"github.com/addp/transfer/internal/models"
	"github.com/addp/transfer/internal/repository"
)

type ContinuousPolicyResponse struct {
	Version                         uint64 `json:"version"`
	DiagnosticsIntervalSeconds      int64  `json:"diagnostics_interval_seconds"`
	RetentionDegradedHorizonSeconds int64  `json:"retention_degraded_horizon_seconds"`
	RetentionCriticalHorizonSeconds int64  `json:"retention_critical_horizon_seconds"`
	CheckpointStaleAfterSeconds     int64  `json:"checkpoint_stale_after_seconds"`
	RecoveryInitialBackoffSeconds   int64  `json:"recovery_initial_backoff_seconds"`
	RecoveryMaxBackoffSeconds       int64  `json:"recovery_max_backoff_seconds"`
	RecoveryMaxFailures             int    `json:"recovery_max_consecutive_failures"`
	RecoveryCircuitOpenSeconds      int64  `json:"recovery_circuit_open_seconds"`
	RecoveryStabilityWindowSeconds  int64  `json:"recovery_stability_window_seconds"`
	PendingRestart                  bool   `json:"pending_restart"`
}

type UpdateContinuousPolicyInput struct {
	Version                         uint64 `json:"version"`
	DiagnosticsIntervalSeconds      int64  `json:"diagnostics_interval_seconds" binding:"required"`
	RetentionDegradedHorizonSeconds int64  `json:"retention_degraded_horizon_seconds" binding:"required"`
	RetentionCriticalHorizonSeconds int64  `json:"retention_critical_horizon_seconds" binding:"required"`
	CheckpointStaleAfterSeconds     int64  `json:"checkpoint_stale_after_seconds" binding:"required"`
	RecoveryInitialBackoffSeconds   int64  `json:"recovery_initial_backoff_seconds" binding:"required"`
	RecoveryMaxBackoffSeconds       int64  `json:"recovery_max_backoff_seconds" binding:"required"`
	RecoveryMaxFailures             int    `json:"recovery_max_consecutive_failures" binding:"required"`
	RecoveryCircuitOpenSeconds      int64  `json:"recovery_circuit_open_seconds" binding:"required"`
	RecoveryStabilityWindowSeconds  int64  `json:"recovery_stability_window_seconds" binding:"required"`
}

type ContinuousPolicyService struct {
	repo *repository.ContinuousPolicyRepository
}

func NewContinuousPolicyService(repo *repository.ContinuousPolicyRepository) *ContinuousPolicyService {
	return &ContinuousPolicyService{repo: repo}
}

func (s *ContinuousPolicyService) Get(ctx context.Context) (ContinuousPolicyResponse, error) {
	value, err := s.repo.Get(ctx)
	if err != nil {
		return ContinuousPolicyResponse{}, err
	}
	if value == nil {
		value = defaultContinuousPolicy()
	}
	return continuousPolicyResponse(value, value.Version > 0), nil
}

func (s *ContinuousPolicyService) Update(ctx context.Context, input UpdateContinuousPolicyInput, updatedBy uint) (ContinuousPolicyResponse, error) {
	value := &models.ContinuousPolicy{
		DiagnosticsIntervalSeconds: input.DiagnosticsIntervalSeconds, RetentionDegradedHorizonSeconds: input.RetentionDegradedHorizonSeconds,
		RetentionCriticalHorizonSeconds: input.RetentionCriticalHorizonSeconds, CheckpointStaleAfterSeconds: input.CheckpointStaleAfterSeconds,
		RecoveryInitialBackoffSeconds: input.RecoveryInitialBackoffSeconds, RecoveryMaxBackoffSeconds: input.RecoveryMaxBackoffSeconds,
		RecoveryMaxFailures: input.RecoveryMaxFailures, RecoveryCircuitOpenSeconds: input.RecoveryCircuitOpenSeconds,
		RecoveryStabilityWindowSeconds: input.RecoveryStabilityWindowSeconds, UpdatedBy: updatedBy,
	}
	if err := validateContinuousPolicy(value); err != nil {
		return ContinuousPolicyResponse{}, err
	}
	if err := s.repo.Save(ctx, value, input.Version); err != nil {
		return ContinuousPolicyResponse{}, err
	}
	return continuousPolicyResponse(value, true), nil
}

func (s *ContinuousPolicyService) Apply(ctx context.Context, cfg *config.Config) error {
	value, err := s.repo.Get(ctx)
	if err != nil || value == nil {
		return err
	}
	if err := validateContinuousPolicy(value); err != nil {
		return fmt.Errorf("stored continuous policy is invalid: %w", err)
	}
	cfg.ContinuousDiagnosticsInterval = time.Duration(value.DiagnosticsIntervalSeconds) * time.Second
	cfg.ContinuousRetentionDegradedHorizon = time.Duration(value.RetentionDegradedHorizonSeconds) * time.Second
	cfg.ContinuousRetentionCriticalHorizon = time.Duration(value.RetentionCriticalHorizonSeconds) * time.Second
	cfg.ContinuousCheckpointStaleAfter = time.Duration(value.CheckpointStaleAfterSeconds) * time.Second
	cfg.ContinuousRecoveryInitialBackoff = time.Duration(value.RecoveryInitialBackoffSeconds) * time.Second
	cfg.ContinuousRecoveryMaxBackoff = time.Duration(value.RecoveryMaxBackoffSeconds) * time.Second
	cfg.ContinuousRecoveryMaxFailures = value.RecoveryMaxFailures
	cfg.ContinuousRecoveryCircuitOpenTime = time.Duration(value.RecoveryCircuitOpenSeconds) * time.Second
	cfg.ContinuousRecoveryStabilityWindow = time.Duration(value.RecoveryStabilityWindowSeconds) * time.Second
	return nil
}

func defaultContinuousPolicy() *models.ContinuousPolicy {
	return &models.ContinuousPolicy{
		DiagnosticsIntervalSeconds: 15, RetentionDegradedHorizonSeconds: 21600, RetentionCriticalHorizonSeconds: 3600,
		CheckpointStaleAfterSeconds: 300, RecoveryInitialBackoffSeconds: 1, RecoveryMaxBackoffSeconds: 60,
		RecoveryMaxFailures: 5, RecoveryCircuitOpenSeconds: 300, RecoveryStabilityWindowSeconds: 300,
	}
}

func validateContinuousPolicy(value *models.ContinuousPolicy) error {
	if value.DiagnosticsIntervalSeconds < 1 || value.DiagnosticsIntervalSeconds > 3600 {
		return fmt.Errorf("diagnostics_interval_seconds must be between 1 and 3600")
	}
	if value.RetentionCriticalHorizonSeconds < 1 || value.RetentionCriticalHorizonSeconds >= value.RetentionDegradedHorizonSeconds {
		return fmt.Errorf("retention critical horizon must be positive and less than degraded horizon")
	}
	if value.CheckpointStaleAfterSeconds <= value.DiagnosticsIntervalSeconds {
		return fmt.Errorf("checkpoint stale threshold must exceed diagnostics interval")
	}
	if value.RecoveryInitialBackoffSeconds < 1 || value.RecoveryInitialBackoffSeconds > value.RecoveryMaxBackoffSeconds {
		return fmt.Errorf("recovery initial backoff must not exceed max backoff")
	}
	if value.RecoveryMaxFailures < 1 || value.RecoveryMaxFailures > 100 {
		return fmt.Errorf("recovery max failures must be between 1 and 100")
	}
	if value.RecoveryCircuitOpenSeconds < 1 || value.RecoveryStabilityWindowSeconds < 1 {
		return fmt.Errorf("recovery circuit and stability durations must be positive")
	}
	return nil
}

func continuousPolicyResponse(value *models.ContinuousPolicy, pendingRestart bool) ContinuousPolicyResponse {
	return ContinuousPolicyResponse{
		Version: value.Version, DiagnosticsIntervalSeconds: value.DiagnosticsIntervalSeconds,
		RetentionDegradedHorizonSeconds: value.RetentionDegradedHorizonSeconds, RetentionCriticalHorizonSeconds: value.RetentionCriticalHorizonSeconds,
		CheckpointStaleAfterSeconds: value.CheckpointStaleAfterSeconds, RecoveryInitialBackoffSeconds: value.RecoveryInitialBackoffSeconds,
		RecoveryMaxBackoffSeconds: value.RecoveryMaxBackoffSeconds, RecoveryMaxFailures: value.RecoveryMaxFailures,
		RecoveryCircuitOpenSeconds: value.RecoveryCircuitOpenSeconds, RecoveryStabilityWindowSeconds: value.RecoveryStabilityWindowSeconds,
		PendingRestart: pendingRestart,
	}
}
