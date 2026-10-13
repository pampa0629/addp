package service

import (
	"context"
	"fmt"

	"github.com/addp/develop/backend/internal/models"
	"github.com/addp/develop/backend/internal/repository"
)

type ExecutionPolicyResponse struct {
	ScopeType                 string `json:"scope_type"`
	TenantID                  *uint  `json:"tenant_id,omitempty"`
	DefaultQueryTimeout       int    `json:"default_query_timeout"`
	MaxQueryTimeout           int    `json:"max_query_timeout"`
	QueryResultLimit          int    `json:"query_result_limit"`
	QueryConcurrency          int    `json:"query_concurrency"`
	QueryPerEngineConcurrency int    `json:"query_per_engine_concurrency"`
	WorkflowConcurrency       int    `json:"workflow_concurrency"`
	NotebookConcurrency       int    `json:"notebook_concurrency"`
	Version                   uint64 `json:"version"`
	Inherited                 bool   `json:"inherited"`
}

type UpdateExecutionPolicyInput struct {
	QueryConcurrency          int    `json:"query_concurrency"`
	QueryPerEngineConcurrency int    `json:"query_per_engine_concurrency"`
	WorkflowConcurrency       int    `json:"workflow_concurrency"`
	NotebookConcurrency       int    `json:"notebook_concurrency"`
	Version                   uint64 `json:"version"`
	DefaultQueryTimeout       int    `json:"default_query_timeout" binding:"required"`
	MaxQueryTimeout           int    `json:"max_query_timeout"`
	QueryResultLimit          int    `json:"query_result_limit"`
}

type ExecutionPolicyService struct {
	repo     *repository.ExecutionPolicyRepository
	onChange func()
}

func (s *ExecutionPolicyService) SetOnChange(notify func()) { s.onChange = notify }

func (s *ExecutionPolicyService) ResolveConcurrency(ctx context.Context) (models.ExecutionConcurrency, error) {
	value, err := s.Get(ctx, "platform", nil)
	return models.ExecutionConcurrency{Query: value.QueryConcurrency, QueryPerEngine: value.QueryPerEngineConcurrency, Workflow: value.WorkflowConcurrency, Notebook: value.NotebookConcurrency}, err
}

func (s *ExecutionPolicyService) ResolveRuntime(ctx context.Context, tenantID uint) (defaultTimeout, maxTimeout, resultLimit int, err error) {
	platform, err := s.repo.Get(ctx, "platform", nil)
	if err != nil {
		return 0, 0, 0, err
	}
	if platform == nil {
		platform = defaultExecutionPolicyModel("platform", nil)
	}
	defaultTimeout = platform.DefaultQueryTimeout
	if tenantID > 0 {
		if tenant, tenantErr := s.repo.Get(ctx, "tenant", &tenantID); tenantErr != nil {
			return 0, 0, 0, tenantErr
		} else if tenant != nil {
			defaultTimeout = tenant.DefaultQueryTimeout
		}
	}
	return defaultTimeout, platform.MaxQueryTimeout, platform.QueryResultLimit, nil
}

func NewExecutionPolicyService(repo *repository.ExecutionPolicyRepository) *ExecutionPolicyService {
	return &ExecutionPolicyService{repo: repo}
}

func (s *ExecutionPolicyService) Get(ctx context.Context, scope string, tenantID *uint) (ExecutionPolicyResponse, error) {
	value, err := s.repo.Get(ctx, scope, tenantID)
	if err != nil {
		return ExecutionPolicyResponse{}, err
	}
	if scope == "tenant" {
		platform, pErr := s.repo.Get(ctx, "platform", nil)
		if pErr != nil {
			return ExecutionPolicyResponse{}, pErr
		}
		if value == nil && platform == nil {
			return defaultExecutionPolicy(scope, tenantID), nil
		}
		if platform == nil {
			platform = defaultExecutionPolicyModel("platform", nil)
		}
		if value == nil {
			return responseFrom(platform, scope, tenantID, true), nil
		}
		platform.DefaultQueryTimeout = value.DefaultQueryTimeout
		platform.Version = value.Version
		return responseFrom(platform, scope, tenantID, false), nil
	}
	if value == nil {
		return defaultExecutionPolicy(scope, tenantID), nil
	}
	return responseFrom(value, scope, tenantID, false), nil
}

