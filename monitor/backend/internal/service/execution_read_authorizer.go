package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	commonclient "github.com/addp/common/client"
	"github.com/addp/common/execution"
)

var ErrExecutionOwnerUnavailable = errors.New("execution owner authorization unavailable")

type ExecutionReadResolver interface {
	Resolve(context.Context, []string, int, int64, string) ([]execution.ReadScope, error)
}

type activeModuleLister interface {
	ListActiveModules(context.Context) ([]*commonclient.ModuleInfo, error)
}

type ExecutionReadAuthorizer struct {
	registry   activeModuleLister
	httpClient *http.Client
	now        func() time.Time
}

func NewExecutionReadAuthorizer(registry activeModuleLister) *ExecutionReadAuthorizer {
	return &ExecutionReadAuthorizer{registry: registry, now: time.Now, httpClient: &http.Client{
		Timeout:       5 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
}

var ownerModulePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,49}$`)

// Resolve forwards the verified User Bearer only within this synchronous BFF
// request. It never forwards caller-supplied subject fields or logs responses.
func (a *ExecutionReadAuthorizer) Resolve(ctx context.Context, owners []string, tenantID int, principalID int64, bearer string) ([]execution.ReadScope, error) {
	if len(owners) == 0 {
		return []execution.ReadScope{}, nil
	}
	if a.registry == nil || tenantID <= 0 || principalID <= 0 || !strings.HasPrefix(bearer, "Bearer ") {
		return nil, ErrExecutionOwnerUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	modules, err := a.registry.ListActiveModules(ctx)
	if err != nil {
		return nil, ErrExecutionOwnerUnavailable
	}
	result := make([]execution.ReadScope, 0, len(owners))
	for _, owner := range owners {
		if !ownerModulePattern.MatchString(owner) {
			return nil, ErrExecutionOwnerUnavailable
		}
		endpoint := ""
		for _, module := range modules {
			if module == nil || module.ModuleName != owner || !module.Enabled {
				continue
			}
			for _, instance := range module.Instances {
				if instance.Role != commonclient.ModuleRuntimeRoleBackend || instance.Status != "up" || !instance.LeaseExpiresAt.After(a.now()) {
					continue
				}
				base, parseErr := url.Parse(instance.ModuleURL)
				if parseErr == nil && (base.Scheme == "http" || base.Scheme == "https") && base.Host != "" && base.User == nil && base.RawQuery == "" && base.Fragment == "" {
					endpoint = strings.TrimRight(instance.ModuleURL, "/") + "/api/v1/" + owner + "/execution-read-scope"
					break
				}
			}
		}
		if endpoint == "" {
			return nil, ErrExecutionOwnerUnavailable
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return nil, ErrExecutionOwnerUnavailable
		}
		request.Header.Set("Authorization", bearer)
		response, err := a.httpClient.Do(request)
		if err != nil {
			return nil, ErrExecutionOwnerUnavailable
		}
		body, readErr := io.ReadAll(io.LimitReader(response.Body, 32*1024+1))
		response.Body.Close()
		var scope execution.ReadScope
		if response.StatusCode != http.StatusOK || readErr != nil || len(body) > 32*1024 || json.Unmarshal(body, &scope) != nil || scope.Module != owner || scope.TenantID != tenantID || scope.PrincipalID != principalID || len(scope.Grants) > 100 {
			return nil, ErrExecutionOwnerUnavailable
		}
		seen := map[string]bool{}
		for _, grant := range scope.Grants {
			if !ownerModulePattern.MatchString(grant.TaskType) || seen[grant.TaskType] {
				return nil, ErrExecutionOwnerUnavailable
			}
			seen[grant.TaskType] = true
		}
		result = append(result, scope)
	}
	return result, nil
}

func (s *ExecutionQueryService) SetReadResolver(resolver ExecutionReadResolver) {
	s.readResolver = resolver
}

func (s *ExecutionQueryService) AuthorizeReadRequest(ctx context.Context, tenantID int, principalID int64, bearer, module string) (context.Context, error) {
	owners, err := s.repo.ListOwnerModules(ctx, tenantID)
	if err != nil {
		return nil, ErrExecutionOwnerUnavailable
	}
	if module != "" {
		selected := []string{}
		for _, owner := range owners {
			if owner == module {
				selected = append(selected, owner)
			}
		}
		owners = selected
	}
	if len(owners) == 0 {
		return execution.WithReadScopes(ctx, []execution.ReadScope{}), nil
	}
	if s.readResolver == nil {
		return nil, ErrExecutionOwnerUnavailable
	}
	scopes, err := s.readResolver.Resolve(ctx, owners, tenantID, principalID, bearer)
	if err != nil {
		return nil, ErrExecutionOwnerUnavailable
	}
	return execution.WithReadScopes(ctx, scopes), nil
}
