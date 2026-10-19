package iam

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

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

func TestAuthContextFailureDiagnosticsPreserveErrorsAndPrivacy(t *testing.T) {
	for _, test := range []struct {
		name, kind, state string
		err               error
	}{
		{name: "internal", kind: "internal", err: errors.New("private-sentinel addp_at_private-sentinel")},
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
				if output.Len() != 0 {
					t.Fatalf("expected credential rejection should not log an internal failure: %s", output.String())
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
