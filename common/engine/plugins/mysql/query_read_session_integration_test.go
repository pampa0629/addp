package mysql

import (
	"bytes"
	"context"
	"fmt"
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
