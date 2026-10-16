package postgresql

import (
	"bytes"
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/addp/common/engine/plugin"
)

func TestIntegrationPostgresQueryReadSessionBindsExactNumericText(t *testing.T) {
	db, pg, connInfo := openPostgresPrepareIntegration(t, false)
	defer db.Close()

	ctx := context.Background()
	schemaName := "common_pg_it"
	tableName := fmt.Sprintf("query_exact_numeric_%d", time.Now().UnixNano())
	createPostgresPrepareBaseTable(t, ctx, db, schemaName, tableName, `
		"id" bigint NOT NULL,
		"amount" numeric(38,20) NOT NULL
	`)
	defer dropPostgresPrepareTable(db, schemaName, tableName)

	const bigintValue = "9007199254740993"
	const decimalValue = "123456789012345678.12345678901234567890"
	if _, err := db.ExecContext(ctx, fmt.Sprintf(
		`INSERT INTO "%s"."%s" ("id", "amount") VALUES ($1, $2)`,
		schemaName,
		tableName,
	), bigintValue, decimalValue); err != nil {
		t.Fatalf("insert exact numeric row failed: %v", err)
	}

	prepared, err := pg.PrepareQuery(ctx, connInfo, plugin.QueryRequest{
		EngineID: 91,
		Language: "sql",
		Query: fmt.Sprintf(
			`SELECT "id"::text AS "id", "amount"::text AS "amount" FROM "%s"."%s" WHERE "id" = :id AND "amount" = :amount`,
			schemaName,
			tableName,
		),
		Options: plugin.QueryOptions{
			ReadOnly: true,
			Parameters: map[string]interface{}{
				"id":     bigintValue,
				"amount": decimalValue,
			},
		},
	})
	if err != nil {
		t.Fatalf("PrepareQuery failed: %v", err)
	}
	session, err := pg.OpenQueryReadSession(ctx, prepared)
	if err != nil {
		t.Fatalf("OpenQueryReadSession failed: %v", err)
	}
	defer session.Close(context.Background())

	batch, err := session.ReadBatch(ctx, 10)
	if err != nil {
		t.Fatalf("ReadBatch failed: %v", err)
	}
	if len(batch.Rows) != 1 || batch.Rows[0]["id"] != bigintValue || batch.Rows[0]["amount"] != decimalValue {
		t.Fatalf("query rows = %#v, want exact bigint and decimal text", batch.Rows)
	}
}

func TestIntegrationPostgresQueryReadSessionPreservesBytea(t *testing.T) {
	db, pg, connInfo := openPostgresPrepareIntegration(t, false)
	defer db.Close()
	ctx := context.Background()
	schemaName := "common_pg_it"
	tableName := fmt.Sprintf("query_bytea_%d", time.Now().UnixNano())
	createPostgresPrepareBaseTable(t, ctx, db, schemaName, tableName, `
  id bigint NOT NULL, payload bytea, label text NOT NULL,
  amount numeric(38,20) NOT NULL, document jsonb NOT NULL
 `)
	defer func() {
		if _, err := db.ExecContext(ctx, fmt.Sprintf(`DROP TABLE "%s"."%s"`, schemaName, tableName)); err != nil {
			t.Errorf("clean up query BYTEA table: %v", err)
		}
		var remaining bool
		if err := db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM pg_tables WHERE schemaname=$1 AND tablename=$2)`, schemaName, tableName).Scan(&remaining); err != nil || remaining {
			t.Errorf("query BYTEA cleanup not proven: remaining=%v error=%v", remaining, err)
		}
	}()
	payloads := [][]byte{{0, 0xff, 0x80, 'A'}, {}, nil, []byte("中文"), {1, 2, 3}}
	const amount = "123456789012345678.12345678901234567890"
	for index, payload := range payloads {
		var value interface{} = payload
		if payload == nil {
			value = nil
		}
		if _, err := db.ExecContext(ctx, fmt.Sprintf(`INSERT INTO "%s"."%s" VALUES ($1,$2,$3,$4,$5)`, schemaName, tableName), index, value, "plain { 中文", amount, `{"n":9007199254740993}`); err != nil {
			t.Fatal(err)
		}
	}
	prepared, err := pg.PrepareQuery(ctx, connInfo, plugin.QueryRequest{
		EngineID: 91, Language: "sql",
		Query:   fmt.Sprintf(`SELECT payload AS "raw bytes", payload || decode('', 'hex') AS "byte expression", label, amount, document FROM "%s"."%s" ORDER BY id`, schemaName, tableName),
		Options: plugin.QueryOptions{ReadOnly: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	session, err := pg.OpenQueryReadSession(ctx, prepared)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close(ctx)
	var firstPayload []byte
	for batchIndex := 0; batchIndex < 3; batchIndex++ {
		batch, err := session.ReadBatch(ctx, 2)
		if err != nil {
			t.Fatal(err)
		}
		wantRows := 2
		if batchIndex == 2 {
			wantRows = 1
		}
		if batch.Offset != int64(batchIndex*2) || len(batch.Rows) != wantRows {
			t.Fatalf("unexpected batch: %#v", batch)
		}
		for index, row := range batch.Rows {
			expected := payloads[batchIndex*2+index]
			for _, column := range []string{"raw bytes", "byte expression"} {
				value := row[column]
				if expected == nil {
					if value != nil {
						t.Fatalf("%s: SQL NULL = %#v", column, value)
					}
					continue
				}
				got, ok := value.([]byte)
				if !ok || got == nil || !bytes.Equal(got, expected) {
					t.Fatalf("%s = %#v (%T), want bytes %x", column, value, value, expected)
				}
				if batchIndex == 0 && index == 0 && column == "raw bytes" {
					firstPayload = got
				}
			}
			if row["label"] != "plain { 中文" || row["amount"] != amount || row["document"] != `{"n": 9007199254740993}` {
				t.Fatalf("non-binary values changed: %#v", row)
			}
		}
	}
	if !bytes.Equal(firstPayload, payloads[0]) {
		t.Fatalf("later batches changed retained bytes: %x", firstPayload)
	}
}
