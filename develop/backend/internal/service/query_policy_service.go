package service

import (
	"context"
	"fmt"

	"github.com/addp/develop/backend/internal/models"
	"github.com/addp/develop/backend/internal/repository"
)

type QueryPolicyResponse struct {
	ScopeType           string `json:"scope_type"`
	TenantID            *uint  `json:"tenant_id,omitempty"`
	DefaultQueryTimeout int    `json:"default_query_timeout"`
	MaxQueryTimeout     int    `json:"max_query_timeout"`
	QueryResultLimit    int    `json:"query_result_limit"`
	Version             uint64 `json:"version"`
	Inherited           bool   `json:"inherited"`
}

type UpdateQueryPolicyInput struct {
	Version             uint64 `json:"version"`
	DefaultQueryTimeout int    `json:"default_query_timeout" binding:"required"`
	MaxQueryTimeout     int    `json:"max_query_timeout"`
	QueryResultLimit    int    `json:"query_result_limit"`
}

type QueryPolicyService struct {
	repo *repository.QueryPolicyRepository
}

func (s *QueryPolicyService) ResolveRuntime(ctx context.Context, tenantID uint) (defaultTimeout, maxTimeout, resultLimit int, err error) {
	platform, err := s.repo.Get(ctx, "platform", nil)
	if err != nil {
		return 0, 0, 0, err
	}
	if platform == nil {
		platform = defaultQueryPolicyModel("platform", nil)
	}
	defaultTimeout := platform.DefaultQueryTimeout
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
		return QueryPolicyResponse{ScopeType: scope, TenantID: tenantID, DefaultQueryTimeout: value.DefaultQueryTimeout, MaxQueryTimeout: platform.MaxQueryTimeout, QueryResultLimit: platform.QueryResultLimit, Version: value.Version}, nil
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
		if input.MaxQueryTimeout < input.DefaultQueryTimeout || input.MaxQueryTimeout > 86400 {
			return QueryPolicyResponse{}, fmt.Errorf("max_query_timeout must be >= default_query_timeout and <= 86400")
		}
		if input.QueryResultLimit < 1 || input.QueryResultLimit > 100000 {
			return QueryPolicyResponse{}, fmt.Errorf("query_result_limit must be between 1 and 100000")
		}
	} else {
		platform, err := s.repo.Get(ctx, "platform", nil)
		if err != nil {
			return QueryPolicyResponse{}, err
		}
		if platform == nil {
			platform = defaultQueryPolicyModel("platform", nil)
		}
		input.MaxQueryTimeout, input.QueryResultLimit = platform.MaxQueryTimeout, platform.QueryResultLimit
	}
	value := &models.QueryPolicy{ScopeType: scope, TenantID: tenantID, DefaultQueryTimeout: input.DefaultQueryTimeout, MaxQueryTimeout: input.MaxQueryTimeout, QueryResultLimit: input.QueryResultLimit, UpdatedBy: updatedBy}
	if scope == "platform" {
		value.TenantID = nil
	}
	if err := s.repo.Save(ctx, value, input.Version); err != nil {
		return QueryPolicyResponse{}, err
	}
	return responseFrom(value, scope, value.TenantID, false), nil
}

func defaultQueryPolicyModel(scope string, tenantID *uint) *models.QueryPolicy {
	return &models.QueryPolicy{ScopeType: scope, TenantID: tenantID, DefaultQueryTimeout: 30, MaxQueryTimeout: 300, QueryResultLimit: 500}
}
func defaultQueryPolicy(scope string, tenantID *uint) QueryPolicyResponse {
	return responseFrom(defaultQueryPolicyModel(scope, tenantID), scope, tenantID, scope == "tenant")
}
func responseFrom(value *models.QueryPolicy, scope string, tenantID *uint, inherited bool) QueryPolicyResponse {
	return QueryPolicyResponse{ScopeType: scope, TenantID: tenantID, DefaultQueryTimeout: value.DefaultQueryTimeout, MaxQueryTimeout: value.MaxQueryTimeout, QueryResultLimit: value.QueryResultLimit, Version: value.Version, Inherited: inherited}
}