func (s *ExecutionPolicyService) Update(ctx context.Context, scope string, tenantID *uint, input UpdateExecutionPolicyInput, updatedBy uint) (ExecutionPolicyResponse, error) {
	if scope != "platform" && scope != "tenant" {
		return ExecutionPolicyResponse{}, fmt.Errorf("invalid query policy scope")
	}
	if input.DefaultQueryTimeout < 1 || input.DefaultQueryTimeout > 3600 {
		return ExecutionPolicyResponse{}, fmt.Errorf("default_query_timeout must be between 1 and 3600")
	}
	if scope == "platform" {
		if input.WorkflowConcurrency <= 0 || input.WorkflowConcurrency > 1000 || input.NotebookConcurrency <= 0 || input.NotebookConcurrency > 1000 {
			return ExecutionPolicyResponse{}, fmt.Errorf("workflow_concurrency and notebook_concurrency must be between 1 and 1000")
		}
		if input.QueryConcurrency <= 0 || input.QueryPerEngineConcurrency <= 0 || input.QueryPerEngineConcurrency > input.QueryConcurrency {
			return ExecutionPolicyResponse{}, fmt.Errorf("query_concurrency and query_per_engine_concurrency must be positive, and per-engine must not exceed total")
		}
		if input.MaxQueryTimeout < input.DefaultQueryTimeout || input.MaxQueryTimeout > 86400 {
			return ExecutionPolicyResponse{}, fmt.Errorf("max_query_timeout must be >= default_query_timeout and <= 86400")
		}
		if input.QueryResultLimit < 1 || input.QueryResultLimit > 100000 {
			return ExecutionPolicyResponse{}, fmt.Errorf("query_result_limit must be between 1 and 100000")
		}
	} else {
		if input.WorkflowConcurrency != 0 || input.NotebookConcurrency != 0 || input.QueryConcurrency != 0 || input.QueryPerEngineConcurrency != 0 || input.MaxQueryTimeout != 0 || input.QueryResultLimit != 0 {
			return ExecutionPolicyResponse{}, fmt.Errorf("tenant may override only default_query_timeout")
		}
		platform, err := s.repo.Get(ctx, "platform", nil)
		if err != nil {
			return ExecutionPolicyResponse{}, err
		}
		if platform == nil {
			platform = defaultExecutionPolicyModel("platform", nil)
		}
		input.MaxQueryTimeout, input.QueryResultLimit = platform.MaxQueryTimeout, platform.QueryResultLimit
		input.QueryConcurrency, input.QueryPerEngineConcurrency = platform.QueryConcurrency, platform.QueryPerEngineConcurrency
		input.WorkflowConcurrency, input.NotebookConcurrency = platform.WorkflowConcurrency, platform.NotebookConcurrency
	}
	value := &models.ExecutionPolicy{ScopeType: scope, TenantID: tenantID, DefaultQueryTimeout: input.DefaultQueryTimeout, MaxQueryTimeout: input.MaxQueryTimeout, QueryResultLimit: input.QueryResultLimit, QueryConcurrency: input.QueryConcurrency, QueryPerEngineConcurrency: input.QueryPerEngineConcurrency, WorkflowConcurrency: input.WorkflowConcurrency, NotebookConcurrency: input.NotebookConcurrency, UpdatedBy: updatedBy}
	if scope == "platform" {
		value.TenantID = nil
	}
	if err := s.repo.Save(ctx, value, input.Version); err != nil {
		return ExecutionPolicyResponse{}, err
	}
	if scope == "platform" && s.onChange != nil {
		s.onChange()
	}
	return responseFrom(value, scope, value.TenantID, false), nil
}

func defaultExecutionPolicyModel(scope string, tenantID *uint) *models.ExecutionPolicy {
	return &models.ExecutionPolicy{ScopeType: scope, TenantID: tenantID, DefaultQueryTimeout: 30, MaxQueryTimeout: 300, QueryResultLimit: 500, QueryConcurrency: 20, QueryPerEngineConcurrency: 5, WorkflowConcurrency: 2, NotebookConcurrency: 1}
}
func defaultExecutionPolicy(scope string, tenantID *uint) ExecutionPolicyResponse {
	return responseFrom(defaultExecutionPolicyModel(scope, tenantID), scope, tenantID, scope == "tenant")
}
func responseFrom(value *models.ExecutionPolicy, scope string, tenantID *uint, inherited bool) ExecutionPolicyResponse {
	return ExecutionPolicyResponse{ScopeType: scope, TenantID: tenantID, DefaultQueryTimeout: value.DefaultQueryTimeout, MaxQueryTimeout: value.MaxQueryTimeout, QueryResultLimit: value.QueryResultLimit, QueryConcurrency: value.QueryConcurrency, QueryPerEngineConcurrency: value.QueryPerEngineConcurrency, WorkflowConcurrency: value.WorkflowConcurrency, NotebookConcurrency: value.NotebookConcurrency, Version: value.Version, Inherited: inherited}
}
