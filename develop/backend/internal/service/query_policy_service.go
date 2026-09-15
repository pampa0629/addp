package service

import (
	"context"
	"fmt"

	"github.com/addp/develop/backend/internal/models"
	"github.com/addp/develop/backend/internal/repository"
)

type QueryPolicyResponse struct {
	ScopeType                 string `json:"scope_type"`
	TenantID                  *uint  `json:"tenant_id,omitempty"`
	DefaultQueryTimeout       int    `json:"default_query_timeout"`
	MaxQueryTimeout           int    `json:"max_query_timeout"`
	QueryResultLimit          int    `json:"query_result_limit"`
	QueryConcurrency          int    `json:"query_concurrency"`
	QueryPerEngineConcurrency int    `json:"query_per_engine_concurrency"`
	Version                   uint64 `json:"version"`
	Inherited                 bool   `json:"inherited"`
}

type UpdateQueryPolicyInput struct {
	QueryConcurrency          int    `json:"query_concurrency"`
	QueryPerEngineConcurrency int    `json:"query_per_engine_concurrency"`
	Version                   uint64 `json:"version"`
	DefaultQueryTimeout       int    `json:"default_query_timeout" binding:"required"`
	MaxQueryTimeout           int    `json:"max_query_timeout"`
	QueryResultLimit          int    `json:"query_result_limit"`
}

type QueryPolicyService struct {
	repo     *repository.QueryPolicyRepository
	onChange func()
}

func (s *QueryPolicyService) SetOnChange(notify func()) { s.onChange = notify }

func (s *QueryPolicyService) ResolveConcurrency(ctx context.Context) (int, int, error) {
	value, err := s.Get(ctx, "platform", nil)
	return value.QueryConcurrency, value.QueryPerEngineConcurrency, err
}

func (s *QueryPolicyService) ResolveRuntime(ctx context.Context, tenantID uint) (defaultTimeout, maxTimeout, resultLimit int, err error) {
	platform, err := s.repo.Get(ctx, "platform", nil)
	if err != nil {
		return 0, 0, 0, err
	}
	if platform == nil {
		platform = defaultQueryPolicyModel("platform", nil)
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

func NewQueryPolicyService(repo *repository.QueryPolicyRepository) *QueryPolicyService {
	return &QueryPolicyService{repo: repo}
}

func (s *QueryPolicyService) Get(ctx context.Context, scope string, tenantID *uint) (QueryPolicyResponse, error) {
	value, err := s.repo.Get(ctx, scope, tenantID)
	if err != nil {
		return QueryPolicyResponse{}, err
	}
	if scope == "tenant" {
		platform, pErr := s.repo.Get(ctx, "platform", nil)
		if pErr != nil {
			return QueryPolicyResponse{}, pErr
		}
		if value == nil && platform == nil {
			return defaultQueryPolicy(scope, tenantID), nil
		}
		if platform == nil {
			platform = defaultQueryPolicyModel("platform", nil)
		}
		if value == nil {
			return responseFrom(platform, scope, tenantID, true), nil
		}
		platform.DefaultQueryTimeout = value.DefaultQueryTimeout
		platform.Version = value.Version
		return responseFrom(platform, scope, tenantID, false), nil
	}
	if value == nil {
		return defaultQueryPolicy(scope, tenantID), nil
	}
	return responseFrom(value, scope, tenantID, false), nil
}

func (s *QueryPolicyService) Update(ctx context.Context, scope string, tenantID *uint, input UpdateQueryPolicyInput, updatedBy uint) (QueryPolicyResponse, error) {
	if scope != "platform" && scope != "tenant" {
		return QueryPolicyResponse{}, fmt.Errorf("invalid query policy scope")
	}
	if input.DefaultQueryTimeout < 1 || input.DefaultQueryTimeout > 3600 {
		return QueryPolicyResponse{}, fmt.Errorf("default_query_timeout must be between 1 and 3600")
	}
	if scope == "platform" {
		if input.QueryConcurrency <= 0 || input.QueryPerEngineConcurrency <= 0 || input.QueryPerEngineConcurrency > input.QueryConcurrency {
			return QueryPolicyResponse{}, fmt.Errorf("query_concurrency and query_per_engine_concurrency must be positive, and per-engine must not exceed total")
		}
		if input.MaxQueryTimeout < input.DefaultQueryTimeout || input.MaxQueryTimeout > 86400 {
			return QueryPolicyResponse{}, fmt.Errorf("max_query_timeout must be >= default_query_timeout and <= 86400")
		}
		if input.QueryResultLimit < 1 || input.QueryResultLimit > 100000 {
			return QueryPolicyResponse{}, fmt.Errorf("query_result_limit must be between 1 and 100000")
		}
	} else {
		if input.QueryConcurrency != 0 || input.QueryPerEngineConcurrency != 0 || input.MaxQueryTimeout != 0 || input.QueryResultLimit != 0 {
			return QueryPolicyResponse{}, fmt.Errorf("tenant may override only default_query_timeout")
		}
		platform, err := s.repo.Get(ctx, "platform", nil)
		if err != nil {
			return QueryPolicyResponse{}, err
		}
		if platform == nil {
			platform = defaultQueryPolicyModel("platform", nil)
		}
		input.MaxQueryTimeout, input.QueryResultLimit = platform.MaxQueryTimeout, platform.QueryResultLimit
		input.QueryConcurrency, input.QueryPerEngineConcurrency = platform.QueryConcurrency, platform.QueryPerEngineConcurrency
	}
	value := &models.QueryPolicy{ScopeType: scope, TenantID: tenantID, DefaultQueryTimeout: input.DefaultQueryTimeout, MaxQueryTimeout: input.MaxQueryTimeout, QueryResultLimit: input.QueryResultLimit, QueryConcurrency: input.QueryConcurrency, QueryPerEngineConcurrency: input.QueryPerEngineConcurrency, UpdatedBy: updatedBy}
	if scope == "platform" {
		value.TenantID = nil
	}
	if err := s.repo.Save(ctx, value, input.Version); err != nil {
		return QueryPolicyResponse{}, err
	}
	if scope == "platform" && s.onChange != nil {
		s.onChange()
	}
	return responseFrom(value, scope, value.TenantID, false), nil
}

func defaultQueryPolicyModel(scope string, tenantID *uint) *models.QueryPolicy {
	return &models.QueryPolicy{ScopeType: scope, TenantID: tenantID, DefaultQueryTimeout: 30, MaxQueryTimeout: 300, QueryResultLimit: 500, QueryConcurrency: 20, QueryPerEngineConcurrency: 5}
}
func defaultQueryPolicy(scope string, tenantID *uint) QueryPolicyResponse {
	return responseFrom(defaultQueryPolicyModel(scope, tenantID), scope, tenantID, scope == "tenant")
}
func responseFrom(value *models.QueryPolicy, scope string, tenantID *uint, inherited bool) QueryPolicyResponse {
	return QueryPolicyResponse{ScopeType: scope, TenantID: tenantID, DefaultQueryTimeout: value.DefaultQueryTimeout, MaxQueryTimeout: value.MaxQueryTimeout, QueryResultLimit: value.QueryResultLimit, QueryConcurrency: value.QueryConcurrency, QueryPerEngineConcurrency: value.QueryPerEngineConcurrency, Version: value.Version, Inherited: inherited}
}
