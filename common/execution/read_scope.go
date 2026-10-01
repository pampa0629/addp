package execution

import (
	"context"
	"fmt"
	"strings"

	"gorm.io/gorm"
)

// ReadGrant describes an Owner decision, never a client supplied SQL predicate.
type ReadGrant struct {
	TaskType    string `json:"task_type"`
	TaskHistory bool   `json:"task_history"`
	OwnAdHoc    bool   `json:"own_ad_hoc"`
}

type ReadScope struct {
	Module      string      `json:"module"`
	TenantID    int         `json:"tenant_id"`
	PrincipalID int64       `json:"principal_id"`
	Grants      []ReadGrant `json:"grants"`
}

type readScopesKey struct{}

// WithReadScopes freezes decisions only for the current authenticated request.
func WithReadScopes(ctx context.Context, scopes []ReadScope) context.Context {
	copyScopes := make([]ReadScope, len(scopes))
	for i, scope := range scopes {
		copyScopes[i] = scope
		copyScopes[i].Grants = append([]ReadGrant{}, scope.Grants...)
	}
	return context.WithValue(ctx, readScopesKey{}, copyScopes)
}

func ReadScopesFromContext(ctx context.Context) ([]ReadScope, bool) {
	scopes, ok := ctx.Value(readScopesKey{}).([]ReadScope)
	return scopes, ok
}

// ReadPredicate uses bound values only. No decision context means an internal
// repository caller; user API composition must install even an empty scope.
func ReadPredicate(ctx context.Context) (string, []interface{}) {
	scopes, ok := ReadScopesFromContext(ctx)
	if !ok {
		return "", nil
	}
	parts := []string{}
	args := []interface{}{}
	for _, scope := range scopes {
		if scope.TenantID <= 0 || scope.PrincipalID <= 0 || scope.Module == "" {
			continue
		}
		for _, grant := range scope.Grants {
			if grant.TaskType == "" {
				continue
			}
			conditions := []string{}
			if grant.TaskHistory {
				conditions = append(conditions, "(source_task_id IS NOT NULL AND source_task_id <> '')")
			}
			if grant.OwnAdHoc {
				conditions = append(conditions, "(source_task_id IS NULL AND triggered_by = ?)")
			}
			if len(conditions) == 0 {
				continue
			}
			parts = append(parts, "(tenant_id = ? AND module = ? AND task_type = ? AND ("+strings.Join(conditions, " OR ")+"))")
			args = append(args, scope.TenantID, scope.Module, grant.TaskType)
			if grant.OwnAdHoc {
				args = append(args, scope.PrincipalID)
			}
		}
	}
	if len(parts) == 0 {
		return "1 = 0", nil
	}
	return "(" + strings.Join(parts, " OR ") + ")", args
}

func ApplyReadScopes(ctx context.Context, db *gorm.DB) *gorm.DB {
	predicate, args := ReadPredicate(ctx)
	if predicate != "" {
		return db.Where(predicate, args...)
	}
	return db
}

func (r *TaskExecutionRepository) ListOwnerModules(ctx context.Context, tenantID int) ([]string, error) {
	if tenantID <= 0 {
		return nil, fmt.Errorf("tenant context required")
	}
	var modules []string
	err := r.db.WithContext(ctx).Model(&TaskExecution{}).Where("tenant_id = ?", tenantID).Distinct("module").Order("module").Pluck("module", &modules).Error
	return modules, err
}
