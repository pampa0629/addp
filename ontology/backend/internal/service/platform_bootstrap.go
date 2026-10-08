package service

import (
	"context"
	"time"

	"github.com/addp/ontology/internal/platform"
	"github.com/addp/ontology/internal/repository"
)

// PublishOnReady runs once per deployment process. It never retries uncertain
// writes or adds publication success as a global Tenant readiness dependency.
func (p *PlatformPublisher) PublishOnReady(ctx context.Context, canPublish func() bool, compile func() (*platform.Snapshot, error)) error {
	if p == nil || canPublish == nil || compile == nil {
		return repository.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	snapshot, err := compile()
	if err != nil {
		return err
	}
	if _, err := snapshot.Context(); err != nil {
		return repository.ErrInvalid
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if canPublish() {
			_, err := p.Publish(ctx, snapshot)
			return err
		}
		timer := time.NewTimer(250 * time.Millisecond)
		select {
		case <-ctx.Done():
		case <-timer.C:
		}
		timer.Stop()
	}
}
