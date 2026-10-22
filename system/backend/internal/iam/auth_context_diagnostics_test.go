package iam

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"syscall"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	commonlogger "github.com/addp/common/logger"
	"github.com/addp/common/middleware/requestid"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

func authContextDiagnosticFixture(t *testing.T) (*AuthContextService, *bytes.Buffer) {
	t.Helper()
	// SQLite supplies only a real database/sql transaction and connection pool.
	// This fixture never executes or substitutes for PostgreSQL authorization SQL.
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: gormlogger.Default.LogMode(gormlogger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pool.Close() })
	var output bytes.Buffer
	commonlogger.Init(commonlogger.Options{Writer: &output})
	t.Cleanup(func() { commonlogger.Init(commonlogger.Options{}) })
	service, err := NewAuthContextService(NewRepository(db))
	if err != nil {
		t.Fatal(err)
	}
	return service, &output
}

func authContextDiagnosticRecord(t *testing.T, output *bytes.Buffer, stage, kind string) map[string]any {
	t.Helper()
	var record map[string]any
	if err := json.Unmarshal(output.Bytes(), &record); err != nil {
		t.Fatalf("expected exactly one diagnostic record: %v; output=%s", err, output.String())
	}
	if record["event"] != "iam.auth_context.resolve_failed" || record["request_id"] != "auth-503-request" ||
		record["stage"] != stage || record["error_kind"] != kind {
		t.Fatalf("diagnostic attribution = %#v", record)
	}
	for _, field := range []string{"elapsed_ms", "stage_elapsed_ms", "pool_open", "pool_in_use", "pool_idle", "pool_max_open", "pool_wait_count_total", "pool_wait_duration_total_ms"} {
		if value, ok := record[field].(float64); !ok || value < 0 {
			t.Fatalf("missing or invalid %s: %#v", field, record[field])
		}
	}
	allowed := map[string]bool{}
	for _, field := range []string{"time", "level", "msg", "event", "request_id", "stage", "error_kind", "sqlstate", "elapsed_ms", "stage_elapsed_ms", "pool_open", "pool_in_use", "pool_idle", "pool_max_open", "pool_wait_count_total", "pool_wait_duration_total_ms"} {
		allowed[field] = true
	}
	for field := range record {
		if !allowed[field] {
			t.Fatalf("unexpected diagnostic field %q", field)
		}
	}
	if strings.Contains(output.String(), "private-sentinel") || strings.Contains(output.String(), "addp_at_") {
		t.Fatalf("diagnostic exposed credential or error contents: %s", output.String())
	}
	return record
}

func authContextRejectionRecord(t *testing.T, output *bytes.Buffer, reason CredentialInvalidReason) map[string]any {
	t.Helper()
	var record map[string]any
	if err := json.Unmarshal(output.Bytes(), &record); err != nil {
		t.Fatalf("expected exactly one rejection record: %v", err)
	}
	if record["event"] != "iam.auth_context.rejected" || record["level"] != "INFO" ||
		record["reason"] != string(reason) || record["request_id"] != "auth-401-request" {
		t.Fatalf("rejection attribution = %#v", record)
	}
	allowed := map[string]bool{}
	for _, field := range []string{"time", "level", "msg", "event", "request_id", "reason", "credential_kind", "credential_id", "family_id"} {
		allowed[field] = true
	}
	for field := range record {
		if !allowed[field] {
			t.Fatalf("unexpected rejection field %q", field)
		}
	}
	if strings.Contains(output.String(), "private-sentinel") || strings.Contains(output.String(), "addp_at_") {
		t.Fatal("rejection exposed credential or error contents")
	}
	return record
}

