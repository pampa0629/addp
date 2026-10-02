package service

import (
	"context"
	"time"

	"github.com/addp/common/logpipeline"
	"github.com/addp/common/runtimelog"
	"github.com/addp/system/internal/models"
)

func (s *ModuleRegistryService) SaveLogSources(ctx context.Context, r runtimelog.SourceReport, node string, retention time.Duration, now time.Time) error {
	if node == "" || r.Node != node || r.Validate(now) != nil || retention < 24*time.Hour {
		return ErrRuntimeLogsInvalid
	}
	return s.repo.SaveLogSources(ctx, r, retention, now)
}
func (s *ModuleRegistryService) ListLogSources(ctx context.Context, f models.ModuleLogSourceFilter, node string, now time.Time) (*models.ModuleLogSourcePage, error) {
	if f.Page < 1 || f.Page > 100000 || f.PageSize < 1 || f.PageSize > 100 {
		return nil, ErrRuntimeLogsInvalid
	}
	for _, value := range []string{f.Module, f.Node} {
		if value != "" && !logpipeline.Identity.MatchString(value) {
			return nil, ErrRuntimeLogsInvalid
		}
	}
	if f.Role != "" && f.Role != "backend" && f.Role != "worker" && f.Role != "scheduler" && f.Role != "ingress" {
		return nil, ErrRuntimeLogsInvalid
	}
	if f.From.IsZero() != f.To.IsZero() || (!f.From.IsZero() && (!f.From.Before(f.To) || f.To.Sub(f.From) > 7*24*time.Hour)) {
		return nil, ErrRuntimeLogsInvalid
	}
	return s.repo.ListLogSources(ctx, f, node, now)
}
func (s *ModuleRegistryService) ResolveLogIdentity(ctx context.Context, module, id string) (string, error) {
	if !logpipeline.Identity.MatchString(module) || !logpipeline.Identity.MatchString(id) {
		return "", ErrRuntimeLogsInvalid
	}
	return s.repo.ResolveLogIdentity(ctx, module, id, time.Now().UTC())
}

func (s *RuntimeLogService) Retention() (time.Duration, error) {
	if s.configInvalid {
		return 0, ErrRuntimeLogsUpstream
	}
	return s.retention, nil
}
