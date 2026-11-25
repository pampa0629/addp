package mysql

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/addp/common/engine/plugin"
)

// Registered in the existing disposable Common MySQL gate.
func TestIntegrationMySQLQueryReadSession(t *testing.T) {
	db, provider, server, database := openMySQLUpsertIntegration(t)
	defer db.Close()
	defer dropMySQLIntegrationDatabase(db, database)
	ctx := t.Context()
	conn := plugin.ConnectionInfo{}
	for k, v := range server {
		conn[k] = v
	}
	conn["database"] = database
	table := mysqlDialect().QualifiedTable(database, "query_stream")
	if _, err := db.ExecContext(ctx, "CREATE TABLE "+table+" (id BIGINT PRIMARY KEY,phone VARCHAR(30),amount DECIMAL(38,18),raw_value VARBINARY(20)) ENGINE=InnoDB"); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 1007; i++ {
		var raw any = []byte{0, 255, byte(i)}
		if i == 2 {
			raw = []byte{}
		}
		if i == 3 {
			raw = nil
		}
		if _, err := db.ExecContext(ctx, "INSERT INTO "+table+" VALUES (?,?,?,?)", i, "13800138000", "12345678901234567890.123456789012345678", raw); err != nil {
			t.Fatal(err)
		}
	}
	prepare := func(query string, opts plugin.QueryOptions) plugin.PreparedQuery {
		t.Helper()
		p, err := provider.PrepareQuery(ctx, conn, plugin.QueryRequest{EngineID: 93, Language: "sql", Query: query, Options: opts})
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	t.Run("source_authorized_SQL", func(t *testing.T) {
		other := mysqlDialect().QualifiedTable(database, "other_stream")
		if _, err := db.ExecContext(ctx, "CREATE TABLE "+other+" (id BIGINT PRIMARY KEY, geom POINT NOT NULL SRID 4326) ENGINE=InnoDB"); err != nil {
			t.Fatal(err)
		}
		if _, err := db.ExecContext(ctx, "INSERT INTO "+other+" VALUES (1, ST_GeomFromText('POINT(45 90)', 4326))"); err != nil {
			t.Fatal(err)
		}
		user := "'reader_" + database[len(database)-19:] + "'@'%'"
		const password = "LocalReadOnly123!"
		if _, err := db.ExecContext(ctx, "CREATE USER "+user+" IDENTIFIED BY '"+password+"'"); err != nil {
			t.Fatal(err)
		}
		defer func() {
			if _, err := db.ExecContext(context.Background(), "DROP USER "+user); err != nil {
				t.Error(err)
			}
		}()
		for _, qualified := range []string{table, other} {
			if _, err := db.ExecContext(ctx, "GRANT SELECT ON "+qualified+" TO "+user); err != nil {
				t.Fatal(err)
			}
		}
		connInfo := plugin.ConnectionInfo{}
		for k, v := range conn {
			connInfo[k] = v
		}
		connInfo["user"], connInfo["password"] = "reader_"+database[len(database)-19:], password
		// Develop passes the original engine version and reuses its bounded pool.
		ctx := plugin.WithPoolEngineVersion(ctx, 8493, 1)
		defer plugin.ClosePool(8493)
		connInfo["pool_max_open_conns"], connInfo["pool_max_idle_conns"] = 1, 1
		pool, err := plugin.GetOrCreatePoolFromFactory(&plugin.Engine{ID: 8493, Version: 1, EngineType: "mysql", ConnectionInfo: connInfo}, nil)
		if err != nil {
			t.Fatal(err)
		}
		native, err := pool.DB()
		if err != nil {
			t.Fatal(err)
		}
		connectionID := func() int64 {
			t.Helper()
			var id int64
			if err := native.QueryRowContext(ctx, "SELECT CONNECTION_ID()").Scan(&id); err != nil {
				t.Fatal(err)
			}
			return id
		}
		original := connectionID()
		first := plugin.TabularItemPath(8493, "database", database, "query_stream")
		second := plugin.TabularItemPath(8493, "database", database, "other_stream")
		denied := errors.New("source authorization refused")
		single := "SELECT id FROM query_stream WHERE id=:id"
		for _, tc := range []struct {
			name, statement string
			sources         []plugin.EngineCatalogPath
			allow           bool
			checkRow        func(*testing.T, map[string]any)
		}{
			{"single", single, []plugin.EngineCatalogPath{first}, true, nil},
			{"ordinary_projection_with_spatial_column", "SELECT id FROM other_stream WHERE id=:id", []plugin.EngineCatalogPath{second}, true, nil},
			{"all_columns_with_spatial_column", "SELECT * FROM other_stream WHERE id=:id", []plugin.EngineCatalogPath{second}, true, func(t *testing.T, row map[string]any) {
				if geometry, ok := row["geom"].([]byte); !ok || len(geometry) < 9 {
					t.Fatalf("missing native spatial result: %T", row["geom"])
				}
			}},
			{"native_srs_dictionary", "SELECT id, ST_AsText(ST_Transform(geom, 3857)) AS projected FROM other_stream WHERE id=:id", []plugin.EngineCatalogPath{second}, true, func(t *testing.T, row map[string]any) {
				if actual := fmt.Sprint(row["projected"]); !strings.HasPrefix(actual, "POINT(10018754.") {
					t.Fatalf("unexpected projected point: %q", actual)
				}
			}},
			{"native_numeric_function", "SELECT id, ABS(-id) AS magnitude FROM other_stream WHERE id=:id", []plugin.EngineCatalogPath{second}, true, func(t *testing.T, row map[string]any) {
				if fmt.Sprint(row["magnitude"]) != "1" {
					t.Fatalf("unexpected numeric result: %v", row["magnitude"])
				}
			}},
			{"join_ungranted", "SELECT q.id FROM query_stream q JOIN other_stream o ON o.id=q.id WHERE q.id=:id", []plugin.EngineCatalogPath{first, second}, false, nil},
			{"recheck_after_withdrawal", single, []plugin.EngineCatalogPath{first}, false, nil},
		} {
			t.Run(tc.name, func(t *testing.T) {
				plan, err := provider.PrepareQuery(ctx, connInfo, plugin.QueryRequest{EngineID: 8493, Language: "sql", Query: tc.statement, Options: plugin.QueryOptions{ReadOnly: true, Parameters: map[string]any{"id": 1}}})
				if err != nil {
					t.Fatal(err)
				}
				expected, err := plugin.NewQueryReadSet(tc.sources...)
				if err != nil {
					t.Fatal(err)
				}
				calls := 0
				session, err := provider.OpenGuardedQueryReadSession(ctx, plan, func(_ context.Context, actual *plugin.QueryReadSet) error {
					calls++
					if !reflect.DeepEqual(expected, actual) {
						t.Fatalf("sources=%+v; want=%+v", actual, expected)
					}
					if !tc.allow {
						return denied
					}
					return nil
				})
				if !tc.allow {
					if session != nil {
						defer session.Close(ctx)
					}
					if calls != 1 || session != nil || !errors.Is(err, denied) {
						t.Fatalf("expected source rejection: calls=%d session=%t err=%v", calls, session != nil, err)
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				defer session.Close(ctx)
				observed, err := session.ReadSet(ctx)
				if err != nil || !reflect.DeepEqual(observed, expected) {
					t.Fatalf("actual sources=%+v err=%v", observed, err)
				}
				batch, err := session.ReadBatch(ctx, 2)
				if err != nil || calls != 1 || batch == nil || len(batch.Rows) != 1 || batch.Rows[0]["id"] != int64(1) {
					t.Fatalf("rows=%+v calls=%d err=%v", batch, calls, err)
				}
				if tc.checkRow != nil {
					tc.checkRow(t, batch.Rows[0])
				}
			})
		}
		if current := connectionID(); current != original {
			t.Fatalf("completed authorized reads replaced the pooled connection: original=%d current=%d", original, current)
		}
	})

	t.Run("complete_parameterized_stream", func(t *testing.T) {
		p := prepare("SELECT id,phone,amount,raw_value FROM "+table+" WHERE id>=:start ORDER BY id", plugin.QueryOptions{ReadOnly: true, Parameters: map[string]any{"start": 1}})
		s, err := provider.OpenQueryReadSession(ctx, p)
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close(ctx)
		if _, err := provider.OpenQueryReadSession(ctx, p); err == nil {
			t.Fatal("consumed plan reused")
		}
		if _, err := db.ExecContext(ctx, "UPDATE "+table+" SET phone='changed' WHERE id=1007"); err != nil {
			t.Fatal(err)
		}
		defer db.ExecContext(ctx, "UPDATE "+table+" SET phone='13800138000' WHERE id=1007")
		total := 0
		for {
			b, err := s.ReadBatch(ctx, 137)
			if err != nil {
				t.Fatal(err)
			}
			if b.Offset != int64(total) {
				t.Fatal("offset changed")
			}
			for _, row := range b.Rows {
				total++
				if row["phone"] != "13800138000" || row["id"] != int64(total) || row["amount"] != "12345678901234567890.123456789012345678" {
					t.Fatal("parameter/order/precision changed")
				}
				if total <= 3 {
					want := []byte{0, 255, byte(total)}
					if total == 2 {
						want = []byte{}
					}
					if total == 3 {
						if row["raw_value"] != nil {
							t.Fatal("NULL changed")
						}
					} else {
						got, ok := row["raw_value"].([]byte)
						if !ok || got == nil || !bytes.Equal(got, want) {
							t.Fatal("binary value changed")
						}
					}
				}
			}
			if len(b.Rows) < 137 {
				break
			}
		}
		if total != 1007 {
			t.Fatalf("implicit preview truncation: %d", total)
		}
	})
	t.Run("business_limit_and_output_finalizer", func(t *testing.T) {
		p := prepare("SELECT id,phone,amount FROM "+table+" ORDER BY id LIMIT 3", plugin.QueryOptions{ReadOnly: true})
		if err := p.(plugin.OutputTransformPreparedQuery).BindOutputTransforms(ctx, []plugin.QueryOutputTransform{{Column: "phone", Kind: "keep_prefix_suffix", Prefix: 0, Suffix: 0, MaskRune: "*", InvalidValue: "omit"}, {Column: "amount", Kind: "omit"}}); err != nil {
			t.Fatal(err)
		}
		s, err := provider.OpenQueryReadSession(ctx, p)
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close(ctx)
		total := 0
		for {
			b, err := s.ReadBatch(ctx, 2)
			if err != nil {
				t.Fatal(err)
			}
			for _, row := range b.Rows {
				if row["phone"] != "***********" {
					t.Fatal("raw field escaped batch finalizer")
				}
				if _, ok := row["amount"]; ok {
					t.Fatal("omitted field escaped finalizer")
				}
			}
			total += len(b.Rows)
			if len(b.Rows) < 2 {
				break
			}
		}
		if total != 3 {
			t.Fatal("business LIMIT changed", total)
		}
	})

	t.Run("empty_result_keeps_schema", func(t *testing.T) {
		p := prepare("SELECT id,amount,raw_value FROM "+table+" WHERE id<0", plugin.QueryOptions{ReadOnly: true})
		s, err := provider.OpenQueryReadSession(ctx, p)
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close(ctx)
		b, err := s.ReadBatch(ctx, 3)
		if err != nil || len(b.Rows) != 0 || len(b.Fields) != 3 || b.Fields[1].Precision != 38 || b.Fields[1].Scale != 18 {
			t.Fatal("empty result lost schema", err, b)
		}
	})
	t.Run("cancel_and_early_close", func(t *testing.T) {
		cancelCtx, cancel := context.WithCancel(ctx)
		p := prepare("SELECT id FROM "+table+" ORDER BY id", plugin.QueryOptions{ReadOnly: true})
		s, err := provider.OpenQueryReadSession(cancelCtx, p)
		if err != nil {
			t.Fatal(err)
		}
		cancel()
		if b, err := s.ReadBatch(cancelCtx, 2); err == nil || b != nil {
			t.Fatal("cancelled read returned data")
		}
		if err := s.Close(ctx); err != nil {
			t.Fatal(err)
		}
		p = prepare("SELECT id FROM "+table, plugin.QueryOptions{ReadOnly: true})
		s, err = provider.OpenQueryReadSession(ctx, p)
		if err != nil {
			t.Fatal(err)
		}
		if err = s.Close(ctx); err != nil {
			t.Fatal(err)
		}
		if err = s.Close(ctx); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("readonly_database_boundary", func(t *testing.T) {
		p := prepare("DELETE FROM "+table, plugin.QueryOptions{ReadOnly: false})
		if _, err := provider.OpenQueryReadSession(ctx, p); err == nil {
			t.Fatal("writable plan accepted")
		}
		for _, opts := range []plugin.QueryOptions{{ReadOnly: true, Limit: 1}, {ReadOnly: true, Offset: 1}} {
			p = prepare("SELECT id FROM "+table, opts)
			if _, err := provider.OpenQueryReadSession(ctx, p); err == nil {
				t.Fatal("preview bounds accepted")
			}
		}

		function := "`" + database + "`.`query_stream_write_probe`"
		if _, err := db.ExecContext(ctx, "CREATE FUNCTION "+function+"() RETURNS INT DETERMINISTIC MODIFIES SQL DATA BEGIN UPDATE "+table+" SET phone='forbidden' WHERE id=1; RETURN 1; END"); err != nil {
			t.Fatal(err)
		}
		defer db.ExecContext(ctx, "DROP FUNCTION "+function)
		p = prepare("SELECT "+function+"()", plugin.QueryOptions{ReadOnly: true})
		if session, err := provider.OpenQueryReadSession(ctx, p); err == nil {
			session.Close(ctx)
			t.Fatal("database read-only transaction allowed a writing function")
		}
		var phone string
		if err := db.QueryRowContext(ctx, "SELECT phone FROM "+table+" WHERE id=1").Scan(&phone); err != nil || phone != "13800138000" {
			t.Fatal("read-only boundary mutated source", err)
		}
		var count int
		if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table).Scan(&count); err != nil || count != 1007 {
			t.Fatal(fmt.Sprintf("source changed: %d %v", count, err))
		}
	})
}