func TestAuthContextRejectionDiagnostics(t *testing.T) {
	for _, reason := range []CredentialInvalidReason{
		CredentialInvalidFormat, CredentialInvalidNotFound, CredentialInvalidTokenExpired,
		CredentialInvalidTokenRevoked, CredentialInvalidSourceTokenExpired, CredentialInvalidSourceTokenRevoked,
		CredentialInvalidFamilyExpired, CredentialInvalidFamilyRevoked, CredentialInvalidPrincipalInactive,
		CredentialInvalidAuthorizationVersion, CredentialInvalidContext, CredentialInvalidMembershipInactive,
		CredentialInvalidTenantInactive, CredentialInvalidUnsupportedTokenType,
	} {
		t.Run(string(reason), func(t *testing.T) {
			service, output := authContextDiagnosticFixture(t)
			ctx := context.WithValue(t.Context(), requestid.RequestIDKey, "auth-401-request")
			original := invalidCredential(reason)
			_, err := service.resolveSessionCredential(ctx, func(*Repository) (*SessionCredentialAuthSnapshot, error) {
				return nil, original
			}, nil)
			if err != original || err.Error() != commonapi.ErrUnauthorized.Error() {
				t.Fatal("rejection diagnostics changed the authentication error")
			}
			record := authContextRejectionRecord(t, output, reason)
			if record["credential_id"] != nil || record["family_id"] != nil || record["credential_kind"] != nil {
				t.Fatal("rejection fabricated a record that was not loaded")
			}
		})
	}
	t.Run("unknown format before database access", func(t *testing.T) {
		service, output := authContextDiagnosticFixture(t)
		ctx := context.WithValue(t.Context(), requestid.RequestIDKey, "auth-401-request")
		_, err := service.ResolveAuthContext(ctx, "private-sentinel")
		if !errors.Is(err, commonapi.ErrUnauthorized) {
			t.Fatal("invalid credential changed the HTTP error category")
		}
		authContextRejectionRecord(t, output, CredentialInvalidFormat)
	})
	for _, original := range []error{
		fmt.Errorf("private-sentinel addp_at_private-sentinel: %w", commonapi.ErrUnauthorized),
		invalidCredential(CredentialInvalidReason("private-sentinel addp_at_private-sentinel")),
	} {
		t.Run("unrecognized cause is redacted", func(t *testing.T) {
			service, output := authContextDiagnosticFixture(t)
			ctx := context.WithValue(t.Context(), requestid.RequestIDKey, "auth-401-request")
			_, err := service.resolveSessionCredential(ctx, func(*Repository) (*SessionCredentialAuthSnapshot, error) {
				return nil, original
			}, nil)
			if err != original {
				t.Fatal("diagnostic changed the original error")
			}
			authContextRejectionRecord(t, output, "authentication_required")
		})
	}
}

func TestAuthContextRejectionCopiesOnlyRecordReferences(t *testing.T) {
	for _, kind := range []string{"access_token", "resource_access_ticket", "delegated_access_token"} {
		t.Run(kind, func(t *testing.T) {
			service, output := authContextDiagnosticFixture(t)
			ctx := context.WithValue(t.Context(), requestid.RequestIDKey, "auth-401-request")
			snapshot := &SessionCredentialAuthSnapshot{CredentialID: 42, CredentialFamilyID: 73,
				FamilyClientID: "private-sentinel", CredentialScopes: []string{"private-sentinel"}}
			original := withCredentialRecord(invalidCredential(CredentialInvalidTokenRevoked), snapshot, kind)
			snapshot.CredentialID, snapshot.CredentialFamilyID = 101, 102
			_, err := service.resolveSessionCredential(ctx, func(*Repository) (*SessionCredentialAuthSnapshot, error) {
				return nil, original
			}, nil)
			if err != original || err.Error() != commonapi.ErrUnauthorized.Error() {
				t.Fatal("record references changed the public authentication error")
			}
			record := authContextRejectionRecord(t, output, CredentialInvalidTokenRevoked)
			if record["credential_kind"] != kind || record["credential_id"] != "42" || record["family_id"] != "73" {
				t.Fatal("diagnostic did not retain the original record references")
			}
			encoded, marshalErr := json.Marshal(err)
			if marshalErr != nil || string(encoded) != `{"Reason":"token_revoked"}` {
				t.Fatal("private record references escaped error serialization")
			}
		})
	}
}

