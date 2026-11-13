package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/addp/common/execution"
	"github.com/addp/common/httptrace"
	"github.com/addp/common/logger"
)

var ErrExecutionOwnerUnavailable = errors.New("execution owner authorization unavailable")

type ExecutionReadResolver interface {
	Resolve(context.Context, []string, int, int64, string) ([]execution.ReadScope, error)
}

type activeModuleLister interface {
	ListActiveModules(context.Context) ([]*ModuleInfo, error)
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

type executionReadFailure struct {
	stage, reason string
	status        int
}

func (f executionReadFailure) log(ctx context.Context, owner string) {
	if !ownerModulePattern.MatchString(owner) {
		owner = ""
	}
	attrs := []any{"observation", "first_observed", "owner_module", owner, "stage", f.stage, "reason", f.reason}
	if f.status != 0 {
		attrs = append(attrs, "http_status", f.status)
	}
	logger.L().ErrorContext(ctx, "execution.owner_authorization.failed", attrs...)
}

func executionReadErrorReason(err error) string {
	if errors.Is(err, context.Canceled) {
		return "cancelled"
	}
	var timeout net.Error
	if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &timeout) && timeout.Timeout()) {
		return "timeout"
	}
	return "unavailable"
}

// Resolve forwards the verified User Bearer only within this synchronous BFF
// request. It never forwards caller-supplied subject fields or logs responses.
func (a *ExecutionReadAuthorizer) Resolve(ctx context.Context, owners []string, tenantID int, principalID int64, bearer string) ([]execution.ReadScope, error) {
	if len(owners) == 0 {
		return []execution.ReadScope{}, nil
	}
	if a.registry == nil || tenantID <= 0 || principalID <= 0 || !strings.HasPrefix(bearer, "Bearer ") {
		executionReadFailure{stage: "input", reason: "invalid_input"}.log(ctx, "")
		return nil, ErrExecutionOwnerUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	modules, err := a.registry.ListActiveModules(ctx)
	if err != nil {
		executionReadFailure{stage: "registry", reason: executionReadErrorReason(err)}.log(ctx, "")
		return nil, ErrExecutionOwnerUnavailable
	}
	// Owners are independent but share one deadline and one all-or-nothing result.
	// A worker pool bounds upstream load without accumulating serial latency.
	result := make([]execution.ReadScope, len(owners))
	jobs := make(chan int)
	var workers sync.WaitGroup
	var failed atomic.Bool
	for range min(4, len(owners)) {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for index := range jobs {
				if ctx.Err() != nil {
					return
				}
				scope, err := a.resolveOwner(ctx, modules, owners[index], tenantID, principalID, bearer)
				if err != nil {
					if failed.CompareAndSwap(false, true) {
						cancel()
						err.log(ctx, owners[index])
					}
					return
				}
				result[index] = scope
			}
		}()
	}
dispatch:
	for index := range owners {
		select {
		case jobs <- index:
		case <-ctx.Done():
			break dispatch
		}
	}
	close(jobs)
	workers.Wait()
	if failed.Load() || ctx.Err() != nil {
		if failed.CompareAndSwap(false, true) {
			executionReadFailure{stage: "dispatch", reason: executionReadErrorReason(ctx.Err())}.log(ctx, "")
		}
		return nil, ErrExecutionOwnerUnavailable
	}
	return result, nil
}

func (a *ExecutionReadAuthorizer) resolveOwner(ctx context.Context, modules []*ModuleInfo, owner string, tenantID int, principalID int64, bearer string) (execution.ReadScope, *executionReadFailure) {
	if !ownerModulePattern.MatchString(owner) {
		return execution.ReadScope{}, &executionReadFailure{stage: "owner", reason: "invalid_owner"}
	}
	endpoint := ""
	for _, module := range modules {
		if module == nil || module.ModuleName != owner || !module.Enabled {
			continue
		}
		for _, instance := range module.Instances {
			if instance.Role != ModuleRuntimeRoleBackend || instance.Status != "up" || !instance.LeaseExpiresAt.After(a.now()) {
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
		return execution.ReadScope{}, &executionReadFailure{stage: "owner", reason: "no_active_instance"}
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return execution.ReadScope{}, &executionReadFailure{stage: "request", reason: "invalid_request"}
	}
	request.Header.Set("Authorization", bearer)
	response, err := httptrace.Do(a.httpClient, request, endpoint)
	if err != nil {
		return execution.ReadScope{}, &executionReadFailure{stage: "transport", reason: executionReadErrorReason(err)}
	}
	body, readErr := io.ReadAll(io.LimitReader(response.Body, 32*1024+1))
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return execution.ReadScope{}, &executionReadFailure{stage: "response", reason: "unexpected_status", status: response.StatusCode}
	}
	if readErr != nil {
		reason := executionReadErrorReason(readErr)
		if reason == "unavailable" {
			reason = "read_failed"
		}
		return execution.ReadScope{}, &executionReadFailure{stage: "response", reason: reason, status: response.StatusCode}
	}
	if len(body) > 32*1024 {
		return execution.ReadScope{}, &executionReadFailure{stage: "response", reason: "too_large", status: response.StatusCode}
	}
	var scope execution.ReadScope
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&scope) != nil || decoder.Decode(new(any)) != io.EOF || scope.Module != owner || scope.TenantID != tenantID || scope.PrincipalID != principalID || len(scope.Grants) > 100 {
		return execution.ReadScope{}, &executionReadFailure{stage: "response", reason: "invalid_scope", status: response.StatusCode}
	}
	seen := map[string]bool{}
	for _, grant := range scope.Grants {
		if !ownerModulePattern.MatchString(grant.TaskType) || seen[grant.TaskType] {
			return execution.ReadScope{}, &executionReadFailure{stage: "response", reason: "invalid_scope", status: response.StatusCode}
		}
		seen[grant.TaskType] = true
	}
	return scope, nil
}
