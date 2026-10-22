package iam

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"net"
	"syscall"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/logger"
	"github.com/addp/common/middleware/requestid"
	"github.com/jackc/pgx/v5/pgconn"
)

func (s *AuthContextService) authContextPoolStats() (sql.DBStats, bool) {
	pool, err := s.repository.db.DB()
	if err != nil {
		return sql.DBStats{}, false
	}
	return pool.Stats(), true
}

// This diagnostic never affects a decision. Stage durations include scheduling
// and database waits; pool deltas include all concurrent users of the same pool.
func (s *AuthContextService) logSlowAuthContextResolution(ctx context.Context, started, finished time.Time,
	stages map[string]time.Duration, before sql.DBStats, available bool) {
	if finished.Sub(started) < 100*time.Millisecond {
		return
	}
	wall := make(map[string]float64, len(stages))
	for stage, duration := range stages {
		wall[stage] = float64(duration) / float64(time.Millisecond)
	}
	after, afterAvailable := s.authContextPoolStats()
	available = available && afterAvailable
	fields := []any{"event", "iam.auth_context.resolve_slow",
		"elapsed_ms", float64(finished.Sub(started)) / float64(time.Millisecond),
		"stage_wall_ms", wall, "pool_stats_available", available}
	if id := requestid.FromContext(ctx); id != "" {
		fields = append(fields, "request_id", id)
	}
	if available {
		fields = append(fields, "pool_open", after.OpenConnections, "pool_in_use", after.InUse,
			"pool_idle", after.Idle, "pool_max_open", after.MaxOpenConnections,
			"pool_wait_count_total", after.WaitCount,
			"pool_wait_duration_total_ms", float64(after.WaitDuration)/float64(time.Millisecond),
			"pool_wait_count_delta", after.WaitCount-before.WaitCount,
			"pool_wait_duration_delta_ms", float64(after.WaitDuration-before.WaitDuration)/float64(time.Millisecond))
	}
	logger.L().InfoContext(ctx, "AuthContext resolution slow", fields...)
}

func (s *AuthContextService) logAuthContextResolutionFailure(ctx context.Context, err error, startedAt time.Time, stage string, stageStartedAt time.Time) {
	// Authentication decisions and dependency failures have separate events.
	// Never log the credential, snapshot, SQL or raw error contents.
	if errors.Is(err, commonapi.ErrUnauthorized) {
		logAuthContextRejection(ctx, err)
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
	cause := err
	var connection *pgconn.ConnectError
	if errors.As(err, &connection) {
		if kind == "internal" {
			kind = "database_connection"
		}
		cause = authContextConnectionFailure(connection)
	}
	switch {
	case errors.Is(err, context.Canceled):
		kind = "context_canceled"
	case errors.Is(err, context.DeadlineExceeded):
		kind = "deadline_exceeded"
	case errors.As(cause, &timeout) && timeout.Timeout():
		kind = "network_timeout"
	case errors.Is(cause, syscall.ECONNREFUSED):
		kind = "connection_refused"
	case errors.Is(cause, syscall.ECONNRESET):
		kind = "connection_reset"
	case errors.Is(cause, io.EOF), errors.Is(cause, io.ErrUnexpectedEOF):
		kind = "connection_closed"
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

func logAuthContextRejection(ctx context.Context, err error) {
	reason := "authentication_required"
	var validation *CredentialValidationError
	if errors.As(err, &validation) {
		switch validation.Reason {
		case CredentialInvalidFormat, CredentialInvalidNotFound, CredentialInvalidTokenExpired,
			CredentialInvalidTokenRevoked, CredentialInvalidSourceTokenExpired, CredentialInvalidSourceTokenRevoked,
			CredentialInvalidFamilyExpired, CredentialInvalidFamilyRevoked, CredentialInvalidPrincipalInactive,
			CredentialInvalidAuthorizationVersion, CredentialInvalidContext, CredentialInvalidMembershipInactive,
			CredentialInvalidTenantInactive, CredentialInvalidUnsupportedTokenType:
			reason = string(validation.Reason)
		}
	}
	fields := []any{"event", "iam.auth_context.rejected", "reason", reason}
	if id := requestid.FromContext(ctx); id != "" {
		fields = append(fields, "request_id", id)
	}
	if validation != nil && validation.record != nil {
		r := validation.record
		switch r.kind {
		case "access_token", "resource_access_ticket", "delegated_access_token":
			if r.credentialID > 0 && r.familyID > 0 {
				fields = append(fields, "credential_kind", r.kind,
					"credential_id", formatIAMID(r.credentialID), "family_id", formatIAMID(r.familyID))
			}
		}
	}
	logger.L().InfoContext(ctx, "AuthContext credential rejected", fields...)
}

func authContextConnectionFailure(connection *pgconn.ConnectError) error {
	cause := connection.Unwrap()
	// pgx joins failures in address-attempt order. A fast IPv6 refusal must not
	// obscure the later IPv4 timeout or disconnect that ended the connection.
	for current := cause; current != nil; current = errors.Unwrap(current) {
		if joined, ok := current.(interface{ Unwrap() []error }); ok {
			failures := joined.Unwrap()
			if len(failures) > 0 {
				return failures[len(failures)-1]
			}
		}
	}
	return cause
}
