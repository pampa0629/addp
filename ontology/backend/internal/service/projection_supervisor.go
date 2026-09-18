package service

import (
	"context"
	"errors"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/addp/common/execution"
	"github.com/addp/ontology/internal/repository"
)

// A single bounded slot per Backend, using Common's durable queue. Multiple
// Backends coordinate through the same PG claim; there is no second queue.
type ProjectionSupervisor struct {
	repo     *repository.RevisionRepository
	executor *ProjectionExecutor
	owner    string
	running  atomic.Bool
}

func NewProjectionSupervisor(repo *repository.RevisionRepository, executor *ProjectionExecutor, owner string) (*ProjectionSupervisor, error) {
	if repo == nil || executor == nil || owner == "" {
		return nil, repository.ErrInvalid
	}
	return &ProjectionSupervisor{repo: repo, executor: executor, owner: owner}, nil
}

func (s *ProjectionSupervisor) Run(ctx context.Context, canClaim func() bool) error {
	if canClaim == nil || !s.running.CompareAndSwap(false, true) {
		return repository.ErrInvalid
	}
	defer s.running.Store(false)
	for ctx.Err() == nil {
		if canClaim() {
			err := s.repo.RecoverProjections(ctx)
			if err == nil {
				var lease *execution.Lease
				lease, err = s.repo.ClaimProjection(ctx, s.owner, 30*time.Second)
				if err == nil && lease != nil {
					err = s.execute(ctx, *lease)
				}
			}
			if err != nil && ctx.Err() == nil {
				slog.Warn("Ontology projection execution did not complete", "error", err)
			}
		}
		timer := time.NewTimer(250 * time.Millisecond)
		select {
		case <-ctx.Done():
		case <-timer.C:
		}
		timer.Stop()
	}
	return ctx.Err()
}

func (s *ProjectionSupervisor) execute(ctx context.Context, lease execution.Lease) error {
	work, cancel := context.WithTimeout(execution.ContextWithLease(ctx, lease), 25*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-work.Done():
				done <- nil
				return
			case <-ticker.C:
				if err := s.repo.RenewProjection(work, lease, 30*time.Second); err != nil {
					terminal, checkErr := s.repo.ProjectionTerminal(work, lease)
					if checkErr == nil && terminal {
						done <- nil
						return
					}
					cancel()
					done <- err
					return
				}
			}
		}
	}()
	err := s.executor.Execute(work, lease)
	cancel()
	return errors.Join(err, <-done)
}
