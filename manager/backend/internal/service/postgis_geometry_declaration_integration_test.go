package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/spatial"
	"github.com/addp/manager/internal/models"
	"github.com/lib/pq"
)

func TestIntegrationPostgresManagerGeometryDeclarations(t *testing.T) {
	if os.Getenv("ADDP_POSTGRES_INTEGRATION") != "1" {
		t.Skip("run make test-manager-postgres with MANAGER_POSTGRES_TEST_DSN")
	}
	dsn := os.Getenv("MANAGER_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Fatal("MANAGER_POSTGRES_TEST_DSN is required")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	schema := fmt.Sprintf("manager_geom_test_%d", time.Now().UnixNano())
	quotedSchema := spatial.QuotePostGISIdentifier(schema)
	exec := func(t *testing.T, query string) {
		t.Helper()
		if _, err := db.ExecContext(ctx, query); err != nil {
			t.Fatalf("fixture SQL: %v", err)
		}
	}
	var extension bool
	if err := db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM pg_extension WHERE extname='postgis')").Scan(&extension); err != nil || !extension {
		t.Fatalf("Manager PostgreSQL gate requires PostGIS: installed=%v error=%v", extension, err)
	}
	exec(t, "CREATE SCHEMA "+quotedSchema)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cleanupCancel()
		if _, err := db.ExecContext(cleanupCtx, "DROP SCHEMA "+quotedSchema+" CASCADE"); err != nil {
			t.Errorf("cleanup owned schema: %v", err)
		}
		var exists bool
		if err := db.QueryRowContext(cleanupCtx, "SELECT EXISTS(SELECT 1 FROM pg_namespace WHERE nspname=$1)", schema).Scan(&exists); err != nil || exists {
			t.Errorf("owned schema residue: exists=%v error=%v", exists, err)
		}
	})
	for _, fixture := range []struct {
		name, dimension, wkt string
		flag                 int
	}{
		{"xy", "Geometry", "POINT(10 20)", 0},
		{"xyz", "GeometryZ", "POINT Z(10 20 30)", 2},
		{"xym", "GeometryM", "POINT M(10 20 40)", 1},
		{"xyzm", "GeometryZM", "POINT ZM(10 20 30 40)", 3},
	} {
		for _, typed := range []bool{true, false} {
			name := fmt.Sprintf("%s_typed_%t", fixture.name, typed)
			t.Run(name, func(t *testing.T) {
				source := spatial.QualifiedPostGISTable(schema, name)
				columnType := "geometry"
				if typed {
					columnType += "(" + fixture.dimension + ",4326)"
				}
				exec(t, "CREATE TABLE "+source+" (id integer PRIMARY KEY, shape "+columnType+")")
				exec(t, "INSERT INTO "+source+" VALUES (1, ST_GeomFromText('"+fixture.wkt+"',4326)), (2,NULL)")
				cfg, err := normalizeVectorMaterializedViewTaskConfig(newVectorMaterializedViewTaskDefinition().Config)
				if err != nil {
					t.Fatal(err)
				}
				cfg.Identity.Schema, cfg.Identity.Table = schema, name
				cfg.Options.TargetSchema, cfg.Options.Attributes = schema, nil
				plan := buildVectorMaterializedViewPlan(cfg, name+"_mv3857", "test")
				if _, err := executeVectorMaterializedViewPlan(ctx, db, cfg, plan); err != nil {
					t.Fatal(err)
				}
				declaration, err := queryGeometryColumnDeclaration(ctx, db, schema, plan.TargetTable, "geom_3857")
				if err != nil || declaration == nil || !declaration.Declared || declaration.SRID != 3857 || declaration.Type != fixture.dimension {
					t.Fatalf("target declaration=%#v error=%v", declaration, err)
				}
				// EWKB equality proves transformed coordinates, subtype, Z and M
				// are all unchanged by the added type declaration.
				var equal bool
				var flag, rows int
				query := "SELECT ST_AsEWKB(geom_3857)=ST_AsEWKB(ST_Transform(ST_GeomFromText($1,4326),3857)), ST_Zmflag(geom_3857), (SELECT COUNT(*) FROM " + spatial.QualifiedPostGISTable(schema, plan.TargetTable) + ") FROM " + spatial.QualifiedPostGISTable(schema, plan.TargetTable)
				if err := db.QueryRowContext(ctx, query, fixture.wkt).Scan(&equal, &flag, &rows); err != nil || !equal || flag != fixture.flag || rows != 1 {
					t.Fatalf("geometry preservation: equal=%v flag=%d rows=%d error=%v", equal, flag, rows, err)
				}
				target, err := discoverExternal3857MaterializedView(ctx, db, schema, name)
				if err != nil || target == nil || target.Table != plan.TargetTable {
					t.Fatalf("declared external target=%#v error=%v", target, err)
				}
			})
		}
	}

	t.Run("empty_declared_target_and_missing_declarations", func(t *testing.T) {
		exec(t, "CREATE TABLE "+quotedSchema+".empty_source (shape geometry(GeometryZM,4326))")
		cfg, err := normalizeVectorMaterializedViewTaskConfig(newVectorMaterializedViewTaskDefinition().Config)
		if err != nil {
			t.Fatal(err)
		}
		cfg.Identity.Schema, cfg.Identity.Table = schema, "empty_source"
		cfg.Options.TargetSchema, cfg.Options.Attributes = schema, nil
		plan := buildVectorMaterializedViewPlan(cfg, "empty_source_mv3857", "empty")
		if _, err := executeVectorMaterializedViewPlan(ctx, db, cfg, plan); err != nil {
			t.Fatal(err)
		}
		if target, err := discoverExternal3857MaterializedView(ctx, db, schema, "empty_source"); err != nil || target == nil {
			t.Fatalf("empty declared target must be recognized: %#v, %v", target, err)
		}
		status, err := validateManagerVectorMaterializedViewTarget(ctx, db, &models.VectorMaterializedView{
			TargetKind:   models.VectorMaterializedViewTargetKindSourceSchemaMaterializedView,
			TargetSchema: schema, TargetTable: plan.TargetTable,
			TargetGeometryColumn: "geom_3857", TargetSRID: 3857,
		})
		if err != nil || !status.Ready {
			t.Fatalf("empty Manager target must be ready without sampling: %#v, %v", status, err)
		}
		exec(t, "CREATE MATERIALIZED VIEW "+quotedSchema+".legacy_mv3857 AS SELECT ST_Transform(ST_GeomFromText('POINT(10 20)',4326),3857) AS geom_3857")
		exec(t, "CREATE INDEX legacy_gist ON "+quotedSchema+".legacy_mv3857 USING gist(geom_3857)")
		if target, err := discoverExternal3857MaterializedView(ctx, db, schema, "legacy"); err != nil || target != nil {
			t.Fatalf("untyped legacy target accepted: %#v, %v", target, err)
		}
		status, err = validateManagerVectorMaterializedViewTarget(ctx, db, &models.VectorMaterializedView{
			TargetKind:   models.VectorMaterializedViewTargetKindSourceSchemaMaterializedView,
			TargetSchema: schema, TargetTable: "legacy_mv3857",
			TargetGeometryColumn: "geom_3857", TargetSRID: 3857,
		})
		if err != nil || status.Ready || status.Reason != "vector materialized view target geometry srid is missing" {
			t.Fatalf("legacy Manager target must require regeneration: %#v, %v", status, err)
		}
		for _, fixture := range []struct{ table, definition string }{
			{"text_column", "geom_3857 varchar(3857)"},
			{"wrong_srid", "geom_3857 geometry(Geometry,4326)"},
			{"missing_column", "id integer"},
		} {
			exec(t, "CREATE TABLE "+spatial.QualifiedPostGISTable(schema, fixture.table)+" ("+fixture.definition+")")
			got, err := queryGeometryColumnDeclaredSRID(ctx, db, schema, fixture.table, "geom_3857")
			want := 0
			if fixture.table == "wrong_srid" {
				want = 4326
			}
			if err != nil || got != want {
				t.Fatalf("%s SRID=%d want=%d error=%v", fixture.table, got, want, err)
			}
		}
		declaration, err := queryGeometryColumnDeclaration(ctx, db, schema, "legacy_mv3857", "geom_3857")
		if err != nil || declaration == nil || declaration.Declared {
			t.Fatalf("legacy target was mutated: %#v, %v", declaration, err)
		}
	})

	t.Run("reject_mixed_or_unknown_dimensions_before_replacing_target", func(t *testing.T) {
		exec(t, "CREATE TABLE "+quotedSchema+".mixed (shape geometry)")
		exec(t, "INSERT INTO "+quotedSchema+".mixed VALUES (ST_GeomFromText('POINT(10 20)',4326)),(ST_GeomFromText('POINT Z(10 20 30)',4326))")
		exec(t, "CREATE TABLE "+quotedSchema+".unknown_empty (shape geometry)")
		for _, table := range []string{"mixed", "unknown_empty"} {
			cfg, _ := normalizeVectorMaterializedViewTaskConfig(newVectorMaterializedViewTaskDefinition().Config)
			cfg.Identity.Schema, cfg.Identity.Table = schema, table
			cfg.Options.TargetSchema, cfg.Options.Attributes = schema, nil
			plan := buildVectorMaterializedViewPlan(cfg, table+"_mv3857", "reject")
			exec(t, "CREATE MATERIALIZED VIEW "+spatial.QualifiedPostGISTable(schema, plan.TargetTable)+" AS SELECT 42 AS sentinel")
			if _, err := executeVectorMaterializedViewPlan(ctx, db, cfg, plan); err == nil || !strings.Contains(err.Error(), "dimensions") {
				t.Fatalf("%s generation should refuse unproven dimensions: %v", table, err)
			}
			var value int
			if err := db.QueryRowContext(ctx, "SELECT sentinel FROM "+spatial.QualifiedPostGISTable(schema, plan.TargetTable)).Scan(&value); err != nil || value != 42 {
				t.Fatalf("prior target changed: value=%d error=%v", value, err)
			}
			if exists, err := materializedViewExists(ctx, db, schema, plan.StagingTable); err != nil || exists {
				t.Fatalf("failed staging residue: exists=%v error=%v", exists, err)
			}
		}
	})

	t.Run("catalog_check_without_content_privilege", func(t *testing.T) {
		// A same-named type outside the extension must not shadow the
		// genuine geometry OID or make arbitrary typmods look spatial.
		exec(t, "CREATE TYPE "+quotedSchema+".geometry AS ENUM ('not_geometry')")
		exec(t, "CREATE TABLE "+quotedSchema+".fake_geometry (geom_3857 "+quotedSchema+".geometry)")
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		role := schema + "_reader"
		defer func() {
			_ = tx.Rollback()
			var exists bool
			if err := db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM pg_roles WHERE rolname=$1)", role).Scan(&exists); err != nil || exists {
				t.Errorf("owned role residue: exists=%v error=%v", exists, err)
			}
		}()
		for _, query := range []string{
			"CREATE ROLE " + spatial.QuotePostGISIdentifier(role) + " NOLOGIN",
			"GRANT USAGE ON SCHEMA " + quotedSchema + " TO " + spatial.QuotePostGISIdentifier(role),
			"SET LOCAL ROLE " + spatial.QuotePostGISIdentifier(role),
			"SET LOCAL search_path TO " + quotedSchema + ", public",
		} {
			if _, err := tx.ExecContext(ctx, query); err != nil {
				t.Fatal(err)
			}
		}
		srid, err := queryGeometryColumnDeclaredSRID(ctx, tx, schema, "xyz_typed_true_mv3857", "geom_3857")
		if err != nil || srid != 3857 {
			t.Fatalf("catalog-only SRID without SELECT=%d, %v", srid, err)
		}
		if srid, err := queryGeometryColumnDeclaredSRID(ctx, tx, schema, "fake_geometry", "geom_3857"); err != nil || srid != 0 {
			t.Fatalf("same-named non-PostGIS type accepted: SRID=%d, %v", srid, err)
		}
		if dimension, err := queryVectorMaterializedViewSourceDimension(ctx, tx, schema, "xyz_typed_true", "shape"); err != nil || dimension != "GeometryZ" {
			t.Fatalf("declared source dimension required content access: dimension=%s error=%v", dimension, err)
		}
		_, err = tx.ExecContext(ctx, "SELECT geom_3857 FROM "+quotedSchema+".xyz_typed_true_mv3857 LIMIT 1")
		var pgError *pq.Error
		if !errors.As(err, &pgError) || pgError.Code != "42501" {
			t.Fatalf("fixture must not have content access: %v", err)
		}
	})
}