func TestAuthContextFailureDiagnosticsPreserveErrorsAndPrivacy(t *testing.T) {
	for _, test := range []struct {
		name, kind, state string
		err               error
	}{
		{name: "internal", kind: "internal", err: errors.New("private-sentinel addp_at_private-sentinel")},
		{name: "database connection refused", kind: "connection_refused", err: authContextConnectFailure(t, syscall.ECONNREFUSED)},
		{name: "database connection reset", kind: "connection_reset", err: authContextConnectFailure(t, syscall.ECONNRESET)},
		{name: "database unexpected EOF", kind: "connection_closed", err: authContextConnectFailure(t, io.ErrUnexpectedEOF)},
		{name: "database connection closed", kind: "connection_closed", err: authContextConnectFailure(t, io.EOF)},
		{name: "other database connect failure", kind: "database_connection", err: authContextConnectFailure(t, errors.New("private-sentinel addp_at_private-sentinel"))},
		{name: "database connect deadline", kind: "deadline_exceeded", err: authContextConnectFailure(t, context.DeadlineExceeded)},
		{name: "database connect cancellation", kind: "context_canceled", err: authContextConnectFailure(t, context.Canceled)},
		{name: "database connect network timeout", kind: "network_timeout", err: authContextConnectFailure(t, &net.DNSError{Err: "private-sentinel", IsTimeout: true})},
		{name: "IPv6 refusal then IPv4 timeout", kind: "network_timeout", err: authContextConnectFailure(t, syscall.ECONNREFUSED, &net.DNSError{Err: "private-sentinel", IsTimeout: true})},
		{name: "IPv6 refusal then IPv4 EOF", kind: "connection_closed", err: authContextConnectFailure(t, syscall.ECONNREFUSED, io.ErrUnexpectedEOF)},
		{name: "wrapped cancellation", kind: "context_canceled", err: fmt.Errorf("private-sentinel: %w", context.Canceled)},
		{name: "deadline", kind: "deadline_exceeded", err: context.DeadlineExceeded},
		{name: "network timeout", kind: "network_timeout", err: &net.DNSError{Err: "private-sentinel", IsTimeout: true}},
		{name: "database", kind: "database", state: "55P03", err: &pgconn.PgError{Code: "55P03", Message: "private-sentinel", Detail: "addp_at_private-sentinel"}},
		{name: "invalid sqlstate", kind: "internal", err: &pgconn.PgError{Code: "private-sentinel", Message: "private-sentinel"}},
		{name: "expected invalid credential", err: invalidCredential(CredentialInvalidTokenExpired)},
	} {
		t.Run(test.name, func(t *testing.T) {
			service, output := authContextDiagnosticFixture(t)
			ctx := context.WithValue(context.Background(), requestid.RequestIDKey, "auth-503-request")
			_, err := service.resolveSessionCredential(ctx, func(*Repository) (*SessionCredentialAuthSnapshot, error) {
				return nil, test.err
			}, nil)
			if err != test.err {
				t.Fatalf("diagnostics changed the original error: got %v, want %v", err, test.err)
			}
			if test.kind == "" {
				var record map[string]any
				if err := json.Unmarshal(output.Bytes(), &record); err != nil ||
					record["event"] != "iam.auth_context.rejected" || record["error_kind"] != nil {
					t.Fatal("expected credential rejection must not log an internal failure")
				}
				return
			}
			record := authContextDiagnosticRecord(t, output, "credential_snapshot", test.kind)
			if test.state != "" && record["sqlstate"] != test.state {
				t.Fatalf("SQLSTATE = %#v, want %q", record["sqlstate"], test.state)
			}
			if test.state == "" && record["sqlstate"] != nil {
				t.Fatalf("unexpected SQLSTATE: %#v", record["sqlstate"])
			}
		})
	}
}

func TestAuthContextFailureDiagnosticsIdentifyConnectionPoolWait(t *testing.T) {
	service, output := authContextDiagnosticFixture(t)
	pool, err := service.repository.db.DB()
	if err != nil {
		t.Fatal(err)
	}
	pool.SetMaxOpenConns(1)
	held, err := pool.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	ctx, cancel := context.WithCancel(context.WithValue(t.Context(), requestid.RequestIDKey, "auth-503-request"))
	defer cancel()
	result := make(chan error, 1)
	go func() {
		_, err := service.ResolveAuthContext(ctx, "addp_at_private-sentinel")
		result <- err
	}()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	poll := time.NewTicker(time.Millisecond)
	defer poll.Stop()
	for pool.Stats().WaitCount == 0 {
		select {
		case <-poll.C:
		case <-deadline.C:
			cancel()
			<-result
			t.Fatal("AuthContext did not wait for the occupied connection")
		}
	}
	cancel()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("pool wait cancellation = %v", err)
	}
	record := authContextDiagnosticRecord(t, output, "transaction_begin", "context_canceled")
	if record["pool_wait_count_total"].(float64) < 1 || record["pool_max_open"] != float64(1) {
		t.Fatalf("missing actual pool wait evidence: %#v", record)
	}
}

// Exercise pgx's real ConnectError and per-address wrappers without a socket,
// server, credential exchange or production dependency.
func authContextConnectFailure(t *testing.T, causes ...error) error {
	t.Helper()
	config, err := pgconn.ParseConfig("host=127.0.0.1 user=private-sentinel password=private-sentinel dbname=private-sentinel sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	for range causes[1:] {
		config.Fallbacks = append(config.Fallbacks, &pgconn.FallbackConfig{Host: "127.0.0.2", Port: config.Port})
	}
	attempts := 0
	config.DialFunc = func(context.Context, string, string) (net.Conn, error) {
		cause := causes[attempts]
		attempts++
		return nil, fmt.Errorf("private-sentinel: %w", cause)
	}
	_, err = pgconn.ConnectConfig(t.Context(), config)
	if err == nil {
		t.Fatal("expected the injected connection failure")
	}
	if attempts != len(causes) {
		t.Fatalf("connection attempts = %d, want %d", attempts, len(causes))
	}
	return err
}
