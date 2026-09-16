package postgresql

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/query/sqlcompile"
)

type incorrectInstanceDateDialect struct{ analyticalExpressionDialect }

func (incorrectInstanceDateDialect) ISODateText(string) string { return "'incorrect'" }

// Force a real in-flight timeout rather than only an already-expired context.
type slowInstanceSession struct{ *sql.Conn }

func (s slowInstanceSession) QueryRowContext(ctx context.Context, _ string, _ ...any) *sql.Row {
	return s.Conn.QueryRowContext(ctx, "SELECT pg_sleep(1)")
}

func TestIntegrationPostgresAnalyticalInstance(t *testing.T) {
	db, provider, info := openPostgresPrepareIntegration(t, false)
	defer db.Close()

	t.Run("capability resolution", func(t *testing.T) {
		base := provider.Capabilities()
		resolved, err := provider.ResolveCapabilities(t.Context(), info, base)
		if err != nil || resolved.Compute.Query.Analytical == nil || !resolved.Compute.Query.Analytical.Supported {
			t.Fatalf("resolved=%#v err=%v", resolved.Compute, err)
		}
		if base.Compute.Query.Analytical != nil {
			t.Fatal("static template mutated")
		}
		encoded, err := plugin.GenerateResolvedCapabilities(t.Context(), &plugin.Engine{EngineType: provider.Type(), ConnectionInfo: info})
		if err != nil {
			t.Fatal(err)
		}
		parsed, err := plugin.ParseEngineCapabilities(encoded)
		if err != nil || !parsed.Compute.Query.Analytical.Supported {
			t.Fatalf("factory=%#v err=%v", parsed, err)
		}
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		failed, err := provider.ResolveCapabilities(ctx, info, resolved)
		if !errors.Is(err, context.Canceled) || failed.Compute.Query.Analytical.Supported {
			t.Fatalf("failed refresh retained capability: %#v %v", failed.Compute.Query.Analytical, err)
		}
		if !resolved.Compute.Query.Analytical.Supported {
			t.Fatal("failed refresh mutated last snapshot")
		}
		encoded, err = plugin.GenerateResolvedCapabilities(ctx, &plugin.Engine{EngineType: provider.Type(), ConnectionInfo: info})
		if !errors.Is(err, context.Canceled) || encoded != "" {
			t.Fatalf("failed factory published output: %s %v", encoded, err)
		}
	})
	conn, err := db.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	r, err := certifyAnalyticalInstance(t.Context(), conn)
	if err != nil || !r.Supported || len(r.Diagnostics) != 0 {
		t.Fatalf("certification=%#v err=%v", r, err)
	}
	if _, err = conn.ExecContext(t.Context(), "SET client_encoding TO 'LATIN1'"); err != nil {
		t.Fatal(err)
	}
	r, err = certifyAnalyticalInstance(t.Context(), conn)
	if err != nil || r.Supported || len(r.Diagnostics) != 1 || r.Diagnostics[0].Code != "analytical_encoding" {
		t.Fatalf("encoding=%#v err=%v", r, err)
	}
	var encoding string
	if err = conn.QueryRowContext(t.Context(), "SHOW client_encoding").Scan(&encoding); err != nil || encoding != "LATIN1" {
		t.Fatalf("probe changed encoding=%s err=%v", encoding, err)
	}
	if _, err = conn.ExecContext(t.Context(), "SET client_encoding TO 'UTF8'"); err != nil {
		t.Fatal(err)
	}

	tx, err := conn.BeginTx(t.Context(), &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		t.Fatal(err)
	}
	report, probeErr := certifyAnalyticalInstance(t.Context(), tx)
	rollbackErr := tx.Rollback()
	if probeErr != nil || !report.Supported || rollbackErr != nil {
		t.Fatalf("transaction=%#v probe=%v rollback=%v", report, probeErr, rollbackErr)
	}
	slowConn, err := db.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	slowContext, stopSlow := context.WithTimeout(t.Context(), 20*time.Millisecond)
	slowReport, slowErr := certifyAnalyticalInstance(slowContext, slowInstanceSession{slowConn})
	stopSlow()
	_ = slowConn.Close()
	if !errors.Is(slowErr, context.DeadlineExceeded) || slowReport.Supported {
		t.Fatalf("in-flight timeout=%#v err=%v", slowReport, slowErr)
	}
	r, err = sqlcompile.ProbeInstanceSemantics(t.Context(), conn, incorrectInstanceDateDialect{})
	if err != nil || r.Supported || len(r.Diagnostics) != 1 || r.Diagnostics[0].Code != "analytical_date_format" {
		t.Fatalf("semantics=%#v err=%v", r, err)
	}
	for _, expired := range []bool{false, true} {
		ctx, cancel := context.WithCancel(t.Context())
		want := error(context.Canceled)
		if expired {
			cancel()
			ctx, cancel = context.WithDeadline(t.Context(), time.Now().Add(-time.Second))
			want = context.DeadlineExceeded
		} else {
			cancel()
		}
		r, err = certifyAnalyticalInstance(ctx, conn)
		cancel()
		if !errors.Is(err, want) || r.Supported {
			t.Fatalf("canceled=%#v err=%v", r, err)
		}
	}
	if _, err = certifyAnalyticalInstance(t.Context(), nil); !errors.Is(err, plugin.ErrAnalyticalInvalid) {
		t.Fatal(err)
	}
	if err = conn.Close(); err != nil {
		t.Fatal(err)
	}
	r, err = certifyAnalyticalInstance(t.Context(), conn)
	if !errors.Is(err, sql.ErrConnDone) || r.Supported {
		t.Fatalf("closed=%#v err=%v", r, err)
	}
}
