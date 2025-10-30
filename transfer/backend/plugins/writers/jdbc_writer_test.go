package writers

import "testing"

func TestJDBCWriterBuildPostgresInsertSQL(t *testing.T) {
	writer := &JDBCWriter{
		table:     "public.cities",
		columns:   []string{"id", "name", "geom"},
		writeMode: "insert",
		driver:    "postgres",
		geometryColumns: map[string]geometryColumnMeta{
			"geom": {SRID: 4326},
		},
	}

	sql := writer.buildInsertSQL()
	expected := `INSERT INTO "public"."cities" ("id", "name", "geom") VALUES ($1, $2, CASE WHEN $3 IS NULL THEN NULL ELSE ST_GeomFromWKB($3, 4326) END)`
	if sql != expected {
		t.Fatalf("unexpected SQL:\nexpected: %s\nactual:   %s", expected, sql)
	}
}

func TestJDBCWriterBuildPostgresUpsertSQL(t *testing.T) {
	writer := &JDBCWriter{
		table:       "public.cities",
		columns:     []string{"id", "name", "geom"},
		writeMode:   "upsert",
		driver:      "postgres",
		conflictKey: "id",
		geometryColumns: map[string]geometryColumnMeta{
			"geom": {SRID: 3857},
		},
	}

	sql := writer.buildInsertSQL()
	expected := `INSERT INTO "public"."cities" ("id", "name", "geom") VALUES ($1, $2, CASE WHEN $3 IS NULL THEN NULL ELSE ST_GeomFromWKB($3, 3857) END) ON CONFLICT ("id") DO UPDATE SET "name" = EXCLUDED."name", "geom" = EXCLUDED."geom"`
	if sql != expected {
		t.Fatalf("unexpected SQL:\nexpected: %s\nactual:   %s", expected, sql)
	}
}
