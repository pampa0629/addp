package service

import (
	"context"
	"fmt"
	"time"

	"github.com/addp/monitor/internal/config"
	"github.com/addp/monitor/internal/models"
	"github.com/addp/monitor/internal/repository"
)

type RuntimePolicyResponse struct {
	Version                    uint64 `json:"version"`
	AlertEvaluationIntervalSec int64  `json:"alert_evaluation_interval_seconds"`
	WebhookDispatchIntervalSec int64  `json:"webhook_dispatch_interval_seconds"`
	WebhookHTTPTimeoutSec      int64  `json:"webhook_http_timeout_seconds"`
	WebhookLeaseDurationSec    int64  `json:"webhook_lease_duration_seconds"`
	WebhookMaxAttempts         int    `json:"webhook_max_attempts"`
	WebhookRetryInitialSec     int64  `json:"webhook_retry_initial_backoff_seconds"`
	WebhookRetryMaxSec         int64  `json:"webhook_retry_max_backoff_seconds"`
	EmailDispatchIntervalSec   int64  `json:"email_dispatch_interval_seconds"`
	EmailSMTPTimeoutSec        int64  `json:"email_smtp_timeout_seconds"`
	EmailLeaseDurationSec      int64  `json:"email_lease_duration_seconds"`
	EmailMaxAttempts           int    `json:"email_max_attempts"`
	EmailRetryInitialSec       int64  `json:"email_retry_initial_backoff_seconds"`
	EmailRetryMaxSec           int64  `json:"email_retry_max_backoff_seconds"`
	PendingRestart             bool   `json:"pending_restart"`
}

type UpdateRuntimePolicyInput struct {
	Version                    uint64 `json:"version"`
	AlertEvaluationIntervalSec int64  `json:"alert_evaluation_interval_seconds" binding:"required"`
	WebhookDispatchIntervalSec int64  `json:"webhook_dispatch_interval_seconds" binding:"required"`
	WebhookHTTPTimeoutSec      int64  `json:"webhook_http_timeout_seconds" binding:"required"`
	WebhookLeaseDurationSec    int64  `json:"webhook_lease_duration_seconds" binding:"required"`
	WebhookMaxAttempts         int    `json:"webhook_max_attempts" binding:"required"`
	WebhookRetryInitialSec     int64  `json:"webhook_retry_initial_backoff_seconds" binding:"required"`
	WebhookRetryMaxSec         int64  `json:"webhook_retry_max_backoff_seconds" binding:"required"`
	EmailDispatchIntervalSec   int64  `json:"email_dispatch_interval_seconds" binding:"required"`
	EmailSMTPTimeoutSec        int64  `json:"email_smtp_timeout_seconds" binding:"required"`
	EmailLeaseDurationSec      int64  `json:"email_lease_duration_seconds" binding:"required"`
	EmailMaxAttempts           int    `json:"email_max_attempts" binding:"required"`
	EmailRetryInitialSec       int64  `json:"email_retry_initial_backoff_seconds" binding:"required"`
	EmailRetryMaxSec           int64  `json:"email_retry_max_backoff_seconds" binding:"required"`
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
	return runtimePolicyResponse(value, value.Version > 0), nil
}

func (s *RuntimePolicyService) Update(ctx context.Context, input UpdateRuntimePolicyInput, updatedBy uint) (RuntimePolicyResponse, error) {
	value := &models.RuntimePolicy{
		AlertEvaluationIntervalSec: input.AlertEvaluationIntervalSec, WebhookDispatchIntervalSec: input.WebhookDispatchIntervalSec,
		WebhookHTTPTimeoutSec: input.WebhookHTTPTimeoutSec, WebhookLeaseDurationSec: input.WebhookLeaseDurationSec,
		WebhookMaxAttempts: input.WebhookMaxAttempts, WebhookRetryInitialSec: input.WebhookRetryInitialSec,
		WebhookRetryMaxSec: input.WebhookRetryMaxSec, EmailDispatchIntervalSec: input.EmailDispatchIntervalSec,
		EmailSMTPTimeoutSec: input.EmailSMTPTimeoutSec, EmailLeaseDurationSec: input.EmailLeaseDurationSec,
		EmailMaxAttempts: input.EmailMaxAttempts, EmailRetryInitialSec: input.EmailRetryInitialSec,
		EmailRetryMaxSec: input.EmailRetryMaxSec, UpdatedBy: updatedBy,
	}
	if err := validateRuntimePolicy(value); err != nil {
		return RuntimePolicyResponse{}, err
	}
	if err := s.repo.Save(ctx, value, input.Version); err != nil {
		return RuntimePolicyResponse{}, err
	}
	return runtimePolicyResponse(value, true), nil
}

