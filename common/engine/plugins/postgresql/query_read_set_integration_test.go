package postgresql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/models"
)

// Authorization targets use logical catalog paths, not physical creation identities.
// These DDL operations affect only this test's disposable fixture, never an enrolled source.
func TestIntegrationResolvePostgresQueryReadSetKeepsLogicalTargetAcrossSameNameRecreation(t *testing.T) {
	db, pg, connInfo := openPostgresPrepareIntegration(t, false)
	schemaName := "common_pg_it"
	tableName := fmt.Sprintf("read_set_identity_%d", time.Now().UnixNano())
	renamedName := tableName + "_renamed"
	t.Cleanup(func() {
		defer db.Close()
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		for _, name := range []string{tableName, renamedName} {
			if _, err := db.ExecContext(cleanupCtx, fmt.Sprintf(`DROP TABLE IF EXISTS "%s"."%s"`, schemaName, name)); err != nil {
				t.Errorf("cleanup lifecycle fixture %s failed: %v", name, err)
			}
		}
		var remaining int
		if err := db.QueryRowContext(cleanupCtx, `
			SELECT count(*) FROM pg_catalog.pg_class cls
			JOIN pg_catalog.pg_namespace ns ON ns.oid = cls.relnamespace
			WHERE ns.nspname = $1 AND cls.relname IN ($2, $3)
		`, schemaName, tableName, renamedName).Scan(&remaining); err != nil {
			t.Errorf("verify lifecycle fixture cleanup failed: %v", err)
		} else if remaining != 0 {
			t.Errorf("lifecycle fixture cleanup left %d relations", remaining)
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	catalog := &postgresDatabaseReadCatalog{db: db}
	readSet := func(name string) *plugin.QueryReadSet {
		t.Helper()
		prepared, err := pg.PrepareQuery(ctx, connInfo, plugin.QueryRequest{
			EngineID: 91, Language: "sql",
			Query:   fmt.Sprintf(`SELECT id FROM "%s"."%s"`, schemaName, name),
			Options: plugin.QueryOptions{ReadOnly: true},
		})
		if err != nil {
			t.Fatalf("prepare lifecycle query failed: %v", err)
		}
		result, err := prepared.ReadSet(ctx)
		if err != nil {
			t.Fatalf("resolve lifecycle read set failed: %v", err)
		}
		if len(result.Paths) != 1 {
			t.Fatalf("expected one lifecycle read path, got %#v", result.Paths)
		}
		assertPostgresReadPath(t, result.Paths[0], 91, schemaName, name, plugin.EngineCatalogKindTable)
		return result
	}
	fingerprint := func(name string) string {
		return models.GenerateItemFingerprint(91, schemaName+"."+name)
	}

	createPostgresPrepareBaseTable(t, ctx, db, schemaName, tableName, `"id" bigint NOT NULL`)
	originalReadSet := readSet(tableName)
	originalFingerprint := fingerprint(tableName)

	if _, err := db.ExecContext(ctx, fmt.Sprintf(`ALTER TABLE "%s"."%s" RENAME TO "%s"`, schemaName, tableName, renamedName)); err != nil {
		t.Fatalf("rename lifecycle fixture failed: %v", err)
	}
	if _, err := catalog.ResolveRelation(ctx, postgresRelationReference{Schema: schemaName, Name: tableName}); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("old name after rename should not resolve, got %v", err)
	}
	if fingerprint(renamedName) == originalFingerprint || reflect.DeepEqual(readSet(renamedName), originalReadSet) {
		t.Fatal("rename must change the name-derived fingerprint and read path")
	}

	if _, err := db.ExecContext(ctx, fmt.Sprintf(`DROP TABLE "%s"."%s"`, schemaName, renamedName)); err != nil {
		t.Fatalf("drop lifecycle fixture failed: %v", err)
	}
	createPostgresPrepareBaseTable(t, ctx, db, schemaName, tableName, `"id" bigint NOT NULL`)
	if fingerprint(tableName) != originalFingerprint || !reflect.DeepEqual(readSet(tableName), originalReadSet) {
		t.Fatal("same-name recreation must retain the logical locator fingerprint and read path")
	}
}

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

func TestIntegrationResolvePostgresQueryReadSetAllowsTrustedPostGISFunctions(t *testing.T) {
	db, pg, connInfo := openPostgresPrepareIntegration(t, false)
	defer db.Close()

	ctx := context.Background()
	schemaName := "common_pg_it"
	tableName := fmt.Sprintf("read_set_postgis_%d", time.Now().UnixNano())
	createPostgresPrepareBaseTable(t, ctx, db, schemaName, tableName, `"id" bigint NOT NULL, "shape" geometry(Point, 4326)`)
	defer dropPostgresPrepareTable(db, schemaName, tableName)

	query := fmt.Sprintf(`
		SELECT id, ST_AsGeoJSON(shape) AS shape
		FROM "%s"."%s"
		WHERE ST_Intersects(shape, ST_MakeEnvelope($1, $2, $3, $4, $5))
	`, schemaName, tableName)
	prepared, err := pg.PrepareQuery(ctx, connInfo, plugin.QueryRequest{
		EngineID: 91, Language: "sql", Query: query,
		Options: plugin.QueryOptions{
			ReadOnly: true,
			Args:     []interface{}{112.5, 27.5, 114.5, 29.5, 4326},
		},
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
