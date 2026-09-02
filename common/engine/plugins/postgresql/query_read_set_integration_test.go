package postgresql

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/addp/common/engine/plugin"
)

func TestIntegrationResolvePostgresQueryReadSetAllowsTrustedBuiltins(t *testing.T) {
	db, pg, connInfo := openPostgresPrepareIntegration(t, false)
	defer db.Close()

	ctx := context.Background()
	schemaName := "common_pg_it"
	tableName := fmt.Sprintf("read_set_functions_%d", time.Now().UnixNano())
	createPostgresPrepareBaseTable(t, ctx, db, schemaName, tableName, `
		"activity_id" text NOT NULL,
		"activity_date_raw" text,
		"activity_status" text,
		"activity_level_raw" text
	`)
	defer dropPostgresPrepareTable(db, schemaName, tableName)

	query := fmt.Sprintf(`
		SELECT activity_id,
		       CASE
		         WHEN btrim(activity_date_raw) ~ '^[0-9]{4}-[0-9]{2}-[0-9]{2}$'
		          AND to_char(to_date(btrim(activity_date_raw), 'YYYY-MM-DD'), 'YYYY-MM-DD') = btrim(activity_date_raw)
		         THEN to_date(btrim(activity_date_raw), 'YYYY-MM-DD')
		         ELSE NULL
		       END AS activity_date,
		       CASE
		         WHEN btrim(activity_level_raw) ~ '^[+-]?[0-9]+([.][0-9]+)?$'
		         THEN btrim(activity_level_raw)::numeric
		         ELSE NULL
		       END AS activity_intensity,
		       count(*) OVER () AS total_count
		FROM "%s"."%s"
		WHERE coalesce(activity_status, '') NOT IN ('拟定中', '已取消')
		  AND nullif(btrim(activity_date_raw), '') IS NOT NULL
	`, schemaName, tableName)
	prepared, err := pg.PrepareQuery(ctx, connInfo, plugin.QueryRequest{
		EngineID: 91, Language: "sql", Query: query, Options: plugin.QueryOptions{ReadOnly: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	readSet, err := prepared.ReadSet(ctx)
	if err != nil {
		t.Fatalf("PreparedQuery.ReadSet failed: %v", err)
	}
	if len(readSet.Paths) != 1 {
		t.Fatalf("paths = %#v, want one", readSet.Paths)
	}
	assertPostgresReadPath(t, readSet.Paths[0], 91, schemaName, tableName, plugin.EngineCatalogKindTable)
}

func TestIntegrationResolvePostgresQueryReadSetRejectsUserFunction(t *testing.T) {
	db, pg, connInfo := openPostgresPrepareIntegration(t, false)
	defer db.Close()

	ctx := context.Background()
	schemaName := "common_pg_it"
	suffix := time.Now().UnixNano()
	tableName := fmt.Sprintf("read_set_user_function_%d", suffix)
	functionName := fmt.Sprintf("read_set_passthrough_%d", suffix)
	createPostgresPrepareBaseTable(t, ctx, db, schemaName, tableName, `"value" text`)
	if _, err := db.ExecContext(ctx, fmt.Sprintf(
		`CREATE FUNCTION "%s"."%s"(text) RETURNS text LANGUAGE SQL STABLE AS 'SELECT $1'`,
		schemaName, functionName,
	)); err != nil {
		t.Fatalf("create function failed: %v", err)
	}
	defer func() {
		_, _ = db.ExecContext(context.Background(), fmt.Sprintf(`DROP FUNCTION IF EXISTS "%s"."%s"(text)`, schemaName, functionName))
		dropPostgresPrepareTable(db, schemaName, tableName)
	}()

	prepared, err := pg.PrepareQuery(ctx, connInfo, plugin.QueryRequest{
		EngineID: 91, Language: "sql",
		Query: fmt.Sprintf(
			`SELECT "%s"."%s"(value) FROM "%s"."%s"`,
			schemaName, functionName, schemaName, tableName,
		),
		Options: plugin.QueryOptions{ReadOnly: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := prepared.ReadSet(ctx); !errors.Is(err, plugin.ErrQueryReadSetUnresolved) {
		t.Fatalf("PreparedQuery.ReadSet error = %v, want ErrQueryReadSetUnresolved", err)
	}
}

func TestIntegrationResolvePostgresQueryReadSetRejectsViewUserFunction(t *testing.T) {
	db, pg, connInfo := openPostgresPrepareIntegration(t, false)
	defer db.Close()

	ctx := context.Background()
	schemaName := "common_pg_it"
	suffix := time.Now().UnixNano()
	tableName := fmt.Sprintf("read_set_view_function_base_%d", suffix)
	functionName := fmt.Sprintf("read_set_view_passthrough_%d", suffix)
	viewName := fmt.Sprintf("read_set_view_function_%d", suffix)
	createPostgresPrepareBaseTable(t, ctx, db, schemaName, tableName, `"value" text`)
	if _, err := db.ExecContext(ctx, fmt.Sprintf(
		`CREATE FUNCTION "%s"."%s"(text) RETURNS text LANGUAGE SQL STABLE AS 'SELECT $1'`,
		schemaName, functionName,
	)); err != nil {
		t.Fatalf("create function failed: %v", err)
	}
	if _, err := db.ExecContext(ctx, fmt.Sprintf(
		`CREATE VIEW "%s"."%s" AS SELECT "%s"."%s"(value) AS value FROM "%s"."%s"`,
		schemaName, viewName, schemaName, functionName, schemaName, tableName,
	)); err != nil {
		t.Fatalf("create view failed: %v", err)
	}
	defer func() {
		_, _ = db.ExecContext(context.Background(), fmt.Sprintf(`DROP VIEW IF EXISTS "%s"."%s"`, schemaName, viewName))
		_, _ = db.ExecContext(context.Background(), fmt.Sprintf(`DROP FUNCTION IF EXISTS "%s"."%s"(text)`, schemaName, functionName))
		dropPostgresPrepareTable(db, schemaName, tableName)
	}()

	prepared, err := pg.PrepareQuery(ctx, connInfo, plugin.QueryRequest{
		EngineID: 91, Language: "sql",
		Query:   fmt.Sprintf(`SELECT value FROM "%s"."%s"`, schemaName, viewName),
		Options: plugin.QueryOptions{ReadOnly: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := prepared.ReadSet(ctx); !errors.Is(err, plugin.ErrQueryReadSetUnresolved) {
		t.Fatalf("PreparedQuery.ReadSet error = %v, want ErrQueryReadSetUnresolved", err)
	}
}

func TestIntegrationResolvePostgresQueryReadSetExpandsView(t *testing.T) {
	db, pg, connInfo := openPostgresPrepareIntegration(t, false)
	defer db.Close()

	ctx := context.Background()
	schemaName := "common_pg_it"
	suffix := time.Now().UnixNano()
	tableName := fmt.Sprintf("read_set_base_%d", suffix)
	viewName := fmt.Sprintf("read_set_view_%d", suffix)
	createPostgresPrepareBaseTable(t, ctx, db, schemaName, tableName, `"id" bigint`)
	if _, err := db.ExecContext(ctx, fmt.Sprintf(`CREATE VIEW "%s"."%s" AS SELECT id FROM "%s"."%s"`, schemaName, viewName, schemaName, tableName)); err != nil {
		t.Fatalf("create view failed: %v", err)
	}
	defer func() {
		_, _ = db.ExecContext(context.Background(), fmt.Sprintf(`DROP VIEW IF EXISTS "%s"."%s"`, schemaName, viewName))
		dropPostgresPrepareTable(db, schemaName, tableName)
	}()

	req := plugin.QueryRequest{
		EngineID: 91,
		Language: "sql",
		Query:    fmt.Sprintf(`SELECT id FROM "%s"."%s"`, schemaName, viewName),
		Options:  plugin.QueryOptions{ReadOnly: true},
	}
	prepared, err := pg.PrepareQuery(ctx, connInfo, req)
	if err != nil {
		t.Fatalf("PrepareQuery failed: %v", err)
	}
	readSet, err := prepared.ReadSet(ctx)
	if err != nil {
		t.Fatalf("PreparedQuery.ReadSet failed: %v", err)
	}
	if len(readSet.Paths) != 2 {
		t.Fatalf("paths = %#v, want view and base table", readSet.Paths)
	}
	assertPostgresReadPath(t, readSet.Paths[0], 91, schemaName, tableName, plugin.EngineCatalogKindTable)
	assertPostgresReadPath(t, readSet.Paths[1], 91, schemaName, viewName, "view")
	lineage, err := prepared.OutputLineage(ctx)
	if err != nil {
		t.Fatalf("PreparedQuery.OutputLineage failed: %v", err)
	}
	if len(lineage.Sources) != 2 || !lineage.Sources[0].OpaqueOutput || len(lineage.Sources[1].Bindings) != 1 || lineage.Sources[1].Bindings[0].Transformation != plugin.QueryOutputTransformationDirect {
		t.Fatalf("view output lineage = %#v", lineage)
	}
	result, err := prepared.Execute(ctx)
	if err != nil {
		t.Fatalf("PreparedQuery.Execute failed: %v", err)
	}
	if len(result.Rows) != 0 {
		t.Fatalf("query rows = %#v, want empty freshly-created view", result.Rows)
	}
}

func TestIntegrationResolvePostgresQueryOutputLineagePreservesAlias(t *testing.T) {
	db, pg, connInfo := openPostgresPrepareIntegration(t, false)
	defer db.Close()

	ctx := context.Background()
	schemaName := "common_pg_it"
	tableName := fmt.Sprintf("output_lineage_%d", time.Now().UnixNano())
	createPostgresPrepareBaseTable(t, ctx, db, schemaName, tableName, `"id" bigint, "phone" text`)
	defer dropPostgresPrepareTable(db, schemaName, tableName)

	prepared, err := pg.PrepareQuery(ctx, connInfo, plugin.QueryRequest{
		EngineID: 91, Language: "sql",
		Query:   fmt.Sprintf(`SELECT phone AS contact FROM "%s"."%s"`, schemaName, tableName),
		Options: plugin.QueryOptions{ReadOnly: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	lineage, err := prepared.OutputLineage(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(lineage.Sources) != 1 || len(lineage.Sources[0].Bindings) != 1 {
		t.Fatalf("output lineage = %#v", lineage)
	}
	binding := lineage.Sources[0].Bindings[0]
	if binding.Transformation != plugin.QueryOutputTransformationDirect || len(binding.SourcePath) != 1 || binding.SourcePath[0] != "phone" || len(binding.OutputPath) != 1 || binding.OutputPath[0] != "contact" {
		t.Fatalf("alias binding = %#v", binding)
	}
}

func TestIntegrationResolvePostgresQueryOutputLineageComposesPublishedServiceWrapper(t *testing.T) {
	db, pg, connInfo := openPostgresPrepareIntegration(t, false)
	defer db.Close()

	ctx := context.Background()
	schemaName := "common_pg_it"
	tableName := fmt.Sprintf("service_lineage_%d", time.Now().UnixNano())
	createPostgresPrepareBaseTable(t, ctx, db, schemaName, tableName, `"id" bigint NOT NULL, "phone" text`)
	defer dropPostgresPrepareTable(db, schemaName, tableName)

	prepared, err := pg.PrepareQuery(ctx, connInfo, plugin.QueryRequest{
		EngineID: 91, Language: "sql",
		Query: fmt.Sprintf(`
			SELECT addp_source.id, addp_source.contact
			FROM (SELECT id, phone AS contact FROM "%s"."%s") AS addp_source
			ORDER BY addp_source.id ASC LIMIT 3`, schemaName, tableName),
		Options: plugin.QueryOptions{ReadOnly: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	lineage, err := prepared.OutputLineage(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(lineage.Sources) != 1 || lineage.Sources[0].OpaqueOutput || len(lineage.Sources[0].Bindings) != 2 {
		t.Fatalf("service wrapper lineage = %#v", lineage)
	}
	assertPostgresOutputBinding(t, lineage.Sources[0].Bindings[0], "id", "id", plugin.QueryOutputTransformationDirect)
	assertPostgresOutputBinding(t, lineage.Sources[0].Bindings[1], "phone", "contact", plugin.QueryOutputTransformationDirect)
}