func (s *RuntimePolicyService) Apply(ctx context.Context, cfg *config.Config) error {
	value, err := s.repo.Get(ctx)
	if err != nil || value == nil {
		return err
	}
	if err := validateRuntimePolicy(value); err != nil {
		return fmt.Errorf("stored monitor runtime policy is invalid: %w", err)
	}
	cfg.AlertEvaluationInterval = time.Duration(value.AlertEvaluationIntervalSec) * time.Second
	cfg.WebhookDispatchInterval = time.Duration(value.WebhookDispatchIntervalSec) * time.Second
	cfg.WebhookHTTPTimeout = time.Duration(value.WebhookHTTPTimeoutSec) * time.Second
	cfg.WebhookLeaseDuration = time.Duration(value.WebhookLeaseDurationSec) * time.Second
	cfg.WebhookMaxAttempts = value.WebhookMaxAttempts
	cfg.WebhookRetryInitial = time.Duration(value.WebhookRetryInitialSec) * time.Second
	cfg.WebhookRetryMax = time.Duration(value.WebhookRetryMaxSec) * time.Second
	cfg.EmailDispatchInterval = time.Duration(value.EmailDispatchIntervalSec) * time.Second
	cfg.EmailSMTPTimeout = time.Duration(value.EmailSMTPTimeoutSec) * time.Second
	cfg.EmailLeaseDuration = time.Duration(value.EmailLeaseDurationSec) * time.Second
	cfg.EmailMaxAttempts = value.EmailMaxAttempts
	cfg.EmailRetryInitial = time.Duration(value.EmailRetryInitialSec) * time.Second
	cfg.EmailRetryMax = time.Duration(value.EmailRetryMaxSec) * time.Second
	return nil
}

func defaultRuntimePolicy() *models.RuntimePolicy {
	return &models.RuntimePolicy{
		AlertEvaluationIntervalSec: 15, WebhookDispatchIntervalSec: 2, WebhookHTTPTimeoutSec: 10, WebhookLeaseDurationSec: 30,
		WebhookMaxAttempts: 8, WebhookRetryInitialSec: 5, WebhookRetryMaxSec: 300, EmailDispatchIntervalSec: 2,
		EmailSMTPTimeoutSec: 15, EmailLeaseDurationSec: 30, EmailMaxAttempts: 8, EmailRetryInitialSec: 5, EmailRetryMaxSec: 300,
	}
}

func validateRuntimePolicy(value *models.RuntimePolicy) error {
	if value.AlertEvaluationIntervalSec < 1 || value.WebhookDispatchIntervalSec < 1 || value.EmailDispatchIntervalSec < 1 {
		return fmt.Errorf("evaluation and dispatcher intervals must be positive")
	}
	if value.WebhookHTTPTimeoutSec < 1 || value.WebhookLeaseDurationSec <= value.WebhookHTTPTimeoutSec {
		return fmt.Errorf("webhook lease must exceed HTTP timeout")
	}
	if value.EmailSMTPTimeoutSec < 1 || value.EmailLeaseDurationSec <= value.EmailSMTPTimeoutSec {
		return fmt.Errorf("email lease must exceed SMTP timeout")
	}
	if value.WebhookMaxAttempts < 1 || value.EmailMaxAttempts < 1 {
		return fmt.Errorf("maximum attempts must be positive")
	}
	if value.WebhookRetryInitialSec < 1 || value.WebhookRetryInitialSec > value.WebhookRetryMaxSec || value.EmailRetryInitialSec < 1 || value.EmailRetryInitialSec > value.EmailRetryMaxSec {
		return fmt.Errorf("retry initial backoff must not exceed retry maximum")
	}
	return nil
}

func runtimePolicyResponse(value *models.RuntimePolicy, pendingRestart bool) RuntimePolicyResponse {
	return RuntimePolicyResponse{
		Version: value.Version, AlertEvaluationIntervalSec: value.AlertEvaluationIntervalSec, WebhookDispatchIntervalSec: value.WebhookDispatchIntervalSec,
		WebhookHTTPTimeoutSec: value.WebhookHTTPTimeoutSec, WebhookLeaseDurationSec: value.WebhookLeaseDurationSec, WebhookMaxAttempts: value.WebhookMaxAttempts,
		WebhookRetryInitialSec: value.WebhookRetryInitialSec, WebhookRetryMaxSec: value.WebhookRetryMaxSec, EmailDispatchIntervalSec: value.EmailDispatchIntervalSec,
		EmailSMTPTimeoutSec: value.EmailSMTPTimeoutSec, EmailLeaseDurationSec: value.EmailLeaseDurationSec, EmailMaxAttempts: value.EmailMaxAttempts,
		EmailRetryInitialSec: value.EmailRetryInitialSec, EmailRetryMaxSec: value.EmailRetryMaxSec, PendingRestart: pendingRestart,
	}
}
