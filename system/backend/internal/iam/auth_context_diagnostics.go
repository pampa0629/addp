package iam

import (
	"context"
	"errors"
	"net"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/logger"
	"github.com/addp/common/middleware/requestid"
)

func (s *AuthContextService) logAuthContextResolutionFailure(ctx context.Context, err error, startedAt time.Time, stage string, stageStartedAt time.Time) {
	// Invalid credentials are expected authentication decisions, not dependency
	// failures. Never log the credential, snapshot, SQL or raw error contents.
	if errors.Is(err, commonapi.ErrUnauthorized) {
		return
	}
	kind := "internal"
	var sqlState interface{ SQLState() string }
	var timeout net.Error
	state := ""
	if errors.As(err, &sqlState) {
		code := sqlState.SQLState()
		valid := len(code) == 5
		for _, character := range code {
			valid = valid && (character >= '0' && character <= '9' || character >= 'A' && character <= 'Z')
		}
		if valid {
			state, kind = code, "database"
		}
	}
	switch {
	case errors.Is(err, context.Canceled):
		kind = "context_canceled"
	case errors.Is(err, context.DeadlineExceeded):
		kind = "deadline_exceeded"
	case errors.As(err, &timeout) && timeout.Timeout():
		kind = "network_timeout"
	}
	now := time.Now()
	fields := []any{
		"event", "iam.auth_context.resolve_failed",
		"stage", stage,
		"elapsed_ms", now.Sub(startedAt).Milliseconds(),
		"stage_elapsed_ms", now.Sub(stageStartedAt).Milliseconds(),
		"error_kind", kind,
	}
	if id := requestid.FromContext(ctx); id != "" {
		fields = append(fields, "request_id", id)
	}
	if state != "" {
		fields = append(fields, "sqlstate", state)
	}
	if pool, poolErr := s.repository.db.DB(); poolErr == nil {
		stats := pool.Stats()
		// These are process-wide pool observations at failure time, not waiting
		// attributed to this request. Reading Stats does not acquire a connection.
		fields = append(fields,
			"pool_open", stats.OpenConnections, "pool_in_use", stats.InUse,
			"pool_idle", stats.Idle, "pool_max_open", stats.MaxOpenConnections,
			"pool_wait_count_total", stats.WaitCount,
			"pool_wait_duration_total_ms", stats.WaitDuration.Milliseconds(),
		)
	}
	logger.L().ErrorContext(ctx, "AuthContext resolution failed", fields...)
}
