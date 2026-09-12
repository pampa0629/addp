package postgresql

import (
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
